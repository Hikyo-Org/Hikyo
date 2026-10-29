//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
)

type target struct {
	URL    string `json:"url"`
	CAFile string `json:"ca_file"`
	// Local development gateways supply an independently observed process
	// label. Public execution must bind these labels to pod UID/start/image
	// and the gateway's upstream map before relying on them.
	Processes []string `json:"processes"`
}

type receipt struct {
	Schema        string            `json:"schema"`
	EvidenceClass string            `json:"evidence_class"`
	Started       time.Time         `json:"started_at_utc"`
	Checks        map[string]string `json:"checks"`
	Measurements  map[string]int64  `json:"milliseconds,omitempty"`
	Identities    map[string]string `json:"identities,omitempty"`
	Events        []phaseEvent      `json:"events,omitempty"`
}

type phaseEvent struct {
	Phase   string    `json:"phase"`
	Process string    `json:"process,omitempty"`
	At      time.Time `json:"at_utc"`
	Status  int       `json:"HTTP_status,omitempty"`
}

func (r *receipt) event(phase, process string, status int) {
	r.Events = append(r.Events, phaseEvent{Phase: phase, Process: process, At: time.Now().UTC(), Status: status})
}

func newReceipt() *receipt {
	return &receipt{Schema: "hikyo.dev/mcp-651-fixture/v1", EvidenceClass: "local_development_rehearsal", Started: time.Now().UTC(), Checks: map[string]string{}, Measurements: map[string]int64{}, Identities: map[string]string{}}
}

type probe struct {
	client               *http.Client
	target               target
	fixture              fixture
	receipt              *receipt
	authenticationDenial response
	scopeDenial          response
}

type response struct {
	Status    int
	Body      []byte
	Challenge string
	Process   string
	Result    struct {
		IsError    bool            `json:"isError"`
		Structured json.RawMessage `json:"structuredContent"`
		Tools      []struct {
			Name string `json:"name"`
		} `json:"tools"`
	} `json:"result"`
	Error json.RawMessage `json:"error"`
}

func newProbe(t target, f fixture, r *receipt) (*probe, error) {
	u, err := url.Parse(t.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "/mcp" || u.RawQuery != "" || u.Fragment != "" || len(t.Processes) != 2 || t.Processes[0] == "" || t.Processes[0] == t.Processes[1] {
		return nil, errors.New("exact HTTPS /mcp target and two distinct observed processes required")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, errors.New("system trust unavailable")
	}
	if t.CAFile != "" {
		b, err := os.ReadFile(t.CAFile)
		if err != nil || !roots.AppendCertsFromPEM(b) {
			return nil, errors.New("explicit CA unavailable")
		}
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, MaxIdleConnsPerHost: 2}
	return &probe{client: &http.Client{Transport: tr, Timeout: 35 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, target: t, fixture: f, receipt: r}, nil
}

func (p *probe) call(ctx context.Context, method, name string, args map[string]any, token string) (response, error) {
	meta := map[string]any{"io.modelcontextprotocol/protocolVersion": mcpserver.ProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}, "io.modelcontextprotocol/clientInfo": map[string]string{"name": "hikyo-651-fixture", "version": "1"}}
	params := map[string]any{"_meta": meta}
	if method == "tools/call" {
		params["name"], params["arguments"] = name, args
	}
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return response{}, errors.New("request encoding failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.target.URL, bytes.NewReader(b))
	if err != nil {
		return response{}, errors.New("invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", mcpserver.ProtocolVersion)
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	h, err := p.client.Do(req)
	if err != nil {
		return response{}, errors.New("verified HTTPS request failed")
	}
	defer h.Body.Close()
	if h.Header.Get("Mcp-Session-Id") != "" {
		return response{}, errors.New("server allocated an MCP session")
	}
	body, err := io.ReadAll(io.LimitReader(h.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return response{}, errors.New("response exceeds bounds or is incomplete")
	}
	for _, canary := range p.fixture.Canaries {
		if canary != "" && bytes.Contains(body, []byte(canary)) {
			return response{}, errors.New("canary disclosed; body withheld")
		}
	}
	for _, credential := range p.fixture.Credentials {
		if credential.Token != "" && bytes.Contains(body, []byte(credential.Token)) {
			return response{}, errors.New("credential disclosed; body withheld")
		}
	}
	out := response{Status: h.StatusCode, Body: body, Challenge: h.Header.Get("WWW-Authenticate"), Process: h.Header.Get("X-Fixture-Process")}
	if out.Status == http.StatusOK {
		if json.Unmarshal(body, &out) != nil {
			return response{}, errors.New("invalid JSON reply")
		}
	}
	return out, nil
}

func (p *probe) args() map[string]any {
	return map[string]any{"org_id": p.fixture.Org, "project_id": p.fixture.Project, "page_size": 1}
}
func successful(r response) bool {
	return r.Status == 200 && !r.Result.IsError && len(r.Error) == 0 && len(r.Result.Structured) > 0
}
func sameDenial(a, b response) bool {
	return a.Status == b.Status && a.Challenge == b.Challenge && bytes.Equal(a.Body, b.Body)
}

func (p *probe) baseline(ctx context.Context) error {
	catalog, err := p.call(ctx, "tools/list", "", nil, "")
	if err != nil || catalog.Status != 200 || len(catalog.Error) > 0 || len(catalog.Result.Tools) != 5 {
		return errors.New("closed five-tool discovery failed")
	}
	want := map[string]bool{}
	for _, name := range mcpserver.ProductionToolNames() {
		want[name] = true
	}
	for _, tool := range catalog.Result.Tools {
		if !want[tool.Name] {
			return errors.New("unknown or duplicate tool")
		}
		delete(want, tool.Name)
	}
	seen := map[string]bool{}
	args := p.args()
	names := map[string]bool{}
	for page := 0; page < 3; page++ {
		r, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, args, p.fixture.Credentials["live"].Token)
		if err != nil || !successful(r) {
			return errors.New("nonempty definitions/cursor read failed")
		}
		if !contains(p.target.Processes, r.Process) {
			return errors.New("unbound process identity")
		}
		if page > 0 && seen[r.Process] && len(seen) == 1 {
			return errors.New("successive calls did not cross processes")
		}
		seen[r.Process] = true
		var output struct {
			Definitions []struct {
				Name string `json:"name"`
			} `json:"definitions"`
			Cursor string `json:"next_cursor"`
		}
		if json.Unmarshal(r.Result.Structured, &output) != nil || len(output.Definitions) != 1 {
			return errors.New("fixture definitions must be nonempty and paginated")
		}
		if names[output.Definitions[0].Name] {
			return errors.New("cursor repeated a definition")
		}
		names[output.Definitions[0].Name] = true
		if page < 2 && output.Cursor == "" {
			return errors.New("missing cursor")
		}
		if page == 2 && output.Cursor != "" {
			return errors.New("unexpected fixture definition")
		}
		args["cursor"] = output.Cursor
	}
	if len(seen) != 2 || !names["PUBLIC_CFG"] || !names["SECOND_CFG"] || !names["CANARY_SECRET"] {
		return errors.New("fixture scope or process coverage incomplete")
	}
	p.receipt.Checks["closed_catalog_nonempty_cross_process_cursor"] = "PASS"
	var denied response
	for i, token := range []string{"", "invalid-fixture-bearer"} {
		r, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), token)
		if err != nil || r.Status != 401 || r.Challenge != "Bearer" {
			return errors.New("uniform authentication refusal failed")
		}
		if i == 0 {
			denied = r
		} else if !sameDenial(denied, r) {
			return errors.New("authentication denial oracle differs")
		}
	}
	p.receipt.Checks["absent_invalid_uniform_denial"] = "PASS"
	p.authenticationDenial = denied
	// An authorized missing-object twin distinguishes scope denial from route
	// or credential failure. Compare only wire-visible oracle, not target IDs.
	wrong := map[string]any{"org_id": p.fixture.OtherOrg, "project_id": p.fixture.OtherProject}
	missing := map[string]any{"org_id": p.fixture.Org, "project_id": "prj_missing_651"}
	a, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, wrong, p.fixture.Credentials["live"].Token)
	if err != nil || !a.Result.IsError {
		return errors.New("wrong-scope positive refusal missing")
	}
	b, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, missing, p.fixture.Credentials["live"].Token)
	if err != nil || !sameDenial(a, b) {
		return errors.New("wrong-scope and missing-object denial differ")
	}
	u, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), p.fixture.Credentials["ungranted"].Token)
	if err != nil || !sameDenial(a, u) {
		return errors.New("ungranted denial differs")
	}
	p.receipt.Checks["scope_ungranted_missing_twin_canary"] = "PASS"
	p.scopeDenial = a
	return nil
}

func contains(items []string, s string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}

func probeConfigured(ctx context.Context, dir string) error {
	var f fixture
	var t target
	if err := readPrivate(filepath.Join(dir, "fixture.json"), &f); err != nil {
		return err
	}
	if err := readPrivate(filepath.Join(dir, "target.json"), &t); err != nil {
		return err
	}
	r := newReceipt()
	r.EvidenceClass = "endpoint_probe_only_unbound_candidate"
	p, err := newProbe(t, f, r)
	if err != nil {
		return err
	}
	defer p.client.CloseIdleConnections()
	if err := p.baseline(ctx); err != nil {
		return err
	}
	return writePrivate(filepath.Join(dir, "probe-receipt.json"), r)
}
