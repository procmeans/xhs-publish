// Command xhsdebug attaches to the running Chrome and dumps publish-button
// candidates on the CURRENT page (no navigation), to find the right selector.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"github.com/playwright-community/playwright-go"
)

func main() {
	cdp := flag.String("cdp", "http://localhost:9222", "CDP endpoint")
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
	fmt.Printf("contexts=%d pages=%d\n", len(browser.Contexts()), len(pages))
	for i, p := range pages {
		fmt.Printf("  page[%d] url=%s\n", i, p.URL())
	}

	// Pick the publish page.
	var page playwright.Page
	for _, p := range pages {
		if u := p.URL(); contains(u, "publish") {
			page = p
		}
	}
	if page == nil && len(pages) > 0 {
		page = pages[0]
	}
	if page == nil {
		log.Fatal("no page")
	}
	fmt.Printf("inspecting: %s\n\n", page.URL())

	// Dump every button / role=button with its text, visibility, enabled state.
	res, err := page.Evaluate(`() => {
		const out = [];
		const els = document.querySelectorAll('button, [role="button"], .btn, [class*="publish"], [class*="submit"]');
		for (const el of els) {
			const r = el.getBoundingClientRect();
			const txt = (el.innerText || el.textContent || '').trim().slice(0, 30);
			if (!txt && r.width === 0) continue;
			out.push({
				tag: el.tagName.toLowerCase(),
				cls: (el.className || '').toString().slice(0, 80),
				text: txt,
				disabled: el.disabled === true || el.getAttribute('aria-disabled') === 'true',
				visible: r.width > 0 && r.height > 0,
				x: Math.round(r.x), y: Math.round(r.y),
			});
		}
		return out;
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
