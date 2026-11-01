package ci_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedNightlySecurityBindsSourceAndPublisher(t *testing.T) {
	const repository = "Hikyo-Org/Hikyo"
	const tag = "v1.2.3-nightly.20261101.42.gaaaaaaaa"
	source, publisher := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, scenario := range []struct {
		name, schema, source, publisher, tag string
		missingPublisher, signatureFail      bool
		foreignPublisher, movedTag           bool
		wantFailure                          bool
	}{
		{name: "legacy bridge", schema: "v1", source: source, publisher: source, tag: tag},
		{name: "distinct dev source", schema: "v2", source: source, publisher: publisher, tag: tag},
		{name: "signed wrong source", schema: "v2", source: strings.Repeat("c", 40), publisher: publisher, tag: tag, wantFailure: true},
		{name: "signed wrong publisher", schema: "v2", source: source, publisher: strings.Repeat("c", 40), tag: tag, wantFailure: true},
		{name: "signed wrong tag", schema: "v2", source: source, publisher: publisher, tag: "v1.2.3-nightly.20261101.43.gaaaaaaaa", wantFailure: true},
		{name: "missing publisher", schema: "v2", source: source, publisher: publisher, tag: tag, missingPublisher: true, wantFailure: true},
		{name: "signature refusal", schema: "v2", source: source, publisher: publisher, tag: tag, signatureFail: true, wantFailure: true},
		{name: "publisher outside main", schema: "v2", source: source, publisher: publisher, tag: tag, foreignPublisher: true, wantFailure: true},
		{name: "tag movement", schema: "v2", source: source, publisher: publisher, tag: tag, movedTag: true, wantFailure: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, content string) {
				t.Helper()
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0700); err != nil {
					t.Fatal(err)
				}
			}
			manifest := map[string]string{"schema": "hikyo.dev/nightly-manifest/" + scenario.schema, "source_commit": scenario.source, "tag": scenario.tag}
			if scenario.schema == "v2" {
				manifest["publisher_commit"] = scenario.publisher
			}
			raw, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			write("manifest.json", string(raw))
			write("bin/git", `#!/bin/bash
set -euo pipefail
case "$*" in
  "rev-parse refs/tags/$FIXTURE_TAG^{commit}") printf '%s\n' "$FIXTURE_TAG_SOURCE" ;;
  "merge-base --is-ancestor $FIXTURE_PUBLISHER HEAD") test "$FIXTURE_FOREIGN_PUBLISHER" = false ;;
  *) exit 90 ;;
esac
`)
			write("bin/gh", `#!/bin/bash
set -euo pipefail
case "$*" in
  "api repos/Hikyo-Org/Hikyo/releases/tags/$FIXTURE_TAG")
    printf '{"tag_name":"%s","draft":false,"published_at":"2026-11-01T00:00:00Z"}\n' "$FIXTURE_TAG" ;;
  "release download $FIXTURE_TAG --repo Hikyo-Org/Hikyo --dir $FIXTURE_WORK/bundle")
    cp "$FIXTURE_MANIFEST" "$FIXTURE_WORK/bundle/release-manifest.json" ;;
  *) exit 91 ;;
esac
`)
			// Real signature and transparency refusal is exercised by the trust
			// owner tests. This fixture checks ordering and identity propagation
			// across the workflow's shell/verifier/runtime boundary.
			write("bin/go", `#!/bin/bash
set -euo pipefail
case "$*" in
  "run ./scripts/release/nightly verify --trust release/trust --directory $FIXTURE_WORK/bundle")
    test "$FIXTURE_SIGNATURE_FAIL" = false
    printf '{}\n' ;;
  "run ./scripts/security/release-evidence prepare --directory $FIXTURE_WORK/bundle --identity $FIXTURE_WORK/identity.json --tag $FIXTURE_TAG --source $FIXTURE_SOURCE --binary $FIXTURE_WORK/hikyo --out $FIXTURE_WORK/prepared.json")
    touch "$FIXTURE_PREPARED" ;;
  *) exit 92 ;;
esac
`)
			write("scripts/release/assemble-nightly.sh", "#!/bin/sh\nset -eu\ntest -f \"$FIXTURE_PREPARED\"\ntouch \"$FIXTURE_ASSEMBLED\"\n")
			publisherInput := publisher
			if scenario.schema == "v1" {
				publisherInput = source
			}
			if scenario.missingPublisher {
				publisherInput = ""
			}
			tagSource := source
			if scenario.movedTag {
				tagSource = strings.Repeat("c", 40)
			}
			work := filepath.Join(dir, "work")
			prepared, assembled := filepath.Join(dir, "prepared"), filepath.Join(dir, "assembled")
			cmd := exec.Command("bash", filepath.Join(repositoryRoot(t), "scripts/security/verify-release.sh"), repository, tag, source, work)
			cmd.Dir = dir
			cmd.Env = []string{
				"PATH=" + filepath.Join(dir, "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
				"GITHUB_REPOSITORY=" + repository, "NIGHTLY_PUBLISHER_COMMIT=" + publisherInput,
				"FIXTURE_TAG=" + tag, "FIXTURE_SOURCE=" + source, "FIXTURE_PUBLISHER=" + publisherInput,
				"FIXTURE_TAG_SOURCE=" + tagSource, "FIXTURE_WORK=" + work, "FIXTURE_MANIFEST=" + filepath.Join(dir, "manifest.json"),
				"FIXTURE_PREPARED=" + prepared, "FIXTURE_ASSEMBLED=" + assembled,
				"FIXTURE_SIGNATURE_FAIL=" + map[bool]string{true: "true", false: "false"}[scenario.signatureFail],
				"FIXTURE_FOREIGN_PUBLISHER=" + map[bool]string{true: "true", false: "false"}[scenario.foreignPublisher],
			}
			out, err := cmd.CombinedOutput()
			if (err != nil) != scenario.wantFailure {
				t.Fatalf("wantFailure=%t: %v\n%s", scenario.wantFailure, err, out)
			}
			for _, path := range []string{prepared, assembled} {
				_, err := os.Stat(path)
				if scenario.wantFailure && !os.IsNotExist(err) || !scenario.wantFailure && err != nil {
					t.Fatalf("runtime payload exposed before identity verification: %s %v", path, err)
				}
			}
		})
	}
}
