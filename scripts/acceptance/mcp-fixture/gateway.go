//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type wireObservation struct {
	Invocation      string `json:"invocation_sha256,omitempty"`
	Method          string `json:"method"`
	Requested       string `json:"requested_protocol,omitempty"`
	Header          string `json:"header_protocol,omitempty"`
	Negotiated      string `json:"negotiated_protocol,omitempty"`
	Status          int    `json:"status"`
	RequestSession  bool   `json:"request_session"`
	ResponseSession bool   `json:"response_session"`
	BearerPresent   bool   `json:"bearer_present"`
}

const probeHeader = "X-Hikyo-Fixture-Probe"

type wireReceipt struct {
	Schema       string            `json:"schema"`
	Failed       bool              `json:"failed"`
	Observations []wireObservation `json:"observations"`
}

func invocationHash(value string) string {
	if !sha256Pattern.MatchString(value) {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type wireRecorder struct {
	mu           sync.Mutex
	observations []wireObservation
	directory    string
	failed       atomic.Bool
}
type wireKey struct{}

func (w *wireRecorder) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp/codex" {
			next.ServeHTTP(out, r)
			return
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, (256<<10)+1))
		r.Body.Close()
		if err != nil || len(b) > 256<<10 {
			http.Error(out, "bounded request required", 400)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(b))
		var req struct {
			Method string `json:"method"`
			Params struct {
				Protocol string `json:"protocolVersion"`
			} `json:"params"`
		}
		if json.Unmarshal(b, &req) != nil {
			http.Error(out, "JSON request required", 400)
			return
		}
		switch req.Method {
		case "initialize", "notifications/initialized", "notifications/cancelled", "ping", "tools/list", "tools/call":
		default:
			req.Method = "other"
		}
		protocol := func(s string) string {
			if s == "" || s == "2025-06-18" || s == "2026-07-28" {
				return s
			}
			return "other"
		}
		observation := &wireObservation{Invocation: invocationHash(r.Header.Get(probeHeader)), Method: req.Method, Requested: protocol(req.Params.Protocol), Header: protocol(r.Header.Get("Mcp-Protocol-Version")), RequestSession: len(r.Header.Values("Mcp-Session-Id")) != 0, BearerPresent: r.Header.Get("Authorization") != ""}
		next.ServeHTTP(out, r.WithContext(context.WithValue(r.Context(), wireKey{}, observation)))
		w.mu.Lock()
		defer w.mu.Unlock()
		if len(w.observations) >= 1000 {
			w.failed.Store(true)
		} else {
			w.observations = append(w.observations, *observation)
		}
		if w.directory != "" {
			if err := replacePrivate(filepath.Join(w.directory, "wire-receipt.json"), w.snapshotLocked()); err != nil {
				w.failed.Store(true)
				// A cached file is never the public oracle; best-effort persist
				// the failure, then remove stale success if writes still fail.
				if replacePrivate(filepath.Join(w.directory, "wire-receipt.json"), w.snapshotLocked()) != nil {
					_ = os.Remove(filepath.Join(w.directory, "wire-receipt.json"))
				}
			}
		}
	})
}

func (w *wireRecorder) response(h *http.Response) error {
	value := h.Request.Context().Value(wireKey{})
	if value == nil {
		return nil
	}
	observation, ok := value.(*wireObservation)
	if !ok {
		return errors.New("wire context unavailable")
	}
	observation.Status = h.StatusCode
	observation.ResponseSession = len(h.Header.Values("Mcp-Session-Id")) != 0
	if observation.Method == "initialize" {
		b, err := io.ReadAll(io.LimitReader(h.Body, (64<<10)+1))
		h.Body.Close()
		if err != nil || len(b) > 64<<10 {
			return errors.New("bounded initialization reply missing")
		}
		h.Body = io.NopCloser(bytes.NewReader(b))
		var reply struct {
			Result struct {
				Protocol string `json:"protocolVersion"`
			} `json:"result"`
		}
		if json.Unmarshal(b, &reply) == nil && reply.Result.Protocol == "2025-06-18" {
			observation.Negotiated = reply.Result.Protocol
		}
	}
	return nil
}

func (w *wireRecorder) valid(expected string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return validCodexWire(w.snapshotLocked(), expected)
}

func (w *wireRecorder) snapshotLocked() wireReceipt {
	return wireReceipt{Schema: "hikyo.dev/mcp-wire/v1", Failed: w.failed.Load(), Observations: append([]wireObservation(nil), w.observations...)}
}
func (w *wireRecorder) snapshot() wireReceipt {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.snapshotLocked()
}
func validCodexWire(document wireReceipt, expected string) bool {
	if document.Schema != "hikyo.dev/mcp-wire/v1" || document.Failed || invocationHash(expected) == "" {
		return false
	}
	wanted := invocationHash(expected)
	initialized, called := false, false
	for _, o := range document.Observations {
		if o.RequestSession || o.ResponseSession {
			return false
		}
		if o.Invocation != wanted {
			continue
		}
		if o.Method == "initialize" && o.Status == 200 && o.Requested == "2025-06-18" && o.Negotiated == "2025-06-18" {
			initialized = true
		}
		if o.Method == "tools/call" && o.Status == 200 && o.Header == "2025-06-18" && o.BearerPresent {
			called = true
		}
	}
	return initialized && called
}

// replacePrivate is restricted to redacted rolling observations. It never
// replaces credential or installation custody.
func replacePrivate(path string, value any) error {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600) {
		return errors.New("unsafe receipt destination")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	tmp := path + ".next"
	if err := writePrivate(tmp, value); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

type gatewaySettings struct {
	Origin      string   `json:"origin"`
	Listen      string   `json:"listen"`
	Certificate string   `json:"certificate_file"`
	Key         string   `json:"key_file"`
	BackendCA   string   `json:"backend_ca_file"`
	Backends    []string `json:"backends"`
}

type observedTransport struct{ base *http.Transport }

func (t observedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var peer string
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { peer = info.Conn.RemoteAddr().String() }}
	h, err := t.base.RoundTrip(r.WithContext(httptrace.WithClientTrace(r.Context(), trace)))
	if err == nil {
		h.Header.Set("X-Fixture-Process", peer)
	}
	return h, err
}

func serveGateway(ctx context.Context, dir string) error {
	var cfg gatewaySettings
	if err := readPrivate(filepath.Join(dir, "gateway.json"), &cfg); err != nil {
		return err
	}
	origin, err := url.Parse(cfg.Origin)
	if err != nil || origin.Scheme != "https" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || cfg.Listen != ":8443" || len(cfg.Backends) != 2 || cfg.Backends[0] == cfg.Backends[1] {
		return errors.New("exact fixture gateway origin and two backends required")
	}
	want := []string{"hikyo-0.server.hikyo-mcp-651.svc.cluster.local:8443", "hikyo-1.server.hikyo-mcp-651.svc.cluster.local:8443"}
	for i, b := range cfg.Backends {
		if b != want[i] {
			return errors.New("gateway refuses nonfixture backend")
		}
	}
	roots := x509.NewCertPool()
	ca, err := os.ReadFile(cfg.BackendCA)
	if err != nil || !roots.AppendCertsFromPEM(ca) {
		return errors.New("gateway backend trust unavailable")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: origin.Hostname(), MinVersion: tls.VersionTLS12}, MaxIdleConnsPerHost: 4}
	defer transport.CloseIdleConnections()
	var next atomic.Uint64
	wire := &wireRecorder{directory: dir}
	closeSnapshot, err := serveWireSnapshot(ctx, dir, wire)
	if err != nil {
		return err
	}
	defer closeSnapshot()
	proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		i := int(next.Add(1)-1) % 2
		backend, _ := url.Parse("https://" + cfg.Backends[i])
		pr.SetURL(backend)
		pr.Out.Host = origin.Host
		pr.Out.Header.Del("X-Fixture-Process")
		pr.SetXForwarded()
		pr.Out.Header.Set("X-Forwarded-Proto", "https")
		pr.Out.Header.Set("X-Fixture-Backend", cfg.Backends[i])
	}, Transport: observedTransport{base: transport}, ModifyResponse: func(h *http.Response) error {
		return wire.response(h)
	}, ErrorHandler: func(out http.ResponseWriter, _ *http.Request, _ error) { out.WriteHeader(502) }, ErrorLog: log.New(io.Discard, "", 0), FlushInterval: -1}
	handler := wire.wrap(proxy)
	server := &http.Server{Addr: cfg.Listen, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		if r.Host != origin.Host || (r.URL.Path != "/mcp" && r.URL.Path != "/mcp/codex" && r.URL.Path != "/api/v1/meta") {
			out.WriteHeader(404)
			return
		}
		handler.ServeHTTP(out, r)
	})}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServeTLS(cfg.Certificate, cfg.Key) }()
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			return errors.New("fixture gateway listener failed")
		}
		return nil
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}
