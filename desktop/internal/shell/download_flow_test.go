package shell

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (h *harness) savingTo(name string) string {
	h.native.savePath, h.native.saveOK = filepath.Join(h.t.TempDir(), name), true
	return h.native.savePath
}

func (h *harness) alertsContain(substr string) bool {
	h.native.mu.Lock()
	defer h.native.mu.Unlock()
	for _, a := range h.native.alerts {
		if strings.Contains(a, substr) {
			return true
		}
	}
	return false
}

// The panel appears only after the instance has answered 200.
func TestDownloadAsksTheInstanceBeforeTheSavePanel(t *testing.T) {
	for status, want := range map[int]string{
		401: "sign in again", 403: "may not download", 404: "not available",
		429: "rate limiting", 500: "answered 500", 302: "answered 302",
	} {
		h := newHarness(t)
		c, _ := h.connect()
		h.savingTo("out")
		h.inst.on("GET /api/v1/export", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		rec := h.page(c, "POST", "/__shell/download", `{"path":"/api/v1/export","filename":"e.bin"}`)
		if rec.Code < 400 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("status %d: %d %s", status, rec.Code, rec.Body)
		}
		if len(h.native.saveNames) != 0 {
			t.Errorf("status %d showed the save panel", status)
		}
	}
}

func TestProxiedNavigationDownloadsAskFirstAndAlertOnFailure(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	dest := h.savingTo("out")
	own := PageScheme + "://" + c.host
	h.inst.on("GET /api/v1/missing", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
	})
	h.inst.on("GET /api/v1/audit/export/data", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret.Token {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="audit-2026.csv"`)
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte("a,b\n"))
	})

	// The 404 JSON a main-frame navigation would show in place of the app.
	if got := h.m.Navigate(NavRequest{PageHost: c.host, URL: own + "/api/v1/missing", MainFrame: true}); got != NavCancel {
		t.Fatalf("main-frame navigation into /api/ = %v", got)
	}
	h.m.Wait()
	if !h.alertsContain("not available") || len(h.native.saveNames) != 0 {
		t.Errorf("alerts %v, panels %v", h.native.alerts, h.native.saveNames)
	}

	// A link on a 200: named from Content-Disposition, saved once.
	if got := h.m.Navigate(NavRequest{PageHost: c.host, URL: own + "/api/v1/audit/export/data", MainFrame: true, Download: true}); got != NavCancel {
		t.Fatalf("download = %v", got)
	}
	h.m.Wait()
	if len(h.native.saveNames) != 1 || h.native.saveNames[0] != "audit-2026.csv" {
		t.Errorf("panel names %v", h.native.saveNames)
	}
	if got, _ := os.ReadFile(dest); string(got) != "a,b\n" {
		t.Errorf("saved %q", got)
	}
	if n := len(h.inst.requests("GET", "/api/v1/audit/export/data")); n != 1 {
		t.Errorf("the instance saw %d requests for one click", n)
	}

	// The link's download attribute wins over the header.
	h.m.Navigate(NavRequest{PageHost: c.host, URL: own + "/api/v1/audit/export/data", Download: true, Filename: "mine.csv"})
	h.m.Wait()
	if got := h.native.saveNames[len(h.native.saveNames)-1]; got != "mine.csv" {
		t.Errorf("download attribute ignored: %q", got)
	}

	// Without either, the last path segment.
	h.inst.on("GET /api/v1/audit/export/plain", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) })
	h.m.Navigate(NavRequest{PageHost: c.host, URL: own + "/api/v1/audit/export/plain", Download: true})
	h.m.Wait()
	if got := h.native.saveNames[len(h.native.saveNames)-1]; got != "plain" {
		t.Errorf("fallback name %q", got)
	}
}

func TestProxiedNavigationRefusalsAreShown(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.savingTo("out")
	own := PageScheme + "://" + c.host
	before := h.inst.total()

	// A form POST is not replayed as a GET.
	h.m.Navigate(NavRequest{PageHost: c.host, URL: own + "/api/v1/export", MainFrame: true, Download: true, Method: "POST"})
	h.m.Wait()
	if !h.alertsContain("form submission") {
		t.Errorf("no alert for a POST: %v", h.native.alerts)
	}
	// A refused route says so.
	h.m.Navigate(NavRequest{PageHost: c.host, URL: own + "/api/v1/auth/cli-login/redeem", Download: true})
	h.m.Wait()
	if !h.alertsContain("isn't available") {
		t.Errorf("no alert for a refused path: %v", h.native.alerts)
	}
	if h.inst.total() != before || len(h.native.saveNames) != 0 {
		t.Error("a refused download reached the instance or showed the panel")
	}

	// Busy: a second download while one waits for the panel.
	release, _ := c.downloadSlot.take()
	h.m.Navigate(NavRequest{PageHost: c.host, URL: own + "/api/v1/export", Download: true})
	h.m.Wait()
	release()
	if !h.alertsContain("already waiting") {
		t.Errorf("no alert when busy: %v", h.native.alerts)
	}

	// Rate limit.
	for c.downloadLimit.allow() {
	}
	h.m.Navigate(NavRequest{PageHost: c.host, URL: own + "/api/v1/export", Download: true})
	h.m.Wait()
	if !h.alertsContain("Too many downloads") {
		t.Errorf("no alert when rate limited: %v", h.native.alerts)
	}
	// An ordinary page route still loads.
	if got := h.m.Navigate(NavRequest{PageHost: c.host, URL: own + "/orgs", MainFrame: true}); got != NavAllow {
		t.Errorf("page navigation = %v", got)
	}
}

func TestDownloadBytesUpToTheSPALimitAndTypeHint(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	dest := h.savingTo("big")
	data := make([]byte, maxDownloadBytes)
	body := `{"bytes_base64":"` + base64.StdEncoding.EncodeToString(data) + `","filename":"audit.csv","content_type":"text/csv; charset=utf-8"}`
	rec := h.page(c, "POST", "/__shell/download", body)
	if rec.Code != 204 {
		t.Fatalf("64 MiB: %d %s", rec.Code, rec.Body)
	}
	if fi, err := os.Stat(dest); err != nil || fi.Size() != maxDownloadBytes {
		t.Errorf("saved %v %v", fi, err)
	}
	if len(h.native.saveMIME) != 1 || h.native.saveMIME[0] != "text/csv" {
		t.Errorf("type hint %v", h.native.saveMIME)
	}
	over := `{"bytes_base64":"` + base64.StdEncoding.EncodeToString(make([]byte, maxDownloadBytes+1)) + `","filename":"x"}`
	if rec := h.page(c, "POST", "/__shell/download", over); rec.Code != 400 {
		t.Errorf("64 MiB + 1: %d", rec.Code)
	}
	if rec := h.page(c, "POST", "/__shell/download", `{"bytes_base64":"aGk=","filename":"x","content_type":"not a type"}`); rec.Code != 400 {
		t.Errorf("bad content_type: %d", rec.Code)
	}
}

func TestRefusalAlertsAreRateLimited(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	for range 30 {
		h.m.Navigate(NavRequest{PageHost: c.host, URL: PageScheme + "://" + c.host + "/api/v1/x", Download: true, Method: "POST"})
	}
	if n := len(h.native.alerts); n == 0 || n > 4 {
		t.Fatalf("%d alerts for 30 refusals", n)
	}
}

func TestSignInWithoutASessionNamesTheOriginFirst(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	_ = h.secrets.Delete(c.host)
	m2 := h.reload()
	h.native.confirmAnswer = false
	before := h.inst.total()
	if err := m2.SignIn(t.Context(), c.host, ""); err == nil {
		t.Fatal("declined sign-in went ahead")
	}
	last := h.native.confirms[len(h.native.confirms)-1]
	if !strings.Contains(last, c.Info().Origin) || h.inst.total() != before || len(h.openedURLs()) != 0 {
		t.Errorf("confirm %q, requests %d", last, h.inst.total()-before)
	}
}
