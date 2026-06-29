package publisher

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/procmeans/xhs-publish/internal/task"
)

// douyinSite implements Platform for the Douyin (抖音) creator center
// (creator.douyin.com). Scope today is VIDEO notes only.
//
// Selectors are best-effort: Douyin uses Semi Design (semi-* classes) and
// reshuffles its compose page often, so each step prefers a stable text match
// and falls back to structural selectors. Use cmd/xhsdebug (-platform douyin,
// or just point it at the open douyin tab) to re-find anything that drifts.
type douyinSite struct{}

// douyinUploadURL is the video upload entry. After the file is accepted Douyin
// transitions this same tab to the publish/edit form.
const douyinUploadURL = "https://creator.douyin.com/creator-micro/content/upload"

func (douyinSite) Name() string { return "抖音" }

func (douyinSite) Host() string { return "creator.douyin.com" }

func (douyinSite) PublishURL(task.Kind) string { return douyinUploadURL }

// LoggedOut: when logged out, Douyin bounces creator-micro pages to its passport
// /login domain (or away from the creator host entirely).
func (douyinSite) LoggedOut(currentURL string) bool {
	if strings.Contains(currentURL, "login") || strings.Contains(currentURL, "passport") {
		return true
	}
	return !strings.Contains(currentURL, "creator.douyin.com")
}

// ReadyLocator: the 作品标题 input only mounts once the upload+form transition
// completes, so it doubles as the "upload finished" signal.
func (douyinSite) ReadyLocator(p *Publisher) playwright.Locator { return douyinTitleLocator(p) }

// SelectTab is a no-op for video: douyinUploadURL is already the video uploader.
// Image notes are not supported yet.
func (douyinSite) SelectTab(_ *Publisher, kind task.Kind) error {
	if kind != task.KindVideo {
		return fmt.Errorf("抖音 image (图文) publishing is not supported yet; use kind=\"video\"")
	}
	return nil
}

func (douyinSite) FillTitle(p *Publisher, title string) error {
	loc := douyinTitleLocator(p)
	if err := loc.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: ms(p.opt.StepTimeout),
	}); err != nil {
		return fmt.Errorf("title field not ready: %w", err)
	}
	p.pause(400, 1100)
	if err := p.humanTypeWithRetype(loc, title); err != nil {
		return fmt.Errorf("type title: %w", err)
	}
	return nil
}

// FillContent types the body into the 作品简介 editor, then appends hashtags.
// Douyin's editor opens a 话题 dropdown when you type "#word"; we type the tag
// and press Enter (with Space as a fallback) so it binds to a real topic.
func (douyinSite) FillContent(p *Publisher, t *task.PublishTask) error {
	editor := douyinDescLocator(p)
	if err := editor.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: ms(p.opt.StepTimeout),
	}); err != nil {
		return fmt.Errorf("description editor not ready: %w", err)
	}
	if t.Content != "" {
		if err := p.humanType(editor, t.Content); err != nil {
			return fmt.Errorf("type content: %w", err)
		}
	}
	// Move the caret to the very end of the editor before each tag. humanType
	// clicks to focus, and a click in a long body drops the caret mid-text, which
	// scatters the hashtags through the content. Collapse the selection to the
	// end instead.
	const editorSel = `.editor-kit-container [contenteditable="true"], .zone-container[contenteditable="true"], div[contenteditable="true"]`
	caretToEnd := `(() => {
		const el = document.querySelector('` + editorSel + `');
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
	// 抖音 caps a post at 5 话题: the editor silently refuses to commit a 6th tag
	// and leaves it as stray text that interleaves with its neighbours (the
	// "#A#I废土I动画" garble). So we cap at 5 — within the cap each tag commits
	// cleanly.
	const maxDouyinTopics = 5
	topics := t.NormalizedTopics()
	if len(topics) > maxDouyinTopics {
		log.Printf("note: 抖音 allows at most %d 话题; keeping the first %d, dropping %v",
			maxDouyinTopics, maxDouyinTopics, topics[maxDouyinTopics:])
		topics = topics[:maxDouyinTopics]
	}
	for _, topic := range topics {
		word := strings.TrimPrefix(topic, "#")
		// Snapshot committed tags before this one. The mention commit is flaky
		// (Slate re-render timing), so we verify a new token appeared and retry
		// once; if a try fails it leaves stray "#word" text we must strip.
		_, before := douyinTagCounts(p, editorSel)
		committed := false
		for try := 0; try < 2 && !committed; try++ {
			p.pause(300, 700) // think before adding the tag
			// Never start a tag while a previous mention dropdown is still open —
			// that is what makes adjacent tags interleave.
			if douyinMentionOpen(p) {
				_ = kb.Press("Escape")
				douyinWaitMention(p, false, 1500*time.Millisecond)
			}
			if _, err := p.page.Evaluate(caretToEnd); err != nil {
				return fmt.Errorf("move caret to end before topic %q: %w", topic, err)
			}
			p.pause(150, 350)
			// One "#" keypress opens the 话题 dropdown; the word then goes in as a
			// single atomic InsertText so per-character typing can't interleave
			// with the editor's async re-render.
			if err := kb.InsertText(" "); err != nil {
				return fmt.Errorf("type topic %q: %w", topic, err)
			}
			p.page.WaitForTimeout(p.randMs(60, 160))
			if err := kb.Press("#"); err != nil {
				return fmt.Errorf("type topic %q: %w", topic, err)
			}
			p.page.WaitForTimeout(p.randMs(120, 260))
			if err := kb.InsertText(word); err != nil {
				return fmt.Errorf("type topic %q: %w", topic, err)
			}
			p.page.WaitForTimeout(p.randMs(120, 260))
			// Wait for the dropdown, then commit by clicking the matching item
			// (more reliable than Enter, which no-ops when nothing is highlighted).
			douyinWaitMention(p, true, 4*time.Second)
			p.pause(400, 800)
			if !douyinClickMentionExact(p, word) {
				_ = kb.Press("Enter")
			}
			if !douyinWaitMention(p, false, 3*time.Second) {
				_ = kb.Press("Escape")
				douyinWaitMention(p, false, 1500*time.Millisecond)
			}
			p.pause(250, 500)

			if _, after := douyinTagCounts(p, editorSel); after > before {
				committed = true
				break
			}
			// Not committed: the typed "#word" sits as stray text at the very end.
			// Strip ONLY that trailing stray by backspacing until the '#' count
			// returns to the committed-tag count (`before`). The body has no '#'
			// and each committed tag has exactly one, so this never deletes body
			// text or other tags — it stops the instant the stray '#' is gone.
			_, _ = p.page.Evaluate(caretToEnd)
			for i := 0; i < 25; i++ {
				if h, _ := douyinTagCounts(p, editorSel); h <= before {
					break
				}
				_ = kb.Press("Backspace")
				p.page.WaitForTimeout(p.randMs(60, 130))
			}
		}
		if !committed {
			log.Printf("note: 抖音 topic %q did not commit after retry; skipped it", topic)
		}
	}

	// Self-declaration (自主声明). For AI-generated content, declare 内容由AI生成 —
	// 抖音 surfaces this declaration on the post and expects it for synthetic
	// media. Best-effort: a missing control must not block the publish.
	if t.AIGenerated {
		if err := douyinSetAIDeclaration(p); err != nil {
			log.Printf("note: set 自主声明=内容由AI生成 failed: %v", err)
		} else {
			log.Printf("自主声明 set: 内容由AI生成")
		}
	}
	return nil
}

// douyinSetAIDeclaration opens the 自主声明 panel ("对作品内容添加声明"), selects the
// 内容由AI生成 radio, and confirms (确定 enables only after a选择). Best-effort.
func douyinSetAIDeclaration(p *Publisher) error {
	opened, _ := p.page.Evaluate(`(() => {
		const el = Array.from(document.querySelectorAll('*'))
			.find(e => e.childNodes.length <= 2 && (e.innerText || '').trim() === '请选择自主声明');
		if (!el) return false;
		el.scrollIntoView({block: 'center'});
		el.click();
		return true;
	})()`)
	if opened != true {
		return fmt.Errorf("自主声明 control (请选择自主声明) not found")
	}
	p.page.WaitForTimeout(1500)

	// Select 内容由AI生成 with a REAL mouse click. The option is a Semi Design
	// radio whose controlled onChange does NOT fire on a synthetic JS .click(),
	// so 确定 would stay disabled; a real mouse event toggles it.
	radio := p.page.Locator(`.semi-radio:has-text("内容由AI生成")`).First()
	if err := radio.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(5000),
	}); err != nil {
		return fmt.Errorf("内容由AI生成 radio not found: %w", err)
	}
	if err := p.humanClickLocator(radio); err != nil {
		return fmt.Errorf("click 内容由AI生成: %w", err)
	}
	p.pause(300, 600)

	// Confirm (确定 enables only after a选择).
	confirm := p.page.Locator(`button.semi-button:has-text("确定")`).First()
	if err := p.humanClickLocator(confirm); err != nil {
		return fmt.Errorf("click 确定: %w", err)
	}
	p.page.WaitForTimeout(1000)
	return nil
}

// douyinTagCounts returns (hashes, mentions): the number of '#' characters in
// the editor text and the number of committed data-mention topic tokens. The
// body carries no '#', so hashes>mentions means stray uncommitted tag text is
// present.
func douyinTagCounts(p *Publisher, editorSel string) (hashes, mentions int) {
	v, err := p.page.Evaluate(`(sel) => {
		const ed = document.querySelector(sel);
		if (!ed) return {h: 0, m: 0};
		return {
			h: (ed.innerText.match(/#/g) || []).length,
			m: ed.querySelectorAll('[data-mention]').length,
		};
	}`, editorSel)
	if err != nil {
		return 0, 0
	}
	toInt := func(x interface{}) int {
		switch n := x.(type) {
		case int:
			return n
		case float64:
			return int(n)
		}
		return 0
	}
	if m, ok := v.(map[string]interface{}); ok {
		return toInt(m["h"]), toInt(m["m"])
	}
	return 0, 0
}

// douyinMentionOpen reports whether the 话题/@ mention dropdown is currently
// visible. Note 发文助手's suggest-* panel is always present, so we key on the
// mention-specific containers (publish-mention-wrapper / mention-suggest).
func douyinMentionOpen(p *Publisher) bool {
	v, err := p.page.Evaluate(`(() => Array.from(document.querySelectorAll('[class*="publish-mention"],[class*="mention-suggest"]'))
		.some(e => e.offsetParent !== null))()`)
	if err != nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

// douyinWaitMention polls until the mention dropdown's visibility matches want
// (true=open, false=closed) or the timeout elapses; returns whether it reached
// the desired state.
func douyinWaitMention(p *Publisher, want bool, timeout time.Duration) bool {
	const step = 200 * time.Millisecond
	for waited := time.Duration(0); waited < timeout; waited += step {
		if douyinMentionOpen(p) == want {
			return true
		}
		p.page.WaitForTimeout(float64(step.Milliseconds()))
	}
	return false
}

// douyinClickMentionExact clicks the 话题 suggestion item whose text matches
// word, committing the tag cleanly. Returns false if no matching item is
// visible (caller falls back to Enter). It deliberately does NOT click a
// non-matching first item, which would commit an unrelated/garbled tag.
func douyinClickMentionExact(p *Publisher, word string) bool {
	v, err := p.page.Evaluate(`(w) => {
		const items = Array.from(document.querySelectorAll('[class*="mention-suggest-item"]'))
			.filter(e => e.offsetParent !== null);
		const norm = s => (s || '').replace(/[\s​]/g, '');
		const target = norm('#' + w);
		const hit = items.find(e => norm(e.innerText).includes(target));
		if (!hit) return false;
		hit.click();
		return true;
	}`, word)
	if err != nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

// SetCover uploads a dedicated cover for a Douyin video. We target the 竖封面
// (3:4) slot — it is the thumbnail 抖音 shows in-feed and matches a 3:4/portrait
// cover with no crop loss (a landscape video's 横封面 would crop a 3:4 cover's
// title/CTA). The flow, verified against the live UI: (1) click the 竖封面
// control's 选择封面 to arm its image input (no native dialog), (2) push the
// image onto that input via CDP — which opens the 设置封面 crop dialog, (3) click
// 保存/完成 on each cover modal until none remain. Best-effort: any failure
// leaves Douyin's default video frame in place.
func (douyinSite) SetCover(p *Publisher, path string) error {
	// 竖封面 (3:4) — the in-feed thumbnail; a 3:4 cover fills it with no crop loss.
	// This is the one that matters, so its failure is fatal to SetCover.
	if err := douyinSetOneCover(p, "竖封面", path); err != nil {
		return err
	}
	// 横封面 (4:3) — sets the second cover so Douyin stops flagging "横/竖双封面
	// 缺失" (优化建议). A 3:4 cover center-crops here (loses top/bottom text) but
	// still fills the slot; best-effort since 竖封面 is already set.
	if err := douyinSetOneCover(p, "横封面", path); err != nil {
		log.Printf("note: 横封面 (secondary) not set (%v); 竖封面 is set", err)
	}
	return nil
}

// douyinSetOneCover sets one cover slot (slotKeyword is "竖封面" or "横封面"): it
// arms that slot's 选择封面 control (no native dialog), pushes the image onto the
// cover image input via CDP — which opens the 设置封面 crop dialog — then clicks
// 保存/完成 on each cover modal until none remain.
func douyinSetOneCover(p *Publisher, slotKeyword, path string) error {
	armed, _ := p.page.Evaluate(`(kw) => {
		const ctrls = Array.from(document.querySelectorAll('[class*="coverControl"]'));
		const v = ctrls.find(c => (c.innerText || '').includes(kw)) || ctrls[0];
		if (!v) return false;
		(v.querySelector('[class*="cover-Jg3"]') || v).click();
		return true;
	}`, slotKeyword)
	if armed != true {
		return fmt.Errorf("%s control (选择封面) not found", slotKeyword)
	}
	p.page.WaitForTimeout(1200)

	if err := p.cdpSetCoverFile(path, ".semi-modal"); err != nil {
		return err
	}

	// Confirm each cover modal: the crop dialog's 保存, then the editor's 完成.
	// Loop because the flow is staged (crop → editor) and the editor mounts a
	// beat after 保存.
	confirmJS := `(() => {
		const labels = ['保存','完成','确定','确认'];
		const btns = Array.from(document.querySelectorAll('.semi-modal button, [class*="modal"] button'))
			.filter(b => b.offsetParent !== null && labels.includes((b.innerText||'').trim()) && !b.disabled);
		if (!btns.length) return 'none';
		btns.sort((a,b) => labels.indexOf(a.innerText.trim()) - labels.indexOf(b.innerText.trim()));
		const t = btns[0].innerText.trim(); btns[0].click(); return t;
	})()`
	confirmed := false
	for i := 0; i < 6; i++ {
		p.page.WaitForTimeout(1500)
		v, _ := p.page.Evaluate(confirmJS)
		if s, _ := v.(string); s == "none" {
			if confirmed {
				break
			}
			continue // crop editor may still be mounting on the first iterations
		} else {
			log.Printf("cover[%s]: clicked %q", slotKeyword, s)
			confirmed = true
		}
	}
	if !confirmed {
		return fmt.Errorf("no cover confirm button (保存/完成) appeared for %s", slotKeyword)
	}
	p.page.WaitForTimeout(1500)
	return nil
}

// ClickPublish waits for the 发布 button to enable (video upload/processing)
// then clicks it like a person. Douyin's 发布 is a Semi button whose label is
// plain text, so we locate it by exact text and read its disabled state.
func (douyinSite) ClickPublish(p *Publisher) error {
	// Douyin's HD ("高清发布") server-side processing can run well past 6 min on a
	// fresh upload before 发布 enables, so wait generously.
	box, err := douyinWaitPublishEnabled(p, 15*time.Minute)
	if err != nil {
		return err
	}
	cx := box.X + box.Width*0.5 + p.jitter(box.Width*0.12)
	cy := box.Y + box.Height*0.5 + p.jitter(box.Height*0.2)
	p.readPage()       // skim the finished post before committing
	p.pause(500, 1400) // a beat of hesitation before committing
	if err := p.humanClickXY(cx, cy); err != nil {
		return fmt.Errorf("click 发布: %w", err)
	}
	// A successful publish navigates to the content manage page.
	p.page.WaitForTimeout(5000)
	log.Printf("publish clicked; current URL: %s", p.page.URL())
	return nil
}

// douyinWaitPublishEnabled polls for the 发布 button until it exists and is no
// longer disabled, returning its viewport bounding box for a positional click.
func douyinWaitPublishEnabled(p *Publisher, timeout time.Duration) (*playwright.Rect, error) {
	const step = 2 * time.Second
	// Returns {x,y,w,h,disabled} for the 发布 button, or null if absent.
	probe := `(() => {
		const btns = Array.from(document.querySelectorAll('button, [role="button"]'));
		const b = btns.find(x => (x.innerText||'').trim() === '发布');
		if (!b) return null;
		const dis = b.disabled || b.getAttribute('aria-disabled') === 'true'
			|| (b.className||'').toString().includes('disabled');
		const r = b.getBoundingClientRect();
		return { x: r.x, y: r.y, w: r.width, h: r.height, disabled: !!dis };
	})()`
	for waited := time.Duration(0); waited < timeout; waited += step {
		v, err := p.page.Evaluate(probe)
		if err != nil {
			log.Printf("note: polling 发布 button: %v", err)
		}
		if m, ok := v.(map[string]interface{}); ok {
			disabled, _ := m["disabled"].(bool)
			// playwright-go decodes whole-number JS values as int, not float64, so
			// a naive m["w"].(float64) assertion fails for an integral width and
			// reads 0 — which would make the button look perpetually "not ready".
			// jsFloat handles both int and float64.
			if w := jsFloat(m["w"]); !disabled && w > 0 {
				return &playwright.Rect{
					X:      jsFloat(m["x"]),
					Y:      jsFloat(m["y"]),
					Width:  jsFloat(m["w"]),
					Height: jsFloat(m["h"]),
				}, nil
			}
		}
		log.Printf("waiting for 发布 to enable (video processing)... %s", waited)
		p.page.WaitForTimeout(float64(step.Milliseconds()))
	}
	return nil, fmt.Errorf("发布 still disabled/absent after %s — video may still be processing", timeout)
}

// jsFloat coerces a value decoded from page.Evaluate (which yields int for
// whole numbers and float64 otherwise) to float64.
func jsFloat(v interface{}) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	case int64:
		return float64(n)
	}
	return 0
}

// douyinTitleLocator matches the 作品标题 input by placeholder, falling back to
// any non-file text input on the form.
func douyinTitleLocator(p *Publisher) playwright.Locator {
	return p.page.Locator(`input[placeholder*="标题"], input[type="text"]:not([type="file"])`).First()
}

// douyinDescLocator matches the 作品简介 rich-text editor. Douyin renders it as a
// contenteditable inside an editor-kit/zone container.
func douyinDescLocator(p *Publisher) playwright.Locator {
	return p.page.Locator(`.editor-kit-container [contenteditable="true"], .zone-container[contenteditable="true"], div[contenteditable="true"]`).First()
}
