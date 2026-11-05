// Throwaway spike of the UI-only desktop shell, option C1 in
// docs/reports/server-webapp-decoupling-2026-11-04.md. Do not ship.
//
// Each named CLI session gets a window on wails://conn-N/. Every request from
// that window is proxied to the instance with the session as a bearer, so the
// instance's own SPA runs same-origin and JavaScript never sees the token.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type instances []string

func (i *instances) String() string     { return strings.Join(*i, ",") }
func (i *instances) Set(v string) error { *i = append(*i, v); return nil }

type session struct {
	Origin string `json:"origin"`
	Token  string `json:"token"`
}

// conn is one connection: the instance, its current bearer, and the proxy that spends it.
type conn struct {
	*httputil.ReverseProxy
	origin *url.URL
	mu     sync.RWMutex
	token  string
	busy   atomic.Bool // one reauth ceremony at a time
}

func (c *conn) bearer() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

func (c *conn) setBearer(t string) {
	c.mu.Lock()
	c.token = t
	c.mu.Unlock()
}

// shortcut: system CA only, so an SPKI-pinned instance fails TLS; a real shell reuses the CLI's trust choice per connection.
func newConn(host string, s session) *conn {
	origin, err := url.Parse(s.Origin)
	if err != nil || origin.Host == "" {
		log.Fatalf("%s: bad origin %q", host, s.Origin)
	}
	c := &conn{origin: origin, token: s.Token}
	c.ReverseProxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(origin)
			h := pr.Out.Header
			for _, name := range []string{"Cookie", "Origin", "Referer", "x-wails-window-id", "x-wails-window-name"} {
				h.Del(name)
			}
			// Let Go's transport decode gzip: a scheme handler's bytes are not decoded by WebKit.
			h.Del("Accept-Encoding")
			h.Set("Authorization", "Bearer "+c.bearer())
		},
		ModifyResponse: func(r *http.Response) error {
			r.Header.Del("Set-Cookie")
			log.Printf("%s %s %s -> %d %s", host, r.Request.Method, r.Request.URL.Path, r.StatusCode, r.Header.Get("Content-Type"))
			if r.Request.Method == http.MethodGet || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				return nil
			}
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				return err
			}
			r.Body.Close()
			out, rotated := takeRotatedToken(raw)
			if rotated != "" {
				c.setBearer(rotated)
				log.Printf("%s %s: session rotated, token withheld from the page", host, r.Request.URL.Path)
			}
			r.Body = io.NopCloser(bytes.NewReader(out))
			r.ContentLength = int64(len(out))
			r.Header.Set("Content-Length", strconv.Itoa(len(out)))
			return nil
		},
		// Streams each chunk as it arrives, which SSE needs.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("%s %s %s -> proxy error: %v", host, r.Method, r.URL.Path, err)
			http.Error(w, "instance unreachable", http.StatusBadGateway)
		},
	}
	return c
}

// takeRotatedToken removes a rotated bearer from a response body. Step-up
// rotates a CLI session: the new token is in the JSON and the old one is dead.
// A browser never sees this field, so the SPA does not miss it. Only the two
// places the contract puts it are read, because a nested key could be a secret's name.
func takeRotatedToken(raw []byte) ([]byte, string) {
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil {
		return raw, ""
	}
	if t := stringField(body, "session_token"); t != "" {
		delete(body, "session_token")
		return remarshal(raw, body), t
	}
	var login map[string]json.RawMessage
	if json.Unmarshal(body["login"], &login) != nil {
		return raw, ""
	}
	t := stringField(login, "session_token")
	if t == "" {
		return raw, ""
	}
	delete(login, "session_token")
	nested, err := json.Marshal(login)
	if err != nil {
		return raw, ""
	}
	body["login"] = nested
	return remarshal(raw, body), t
}

func stringField(m map[string]json.RawMessage, key string) string {
	var v string
	_ = json.Unmarshal(m[key], &v)
	return v
}

func remarshal(raw []byte, body map[string]json.RawMessage) []byte {
	out, err := json.Marshal(body)
	if err != nil {
		return raw
	}
	return out
}

// shell is outermost in Wails' chain, so instance code never reaches /wails/*.
func shell(proxies map[string]*conn, windowHosts map[string]string, probing bool) application.Middleware {
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, known := proxies[r.Host]
			switch {
			case !known:
				log.Printf("refused unknown host %q %s", r.Host, r.URL.Path)
				http.NotFound(w, r)
			case r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "wails://"+r.Host:
				// Without this, connection A could spend connection B's session on a blind request.
				log.Printf("refused cross-connection %s -> %s %s %s", r.Header.Get("Origin"), r.Host, r.Method, r.URL.Path)
				http.Error(w, "cross-connection request refused", http.StatusForbidden)
			case windowHosts[r.Header.Get("x-wails-window-id")] != r.Host:
				// CSP cannot stop a top-level navigation, so a window stays on its own connection here.
				log.Printf("refused window %q -> %s %s %s", r.Header.Get("x-wails-window-id"), r.Host, r.Method, r.URL.Path)
				http.Error(w, "this window belongs to another connection", http.StatusForbidden)
			case strings.HasPrefix(r.URL.Path, "/wails/"):
				log.Printf("refused runtime path from %s: %s", r.Host, r.URL.Path)
				http.Error(w, "runtime is not exposed to instance code", http.StatusForbidden)
			case r.Method == http.MethodPost && r.URL.Path == "/__shell/reauth":
				c.reauth(w, r)
			case probing && r.URL.Path == "/__spike/sleep":
				// An occluded WKWebView throttles setTimeout to about once a minute; network replies still arrive.
				ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
				time.Sleep(time.Duration(ms) * time.Millisecond)
				w.WriteHeader(http.StatusNoContent)
			case probing && r.URL.Path == "/__spike/report":
				body, _ := io.ReadAll(r.Body)
				fmt.Printf("PROBE %s %s %s\n", time.Now().Format(time.TimeOnly), r.Host, body)
				w.WriteHeader(http.StatusNoContent)
			default:
				c.ServeHTTP(w, r)
			}
		})
	}
}

func main() {
	var names instances
	flag.Var(&names, "instance", "CLI instance name from `hikyo login --name`; repeat for more windows")
	probePath := flag.String("probe", "", "JavaScript to run after each navigation, reporting to /__spike/report")
	flag.Parse()
	if len(names) == 0 {
		names = defaultInstances()
	}
	if len(names) == 0 {
		log.Fatal("usage: wails-shell -instance NAME [-instance NAME]")
	}

	sessions := loadSessions()
	proxies := map[string]*conn{}
	hosts := map[string]string{}
	for i, name := range names {
		s, ok := sessions[name]
		if !ok {
			log.Fatalf("no session named %q; run hikyo login first", name)
		}
		host := fmt.Sprintf("conn-%d", i)
		proxies[host] = newConn(host, s)
		hosts[name] = host
		log.Printf("%s = %s (%s)", host, name, s.Origin)
	}

	connections = proxies
	var probe string
	if *probePath != "" {
		raw, err := os.ReadFile(*probePath)
		if err != nil {
			log.Fatal(err)
		}
		probe = string(raw)
	}

	windowHosts := map[string]string{}
	app := application.New(application.Options{
		Name:   "Hikyo Spike",
		Assets: application.AssetOptions{Handler: http.NotFoundHandler(), Middleware: shell(proxies, windowHosts, probe != "")},
		Mac:    application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	for _, name := range names {
		window := app.Window.NewWithOptions(application.WebviewWindowOptions{
			Title: "Hikyo Spike: " + name, Width: 1280, Height: 860, URL: "wails://" + hosts[name] + "/",
		})
		windowHosts[strconv.FormatUint(uint64(window.ID()), 10)] = hosts[name]
		if probe != "" {
			window.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) {
				// ExecJS queues until the Wails runtime reports ready, and this shell never loads that runtime.
				window.HandleMessage("wails:runtime:ready")
				window.Focus()
				window.ExecJS(probe)
			})
		}
	}
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
