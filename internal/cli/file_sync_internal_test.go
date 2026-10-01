package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/compose"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/filesync"
)

func TestFileSyncOfflinePreservesReceiptOnlyForActuallyWrittenKeys(t *testing.T) {
	stateDir, destDir := filepath.Join(t.TempDir(), "state"), t.TempDir()
	keys, err := crypto.LoadOrCreateLocalKey(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := filesync.ParseConfig([]byte(fmt.Sprintf("version: 1\ninstance: https://hikyo.example\norg: org_11111111-1111-1111-1111-111111111111\nproject: prj_11111111-1111-1111-1111-111111111111\nenvironment: env_11111111-1111-1111-1111-111111111111\ntarget: ftg_11111111-1111-1111-1111-111111111111\ndestination:\n  directory: %q\nsnapshot:\n  offline_serve: true\nfiles:\n  - name: app.env\n    format: dotenv\n    keys: [TOKEN]\n", destDir)))
	if err != nil {
		t.Fatal(err)
	}
	session := &fileSyncSession{cfg: cfg, policy: filesync.Policy{Mode: 0600, UID: -1, GID: -1}, keys: keys, stateDir: stateDir, origin: cfg.Instance, token: "token"}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	binding, err := session.snapshotBinding()
	if err != nil {
		t.Fatal(err)
	}
	binding, err = binding.WithDelivery(crypto.SnapshotBindingDelivery{CredentialID: "cred_one", PinnedRevision: 1, ChangeToken: "stamp", Projection: []string{"read", "reveal"}, IssuedAt: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := compose.SaveSnapshot(keys, binding, compose.SnapshotPayload{Rows: []compose.SnapshotRow{
		{Name: "TOKEN", KeyID: "key_selected", Classification: "secret", Value: "selected", Receipt: "receipt-selected"},
		{Name: "UNSELECTED", KeyID: "key_other", Classification: "config", Value: "other", Receipt: "receipt-other"},
	}}); err != nil {
		t.Fatal(err)
	}
	dest, err := filesync.OpenDestination(destDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer dest.Close()
	ios := IO{Stdout: io.Discard, Stderr: io.Discard, Now: func() time.Time { return now }}
	outcome, err := session.offline(t.Context(), ios, dest, failf(ExitUnavailable, "offline"))
	if err != nil || outcome != fileSyncOffline {
		t.Fatalf("offline file sync failed: outcome=%v err=%v", outcome, err)
	}
	records, _, err := compose.Pending(stateDir)
	if err != nil || len(records) != 1 || records[0].KeyID != "key_selected" || records[0].SnapshotReceipt != "receipt-selected" {
		t.Fatalf("offline receipt set differs from output: records=%+v err=%v", records, err)
	}
	content, err := os.ReadFile(filepath.Join(destDir, "app.env"))
	if err != nil || string(content) != "TOKEN=selected\n" {
		t.Fatalf("offline output=%q err=%v", content, err)
	}
}

func TestFileSyncDoctorInspectsAuthenticatedHashedSnapshots(t *testing.T) {
	for _, status := range []string{"fresh", "expired", "missing"} {
		t.Run(status, func(t *testing.T) {
			stateDir := filepath.Join(t.TempDir(), "state")
			keys, err := crypto.LoadOrCreateLocalKey(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := filesync.ParseConfig([]byte(fmt.Sprintf("version: 1\ninstance: https://hikyo.example\norg: org_11111111-1111-1111-1111-111111111111\nproject: prj_11111111-1111-1111-1111-111111111111\nenvironment: env_11111111-1111-1111-1111-111111111111\ntarget: ftg_11111111-1111-1111-1111-111111111111\ndestination:\n  directory: %q\nsnapshot:\n  offline_serve: true\nfiles:\n  - name: app.env\n    format: dotenv\n    keys: [TOKEN]\n", t.TempDir())))
			if err != nil {
				t.Fatal(err)
			}
			session := &fileSyncSession{cfg: cfg, policy: filesync.Policy{Mode: 0600, UID: -1, GID: -1}, keys: keys, stateDir: stateDir, origin: cfg.Instance, token: "token"}
			if err := session.saveCursor(fileSyncCursor{Credential: credentialFingerprint(session.token), AppliedAt: "2026-10-01T00:00:00Z"}); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			if status != "missing" {
				issued := now.Add(-time.Hour)
				if status == "expired" {
					issued = now.Add(-8 * 24 * time.Hour)
				}
				binding, err := session.snapshotBinding()
				if err != nil {
					t.Fatal(err)
				}
				binding, err = binding.WithDelivery(crypto.SnapshotBindingDelivery{CredentialID: "cred_one", PinnedRevision: 1, ChangeToken: "stamp", Projection: []string{"read"}, IssuedAt: issued.Format(time.RFC3339), ExpiresAt: issued.Add(7 * 24 * time.Hour).Format(time.RFC3339)})
				if err != nil {
					t.Fatal(err)
				}
				if err := compose.SaveSnapshot(keys, binding, compose.SnapshotPayload{Rows: []compose.SnapshotRow{{Receipt: "server-receipt", Name: "TOKEN", Value: "doctor-private-canary"}}}); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			if err := fileSyncDoctor(IO{Stdout: &out, Stderr: io.Discard, Now: func() time.Time { return now }}, FormatJSON, cfg, session.policy, stateDir); err != nil {
				t.Fatal(err)
			}
			if status == "fresh" && !strings.Contains(out.String(), "authenticated offline snapshot") {
				t.Fatalf("doctor ignored current-format snapshot: %s", out.String())
			}
			if status != "fresh" && !strings.Contains(out.String(), "offline serve will refuse") {
				t.Fatalf("doctor hid unusable offline state: %s", out.String())
			}
			if strings.Contains(out.String(), "doctor-private-canary") {
				t.Fatal("snapshot diagnostics disclosed cached plaintext")
			}
		})
	}
}

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
