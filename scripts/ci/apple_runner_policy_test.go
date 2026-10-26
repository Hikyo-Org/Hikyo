package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppleNativeRunnerPolicy(t *testing.T) {
	root := t.TempDir()
	repo := repositoryRoot(t)
	files := []string{"scripts/ci/check-cache-policy_test.sh", "scripts/ci/go-tool-modules.txt", "scripts/ci/verify-docs.sh", "web/package.json", "docs/site/package.json"}
	entries, err := os.ReadDir(filepath.Join(repo, ".github/workflows"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".yml") || strings.HasSuffix(entry.Name(), ".yaml") {
			files = append(files, filepath.Join(".github/workflows", entry.Name()))
		}
	}
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Join(repo, name))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, ".github/workflows/nightly.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "\n  apple-acceptance:\n")
	end := strings.Index(source, "\n  publish:\n")
	if start < 0 || end <= start {
		t.Fatal("missing native acceptance job")
	}
	acceptance := source[start:end]
	for _, tc := range []struct {
		name, from, to, refusal string
	}{
		{name: "approved native pair"},
		{"different runner", "runner: macos-15-intel", "runner: macos-16-intel", "approved ARM and Intel runner pair"},
		{"swapped architecture", "- arch: x86_64", "- arch: arm64", "approved ARM and Intel runner pair"},
		{"extra architecture", "    runs-on:", "          - arch: extra\n            runner: macos-15\n    runs-on:", "exactly two architecture/runner pairs"},
		{"literal runner", "runs-on: ${{ matrix.runner }}", "runs-on: macos-15", "closed runner matrix"},
		{"shared cache", "actions/download-artifact@", "actions/cache/restore@", "must not receive credentials, shared caches or write authority"},
		{"setup cache", "actions/download-artifact@", "actions/setup-go@", "must not receive credentials, shared caches or write authority"},
		{"signing secret", "vars.APPLE_SIGNING_IDENTITY", "secrets.APPLE_SIGNING_IDENTITY", "must not receive credentials, shared caches or write authority"},
		{"write authority", "contents: read", "contents: write", "must not receive credentials, shared caches or write authority"},
		{"unrelated native job", "", "", "runner other than ubuntu-latest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := source
			if tc.from != "" {
				if !strings.Contains(acceptance, tc.from) {
					t.Fatal("mutation no longer matches native acceptance")
				}
				changed = source[:start] + strings.Replace(acceptance, tc.from, tc.to, 1) + source[end:]
			}
			if tc.name == "unrelated native job" {
				changed = strings.Replace(changed, "runs-on: ubuntu-latest", "runs-on: macos-15-intel", 1)
			}
			if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command("sh", filepath.Join(root, "scripts/ci/check-cache-policy_test.sh")).CombinedOutput()
			if tc.refusal == "" {
				if err != nil {
					t.Fatalf("approved native pair refused: %v\n%s", err, output)
				}
			} else if err == nil || !strings.Contains(string(output), tc.refusal) {
				t.Fatalf("unsafe runner policy must fail with %q: %v\n%s", tc.refusal, err, output)
			}
		})
	}
}
