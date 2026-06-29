// Command xhsdebug attaches to the running Chrome and inspects the CURRENT
// publish page (no navigation). Default: dump file inputs + cover-related
// elements + publish-button candidates. With -js, evaluate arbitrary JS and
// print the JSON result — a live-page REPL for finding selectors / driving the
// cover-upload flow.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/playwright-community/playwright-go"
)

func main() {
	cdp := flag.String("cdp", "http://localhost:9222", "CDP endpoint")
	js := flag.String("js", "", "arbitrary JS expression to evaluate on the publish page")
	shot := flag.String("shot", "", "screenshot the page to this path and exit")
	coverfile := flag.String("coverfile", "", "CDP DOM.setFileInputFiles the cover image onto the modal's image input")
	flag.Parse()

	pw, err := playwright.Run()
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	browser, err := pw.Chromium.ConnectOverCDP(*cdp)
	if err != nil {
		log.Fatal(err)
	}
	defer browser.Close()

	ctx := browser.Contexts()[0]
	pages := ctx.Pages()
	var page playwright.Page
	// Prefer a compose/upload tab on either platform (小红书 publish, 抖音 upload).
	// When several match, prefer one whose title field is already filled (the
	// form the publisher actually drove) over an empty upload landing page.
	for _, p := range pages {
		u := p.URL()
		if !(contains(u, "publish") || contains(u, "upload") || contains(u, "creator.douyin")) {
			continue
		}
		page = p
		v, _ := p.Evaluate(`(() => { const e = document.querySelector('input[placeholder*="标题"], input[type=text]'); return e && (e.value||'').trim().length > 0; })()`)
		if filled, _ := v.(bool); filled {
			break
		}
	}
	if page == nil && len(pages) > 0 {
		page = pages[0]
	}
	if page == nil {
		log.Fatal("no page")
	}
	_ = page.BringToFront()
	fmt.Printf("inspecting: %s\n\n", page.URL())

	if *shot != "" {
		sess, err := ctx.NewCDPSession(page)
		if err != nil {
			log.Fatal(err)
		}
		res, err := sess.Send("Page.captureScreenshot", map[string]interface{}{"format": "png", "fromSurface": false, "captureBeyondViewport": false})
		if err != nil {
			log.Fatal(err)
		}
		data, _ := res.(map[string]interface{})["data"].(string)
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(*shot, raw, 0644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("screenshot saved: %s (%d bytes)\n", *shot, len(raw))
		return
	}

	if *coverfile != "" {
		sess, err := ctx.NewCDPSession(page)
		if err != nil {
			log.Fatal(err)
		}
		sess.Send("DOM.enable", nil)
		res, err := sess.Send("Runtime.evaluate", map[string]interface{}{
			"expression": `(() => { const m = document.querySelector('.cover-modal input[type=file][accept*="image"]'); if (m) return m; const xs = Array.from(document.querySelectorAll('input[type=file]')); return xs.find(e => ((e.getAttribute('accept')||'').toLowerCase().includes('image'))) || null; })()`,
		})
		if err != nil {
			log.Fatal(err)
		}
		oid := res.(map[string]interface{})["result"].(map[string]interface{})["objectId"]
		if oid == nil {
			log.Fatal("no image input found (open the cover editor first)")
		}
		if _, err := sess.Send("DOM.setFileInputFiles", map[string]interface{}{"files": []string{*coverfile}, "objectId": oid}); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("cover file set on image input: %s\n", *coverfile)
		return
	}

	if *js != "" {
		res, err := page.Evaluate(*js)
		if err != nil {
			log.Fatal(err)
		}
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
		return
	}

	// Default dump: file inputs + cover-related elements.
	res, err := page.Evaluate(`() => {
		const rect = el => { const r = el.getBoundingClientRect(); return {x:Math.round(r.x),y:Math.round(r.y),w:Math.round(r.width),h:Math.round(r.height)}; };
		const fileInputs = Array.from(document.querySelectorAll('input[type=file]')).map(el => ({
			accept: el.getAttribute('accept') || '',
			cls: (el.className||'').toString().slice(0,80),
			rect: rect(el),
		}));
		const KW = ['封面','编辑','上传','裁剪','设置','确定','完成','选择','本地'];
		const coverEls = [];
		for (const el of document.querySelectorAll('div,span,button,[role="button"],a,p')) {
			const t = (el.innerText||el.textContent||'').trim();
			if (!t || t.length > 12) continue;
			if (KW.some(k => t.includes(k))) {
				const r = el.getBoundingClientRect();
				if (r.width === 0 && r.height === 0) continue;
				coverEls.push({ tag: el.tagName.toLowerCase(), text: t, cls:(el.className||'').toString().slice(0,70), rect: rect(el) });
			}
		}
		const seen = new Set();
		const cov = coverEls.filter(e => { const k = e.text+e.rect.x+e.rect.y; if(seen.has(k))return false; seen.add(k); return true; });
		// Title inputs (by placeholder) — find the 标题 field on either platform.
		const titleInputs = Array.from(document.querySelectorAll('input')).filter(el => {
			const ph = el.getAttribute('placeholder') || '';
			return el.type !== 'file' && (ph.includes('标题') || el.type === 'text');
		}).map(el => ({ placeholder: el.getAttribute('placeholder')||'', cls:(el.className||'').toString().slice(0,70), rect: rect(el) }));
		// Publish-button candidates (发布 / 存草稿 / 暂存) across buttons & web components.
		const PB = ['发布','存草稿','暂存离开','暂存','下一步','完成'];
		const publishButtons = [];
		for (const el of document.querySelectorAll('button,[role="button"],xhs-publish-btn')) {
			const t = (el.innerText||el.textContent||'').trim();
			const r = el.getBoundingClientRect();
			if (r.width === 0 && r.height === 0) continue;
			if (el.tagName.toLowerCase() === 'xhs-publish-btn' || PB.some(k => t === k)) {
				publishButtons.push({ tag: el.tagName.toLowerCase(), text: t.slice(0,12), disabled: el.disabled || el.getAttribute('aria-disabled')==='true' || el.getAttribute('submit-disabled'), cls:(el.className||'').toString().slice(0,60), rect: rect(el) });
			}
		}
		return { fileInputs, coverEls: cov, titleInputs, publishButtons };
	}`)
	if err != nil {
		log.Fatal(err)
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(b))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
