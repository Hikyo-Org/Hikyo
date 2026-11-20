package shell

import (
	"os"
	"sync"
	"time"
)

// downloadLifetime bounds a native download whose WKDownload never reports
// finish or failure. Past it the temp file is removed; a late finish then
// finds nothing registered and writes nothing.
const downloadLifetime = 30 * time.Minute

// PendingDownloads tracks native (WKDownload) downloads: where WebKit writes
// and where the file goes on success. Every entry ends by Take (WebKit
// reported), Drop (its window closed), or the lifetime timer, and in the last
// two the temp file is removed, so no path leaves a partial file behind.
type PendingDownloads struct {
	mu       sync.Mutex
	entries  map[uintptr]*pendingDownload
	lifetime time.Duration
}

type pendingDownload struct {
	host, temp, final string
	replace           bool
	timer             *time.Timer
}

func NewPendingDownloads() *PendingDownloads {
	return &PendingDownloads{entries: map[uintptr]*pendingDownload{}, lifetime: downloadLifetime}
}

// Add registers a download writing to temp for the window bound to host.
// replace is true when the save panel saw a file at final and the person
// approved replacing it.
func (p *PendingDownloads) Add(key uintptr, host, temp, final string, replace bool) {
	d := &pendingDownload{host: host, temp: temp, final: final, replace: replace}
	p.mu.Lock()
	defer p.mu.Unlock()
	d.timer = time.AfterFunc(p.lifetime, func() { p.expire(key, d) })
	p.entries[key] = d
}

// Take removes an entry WebKit reported on and returns its paths.
func (p *PendingDownloads) Take(key uintptr) (temp, final string, replace, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.entries[key]
	if !ok {
		return "", "", false, false
	}
	d.timer.Stop()
	delete(p.entries, key)
	return d.temp, d.final, d.replace, true
}

// Drop discards every download of a closed window and removes its temp file.
func (p *PendingDownloads) Drop(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, d := range p.entries {
		if d.host == host {
			d.timer.Stop()
			delete(p.entries, k)
			_ = os.Remove(d.temp)
		}
	}
}

func (p *PendingDownloads) expire(key uintptr, d *pendingDownload) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries[key] == d {
		delete(p.entries, key)
		_ = os.Remove(d.temp)
	}
}
