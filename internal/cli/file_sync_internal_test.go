package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/filesync"
)

func TestFileSyncCurrentReportsOnlyVerifiedDestinationStamp(t *testing.T) {
	for _, matched := range []bool{true, false} {
		t.Run(map[bool]string{true: "cursor-replaced-during-fetch", false: "unverified-cursor"}[matched], func(t *testing.T) {
			stateDir, destDir := filepath.Join(t.TempDir(), "state"), t.TempDir()
			keys, err := crypto.LoadOrCreateLocalKey(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			session := &fileSyncSession{cfg: &filesync.Config{Org: "org_one", Project: "prj_one", Environment: "env_one", Target: "ftg_one", Destination: filesync.DestinationSettings{Directory: destDir}, Files: []filesync.File{{Name: "app.env"}}}, policy: filesync.Policy{Mode: 0600, UID: -1, GID: -1}, keys: keys, stateDir: stateDir, token: "token"}
			files := []filesync.Rendered{{Name: "app.env", Content: []byte("A=1")}}
			stamp := filesync.GenerationStamp(keys, session.cfg.Target, session.policy, files)
			dest, err := filesync.OpenDestination(destDir, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := dest.Publish(t.Context(), filesync.Plan{Target: session.cfg.Target, Stamp: stamp, Policy: session.policy, Files: files}, nil); err != nil {
				t.Fatal(err)
			}
			if err := dest.Close(); err != nil {
				t.Fatal(err)
			}
			credential := credentialFingerprint(session.token)
			if !matched {
				credential = "different"
			}
			cursor := fileSyncCursor{Cursor: "cursor", Stamp: stamp, Credential: credential, ConfigDigest: session.configDigest()}
			if err := session.saveCursor(cursor); err != nil {
				t.Fatal(err)
			}
			reported := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == api.PathPrefix+"/meta" {
					_ = json.NewEncoder(w).Encode(apigen.Meta{ApiRevision: api.Revision})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/delivery") {
					want := ""
					if matched {
						want = "cursor"
					}
					if got := r.URL.Query().Get("cursor"); got != want {
						t.Errorf("cursor=%q, want %q", got, want)
					}
					cursor.Stamp = "v1-ffffffffffffffffffffffffffffffff"
					if err := session.saveCursor(cursor); err != nil {
						t.Error(err)
					}
					generation := int64(1)
					_ = json.NewEncoder(w).Encode(apigen.DeliveryResponse{Current: true, Revision: 1, FileTargetGeneration: &generation})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/report") {
					var report apigen.FileTargetReportRequest
					if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
						t.Error(err)
					}
					if matched {
						if report.Stamp == nil || *report.Stamp != stamp {
							t.Errorf("report lost verified stamp: %v", report.Stamp)
						}
					} else if report.Stamp != nil {
						t.Errorf("unverified stamp reported: %q", *report.Stamp)
					}
					reported = true
					w.WriteHeader(http.StatusNoContent)
					return
				}
				t.Errorf("unexpected request: %s", r.URL)
				http.NotFound(w, r)
			}))
			defer srv.Close()
			session.client = &Client{Entry: TrustEntry{Origin: srv.URL}, HTTP: srv.Client(), Bearer: "token"}
			outcome, err := session.pass(t.Context(), IO{Stdout: io.Discard, Stderr: io.Discard})
			if err != nil || outcome != fileSyncCurrent || !reported {
				t.Fatalf("outcome=%v report=%v err=%v", outcome, reported, err)
			}
		})
	}
}
