package shell

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func partial(t *testing.T) string {
	p := filepath.Join(t.TempDir(), ".hikyo-download-x")
	if err := os.WriteFile(p, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func gone(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("%s still exists (%v)", p, err)
	}
}

func TestPendingDownloadRemovedAfterLifetime(t *testing.T) {
	p := NewPendingDownloads()
	p.lifetime = 20 * time.Millisecond
	tmp := partial(t)
	p.Add(1, "h", tmp, "/dest", false)
	time.Sleep(200 * time.Millisecond)
	gone(t, tmp)
	if _, _, _, ok := p.Take(1); ok {
		t.Error("a late finish must find nothing and write nothing")
	}
}

func TestPendingDownloadRemovedWithItsWindow(t *testing.T) {
	p := NewPendingDownloads()
	a, b := partial(t), partial(t)
	p.Add(1, "h1", a, "/a", false)
	p.Add(2, "h2", b, "/b", false)
	p.Drop("h1")
	gone(t, a)
	if _, err := os.Stat(b); err != nil {
		t.Errorf("another window's download touched: %v", err)
	}
	if _, _, _, ok := p.Take(1); ok {
		t.Error("dropped entry still taken")
	}
	if tmp, final, _, ok := p.Take(2); !ok || tmp != b || final != "/b" {
		t.Errorf("take = %q %q %v", tmp, final, ok)
	}
}

func TestPendingDownloadTakeStopsTimer(t *testing.T) {
	p := NewPendingDownloads()
	p.lifetime = 20 * time.Millisecond
	tmp := partial(t)
	p.Add(1, "h", tmp, "/d", false)
	p.Take(1)
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(tmp); err != nil {
		t.Errorf("taken download removed by the timer: %v", err)
	}
}
