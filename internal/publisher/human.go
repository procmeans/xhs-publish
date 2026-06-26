package publisher

import (
	"math"
	"strings"

	"github.com/playwright-community/playwright-go"
)

// human.go adds human-like interaction noise so the automation doesn't look
// like a metronome: variable "thinking" pauses, curved mouse paths with hover,
// variable-speed typing, occasional typos that get corrected, and the odd
// retype. All of this runs against the user's REAL, visible Chrome (attached
// over CDP) — never a headless browser.

// typoPool are plausible mis-types inserted then immediately corrected.
var typoPool = []rune("的了是在和也都就这那有还要么呢吧啊吗一不我你它")

// pause sleeps a random duration in [minMs, maxMs] (a "thinking" beat).
func (p *Publisher) pause(minMs, maxMs int) {
	if !p.human {
		return
	}
	d := minMs
	if maxMs > minMs {
		d += p.rng.Intn(maxMs - minMs)
	}
	p.page.WaitForTimeout(float64(d))
}

// jitter returns a random offset in [-r, r].
func (p *Publisher) jitter(r float64) float64 {
	return (p.rng.Float64()*2 - 1) * r
}

// humanMoveTo glides the cursor from its last position to (tx,ty) along a
// quadratic Bézier curve with small per-step jitter and micro-pauses, instead
// of teleporting. Tracks the cursor so successive moves chain naturally.
func (p *Publisher) humanMoveTo(tx, ty float64) {
	if !p.human {
		p.page.Mouse().Move(tx, ty)
		p.lastX, p.lastY = tx, ty
		return
	}
	sx, sy := p.lastX, p.lastY
	dist := math.Hypot(tx-sx, ty-sy)

	// Control point offset perpendicular-ish to the path for a natural arc.
	curve := math.Min(120, dist*0.25)
	mx := (sx+tx)/2 + p.jitter(curve)
	my := (sy+ty)/2 + p.jitter(curve)

	steps := 14 + p.rng.Intn(14) // 14..27
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		// ease-in-out so it accelerates then settles
		te := t * t * (3 - 2*t)
		x := (1-te)*(1-te)*sx + 2*(1-te)*te*mx + te*te*tx
		y := (1-te)*(1-te)*sy + 2*(1-te)*te*my + te*te*ty
		p.page.Mouse().Move(x+p.jitter(1.5), y+p.jitter(1.5))
		if p.rng.Float64() < 0.18 {
			p.page.WaitForTimeout(float64(8 + p.rng.Intn(22)))
		}
	}
	p.page.Mouse().Move(tx, ty)
	p.lastX, p.lastY = tx, ty
}

// humanClickLocator hovers over a located element (with a settle pause) and
// clicks a slightly off-center point inside it, like a person would.
func (p *Publisher) humanClickLocator(loc playwright.Locator) error {
	if err := loc.ScrollIntoViewIfNeeded(); err != nil {
		// non-fatal; continue and let the click attempt surface real errors
		_ = err
	}
	box, err := loc.BoundingBox()
	if err != nil || box == nil {
		// fall back to a plain click if we can't measure it
		return loc.Click()
	}
	// aim near the center, with a little human imprecision
	tx := box.X + box.Width*0.5 + p.jitter(box.Width*0.18)
	ty := box.Y + box.Height*0.5 + p.jitter(box.Height*0.22)
	p.humanMoveTo(tx, ty)
	p.pause(120, 380) // hover/settle before pressing
	return p.page.Mouse().Click(tx, ty)
}

// humanClickXY hovers to (x,y) and clicks, with a settle pause.
func (p *Publisher) humanClickXY(x, y float64) error {
	p.humanMoveTo(x, y)
	p.pause(140, 420)
	return p.page.Mouse().Click(x, y)
}

// humanType focuses loc and types text rune-by-rune at a variable cadence,
// occasionally fumbling a character and backspacing to fix it, and pausing a
// touch longer at sentence boundaries — the rhythm of a person at a keyboard.
func (p *Publisher) humanType(loc playwright.Locator, text string) error {
	if err := p.humanClickLocator(loc); err != nil {
		return err
	}
	p.pause(200, 600)

	if !p.human {
		return loc.PressSequentially(text, playwright.LocatorPressSequentiallyOptions{
			Delay: playwright.Float(20),
		})
	}

	kb := p.page.Keyboard()
	for _, r := range text {
		// Occasional typo on a "real" character: type a wrong one, notice, fix.
		if !isBoundary(r) && p.rng.Float64() < 0.05 {
			wrong := typoPool[p.rng.Intn(len(typoPool))]
			if err := kb.InsertText(string(wrong)); err != nil {
				return err
			}
			p.page.WaitForTimeout(float64(140 + p.rng.Intn(220))) // "wait, that's wrong"
			if err := kb.Press("Backspace"); err != nil {
				return err
			}
			p.page.WaitForTimeout(float64(90 + p.rng.Intn(140)))
		}

		if err := kb.InsertText(string(r)); err != nil {
			return err
		}

		// Variable inter-key delay; longer think-pause after punctuation.
		switch {
		case isBoundary(r):
			p.page.WaitForTimeout(float64(260 + p.rng.Intn(520)))
		case p.rng.Float64() < 0.08:
			p.page.WaitForTimeout(float64(300 + p.rng.Intn(700))) // mid-thought drift
		default:
			p.page.WaitForTimeout(float64(45 + p.rng.Intn(120)))
		}
	}
	return nil
}

// humanTypeWithRetype types text, but for short strings (e.g. a title) there's
// a chance the "writer" second-guesses it: selects all, clears, and retypes.
func (p *Publisher) humanTypeWithRetype(loc playwright.Locator, text string) error {
	if err := p.humanType(loc, text); err != nil {
		return err
	}
	if !p.human || p.rng.Float64() >= 0.35 {
		return nil
	}
	// reconsider the title
	p.pause(500, 1200)
	if err := p.page.Keyboard().Press("ControlOrMeta+a"); err != nil {
		return nil // best-effort; leave the first version if select fails
	}
	p.pause(150, 400)
	if err := p.page.Keyboard().Press("Backspace"); err != nil {
		return nil
	}
	p.pause(300, 800)
	return p.humanType(loc, text)
}

func isBoundary(r rune) bool {
	return strings.ContainsRune("。！？，、,.!?；;：:\n ", r)
}
