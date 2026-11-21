// Command driven runs the desktop shell's probe build against a fake instance
// and checks what a real WKWebView observes (ADR D10). Playwright cannot drive
// WKWebView, so the page runs a script the probe build injects after each
// navigation, and the script reports its findings back to this program through
// the shell's own proxy.
//
//	go build -tags probe -o hikyo-desktop-probe ..
//	go run ./probes/driven -app ./hikyo-desktop-probe
//
// The probe build seeds one connection with the fake token below, so a run adds
// one item to the login keychain; the driver deletes exactly that item at exit.
package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	fakeToken = "probe-token" // the secret probeSeed stores; pages must never see it
	service   = "io.hikyo.desktop"
)

// pageScript runs in the window after every navigation. __ORIGIN__ is the fake
// instance's origin. It reports one result per named check.
const pageScript = `(async () => {
  const checks = {};
  const rec = (name, ok, detail) => { checks[name] = { ok: !!ok, detail: String(detail ?? '') }; };
  const call = async (path, init) => {
    const res = await fetch(path, init);
    const text = await res.text();
    let body = null;
    try { body = JSON.parse(text); } catch { /* not JSON */ }
    return { status: res.status, text, body };
  };
  try {
    rec('app mode scheme', location.protocol === 'wails:', location.protocol);
    const origin = await call('/__shell/origin');
    rec('shell answers its own route', origin.body && origin.body.origin === '__ORIGIN__', origin.text);
    const ping = await call('/__probe/ping');
    rec('proxy forwards page requests', ping.status === 200, ping.status);
    const refused = await call('/api/v1/auth/cli-login/start', { method: 'POST', headers: { 'content-type': 'application/json' }, body: '{}' });
    rec('refused route never leaves the shell', refused.status === 403 && refused.body && refused.body.error === 'refused', refused.status + ' ' + refused.text);
    const runtime = await call('/wails/runtime.js');
    rec('wails runtime is not exposed', runtime.status === 403, runtime.status);
    const stored = document.cookie + JSON.stringify(Object.entries(localStorage)) + JSON.stringify(Object.entries(sessionStorage));
    rec('no credential in page storage', !stored.includes('` + fakeToken + `'), stored.length);
    rec('no credential in the page body', !document.documentElement.outerHTML.includes('` + fakeToken + `'), '');
  } catch (e) {
    rec('script ran to the end', false, e && e.message);
  }
  await fetch('/__probe/report', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(checks) });
})();`

// requiredChecks are the findings a passing run reports, in order.
var requiredChecks = []string{
	"app mode scheme",
	"shell answers its own route",
	"proxy forwards page requests",
	"refused route never leaves the shell",
	"wails runtime is not exposed",
	"no credential in page storage",
	"no credential in the page body",
}

type finding struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// verdict lists every failed expectation of a page report plus what the fake
// instance saw of the page's proxied request. Missing checks fail.
func verdict(report map[string]finding, pingAuth string) []string {
	var failures []string
	for _, name := range requiredChecks {
		f, ok := report[name]
		switch {
		case !ok:
			failures = append(failures, name+": not reported")
		case !f.OK:
			failures = append(failures, fmt.Sprintf("%s: %s", name, f.Detail))
		}
	}
	for name := range report {
		if !sliceContains(requiredChecks, name) && !report[name].OK {
			failures = append(failures, fmt.Sprintf("%s: %s", name, report[name].Detail))
		}
	}
	if pingAuth != "Bearer "+fakeToken {
		failures = append(failures, fmt.Sprintf("the proxy sent Authorization %q to the instance, want the session bearer", pingAuth))
	}
	sort.Strings(failures)
	return failures
}

func sliceContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// instance is the fake Hikyo server.
type instance struct {
	mu       sync.Mutex
	pingAuth string
	report   map[string]finding
	done     chan struct{}
}

func (in *instance) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/meta", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"api_revision":1,"server_version":"probe","protocol_capabilities":["desktop-app/1"]}`)
	})
	mux.HandleFunc("GET /__probe/ping", func(w http.ResponseWriter, r *http.Request) {
		in.mu.Lock()
		in.pingAuth = r.Header.Get("Authorization")
		in.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("POST /__probe/report", func(w http.ResponseWriter, r *http.Request) {
		var report map[string]finding
		if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 1<<20), &report); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		in.mu.Lock()
		first := in.report == nil
		if first {
			in.report = report
		}
		in.mu.Unlock()
		if first {
			close(in.done)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!doctype html><title>probe</title><p>probe page</p>`)
	})
	return mux
}

func main() {
	app := flag.String("app", "", "path to the probe build of the desktop shell (go build -tags probe)")
	timeout := flag.Duration("timeout", 90*time.Second, "how long to wait for the page's report")
	flag.Parse()
	if *app == "" {
		log.Fatal("driven: -app is required")
	}
	if err := run(*app, *timeout); err != nil {
		log.Fatalf("driven: %v", err)
	}
	fmt.Println("driven: every WKWebView check passed")
}

func run(app string, timeout time.Duration) error {
	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	in := &instance{done: make(chan struct{})}
	srv := &http.Server{Handler: in.handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(lis) }()
	defer func() { _ = srv.Close() }()
	origin := "http://" + lis.Addr().String()

	work, err := os.MkdirTemp("", "hikyo-driven-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	script := filepath.Join(work, "probe.js")
	if err := os.WriteFile(script, []byte(strings.ReplaceAll(pageScript, "__ORIGIN__", origin)), 0o600); err != nil {
		return err
	}

	sig, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(sig, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, app)
	cmd.Env = append(os.Environ(),
		"HIKYO_DESKTOP_HOME="+work,
		"HIKYO_DESKTOP_ALLOW_LOOPBACK_HTTP=1",
		"HIKYO_DESKTOP_PROBE_SEED="+origin,
		"HIKYO_DESKTOP_PROBE_JS="+script,
	)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	// exited closes when the app stops; waitErr is valid only after that, so the
	// select and the deferred wait below can both observe the exit.
	var waitErr error
	exited := make(chan struct{})
	go func() { waitErr = cmd.Wait(); close(exited) }()
	defer deleteSeededSecret(work)
	defer func() { cancel(); <-exited }()

	select {
	case <-in.done:
	case <-exited:
		return fmt.Errorf("the app exited before the page reported: %w", waitErr)
	case <-ctx.Done():
		return errors.New("timed out waiting for the page's report")
	}
	in.mu.Lock()
	failures := verdict(in.report, in.pingAuth)
	in.mu.Unlock()
	if len(failures) > 0 {
		return fmt.Errorf("WKWebView checks failed:\n  %s", strings.Join(failures, "\n  "))
	}
	return nil
}

// deleteSeededSecret removes the keychain item the probe seed created: only
// the item for the seeded connection's own host, never the service's others.
func deleteSeededSecret(home string) {
	raw, err := os.ReadFile(filepath.Join(home, "connections.json"))
	if err != nil {
		return
	}
	var list struct {
		Connections []struct {
			Host string `json:"host"`
		} `json:"connections"`
	}
	if json.Unmarshal(raw, &list) != nil {
		return
	}
	for _, c := range list.Connections {
		if c.Host != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			//nolint:gosec // fixed binary; service is a constant and the host came from the connection file this run wrote
			if err := exec.CommandContext(ctx, "security", "delete-generic-password", "-s", service, "-a", c.Host).Run(); err != nil {
				log.Printf("driven: removing the seeded keychain item for %s: %v", c.Host, err)
			}
			cancel()
		}
	}
}
