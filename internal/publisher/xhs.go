package publisher

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/procmeans/xhs-publish/internal/task"
)

// xhsSite implements Platform for the Xiaohongshu (小红书) creator center.
type xhsSite struct{}

const xhsPublishURL = "https://creator.xiaohongshu.com/publish/publish"

func (xhsSite) Name() string { return "小红书" }

func (xhsSite) Host() string { return "creator.xiaohongshu.com" }

func (xhsSite) PublishURL(task.Kind) string { return xhsPublishURL }

// LoggedOut: when logged out, 小红书 redirects the compose page to a login URL.
func (xhsSite) LoggedOut(currentURL string) bool {
	return strings.Contains(currentURL, "login")
}

func (xhsSite) ReadyLocator(p *Publisher) playwright.Locator { return xhsTitleLocator(p) }

// SelectTab switches to the 上传图文 / 上传视频 tab.
//
// The composer defaults to whichever tab the account last used (some accounts
// land on 上传视频). We must positively land on the right one, because uploadMedia
// feeds files to the first <input type=file> on the page — the WRONG tab's input
// would silently swallow the file and the editor would never appear. So we click
// the tab by its exact-text position (real mouse move, for stealth) and VERIFY
// the switch via the file input's accept type, retrying before giving up.
func (xhsSite) SelectTab(p *Publisher, kind task.Kind) error {
	label := "上传图文"
	wantImage := true
	if kind == task.KindVideo {
		label, wantImage = "上传视频", false
	}
	p.pause(500, 1300) // look at the page before choosing a tab

	if xhsOnTab(p, wantImage) {
		p.pause(300, 700)
		return nil // already on the right tab
	}
	for i := 0; i < 15; i++ {
		if err := xhsClickTab(p, label); err != nil {
			log.Printf("note: click %q tab (attempt %d): %v", label, i+1, err)
		}
		p.page.WaitForTimeout(p.randMs(700, 1100))
		if xhsOnTab(p, wantImage) {
			p.pause(300, 700)
			return nil
		}
	}
	return fmt.Errorf("could not switch to the %q tab (composer stayed on the other uploader)", label)
}

// xhsOnTab reports whether the composer is currently on the image (or video)
// uploader, judged by the accept type of the page's file input.
func xhsOnTab(p *Publisher, wantImage bool) bool {
	v, err := p.page.Evaluate(`() => {
		const e = document.querySelector('input[type=file]');
		return e ? (e.getAttribute('accept') || '').toLowerCase() : '';
	}`)
	if err != nil {
		return false
	}
	acc, _ := v.(string)
	isImage := strings.Contains(acc, "image") || strings.Contains(acc, ".jpg") || strings.Contains(acc, ".png") || strings.Contains(acc, ".webp")
	isVideo := strings.Contains(acc, "video") || strings.Contains(acc, ".mp4") || strings.Contains(acc, ".mov")
	if wantImage {
		return isImage && !isVideo
	}
	return isVideo && !isImage
}

// asFloat coerces a value pulled out of a Playwright Evaluate result to float64.
// Playwright-go decodes integer-valued JS numbers as int (getBoundingClientRect
// can return whole CSS pixels), so a plain v.(float64) assertion would fail and
// silently yield 0 — which is exactly the bug that made tab clicks land at (0,0).
func asFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	}
	return 0
}

// xhsClickTab locates the tab whose exact trimmed text is label (the topmost,
// smallest leaf element — i.e. the real tab, not an ancestor container) and
// clicks its center with a human mouse move. Falls back to a JS click if the
// element can't be measured.
func xhsClickTab(p *Publisher, label string) error {
	// Visibility is judged by the on-screen rect (getBoundingClientRect), NOT
	// offsetParent — the tab bar is position:fixed/sticky, whose offsetParent is
	// null even while fully visible, which would wrongly exclude the real tab.
	v, err := p.page.Evaluate(`(lbl) => {
		const vis = e => { const r = e.getBoundingClientRect();
			return r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < (window.innerHeight || 900); };
		const els = Array.from(document.querySelectorAll('div,span,button,a'))
			.filter(e => e.children.length === 0 && (e.textContent || '').trim() === lbl && vis(e));
		if (!els.length) return null;
		els.sort((a, b) => a.getBoundingClientRect().top - b.getBoundingClientRect().top);
		const r = els[0].getBoundingClientRect();
		return { x: r.x, y: r.y, w: r.width, h: r.height };
	}`, label)
	if err != nil {
		return err
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		// fall back to a direct JS click on the same element
		_, _ = p.page.Evaluate(`(lbl) => {
			const vis = e => { const r = e.getBoundingClientRect();
				return r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < (window.innerHeight || 900); };
			const t = Array.from(document.querySelectorAll('div,span,button,a'))
				.find(e => e.children.length === 0 && (e.textContent || '').trim() === lbl && vis(e));
			if (t) (t.closest('[class]') || t).click();
		}`, label)
		return fmt.Errorf("tab %q not found for mouse click; used JS fallback", label)
	}
	x, y, w, h := asFloat(m["x"]), asFloat(m["y"]), asFloat(m["w"]), asFloat(m["h"])
	if w <= 0 || h <= 0 {
		return fmt.Errorf("tab %q has a zero-size rect", label)
	}
	return p.humanClickXY(x+w*0.5+p.jitter(w*0.15), y+h*0.5+p.jitter(h*0.2))
}

func (xhsSite) FillTitle(p *Publisher, title string) error {
	loc := xhsTitleLocator(p)
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

// FillContent types the body followed by hashtags. Topics are typed (not
// pasted) so the editor's # autocomplete can bind them to real topic pages.
func (xhsSite) FillContent(p *Publisher, t *task.PublishTask) error {
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
	// Move the caret to the very end of the editor. humanType clicks to focus,
	// and a click in a long body drops the caret mid-text, which scatters the
	// hashtags through the content. Collapse the selection to the end instead.
	caretToEnd := `(() => {
		const el = document.querySelector('div[contenteditable="true"]');
		if (!el) return false;
		el.focus();
		const r = document.createRange();
		r.selectNodeContents(el);
		r.collapse(false);
		const s = window.getSelection();
		s.removeAllRanges();
		s.addRange(r);
		return true;
	})()`
	kb := p.page.Keyboard()
	for _, topic := range t.NormalizedTopics() {
		p.pause(300, 900) // think before adding the next tag
		// Re-anchor the caret at the end (no click), then type the tag so the
		// editor's # autocomplete can bind it to a real topic page.
		if _, err := p.page.Evaluate(caretToEnd); err != nil {
			return fmt.Errorf("move caret to end before topic %q: %w", topic, err)
		}
		p.pause(120, 300)
		for _, r := range " " + topic {
			if isTypeable(r) {
				if err := kb.Press(string(r), playwright.KeyboardPressOptions{
					Delay: playwright.Float(p.randMs(35, 110) / p.profile.SpeedFactor),
				}); err != nil {
					return fmt.Errorf("type topic %q: %w", topic, err)
				}
			} else if err := p.cdpIMEType(r); err != nil {
				// CJK topic char via real IME composition, not a bare InsertText.
				return fmt.Errorf("type topic %q: %w", topic, err)
			}
			p.page.WaitForTimeout(p.randMs(40, 120))
		}
		// Wait for the # suggestion dropdown, then accept the first item.
		p.pause(700, 1300)
		if err := kb.Press("Enter"); err != nil {
			log.Printf("note: could not confirm topic %q via Enter: %v", topic, err)
		}
		p.pause(200, 500)
	}
	return nil
}

// SetCover uploads a dedicated cover image for a video note. The flow mirrors a
// human's: 小红书 defaults to a video frame and only mounts the cover image
// <input type=file> inside an editor modal. So we (1) click the "修改封面" entry
// to open the cover editor (it appears only after recommended covers finish
// loading), (2) push the image onto the modal's image input via raw CDP
// DOM.setFileInputFiles (no 50MB cap, no native file dialog), (3) click 确定.
// Best-effort: any step failing returns an error and the caller keeps the
// default video-frame cover.
func (xhsSite) SetCover(p *Publisher, path string) error {
	const imgInputSel = `input[type=file][accept*="image"]`

	// 1. Open the cover editor. The "修改封面" entry (.operator.noCover.pointer)
	//    only exists once recommended covers load, so poll for up to ~30s.
	openJS := `(() => {
		if (document.querySelector('.cover-modal ` + `input[type=file][accept*="image"]` + `')) return true;
		const e = document.querySelector('.cover-plugin-preview .operator.noCover.pointer')
			|| Array.from(document.querySelectorAll('div')).find(x => { const c=(x.className||'').toString(); return c.includes('noCover') && c.includes('pointer'); })
			|| Array.from(document.querySelectorAll('div,span')).find(x => (x.innerText||'').trim() === '修改封面');
		if (e) { e.click(); }
		return false;
	})()`
	opened := false
	for i := 0; i < 30; i++ {
		if p.domHas(`.cover-modal ` + imgInputSel) {
			opened = true
			break
		}
		_, _ = p.page.Evaluate(openJS)
		p.page.WaitForTimeout(1000)
	}
	if !opened {
		return fmt.Errorf("cover editor did not open (recommended covers may still be loading)")
	}

	// 2. Push the image onto the modal's image input via CDP.
	if err := p.cdpSetCoverFile(path, ".cover-modal"); err != nil {
		return err
	}

	// 3. The upload lands in the editor's image strip as a blob thumbnail;
	//    click it so it becomes the active cover on the canvas.
	p.page.WaitForTimeout(2500)
	selectJS := `(() => {
		const m = document.querySelector('.cover-modal'); if (!m) return false;
		const img = Array.from(m.querySelectorAll('img')).find(e => (e.src||'').startsWith('blob:'));
		if (!img) return false;
		(img.closest('[class]') || img).click();
		img.click();
		return true;
	})()`
	if v, _ := p.page.Evaluate(selectJS); v != true {
		return fmt.Errorf("uploaded cover thumbnail not found to select")
	}

	// 4. Confirm (确定) to apply the cover to the note.
	p.page.WaitForTimeout(1500)
	confirmJS := `(() => {
		const m = document.querySelector('.cover-modal'); if (!m) return false;
		const btn = Array.from(m.querySelectorAll('button')).find(b => (b.innerText||'').trim() === '确定');
		if (!btn) return false; btn.click(); return true;
	})()`
	v, err := p.page.Evaluate(confirmJS)
	if b, ok := v.(bool); err != nil || !ok || !b {
		return fmt.Errorf("cover confirm (确定) not clicked (err=%v)", err)
	}
	p.page.WaitForTimeout(2000)
	return nil
}

// ClickPublish clicks the 发布 sub-button inside the <xhs-publish-btn> web
// component. That component renders 暂存离开 / 发布 inside a CLOSED shadow root,
// so the labels are invisible to text/role/CSS selectors (and the red is a
// gradient, not a queryable background-color). We instead target the host
// element — which exposes readable attributes (submit-text, submit-disabled) —
// wait until submit-disabled="false" (video processing finished), then click
// the right-hand (发布) pill by position.
func (xhsSite) ClickPublish(p *Publisher) error {
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
	if err := xhsWaitPublishEnabled(p, 6*time.Minute); err != nil {
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

// xhsWaitPublishEnabled polls the component's submit-disabled attribute until
// the platform finishes processing the video and enables 发布.
func xhsWaitPublishEnabled(p *Publisher, timeout time.Duration) error {
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

// xhsTitleLocator matches the title <input> by its placeholder text, falling
// back to any non-file text input on the form.
func xhsTitleLocator(p *Publisher) playwright.Locator {
	return p.page.Locator(`input[placeholder*="标题"], input[type="text"]:not([type="file"])`).First()
}
