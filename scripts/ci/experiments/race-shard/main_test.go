package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func encoded(events ...event) string {
	var b bytes.Buffer
	for _, e := range events {
		_ = json.NewEncoder(&b).Encode(e)
	}
	return b.String()
}
func ev(action, name string) event {
	return event{Action: action, Package: prefix + "internal/fixture", Test: name}
}
func fixtureEntry() entry {
	return entry{Package: prefix + "internal/fixture", Targets: []string{"TestOne"}}
}
func TestPositiveReceiptsAndNegativeMutations(t *testing.T) {
	base := []event{ev("start", ""), ev("run", "TestOne"), ev("run", "TestOne/child"), ev("pass", "TestOne/child"), ev("pass", "TestOne"), ev("pass", "")}
	_, targets, err := validateEvents(fixtureEntry(), strings.NewReader(encoded(base...)))
	if err != nil {
		t.Fatal(err)
	}
	if targets["TestOne/child"] != "pass" {
		t.Fatal("subtest outcome lost")
	}
	cases := map[string][]event{
		"zero exit and package pass without tests": {ev("start", ""), ev("pass", "")},
		"missing root terminal":                    {ev("start", ""), ev("run", "TestOne"), ev("pass", "")},
		"missing subtest terminal":                 {ev("start", ""), ev("run", "TestOne"), ev("run", "TestOne/child"), ev("pass", "TestOne"), ev("pass", "")},
		"unexpected root":                          {ev("start", ""), ev("run", "TestOther"), ev("pass", "TestOther"), ev("pass", "")},
		"duplicate run":                            {ev("start", ""), ev("run", "TestOne"), ev("run", "TestOne"), ev("pass", "TestOne"), ev("pass", "")},
		"unjustified root skip":                    {ev("start", ""), ev("run", "TestOne"), ev("skip", "TestOne"), ev("pass", "")},
		"postgres subtest skip":                    {ev("start", ""), ev("run", "TestOne"), ev("run", "TestOne/postgres"), ev("skip", "TestOne/postgres"), ev("pass", "TestOne"), ev("pass", "")},
		"failure":                                  {ev("start", ""), ev("run", "TestOne"), ev("fail", "TestOne"), ev("fail", "")},
		"missing package terminal":                 {ev("start", ""), ev("run", "TestOne"), ev("pass", "TestOne")},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := validateEvents(fixtureEntry(), strings.NewReader(encoded(events...))); err == nil {
				t.Fatal("false green accepted")
			}
		})
	}
	if _, _, err := validateEvents(fixtureEntry(), strings.NewReader("not JSON")); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	noTests := entry{Package: prefix + "internal/fixture", NoTestFiles: true}
	if status, _, err := validateEvents(noTests, strings.NewReader(encoded(ev("start", ""), ev("skip", "")))); err != nil || status != "skip" {
		t.Fatalf("no-test classification: %s %v", status, err)
	}
	if _, _, err := validateEvents(noTests, strings.NewReader(encoded(ev("start", ""), ev("pass", "")))); err == nil {
		t.Fatal("invented no-test PASS accepted")
	}
}
func TestOnlyDocumentedCosignSkip(t *testing.T) {
	pkg := prefix + "internal/releasetrust"
	name := "TestCosignLegacyBundleInProcessInterop"
	e := entry{Package: pkg, Targets: []string{name}}
	events := []event{{Action: "start", Package: pkg}, {Action: "run", Package: pkg, Test: name}, {Action: "output", Package: pkg, Test: name, Output: "    cosign_interop_test.go:17: release fixture job supplies pinned COSIGN_BIN\n"}, {Action: "skip", Package: pkg, Test: name}, {Action: "pass", Package: pkg}}
	if _, out, err := validateEvents(e, strings.NewReader(encoded(events...))); err != nil || out[name] != "skip" {
		t.Fatalf("allowed skip: %v", err)
	}
	events[2].Output = "different skip reason\n"
	if _, _, err := validateEvents(e, strings.NewReader(encoded(events...))); err == nil {
		t.Fatal("wrong skip reason accepted")
	}
}
func TestDiscoveryAndFilterConservation(t *testing.T) {
	dir := t.TempDir()
	source := `package fixture
import "testing"
func TestOne(t *testing.T) {}
func Testlower(t *testing.T) {}
func TestÉ(t *testing.T) {}
func FuzzOne(f *testing.F) {}
func TestMain(m *testing.M) {}
func ExampleOne() { println("ok") // Output: ok
}
func ExampleOne_empty() { // Output:
}
func ExampleOne_noOutput() {}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	names, err := discover(metadata{Dir: dir, TestGoFiles: []string{"fixture_test.go"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ExampleOne", "ExampleOne_empty", "FuzzOne", "TestOne", "TestÉ"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("discovery got %v want %v", names, want)
	}
	got, err := selectTargets(names, "^(TestOne|FuzzOne)$")
	if err != nil || !reflect.DeepEqual(got, []string{"FuzzOne", "TestOne"}) {
		t.Fatalf("selection %v %v", got, err)
	}
	for _, filter := range []string{"TestOne", "^(Missing)$", "^(TestOne|TestOne)$", "^(TestOne.*)$", "^(TestOne)$\textra"} {
		if _, err := selectTargets(names, filter); err == nil {
			t.Fatalf("invalid filter accepted %q", filter)
		}
	}
}
func TestExactInvocationFlags(t *testing.T) {
	e := fixtureEntry()
	e.Filter = "^(TestOne)$"
	m := manifest{Entries: []entry{e}}
	args := []string{"test", "-json", "-race", "-p", "2", "-timeout=20m", "-vet=off", "-count=1", "-run", e.Filter, e.Package}
	if _, err := matchingEntry(m, args); err != nil {
		t.Fatal(err)
	}
	for i := range args {
		bad := append([]string{}, args...)
		bad[i] += "changed"
		if _, err := matchingEntry(m, bad); err == nil {
			t.Fatalf("changed argument accepted index %d", i)
		}
	}
	if _, err := matchingEntry(m, append(args, "-short")); err == nil {
		t.Fatal("extra flag accepted")
	}
}
func TestFrozenInventoryDigestAndManifestRefusal(t *testing.T) {
	if digest(frozenPlan) != planSHA {
		t.Fatal("frozen shard changed")
	}
	var expected map[string][]string
	if err := json.Unmarshal(frozenExpected, &expected); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, names := range expected {
		total += len(names)
	}
	if len(expected) != 16 || total != 367 {
		t.Fatalf("inventory %d packages %d roots", len(expected), total)
	}
	name := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(name, []byte(`{"SourceSHA":"unknown","Entries":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(name); err == nil {
		t.Fatal("malformed inventory accepted")
	}
}
func TestTinyNativeBatchAndTest2JSONParity(t *testing.T) {
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRIAL_REAL_GO", realGo)
	t.Setenv("CGO_ENABLED", "1")
	t.Setenv("GOFLAGS", "-mod=readonly")
	root := t.TempDir()
	for _, dir := range []string{"internal/fixture", "internal/empty"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{"go.mod": "module github.com/Hikyo-Org/hikyo\n\ngo 1.27.2\n", "internal/fixture/fixture.go": "package fixture\n", "internal/fixture/fixture_test.go": `package fixture
import "testing"
func TestOne(t *testing.T) { t.Run("child",func(t *testing.T){}) }
`, "internal/empty/empty.go": "package empty\n"}
	for name, s := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e := fixtureEntry()
	e.Dir = filepath.Join(root, "internal/fixture")
	e.Binary = "fixture.test"
	empty := entry{Package: prefix + "internal/empty", Dir: filepath.Join(root, "internal/empty"), Binary: "empty.test", NoTestFiles: true}
	m := manifest{Root: root, Entries: []entry{e, empty}}
	bin := filepath.Join(root, "binaries")
	if err := compile(m, bin); err != nil {
		t.Fatal(err)
	}
	baseline := command(root, "test", "-json", "-race", "-p", "2", "-timeout=20m", "-vet=off", "-count=1", e.Package)
	raw, err := baseline.Output()
	if err != nil {
		t.Fatal(err)
	}
	_, a, err := validateEvents(e, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	direct := command(e.Dir, "tool", "test2json", "-t", "-p", e.Package, filepath.Join(bin, e.Binary), "-test.v=test2json", "-test.paniconexit0", "-test.count=1", "-test.timeout=20m")
	raw, err = direct.Output()
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := validateEvents(e, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("native/direct outcome mismatch %v vs %v", a, b)
	}
	raw, err = command(root, "test", "-json", "-race", "-p", "2", "-timeout=20m", "-vet=off", "-count=1", empty.Package).Output()
	if err != nil {
		t.Fatal(err)
	}
	if status, _, err := validateEvents(empty, bytes.NewReader(raw)); err != nil || status != "skip" {
		t.Fatalf("native no-test: %s %v", status, err)
	}
	// Even a package without tests must fail its complete batch if it cannot build.
	if err := os.WriteFile(filepath.Join(empty.Dir, "empty.go"), []byte("package empty\nvar _ = missingName\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := compile(m, filepath.Join(root, "bad-binaries")); err == nil {
		t.Fatal("unbuildable no-test package accepted")
	}
}

// Drive the real invocation handler with a zero-exit fake Go driver. Success
// from a process is deliberately insufficient proof that selected tests ran.
func TestInvocationRefusesZeroExitFalseGreenAndMissingBinary(t *testing.T) {
	root := t.TempDir()
	var ref map[string][]string
	if err := json.Unmarshal(frozenExpected, &ref); err != nil {
		t.Fatal(err)
	}
	m := manifest{SourceSHA: sourceSHA, PlanSHA: planSHA, SchedulerSHA: schedulerSHA, Root: root}
	for _, line := range strings.Split(strings.TrimSuffix(string(frozenPlan), "\n"), "\n") {
		parts := strings.Split(line, "\t")
		e := entry{Index: len(m.Entries), Package: parts[0], Dir: root, Binary: filepath.Base(parts[0]) + ".test", Targets: ref[strings.TrimPrefix(parts[0], prefix)]}
		if len(parts) == 2 {
			e.Filter = parts[1]
		}
		e.NoTestFiles = len(e.Targets) == 0
		m.Entries = append(m.Entries, e)
	}
	manifestFile := filepath.Join(root, "manifest.json")
	if err := writeJSON(manifestFile, m); err != nil {
		t.Fatal(err)
	}
	records := filepath.Join(root, "records")
	if err := os.Mkdir(records, 0700); err != nil {
		t.Fatal(err)
	}
	e := m.Entries[0]
	fake := filepath.Join(root, "go")
	script := "#!/bin/sh\nprintf '%s\\n' '" + encoded(event{Action: "start", Package: e.Package}, event{Action: "pass", Package: e.Package}) + "'\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRIAL_REAL_GO", fake)
	t.Setenv("TRIAL_MANIFEST", manifestFile)
	t.Setenv("TRIAL_MODE", "baseline")
	t.Setenv("TRIAL_RECORDS", records)
	args := []string{"test", "-json", "-race", "-p", "2", "-timeout=20m", "-vet=off", "-count=1", "-run", e.Filter, e.Package}
	if err := invoke(args); err != nil {
		t.Fatal(err)
	}
	var r receipt
	if err := readJSON(filepath.Join(records, "00.receipt.json"), &r); err != nil {
		t.Fatal(err)
	}
	if r.Exit != 0 {
		t.Fatal("fake driver must demonstrate actual zero exit")
	}
	f, err := os.Open(filepath.Join(records, "00.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, err := validateEvents(e, f); err == nil {
		t.Fatal("zero-exit false green accepted")
	}
	if err := invoke(args); err == nil {
		t.Fatal("duplicate package invocation accepted")
	}
	t.Setenv("TRIAL_MODE", "candidate")
	candidate := filepath.Join(root, "candidate")
	if err := os.Mkdir(candidate, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRIAL_RECORDS", candidate)
	t.Setenv("TRIAL_BINARIES", filepath.Join(root, "missing-binaries"))
	if err := invoke(args); err == nil {
		t.Fatal("missing candidate binary accepted")
	}
	// Even otherwise well-shaped manifests must conserve exact frozen selectors.
	m.Entries[0].Filter = "^(TestOther)$"
	if err := writeJSON(manifestFile, m); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(manifestFile); err == nil {
		t.Fatal("changed frozen selector accepted")
	}
}
