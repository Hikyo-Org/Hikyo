package ci_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// A release-source edge names its source by the exact release identity the planner
// records after an upgrade. The compatibility check takes that identity from the
// verifier's own output and never rebuilds it (scripts/release/stable/historical_test.go
// pins what `stable verify` prints). This test binds the edge rule to the way a release
// source is encoded: an edge encoded the way the generator and the planner encode a
// release source is judged sound against the identity taken from the output, and an
// edge that differs in any one of the six identity fields is not.
func TestVerifierIdentityIsWhatTheEdgeCheckExpects(t *testing.T) {
	f := testfixture.New(t)
	signed := f.AddStable(t, "1.1.0", 2, "0123456789abcdef0123456789abcdef01234567", []byte(`{"schema":"hikyo.dev/upgrade-compatibility/v1","probe":"identity binding"}`))
	verified, err := releasetrust.VerifyStable(f.Snapshot(t), signed.Material)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, value any) {
		if err := os.WriteFile(filepath.Join(dir, name), testfixture.JSON(t, value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("verify.json", map[string]any{"identity": verified.Identity(), "published_oci_verified": true})
	digest := strings.Repeat("a", 64)
	migrations := func(engine string) map[string]any {
		return map[string]any{"engine": engine, "entries": []any{}}
	}
	write("baseline.json", map[string]any{"engines": []any{
		map[string]any{"migrations": migrations("sqlite"), "schema_sha256": digest},
		map[string]any{"migrations": migrations("postgres"), "schema_sha256": digest},
	}})
	var source map[string]any
	if err := json.Unmarshal(testfixture.JSON(t, releaseidentity.Source{Release: verified.Identity()}), &source); err != nil {
		t.Fatal(err)
	}
	if release, ok := source["release"].(map[string]any); !ok || len(release) != 6 {
		t.Fatalf("a release source no longer encodes the six identity fields: %v", source)
	}
	edges := func(source map[string]any) map[string]any {
		edge := func(engine string) []any {
			return []any{map[string]any{"source": source, "migrations": migrations(engine), "schema_sha256": digest, "mode": "maintenance"}}
		}
		return map[string]any{"sqlite": edge("sqlite"), "postgres": edge("postgres")}
	}
	write("edges-ok.json", edges(source))
	changes := map[string]any{"profile": "stable/v2", "version": "1.1.1", "sequence": float64(3), "commit": strings.Repeat("9", 40),
		"compatibility_sha256": strings.Repeat("f", 64), "manifest_sha256": strings.Repeat("e", 64)}
	for field, value := range changes {
		other := map[string]any{"release": map[string]any{}}
		for key, kept := range source["release"].(map[string]any) {
			other["release"].(map[string]any)[key] = kept
		}
		other["release"].(map[string]any)[field] = value
		write("edges-"+field+".json", edges(other))
	}

	problems := func(edgesFile string) string {
		command := exec.CommandContext(context.Background(), "sh", "-c", `set -eu
. "$1/scripts/lib/release.sh"
identity=$(jq -ce '.identity | select(type == "object")' "$2/verify.json")
release_source_edge_problems "$identity" "$2/baseline.json" "$2/$3"`, "sh", repositoryRoot(t), dir, edgesFile)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("release_source_edge_problems: %v\n%s", err, output)
		}
		return strings.TrimSpace(string(output))
	}
	if got := problems("edges-ok.json"); got != "" {
		t.Fatalf("an edge that names the verified identity was refused:\n%s", got)
	}
	for field := range changes {
		if got := problems("edges-" + field + ".json"); !strings.Contains(got, "sqlite: no release-source edge from v1.1.0") {
			t.Fatalf("an edge that differs in %s was accepted:\n%s", field, got)
		}
	}
}
