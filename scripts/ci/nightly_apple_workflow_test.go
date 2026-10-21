package ci_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNightlyAppleSigningGatesPublication(t *testing.T) {
	var workflow struct {
		Jobs map[string]struct {
			Needs       yaml.Node            `yaml:"needs"`
			If          string               `yaml:"if"`
			RunsOn      string               `yaml:"runs-on"`
			Environment string               `yaml:"environment"`
			Permissions map[string]string    `yaml:"permissions"`
			Outputs     map[string]string    `yaml:"outputs"`
			Steps       []mainValidationStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	raw, err := os.ReadFile("../../.github/workflows/nightly.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string][]string{
		"build-artifacts": {"validation"},
		"apple-sign":      {"build-artifacts"},
		"publish":         {"validation", "build-artifacts", "apple-sign"},
		"container":       {"publish"},
	} {
		needs, err := workflowNeeds(workflow.Jobs[name].Needs)
		if err != nil || !reflect.DeepEqual(needs, expected) {
			t.Fatalf("%s dependencies = %v, expected %v: %v", name, needs, expected, err)
		}
	}
	apple := workflow.Jobs["apple-sign"]
	if apple.Environment != "apple-signing" || apple.RunsOn != "macos-15" || !reflect.DeepEqual(apple.Permissions, map[string]string{"contents": "read"}) || apple.If != "github.ref == 'refs/heads/main' && needs.build-artifacts.outputs.skip != 'true'" {
		t.Fatal("nightly Apple signing requires its protected credential environment, main, macOS and read-only repository access; only an already published release may skip it")
	}
	if workflow.Jobs["container"].If != "${{ !cancelled() && github.ref == 'refs/heads/main' && needs.publish.result == 'success' }}" {
		t.Fatal("container retries require an explicit status predicate so a skipped Apple ancestor cannot skip an already published release")
	}
	build := workflow.Jobs["build-artifacts"]
	for key, expected := range map[string]string{
		"non_darwin_sha256":    "${{ steps.retained-integrity.outputs.non_darwin_sha256 }}",
		"compatibility_sha256": "${{ steps.compatibility.outputs.sha256 }}",
	} {
		if build.Outputs[key] != expected {
			t.Fatalf("build must independently export %s", key)
		}
	}
	publish := workflow.Jobs["publish"]
	if publish.Environment != "nightly-release" || publish.If != "always() && github.ref == 'refs/heads/main' && needs.validation.result == 'success' && needs.build-artifacts.result == 'success' && (needs.build-artifacts.outputs.skip == 'true' || needs.apple-sign.result == 'success')" {
		t.Fatal("nightly publication must fail closed on validation/build/Apple failure while permitting already-published container retries")
	}
	for _, key := range []string{"tag", "version", "commit"} {
		if publish.Outputs[key] != "${{ needs.build-artifacts.outputs."+key+" }}" {
			t.Fatalf("container retry loses immutable source output %s", key)
		}
	}
	builds, signatures := 0, 0
	for name, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Uses, "goreleaser/goreleaser-action@") {
				builds++
				if name != "build-artifacts" {
					t.Fatal("nightly binaries may only be built before Apple signing")
				}
			}
			for key, value := range step.Env {
				if strings.HasPrefix(key, "APPLE_") && strings.Contains(value, "secrets.") && name != "apple-sign" {
					t.Fatal("nightly Apple secrets escaped the signing job")
				}
			}
			if step.Run == "./scripts/release/sign-apple-artifacts.sh dist" {
				signatures++
				if name != "apple-sign" || step.If != "" || step.Env["APPLE_RELEASE_CHANNEL"] != "nightly" || step.Env["APPLE_RELEASE_VERSION"] != "${{ needs.build-artifacts.outputs.version }}" || step.Env["APPLE_RELEASE_COMMIT"] != "${{ needs.build-artifacts.outputs.commit }}" {
					t.Fatal("nightly Apple signing must bind the canonical build identity without an optional step")
				}
				for _, key := range []string{"APPLE_DEVELOPER_ID_P12_BASE64", "APPLE_DEVELOPER_ID_P12_PASSWORD", "APPLE_NOTARY_KEY_P8"} {
					if step.Env[key] != "${{ secrets."+key+" }}" {
						t.Fatalf("nightly Apple signing lacks %s", key)
					}
				}
			}
			if strings.Contains(step.Uses, "actions/download-artifact@") && (name == "apple-sign" || name == "publish") {
				upstream := "build-artifacts"
				if name == "publish" {
					upstream = "apple-sign"
				}
				if step.With["artifact-ids"] != "${{ needs."+upstream+".outputs.artifact_id }}" || step.With["merge-multiple"] != "true" || step.With["name"] != "" {
					t.Fatalf("%s must consume this run's immutable upstream artifact ID", name)
				}
			}
		}
	}
	if builds != 1 || signatures != 1 {
		t.Fatal("nightly requires exactly one canonical build and Apple signing stage")
	}
	var final strings.Builder
	for _, step := range publish.Steps {
		final.WriteString(step.Run)
		final.WriteByte('\n')
	}
	previous := -1
	for _, gate := range []string{"test \"$COMMIT\" = \"$GITHUB_SHA\"", "sha256sum --check --strict checksums.txt", "Darwin_$arch.dmg", "test \"$non_darwin_sha256\" = \"$NON_DARWIN_SHA256\"", "sha256sum nightly-context/upgrade-compatibility.json", "cp nightly-context/upgrade-compatibility.json", "prepare-image-root.sh", "cmp nightly-context/nightly-sources.json", "dist/hikyo_*_Darwin_*.dmg", "create-nightly-manifest.sh", "cosign sign-blob", "nightly verify", "smoke-release-startup.sh", "TestPackagedNightlyReleaseUpgrade", "gh release create"} {
		position := strings.Index(final.String(), gate)
		if position <= previous {
			t.Fatalf("missing or reordered nightly publication gate %s", gate)
		}
		previous = position
	}
}

// Execute the workflow's actual pre/post-Apple guards. Rewriting checksums.txt
// together with changed payload bytes must not rebind Linux or Windows inputs.
func TestNightlyAppleTransferPreservesUnchangedBuildInputs(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/nightly.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	find := func(job, name string) mainValidationStep {
		t.Helper()
		for _, step := range workflow.Jobs[job].Steps {
			if step.Name == name {
				return step
			}
		}
		t.Fatalf("missing %s/%s", job, name)
		return mainValidationStep{}
	}
	bind := find("build-artifacts", "Bind untouched nightly archive bytes")
	verify := find("publish", "Verify untouched nightly archives and compatibility")
	if bind.If != "steps.identity.outputs.skip != 'true'" || verify.If != "needs.build-artifacts.outputs.skip != 'true'" ||
		bind.Env["VERSION"] != "${{ steps.identity.outputs.version }}" ||
		!reflect.DeepEqual(verify.Env, map[string]string{
			"VERSION":              "${{ needs.build-artifacts.outputs.version }}",
			"NON_DARWIN_SHA256":    "${{ needs.build-artifacts.outputs.non_darwin_sha256 }}",
			"COMPATIBILITY_SHA256": "${{ needs.build-artifacts.outputs.compatibility_sha256 }}",
		}) {
		t.Fatal("nightly transfer verification must use independent immutable build outputs on every new release")
	}
	version := "1.2.3-nightly.20261021.1.gaaaaaaaa"
	for _, mutation := range []string{"none", "Linux bytes", "Windows bytes", "bytes and checksums", "missing archive", "compatibility"} {
		t.Run(mutation, func(t *testing.T) {
			directory := t.TempDir()
			for _, subdirectory := range []string{"dist", "nightly-context", "runner-temp"} {
				if err := os.Mkdir(filepath.Join(directory, subdirectory), 0700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(name string, raw []byte) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(directory, name), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			linux := "hikyo_" + version + "_Linux_arm64.tar.gz"
			windows := "hikyo_" + version + "_Windows_x86_64.zip"
			var names []string
			for _, osName := range []string{"Linux", "Windows"} {
				extension := "tar.gz"
				if osName == "Windows" {
					extension = "zip"
				}
				for _, arch := range []string{"arm64", "x86_64"} {
					name := "hikyo_" + version + "_" + osName + "_" + arch + "." + extension
					names = append(names, name)
					write("dist/"+name, []byte("canonical "+name))
				}
			}
			compatibility := []byte("canonical compatibility declaration")
			write("nightly-context/upgrade-compatibility.json", compatibility)
			output := filepath.Join(directory, "outputs")
			run := func(script string, extra ...string) ([]byte, error) {
				cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", script)
				cmd.Dir = directory
				cmd.Env = append(os.Environ(), "VERSION="+version, "RUNNER_TEMP="+filepath.Join(directory, "runner-temp"), "GITHUB_OUTPUT="+output)
				cmd.Env = append(cmd.Env, extra...)
				return cmd.CombinedOutput()
			}
			if output, err := run(bind.Run); err != nil {
				t.Fatalf("bind canonical archives: %v\n%s", err, output)
			}
			bound, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			digest, ok := strings.CutPrefix(strings.TrimSpace(string(bound)), "non_darwin_sha256=")
			if !ok || len(digest) != 64 {
				t.Fatalf("missing independent archive digest: %s", bound)
			}
			switch mutation {
			case "Linux bytes", "bytes and checksums":
				write("dist/"+linux, []byte("changed Linux archive"))
			case "Windows bytes":
				write("dist/"+windows, []byte("changed Windows archive"))
			case "missing archive":
				if err := os.Remove(filepath.Join(directory, "dist", linux)); err != nil {
					t.Fatal(err)
				}
			case "compatibility":
				write("nightly-context/upgrade-compatibility.json", []byte("changed compatibility declaration"))
			}
			if mutation == "bytes and checksums" {
				var checksums strings.Builder
				for _, name := range names {
					raw, err := os.ReadFile(filepath.Join(directory, "dist", name))
					if err != nil {
						t.Fatal(err)
					}
					fmt.Fprintf(&checksums, "%x  %s\n", sha256.Sum256(raw), name)
				}
				write("dist/checksums.txt", []byte(checksums.String()))
				if output, err := run("cd dist && sha256sum --check --strict checksums.txt"); err != nil {
					t.Fatalf("rewritten checksum fixture must pass ordinary checksum verification: %v\n%s", err, output)
				}
			}
			result, err := run(verify.Run, "NON_DARWIN_SHA256="+digest, fmt.Sprintf("COMPATIBILITY_SHA256=%x", sha256.Sum256(compatibility)))
			if mutation == "none" && err != nil {
				t.Fatalf("unchanged build inputs rejected: %v\n%s", err, result)
			}
			if mutation != "none" && err == nil {
				t.Fatalf("changed %s accepted", mutation)
			}
		})
	}
}
