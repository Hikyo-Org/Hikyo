//go:build (ios || android) && probe

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Debug server for the device experiments, compiled only with the probe tag. Every request needs the per-launch secret that is written to the app log.
func init() { go probeServer() }

func stamp() string { return time.Now().Format("15:04:05.000") }

var ticking atomic.Bool

const probeAddr = "127.0.0.1:18099"

// guard rejects any Host but the exact probe address (blocks browsers and DNS rebinding), non-POST for actions, and a wrong secret.
func guard(secret string, post bool, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Host != probeAddr || (post && r.Method != http.MethodPost) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Probe-Secret")), []byte(secret)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func probeServer() {
	secret := proof()
	log.Printf("PROBE-SECRET %s", secret)
	mux := http.NewServeMux()
	handle := func(path string, post bool, h http.HandlerFunc) { mux.HandleFunc(path, guard(secret, post, h)) }
	handle("/ping", false, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "pong", stamp()) })
	handle("/tick", true, func(w http.ResponseWriter, r *http.Request) {
		if ticking.CompareAndSwap(false, true) {
			go func() {
				for range time.Tick(time.Second) {
					log.Printf("TICK %s", stamp())
				}
			}()
		}
		fmt.Fprintln(w, "ticking")
	})
	// The hop listener is separate and answers only /cb, so the browser needs no secret and gets no controls.
	handle("/listen", true, func(w http.ResponseWriter, r *http.Request) {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		go func() {
			hop := http.NewServeMux()
			hop.HandleFunc("/cb", func(w http.ResponseWriter, r *http.Request) {
				log.Printf("HOP-ACCEPT %s %s %s ua=%q", stamp(), l.Addr(), r.URL.RequestURI(), r.UserAgent())
				fmt.Fprintln(w, "accepted by the app at", stamp())
			})
			srv := &http.Server{Handler: hop}
			time.AfterFunc(10*time.Minute, func() { srv.Close() })
			_ = srv.Serve(l)
		}()
		log.Printf("HOP-LISTEN %s %s", stamp(), l.Addr())
		fmt.Fprintln(w, l.Addr().(*net.TCPAddr).Port)
	})
	handle("/open", true, func(w http.ResponseWriter, r *http.Request) {
		log.Printf("OPEN %s %s", stamp(), r.URL.Query().Get("url"))
		fmt.Fprintln(w, openURL(r.URL.Query().Get("url")))
	})
	handle("/nav", true, func(w http.ResponseWriter, r *http.Request) {
		host := r.URL.Query().Get("host")
		if host == "" {
			host = connHost(0)
		}
		// SetURL is a no-op on Android, so navigate from script.
		target, _ := json.Marshal(pageScheme + "://" + host + r.URL.Query().Get("path"))
		application.Get().Window.GetAll()[0].ExecJS("location.href=" + string(target))
		fmt.Fprintln(w, "navigating")
	})
	// Reports success and whether a value exists or matches, never the value.
	handle("/secure", true, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("op") {
		case "set":
			fmt.Fprintln(w, "set err =", secureSet(q.Get("k"), q.Get("v")))
		case "has":
			v, found, err := secureGet(q.Get("k"))
			match := q.Has("v") && subtle.ConstantTimeCompare([]byte(v), []byte(q.Get("v"))) == 1
			fmt.Fprintf(w, "has found=%v match=%v err=%v\n", found, match, err)
		case "del":
			fmt.Fprintln(w, "del err =", secureDelete(q.Get("k")))
		}
	})
	handle("/login", true, func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		go func() {
			if err := cliLogin(connections[connHost(0)], mode); err != nil {
				log.Printf("LOGIN-FAILED %s %v", stamp(), err)
			}
		}()
		fmt.Fprintln(w, "login started, mode", mode)
	})
	probeRoutes(handle)
	log.Fatal(http.ListenAndServe(probeAddr, mux))
}

func proof() string {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func postAnon(ctx context.Context, c *conn, path string, body, out any) error {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin.JoinPath(path).String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := bearerClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s -> %d %s", path, resp.StatusCode, data)
	}
	return json.Unmarshal(data, out)
}

// cliLogin is the CLI's loopback login (internal/cli/login_transport.go) with the browser hop chosen by mode: "safari" or "ases".
func cliLogin(c *conn, mode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer l.Close()
	state, verifier := proof(), proof()
	digest := sha256.Sum256([]byte(verifier))
	redirect := "http://" + l.Addr().String() + "/callback"
	var started struct {
		State string `json:"state"`
	}
	if err := postAnon(ctx, c, "/api/v1/auth/cli-login/start", map[string]string{
		"transport": "loopback", "state": state, "pkce_challenge": base64.RawURLEncoding.EncodeToString(digest[:]), "redirect_uri": redirect,
	}, &started); err != nil {
		return err
	}
	log.Printf("LOGIN-START %s listener=%s", stamp(), l.Addr())
	code := make(chan string, 1)
	var once sync.Once
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("LOGIN-CALLBACK %s %s", stamp(), r.URL.Path)
		if r.Method != http.MethodGet || r.URL.Path != "/callback" || !exactCallbackQuery(r.URL.Query(), state) {
			http.Error(w, "This CLI login callback did not match the request.", http.StatusBadRequest)
			return
		}
		once.Do(func() { code <- r.URL.Query().Get("code") })
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "CLI login received. Return to the app.\n")
		hopDone(mode)
	})}
	go srv.Serve(l)
	defer srv.Close()
	target := *c.origin
	target.Path, target.RawQuery = "/login/cli", url.Values{"state": {state}}.Encode()
	log.Printf("LOGIN-OPEN %s %s %s", stamp(), mode, strings.ReplaceAll(target.String(), state, "<state>"))
	if err := hopOpen(mode, target.String()); err != nil {
		return err
	}
	var got string
	select {
	case got = <-code:
	case <-ctx.Done():
		return ctx.Err()
	}
	var redeemed struct {
		SessionToken string `json:"session_token"`
	}
	if err := postAnon(ctx, c, "/api/v1/auth/cli-login/redeem", map[string]string{"code": got, "pkce_verifier": verifier, "redirect_uri": redirect}, &redeemed); err != nil {
		return err
	}
	if redeemed.SessionToken == "" {
		return fmt.Errorf("redeem returned no session token")
	}
	c.setBearer(redeemed.SessionToken)
	log.Printf("LOGIN-DONE %s session redeemed, bearer set (not logged)", stamp())
	return nil
}
