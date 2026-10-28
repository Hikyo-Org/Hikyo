package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http/httptest"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/webauthntest"
)

// The real CLI, API authorization and chunked service must agree on completion.
// A successful HTTP fixture alone cannot prove ciphertext moved or keys retired.
func TestDEKCLICompletesRotation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := bootstrapWebAuthnAdmin(t, db)
		seedComposeCatalogue(t, db) // live values and retained published snapshots
		kr := probeKeyring(t, db)
		reencrypt := &service.Reencrypt{DB: db, Keyring: kr, ChunkSize: 1, ChunkPause: -1}
		srv := httptest.NewTLSServer(server.New(&server.API{
			Auth:      admin.auth,
			Rotation:  &service.Rotation{DB: db, Keyring: kr, RootKey: probeRootSource{db: db}},
			Reencrypt: reencrypt,
			Version:   "dek-cli-fixture",
		}, nil))
		t.Cleanup(srv.Close)
		stateDir, workDir := t.TempDir(), t.TempDir()
		var stdout, stderr bytes.Buffer
		ios := cli.IO{Stdout: &stdout, Stderr: &stderr, Workdir: workDir, Env: cli.Env{Getenv: func(key string) string {
			if key == "HIKYO_STATE_DIR" {
				return stateDir
			}
			return ""
		}}}
		state, err := cli.NewState(ios.Env)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Trust().Put(cli.TrustEntry{Name: "fixture", Origin: srv.URL, SPKIPin: cli.SPKIFingerprint(srv.Certificate())}); err != nil {
			t.Fatal(err)
		}
		setSession := func(token string) {
			t.Helper()
			if err := state.PutSession(cli.SessionArtifact{Instance: "fixture", Origin: srv.URL, Token: token, Principal: string(admin.boot.PrincipalID)}); err != nil {
				t.Fatal(err)
			}
		}
		run := func(t *testing.T, want int, args ...string) []byte {
			t.Helper()
			stdout.Reset()
			stderr.Reset()
			args = append(args, "--instance", "fixture", "-o", "json")
			if code := cli.Run(t.Context(), ios, args); code != want {
				t.Fatalf("%v: exit %d, want %d: %s", args, code, want, &stderr)
			}
			if want != cli.ExitOK && stdout.Len() != 0 {
				t.Fatalf("refusal printed success: %s", &stdout)
			}
			return bytes.Clone(stdout.Bytes())
		}
		setSession("invalid-fixture-session")
		// The actual API must reject unauthenticated requests before rotation.
		for _, verb := range []string{"rotate-dek", "reencrypt"} {
			args := []string{verb, "--instance-scope"}
			if verb == "rotate-dek" {
				args = append(args, "--yes")
			}
			run(t, cli.ExitAuth, args...)
		}
		dev := webauthntest.New(waRPID, waOrigin)
		// Keep two live sessions for the same account: owning a passkey must
		// not elevate the password-only session's recorded assurance.
		passwordSession := enrolPasskey(t, admin.auth, t.Context(), admin.token, admin.password, dev)
		passkey, err := discoverableLogin(t, admin.auth, t.Context(), dev)
		if err != nil {
			t.Fatal(err)
		}
		setSession(passkey.SessionToken)
		// The project exists, but not under this org. Neither verb may mint a
		// phantom DEK or fall back to the instance key on a malformed chain.
		keysBefore := queryInt(t, db, "SELECT COUNT(*) FROM tier3_keys")
		for _, verb := range []string{"rotate-dek", "reencrypt"} {
			args := []string{verb, "--org", "org_22222222-2222-4222-8222-222222222222", "--project", cPrj}
			if verb == "rotate-dek" {
				args = append(args, "--yes")
			}
			run(t, cli.ExitNotFound, args...)
		}
		if after := queryInt(t, db, "SELECT COUNT(*) FROM tier3_keys"); after != keysBefore {
			t.Fatalf("wrong-target refusal changed key count from %d to %d", keysBefore, after)
		}

		for _, scope := range []string{"project", "instance"} {
			t.Run(scope, func(t *testing.T) {
				selectors := []string{"--instance-scope"}
				org, project := "", ""
				if scope == "project" {
					selectors = []string{"--org", cOrg, "--project", cPrj}
					org, project = cOrg, cPrj
				}
				passwordRefused := func(t *testing.T, verb string) {
					t.Helper()
					identity, err := admin.auth.Identity(t.Context(), passwordSession)
					if err != nil {
						t.Fatalf("password session must still authenticate: %v", err)
					}
					if len(identity.Assurance.Factors) != 1 || identity.Assurance.Factors[0] != "password" || identity.EnrolmentRequired {
						t.Fatalf("expected an unrestricted password-only session: %+v", identity)
					}
					keysBefore := queryInt(t, db, "SELECT COUNT(*) FROM tier3_keys")
					statesBefore, err := queryTier3StatesPurpose(db, t.Context(), scope, org, project)
					if err != nil {
						t.Fatal(err)
					}
					ciphertextQuery := "SELECT verifier FROM password_credentials ORDER BY account_id LIMIT 1"
					if scope == "project" {
						ciphertextQuery = "SELECT ciphertext FROM value_entries WHERE key_id='" + cKeyPw + "'"
					}
					ciphertextBefore := reencReadBlob(t, db, t.Context(), ciphertextQuery)
					trail := "instance"
					if scope == "project" {
						trail = "tenant"
					}
					completedQuery := "SELECT COUNT(*) FROM audit_" + trail + "_events WHERE type='crypto.reencrypt_completed'"
					completedBefore := queryInt(t, db, completedQuery)
					chunks := 0
					reencrypt.AfterChunk = func(context.Context, string) error { chunks++; return nil }
					t.Cleanup(func() { reencrypt.AfterChunk = nil; setSession(passkey.SessionToken) })
					setSession(passwordSession)
					args := append([]string{verb}, selectors...)
					if verb == "rotate-dek" {
						args = append(args, "--yes")
					}
					run(t, cli.ExitRefused, args...)
					statesAfter, err := queryTier3StatesPurpose(db, t.Context(), scope, org, project)
					if err != nil {
						t.Fatal(err)
					}
					if queryInt(t, db, "SELECT COUNT(*) FROM tier3_keys") != keysBefore || !maps.Equal(statesBefore, statesAfter) || chunks != 0 {
						t.Fatalf("password refusal mutated keys or walked ciphertext: before %v, after %v, chunks %d", statesBefore, statesAfter, chunks)
					}
					if !bytes.Equal(ciphertextBefore, reencReadBlob(t, db, t.Context(), ciphertextQuery)) || queryInt(t, db, completedQuery) != completedBefore {
						t.Fatal("password refusal changed ciphertext or emitted completion")
					}
				}
				if !t.Run("password-only-rotate", func(t *testing.T) { passwordRefused(t, "rotate-dek") }) {
					return
				}
				var rotated apigen.DEKRotation
				raw := run(t, cli.ExitOK, append([]string{"rotate-dek", "--yes"}, selectors...)...)
				if err := json.Unmarshal(raw, &rotated); err != nil {
					t.Fatal(err)
				}
				if string(rotated.Scope) != scope || rotated.KeyVersion != 2 {
					t.Fatalf("rotation = %+v", rotated)
				}
				before, err := queryTier3StatesPurpose(db, t.Context(), scope, org, project)
				if err != nil {
					t.Fatal(err)
				}
				if before[1] != "retiring" || before[2] != "active" {
					t.Fatalf("rotation alone retired data key: %v", before)
				}
				if !t.Run("password-only-reencrypt", func(t *testing.T) { passwordRefused(t, "reencrypt") }) {
					return
				}
				if scope == "project" {
					// Fail after an earlier chunk committed. The CLI must report
					// failure, preserve the retiring key and resume the same scope.
					chunks := 0
					reencrypt.AfterChunk = func(_ context.Context, table string) error {
						if table == "value" {
							chunks++
							if chunks == 2 {
								return domain.ErrConflict
							}
						}
						return nil
					}
					run(t, cli.ExitRefused, append([]string{"reencrypt"}, selectors...)...)
					reencrypt.AfterChunk = nil
					partial, err := queryTier3States(db, t.Context(), org, project)
					if err != nil {
						t.Fatal(err)
					}
					if partial[1] != "retiring" {
						t.Fatalf("failed walk retired v1: %v", partial)
					}
					if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='crypto.reencrypt_completed'"); n != 0 {
						t.Fatalf("failed walk emitted %d completion events", n)
					}
				}
				for attempt := 0; attempt < 2; attempt++ {
					var result apigen.ReencryptResult
					raw := run(t, cli.ExitOK, append([]string{"reencrypt"}, selectors...)...)
					if err := json.Unmarshal(raw, &result); err != nil {
						t.Fatal(err)
					}
					if string(result.Scope) != scope || (attempt == 0 && result.RowsMoved == 0) || (attempt == 1 && result.RowsMoved != 0) {
						t.Fatalf("attempt %d: %+v", attempt, result)
					}
				}
				after, err := queryTier3StatesPurpose(db, t.Context(), scope, org, project)
				if err != nil {
					t.Fatal(err)
				}
				if after[1] != "retired" || after[2] != "active" {
					t.Fatalf("reencrypt did not retire v1: %v", after)
				}
			})
		}
		// Both the project live value and the instance password remain readable
		// under v2, after the corresponding old keys have been retired.
		sealer, err := kr.ForProject(t.Context(), cOrg, cPrj)
		if err != nil {
			t.Fatal(err)
		}
		rows := reencQueryStrings(t, db, t.Context(), "SELECT id FROM value_entries WHERE key_id='"+cKeyPw+"'")
		if len(rows) != 1 {
			t.Fatalf("password rows = %v", rows)
		}
		ct := reencReadBlob(t, db, t.Context(), "SELECT ciphertext FROM value_entries WHERE id='"+rows[0]+"'")
		pt, err := sealer.OpenValue(crypto.ValueAAD{OrgID: cOrg, ProjectID: cPrj, EnvID: cEnv, KeyID: cKeyPw, RowID: rows[0], FieldTag: "value"}, ct)
		if err != nil || string(pt) != "dev-secret" {
			t.Fatalf("read after retirement: %q, %v", pt, err)
		}
		if _, err := admin.auth.LocalLogin(t.Context(), waAdmin, admin.password, service.ArtifactBrowser); err != nil {
			t.Fatalf("login after instance retirement: %v", err)
		}
	})
}
