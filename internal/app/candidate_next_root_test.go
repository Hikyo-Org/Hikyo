package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

func TestCandidateHealthChecksEnrolledNextRootWithoutProvider(t *testing.T) {
	d, srv, client := deploymentAdapterFixture(t, false)
	cfg := *srv.owner.base
	raw, err := json.Marshal(d.enrollment)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConfigRolloutEnrollment = filepath.Join(t.TempDir(), "enrollment.json")
	if err := os.WriteFile(cfg.ConfigRolloutEnrollment, raw, 0600); err != nil {
		t.Fatal(err)
	}
	// No signer, in-cluster API or runtime datastore is required for this check.
	cfg.ConfigRolloutSigningKey = ""
	node, err := cfg.SeedNodeValues()
	if err != nil {
		t.Fatal(err)
	}
	node[config.ManagedNewRootSourceKey] = "root-next"
	values, err := cfg.ManagedSeedForNode(node)
	if err != nil {
		t.Fatal(err)
	}
	values[config.ManagedNodeOverridesKey], err = runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{"local": node})
	if err != nil {
		t.Fatal(err)
	}
	projection := &upgrade.CandidateConfiguration{SchemaVersion: runtimeconfig.SchemaVersion}
	for _, key := range runtimeconfig.Catalogue() {
		compiled, err := schema.CompileClassified(key.Classification, key.Declaration)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := compiled.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		projection.Catalogue = append(projection.Catalogue, upgrade.CandidateConfigurationKey{Name: key.Name, Classification: string(key.Classification), Declaration: string(canonical), RequiredMode: string(schema.PresenceNone), ForbiddenMode: string(schema.PresenceNone)})
	}
	cfg.NewRootKeyFile = "/obsolete/startup/candidate"
	if err := checkCandidateConfigurationFromSources(t.Context(), &cfg, projection, values, d.sourcesDirectory); err != nil {
		t.Fatalf("valid saved source consulted obsolete startup path: %v", err)
	}
	path := filepath.Join(d.sourcesDirectory, "root", "root-next", "root-key")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"missing", "format", "mode", "enrollment"} {
		t.Run(failure, func(t *testing.T) {
			copy := cfg
			writeDeploymentFixture(t, path, string(original), 0600)
			switch failure {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "format":
				writeDeploymentFixture(t, path, "invalid root key", 0600)
			case "mode":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "enrollment":
				copy.ConfigRolloutEnrollment = ""
			}
			if err := checkCandidateConfigurationFromSources(t.Context(), &copy, projection, values, d.sourcesDirectory); err == nil {
				t.Fatal("invalid saved root candidate passed health check")
			}
		})
	}
	writeDeploymentFixture(t, path, string(original), 0600)
	cfg.NewRootKeyFile = path
	if err := checkCandidateConfigurationFromSources(t.Context(), &cfg, nil, nil, d.sourcesDirectory); err != nil {
		t.Fatalf("bootstrap exact alias import failed: %v", err)
	}
	// A newly joining node cannot drop its candidate path while using bootstrap
	// settings for administrative recovery under an existing owner projection.
	values[config.ManagedNodeOverridesKey], err = runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{"other-node": node})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkCandidateConfigurationFromSources(t.Context(), &cfg, projection, values, d.sourcesDirectory); err != nil {
		t.Fatalf("missing-node fallback refused valid enrolled candidate: %v", err)
	}
	cfg.NewRootKeyFile = "/unregistered/candidate"
	if err := checkCandidateConfigurationFromSources(t.Context(), &cfg, projection, values, d.sourcesDirectory); err == nil {
		t.Fatal("missing-node fallback dropped unregistered startup candidate")
	}
	if err := checkCandidateConfigurationFromSources(t.Context(), &cfg, nil, nil, d.sourcesDirectory); err == nil {
		t.Fatal("bootstrap candidate was silently dropped")
	}
	if len(client.Actions()) != 0 {
		t.Fatal("candidate source health used Kubernetes API")
	}
}
