package ci_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// A release-source edge names its source by the exact release identity the
// planner records after an upgrade. scripts/lib/release.sh builds that identity
// from a downloaded release's files; it must equal the one the real verifier
// derives from the same bytes and the one a generated edge encodes.
func TestReleaseIdentityFromPublishedFilesMatchesTheVerifier(t *testing.T) {
	f := testfixture.New(t)
	compatibility := []byte(`{"schema":"hikyo.dev/upgrade-compatibility/v1","probe":"identity binding"}`)
	signed := f.AddStable(t, "1.1.0", 2, "0123456789abcdef0123456789abcdef01234567", compatibility)
	verified, err := releasetrust.VerifyStable(f.Snapshot(t), signed.Material)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, content := range map[string][]byte{"release-manifest.json": signed.Material.Manifest, "upgrade-compatibility.json": signed.Material.Compatibility} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(context.Background(), "sh", "-c", `. "$1/scripts/lib/release.sh" && release_identity_json "$2/release-manifest.json" "$2/upgrade-compatibility.json"`, "sh", repositoryRoot(t), dir)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("release_identity_json: %v", err)
	}
	var fromShell, fromVerifier map[string]any
	if err := json.Unmarshal(output, &fromShell); err != nil {
		t.Fatal(err)
	}
	// The identity as the generator and the planner encode it inside an edge's source.
	encoded := testfixture.JSON(t, releaseidentity.Source{Release: verified.Identity()})
	var source struct {
		Release map[string]any `json:"release"`
	}
	if err := json.Unmarshal(encoded, &source); err != nil {
		t.Fatal(err)
	}
	fromVerifier = source.Release
	if !reflect.DeepEqual(fromShell, fromVerifier) || len(fromShell) != 6 {
		t.Fatalf("identity from the published files differs from the verified one:\nshell    %v\nverifier %v", fromShell, fromVerifier)
	}
}
