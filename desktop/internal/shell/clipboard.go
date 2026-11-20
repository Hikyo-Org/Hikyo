package shell

import (
	"sync"
	"time"
)

const (
	clipboardClearAfter   = 45 * time.Second
	clipboardRecentWithin = 5 * time.Second
	clipboardPoll         = 250 * time.Millisecond
)

// clipboardGuard clears a copied secret later, but only if the pasteboard has
// not changed since (ADR D12). WebKit refuses the clipboard read the SPA
// relies on, so the shell watches the pasteboard's change count itself.
type clipboardGuard struct {
	native Native
	now    func() time.Time
	after  func(time.Duration, func()) (stop func())

	mu         sync.Mutex
	lastCount  int64
	lastChange time.Time
	armedCount int64
	stopArmed  func()
}

func newClipboardGuard(native Native, now func() time.Time) *clipboardGuard {
	g := &clipboardGuard{native: native, now: now}
	g.after = func(d time.Duration, f func()) func() {
		t := time.AfterFunc(d, f)
		return func() { t.Stop() }
	}
	g.lastCount = native.PasteboardCount()
	return g
}

// observe records the pasteboard's change count. The shell calls it on a short
// timer while it runs; arm compares against what it saw.
func (g *clipboardGuard) observe() {
	count := g.native.PasteboardCount()
	g.mu.Lock()
	defer g.mu.Unlock()
	if count != g.lastCount {
		g.lastCount, g.lastChange = count, g.now()
	}
}

// arm is called after the page copies a secret. It schedules a clear only if
// a pasteboard change was observed, and clears only if the count is unchanged
// when the timer fires.
func (g *clipboardGuard) arm() {
	count := g.native.PasteboardCount()
	g.mu.Lock()
	defer g.mu.Unlock()
	if count == g.lastCount && (g.lastChange.IsZero() || g.now().Sub(g.lastChange) > clipboardRecentWithin) {
		return
	}
	g.lastCount, g.lastChange = count, g.now()
	if g.stopArmed != nil {
		g.stopArmed()
	}
	g.armedCount = count
	g.stopArmed = g.after(clipboardClearAfter, g.fire)
}

func (g *clipboardGuard) fire() {
	count := g.native.PasteboardCount()
	g.mu.Lock()
	armed := g.armedCount
	g.armedCount, g.stopArmed = 0, nil
	g.mu.Unlock()
	if count == armed {
		g.native.ClearPasteboard()
	}
}
