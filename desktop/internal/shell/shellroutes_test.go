package shell

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellRoutesAreAClosedList(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/__shell/nope", 400},
		{"GET", "/__shell/", 400},
		{"POST", "/__shell/origin", 405},
		{"GET", "/__shell/reauth", 405},
		{"DELETE", "/__shell/download", 405},
	} {
		if rec := h.page(c, tc.method, tc.path, ""); rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
	if rec := h.page(c, "GET", "/__shell/origin", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), c.info.Origin) {
		t.Errorf("origin: %d %s", rec.Code, rec.Body)
	}
}

func TestShellRoutesRequireTheOwnWindow(t *testing.T) {
	h := newHarness(t)
	a, _ := h.connect()
	b, _ := h.connect()
	for _, path := range []string{"/__shell/origin", "/__shell/reauth", "/__shell/open-in-browser"} {
		rec := h.pageFrom(a.host, "win-"+b.host, "POST", path, `{}`)
		if rec.Code != 403 {
			t.Errorf("%s from another window = %d", path, rec.Code)
		}
	}
	if len(h.openedURLs()) != 0 {
		t.Error("another window opened a browser")
	}
}

func TestShellRoutesRejectHostileBodies(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	valid := `{"purpose":"self-config","self_config":{"intent":"x"}}`
	_ = valid
	huge := `{"purpose":"` + strings.Repeat("a", 2<<20) + `"}`
	for _, route := range []string{"reauth", "sign-in", "open-in-browser", "clipboard"} {
		path := "/__shell/" + route
		for name, body := range map[string]string{
			"oversized":     huge,
			"duplicate key": `{"path":"/a","path":"/b","method":"x","method":"y"}`,
			"unknown key":   `{"nope":1}`,
			"not json":      `path=/x`,
			"array":         `[]`,
			"null":          `null`,
		} {
			rec := h.page(c, "POST", path, body)
			if rec.Code < 400 || rec.Code >= 500 {
				t.Errorf("%s %s = %d", route, name, rec.Code)
			}
		}
		// A form content type is not JSON.
		rec := h.page(c, "POST", path, `{}`, "Content-Type", "text/plain")
		if rec.Code != 400 {
			t.Errorf("%s with text/plain = %d", route, rec.Code)
		}
	}
	if len(h.openedURLs()) != 0 || h.inst.total() != 0 {
		t.Errorf("hostile bodies caused effects: opened %v, requests %d", h.openedURLs(), h.inst.total())
	}
}

func TestReauthValidation(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	for name, body := range map[string]string{
		"developer-credential purpose":   `{"purpose":"developer-credential"}`,
		"developer-credential operation": `{"purpose":"self-config","operation":"developer-credential.mint"}`,
		"unknown purpose":                `{"purpose":"anything"}`,
		"empty":                          `{}`,
		"bad environment id":             `{"purpose":"reveal","environment_ids":["../x"]}`,
	} {
		rec := h.page(c, "POST", "/__shell/reauth", body)
		if rec.Code != 400 && rec.Code != 403 {
			t.Errorf("%s = %d %s", name, rec.Code, rec.Body)
		}
	}
	if len(h.openedURLs()) != 0 {
		t.Error("an invalid ceremony opened the browser")
	}
}

func TestShellRoutesAreRateLimited(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.inst.on("GET /x", nil)
	codes := map[int]int{}
	for i := 0; i < 12; i++ {
		rec := h.page(c, "POST", "/__shell/open-in-browser", `{"path":"/orgs"}`)
		codes[rec.Code]++
	}
	if codes[429] == 0 || codes[204] != 10 {
		t.Errorf("responses: %v", codes)
	}
	if n := len(h.openedURLs()); n != 10 {
		t.Errorf("opened %d pages", n)
	}
}

func TestOpenInBrowserCanonicalization(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	ok := map[string]string{
		"/":                   "/",
		"/orgs/acme/projects": "/orgs/acme/projects",
		"/orgs?tab=keys":      "/orgs?tab=keys",
		"/login":              "/login",
		"/reauth/other":       "/reauth/other",
		"/workspaces":         "/workspaces",
		"/login/cliff":        "/login/cliff",
		"/orgs?returning=1":   "/orgs?returning=1",
		"/orgs?next=/x":       "/orgs?next=/x",
	}
	for in, want := range ok {
		got, err := c.BrowserURL(in)
		if err != nil || got != c.info.Origin+want {
			t.Errorf("%q = %q, %v", in, got, err)
		}
	}
	bad := []string{
		"", "orgs", "//evil.example/x", "///x", "https://evil.example/x", "/\\evil.example", "/x\\y",
		"/a//b", "/a/../b", "/a/./b", "/a/%2e%2e/b", "/a/%2E/b", "/a%2fb", "/a%5cb", "/a%252fb", "/%2541",
		"/x#frag", "/x y", "/orgs/a%20b", "/x\x00", "/x\n", "/x‮", "/xé", "/x​", "/orgs?returnTo=/x", "/orgs?return_to=/x",
		"/orgs?RETURN-TO=/x", "/orgs?a=1&returnTo=https://x", "/orgs?%72eturnTo=/x",
		"/login/cli", "/login/cli/", "/LOGIN/CLI", "/login/cli/x", "/reauth/cli", "/Reauth/Cli?x=1",
		"/workspace/approve", "/workspace/approve/x", "/%6cogin/cli", "/login/%63li", "/login//cli",
		"/" + strings.Repeat("a", maxBrowserPath),
	}
	for _, in := range bad {
		if got, err := c.BrowserURL(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestExternalLinkPolicy(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	for in, want := range map[string]string{
		"https://example.com/a?b=1#frag": "https://example.com/a?b=1",
		"https://example.com":            "https://example.com",
	} {
		if got, err := c.ExternalLink(in); err != nil || got != want {
			t.Errorf("%q = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{
		"http://example.com", "file:///etc/passwd", "javascript:alert(1)", "data:text/html,x", "blob:x", "wails://c-x/", "mailto:a@b.c",
		"https://user:pw@example.com", "https:///x", "ssh://x", "",
	} {
		if got, err := c.ExternalLink(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
	// A link to the connection's own origin obeys the handoff refusals.
	h2 := newHarness(t)
	c2, _ := h2.connect()
	c2.inst.Origin = "https://hikyo.example"
	for _, spelling := range []string{
		"https://hikyo.example/login/cli?x=1", "https://HIKYO.example/login/cli", "https://hikyo.example:443/login/cli",
		"https://hikyo.example./login/cli", "https://hikyo.example:443/reauth/cli",
	} {
		if got, err := c2.ExternalLink(spelling); err == nil {
			t.Errorf("handoff page opened through %q as %q", spelling, got)
		}
	}
	if got, err := c2.ExternalLink("https://hikyo.example.:443/orgs"); err != nil || got != "https://hikyo.example/orgs" {
		t.Errorf("own-origin spelling: %q %v", got, err)
	}
	if got, err := c2.ExternalLink("https://hikyo.example/orgs#x"); err != nil || got != "https://hikyo.example/orgs" {
		t.Errorf("own-origin link: %q %v", got, err)
	}
}

func TestDownloadRoutes(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	dir := t.TempDir()
	h.native.savePath, h.native.saveOK = filepath.Join(dir, "out.bin"), true
	h.inst.on("GET /api/v1/export", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret.Token {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte("payload"))
	})
	// Bytes the page already holds.
	body := `{"bytes_base64":"` + base64.StdEncoding.EncodeToString([]byte("hello")) + `","filename":"../../.ssh/a‮txt.exe"}`
	if rec := h.page(c, "POST", "/__shell/download", body); rec.Code != 204 {
		t.Fatalf("bytes: %d %s", rec.Code, rec.Body)
	}
	if got, _ := os.ReadFile(h.native.savePath); string(got) != "hello" {
		t.Errorf("file holds %q", got)
	}
	if name := h.native.saveNames[0]; strings.ContainsAny(name, "/\\‮") || strings.HasPrefix(name, ".") {
		t.Errorf("save panel got %q", name)
	}
	// A proxied path, replacing the file the person approved replacing.
	h.native.saveReplace = true
	if rec := h.page(c, "POST", "/__shell/download", `{"path":"/api/v1/export","filename":"e.bin"}`); rec.Code != 204 {
		t.Fatalf("path: %d %s", rec.Code, rec.Body)
	}
	if got, _ := os.ReadFile(h.native.savePath); string(got) != "payload" {
		t.Errorf("file holds %q", got)
	}
	// Cancelling the panel writes nothing.
	h.native.saveOK = false
	_ = os.Remove(h.native.savePath)
	h.page(c, "POST", "/__shell/download", `{"bytes_base64":"aGk=","filename":"x"}`)
	if _, err := os.Stat(h.native.savePath); err == nil {
		t.Error("a cancelled save wrote a file")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("leftovers: %v", entries)
	}
}

func TestDownloadRefusals(t *testing.T) {
	h := newHarness(t)
	c, _ := h.connect()
	h.native.savePath, h.native.saveOK = filepath.Join(t.TempDir(), "o"), true
	for name, body := range map[string]string{
		"both":         `{"path":"/api/v1/x","bytes_base64":"aGk=","filename":"x"}`,
		"neither":      `{"filename":"x"}`,
		"not api":      `{"path":"/assets/x","filename":"x"}`,
		"cli-login":    `{"path":"/api/v1/auth/cli-login/redeem","filename":"x"}`,
		"dot segments": `{"path":"/api/v1/../x","filename":"x"}`,
		"bad base64":   `{"bytes_base64":"!!","filename":"x"}`,
		"other origin": `{"path":"https://evil.example/api/v1/x","filename":"x"}`,
	} {
		before := h.inst.total()
		rec := h.page(c, "POST", "/__shell/download", body)
		if rec.Code < 400 || rec.Code >= 500 || h.inst.total() != before {
			t.Errorf("%s: %d, instance saw %d requests", name, rec.Code, h.inst.total()-before)
		}
	}
	if len(h.native.saveNames) != 0 {
		t.Errorf("save panel shown for a refused download: %v", h.native.saveNames)
	}
}

func TestSanitizeFilename(t *testing.T) {
	long := strings.Repeat("é", 300)
	for in, want := range map[string]string{
		"report.pdf":                    "report.pdf",
		"../../etc/passwd":              "passwd",
		`..\..\win\system.ini`:          "system.ini",
		".hidden":                       "hidden",
		"   ":                           "download",
		"":                              "download",
		"..":                            "download",
		"a\x00b\nc.txt":                 "abc.txt",
		"invoice‮fdp.exe":               "invoicefdp.exe",
		"a:b.txt":                       "a-b.txt",
		"/":                             "download",
		"bad\xffname":                   "badname",
		"name.verylongextension1234567": "name.verylongextension1234567",
	} {
		if got := SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	if got := SanitizeFilename(long + ".txt"); len(got) > 205 || !strings.HasSuffix(got, ".txt") {
		t.Errorf("long name = %d bytes", len(got))
	}
}
