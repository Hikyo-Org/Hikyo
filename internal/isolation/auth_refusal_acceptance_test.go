package isolation

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// #1034 acceptance. These regressions drive the production routers, MCP
// transports, services and both datastores. They close the gaps the producer
// suites left: proxy-derived source attribution on the retained sample, the
// MCP artifact states, a canary scan of the whole datastore, and a concurrent
// flood that must stay bounded without locking out a legitimate credential.

// refusalMCPPublic mounts the production MCP transport behind the public
// router, with the same trusted proxies on both so attribution cannot diverge.
func refusalMCPPublic(t *testing.T, db *store.DB, auth *service.Auth, signals *service.RefusalSignals, proxies []*net.IPNet) http.Handler {
	t.Helper()
	kr := probeKeyring(t, db)
	sealer, err := kr.MCPCursorSealer()
	if err != nil {
		t.Fatal(err)
	}
	registry := mcpserver.NewRegistry()
	if err := mcpserver.RegisterProductionTools(registry, mcpserver.ProductionServices{
		Admission:     &service.MCPAdmission{DB: db},
		Definitions:   &service.Keys{DB: db, Keyring: kr},
		Environments:  &service.Environments{DB: db, Keyring: kr},
		Configuration: &service.Values{DB: db, Keyring: kr, Auth: auth},
		Pending:       &service.Revisions{DB: db, Keyring: kr, Auth: auth},
		Revisions:     &service.Revisions{DB: db, Keyring: kr, Auth: auth},
	}); err != nil {
		t.Fatal(err)
	}
	options := mcpserver.Options{Registry: registry, ExternalOrigin: "https://hikyo.example.com", CursorSealer: sealer, TrustedProxies: proxies}
	transport, err := mcpserver.New(options)
	if err != nil {
		t.Fatal(err)
	}
	return server.NewPublic(&server.API{Auth: auth, Refusals: signals, TrustedProxies: proxies}, nil, server.PublicOptions{MCP: transport})
}

// withPeer fixes the socket peer and forwarding field lines of every request.
func withPeer(next http.Handler, peer string, forwarded []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = peer
		// A proxy-fronted MCP transport also requires its forwarded host and scheme.
		r.Header.Set("X-Forwarded-Host", "hikyo.example.com")
		r.Header.Set("X-Forwarded-Proto", "https")
		if forwarded != nil {
			r.Header["X-Forwarded-For"] = forwarded
		}
		next.ServeHTTP(w, r)
	})
}

// retainedRefusalSample finds a request's sample in the sealed instance trail.
func retainedRefusalSample(t *testing.T, db *store.DB, requestID string) (audit.RefusalSample, bool) {
	t.Helper()
	page, err := (&service.Audits{DB: db}).InstanceQuery(t.Context(), root, store.AuditFilter{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range page.Events {
		if event.Type != audit.EventRequestRefused {
			continue
		}
		var p struct {
			Stage   audit.RefusalStage    `json:"stage"`
			Samples []audit.RefusalSample `json:"samples"`
		}
		if err := json.Unmarshal([]byte(event.RawPayload), &p); err != nil {
			t.Fatal(err)
		}
		for _, sample := range p.Samples {
			if sample.RequestID == requestID {
				// The sealed event owns the stage; its samples carry only the rest.
				sample.Stage = p.Stage
				return sample, true
			}
		}
	}
	return audit.RefusalSample{}, false
}

// sealAndFindRefusal seals the pending window, then requires the request's
// retained sample to carry the expected stage and trustworthy source.
func sealAndFindRefusal(t *testing.T, db *store.DB, signals *service.RefusalSignals, label string, rec *httptest.ResponseRecorder, stage audit.RefusalStage, source string) {
	t.Helper()
	id := rec.Header().Get(server.RefusalRequestIDHeader)
	if parsed, err := uuid.Parse(id); err != nil || parsed.Version() != 7 {
		t.Fatalf("%s: no server-generated correlation id: %q", label, id)
	}
	expireRefusalWindow(t, db)
	sealRefusals(t, signals)
	sample, ok := retainedRefusalSample(t, db, id)
	if !ok {
		t.Fatalf("%s: refusal missing from the supported instance reader", label)
	}
	if sample.Stage != stage || sample.SourceIP != source {
		t.Fatalf("%s: retained sample %+v, want stage %s from %s", label, sample, stage, source)
	}
}

func TestRequestRefusalAttributesProxyClientAcrossTransports(t *testing.T) {
	_, proxy, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	proxies := []*net.IPNet{proxy}
	cases := []struct {
		name      string
		peer      string
		forwarded []string
		want      string
	}{
		{"trusted proxy names the client", "10.1.2.3:4000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"forged first line, proxy appends its own", "10.1.2.3:4000", []string{"203.0.113.9", "198.51.100.7, 10.9.9.9"}, "198.51.100.7"},
		{"forged leftmost entry in one list", "10.1.2.3:4000", []string{"203.0.113.9, 198.51.100.7, 10.4.4.4"}, "198.51.100.7"},
		{"IPv6 client", "10.1.2.3:4000", []string{"2001:db8::7"}, "2001:db8::7"},
		{"malformed hop ends the walk at the proxy", "10.1.2.3:4000", []string{"198.51.100.7, not-an-ip"}, "10.1.2.3"},
		{"IPv4-mapped IPv6 client", "10.1.2.3:4000", []string{"::ffff:198.51.100.7"}, "198.51.100.7"},
		{"port-suffixed hop ends the walk at the proxy", "10.1.2.3:4000", []string{"198.51.100.7:4711"}, "10.1.2.3"},
		{"no forwarded header", "10.1.2.3:4000", nil, "10.1.2.3"},
		{"untrusted peer cannot choose its source", "192.0.2.50:4000", []string{"198.51.100.7"}, "192.0.2.50"},
	}
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth := authService(t, db)
		signals := service.NewRefusalSignals(db)
		rest := server.New(&server.API{Auth: auth, Refusals: signals, TrustedProxies: proxies}, nil)
		public := refusalMCPPublic(t, db, auth, signals, proxies)
		cli, _, err := crypto.NewArtifact(crypto.ArtifactCLISession)
		if err != nil {
			t.Fatal(err)
		}
		bearer, _, err := crypto.NewArtifact(crypto.ArtifactAutomation)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range cases {
			t.Run("rest/"+tc.name, func(t *testing.T) {
				rec := confirmedRefusal(t, signals, func() *httptest.ResponseRecorder {
					r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, api.PathPrefix+"/auth/whoami", nil)
					r.Header.Set("Authorization", "Bearer "+cli)
					rec := httptest.NewRecorder()
					withPeer(rest, tc.peer, tc.forwarded).ServeHTTP(rec, r)
					return rec
				})
				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("status %d", rec.Code)
				}
				sealAndFindRefusal(t, db, signals, "rest", rec, audit.RefusalArtifact, tc.want)
			})
			for _, codex := range []bool{false, true} {
				name := "mcp/" + tc.name
				if codex {
					name = "codex/" + tc.name
				}
				t.Run(name, func(t *testing.T) {
					rec := confirmedRefusal(t, signals, func() *httptest.ResponseRecorder {
						return mcpCall(t, mcpProfile(withPeer(public, tc.peer, tc.forwarded), codex), bearer, mcpserver.ToolListDefinitions, `{"org_id":"org_a","project_id":"prj_a1"}`)
					})
					if rec.Code != http.StatusUnauthorized {
						t.Fatalf("status %d", rec.Code)
					}
					sealAndFindRefusal(t, db, signals, "mcp", rec, audit.RefusalArtifact, tc.want)
				})
			}
		}
	})
}

func TestRequestRefusalMCPArtifactStatesAreSignalledUniformly(t *testing.T) {
	for _, codex := range []bool{false, true} {
		name := "modern"
		if codex {
			name = "codex"
		}
		t.Run(name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES (`+
					`'g_cu_mid', 'usr_custodian', 'manage-identities', 'org_a', NULL, NULL, `+ts+`)`)
				auth := authService(t, db)
				signals := service.NewRefusalSignals(db)
				public := refusalMCPPublic(t, db, auth, signals, nil)
				scope := scopeProject(orgA, prjA1)
				live := mintMCPAutomation(t, db, auth, scope, "refusal-live", true)
				revoked := mintMCPAutomation(t, db, auth, scope, "refusal-revoked", true)
				if err := (&service.Identities{DB: db, Auth: auth}).RevokeCredential(t.Context(), service.LocalPrincipal(custodian), scope, revoked.AccountID, revoked.CredentialID); err != nil {
					t.Fatal(err)
				}
				expired := mintExpiredMCPAutomation(t, db, auth, scope, "refusal-expired")
				stale := mintMCPAutomation(t, db, auth, scope, "refusal-stale", true)
				unknown, _, err := crypto.NewArtifact(crypto.ArtifactAutomation)
				if err != nil {
					t.Fatal(err)
				}
				const args = `{"org_id":"org_a","project_id":"prj_a1"}`
				call := func(token string) *httptest.ResponseRecorder {
					return mcpCall(t, mcpProfile(public, codex), token, mcpserver.ToolListDefinitions, args)
				}
				_, total := refusalWireAccounting(t, db)

				// Negative controls: a live credential and, on the modern profile
				// (Codex has no anonymous discovery), anonymous discovery are not
				// refusals, even when discovery carries a dead bearer.
				if rec := call(live.token); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), mcpserver.SafeOperationError) {
					t.Fatalf("live positive control: %d %s", rec.Code, rec.Body)
				}
				if !codex {
					if rec := mcpRequest(t, public, revoked.token, "tools/list", "", "", mcpserver.ProtocolVersion); rec.Code != http.StatusOK {
						t.Fatalf("anonymous discovery with a dead bearer: %d %s", rec.Code, rec.Body)
					}
				}
				if _, now := refusalWireAccounting(t, db); now != total {
					t.Fatalf("a live call or anonymous discovery staged %d refusals", now-total)
				}

				reference := call(unknown)
				if reference.Code != http.StatusUnauthorized {
					t.Fatalf("unknown bearer: %d %s", reference.Code, reference.Body)
				}
				refuse := func(label, token string) {
					t.Helper()
					_, before := refusalWireAccounting(t, db)
					attempts := int64(0)
					rec := confirmedRefusal(t, signals, func() *httptest.ResponseRecorder { attempts++; return call(token) })
					if rec.Code != reference.Code || rec.Body.String() != reference.Body.String() ||
						rec.Header().Get("WWW-Authenticate") != reference.Header().Get("WWW-Authenticate") {
						t.Fatalf("%s: wire differs from the unknown bearer: %d %q", label, rec.Code, rec.Body)
					}
					if _, after := refusalWireAccounting(t, db); after-before < 1 || after-before > attempts {
						t.Fatalf("%s: staged %d refusals over %d attempts", label, after-before, attempts)
					}
					sealAndFindRefusal(t, db, signals, label, rec, audit.RefusalArtifact, "192.0.2.1")
				}
				for _, state := range []struct{ name, token string }{
					{"missing", ""}, {"malformed", "x"}, {"unknown", unknown}, {"expired", expired.token}, {"revoked", revoked.token},
				} {
					refuse(state.name, state.token)
				}
				// Raising the instance credential epoch stales every outstanding
				// machine credential, including ones that were live a moment ago.
				execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch = credential_epoch + 1 WHERE id=1")
				refuse("epoch-stale", stale.token)
				refuse("epoch-stale (previously live)", live.token)
			})
		})
	}
}

// datastoreText renders every row of the named tables, or of every table, as
// text plus the hex of any binary column, so a scan sees both encodings.
func datastoreText(t *testing.T, db *store.DB, only ...string) string {
	t.Helper()
	var out strings.Builder
	var tables []string
	if db.Engine() == store.EnginePostgres {
		rows, err := db.PG().Query(t.Context(), `SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE' ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			tables = append(tables, name)
		}
		rows.Close()
		if rows.Err() != nil {
			t.Fatal(rows.Err())
		}
	} else {
		rows, err := db.SQLiteRead().QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			tables = append(tables, name)
		}
		_ = rows.Close()
		if rows.Err() != nil {
			t.Fatal(rows.Err())
		}
	}
	for _, table := range tables {
		if len(only) > 0 && !slices.Contains(only, table) {
			continue
		}
		fmt.Fprintf(&out, "\n--%s\n", table)
		if db.Engine() == store.EnginePostgres {
			rows, err := db.PG().Query(t.Context(), `SELECT t::text FROM "`+table+`" AS t ORDER BY 1`)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var row string
				if err := rows.Scan(&row); err != nil {
					t.Fatal(err)
				}
				out.WriteString(row + "\n")
			}
			rows.Close()
			if rows.Err() != nil {
				t.Fatal(rows.Err())
			}
			continue
		}
		rows, err := db.SQLiteRead().QueryContext(t.Context(), `SELECT * FROM "`+table+`"`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			for _, v := range values {
				if b, ok := v.([]byte); ok {
					out.Write(b)
					out.WriteString(" " + hex.EncodeToString(b) + " ")
					continue
				}
				fmt.Fprint(&out, v, " ")
			}
			out.WriteString("\n")
		}
		_ = rows.Close()
		if rows.Err() != nil {
			t.Fatal(rows.Err())
		}
	}
	return out.String()
}

func TestRequestRefusalCapturesNoCredentialOrRequestDataAnywhere(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		auth := authService(t, db)
		metrics := server.NewMetrics(nil)
		signals := service.NewRefusalSignals(db)
		metrics.SetRefusalSource(signals)
		var logs bytes.Buffer
		limiter := preauthLimiter(t)
		const exhausted = "192.0.2.150"
		for range admission.MetaPerIPPerMinute {
			if err := limiter.AdmitDiscovery(exhausted); err != nil {
				t.Fatal(err)
			}
		}
		kr := probeKeyring(t, db)
		sealer, err := kr.MCPCursorSealer()
		if err != nil {
			t.Fatal(err)
		}
		registry := mcpserver.NewRegistry()
		if err := mcpserver.RegisterProductionTools(registry, mcpserver.ProductionServices{
			Admission: &service.MCPAdmission{DB: db}, Definitions: &service.Keys{DB: db, Keyring: kr},
			Environments: &service.Environments{DB: db, Keyring: kr}, Configuration: &service.Values{DB: db, Keyring: kr, Auth: auth},
			Pending: &service.Revisions{DB: db, Keyring: kr, Auth: auth}, Revisions: &service.Revisions{DB: db, Keyring: kr, Auth: auth},
		}); err != nil {
			t.Fatal(err)
		}
		transport, err := mcpserver.New(mcpserver.Options{Registry: registry, ExternalOrigin: "https://hikyo.example.com", CursorSealer: sealer, Admission: limiter})
		if err != nil {
			t.Fatal(err)
		}
		handler := server.NewPublic(&server.API{
			Auth: auth, Refusals: signals, Admission: limiter, Metrics: metrics,
			Settings: &service.ProjectSettings{DB: db, Auth: auth},
			Log:      slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		}, nil, server.PublicOptions{MCP: transport})

		const secret = "REFUSAL-CANARY-REQUEST-DATA"
		var wire bytes.Buffer
		var canaries, unknownTokens []string
		canaries = append(canaries, secret, "REFUSAL-CANARY-USERNAME", "REFUSAL-CANARY-PASSWORD")
		serve := func(r *http.Request) *httptest.ResponseRecorder {
			// Free-text user agents are a bounded, sanitized envelope field of
			// dedicated events (an unknown developer artifact and a failed
			// login here). A client that pastes its credential there must
			// still not retain it, so those requests do exactly that.
			if r.Header.Get("User-Agent") == "" {
				r.Header.Set("User-Agent", "refusal-agent")
			}
			r.Header.Set("X-Request-ID", secret)
			r.Header.Set("Referer", "https://example.test/"+secret)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			fmt.Fprint(&wire, w.Header(), w.Body.String())
			return w
		}
		rest := func(method, path, source string, edit func(*http.Request)) {
			r := httptest.NewRequestWithContext(t.Context(), method, path, nil)
			r.RemoteAddr = source + ":1234"
			if edit != nil {
				edit(r)
			}
			serve(r)
		}
		const settings = api.PathPrefix + "/orgs/org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fee/projects/prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fdd/environments/env_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fcc/settings"
		// Every artifact class, unknown, through the ordinary and admission stages.
		for _, kind := range []crypto.ArtifactType{
			crypto.ArtifactCLISession, crypto.ArtifactBrowserSession, crypto.ArtifactWorkload, crypto.ArtifactAutomation,
			crypto.ArtifactInstanceConn, crypto.ArtifactSCIM, crypto.ArtifactDeveloper,
		} {
			token, _, err := crypto.NewArtifact(kind)
			if err != nil {
				t.Fatal(err)
			}
			canaries, unknownTokens = append(canaries, token), append(unknownTokens, token)
			for _, source := range []string{"192.0.2.151", exhausted} {
				rest(http.MethodGet, settings+"?secret="+secret, source, func(r *http.Request) {
					r.Header.Set("User-Agent", "agent/"+token)
					if kind == crypto.ArtifactBrowserSession {
						r.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: token})
					} else {
						r.Header.Set("Authorization", "Bearer "+token)
					}
				})
			}
		}
		// Dead sessions, a conflicting pair, disallowed proof and CSRF mismatch.
		for _, artifact := range []crypto.ArtifactType{crypto.ArtifactCLISession, crypto.ArtifactBrowserSession} {
			cookie := artifact == crypto.ArtifactBrowserSession
			for _, state := range []string{"expired", "revoked", "live"} {
				token := refusalSession(t, db, artifact, state)
				canaries = append(canaries, token)
				rest(http.MethodGet, api.PathPrefix+"/auth/whoami", "192.0.2.152", func(r *http.Request) {
					r.Header.Set("User-Agent", "agent/"+token)
					if cookie {
						r.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: token})
					} else {
						r.Header.Set("Authorization", "Bearer "+token)
					}
				})
				if state != "live" {
					continue
				}
				rest(http.MethodGet, api.PathPrefix+"/auth/whoami", "192.0.2.152", func(r *http.Request) {
					r.Header.Set("Authorization", "Bearer "+token)
					r.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: token})
				})
				if cookie {
					rest(http.MethodPost, api.PathPrefix+"/auth/logout", "192.0.2.152", func(r *http.Request) {
						r.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: token})
						r.Header.Set("X-Hikyo-CSRF", secret)
					})
				}
			}
		}
		rest(http.MethodGet, api.PathPrefix+"/auth/whoami", "192.0.2.152", func(r *http.Request) {
			r.Header.Set("Authorization", "DPoP "+secret)
			r.Header.Set("DPoP", secret)
		})
		// A failed password login keeps its dedicated event and carries no prose.
		loginAgent, _, err := crypto.NewArtifact(crypto.ArtifactCLISession)
		if err != nil {
			t.Fatal(err)
		}
		canaries = append(canaries, loginAgent)
		rest(http.MethodPost, api.PathPrefix+"/auth/local/login", "192.0.2.152", func(r *http.Request) {
			r.Header.Set("User-Agent", "agent/"+loginAgent)
			r.Body = io.NopCloser(strings.NewReader(`{"username":"REFUSAL-CANARY-USERNAME","password":"REFUSAL-CANARY-PASSWORD"}`))
			r.Header.Set("Content-Type", "application/json")
		})
		// Unmatched paths and queries are not refusals, but their secrets must
		// stay out of RED telemetry and logs as well.
		for i := range 8 {
			rest(http.MethodGet, fmt.Sprintf("%s/%s-%d?secret=%s", api.PathPrefix, secret, i, secret), "192.0.2.153", nil)
		}
		// MCP, with request data in headers and tool arguments.
		mcpToken, _, err := crypto.NewArtifact(crypto.ArtifactAutomation)
		if err != nil {
			t.Fatal(err)
		}
		canaries, unknownTokens = append(canaries, mcpToken), append(unknownTokens, mcpToken)
		_, beforeMCP := refusalWireAccounting(t, db)
		carrying := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-Request-ID", secret)
			r.Header.Set("Referer", "https://example.test/"+secret)
			r.Header.Set("User-Agent", "agent/"+mcpToken)
			handler.ServeHTTP(w, r)
		})
		for _, codex := range []bool{false, true} {
			rec := mcpCall(t, mcpProfile(withPeer(carrying, "192.0.2.154:1234", nil), codex), mcpToken, mcpserver.ToolListDefinitions, `{"org_id":"`+secret+`","project_id":"prj_a1"}`)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("MCP canary request was not an artifact refusal: %d %s", rec.Code, rec.Body)
			}
			fmt.Fprint(&wire, rec.Header(), rec.Body.String())
		}
		// The scan only proves something if the MCP requests reached the signal.
		if _, afterMCP := refusalWireAccounting(t, db); afterMCP-beforeMCP < 1 {
			t.Fatal("MCP canary requests staged no refusal")
		}

		expireRefusalWindow(t, db)
		sealRefusals(t, signals)
		if sealed := sealedRefusalCount(t, db); sealed < 10 {
			t.Fatalf("scenario sealed only %d refusals", sealed)
		}
		op := server.NewOperational(nil, mcpOperationalHealth{}, metrics)
		scrape := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
		scrape.RemoteAddr = "127.0.0.1:12345"
		rec := httptest.NewRecorder()
		op.ServeHTTP(rec, scrape)
		if rec.Code != http.StatusOK {
			t.Fatalf("metrics: %d", rec.Code)
		}
		var captured bytes.Buffer
		captured.Write(wire.Bytes())
		captured.Write(logs.Bytes())
		captured.Write(rec.Body.Bytes())
		audits := &service.Audits{DB: db}
		if err := audits.InstanceExport(t.Context(), root, store.AuditFilter{}, 1000, &captured); err != nil {
			t.Fatal(err)
		}
		if err := audits.Export(t.Context(), root, domain.Scope{Org: orgA}, store.AuditFilter{}, 1000, &captured); err != nil {
			t.Fatal(err)
		}
		stored := datastoreText(t, db)
		// The scan must be able to see: sealed summaries and a live verifier
		// are present, so an empty haystack cannot pass.
		live := refusalSession(t, db, crypto.ArtifactCLISession, "live")
		if !strings.Contains(datastoreText(t, db), hex.EncodeToString(crypto.ArtifactVerifier(live))) || !strings.Contains(stored, string(audit.EventRequestRefused)) {
			t.Fatal("datastore scan cannot see stored verifiers or sealed refusal summaries")
		}
		for _, c := range canaries {
			for surface, haystack := range map[string]string{"wire/log/metrics/export": captured.String(), "datastore": stored} {
				if strings.Contains(haystack, c) || strings.Contains(haystack, hex.EncodeToString([]byte(c))) {
					i := strings.Index(haystack, c)
					t.Fatalf("canary %q leaked into %s: ...%s...", c, surface, haystack[max(i-80, 0):min(i+120, len(haystack))])
				}
			}
		}
		// An unknown artifact leaves nothing behind, not even its verifier.
		for _, token := range unknownTokens {
			if strings.Contains(stored, hex.EncodeToString(crypto.ArtifactVerifier(token))) {
				t.Fatalf("verifier of an unknown artifact was stored")
			}
		}
	})
}

func TestRequestRefusalConcurrentFloodIsBoundedAndNeverLocksOut(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		auth := authService(t, db)
		signals := service.NewRefusalSignals(db)
		handler := server.New(&server.API{Auth: auth, Refusals: signals}, nil)
		victim := refusalSession(t, db, crypto.ArtifactCLISession, "live")
		credentialTables := []string{"principals", "accounts", "password_credentials", "sessions", "auth_instance_state", "grants"}
		identityBefore := datastoreText(t, db, credentialTables...)
		_, totalBefore := refusalWireAccounting(t, db)
		baseline := runtime.NumGoroutine()

		// Credentials are minted here so a failure fails the test, not a goroutine.
		mint := func(artifact crypto.ArtifactType) string {
			token, _, err := crypto.NewArtifact(artifact)
			if err != nil {
				t.Fatal(err)
			}
			return token
		}
		flood := func(count int) (codes map[int]int, bodies map[int]map[string]int, peakInFlight, peakWaiting int) {
			t.Helper()
			cli, browser := make([]string, count), make([]string, count)
			for i := range count {
				cli[i], browser[i] = mint(crypto.ArtifactCLISession), mint(crypto.ArtifactBrowserSession)
			}
			var wg sync.WaitGroup
			var mu sync.Mutex
			codes, bodies = map[int]int{}, map[int]map[string]int{}
			stop := make(chan struct{})
			sampled := make(chan struct{})
			go func() {
				defer close(sampled)
				for {
					health := signals.RefusalHealth()
					peakInFlight, peakWaiting = max(peakInFlight, health.InFlight), max(peakWaiting, health.Waiting)
					select {
					case <-stop:
						return
					case <-time.After(time.Millisecond):
					}
				}
			}()
			for i := range count {
				wg.Add(1)
				go func() {
					defer wg.Done()
					r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, api.PathPrefix+"/auth/whoami", nil)
					r.RemoteAddr = fmt.Sprintf("198.51.100.%d:1234", i%250+1)
					switch i % 4 {
					case 0:
						r.Header.Set("Authorization", "Bearer "+cli[i])
					case 1:
						r.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: browser[i]})
					case 2:
						r.Header.Set("Authorization", "DPoP flood")
						r.Header.Set("DPoP", "flood")
					}
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, r)
					mu.Lock()
					codes[w.Code]++
					if bodies[w.Code] == nil {
						bodies[w.Code] = map[string]int{}
					}
					bodies[w.Code][w.Body.String()]++
					mu.Unlock()
				}()
			}
			wg.Wait()
			close(stop)
			<-sampled
			return codes, bodies, peakInFlight, peakWaiting
		}

		// The sink is blocked for the whole flood: every refusal keeps its
		// uniform response, every unconfirmed accounting is visible, nothing
		// commits late, and no work is retained.
		failuresBefore := signals.RefusalHealth().Failures
		release := holdRefusalWriter(t, db)
		codes, bodies, peakInFlight, peakWaiting := flood(400)
		release()
		if codes[http.StatusUnauthorized] != 400 || len(bodies[http.StatusUnauthorized]) != 1 {
			t.Fatalf("blocked sink changed the wire: %v, bodies %v", codes, bodies)
		}
		// The held writer must actually have saturated the bounded work queue.
		if peakInFlight != 8 || peakWaiting == 0 {
			t.Fatalf("blocked sink never exercised the bounds: peak in flight %d, waiting %d", peakInFlight, peakWaiting)
		}
		health := signals.RefusalHealth()
		if health.Healthy || health.Failures-failuresBefore != 400 || health.InFlight != 0 || health.Waiting != 0 {
			t.Fatalf("blocked sink hid or leaked accounting: %+v", health)
		}
		if _, total := refusalWireAccounting(t, db); total != totalBefore {
			t.Fatalf("timed-out accounting committed %d late counts", total-totalBefore)
		}

		// A healthy sink under a larger flood: bounded samples and state, a
		// bounded number of sealed events, and the sink recovers.
		accepted := signals.RefusalHealth().Failures
		codes, bodies, _, _ = flood(800)
		// Past the public in-flight cap a refusal is the uniform 429, which the
		// admission stage also accounts; the 401 body must stay a single value.
		if codes[http.StatusUnauthorized]+codes[http.StatusTooManyRequests] != 800 || codes[http.StatusUnauthorized] == 0 ||
			len(bodies[http.StatusUnauthorized]) != 1 || len(bodies[http.StatusTooManyRequests]) > 1 {
			t.Fatalf("flood changed the wire: %v, bodies %v", codes, bodies)
		}
		unconfirmed := int64(signals.RefusalHealth().Failures - accepted)
		_, total := refusalWireAccounting(t, db)
		if got := total - totalBefore; got < 800-unconfirmed || got > 800 {
			t.Fatalf("flood accounted %d of 800 with %d unconfirmed", got, unconfirmed)
		}
		if raw := queryString(t, db, "SELECT state FROM auth_refusal_window WHERE id=1"); len(raw) > audit.RefusalStateBytes {
			t.Fatalf("pending state grew to %d bytes", len(raw))
		}
		if queryInt(t, db, "SELECT window_start FROM auth_refusal_window WHERE id=1") != 0 {
			expireRefusalWindow(t, db)
		}
		sealRefusals(t, signals)
		// At most one event per stage per sealed window, and a flood spans at most two.
		if events := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.request_refused'"); events > 2*int64(len(audit.RefusalStages())) {
			t.Fatalf("flood sealed %d summary events", events)
		}
		if h := signals.RefusalHealth(); !h.Healthy || h.PendingSince != 0 {
			t.Fatalf("sink did not recover after the flood: %+v", h)
		}

		// No lockout: refusals never touch credential state, and the victim's
		// own credential still works from another source.
		if after := datastoreText(t, db, credentialTables...); after != identityBefore {
			t.Fatal("a refusal flood changed credential, grant or session state")
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, api.PathPrefix+"/auth/whoami", nil)
		r.RemoteAddr = "203.0.113.200:1234"
		r.Header.Set("Authorization", "Bearer "+victim)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("victim credential refused after the flood: %d %s", w.Code, w.Body)
		}
		deadline := time.Now().Add(3 * time.Second)
		for runtime.NumGoroutine() > baseline+4 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if got := runtime.NumGoroutine(); got > baseline+4 {
			t.Fatalf("flood left %d goroutines over a baseline of %d", got, baseline)
		}
	})
}

// A client that disconnects as its refusal is answered cancels the request
// context. The refusal must still be recorded: the signal belongs to the
// server's accounting, not to the caller who may already be gone.
func TestRequestRefusalSurvivesCancelledRequestContext(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		signals := service.NewRefusalSignals(db)
		cancelled, cancel := context.WithCancel(t.Context())
		cancel()
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		_, before := refusalWireAccounting(t, db)
		sample := audit.RefusalSample{Stage: audit.RefusalArtifact, SourceIP: "192.0.2.7", RequestID: id.String(), Operation: "whoami"}
		if err := signals.Record(cancelled, sample); err != nil {
			t.Fatalf("cancelled context lost the refusal: %v", err)
		}
		if _, after := refusalWireAccounting(t, db); after != before+1 {
			t.Fatalf("service record under a cancelled context staged %d refusals, want 1", after-before)
		}
		handler := server.New(&server.API{Auth: authService(t, db), Refusals: signals}, nil)
		r := httptest.NewRequestWithContext(cancelled, http.MethodGet, api.PathPrefix+"/auth/whoami", nil)
		r.RemoteAddr = "192.0.2.8:1234"
		// A proof-scheme refusal is decided before any datastore read, so the
		// cancelled context reaches only the refusal accounting.
		r.Header.Set("Authorization", "DPoP canary")
		r.Header.Set("DPoP", "canary")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		if _, after := refusalWireAccounting(t, db); after != before+2 || !signals.RefusalHealth().Healthy {
			t.Fatalf("handler refusal under a cancelled request staged %d, health %+v", after-before-1, signals.RefusalHealth())
		}
	})
}
