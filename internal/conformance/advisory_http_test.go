package conformance

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func init() {
	corpus = append(corpus, scenario{"advisory_http_nonstorage_and_lifecycle", scenarioAdvisoryHTTP})
}

// Run the actual HTTP route with real session resolution and per-event
// authorization on each engine, including cancellation and revoked reconnects.
func scenarioAdvisoryHTTP(t *testing.T, db *store.DB) {
	for _, kind := range []crypto.ArtifactType{crypto.ArtifactCLISession, crypto.ArtifactBrowserSession} {
		t.Run(string(kind), func(t *testing.T) {
			label := "advisoryhttp" + string(kind)
			who, scope, _, envs, _ := valueFixture(t, db, label)
			actor := service.LocalPrincipal(who)
			dev := mustEnv(t, envs, actor, scope, "dev")
			reader := newPrincipal(t, db, "usr_"+label+"_reader", []grantSpec{{"read", scope}})
			artifact, verifier, err := crypto.NewArtifact(kind)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			sessionType := "cli"
			if kind == crypto.ArtifactBrowserSession {
				sessionType = "browser"
			}
			if err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				generation, err := az.PrincipalGeneration(ctx, reader)
				if err != nil {
					return err
				}
				epoch, err := az.CredentialEpoch(ctx)
				if err != nil {
					return err
				}
				return az.MintSession(ctx, authz.NewSession{ID: "ses_" + label, PrincipalID: reader, Verifier: verifier, Artifact: sessionType, SessionGeneration: generation, CredentialEpoch: epoch, AuthMethod: "local-password", Factors: `["password"]`, AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour)})
			}); err != nil {
				t.Fatal(err)
			}
			envs.Advisory = service.NewAdvisory()
			auth := &service.Auth{DB: db, Keyring: sharedKeyring(t, db)}
			handler := server.New(&server.API{Auth: auth, Revisions: &service.Revisions{DB: db, Keyring: sharedKeyring(t, db), Advisory: envs.Advisory}}, nil)
			finished := make(chan struct{}, 3)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r); finished <- struct{}{} }))
			defer srv.Close()
			path := srv.URL + "/api/v1/orgs/" + string(scope.Org) + "/projects/" + string(scope.Project) + "/events"
			open := func(ctx context.Context) *http.Response {
				t.Helper()
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Accept", "text/event-stream; hikyo-topology=1")
				req.Header.Set("Last-Event-ID", "old-event")
				if kind == crypto.ArtifactBrowserSession {
					req.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: artifact})
				} else {
					req.Header.Set("Authorization", "Bearer "+artifact)
				}
				response, err := srv.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { response.Body.Close() })
				return response
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			first := open(ctx)
			if first.StatusCode != http.StatusOK {
				t.Fatalf("SSE status = %d", first.StatusCode)
			}
			if got := first.Header.Get("Cache-Control"); got != "no-store" {
				t.Errorf("SSE Cache-Control = %q, want no-store", got)
			}
			if first.Header.Get("Content-Type") != "text/event-stream" || first.Header.Get("X-Accel-Buffering") != "no" || first.Header.Get("Connection") != "keep-alive" {
				t.Fatalf("stream headers changed: %v", first.Header)
			}
			vary := strings.Join(first.Header.Values("Vary"), ",")
			for _, field := range []string{"Accept", "Origin", "Cookie", "Authorization"} {
				found := false
				for part := range strings.SplitSeq(vary, ",") {
					if strings.EqualFold(strings.TrimSpace(part), field) {
						found = true
					}
				}
				if !found {
					t.Errorf("Vary = %q, missing %s", vary, field)
				}
			}
			frames, closed := readAdvisoryFrames(first.Body)
			if preamble := awaitAdvisoryFrame(t, frames); !strings.HasPrefix(preamble, "retry: ") {
				t.Fatalf("missing retry: %q", preamble)
			}
			if _, err := envs.Rename(t.Context(), actor, dev, "before-cancel", nil); err != nil {
				t.Fatal(err)
			}
			frame := awaitAdvisoryFrame(t, frames)
			if !strings.HasPrefix(frame, "event: environments.changed\ndata: ") {
				t.Fatalf("unexpected event: %q", frame)
			}
			var metadata map[string]string
			if err := json.Unmarshal([]byte(strings.TrimPrefix(frame, "event: environments.changed\ndata: ")), &metadata); err != nil {
				t.Fatal(err)
			}
			if len(metadata) != 2 || metadata["project_id"] != string(scope.Project) || metadata["type"] != service.AdvisoryEnvironmentsChanged {
				t.Fatalf("unexpected disclosure: %v", metadata)
			}
			cancel()
			awaitAdvisoryReadEnd(t, closed, true)
			awaitAdvisoryClose(t, finished)
			// Reconnect does not replay the prior event. Its first frame is the retry
			// hint, then revocation closes it on the next real metadata notification.
			secondCtx, cancelSecond := context.WithCancel(t.Context())
			defer cancelSecond()
			second := open(secondCtx)
			frames, closed = readAdvisoryFrames(second.Body)
			if frame := awaitAdvisoryFrame(t, frames); !strings.HasPrefix(frame, "retry: ") {
				t.Fatalf("reconnect preamble = %q", frame)
			}
			if err := auth.Logout(t.Context(), artifact); err != nil {
				t.Fatal(err)
			}
			if _, err := envs.Rename(t.Context(), actor, dev, "after-revocation", nil); err != nil {
				t.Fatal(err)
			}
			awaitAdvisoryReadEnd(t, closed, false)
			awaitAdvisoryClose(t, finished)
			if frame, ok := <-frames; ok {
				t.Fatalf("revoked/reconnected stream disclosed %q", frame)
			}
			refusedCtx, cancelRefused := context.WithCancel(t.Context())
			defer cancelRefused()
			refused := open(refusedCtx)
			if refused.StatusCode != http.StatusUnauthorized || refused.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("revoked reconnect = %d %v", refused.StatusCode, refused.Header)
			}
		})
	}
}

func readAdvisoryFrames(body io.Reader) (<-chan string, <-chan error) {
	frames := make(chan string, 8)
	closed := make(chan error, 1)
	go func() {
		defer close(closed)
		defer close(frames)
		scanner := bufio.NewScanner(body)
		var lines []string
		for scanner.Scan() {
			if scanner.Text() == "" {
				frames <- strings.Join(lines, "\n")
				lines = nil
			} else {
				lines = append(lines, scanner.Text())
			}
		}
		closed <- scanner.Err()
	}()
	return frames, closed
}

func awaitAdvisoryFrame(t *testing.T, frames <-chan string) string {
	t.Helper()
	select {
	case frame, ok := <-frames:
		if !ok {
			t.Fatal("SSE closed before expected frame")
		}
		return frame
	case <-time.After(3 * time.Second):
		t.Fatal("SSE frame did not arrive")
		return ""
	}
}

func awaitAdvisoryClose(t *testing.T, closed <-chan struct{}) {
	t.Helper()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("SSE did not terminate")
	}
}

func awaitAdvisoryReadEnd(t *testing.T, closed <-chan error, canceled bool) {
	t.Helper()
	select {
	case err := <-closed:
		if err != nil && !(canceled && errors.Is(err, context.Canceled)) {
			t.Fatalf("SSE read ended with unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SSE reader did not terminate")
	}
}
