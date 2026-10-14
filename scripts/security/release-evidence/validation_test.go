package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/scripts/ci"
)

func passingAttacks() attackResult {
	r := attackResult{SchemaVersion: 1, Profile: "hikyo-artifact-adversarial-v1", HarnessVersion: 1, ArtifactSHA256: strings.Repeat("a", 64), BinaryVersion: "hikyo version 1.2.3", Steps: 8, StartedAt: time.Now().Add(-time.Minute), CompletedAt: time.Now().Add(-time.Second), Status: "passed", ExpectedCases: mandatoryCases, Configuration: requiredArtifactConfiguration(), AssertionMetric: "successful oracle groups, not individual predicates, HTTP requests, distinct attacks or coverage percentage"}
	for _, engine := range []string{"sqlite", "postgres"} {
		b := attackBackend{Engine: engine, Cleanup: "complete", Sequence: []string{"remove", "restore", "remove", "restore", "revoke", "restore", "restart", "probe"}}
		for _, id := range mandatoryCases {
			n := 1
			if id == "generated-sequence" {
				n = 24
			}
			b.Cases = append(b.Cases, attackCase{ID: id, Status: "passed", Expected: "deny", Observed: "denied", Assertions: n})
		}
		r.Backends = append(r.Backends, b)
	}
	return r
}

func TestMandatoryArtifactInventoryFailsClosed(t *testing.T) {
	mutations := map[string]func(*attackResult){
		"all-passing-but-missing-backend": func(r *attackResult) { r.Backends = r.Backends[:1] },
		"missing-configuration":           func(r *attackResult) { r.Configuration = nil },
		"relaxed-disclosure-window":       func(r *attackResult) { r.Configuration["disclosure_window"] = "300 seconds" },
		"missing-count-semantics":         func(r *attackResult) { r.AssertionMetric = "" },
		"inflated-count-semantics":        func(r *attackResult) { r.AssertionMetric = "distinct attacks" },
		"invented-profile":                func(r *attackResult) { r.Profile = "unreviewed-profile" },
		"no-cleanup":                      func(r *attackResult) { r.Backends[1].Cleanup = "failed" },
		"digest-substitution":             func(r *attackResult) { r.ArtifactSHA256 = strings.Repeat("b", 64) },
		"trimmed-expected-inventory":      func(r *attackResult) { r.ExpectedCases = r.ExpectedCases[:1] },
		"all-passing-but-missing-case":    func(r *attackResult) { r.Backends[1].Cases = r.Backends[1].Cases[:1] },
		"skipped-case":                    func(r *attackResult) { r.Backends[0].Cases[0].Status = "skipped" },
		"duplicate-case":                  func(r *attackResult) { r.Backends[0].Cases[1].ID = r.Backends[0].Cases[0].ID },
		"unexecuted-case":                 func(r *attackResult) { r.Backends[0].Cases[0].Assertions = 0 },
		"wrong-generated-count":           func(r *attackResult) { r.Backends[0].Cases[len(mandatoryCases)-1].Assertions = 1 },
		"no-generated-trace":              func(r *attackResult) { r.Backends[0].Sequence = nil },
		"future-completion":               func(r *attackResult) { r.CompletedAt = time.Now().Add(time.Hour) },
	}
	if err := validateAttacks(passingAttacks(), strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			r := passingAttacks()
			mutate(&r)
			if validateAttacks(r, strings.Repeat("a", 64)) == nil {
				t.Fatal("incomplete receipt accepted")
			}
		})
	}
}

func TestBrowserReceiptRequiresExactBinaryAndMandatoryControls(t *testing.T) {
	digest, source := strings.Repeat("a", 64), strings.Repeat("b", 40)
	valid := map[string]any{"schema_version": 1, "profile": "hikyo-browser-security-v1", "status": "passed", "artifact_kind": "provided-binary", "binary_sha256": digest, "source_sha": source, "datastore": "sqlite", "authentication": "native-password-totp", "fixture_configuration": passingBrowserConfiguration(), "elapsed_ms": 100, "required_tests": []string{"stored-content", "hostile-origin", "oracle-negative-controls"}, "tests": []map[string]any{{"id": "stored-content", "status": "passed", "duration_ms": 1}, {"id": "hostile-origin", "status": "passed", "duration_ms": 1}, {"id": "oracle-negative-controls", "status": "passed", "duration_ms": 1}}}
	raw, _ := json.Marshal(valid)
	if err := validateBrowser(raw, digest, source); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"binary_sha256", "source_sha", "required_tests", "tests", "artifact_kind", "status", "fixture_configuration"} {
		t.Run(key, func(t *testing.T) {
			copy := map[string]any{}
			for k, v := range valid {
				copy[k] = v
			}
			delete(copy, key)
			raw, _ := json.Marshal(copy)
			if validateBrowser(raw, digest, source) == nil {
				t.Fatal("incomplete browser evidence accepted")
			}
		})
	}
}

func TestScanRequiresPinnedBinaryModeAndNoSymbolFindings(t *testing.T) {
	version := scannerPin(t)
	config := fmt.Sprintf(`{"config":{"protocol_version":"v1.0.0","scanner_name":"govulncheck","scanner_version":%q,"db":"https://vuln.go.dev","db_last_modified":"2026-10-14T00:00:00Z","go_version":"go1.26.0","scan_mode":"binary","scan_level":"symbol"}}`+"\n"+`{"SBOM":{"go_version":"go1.26.0","roots":["github.com/Hikyo-Org/hikyo/cmd/hikyo"]}}`, version)
	if _, err := validateScan([]byte(config)); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{"empty": "", "source-mode": strings.Replace(config, `"binary"`, `"source"`, 1), "wrong-version": strings.Replace(config, version, version+"-unreviewed", 1), "finding": config + "\n" + `{"finding":{"osv":"GO-TEST"}}`, "config-only": strings.Split(config, "\n")[0], "truncated": config + "\n{"} {
		t.Run(name, func(t *testing.T) {
			if _, err := validateScan([]byte(raw)); err == nil {
				t.Fatal("nonqualifying scan accepted")
			}
		})
	}
}

func scannerPin(t *testing.T) string {
	t.Helper()
	version, err := ci.GoToolVersion("golang.org/x/vuln")
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func TestScanUsesCanonicalToolPin(t *testing.T) {
	version := scannerPin(t)
	config := fmt.Sprintf(`{"config":{"protocol_version":"v1.0.0","scanner_name":"govulncheck","scanner_version":%q,"db":"https://vuln.go.dev","db_last_modified":"2026-10-14T00:00:00Z","scan_mode":"binary","scan_level":"symbol"}}`+"\n"+`{"SBOM":{"go_version":"go1.27.2","roots":["github.com/Hikyo-Org/hikyo/cmd/hikyo"]}}`, version)
	if result, err := validateScan([]byte(config)); err != nil || result.Version != version {
		t.Fatalf("canonical manifest pin %s rejected: %+v, %v", version, result, err)
	}
	for _, mismatch := range []string{"v0.0.0", "v1.6.0", "v1.7.0", version + "-unreviewed", version + "+unreviewed"} {
		if mismatch == version {
			continue
		}
		if _, err := validateScan([]byte(strings.Replace(config, version, mismatch, 1))); err == nil {
			t.Fatalf("unreviewed scanner version %s accepted", mismatch)
		}
	}
}

func TestRealGovulnJSONKeepsAdvisoriesSeparateFromSymbolPolicy(t *testing.T) {
	// Pinned govulncheck v1.7.0 emitted this module-only record for the locally
	// compiled source UI binary. Unused SBOM/OSV data is omitted from the fixture.
	// This is a parser regression, not a receipt about published release bytes.
	raw, err := os.ReadFile(filepath.Join("testdata", "govulncheck-binary-module-advisory.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// Preserve the historical raw fixture; only its config version is normalized
	// in memory so parser/advisory coverage survives a coordinated tool upgrade.
	raw = bytes.Replace(raw, []byte(`"scanner_version": "v1.7.0"`), []byte(fmt.Sprintf(`"scanner_version": %q`, scannerPin(t))), 1)
	r, err := validateScan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if r.ModuleFindings != 1 || r.PackageFindings != 0 || r.Findings != 0 || len(r.Advisories) != 1 || r.Advisories[0].OSV != "GO-2026-5932" {
		t.Fatalf("advisory hidden or misclassified: %+v", r)
	}
	for name, frame := range map[string]string{
		"package":              `"module":"golang.org/x/crypto","package":"golang.org/x/crypto/ssh"`,
		"symbol":               `"module":"golang.org/x/crypto","package":"golang.org/x/crypto/ssh","function":"NewClientConn"`,
		"malformed":            `"module":""`,
		"unknown-symbol-field": `"module":"golang.org/x/crypto","symbol":"NewClientConn"`,
		"receiver-only":        `"module":"golang.org/x/crypto","receiver":"Client"`,
	} {
		t.Run(name, func(t *testing.T) {
			// Keep actual config and compiler SBOM, replacing only the trace fixture.
			prefix := string(raw[:bytes.Index(raw, []byte(`{"finding"`))])
			changed := []byte(prefix + `{"finding":{"osv":"GO-2026-5932","trace":[{` + frame + `}]}}`)
			got, err := validateScan(changed)
			if name == "package" {
				if err != nil || got.PackageFindings != 1 || got.ModuleFindings != 0 {
					t.Fatalf("package advisory policy: %+v %v", got, err)
				}
			} else if err == nil {
				t.Fatal("blocking or unclassifiable finding accepted")
			}
		})
	}
}

func TestArchiveExecutableExtractionRefusesAmbiguousOrLinkedBytes(t *testing.T) {
	for name, headers := range map[string][]tar.Header{
		"regular":   {{Name: "hikyo", Mode: 0700, Size: 3, Typeflag: tar.TypeReg}},
		"symlink":   {{Name: "hikyo", Linkname: "/bin/sh", Typeflag: tar.TypeSymlink}},
		"nested":    {{Name: "bin/hikyo", Size: 3, Typeflag: tar.TypeReg}},
		"duplicate": {{Name: "hikyo", Size: 3, Typeflag: tar.TypeReg}, {Name: "hikyo", Size: 3, Typeflag: tar.TypeReg}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "app.tar.gz")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(f)
			tw := tar.NewWriter(gz)
			for _, h := range headers {
				if err := tw.WriteHeader(&h); err != nil {
					t.Fatal(err)
				}
				if h.Size > 0 {
					_, _ = tw.Write([]byte("app"))
				}
			}
			_ = tw.Close()
			_ = gz.Close()
			_ = f.Close()
			dest := filepath.Join(dir, "hikyo")
			digest, err := extractBinary(archive, dest)
			if name == "regular" {
				if err != nil || digest != hashBytes([]byte("app")) {
					t.Fatalf("extraction: %s %v", digest, err)
				}
			} else {
				if err == nil {
					t.Fatal("unsafe archive accepted")
				}
				if _, err := os.Stat(dest); !os.IsNotExist(err) {
					t.Fatal("refused archive wrote executable")
				}
			}
		})
	}
}

func TestNoPassDirectoryWithoutValidatedInputs(t *testing.T) {
	out := filepath.Join(t.TempDir(), "pass")
	if err := run([]string{"publish", "--out", out, "--datastore-cleaned", "--harness", strings.Repeat("a", 40), "--run-url", "https://github.com/Hikyo-Org/Hikyo/actions/runs/1/attempts/1"}); err == nil {
		t.Fatal("missing authenticated evidence accepted")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("failure produced a PASS directory")
	}
}

func TestPublisherWritesOnlyExactCompleteProfile(t *testing.T) {
	for _, scenario := range []string{"passed", "missing-cleanup", "missing-postgres", "wrong-browser-digest", "unreviewed-profile", "vulnerability-finding"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "pass")
			source, digest := strings.Repeat("b", 40), strings.Repeat("a", 64)
			p := prepared{Tag: "v1.2.3", Identity: releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.2.3", Sequence: 1, Commit: source, ManifestSHA256: releaseidentity.Digest(strings.Repeat("c", 64)), CompatibilitySHA256: releaseidentity.Digest(strings.Repeat("d", 64))}, Archive: releasetrust.Artifact{Name: "hikyo_1.2.3_Linux_x86_64.tar.gz", Platform: "linux/amd64", Kind: "binary", SHA256: strings.Repeat("e", 64)}, BinarySHA256: digest}
			attacks := passingAttacks()
			browser := map[string]any{"schema_version": 1, "profile": "hikyo-browser-security-v1", "status": "passed", "artifact_kind": "provided-binary", "binary_sha256": digest, "source_sha": source, "datastore": "sqlite", "authentication": "native-password-totp", "fixture_configuration": passingBrowserConfiguration(), "elapsed_ms": 100, "required_tests": []string{"stored-content", "hostile-origin", "oracle-negative-controls"}, "tests": []map[string]any{{"id": "stored-content", "status": "passed", "duration_ms": 1}, {"id": "hostile-origin", "status": "passed", "duration_ms": 1}, {"id": "oracle-negative-controls", "status": "passed", "duration_ms": 1}}}
			scan := fmt.Sprintf(`{"config":{"protocol_version":"v1.0.0","scanner_name":"govulncheck","scanner_version":%q,"db":"https://vuln.go.dev","db_last_modified":"2026-10-14T00:00:00Z","scan_mode":"binary","scan_level":"symbol"}}`+"\n"+`{"SBOM":{"go_version":"go1.27.2","roots":["github.com/Hikyo-Org/hikyo/cmd/hikyo"]}}`, scannerPin(t))
			switch scenario {
			case "missing-postgres":
				attacks.Backends = attacks.Backends[:1]
			case "wrong-browser-digest":
				browser["binary_sha256"] = strings.Repeat("f", 64)
			case "unreviewed-profile":
				attacks.Profile = "other"
			case "vulnerability-finding":
				scan += "\n" + `{"finding":{"osv":"GO-TEST"}}`
			}
			for name, value := range map[string]any{"prepared.json": p, "attacks.json": attacks, "browser.json": browser} {
				if err := writeJSON(filepath.Join(dir, name), value); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "scan.jsonl"), []byte(scan), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"publish", "--prepared", filepath.Join(dir, "prepared.json"), "--result", filepath.Join(dir, "attacks.json"), "--browser", filepath.Join(dir, "browser.json"), "--scan", filepath.Join(dir, "scan.jsonl"), "--scan-completed-at", time.Now().UTC().Format(time.RFC3339), "--harness", source, "--run-url", "https://github.com/Hikyo-Org/Hikyo/actions/runs/1/attempts/1", "--out", out}
			if scenario != "missing-cleanup" {
				args = append(args, "--datastore-cleaned")
			}
			err := run(args)
			if scenario != "passed" {
				if err == nil {
					t.Fatal("nonqualifying evidence published")
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatal("refused profile produced PASS artifacts")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			html, err := os.ReadFile(filepath.Join(out, "security-evidence.html"))
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{"v1.2.3: runtime regression profile passed", digest, "disposable_postgres_removed", "CodeQL", "oracle-negative-controls"} {
				if !strings.Contains(string(html), required) {
					t.Fatalf("HTML missing full evidence: %s", required)
				}
			}
			if strings.Contains(string(html), "<script src=") || strings.Contains(string(html), "<link ") {
				t.Fatal("HTML depends on network assets")
			}
		})
	}
}

func passingBrowserConfiguration() map[string]any {
	return map[string]any{
		"transport": "loopback-http-dev", "admission_limits": "product-default",
		"service_budgets": "product-default", "fake_adapter_provider": false,
		"child_environment": "allowlist", "development_disclosure_window_seconds": 300,
		"protected_disclosure_window_seconds": 0, "external_identity_providers": false,
	}
}

func TestBrowserFixtureConfigurationCannotInheritWeakenedControls(t *testing.T) {
	digest, source := strings.Repeat("a", 64), strings.Repeat("b", 40)
	for _, field := range []string{"transport", "admission_limits", "service_budgets", "fake_adapter_provider", "child_environment", "development_disclosure_window_seconds", "protected_disclosure_window_seconds", "external_identity_providers"} {
		t.Run(field, func(t *testing.T) {
			for _, missing := range []bool{false, true} {
				config := passingBrowserConfiguration()
				if missing {
					delete(config, field)
				} else {
					switch config[field].(type) {
					case bool:
						config[field] = true
					case int:
						config[field] = 600
					default:
						config[field] = "inherited-relaxed-fixture"
					}
				}
				report := map[string]any{"schema_version": 1, "profile": "hikyo-browser-security-v1", "status": "passed", "artifact_kind": "provided-binary", "binary_sha256": digest, "source_sha": source, "datastore": "sqlite", "authentication": "native-password-totp", "fixture_configuration": config, "elapsed_ms": 100, "required_tests": []string{"stored-content", "hostile-origin", "oracle-negative-controls"}, "tests": []map[string]any{{"id": "stored-content", "status": "passed", "duration_ms": 1}, {"id": "hostile-origin", "status": "passed", "duration_ms": 1}, {"id": "oracle-negative-controls", "status": "passed", "duration_ms": 1}}}
				raw, err := json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
				if validateBrowser(raw, digest, source) == nil {
					t.Fatal("absent or weakened browser fixture configuration accepted")
				}
			}
		})
	}
}
