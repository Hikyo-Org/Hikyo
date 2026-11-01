package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
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
	raw, err := json.Marshal(index)
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
	raw, _ = json.Marshal(index)
	if _, err := securityIndexPlatform(raw, "linux", "amd64"); err == nil {
		t.Fatal("ambiguous platform accepted")
	}
	index.Manifests = index.Manifests[:1]
	index.Manifests[0].Digest.Algorithm = "sha512"
	raw, _ = json.Marshal(index)
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
	raw, _ := json.Marshal(c)
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
		changed, err := json.Marshal(c)
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
	raw, _ = json.Marshal(c)
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

func TestSecurityMaintenanceConfigurationRefusesOwnerAndOfflineConflicts(t *testing.T) {
	for _, edit := range []func(*SecurityMaintenanceConfiguration){
		func(c *SecurityMaintenanceConfiguration) { c.Kind = "ecs" },
		func(c *SecurityMaintenanceConfiguration) { c.StateDirectory = "relative" },
		func(c *SecurityMaintenanceConfiguration) { c.NativeConfig = "" },
		func(c *SecurityMaintenanceConfiguration) { c.Policy.Mode = securitymaintenance.Offline },
		func(c *SecurityMaintenanceConfiguration) { c.Feed.OfflineDirectory = "/var/lib/import" },
		func(c *SecurityMaintenanceConfiguration) { c.Policy.UpgradeClass = "skip-safety" },
	} {
		c := testSecurityMaintenanceConfiguration()
		edit(&c)
		if err := c.Validate(); err == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
}
