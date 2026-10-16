package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Exercise the shell fixture itself, so the mutation tests cover its extraction
// boundaries as well as the artifact checks used by the supply-chain job.
func TestWebFlowMatrixWorkflowArtifactReuse(t *testing.T) {
	repo := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(repo, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	stable := "          name: hikyo-app-${{ github.run_id }}"
	attempt := stable + "-${{ github.run_attempt }}"
	report := "\n      - name: Retain attempt-bound diagnostic report\n" +
		"        uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02 # v4.6.2\n" +
		"        with:\n          name: diagnostic-${{ github.run_id }}-${{ github.run_attempt }}\n" +
		"          path: diagnostic.json\n          overwrite: true\n"

	// This repository intentionally formats job headers at two spaces. Preserve
	// source text instead of reserializing YAML and changing the fixture's input.
	jobHeaders := regexp.MustCompile(`(?m)^  [A-Za-z0-9_-]+:`)
	mutateJob := func(source, job string, change func(string) string) string {
		t.Helper()
		start := strings.Index(source, "\n  "+job+":\n")
		if start < 0 {
			t.Fatalf("missing mutation job %s", job)
		}
		start++
		end := len(source)
		for _, header := range jobHeaders.FindAllStringIndex(source[start:], -1) {
			if header[0] > 0 {
				end = start + header[0]
				break
			}
		}
		return source[:start] + change(source[start:end]) + source[end:]
	}
	replace := func(old, replacement string) func(string) string {
		return func(block string) string {
			t.Helper()
			if strings.Count(block, old) != 1 {
				t.Fatalf("mutation requires exactly one %q", old)
			}
			return strings.Replace(block, old, replacement, 1)
		}
	}
	appendStep := func(step string) func(string) string {
		return func(block string) string { return block + step }
	}
	type testCase struct {
		name     string
		workflow string
		refusal  string
	}
	tests := []testCase{
		{name: "current reports are allowed", workflow: workflow},
		{name: "producer report is allowed", workflow: mutateJob(workflow, "app-build", appendStep(report))},
		{name: "no-egress report is allowed", workflow: mutateJob(workflow, "web-go", appendStep(report))},
	}
	for _, job := range []string{"app-build", "web-go", "web", "source-artifact-security"} {
		tests = append(tests, testCase{
			name:     job + " attempt-bound app name is refused",
			workflow: mutateJob(workflow, job, replace(stable, attempt)),
			refusal:  job + " app artifact name is not stable across partial reruns",
		})
	}
	download := "\n      - name: Duplicate app download\n" +
		"        uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1\n" +
		"        with:\n" + attempt + "\n"
	reportDownload := "\n      - name: Download diagnostic report\n" +
		"        uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1\n" +
		"        with:\n          name: diagnostic-${{ github.run_id }}-${{ github.run_attempt }}\n"
	tests = append(tests,
		testCase{"consumer report is allowed", mutateJob(workflow, "web", appendStep(reportDownload)), ""},
		testCase{"duplicate consumer is refused", mutateJob(workflow, "web", appendStep(download)), "web must contain exactly one app artifact step"},
		testCase{"duplicate pattern consumer is refused", mutateJob(workflow, "web", appendStep(strings.Replace(download, "          name:", "          pattern:", 1))), "web must contain exactly one app artifact step"},
		testCase{"duplicate wildcard consumer is refused", mutateJob(workflow, "web", appendStep(strings.Replace(download, attempt, "          pattern: '*'", 1))), "web must contain exactly one app artifact step"},
		testCase{"duplicate unnamed consumer is refused", mutateJob(workflow, "web", appendStep(strings.Replace(download, "        with:\n"+attempt+"\n", "", 1))), "web must contain exactly one app artifact step"},
		testCase{"duplicate empty-name consumer is refused", mutateJob(workflow, "web", appendStep(strings.Replace(download, attempt, "          name: ''", 1))), "web must contain exactly one app artifact step"},
		testCase{"duplicate null-name consumer is refused", mutateJob(workflow, "web", appendStep(strings.Replace(download, attempt, "          name: null", 1))), "web must contain exactly one app artifact step"},
		testCase{"duplicate quoted consumer is refused", mutateJob(workflow, "web", appendStep(strings.Replace(download, attempt, "          name: 'hikyo-app-${{ github.run_id }}-${{ github.run_attempt }}'", 1))), "web must contain exactly one app artifact step"},
		testCase{"renamed app artifact is refused", mutateJob(workflow, "web", replace(stable, "          name: wrong-artifact")), "web app artifact name is not stable across partial reruns"},
		testCase{"environment cannot impersonate artifact input", mutateJob(workflow, "web", replace(stable, "          path: ci-artifacts\n        env:\n"+stable)), "web app artifact name is not stable across partial reruns"},
		testCase{"report cannot supply pinned download action", mutateJob(mutateJob(workflow, "web", replace("uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1", "uses: actions/download-artifact@latest")), "web", appendStep(reportDownload)), "web app artifact does not use the repository-pinned transfer action"},
		testCase{"adjacent job cannot supply no-egress probe", mutateJob(mutateJob(workflow, "web-go", replace("run: ./scripts/ci/no-egress.sh", "run: echo removed")), "source-artifact-security", appendStep("\n      - name: Unrelated probe\n        run: ./scripts/ci/no-egress.sh\n")), "no-egress probe is missing from web-go"},
		testCase{"adjacent job compilation is allowed", mutateJob(workflow, "source-artifact-security", appendStep("\n      - name: Unrelated build\n        run: go build ./cmd/hikyo\n")), ""},
		testCase{"report cannot supply overwrite", mutateJob(mutateJob(workflow, "app-build", replace("          overwrite: true", "          overwrite: false")), "app-build", appendStep(report)), "app-build artifact cannot be replaced by a full workflow rerun"},
		testCase{"no-egress compilation is refused", mutateJob(workflow, "web-go", appendStep("\n      - name: Duplicate build\n        run: go build ./cmd/hikyo\n")), "no-egress repeats app-build compilation"},
		testCase{"browser Go provisioning is refused", mutateJob(workflow, "web", appendStep("\n      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e\n")), "a browser shard still provisions Go or compiles a helper"},
		testCase{"browser frontend work is refused", mutateJob(workflow, "web", appendStep("\n      - name: Duplicate frontend build\n        run: pnpm run build\n")), "a browser shard repeats app-build frontend work"},
		testCase{"missing binary build is refused", mutateJob(workflow, "app-build", replace("go build -tags ui -o ci-artifacts/hikyo-ui ./cmd/hikyo", "echo removed")), "app-build does not produce the release-shaped CI binary"},
		testCase{"missing mail compilation is refused", mutateJob(workflow, "app-build", replace("go build -o ci-artifacts/mail-sink ./internal/mailtest/cmd", "echo removed")), "app-build does not produce the browser mail helper"},
		testCase{"missing mail upload is refused", mutateJob(workflow, "app-build", replace("            ci-artifacts/mail-sink", "            removed-mail-sink")), "app-build does not upload the browser mail helper"},
		testCase{"missing mail execute permission is refused", mutateJob(workflow, "web", replace("chmod +x ci-artifacts/hikyo-ui ci-artifacts/oidctest-idp ci-artifacts/mail-sink", "chmod +x ci-artifacts/hikyo-ui ci-artifacts/oidctest-idp")), "web does not restore every artifact executable mode"},
		testCase{"missing mail fixture environment is refused", mutateJob(workflow, "web", replace("HIKYO_E2E_MAIL_BINARY: ${{ github.workspace }}/ci-artifacts/mail-sink", "HIKYO_E2E_MAIL_BINARY: wrong-binary")), "web does not pass the prebuilt mail helper to the browser harness"},
		testCase{"missing executable restoration is refused", mutateJob(workflow, "web-go", replace("run: chmod +x ci-artifacts/hikyo-ui", "run: echo removed")), "no-egress does not restore execute permission after artifact download"},
		testCase{"missing downloaded probe binary is refused", mutateJob(workflow, "web-go", replace("HIKYO_NO_EGRESS_BIN: ${{ github.workspace }}/ci-artifacts/hikyo-ui", "HIKYO_NO_EGRESS_BIN: wrong-binary")), "no-egress does not pass the downloaded binary to the probe"},
		testCase{"serialized release snapshot is refused", mutateJob(workflow, "release-snapshot", replace("needs: changes", "needs: [changes, app-build]")), "release snapshot is serialized behind app-build"},
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var document yaml.Node
			if err := yaml.Unmarshal([]byte(tt.workflow), &document); err != nil {
				t.Fatalf("invalid mutation workflow: %v", err)
			}
			fixture := t.TempDir()
			for _, file := range []string{
				"scripts/ci/check-build-artifact-reuse_test.sh",
				"scripts/ci/check-no-egress-lifecycle_test.sh",
				"scripts/ci/no-egress.sh",
				"scripts/ci/build-spa.sh",
				"web/e2e/fixtures/instance.ts",
				"web/e2e/fixtures/mail.ts",
			} {
				data, err := os.ReadFile(filepath.Join(repo, file))
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(fixture, file)
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, data, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			workflowPath := filepath.Join(fixture, ".github", "workflows", "ci.yml")
			if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(workflowPath, []byte(tt.workflow), 0o644); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command("sh", filepath.Join(fixture, "scripts", "ci", "check-build-artifact-reuse_test.sh")).CombinedOutput()
			if tt.refusal == "" {
				if err != nil {
					t.Fatalf("valid workflow refused: %v\n%s", err, output)
				}
				return
			}
			if err == nil || !strings.Contains(string(output), tt.refusal) {
				t.Fatalf("want refusal %q; error %v\n%s", tt.refusal, err, output)
			}
		})
	}
}
