package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/importer"
)

func importDiagnosticLines(output string) string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "hikyo: ") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func TestValuesImportDiagnostics(t *testing.T) {
	for _, mode := range []string{"artifact", "dotenv"} {
		for level := 0; level <= 3; level++ {
			t.Run(fmt.Sprintf("%s/level%d", mode, level), func(t *testing.T) {
				calls := 0
				ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/values/import") {
						t.Errorf("unexpected request: %s", r.Method)
						http.NotFound(w, r)
						return
					}
					calls++
					var body apigen.ImportValuesRequest
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						return
					}
					if len(body.Entries) != 1 || body.Entries[0].Value != "value-sentinel" {
						t.Errorf("incorrect import payload")
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(apigen.ImportValuesResult{Imported: []string{"SECRET_SENTINEL"}})
				}))
				path := filepath.Join(t.TempDir(), "path-sentinel")
				var data []byte
				flag := "--file"
				if mode == "dotenv" {
					flag = "--from-dotenv"
					data = []byte("SECRET_SENTINEL=value-sentinel\n")
				} else {
					var err error
					data, err = importer.Encode(importer.ValuesFile{FormatVersion: importer.RunArtifactFormatVersion, CommitmentKey: strings.Repeat("ab", 32), Project: "prj_70", Environment: "env_70", Entries: []importer.ValuesEntry{{Key: "SECRET_SENTINEL", Value: "value-sentinel"}}})
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				args := []string{"values", "import", flag, path, "--instance", "local", "--org", "org_70", "--project", "prj_70", "--env", "env_70", "-o", "json"}
				if level > 0 {
					args = append([]string{"-" + strings.Repeat("v", level)}, args...)
				}
				if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
					t.Fatalf("exit %d: %s", code, stderr.String())
				}
				if calls != 1 {
					t.Fatalf("imports=%d", calls)
				}
				var result apigen.ImportValuesResult
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					t.Fatalf("stdout is not JSON: %v", err)
				}
				if len(result.Imported) != 1 || result.Imported[0] != "SECRET_SENTINEL" {
					t.Fatalf("stdout changed: %s", stdout.String())
				}
				output := importDiagnosticLines(stderr.String())
				for _, secret := range []string{"SECRET_SENTINEL", "value-sentinel", "path-sentinel", "env_70", "prj_70", "test-token"} {
					if strings.Contains(output, secret) {
						t.Fatalf("diagnostic leaked %q: %s", secret, output)
					}
				}
				if level == 0 {
					if output != "" {
						t.Fatalf("default diagnostics: %s", output)
					}
					return
				}
				for _, phase := range []string{"values import: preparing import", "values import: reading and parsing", "values import: applying values", "values import: complete"} {
					if !strings.Contains(output, phase) {
						t.Errorf("missing phase %q: %s", phase, output)
					}
				}
				if strings.Contains(output, "entries=1") != (level >= 2) {
					t.Errorf("wrong detail level: %s", output)
				}
				if strings.Contains(output, "values import apply elapsed=") != (level >= 3) {
					t.Errorf("wrong timing level: %s", output)
				}
			})
		}
	}
}

func TestValuesImportDiagnosticsStopOnFailure(t *testing.T) {
	for _, failure := range []string{"parse", "apply"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"error":{"code":"undeclared_key","message":"rejected"}}`))
			}))
			body := "SECRET_SENTINEL=value-sentinel\n"
			if failure == "parse" {
				body = "invalid dotenv line"
			}
			path := filepath.Join(t.TempDir(), "path-sentinel")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			code := cli.Run(t.Context(), ios, []string{"-vvv", "values", "import", "--from-dotenv", path, "--instance", "local", "--org", "org_70", "--project", "prj_70", "--env", "env_70"})
			if code == cli.ExitOK {
				t.Fatal("failed import succeeded")
			}
			output := importDiagnosticLines(stderr.String())
			if strings.Contains(output, "values import: complete") || strings.Contains(output, "values import: imported=") {
				t.Fatalf("false completion: %s", output)
			}
			if failure == "parse" && (calls != 0 || strings.Contains(output, "values import: applying values")) {
				t.Fatalf("parse error reached apply: calls=%d %s", calls, output)
			}
			if failure == "apply" && calls != 1 {
				t.Fatalf("apply calls=%d", calls)
			}
			if stdout.Len() != 0 {
				t.Fatalf("failure emitted stdout: %s", stdout.String())
			}
		})
	}
}

func TestSourceImportDiagnosticsAuthorArtifacts(t *testing.T) {
	for level := 0; level <= 3; level++ {
		t.Run(fmt.Sprintf("level%d", level), func(t *testing.T) {
			calls := 0
			ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if !strings.HasSuffix(r.URL.Path, "/values/occurrences") {
					t.Errorf("import attempted non-presence request")
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"definitions_revision":0,"items":[{"name":"SECRET_SENTINEL","declared":false,"set":false,"token":"occurrence-sentinel"}]}`))
			}))
			source := filepath.Join(t.TempDir(), "source-path-sentinel")
			if err := os.WriteFile(source, []byte("apiVersion: v1\nkind: Secret\nmetadata:\n  name: source-sentinel\nstringData:\n  SECRET_SENTINEL: value-sentinel\n"), 0600); err != nil {
				t.Fatal(err)
			}
			outDir := filepath.Join(t.TempDir(), "artifacts")
			args := []string{"import", "--from", "k8s", "--file", source, "--out-dir", outDir, "--instance", "local", "--org", "org_70", "--project", "prj_70", "--environment", "env_70"}
			if level > 0 {
				args = append([]string{"-" + strings.Repeat("v", level)}, args...)
			}
			if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
				t.Fatalf("exit=%d: %s", code, stderr.String())
			}
			if calls != 1 {
				t.Fatalf("presence requests=%d", calls)
			}
			if !strings.Contains(stdout.String(), "definitions-bundle.json") {
				t.Fatalf("artifact report missing: %s", stdout.String())
			}
			values, err := os.ReadFile(filepath.Join(outDir, "values-env_70.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(values), "value-sentinel") {
				t.Fatal("values artifact lost source value")
			}
			output := importDiagnosticLines(stderr.String())
			for _, secret := range []string{"SECRET_SENTINEL", "value-sentinel", "source-path-sentinel", "source-sentinel", "env_70", "prj_70", "test-token"} {
				if strings.Contains(output, secret) {
					t.Fatalf("leaked %q: %s", secret, output)
				}
			}
			if level == 0 {
				if output != "" {
					t.Fatalf("default diagnostics: %s", output)
				}
				return
			}
			for _, phase := range []string{"import: reading and parsing source", "import: checking existing values", "import: writing review artifacts", "import: review artifacts ready; values have not been applied"} {
				if !strings.Contains(output, phase) {
					t.Errorf("missing %q: %s", phase, output)
				}
			}
			if strings.Contains(output, "parsed records=1") != (level >= 2) {
				t.Errorf("wrong detail level: %s", output)
			}
			if strings.Contains(output, "import artifact write elapsed=") != (level >= 3) {
				t.Errorf("wrong timing level: %s", output)
			}
		})
	}
}

func TestSourceImportDiagnosticsStopOnParsingFailure(t *testing.T) {
	calls := 0
	ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.NotFound(w, r) }))
	source := filepath.Join(t.TempDir(), "source-path-sentinel")
	if err := os.WriteFile(source, []byte("invalid source document"), 0600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "artifacts")
	code := cli.Run(t.Context(), ios, []string{"-vvv", "import", "--from", "k8s", "--file", source, "--out-dir", outDir, "--instance", "local", "--org", "org_70", "--project", "prj_70", "--environment", "env_70"})
	if code == cli.ExitOK {
		t.Fatal("invalid source succeeded")
	}
	output := importDiagnosticLines(stderr.String())
	for _, phase := range []string{"import: preparing candidate plan", "import: resolving authenticated target", "import: writing review artifacts", "import: review artifacts ready"} {
		if strings.Contains(output, phase) {
			t.Errorf("failed source reached %q: %s", phase, output)
		}
	}
	if calls != 0 || stdout.Len() != 0 {
		t.Fatalf("failed source made requests=%d stdout=%q", calls, stdout.String())
	}
	if _, err := os.Stat(outDir); !os.IsNotExist(err) {
		t.Fatalf("failed source created artifact directory: %v", err)
	}
}
