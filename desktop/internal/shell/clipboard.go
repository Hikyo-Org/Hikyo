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
	// armed holds one pending clear per connection, so arming one connection
	// never cancels another's.
	armed map[string]*pendingClear
}

type pendingClear struct {
	count int64
	stop  func()
}

func newClipboardGuard(native Native, now func() time.Time) *clipboardGuard {
	g := &clipboardGuard{native: native, now: now, armed: map[string]*pendingClear{}}
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
	count, active, base := g.native.PasteboardCount(), g.native.AppActive(), g.native.PasteboardCountAtActivation()
	g.mu.Lock()
	defer g.mu.Unlock()
	if count != g.lastCount {
		g.lastCount = count
		// A change made while another app was frontmost is not the page's
		// copy and must never be cleared (ADR D12). The count at activation
		// closes the poll gap: a copy made elsewhere just before switching
		// back is at or below it, whenever this poll runs.
		if active && count != base {
			g.lastChange = g.now()
		}
	}
}

// arm is called after a connection's page copies a secret. It schedules a
// clear only if a pasteboard change was observed, and clears only if the count
// is unchanged when the timer fires.
func (g *clipboardGuard) arm(owner string) {
	if !g.native.AppActive() {
		return
	}
	count, base := g.native.PasteboardCount(), g.native.PasteboardCountAtActivation()
	if count == base {
		return // nothing changed since the app came to the front
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if count == g.lastCount && (g.lastChange.IsZero() || g.now().Sub(g.lastChange) > clipboardRecentWithin) {
		return
	}
	// lastChange is not refreshed here: a page that arms repeatedly must not
	// keep the "recent" window open for a copy made elsewhere.
	g.lastCount = count
	if prev := g.armed[owner]; prev != nil {
		prev.stop()
	}
	p := &pendingClear{count: count}
	g.armed[owner] = p
	p.stop = g.after(clipboardClearAfter, func() { g.fire(owner, p) })
}

func (g *clipboardGuard) fire(owner string, p *pendingClear) {
	g.mu.Lock()
	current := g.armed[owner] == p
	if current {
		delete(g.armed, owner)
	}
	g.mu.Unlock()
	if current {
		// The count is checked where the pasteboard is cleared, so a copy made
		// after this point is never cleared.
		g.native.ClearPasteboardIf(p.count)
	}
}
