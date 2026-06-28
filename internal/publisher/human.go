package publisher

import (
	"log"
	"math"
	"strings"

	"github.com/playwright-community/playwright-go"
)

// human.go adds human-like interaction noise so the automation doesn't look
// like a metronome. It models the things real-user-behavior detectors actually
// look at:
//
//   - mouse paths that curve, ease, overshoot-and-correct, and drift while idle
//   - clicks with a real mousedown→mouseup dwell (and the odd micro-slip)
//   - typing that fires genuine key events for latin text (not just `input`),
//     types Chinese over InsertText, varies cadence in autocorrelated bursts,
//     and fumbles characters with plausible (adjacent-key / double / delayed)
//     typos that get corrected
//   - inertial wheel scrolling and a "read the page" glance before committing
//   - a stealth pass that neutralises the obvious navigator.webdriver tell
//
// All of this runs against the user's REAL, visible Chrome (attached over CDP)
// — never a headless browser — which is itself the single biggest anti-detection
// win: the fingerprint, history, and TLS stack are all genuine.

// typoPool are plausible Chinese mis-types, used when the intended character is
// itself CJK (no physical-key adjacency to model).
var typoPool = []rune("的了是在和也都就这那有还要么呢吧啊吗一不我你它")

// qwertyNeighbors maps a lowercase key to the keys physically adjacent to it, so
// a latin typo lands where a real finger would slip.
var qwertyNeighbors = map[rune]string{
	'q': "wa", 'w': "qeas", 'e': "wrds", 'r': "etfd", 't': "rygf",
	'y': "tuhg", 'u': "yijh", 'i': "uokj", 'o': "iplk", 'p': "ol",
	'a': "qwsz", 's': "awedxz", 'd': "serfcx", 'f': "drtgvc", 'g': "ftyhbv",
	'h': "gyujnb", 'j': "huikmn", 'k': "jiolm", 'l': "kop",
	'z': "asx", 'x': "zsdc", 'c': "xdfv", 'v': "cfgb", 'b': "vghn",
	'n': "bhjm", 'm': "njk",
	'0': "9", '1': "2", '2': "13", '3': "24", '4': "35",
	'5': "46", '6': "57", '7': "68", '8': "79", '9': "80",
}

// randMs returns a random duration in [minMs, maxMs] as a float for Playwright's
// millisecond-based timeouts. It is the single place inter-action timing noise
// is generated.
func (p *Publisher) randMs(minMs, maxMs int) float64 {
	if maxMs <= minMs {
		return float64(minMs)
	}
	return float64(minMs + p.rng.Intn(maxMs-minMs))
}

// sleep waits a random [minMs, maxMs] beat unconditionally. Callers that must
// honour the humanization toggle should use pause instead.
func (p *Publisher) sleep(minMs, maxMs int) {
	p.page.WaitForTimeout(p.randMs(minMs, maxMs))
}

// pause sleeps a random duration in [minMs, maxMs] (a "thinking" beat), but only
// when humanization is enabled. For longer beats it occasionally lets the idle
// hand drift a pixel or two rather than holding the cursor perfectly still — a
// dead-still cursor is itself a tell.
func (p *Publisher) pause(minMs, maxMs int) {
	if !p.human {
		return
	}
	// Caution stretches (or compresses) every deliberate pause.
	minMs = int(float64(minMs) * p.profile.Caution)
	maxMs = int(float64(maxMs) * p.profile.Caution)
	if maxMs >= 500 && p.rng.Float64() < 0.5 {
		half := p.randMs(minMs, maxMs) * 0.5
		p.page.WaitForTimeout(half)
		nx, ny := p.lastX+p.jitter(2), p.lastY+p.jitter(2)
		p.page.Mouse().Move(nx, ny)
		p.lastX, p.lastY = nx, ny
		p.page.WaitForTimeout(half)
		return
	}
	p.sleep(minMs, maxMs)
}

// jitter returns a random offset in [-r, r].
func (p *Publisher) jitter(r float64) float64 {
	return (p.rng.Float64()*2 - 1) * r
}

// glide moves the cursor from its last position to (tx,ty) along a quadratic
// Bézier curve with eased speed, small per-step jitter, and micro-pauses. It
// assumes humanization is on (callers gate that) and tracks the cursor so
// successive moves chain naturally.
func (p *Publisher) glide(tx, ty float64) {
	sx, sy := p.lastX, p.lastY
	dist := math.Hypot(tx-sx, ty-sy)

	// Control point offset perpendicular-ish to the path for a natural arc.
	curve := math.Min(120, dist*0.25)
	mx := (sx+tx)/2 + p.jitter(curve)
	my := (sy+ty)/2 + p.jitter(curve)

	// Scale the number of steps with distance: a short nudge shouldn't crawl
	// through dozens of micro-moves, while a long sweep needs more to stay
	// smooth. Roughly one step per 12px, clamped, plus a little randomness.
	steps := int(dist / 12)
	switch {
	case steps < 8:
		steps = 8
	case steps > 36:
		steps = 36
	}
	steps += p.rng.Intn(6)

	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		// ease-in-out so it accelerates then settles
		te := t * t * (3 - 2*t)
		x := (1-te)*(1-te)*sx + 2*(1-te)*te*mx + te*te*tx
		y := (1-te)*(1-te)*sy + 2*(1-te)*te*my + te*te*ty
		p.page.Mouse().Move(x+p.jitter(1.5), y+p.jitter(1.5))
		if p.rng.Float64() < 0.18 {
			p.sleep(8, 30)
		}
	}
	// Land exactly on target (the last jittered step may be a hair off).
	p.page.Mouse().Move(tx, ty)
	p.lastX, p.lastY = tx, ty
}

// humanMoveTo glides the cursor to (tx,ty). For longer moves it sometimes
// overshoots the target and corrects back, the way a real hand does under
// Fitts's law instead of decelerating perfectly onto the pixel.
func (p *Publisher) humanMoveTo(tx, ty float64) {
	if !p.human {
		p.page.Mouse().Move(tx, ty)
		p.lastX, p.lastY = tx, ty
		return
	}
	dist := math.Hypot(tx-p.lastX, ty-p.lastY)
	if dist > 140 && p.rng.Float64() < 0.45 {
		// shoot a little past the target along the direction of travel...
		ux, uy := (tx-p.lastX)/dist, (ty-p.lastY)/dist
		over := 6 + p.rng.Float64()*16
		p.glide(tx+ux*over+p.jitter(4), ty+uy*over+p.jitter(4))
		p.sleep(40, 130) // "missed it"
		// ...then settle back onto it.
		p.glide(tx, ty)
		return
	}
	p.glide(tx, ty)
}

// glanceAround occasionally drifts the cursor to a nearby spot and pauses, as if
// the user's eye (and hand) wandered over the page before reaching for a target.
func (p *Publisher) glanceAround() {
	if !p.human || p.rng.Float64() >= 0.35 {
		return
	}
	nx, ny := p.lastX+p.jitter(220), p.lastY+p.jitter(140)
	if nx < 8 {
		nx = 8
	}
	if ny < 8 {
		ny = 8
	}
	p.glide(nx, ny)
	p.sleep(160, 520)
}

// humanClick presses at (x,y) with a realistic mousedown→mouseup dwell, and the
// occasional sub-pixel slip under the finger.
func (p *Publisher) humanClick(x, y float64) error {
	if !p.human {
		return p.page.Mouse().Click(x, y)
	}
	m := p.page.Mouse()
	if err := m.Down(); err != nil {
		return err
	}
	p.sleep(60, 150) // hold the button down like a person, not a relay
	if p.rng.Float64() < 0.3 {
		m.Move(x+p.jitter(1), y+p.jitter(1))
	}
	if err := m.Up(); err != nil {
		return err
	}
	p.lastX, p.lastY = x, y
	return nil
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
	p.glanceAround()
	// aim near the center, with a little human imprecision
	tx := box.X + box.Width*0.5 + p.jitter(box.Width*0.18)
	ty := box.Y + box.Height*0.5 + p.jitter(box.Height*0.22)
	p.humanMoveTo(tx, ty)
	p.pause(120, 380) // hover/settle before pressing
	return p.humanClick(tx, ty)
}

// humanClickXY hovers to (x,y) and clicks, with a settle pause.
func (p *Publisher) humanClickXY(x, y float64) error {
	p.glanceAround()
	p.humanMoveTo(x, y)
	p.pause(140, 420)
	return p.humanClick(x, y)
}

// isTypeable reports whether a rune can be sent as a genuine physical keypress
// (printable ASCII). Such characters go through Keyboard.Press so the page sees
// keydown/keypress/keyup, not just an `input` event. Everything else (CJK,
// emoji, newlines) goes through InsertText.
func isTypeable(r rune) bool {
	return r >= 0x20 && r < 0x7f
}

// wrongFor returns a plausible mis-typed character for r: an adjacent key for
// latin/digits, a random Chinese character for CJK, and a duplicate of the
// character itself as a fallback (the classic "typed it twice" slip).
func (p *Publisher) wrongFor(r rune) rune {
	if isTypeable(r) {
		lower := r
		if r >= 'A' && r <= 'Z' {
			lower = r + 32
		}
		if n, ok := qwertyNeighbors[lower]; ok && len(n) > 0 && p.rng.Float64() < 0.85 {
			w := rune(n[p.rng.Intn(len(n))])
			if r >= 'A' && r <= 'Z' && w >= 'a' && w <= 'z' {
				w -= 32 // keep the slip in the same case
			}
			return w
		}
		return r // duplicate
	}
	if r > 0x2e7f { // CJK & friends
		return typoPool[p.rng.Intn(len(typoPool))]
	}
	return r
}

// typingScale returns the timing multiplier for the i-th of n characters: the
// inverse of SpeedFactor, optionally shaped by a warm-up/fatigue curve. A real
// typist starts a touch slow, settles into a groove, then drifts slower over a
// long passage. Short passages (≤20 chars) skip the curve.
func (p *Publisher) typingScale(i, n int) float64 {
	m := 1.0 / p.profile.SpeedFactor
	if !p.profile.Fatigue || n <= 20 {
		return m
	}
	f := float64(i) / float64(n)
	switch {
	case f < 0.1:
		m *= 1.15 - 1.5*f // warm-up: 1.15 -> 1.0
	case f > 0.6:
		mag := math.Min(1, float64(n)/200) // longer text fatigues more
		m *= 1 + (f-0.6)*0.6*mag           // up to ~1.24 by the end
	}
	return m
}

// humanType focuses loc and types text at a human cadence: real key events for
// latin, InsertText for Chinese, autocorrelated inter-key delays (bursts and
// stalls rather than a fixed beat), longer think-pauses at sentence boundaries,
// and occasional typos — some fixed immediately, some only noticed a character
// or two later.
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
	var ferr error
	emit := func(r rune) {
		if ferr != nil {
			return
		}
		if isTypeable(r) {
			ferr = kb.Press(string(r), playwright.KeyboardPressOptions{
				Delay: playwright.Float(p.randMs(35, 110) / p.profile.SpeedFactor), // keydown→keyup hold
			})
		} else {
			ferr = kb.InsertText(string(r))
		}
	}
	press := func(key string) {
		if ferr != nil {
			return
		}
		ferr = kb.Press(key)
	}

	runes := []rune(text)
	lastDelay := p.randMs(60, 120)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if !isBoundary(r) && p.rng.Float64() < p.profile.TypoRate {
			wrong := p.wrongFor(r)
			if i+1 < len(runes) && p.rng.Float64() < 0.30 {
				// Delayed correction: fumble, carry on a char or two, then
				// notice and backspace all the way back.
				emit(wrong)
				p.page.WaitForTimeout(p.randMs(70, 180))
				k := 1
				if i+2 < len(runes) && p.rng.Float64() < 0.45 {
					k = 2
				}
				for j := 1; j <= k; j++ {
					emit(runes[i+j])
					p.page.WaitForTimeout(p.randMs(70, 190))
				}
				p.page.WaitForTimeout(p.randMs(180, 460)) // "wait, that's wrong"
				for b := 0; b < k+1; b++ {
					press("Backspace")
					p.page.WaitForTimeout(p.randMs(70, 150))
				}
				p.page.WaitForTimeout(p.randMs(90, 220))
				// fall through: the real runes[i] is retyped below, and the
				// loop then naturally retypes runes[i+1..].
			} else {
				// Immediate correction.
				emit(wrong)
				p.page.WaitForTimeout(p.randMs(140, 360))
				press("Backspace")
				p.page.WaitForTimeout(p.randMs(90, 230))
			}
		}
		if ferr != nil {
			return ferr
		}

		emit(r)
		if ferr != nil {
			return ferr
		}

		// Inter-key delay, scaled by speed and the fatigue curve. Boundaries and
		// the odd mid-thought drift get a long pause; ordinary keys are
		// autocorrelated with the previous delay so the rhythm comes in bursts
		// and stalls rather than independent noise.
		scale := p.typingScale(i, len(runes))
		switch {
		case isBoundary(r):
			lastDelay = p.randMs(45, 95)
			p.page.WaitForTimeout(p.randMs(260, 780) * scale)
		case p.rng.Float64() < 0.08:
			lastDelay = p.randMs(45, 95)
			p.page.WaitForTimeout(p.randMs(300, 1000) * scale)
		default:
			target := p.randMs(40, 175)
			lastDelay = 0.6*lastDelay + 0.4*target
			p.page.WaitForTimeout(lastDelay * scale)
		}
	}
	return ferr
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

// humanScroll scrolls the page by dy pixels (positive = down) the way a hand on
// a wheel does: a few decelerating flicks, each broken into sub-deltas, with
// reading pauses between them — not one instantaneous jump.
func (p *Publisher) humanScroll(dy float64) {
	if !p.human {
		_ = p.page.Mouse().Wheel(0, dy)
		return
	}
	remaining := dy
	ticks := 3 + p.rng.Intn(3)
	for i := 0; i < ticks && math.Abs(remaining) > 1; i++ {
		step := remaining * (0.35 + p.rng.Float64()*0.3) // inertia: shrinking flicks
		if i == ticks-1 {
			step = remaining
		}
		subs := 2 + p.rng.Intn(3)
		for s := 0; s < subs; s++ {
			_ = p.page.Mouse().Wheel(p.jitter(2), step/float64(subs))
			p.sleep(16, 42)
		}
		remaining -= step
		p.sleep(120, 380) // glance at what scrolled into view
	}
}

// readPage simulates skimming the form before acting: scroll down a bit, read,
// then scroll back up to where the action is.
func (p *Publisher) readPage() {
	if !p.human {
		return
	}
	down := 220 + float64(p.rng.Intn(360))
	p.humanScroll(down)
	p.pause(500, 1500)
	p.humanScroll(-down)
	p.pause(300, 800)
}

// stealthInit neutralises the most common automation tell (navigator.webdriver)
// for any page that loads after it is installed.
const stealthInit = `try{Object.defineProperty(navigator,'webdriver',{get:()=>false,configurable:true});}catch(e){}`

// HardenStealth installs the stealth patch for future navigations and applies it
// to the page that's already open, then logs the observed webdriver flag so a
// leak is visible. Attaching to a real Chrome profile already gives a genuine
// fingerprint; this just closes the one gap CDP control can open.
func (p *Publisher) HardenStealth() {
	if err := p.page.AddInitScript(playwright.Script{Content: playwright.String(stealthInit)}); err != nil {
		log.Printf("note: add stealth init script: %v", err)
	}
	v, err := p.page.Evaluate(`(() => { ` + stealthInit + ` return navigator.webdriver; })()`)
	if err != nil {
		log.Printf("note: apply stealth to current page: %v", err)
		return
	}
	log.Printf("stealth: navigator.webdriver = %v", v)
}

func isBoundary(r rune) bool {
	return strings.ContainsRune("。！？，、,.!?；;：:\n ", r)
}
