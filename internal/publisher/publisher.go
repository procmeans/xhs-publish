// Package publisher drives a creator web UI (Xiaohongshu, Douyin, …) via
// Playwright.
//
// The approach mirrors the "Playwright MCP 小红书全自动发布" guide: instead of
// scripting the login (which triggers captchas), we attach over the Chrome
// DevTools Protocol to a browser the user already logged into once, and reuse
// that session for every publish.
//
// Everything that is the SAME across platforms — attaching over CDP, the
// human-like interaction layer (human.go), uploading media with no size cap —
// lives on *Publisher. Everything that DIFFERS per site — which URL to open,
// how to tell "logged out", the title/body selectors, the cover editor, the
// publish button — lives behind the Platform interface (see xhs.go, douyin.go).
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

// Platform is one publishing site's page-flow: the steps that differ between
// Xiaohongshu, Douyin, and any future target. Each method receives the shared
// *Publisher so it can reuse the human-like helpers (humanType, humanClick…)
// and the CDP plumbing.
type Platform interface {
	// Name is the human-readable platform name (used in log/error messages).
	Name() string
	// Host is the creator-center hostname; used to pick this platform's tab out
	// of an already-open multi-tab Chrome so we never hijack another site's tab.
	Host() string
	// PublishURL is the page to open to start composing a note of this kind.
	PublishURL(kind task.Kind) string
	// LoggedOut reports whether the current URL means "not authenticated"
	// (i.e. we got bounced to a login/passport page).
	LoggedOut(currentURL string) bool
	// SelectTab switches the composer to the image/video mode (may be a no-op
	// on sites whose upload page is already kind-specific).
	SelectTab(p *Publisher, kind task.Kind) error
	// ReadyLocator becomes visible only once media upload/processing finishes;
	// the orchestrator waits on it to ride out video transcoding. Usually the
	// title field.
	ReadyLocator(p *Publisher) playwright.Locator
	// FillTitle types the note title.
	FillTitle(p *Publisher, title string) error
	// FillContent types the body and hashtags.
	FillContent(p *Publisher, t *task.PublishTask) error
	// SetCover uploads a dedicated cover image (best-effort; a returned error is
	// logged and the platform's default frame is kept).
	SetCover(p *Publisher, coverPath string) error
	// ClickPublish commits the post.
	ClickPublish(p *Publisher) error
}

// platformFor maps a platform key to its implementation. An empty key defaults
// to Xiaohongshu so existing task files / commands keep working unchanged.
func platformFor(name string) (Platform, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "xhs", "xiaohongshu", "小红书":
		return xhsSite{}, nil
	case "douyin", "dy", "抖音":
		return douyinSite{}, nil
	default:
		return nil, fmt.Errorf("unknown platform %q (want %q or %q)", name, "xhs", "douyin")
	}
}

// Options configures a publish run.
type Options struct {
	Platform    string        // "xhs" (default) or "douyin"
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
		Platform:    "xhs",
		CDPEndpoint: "http://localhost:9222",
		DryRun:      true,
		Humanize:    true,
		Human:       DefaultHumanProfile(),
		StepTimeout: 60 * time.Second,
	}
}

// Publisher owns a Playwright connection to an already-running Chrome and a
// chosen Platform whose page-flow it drives.
type Publisher struct {
	pw      *playwright.Playwright
	browser playwright.Browser
	page    playwright.Page
	opt     Options
	site    Platform

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
// and log into the target platform once in it.
func New(opt Options) (*Publisher, error) {
	site, err := platformFor(opt.Platform)
	if err != nil {
		return nil, err
	}
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

	// Pick THIS platform's tab so we never hijack another site's tab (e.g. an
	// in-progress 小红书 note) when several creator tabs are open. Prefer a tab
	// already on the platform host; otherwise open a fresh tab for our work.
	var page playwright.Page
	for _, pg := range ctx.Pages() {
		if strings.Contains(pg.URL(), site.Host()) {
			page = pg
			break
		}
	}
	if page == nil {
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
		site:    site,
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

// ensureLoggedIn navigates to the platform's compose page and verifies the
// attached session is authenticated.
func (p *Publisher) ensureLoggedIn(kind task.Kind) error {
	url := p.site.PublishURL(kind)
	if _, err := p.page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return fmt.Errorf("navigate to %s creator center: %w", p.site.Name(), err)
	}
	if u := p.page.URL(); p.site.LoggedOut(u) {
		return fmt.Errorf("%s session is not logged in (landed on %s). "+
			"Log into %s in the attached Chrome, then retry", p.site.Name(), u, p.site.Name())
	}
	return nil
}

// Publish runs the full flow for one task against the configured platform.
func (p *Publisher) Publish(t *task.PublishTask) error {
	if err := p.ensureLoggedIn(t.Kind); err != nil {
		return err
	}
	if err := p.site.SelectTab(p, t.Kind); err != nil {
		return err
	}

	media, err := t.MediaFiles()
	if err != nil {
		return err
	}
	if err := p.uploadMedia(t.Kind, media); err != nil {
		return err
	}

	if err := p.site.FillTitle(p, t.Title); err != nil {
		return err
	}
	if err := p.site.FillContent(p, t); err != nil {
		return err
	}

	// Custom cover: upload a dedicated cover image rather than letting the
	// platform default to a video frame. Best-effort — a missing cover control
	// must not block the publish.
	if t.Kind == task.KindVideo && t.Cover != "" {
		if err := p.site.SetCover(p, t.Cover); err != nil {
			log.Printf("note: set custom cover failed (%v); %s will fall back to a video frame", err, p.site.Name())
		} else {
			log.Printf("custom cover set: %s", t.Cover)
		}
	}

	if p.opt.DryRun {
		log.Println("[dry-run] form filled; NOT clicking 发布. Review the browser, then re-run with --publish.")
		return nil
	}
	return p.site.ClickPublish(p)
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
		if err := p.site.ReadyLocator(p).WaitFor(playwright.LocatorWaitForOptions{
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

// cdpSetCoverFile sets a cover image onto an image-accepting <input type=file>
// via raw CDP DOM.setFileInputFiles (no 50MB cap, no native file dialog). It
// prefers an input scoped to scopeSel (the cover editor modal, e.g.
// ".cover-modal" on xhs or ".semi-modal" on douyin) and falls back to any
// image input on the page. The cover editor modal must already be open.
func (p *Publisher) cdpSetCoverFile(path, scopeSel string) error {
	sess, err := p.page.Context().NewCDPSession(p.page)
	if err != nil {
		return fmt.Errorf("new cdp session: %w", err)
	}
	defer sess.Detach()

	if _, err := sess.Send("DOM.enable", nil); err != nil {
		return fmt.Errorf("DOM.enable: %w", err)
	}
	res, err := sess.Send("Runtime.evaluate", map[string]interface{}{
		"expression": `(() => {
			const inModal = document.querySelector('` + scopeSel + ` input[type=file][accept*="image"]');
			if (inModal) return inModal;
			const xs = Array.from(document.querySelectorAll('input[type=file]'));
			return xs.find(e => ((e.getAttribute('accept') || '').toLowerCase().includes('image'))) || null;
		})()`,
	})
	if err != nil {
		return fmt.Errorf("locate cover input via cdp: %w", err)
	}
	objectID, err := evalObjectID(res)
	if err != nil {
		return fmt.Errorf("cover image <input type=file> not found in modal: %w", err)
	}
	if _, err := sess.Send("DOM.setFileInputFiles", map[string]interface{}{
		"files":    []string{path},
		"objectId": objectID,
	}); err != nil {
		return fmt.Errorf("DOM.setFileInputFiles(cover): %w", err)
	}
	return nil
}

// domHas reports whether the page currently has an element matching sel.
func (p *Publisher) domHas(sel string) bool {
	v, err := p.page.Evaluate(`(s) => !!document.querySelector(s)`, sel)
	if err != nil {
		return false
	}
	b, _ := v.(bool)
	return b
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

func ms(d time.Duration) *float64 { return playwright.Float(float64(d.Milliseconds())) }
