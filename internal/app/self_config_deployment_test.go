package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func deploymentAdapterFixture(t *testing.T, postgres bool) (*bootstrapDeployment, *Server, *fake.Clientset) {
	t.Helper()
	cfg := devConfig(t)
	if postgres {
		cfg = nodePostgresConfig(t)
	}
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	owner, incarnation, err := srv.db.RecoveryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	enrollment := configrollout.Enrollment{
		ID: "enrollment-1", OwnerInstanceID: owner, Incarnation: incarnation,
		Target: configrollout.Target{Namespace: "hikyo", Deployment: "hikyo", DeploymentUID: "deployment-1", StableNodeID: "local", Container: "hikyo", ConfigSecret: "config", RollbackSecret: "rollback", RequestSecret: "request", ReceiptSecret: "receipt",
			DatabaseSources: map[string]configrollout.SecretSource{}, RootSources: map[string]configrollout.SecretSource{"root-primary": {Name: "root-primary", Key: "root-key"}, "root-next": {Name: "root-next", Key: "root-key"}}},
		CommandSecret: "command", CommandSecretUID: "command-1", ResponseSecret: "response", ResponseSecretUID: "response-1", JournalSecret: "journal", JournalSecretUID: "journal-1", LeaseName: "lease", LeaseUID: "lease-1", ExecutorPod: "executor-0",
	}
	client := fake.NewClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "command", Namespace: "hikyo", UID: "command-1", ResourceVersion: "1"}}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "response", Namespace: "hikyo", UID: "response-1", ResourceVersion: "1"}})
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	provider := &bootstrapDeployment{db: srv.db, keyring: srv.keyring, cfg: cfg, enrollment: enrollment, signer: private, sourcesDirectory: t.TempDir(), selectionDirectory: t.TempDir(), proofs: make(map[string]deploymentSourceProof), installed: config.ManagedBootstrapSources{Version: 1, RootSource: "root-primary"}, identity: service.DeploymentIdentity{EnrollmentID: enrollment.ID, OwnerInstanceID: owner, Incarnation: incarnation, DeploymentUID: string(enrollment.Target.DeploymentUID)}}
	root, err := provider.currentRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(root)
	writeDeploymentFixture(t, filepath.Join(provider.sourcesDirectory, "root/root-primary/root-key"), crypto.EncodeRootKey(root), 0600)
	newRoot, err := crypto.GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(newRoot)
	writeDeploymentFixture(t, filepath.Join(provider.sourcesDirectory, "root/root-next/root-key"), crypto.EncodeRootKey(newRoot), 0600)
	if postgres {
		provider.installed.DatabaseSource = "database-primary"
		provider.enrollment.Target.DatabaseSources["database-primary"] = configrollout.SecretSource{Name: "database-primary", Key: "dsn"}
		provider.enrollment.Target.DatabaseSources["database-next"] = configrollout.SecretSource{Name: "database-next", Key: "dsn"}
		writeDeploymentFixture(t, filepath.Join(provider.sourcesDirectory, "database/database-primary/dsn"), cfg.Store.DSN, 0440)
		alias, err := url.Parse(cfg.Store.DSN)
		if err != nil {
			t.Fatal(err)
		}
		q := alias.Query()
		q.Set("application_name", "next-alias")
		alias.RawQuery = q.Encode()
		writeDeploymentFixture(t, filepath.Join(provider.sourcesDirectory, "database/database-next/dsn"), alias.String(), 0440)
	}
	writeDeploymentFixture(t, filepath.Join(provider.selectionDirectory, "stamp"), "", 0444)
	writeDeploymentFixture(t, filepath.Join(provider.selectionDirectory, "database-alias"), provider.installed.DatabaseSource, 0444)
	writeDeploymentFixture(t, filepath.Join(provider.selectionDirectory, "root-alias"), provider.installed.RootSource, 0444)
	provider.mailbox, err = configrollout.NewMailbox(client, provider.enrollment)
	if err != nil {
		t.Fatal(err)
	}
	return provider, srv, client
}

func writeDeploymentFixture(t *testing.T, path, value string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte(value), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func deploymentBundle(t *testing.T, sources config.ManagedBootstrapSources) *runtimeconfig.Bundle {
	t.Helper()
	raw, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := runtimeconfig.Prepare(map[string]string{config.ManagedBootstrapSourcesKey: string(raw)})
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func deploymentIntent(d *bootstrapDeployment) configrollout.Intent {
	return configrollout.Intent{JobID: "job-1", OwnerInstanceID: d.identity.OwnerInstanceID, Incarnation: d.identity.Incarnation, SnapshotID: "snapshot-1", Revision: 2, CatalogueVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1, Generation: 2}
}

func TestBootstrapDeploymentDatabaseProofPrecedesSignedSubmitAndTransport(t *testing.T) {
	d, _, client := deploymentAdapterFixture(t, true)
	pool, err := d.db.PreparePostgresPool(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = pool.Close()
	sources := d.installed
	sources.DatabaseSource = "database-next"
	prepared, err := d.PrepareCommand(t.Context(), deploymentIntent(d), deploymentBundle(t, sources), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.Actions()) != 0 {
		t.Fatal("preparation sent an external command")
	}
	submit, err := d.DecisionCommand(t.Context(), prepared, configrollout.ActionSubmit, 2, strings.Repeat("a", 64), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.Actions()) != 0 {
		t.Fatal("signing submitted before durable coordinator commit")
	}
	if submit.Command.Bootstrap != nil || submit.Command.PlanDigest != strings.Repeat("a", 64) {
		t.Fatal("submit did not bind prepared plan alone")
	}
	if err := d.Send(t.Context(), submit); err != nil {
		t.Fatal(err)
	}
	for _, action := range client.Actions() {
		if action.GetResource().Resource != "secrets" || action.GetVerb() != "get" && action.GetVerb() != "update" {
			t.Fatalf("unexpected transport authority: %s", action.GetVerb())
		}
	}
	observe, err := d.DecisionCommand(t.Context(), submit, configrollout.ActionObserve, 3, submit.Command.PlanDigest, nil)
	if err != nil || observe.Command.Intent != prepared.Command.Intent {
		t.Fatalf("observation after persisted submit: %v", err)
	}
	if _, err := d.DecisionCommand(t.Context(), submit, configrollout.ActionObserve, 3, strings.Repeat("b", 64), nil); err == nil {
		t.Fatal("observation changed plan binding")
	}
	prepared.Command.Intent.JobID = "tampered"
	if _, err := d.DecisionCommand(t.Context(), prepared, configrollout.ActionSubmit, 4, submit.Command.PlanDigest, nil); err == nil {
		t.Fatal("unsigned intent mutation accepted")
	}
}

func TestBootstrapDeploymentRejectsReplacedAliasAndExpiredPrivateProof(t *testing.T) {
	d, _, _ := deploymentAdapterFixture(t, true)
	sources := d.installed
	sources.DatabaseSource = "database-next"
	bundle := deploymentBundle(t, sources)
	prepared, err := d.PrepareCommand(t.Context(), deploymentIntent(d), bundle, 1)
	if err != nil {
		t.Fatal(err)
	}
	// This still reaches the same DB, but its descriptor was not reviewed by
	// this exact preparation. A replacement cannot borrow the old proof digest.
	writeDeploymentFixture(t, filepath.Join(d.sourcesDirectory, "database/database-next/dsn"), d.cfg.Store.DSN, 0440)
	if _, err := d.DecisionCommand(t.Context(), prepared, configrollout.ActionSubmit, 2, strings.Repeat("a", 64), nil); !errors.Is(err, service.ErrDeploymentPreparationExpired) {
		t.Fatalf("changed alias reused proof: %v", err)
	}
	prepared, err = d.PrepareCommand(t.Context(), deploymentIntent(d), bundle, 3)
	if err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	clear(d.proofs)
	d.mu.Unlock()
	if _, err := d.DecisionCommand(t.Context(), prepared, configrollout.ActionSubmit, 4, strings.Repeat("a", 64), nil); !errors.Is(err, service.ErrDeploymentPreparationExpired) {
		t.Fatalf("restart-lost proof accepted: %v", err)
	}
}

func TestBootstrapDeploymentRootPrepareIsSealedButNotPersisted(t *testing.T) {
	d, _, client := deploymentAdapterFixture(t, false)
	seed, err := d.SeedSources(t.Context())
	if err != nil || seed != d.installed {
		t.Fatalf("verified source seed: %+v %v", seed, err)
	}
	if err := d.VerifyInstalled(t.Context(), deploymentBundle(t, seed)); err != nil {
		t.Fatal(err)
	}
	sources := d.installed
	sources.RootSource = "root-next"
	prepared, err := d.PrepareCommand(t.Context(), deploymentIntent(d), deploymentBundle(t, sources), 1)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := d.RootPreparation(t.Context(), prepared)
	if err != nil || wrapper == nil || wrapper.RootKeyEpoch != 2 || len(wrapper.Blob) == 0 {
		t.Fatalf("encrypted root candidate missing: %v", err)
	}
	wrapper.Blob[0] ^= 1
	second, err := d.RootPreparation(t.Context(), prepared)
	if err != nil || second.Blob[0] == wrapper.Blob[0] {
		t.Fatal("root candidate escaped immutable cache")
	}
	if _, err := d.DecisionCommand(t.Context(), prepared, configrollout.ActionSubmit, 2, strings.Repeat("a", 64), nil); err != nil {
		t.Fatalf("pure root reproof: %v", err)
	}
	if len(client.Actions()) != 0 || d.keyring.RootRotationPending() {
		t.Fatal("read-only preparation performed root rotation or sent command")
	}
	wrappers, err := (&keyring.Store{DB: d.db}).ActiveMasterWrappers(t.Context())
	if err != nil || len(wrappers) != 1 {
		t.Fatalf("pure preparation changed persisted wrapper count: %d %v", len(wrappers), err)
	}
	if err := d.VerifyInstalled(t.Context(), deploymentBundle(t, sources)); !errors.Is(err, service.ErrDeploymentSourcesPending) {
		t.Fatalf("old source acknowledged new alias: %v", err)
	}
	writeDeploymentFixture(t, filepath.Join(d.selectionDirectory, "root-alias"), "root-next", 0444)
	if err := d.VerifyInstalled(t.Context(), deploymentBundle(t, sources)); !errors.Is(err, service.ErrDeploymentSourcesPending) {
		t.Fatal("annotation change replaced actual installed source")
	}
}

func TestBootstrapDeploymentSignerUsesStrictInstalledPEM(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	raw := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	path := filepath.Join(t.TempDir(), "authority.key")
	writeDeploymentFixture(t, path, string(raw), 0600)
	signer, err := readDeploymentSigner(path)
	if err != nil || !signer.Public().(ed25519.PublicKey).Equal(private.Public()) {
		t.Fatalf("PKCS8 signer: %v", err)
	}
	if err := os.Chmod(path, 0440); err != nil {
		t.Fatal(err)
	}
	if _, err := readDeploymentSigner(path); err == nil {
		t.Fatal("group-readable signer accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	writeDeploymentFixture(t, path, "unexpected prelude\n"+string(raw), 0600)
	if _, err := readDeploymentSigner(path); err == nil {
		t.Fatal("private-key prelude accepted")
	}
}

// bootDeploymentProbe isolates app boot/seed fencing from Kubernetes transport,
// which the real adapter tests above exercise separately.
type bootDeploymentProbe struct {
	service.BootstrapDeployment
	identity            service.DeploymentIdentity
	pending, rejectSeed bool
}

func (p *bootDeploymentProbe) Identity() service.DeploymentIdentity { return p.identity }
func (p *bootDeploymentProbe) SeedSources(context.Context) (config.ManagedBootstrapSources, error) {
	if p.rejectSeed {
		return config.ManagedBootstrapSources{}, errors.New("stale bootstrap seed must not be read")
	}
	return config.ManagedBootstrapSources{Version: 1, RootSource: "root-primary"}, nil
}
func (p *bootDeploymentProbe) VerifyInstalled(context.Context, *runtimeconfig.Bundle) error {
	if p.pending {
		return service.ErrDeploymentSourcesPending
	}
	return nil
}

func TestBootstrapDeploymentBootSeedsSelectorsAndKeepsPendingRepairFenced(t *testing.T) {
	cfg := devConfig(t)
	pending := false
	resources := defaultBootResources()
	resources.configureDeployment = func(_ context.Context, _ *config.Config, db *store.DB, _ *crypto.Keyring) (service.BootstrapDeployment, error) {
		owner, inc, err := db.RecoveryIdentity()
		if err != nil {
			return nil, err
		}
		return &bootDeploymentProbe{identity: service.DeploymentIdentity{EnrollmentID: "enrollment", OwnerInstanceID: owner, Incarnation: inc, DeploymentUID: "deployment"}, pending: pending, rejectSeed: pending}, nil
	}
	first, err := boot(t.Context(), cfg, testLogger(), resources)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.owner.current.graph.auth.BootstrapAdmin(t.Context(), "owner", "Owner", "stdout"); err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	if err := first.selfConfig.LoadRuntime(t.Context()); err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	bundle, err := first.selfConfig.ResolveRuntimeBundle(t.Context())
	if err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	if bundle.BootstrapSources().RootSource != "root-primary" {
		_ = first.Close()
		t.Fatal("setup omitted verified bootstrap source alias")
	}
	// Preserve the last completed snapshot as a committed target would. The
	// next boot must obtain repair owner settings from this retained snapshot,
	// never from a stale external seed or an unverified target acknowledgement.
	if _, err := first.db.SQLiteWrite().ExecContext(t.Context(), "UPDATE self_config_binding SET previous_snapshot_id=desired_snapshot_id,generation=generation+1 WHERE id=1"); err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	pending = true
	second, err := boot(t.Context(), cfg, testLogger(), resources)
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, second)
	if _, err := second.selfConfig.Capture(t.Context()); err == nil {
		t.Fatal("pending bootstrap sources acknowledged")
	}
	if status := ownerHTTPStatus(t, second, http.MethodGet, "/ready"); status == http.StatusOK {
		t.Fatal("pending bootstrap source reported ready")
	}
	if status := ownerHTTPStatus(t, second, http.MethodGet, "/api/v1/orgs/org_00000000-0000-7000-8000-000000000001/projects"); status != http.StatusServiceUnavailable {
		t.Fatalf("business graph not fenced: %d", status)
	}
	if status := ownerHTTPStatus(t, second, http.MethodGet, "/api/v1/auth/whoami"); status != http.StatusUnauthorized {
		t.Fatalf("repair authentication unavailable: %d", status)
	}
}

func TestBootstrapDeploymentRefusesHAEnrollmentBeforeReadingCustody(t *testing.T) {
	cfg := &config.Config{HA: true, ConfigRolloutEnrollment: "/must-not-read/enrollment", ConfigRolloutSigningKey: "/must-not-read/authority"}
	if _, err := configureBootstrapDeployment(t.Context(), cfg, nil, nil); !errors.Is(err, configrollout.ErrUnsupported) {
		t.Fatalf("HA enrollment: %v", err)
	}
	cfg.ConfigRolloutEnrollment, cfg.ConfigRolloutSigningKey = "", ""
	if provider, err := configureBootstrapDeployment(t.Context(), cfg, nil, nil); err != nil || provider != nil {
		t.Fatal("unenrolled HA changed normal runtime")
	}
}

func TestBootstrapDeploymentEnrollmentPinsStandaloneNode(t *testing.T) {
	d, srv, _ := deploymentAdapterFixture(t, false)
	enrollment := d.enrollment
	enrollment.Target.StableNodeID = "another-node"
	raw, err := json.Marshal(enrollment)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "enrollment.json")
	writeDeploymentFixture(t, path, string(raw), 0444)
	cfg := *d.cfg
	cfg.ConfigRolloutEnrollment = path
	cfg.ConfigRolloutSigningKey = "/must-not-read/authority.key"
	if _, err := configureBootstrapDeployment(t.Context(), &cfg, srv.db, srv.keyring); !errors.Is(err, configrollout.ErrConflict) {
		t.Fatalf("unbound node admitted: %v", err)
	}
}
