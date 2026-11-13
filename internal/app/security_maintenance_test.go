package app

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestSecurityMaintenanceHelpAvoidsAuthorityAndDatastore(t *testing.T) {
	for _, args := range [][]string{{}, {"check", "--help"}, {"enroll", "--help"}, {"run", "--help"}, {"status", "--help"}, {"stage", "--help"}, {"inspect", "--help"}, {"health", "--help"}, {"feed", "--help"}, {"feed", "publish", "--help"}} {
		var out bytes.Buffer
		if err := RunSecurityMaintenance(t.Context(), args, &out); err != nil && !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("%v: %v", args, err)
		}
		if out.Len() == 0 {
			t.Fatalf("empty help: %v", args)
		}
	}
	for _, args := range [][]string{{"stage"}, {"stage", "--expected-manifest", "invalid"}, {"stage", "--expected-manifest", strings.Repeat("a", 64), "unexpected"}, {"inspect", "--remote-apply"}, {"run", "unexpected"}} {
		if err := RunSecurityMaintenance(t.Context(), args, &bytes.Buffer{}); err == nil {
			t.Fatalf("invalid local command accepted: %v", args)
		}
	}
}

func TestSecurityOperationalHealthUsesLiveLoopbackReadiness(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	cfg := &config.Config{OperationalListen: strings.TrimPrefix(server.URL, "http://")}
	if err := securityOperationalHealth(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{http.StatusServiceUnavailable, http.StatusFound, http.StatusUnauthorized} {
		status.Store(int32(code))
		if err := securityOperationalHealth(t.Context(), cfg); err == nil {
			t.Fatalf("status %d treated as ready", code)
		}
	}
	for _, address := range []string{"example.org:8081", "192.0.2.1:8081", "127.0.0.1:0", "invalid"} {
		cfg.OperationalListen = address
		if err := securityOperationalHealth(t.Context(), cfg); err == nil {
			t.Fatalf("unsafe address %s", address)
		}
	}
}

func TestSecurityImagePlatformDigestComesFromUnambiguousIndex(t *testing.T) {
	digest := v1.Hash{Algorithm: "sha256", Hex: strings.Repeat("a", 64)}
	index := v1.IndexManifest{SchemaVersion: 2, MediaType: types.OCIImageIndex, Manifests: []v1.Descriptor{{Digest: digest, MediaType: types.OCIManifestSchema1, Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}}}
	raw, err := json.Marshal(index, jsonwire.Stable)
	if err != nil {
		t.Fatal(err)
	}
	if actual, err := securityIndexPlatform(raw, "linux", "amd64"); err != nil || actual != digest.Hex {
		t.Fatalf("platform mapping %s %v", actual, err)
	}
	if _, err := securityIndexPlatform(raw, "linux", "arm64"); err == nil {
		t.Fatal("missing platform accepted")
	}
	index.Manifests = append(index.Manifests, index.Manifests[0])
	if raw, err = json.Marshal(index, jsonwire.Stable); err != nil {
		t.Fatal(err)
	}
	if _, err := securityIndexPlatform(raw, "linux", "amd64"); err == nil {
		t.Fatal("ambiguous platform accepted")
	}
	index.Manifests = index.Manifests[:1]
	index.Manifests[0].Digest = v1.Hash{Algorithm: "sha512", Hex: strings.Repeat("a", 128)}
	if raw, err = json.Marshal(index, jsonwire.Stable); err != nil {
		t.Fatal(err)
	}
	// The index must parse, so the refusal below comes from the algorithm check.
	if _, err := v1.ParseIndexManifest(bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := securityIndexPlatform(raw, "linux", "amd64"); err == nil {
		t.Fatal("unsupported digest accepted")
	}
}

type policyTestSecurityDriver struct {
	applies        int
	beforeMutation func()
}

func (d *policyTestSecurityDriver) Observe(context.Context) (securitymaintenance.Observation, error) {
	return securitymaintenance.Observation{}, errors.New("unused observation")
}
func (d *policyTestSecurityDriver) Prepare(context.Context, securitymaintenance.Target) (securitymaintenance.Preparation, error) {
	return securitymaintenance.Preparation{}, errors.New("unused preparation")
}
func (d *policyTestSecurityDriver) Apply(_ context.Context, target securitymaintenance.Target) error {
	if d.beforeMutation != nil {
		d.beforeMutation()
	}
	if target.BeforeMutation != nil {
		if err := target.BeforeMutation(); err != nil {
			return err
		}
	}
	d.applies++
	return nil
}

func testSecurityMaintenanceConfiguration() SecurityMaintenanceConfiguration {
	return SecurityMaintenanceConfiguration{Format: SecurityMaintenanceConfigFormat, StateDirectory: "/var/lib/hikyo-security-maintenance", Kind: "native", NativeConfig: "/etc/hikyo/upgrade.json",
		Feed: SecurityFeedConfiguration{MetadataURL: "https://updates.example.org/metadata/", TargetsURL: "https://updates.example.org/targets/", RootFile: "/etc/hikyo/feed-root.json"}, Policy: securitymaintenance.DefaultPolicy()}
}

func TestSecurityMaintenanceLocalPolicyChangeCannotRaceStagedApply(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "configuration.json")
	c := testSecurityMaintenanceConfiguration()
	raw, _ := json.Marshal(c, jsonwire.Stable)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	driver := &policyTestSecurityDriver{}
	engine := &securitymaintenance.Engine{Driver: driver}
	if err := bindSecurityPolicy(engine, path, c); err != nil {
		t.Fatal(err)
	}
	if err := engine.Driver.Apply(t.Context(), securitymaintenance.Target{}); err != nil || driver.applies != 1 {
		t.Fatalf("unchanged policy %v", err)
	}
	// The deployment owner can perform long final re-staging after the
	// wrapper's initial check. Re-read local authority at its mutation gate.
	driver.beforeMutation = func() {
		c.Policy.Mode = securitymaintenance.Manual
		changed, err := json.Marshal(c, jsonwire.Stable)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, changed, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := engine.Driver.Apply(t.Context(), securitymaintenance.Target{}); !errors.Is(err, securitymaintenance.ErrNotStarted) || driver.applies != 1 {
		t.Fatal("policy changed during final preparation but workload was mutated", err)
	}
	driver.beforeMutation = nil
	c.Policy.Pin = "1.2.0"
	raw, _ = json.Marshal(c, jsonwire.Stable)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := engine.Driver.Apply(t.Context(), securitymaintenance.Target{}); !errors.Is(err, securitymaintenance.ErrNotStarted) || driver.applies != 1 {
		t.Fatalf("changed policy applied: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"format":"hikyo.security-maintenance-config/v1","format":"duplicate"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSecurityMaintenanceConfiguration(path); err == nil {
		t.Fatal("ambiguous operator configuration accepted")
	}
}

// The source build binds and a target build rechecks through --maintenance-policy-sha256,
// so the digest must not depend on either build's configuration struct.
func TestSecurityPolicyDigestIsIndependentOfConfigurationStructShape(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "configuration.json")
	c := testSecurityMaintenanceConfiguration()
	canonical, err := json.Marshal(c, jsonwire.Stable)
	if err != nil {
		t.Fatal(err)
	}
	// Operator formatting makes the file bytes differ from any re-marshalled struct.
	raw, err := json.Marshal(c, jsonwire.Stable, jsontext.WithIndent("    "))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	engine := &securitymaintenance.Engine{Driver: &policyTestSecurityDriver{}}
	if err := bindSecurityPolicy(engine, path, c); err != nil {
		t.Fatal(err)
	}
	bound, ok := engine.Driver.(policyBoundSecurityDriver)
	if !ok || bound.expected != releaseidentity.Hash(raw) || bound.expected == releaseidentity.Hash(canonical) {
		t.Fatalf("policy digest is not the raw file hash: %+v", engine.Driver)
	}
	if err := verifySecurityPolicyFingerprint(path, bound.expected); err != nil {
		t.Fatal(err)
	}
	// A target whose struct differs from this file, shown by a field this build
	// cannot decode, still recognizes the exact bytes its source validated.
	shape := append(append([]byte(nil), canonical[:len(canonical)-1]...), []byte(`,"field_of_another_build":{"added":true}}`)...)
	if err := os.WriteFile(path, shape, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSecurityMaintenanceConfiguration(path); err == nil {
		t.Fatal("fixture does not differ from this build's struct shape")
	}
	if err := verifySecurityPolicyFingerprint(path, releaseidentity.Hash(shape)); err != nil {
		t.Fatalf("differing struct shape changed the policy digest: %v", err)
	}
	if err := verifySecurityPolicyFingerprint(path, bound.expected); !errors.Is(err, securitymaintenance.ErrNotStarted) {
		t.Fatalf("changed bytes accepted: %v", err)
	}
	// The engine runs c, so newer bytes than c was parsed from must not be bound.
	changed := c
	changed.Policy.Mode = securitymaintenance.Manual
	raw, _ = json.Marshal(changed, jsonwire.Stable)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := bindSecurityPolicy(engine, path, c); err == nil {
		t.Fatal("bound a policy file newer than the running configuration")
	}
}

func TestSecurityMaintenanceConfigurationRefusesOwnerAndOfflineConflicts(t *testing.T) {
	for _, edit := range []func(*SecurityMaintenanceConfiguration){
		func(c *SecurityMaintenanceConfiguration) { c.Kind = "ecs" },
		func(c *SecurityMaintenanceConfiguration) { c.StateDirectory = "relative" },
		func(c *SecurityMaintenanceConfiguration) { c.NativeConfig = "" },
		func(c *SecurityMaintenanceConfiguration) { c.Policy.Mode = securitymaintenance.Offline },
		func(c *SecurityMaintenanceConfiguration) { c.Feed.OfflineDirectory = "/var/lib/import" },
		func(c *SecurityMaintenanceConfiguration) { c.Policy.UpgradeClass = "skip-safety" },
		func(c *SecurityMaintenanceConfiguration) { c.StatusFile = "status.json" },
		func(c *SecurityMaintenanceConfiguration) {
			c.StatusFile = "/var/lib/hikyo-security-status/../status.json"
		},
		// The private status.json shares this name; the public subset must never replace it.
		func(c *SecurityMaintenanceConfiguration) { c.StatusFile = c.StateDirectory + "/status.json" },
		// A status file at or above the state directory names a directory, so status is never published.
		func(c *SecurityMaintenanceConfiguration) { c.StatusFile = c.StateDirectory },
		func(c *SecurityMaintenanceConfiguration) { c.StatusFile = filepath.Dir(c.StateDirectory) },
		func(c *SecurityMaintenanceConfiguration) { c.StatusFile = "/" },
	} {
		c := testSecurityMaintenanceConfiguration()
		edit(&c)
		if err := c.Validate(); err == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
}

func TestSecurityStageDirectoryKeepsSourceAndIsPerTarget(t *testing.T) {
	directory := t.TempDir()
	source, older, target := releaseidentity.Hash([]byte("source")), releaseidentity.Hash([]byte("older")), releaseidentity.Hash([]byte("target"))
	for _, digest := range []releaseidentity.Digest{source, older} {
		if err := os.MkdirAll(filepath.Join(directory, "security-stage-"+string(digest), "payload"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// The serving source's own cache is never a staging location.
	downloads := filepath.Join(directory, "downloads", "bundle")
	if err := os.MkdirAll(downloads, 0700); err != nil {
		t.Fatal(err)
	}
	cache, err := securityStageCache(directory, target, source)
	if err != nil {
		t.Fatal(err)
	}
	if cache != filepath.Join(directory, "security-stage-"+string(target)) {
		t.Fatalf("stage cache %s", cache)
	}
	if info, err := os.Stat(cache); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("stage cache not private: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "security-stage-"+string(source), "payload")); err != nil {
		t.Fatalf("source cache removed: %v", err)
	}
	if _, err := os.Stat(downloads); err != nil {
		t.Fatalf("source downloads removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "security-stage-"+string(older))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded stage cache kept: %v", err)
	}
	if again, err := securityStageCache(directory, target, source); err != nil || again != cache {
		t.Fatalf("retried stage lost its cache: %s %v", again, err)
	}
}

func TestSecurityMaintenanceStatusFileIsOptionalAndKeepsPolicyBinding(t *testing.T) {
	c := testSecurityMaintenanceConfiguration()
	if err := c.Validate(); err != nil {
		t.Fatalf("existing configuration without status_file: %v", err)
	}
	c.StatusFile = "/var/lib/hikyo-security-status/status.json"
	if err := c.Validate(); err != nil {
		t.Fatalf("status_file: %v", err)
	}
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "configuration.json")
	raw, err := json.Marshal(c, jsonwire.Stable, jsontext.WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSecurityMaintenanceConfiguration(path)
	if err != nil || loaded.StatusFile != c.StatusFile {
		t.Fatalf("load: %+v %v", loaded, err)
	}
	engine := &securitymaintenance.Engine{Driver: &policyTestSecurityDriver{}}
	if err := bindSecurityPolicy(engine, path, loaded); err != nil {
		t.Fatal(err)
	}
}

func TestSecurityMaintenanceKubernetesConfigurations(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "install", "security-maintenance", "kubernetes.json.example"))
	if err != nil {
		t.Fatal(err)
	}
	var example SecurityMaintenanceConfiguration
	if err := definitions.DecodeStrict(raw, &example); err != nil || example.Validate() != nil {
		t.Fatalf("external-host example invalid: %v %v", err, example.Validate())
	}
	if example.inCluster() || example.volumeRoot() != "" {
		t.Fatal("the kubeconfig example claims to run in the cluster")
	}
	inCluster := func() SecurityMaintenanceConfiguration {
		c := testSecurityMaintenanceConfiguration()
		c.Kind, c.NativeConfig = "kubernetes", ""
		c.StateDirectory, c.StatusFile = "/var/lib/hikyo-security-maintenance/state", "/run/hikyo-security-status/status.json"
		c.Deployment = &securitydeployment.Config{Kind: securitydeployment.Kubernetes, StateDirectory: c.StateDirectory + "/deployment", ImageRepository: "ghcr.io/hikyo-org/hikyo",
			Kubernetes: &securitydeployment.KubernetesConfig{Namespace: "hikyo", Deployment: "r-hikyo", Container: "server", StagePod: "r-hikyo-security-stage", Binary: "/usr/local/bin/hikyo",
				CronJob: "r-hikyo-security-maintenance", StatusConfigMap: "r-hikyo-security-status", VolumeRoot: "/var/lib/hikyo-security-maintenance"}}
		return c
	}
	c := inCluster()
	if err := c.Validate(); err != nil || !c.inCluster() || c.volumeRoot() != "/var/lib/hikyo-security-maintenance" || c.acceptWorldWritableVolumeRoot() {
		t.Fatalf("in-cluster configuration refused: %v", err)
	}
	c.Deployment.Kubernetes.AcceptWorldWritableVolumeRoot = true
	if err := c.Validate(); err != nil || !c.acceptWorldWritableVolumeRoot() {
		t.Fatalf("an accepted world-writable volume root refused in cluster: %v", err)
	}
	for name, edit := range map[string]func(*SecurityMaintenanceConfiguration){
		"status ConfigMap without the status it copies": func(c *SecurityMaintenanceConfiguration) { c.StatusFile = "" },
		"kubeconfig without a context":                  func(c *SecurityMaintenanceConfiguration) { c.Deployment.Kubernetes.Kubeconfig = "/etc/hikyo/kube" },
		"volume root outside the state":                 func(c *SecurityMaintenanceConfiguration) { c.Deployment.Kubernetes.VolumeRoot = "/srv" },
		"world-writable acceptance without a volume root": func(c *SecurityMaintenanceConfiguration) {
			c.Deployment.Kubernetes.VolumeRoot, c.Deployment.Kubernetes.AcceptWorldWritableVolumeRoot = "", true
		},
		"world-writable acceptance off the cluster": func(c *SecurityMaintenanceConfiguration) {
			c.Deployment.Kubernetes.Kubeconfig, c.Deployment.Kubernetes.Context, c.StatusFile, c.Deployment.Kubernetes.StatusConfigMap = "/etc/hikyo/kube", "installed", "", ""
			c.Deployment.Kubernetes.AcceptWorldWritableVolumeRoot = true
		},
	} {
		bad := inCluster()
		edit(&bad)
		if bad.Validate() == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

// The coordinator state on a volume root any local user can write opens only when the operator accepted it.
func TestSecurityMaintenanceStoreOnWorldWritableVolumeNeedsAcceptance(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("volume ownership checks are qualified only on Linux/macOS")
	}
	volume, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(volume, 0777); err != nil {
		t.Fatal(err)
	}
	c := SecurityMaintenanceConfiguration{Kind: "kubernetes", StateDirectory: filepath.Join(volume, "state"), Deployment: &securitydeployment.Config{Kind: securitydeployment.Kubernetes, Kubernetes: &securitydeployment.KubernetesConfig{VolumeRoot: volume}}}
	if _, err := c.openStore(); err == nil {
		t.Fatal("a world-writable volume root was accepted without the operator's acceptance")
	}
	c.Deployment.Kubernetes.AcceptWorldWritableVolumeRoot = true
	if _, err := c.openStore(); err != nil {
		t.Fatalf("an accepted world-writable volume root refused: %v", err)
	}
}

type followingSecurityDeployment struct {
	delayedSecurityDeployment
	followed []string
}

func (d *followingSecurityDeployment) FollowCoordinator(_ context.Context, image string) error {
	d.followed = append(d.followed, image)
	return nil
}

// A reloaded configuration rebinds the follower to its own driver and forgets what the old one followed.
func TestSecurityCoordinatorFollowerIsRebuiltWhenTheConfigurationChanges(t *testing.T) {
	noCronJob := SecurityMaintenanceConfiguration{Kind: "kubernetes", Deployment: &securitydeployment.Config{Kind: securitydeployment.Kubernetes, Kubernetes: &securitydeployment.KubernetesConfig{}}}
	withCronJob := noCronJob
	withCronJob.Deployment = &securitydeployment.Config{Kind: securitydeployment.Kubernetes, Kubernetes: &securitydeployment.KubernetesConfig{CronJob: "coordinator"}}
	old := newCoordinatorFollower(noCronJob, &securityDeploymentDriver{executor: &followingSecurityDeployment{}})
	old.followed = runningState(releaseidentity.StableV1).Running.Identity
	same, err := rebindCoordinatorFollower(old, noCronJob, noCronJob, &securityDeploymentDriver{executor: &followingSecurityDeployment{}})
	if err != nil || same != old {
		t.Fatalf("an unchanged configuration lost its follower: %v", err)
	}
	executor := &followingSecurityDeployment{}
	rebound, err := rebindCoordinatorFollower(old, noCronJob, withCronJob, &securityDeploymentDriver{executor: executor})
	if err != nil || rebound == old || rebound.followed != (releaseidentity.Identity{}) {
		t.Fatalf("a changed configuration kept the old follower or what it followed: %v", err)
	}
	if err := rebound.replace(t.Context(), preparedSecurityRelease{}, probeImage); err != nil || len(executor.followed) != 1 || executor.followed[0] != probeImage {
		t.Fatalf("the rebuilt follower did not use the reloaded driver: %v %v", err, executor.followed)
	}
}

// Startup and every reload share one authority check.
func TestSecurityCoordinatorConfigurationRequiresTheRootOperatorOutsideTheCluster(t *testing.T) {
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		t.Skip("the root operator is allowed")
	}
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "configuration.json")
	raw, err := json.Marshal(testSecurityMaintenanceConfiguration(), jsonwire.Stable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSecurityMaintenanceConfiguration(path); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCoordinatorConfiguration(path); err == nil {
		t.Fatal("a host configuration was accepted without the root operator")
	}
}
