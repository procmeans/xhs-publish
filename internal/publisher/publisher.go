// Package publisher drives the Xiaohongshu creator web UI via Playwright.
//
// The approach mirrors the "Playwright MCP 小红书全自动发布" guide: instead of
// scripting the login (which triggers captchas), we attach over the Chrome
// DevTools Protocol to a browser the user already logged into once, and reuse
// that session for every publish.
package publisher

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/procmeans/xhs-publish/internal/task"
)

const publishURL = "https://creator.xiaohongshu.com/publish/publish"

// Options configures a publish run.
type Options struct {
	CDPEndpoint string        // e.g. http://localhost:9222
	DryRun      bool          // fill everything but do NOT click 发布
	Headful     bool          // unused for CDP attach; kept for clarity
	Humanize    bool          // add human-like pauses/mouse paths/typos
	Human       HumanProfile  // tunes HOW human the behavior is (when Humanize)
	StepTimeout time.Duration // per-action timeout
}

// HumanProfile tunes the humanization behavior. All factors are multipliers
// around the built-in baseline (1.0 = the hand-tuned default); a zero value for
// any field falls back to its default via withDefaults, so a manually built
// Options{Humanize: true} still behaves sensibly.
type HumanProfile struct {
	TypoRate    float64 // chance of fumbling a character (0 disables typos)
	SpeedFactor float64 // typing speed multiplier; >1 faster, <1 slower
	Caution     float64 // multiplier on "thinking" pauses & hesitation
	Fatigue     bool    // warm up then slow down over long passages
}

// DefaultHumanProfile is the hand-tuned baseline behavior.
func DefaultHumanProfile() HumanProfile {
	return HumanProfile{
		TypoRate:    0.05,
		SpeedFactor: 1.0,
		Caution:     1.0,
		Fatigue:     true,
	}
}

// withDefaults replaces non-positive factors with their baseline so partially
// filled profiles (and the zero value) stay usable. TypoRate is left as-is so
// callers can deliberately set it to 0 to disable typos.
func (h HumanProfile) withDefaults() HumanProfile {
	d := DefaultHumanProfile()
	if h.SpeedFactor <= 0 {
		h.SpeedFactor = d.SpeedFactor
	}
	if h.Caution <= 0 {
		h.Caution = d.Caution
	}
	if h.TypoRate < 0 {
		h.TypoRate = 0
	}
	return h
}

// DefaultOptions returns sane defaults.
func DefaultOptions() Options {
	return Options{
		CDPEndpoint: "http://localhost:9222",
		DryRun:      true,
		Humanize:    true,
		Human:       DefaultHumanProfile(),
		StepTimeout: 60 * time.Second,
	}
}

// Publisher owns a Playwright connection to an already-running Chrome.
type Publisher struct {
	pw      *playwright.Playwright
	browser playwright.Browser
	page    playwright.Page
	opt     Options

	human        bool         // humanization on
	profile      HumanProfile // tunables for the humanization behavior
	rng          *rand.Rand   // randomness for pauses/jitter/typos
	lastX, lastY float64      // tracked cursor position for mouse paths
}

// New attaches to the Chrome instance exposed on opt.CDPEndpoint.
//
// Start that Chrome with:
//
//	google-chrome --remote-debugging-port=9222 --user-data-dir=/path/to/profile
//
// and log into Xiaohongshu once in it.
func New(opt Options) (*Publisher, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("start playwright driver: %w", err)
	}
	browser, err := pw.Chromium.ConnectOverCDP(opt.CDPEndpoint)
	if err != nil {
		pw.Stop()
		return nil, fmt.Errorf("connect to chrome at %s: %w\n"+
			"Is Chrome running with --remote-debugging-port? See scripts/start-chrome.sh",
			opt.CDPEndpoint, err)
	}
	if len(browser.Contexts()) == 0 {
		browser.Close()
		pw.Stop()
		return nil, fmt.Errorf("no browser context found on %s", opt.CDPEndpoint)
	}
	ctx := browser.Contexts()[0]
	ctx.SetDefaultTimeout(float64(opt.StepTimeout.Milliseconds()))

	// Reuse an existing tab if present, otherwise open one.
	var page playwright.Page
	if pages := ctx.Pages(); len(pages) > 0 {
		page = pages[0]
	} else {
		if page, err = ctx.NewPage(); err != nil {
			browser.Close()
			pw.Stop()
			return nil, fmt.Errorf("open page: %w", err)
		}
	}

	p := &Publisher{
		pw:      pw,
		browser: browser,
		page:    page,
		opt:     opt,
		human:   opt.Humanize,
		profile: opt.Human.withDefaults(),
		// Seed from wall-clock so each run's noise differs.
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
		lastX: 660 + float64(rand.Intn(120)),
		lastY: 360 + float64(rand.Intn(120)),
	}
	if p.human {
		p.HardenStealth() // mask navigator.webdriver before we touch the site
	}
	return p, nil
}

// Close detaches from Chrome. It does NOT close the user's browser.
func (p *Publisher) Close() {
	if p.browser != nil {
		_ = p.browser.Close() // detaches the CDP connection only
	}
	if p.pw != nil {
		_ = p.pw.Stop()
	}
}

// EnsureLoggedIn verifies the attached session is authenticated.
func (p *Publisher) EnsureLoggedIn() error {
	if _, err := p.page.Goto(publishURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return fmt.Errorf("navigate to creator center: %w", err)
	}
	// Logged out → redirected to a login page.
	if u := p.page.URL(); strings.Contains(u, "login") {
		return fmt.Errorf("session is not logged in (landed on %s). "+
			"Log into Xiaohongshu in the attached Chrome, then retry", u)
	}
	return nil
}

// Publish runs the full flow for one task.
func (p *Publisher) Publish(t *task.PublishTask) error {
	if err := p.EnsureLoggedIn(); err != nil {
		return err
	}

	if err := p.selectTab(t.Kind); err != nil {
		return err
	}

	media, err := t.MediaFiles()
	if err != nil {
		return err
	}
	if err := p.uploadMedia(t.Kind, media); err != nil {
		return err
	}

	if err := p.fillTitle(t.Title); err != nil {
		return err
	}
	if err := p.fillContent(t); err != nil {
		return err
	}

	if p.opt.DryRun {
		log.Println("[dry-run] form filled; NOT clicking 发布. Review the browser, then re-run with --publish.")
		return nil
	}
	return p.clickPublish()
}

// selectTab switches between the 上传图文 / 上传视频 tabs.
func (p *Publisher) selectTab(kind task.Kind) error {
	label := "上传图文"
	if kind == task.KindVideo {
		label = "上传视频"
	}
	p.pause(500, 1300) // look at the page before choosing a tab
	// The tab labels live in a few possible containers; match by visible text.
	tab := p.page.Locator(fmt.Sprintf(`div:has-text("%s"), span:has-text("%s")`, label, label)).First()
	if err := p.humanClickLocator(tab); err != nil {
		// Image tab is usually default; only hard-fail for video.
		if kind == task.KindVideo {
			return fmt.Errorf("select %q tab: %w", label, err)
		}
		log.Printf("note: could not click %q tab (likely already active): %v", label, err)
	}
	p.pause(700, 1100)
	return nil
}

// uploadMedia feeds files to the hidden <input type=file> and waits for the
// uploads to settle.
//
// We set the files via a raw CDP DOM.setFileInputFiles call rather than
// Playwright's SetInputFiles. SetInputFiles streams the file bytes over the CDP
// wire, which the protocol caps at 50MB when attached over CDP (ConnectOverCDP)
// — far too small for an HD video that can be hundreds of MB or a GB. CDP's
// DOM.setFileInputFiles instead hands the browser a local filesystem path it
// reads itself, so there is no size limit (the browser is co-located with the
// files). Playwright's SetInputFiles remains a fallback if CDP resolution fails.
func (p *Publisher) uploadMedia(kind task.Kind, files []string) error {
	input := p.page.Locator(`input[type="file"]`).First()
	// Ensure the input exists in the DOM before resolving it over CDP.
	if err := input.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateAttached,
		Timeout: ms(p.opt.StepTimeout),
	}); err != nil {
		return fmt.Errorf("file input not found: %w", err)
	}
	if err := p.cdpSetInputFiles(files); err != nil {
		log.Printf("note: CDP upload failed (%v); falling back to Playwright SetInputFiles (50MB cap)", err)
		if err := input.SetInputFiles(files); err != nil {
			return fmt.Errorf("set input files: %w", err)
		}
	}
	log.Printf("uploaded %d file(s); waiting for processing...", len(files))

	if kind == task.KindVideo {
		// Video transcoding can take a while. Wait until the title field is
		// editable, which the platform only enables once upload completes.
		if err := p.titleLocator().WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(10 * 60 * 1000), // up to 10 min
		}); err != nil {
			return fmt.Errorf("video upload did not finish in time: %w", err)
		}
	} else {
		// Images upload fast; give thumbnails a moment to render.
		p.page.WaitForTimeout(3000)
	}
	return nil
}

// cdpSetInputFiles sets file paths on the first <input type=file> via raw CDP
// (DOM.setFileInputFiles), which reads files from local disk with no size limit
// — unlike Playwright's SetInputFiles over CDP, which is capped at 50MB.
func (p *Publisher) cdpSetInputFiles(files []string) error {
	sess, err := p.page.Context().NewCDPSession(p.page)
	if err != nil {
		return fmt.Errorf("new cdp session: %w", err)
	}
	defer sess.Detach()

	if _, err := sess.Send("DOM.enable", nil); err != nil {
		return fmt.Errorf("DOM.enable: %w", err)
	}
	// Resolve the file input element to a CDP remote object id.
	res, err := sess.Send("Runtime.evaluate", map[string]interface{}{
		"expression": `document.querySelector('input[type=file]')`,
	})
	if err != nil {
		return fmt.Errorf("locate input via cdp: %w", err)
	}
	objectID, err := evalObjectID(res)
	if err != nil {
		return err
	}
	if _, err := sess.Send("DOM.setFileInputFiles", map[string]interface{}{
		"files":    files,
		"objectId": objectID,
	}); err != nil {
		return fmt.Errorf("DOM.setFileInputFiles: %w", err)
	}
	return nil
}

// evalObjectID extracts result.objectId from a Runtime.evaluate CDP response.
func evalObjectID(res interface{}) (string, error) {
	m, ok := res.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected cdp response type %T", res)
	}
	result, ok := m["result"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("cdp evaluate: missing result object")
	}
	objectID, ok := result["objectId"].(string)
	if !ok || objectID == "" {
		return "", fmt.Errorf("cdp evaluate: file input not present in DOM")
	}
	return objectID, nil
}

func (p *Publisher) fillTitle(title string) error {
	loc := p.titleLocator()
	if err := loc.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: ms(p.opt.StepTimeout),
	}); err != nil {
		return fmt.Errorf("title field not ready: %w", err)
	}
	p.pause(400, 1100) // glance at the title field before typing
	if err := p.humanTypeWithRetype(loc, title); err != nil {
		return fmt.Errorf("type title: %w", err)
	}
	return nil
}

// fillContent types the body followed by hashtags. Topics are typed (not
// pasted) so the editor's # autocomplete can bind them to real topic pages.
func (p *Publisher) fillContent(t *task.PublishTask) error {
	editor := p.page.Locator(`div[contenteditable="true"]`).First()
	if err := editor.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: ms(p.opt.StepTimeout),
	}); err != nil {
		return fmt.Errorf("content editor not ready: %w", err)
	}
	if t.Content != "" {
		if err := p.humanType(editor, t.Content); err != nil {
			return fmt.Errorf("type content: %w", err)
		}
	}
	for _, topic := range t.NormalizedTopics() {
		p.pause(300, 900) // think before adding the next tag
		// keep the editor focused, then type the tag at a human cadence
		if err := p.humanType(editor, " "+topic); err != nil {
			return fmt.Errorf("type topic %q: %w", topic, err)
		}
		// Wait for the # suggestion dropdown, then accept the first item.
		p.pause(700, 1300)
		if err := p.page.Keyboard().Press("Enter"); err != nil {
			log.Printf("note: could not confirm topic %q via Enter: %v", topic, err)
		}
		p.pause(200, 500)
	}
	return nil
}

// clickPublish clicks the 发布 sub-button inside the <xhs-publish-btn> web
// component. That component renders 暂存离开 / 发布 inside a CLOSED shadow root,
// so the labels are invisible to text/role/CSS selectors (and the red is a
// gradient, not a queryable background-color). We instead target the host
// element — which exposes readable attributes (submit-text, submit-disabled) —
// wait until submit-disabled="false" (video processing finished), then click
// the right-hand (发布) pill by position.
func (p *Publisher) clickPublish() error {
	host := p.page.Locator("xhs-publish-btn").First()
	if err := host.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(10 * 60 * 1000),
	}); err != nil {
		return fmt.Errorf("publish component <xhs-publish-btn> not found: %w", err)
	}
	if err := host.ScrollIntoViewIfNeeded(); err != nil {
		log.Printf("note: scroll publish button into view: %v", err)
	}
	if err := p.waitPublishEnabled(6 * time.Minute); err != nil {
		return err
	}

	box, err := host.BoundingBox()
	if err != nil || box == nil {
		return fmt.Errorf("publish component has no bounding box: %w", err)
	}
	// 暂存离开 sits on the left of the component, 发布 on the right (~0.61 across).
	cx := box.X + box.Width*0.61 + p.jitter(box.Width*0.03)
	cy := box.Y + box.Height*0.5 + p.jitter(box.Height*0.18)
	p.readPage()       // skim the finished post before committing
	p.pause(500, 1400) // a beat of hesitation before committing
	if err := p.humanClickXY(cx, cy); err != nil {
		return fmt.Errorf("click 发布: %w", err)
	}
	// A successful publish navigates away from the form (to the note list).
	p.page.WaitForTimeout(5000)
	log.Printf("publish clicked; current URL: %s", p.page.URL())
	return nil
}

// waitPublishEnabled polls the component's submit-disabled attribute until the
// platform finishes processing the video and enables 发布.
func (p *Publisher) waitPublishEnabled(timeout time.Duration) error {
	const step = 2 * time.Second
	for waited := time.Duration(0); waited < timeout; waited += step {
		v, err := p.page.Evaluate(`() => {
			const e = document.querySelector('xhs-publish-btn');
			return e ? e.getAttribute('submit-disabled') : null;
		}`)
		if s, ok := v.(string); ok && s == "false" {
			return nil
		} else if err != nil {
			log.Printf("note: polling submit-disabled: %v", err)
		}
		log.Printf("waiting for 发布 to enable (video processing)... %s", waited)
		p.page.WaitForTimeout(float64(step.Milliseconds()))
	}
	return fmt.Errorf("发布 still disabled after %s — video may still be processing", timeout)
}

// titleLocator matches the title <input> by its placeholder text, falling back
// to any non-file text input on the form.
func (p *Publisher) titleLocator() playwright.Locator {
	return p.page.Locator(`input[placeholder*="标题"], input[type="text"]:not([type="file"])`).First()
}

func ms(d time.Duration) *float64 { return playwright.Float(float64(d.Milliseconds())) }
