package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storekeyring "github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestNextRootSeedRequiresExactEnrolledProjection(t *testing.T) {
	d, srv, _ := deploymentAdapterFixture(t, false)
	cfg := *srv.owner.base
	cfg.NewRootKeyFile = filepath.Join(d.sourcesDirectory, "root", "root-next", "root-key")
	seed := newSelfConfig(&cfg, srv.db, srv.keyring, srv.selfConfig.Auth)
	seed.Deployment = d
	values, err := seed.SeedNode()
	if err != nil || values[config.ManagedNewRootSourceKey] != "root-next" {
		t.Fatalf("candidate source not imported: %v %v", values, err)
	}
	for _, value := range values {
		if value == cfg.NewRootKeyFile {
			t.Fatal("candidate path imported")
		}
	}
	for _, path := range []string{"/arbitrary/operator/key", filepath.Join(d.sourcesDirectory, "root", "missing", "root-key")} {
		if _, err := d.nextRootAlias(path); !errors.Is(err, errNextRootEnrollment) {
			t.Fatalf("unregistered path accepted: %v", err)
		}
	}
	cfg.NewRootKeyFile = "/arbitrary/operator/key"
	seed = newSelfConfig(&cfg, srv.db, srv.keyring, srv.selfConfig.Auth)
	if _, err := seed.SeedNode(); !errors.Is(err, errNextRootEnrollment) || !strings.Contains(err.Error(), "rollout.rootSources") {
		t.Fatalf("missing enrollment lost operator remedy: %v", err)
	}
	// Empty initial configuration still boots/adopts without enrollment.
	cfg.NewRootKeyFile = ""
	if _, err := newSelfConfig(&cfg, srv.db, srv.keyring, srv.selfConfig.Auth).SeedNode(); err != nil {
		t.Fatal(err)
	}
}

func TestNextRootPreparationRejectsUnavailableSources(t *testing.T) {
	for _, mode := range []string{"unknown", "missing", "permissions", "format", "unenrolled"} {
		t.Run(mode, func(t *testing.T) {
			d, srv, _ := deploymentAdapterFixture(t, false)
			srv.selfConfig.Deployment = d
			alias := "root-next"
			path := filepath.Join(d.sourcesDirectory, "root", alias, "root-key")
			switch mode {
			case "unknown":
				alias = "unregistered"
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "format":
				writeDeploymentFixture(t, path, "not a root", 0600)
			case "unenrolled":
				srv.selfConfig.Deployment = nil
			}
			before := srv.owner.current.graph
			bundle := nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) { node[config.ManagedNewRootSourceKey] = alias })
			if _, err := srv.owner.Prepare(t.Context(), bundle); err == nil {
				t.Fatal("invalid candidate source prepared")
			}
			if srv.owner.current.graph != before {
				t.Fatal("refused source changed graph")
			}
		})
	}
}

func TestNextRootLiveSelectionKeepsRotationSeparate(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		name := "sqlite"
		if postgres {
			name = "postgres"
		}
		t.Run(name, func(t *testing.T) {
			d, srv, _ := deploymentAdapterFixture(t, postgres)
			bootstrap, err := srv.owner.current.graph.auth.BootstrapAdmin(t.Context(), "operator", "Operator", "stdout")
			if err != nil {
				t.Fatal(err)
			}
			if err := srv.selfConfig.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			srv.selfConfig.Deployment = d
			token, verifier, err := crypto.NewArtifact(crypto.ArtifactBrowserSession)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			err = tx.Write(t.Context(), srv.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				generation, err := az.PrincipalGeneration(ctx, bootstrap.PrincipalID)
				if err != nil {
					return err
				}
				epoch, err := az.CredentialEpoch(ctx)
				if err != nil {
					return err
				}
				return az.MintSession(ctx, authz.NewSession{ID: "ses_next_root", PrincipalID: bootstrap.PrincipalID, Verifier: verifier, Artifact: "browser", SessionGeneration: generation, CredentialEpoch: epoch, AuthMethod: "local-passkey", Factors: `["webauthn"]`, AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour), SourceIP: "127.0.0.1", UserAgent: "next-root-test"})
			})
			if err != nil {
				t.Fatal(err)
			}
			startOwnerServer(t, srv)
			before, db, kr, listener := srv.owner.current.graph, srv.db, srv.keyring, srv.publicLn
			current, err := d.currentRoot()
			if err != nil {
				t.Fatal(err)
			}
			defer crypto.Zero(current)
			next, err := d.rootSource("root-next")
			if err != nil {
				t.Fatal(err)
			}
			defer crypto.Zero(next)
			wrappers := &storekeyring.Store{DB: srv.db}
			count := func(want int) {
				t.Helper()
				rows, err := wrappers.ActiveMasterWrappers(t.Context())
				if err != nil || len(rows) != want {
					t.Fatalf("wrappers=%d want%d: %v", len(rows), want, err)
				}
			}
			count(1)
			bundle := nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) { node[config.ManagedNewRootSourceKey] = "root-next" })
			prepared, err := srv.owner.Prepare(t.Context(), bundle)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if srv.owner.current.graph != before {
				t.Fatal("preparation changed active selector")
			}
			count(1)
			if err := prepared.Activate(t.Context()); err != nil {
				t.Fatal(err)
			}
			if srv.owner.current.graph == before || srv.db != db || srv.keyring != kr || srv.publicLn != listener {
				t.Fatal("selector did not reload within same running server")
			}
			count(1)
			source, err := srv.owner.rotationRootSource(t.Context(), srv.owner.current.graph.cfg)
			if err != nil {
				t.Fatal(err)
			}
			got, err := source.Current(t.Context())
			if err != nil || !bytes.Equal(got, current) {
				t.Fatal("candidate changed primary root", err)
			}
			crypto.Zero(got)
			call := func(bearer string) int {
				t.Helper()
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+srv.Addr+"/api/v1/instance/rotate-root-key", strings.NewReader(`{"phase":"prepare"}`))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				if bearer != "" {
					req.Header.Set("Authorization", "Bearer "+bearer)
				}
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				_, _ = io.Copy(io.Discard, res.Body)
				return res.StatusCode
			}
			if code := call(""); code != http.StatusUnauthorized {
				t.Fatalf("unauthorized prepare=%d", code)
			}
			count(1)
			if code := call(token); code != http.StatusOK {
				t.Fatalf("authorized explicit prepare=%d", code)
			}
			count(2)
			if err := crypto.VerifyExistingHierarchy(t.Context(), wrappers, bytes.Clone(next)); err != nil {
				t.Fatal("explicit prepare did not use selected next root", err)
			}
			// Clearing is complete projection state, even if startup still names a file.
			srv.owner.base.NewRootKeyFile = filepath.Join(d.sourcesDirectory, "root", "root-next", "root-key")
			activateNode(t, srv, nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) { delete(node, config.ManagedNewRootSourceKey) }))
			source, err = srv.owner.rotationRootSource(t.Context(), srv.owner.current.graph.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if value, err := source.Next(t.Context()); err == nil {
				crypto.Zero(value)
				t.Fatal("clear revived startup file")
			}
			count(2)
		})
	}
}
