package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nativeTiming = "pkg: example.invalid/project/internal/service\nBenchmarkPublish-2    14    5644812 ns/op\n"

func processedTiming(name, uri string, ns float64) string {
	return fmt.Sprintf(`{"creator":{"name":"codspeed-go","version":"1.3.0","pid":54581},"instrument":{"type":"walltime"},"benchmarks":[{"name":%q,"uri":%q,"config":{},"stats":{"mean_ns":%g,"rounds":14}}]}`, name, uri, ns)
}

func TestVerifyTimerTailReproduction(t *testing.T) {
	// These values come from the pinned Go integration control: the same 5ms
	// operation printed 5.619ms but its broken timer tail uploaded only 8us.
	for _, test := range []struct {
		name    string
		native  float64
		mean    float64
		wantErr bool
	}{
		{"broken timer tail", 5619182, 8011.545454545455, true},
		{"operation final", 5644812, 5641050.642857143, false},
		{"too much untimed work included", 5644812, 24000000, true},
		{"upper boundary", 5644812, 11289624, false},
		{"lower boundary", 5644812, 2822406, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			log := fmt.Sprintf("pkg: example.invalid/project/internal/service\nBenchmarkPublish-2 14 %g ns/op\n", test.native)
			results := processedTiming("BenchmarkPublish", "internal/service/ux_benchmark_test.go::BenchmarkPublish", test.mean)
			err := verifyFixture(t, log, map[string]string{"54581.json": results})
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, want error %v", err, test.wantErr)
			}
			if test.wantErr && !strings.Contains(err.Error(), "measurement paths disagree") {
				t.Fatalf("wrong failure: %v", err)
			}
		})
	}
}

func TestVerifyNestedAndRepeatedNamesAcrossPackages(t *testing.T) {
	name := "BenchmarkTimer::case-with-hyphen::batch=2"
	log := "pkg: example.invalid/project/internal/one\nBenchmarkTimer/case-with-hyphen/batch=2-14 3 5000000 ns/op\n" +
		"pkg: example.invalid/project/internal/two\nBenchmarkTimer/case-with-hyphen/batch=2-2 3 5500000 ns/op\n"
	files := map[string]string{
		"1.json": processedTiming(name, "internal/one/timer_test.go::"+name, 5000000),
		"2.json": processedTiming(name, "internal/two/timer_test.go::"+name, 5500000),
	}
	if err := verifyFixture(t, log, files); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRootAndNestedPackageNames(t *testing.T) {
	log := "pkg: example.invalid/project\nBenchmarkTimer-2 3 5000000 ns/op\n" +
		"pkg: example.invalid/project/internal/two\nBenchmarkTimer-2 3 5500000 ns/op\n"
	files := map[string]string{
		"1.json": processedTiming("BenchmarkTimer", "timer_test.go::BenchmarkTimer", 5000000),
		"2.json": processedTiming("BenchmarkTimer", "internal/two/timer_test.go::BenchmarkTimer", 5500000),
	}
	if err := verifyFixture(t, log, files); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyMatchesDeepestPackageOwnerFirst(t *testing.T) {
	log := "pkg: example.invalid/project/internal/service\nBenchmarkTimer-2 3 5000000 ns/op\n" +
		"pkg: example.invalid/project/other/internal/service\nBenchmarkTimer-2 3 25000000 ns/op\n"
	files := map[string]string{
		"1.json": processedTiming("BenchmarkTimer", "internal/service/timer_test.go::BenchmarkTimer", 5000000),
		"2.json": processedTiming("BenchmarkTimer", "other/internal/service/timer_test.go::BenchmarkTimer", 25000000),
	}
	if err := verifyFixture(t, log, files); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyPreservesNumericCaseSuffix(t *testing.T) {
	log := "pkg: example.invalid/project/internal/service\nBenchmarkTimer/case-123-14 3 5000000 ns/op\n"
	files := map[string]string{
		"1.json": processedTiming("BenchmarkTimer::case-123", "internal/service/timer_test.go::BenchmarkTimer::case-123", 5000000),
	}
	if err := verifyFixture(t, log, files); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsMissingAmbiguousAndInvalidEvidence(t *testing.T) {
	valid := processedTiming("BenchmarkPublish", "internal/service/ux_benchmark_test.go::BenchmarkPublish", 5641050)
	for _, test := range []struct {
		name  string
		log   string
		files map[string]string
		want  string
	}{
		{"empty native", "PASS\n", map[string]string{"1.json": valid}, "no benchmark timings"},
		{"missing package", "BenchmarkPublish-2 14 5644812 ns/op\n", map[string]string{"1.json": valid}, "missing package"},
		{"duplicate native", nativeTiming + "BenchmarkPublish-2 14 5644812 ns/op\n", map[string]string{"1.json": valid}, "duplicate Go"},
		{"zero iterations", strings.Replace(nativeTiming, "14    ", "0    ", 1), map[string]string{"1.json": valid}, "invalid benchmark iterations"},
		{"zero native", strings.Replace(nativeTiming, "5644812", "0", 1), map[string]string{"1.json": valid}, "invalid benchmark duration"},
		{"malformed native", strings.Replace(nativeTiming, "5644812", "NaN", 1), map[string]string{"1.json": valid}, "invalid benchmark timing"},
		{"no results", nativeTiming, nil, "no JSON files"},
		{"bad JSON", nativeTiming, map[string]string{"1.json": "{"}, "unexpected end"},
		{"trailing JSON", nativeTiming, map[string]string{"1.json": valid + "{}"}, "invalid character"},
		{"wrong creator", nativeTiming, map[string]string{"1.json": strings.Replace(valid, "codspeed-go", "other", 1)}, "expected nonempty"},
		{"unqualified version", nativeTiming, map[string]string{"1.json": strings.Replace(valid, "1.3.0", "1.4.0", 1)}, "unqualified Go integration version"},
		{"wrong instrument", nativeTiming, map[string]string{"1.json": strings.Replace(valid, "walltime", "simulation", 1)}, "expected nonempty"},
		{"empty benchmarks", nativeTiming, map[string]string{"1.json": `{"creator":{"name":"codspeed-go","version":"1.3.0"},"instrument":{"type":"walltime"},"benchmarks":[]}`}, "expected nonempty"},
		{"zero mean", nativeTiming, map[string]string{"1.json": strings.Replace(valid, `"mean_ns":5.64105e+06`, `"mean_ns":0`, 1)}, "invalid mean"},
		{"overflowed mean", nativeTiming, map[string]string{"1.json": strings.Replace(valid, `"mean_ns":5.64105e+06`, `"mean_ns":1e400`, 1)}, "cannot unmarshal"},
		{"missing rounds", nativeTiming, map[string]string{"1.json": strings.Replace(valid, `"rounds":14`, `"rounds":0`, 1)}, "invalid mean"},
		{"identity mismatch", nativeTiming, map[string]string{"1.json": strings.Replace(valid, `"name":"BenchmarkPublish"`, `"name":"BenchmarkOther"`, 1)}, "invalid benchmark identity"},
		{"absolute URI", nativeTiming, map[string]string{"1.json": strings.Replace(valid, `"uri":"internal`, `"uri":"/internal`, 1)}, "invalid benchmark identity"},
		{"outside URI", nativeTiming, map[string]string{"1.json": strings.Replace(valid, `"uri":"internal`, `"uri":"../internal`, 1)}, "invalid benchmark identity"},
		{"duplicate results", nativeTiming, map[string]string{"1.json": valid, "2.json": valid}, "duplicate CodSpeed"},
		{"extra native", nativeTiming + "BenchmarkOther-2 3 5000000 ns/op\n", map[string]string{"1.json": valid}, "count mismatch"},
		{"different owner", nativeTiming, map[string]string{"1.json": strings.Replace(valid, "internal/service/", "internal/other/", 1)}, "matches 0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := verifyFixture(t, test.log, test.files)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestVerifyRequiresExplicitArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--log", "x"}, {"--results", "x"}, {"--unknown"}, {"--log", "x", "--results", "y", "extra"}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}

func verifyFixture(t *testing.T, log string, files map[string]string) error {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "stdout.log")
	if err := os.WriteFile(logPath, []byte(log), 0600); err != nil {
		t.Fatal(err)
	}
	results := filepath.Join(dir, "results")
	if err := os.Mkdir(results, 0700); err != nil {
		t.Fatal(err)
	}
	for filename, data := range files {
		if err := os.WriteFile(filepath.Join(results, filename), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	err := run([]string{"--log", logPath, "--results", results}, &output)
	if err == nil && !strings.Contains(output.String(), "CodSpeed timing validation passed") {
		t.Fatal("missing success evidence")
	}
	return err
}
