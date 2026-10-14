// This temporary experiment measures one fixed cold Linux race shard.
package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const sourceSHA = "48189fff82e94b32f32637b1920464e278d9caff"
const planSHA = "af26e90992b16e6e0421f849113899532013ecf138165bf794c786c3e438c2c6"
const schedulerSHA = "ce4146252c72412ca95c8fe27bd42ab7f531abd2ce5c36829ea35e53f150cd58"
const prefix = "github.com/Hikyo-Org/hikyo/"

//go:embed shard0.tsv
var frozenPlan []byte

//go:embed expected.json
var frozenExpected []byte

type entry struct {
	Index                        int
	Package, Dir, Filter, Binary string
	Targets                      []string
	NoTestFiles                  bool
}
type manifest struct {
	SourceSHA, PlanSHA, SchedulerSHA, Root string
	Entries                                []entry
}
type metadata struct {
	ImportPath, Dir           string
	TestGoFiles, XTestGoFiles []string
	Error                     *struct{ Err string }
}
type receipt struct {
	Package, Filter, Mode string
	Args                  []string
	Start, End            time.Time
	Exit                  int
}
type event struct{ Action, Package, Test, Output string }
type result struct {
	Packages map[string]string
	Targets  map[string]map[string]string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "race shard trial:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected prepare, compile, invoke, validate or compare")
	}
	switch args[0] {
	case "prepare":
		if len(args) != 3 {
			return errors.New("prepare ROOT OUTPUT")
		}
		m, err := prepare(args[1])
		if err != nil {
			return err
		}
		return writeJSON(args[2], m)
	case "compile":
		if len(args) != 3 {
			return errors.New("compile MANIFEST BINARY_DIRECTORY")
		}
		m, err := readManifest(args[1])
		if err != nil {
			return err
		}
		return compile(m, args[2])
	case "invoke":
		return invoke(args[1:])
	case "validate":
		if len(args) != 4 {
			return errors.New("validate MANIFEST RECORDS RESULT")
		}
		m, err := readManifest(args[1])
		if err != nil {
			return err
		}
		r, err := validate(m, args[2])
		if err != nil {
			return err
		}
		return writeJSON(args[3], r)
	case "compare":
		if len(args) != 3 {
			return errors.New("compare BASELINE_RESULT CANDIDATE_RESULT")
		}
		var a, b result
		if err := readJSON(args[1], &a); err != nil {
			return err
		}
		if err := readJSON(args[2], &b); err != nil {
			return err
		}
		if !reflect.DeepEqual(a, b) {
			return errors.New("package or root target outcome sets differ")
		}
		return nil
	default:
		return errors.New("unknown command")
	}
}
func digest(b []byte) string { d := sha256.Sum256(b); return hex.EncodeToString(d[:]) }
func writeJSON(name string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(name, append(b, '\n'), 0600)
}
func readJSON(name string, v any) error {
	f, e := os.Open(name)
	if e != nil {
		return e
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		return e
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func readManifest(name string) (manifest, error) {
	var m manifest
	e := readJSON(name, &m)
	if e != nil {
		return m, e
	}
	if m.SourceSHA != sourceSHA || m.PlanSHA != planSHA || m.SchedulerSHA != schedulerSHA || len(m.Entries) != 16 {
		return m, errors.New("unrecognized manifest")
	}
	var expected map[string][]string
	if err := json.Unmarshal(frozenExpected, &expected); err != nil {
		return m, err
	}
	lines := strings.Split(strings.TrimSuffix(string(frozenPlan), "\n"), "\n")
	for i, e := range m.Entries {
		parts := strings.Split(lines[i], "\t")
		filter := ""
		if len(parts) == 2 {
			filter = parts[1]
		}
		names := expected[strings.TrimPrefix(parts[0], prefix)]
		if e.Index != i || e.Package != parts[0] || e.Filter != filter || e.Dir == "" || !filepath.IsAbs(e.Dir) || e.Binary != path.Base(e.Package)+".test" || !reflect.DeepEqual(e.Targets, names) || e.NoTestFiles != (len(names) == 0) {
			return m, errors.New("malformed manifest entry")
		}
	}
	return m, nil
}
func cleanEnv() []string {
	var env []string
	for _, s := range os.Environ() {
		if !strings.HasPrefix(s, "BASH_FUNC_go%%=") {
			env = append(env, s)
		}
	}
	return env
}
func command(root string, args ...string) *exec.Cmd {
	c := exec.Command(os.Getenv("TRIAL_REAL_GO"), args...)
	c.Dir = root
	c.Env = cleanEnv()
	return c
}
func prepare(root string) (manifest, error) {
	m := manifest{SourceSHA: sourceSHA, PlanSHA: planSHA, SchedulerSHA: schedulerSHA}
	var err error
	m.Root, err = filepath.Abs(root)
	if err != nil {
		return m, err
	}
	if digest(frozenPlan) != planSHA {
		return m, errors.New("frozen shard digest mismatch")
	}
	b, err := os.ReadFile(filepath.Join(m.Root, "scripts/ci/test-race-packages.sh"))
	if err != nil || digest(b) != schedulerSHA {
		return m, errors.New("scheduler does not match pinned source")
	}
	head := exec.Command("git", "-C", m.Root, "rev-parse", "HEAD")
	b, err = head.Output()
	if err != nil || strings.TrimSpace(string(b)) != sourceSHA {
		return m, errors.New("product checkout does not match source pin")
	}
	var reference map[string][]string
	if err = json.Unmarshal(frozenExpected, &reference); err != nil {
		return m, err
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(frozenPlan), "\n"), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) > 2 || parts[0] == "" {
			return m, errors.New("invalid frozen inventory")
		}
		e := entry{Index: len(m.Entries), Package: parts[0], Binary: path.Base(parts[0]) + ".test"}
		if len(parts) == 2 {
			e.Filter = parts[1]
		}
		m.Entries = append(m.Entries, e)
	}
	args := []string{"list", "-race", "-json"}
	for _, e := range m.Entries {
		args = append(args, e.Package)
	}
	c := command(m.Root, args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	b, err = c.Output()
	if err != nil {
		return m, fmt.Errorf("go list: %w: %s", err, stderr.String())
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	seen := map[string]bool{}
	binary := map[string]bool{}
	count := 0
	for {
		var p metadata
		err = dec.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			return m, err
		}
		if p.Error != nil {
			return m, errors.New(p.Error.Err)
		}
		if seen[p.ImportPath] {
			return m, errors.New("duplicate metadata")
		}
		seen[p.ImportPath] = true
		found := false
		for i := range m.Entries {
			e := &m.Entries[i]
			if e.Package != p.ImportPath {
				continue
			}
			found = true
			e.Dir = p.Dir
			e.NoTestFiles = len(p.TestGoFiles)+len(p.XTestGoFiles) == 0
			names, err := discover(p)
			if err != nil {
				return m, err
			}
			e.Targets, err = selectTargets(names, e.Filter)
			if err != nil {
				return m, err
			}
			if !reflect.DeepEqual(e.Targets, reference[strings.TrimPrefix(e.Package, prefix)]) {
				return m, fmt.Errorf("independent frozen target inventory differs: %s", e.Package)
			}
			if binary[e.Binary] {
				return m, errors.New("test binary basename collision")
			}
			binary[e.Binary] = true
			count += len(e.Targets)
		}
		if !found {
			return m, errors.New("unexpected package metadata")
		}
	}
	if len(seen) != 16 || count != 367 {
		return m, fmt.Errorf("incomplete fixed inventory: %d packages, %d targets", len(seen), count)
	}
	return m, nil
}
func runnable(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(name, prefix)
	if suffix == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(suffix)
	return !unicode.IsLower(r)
}
func discover(p metadata) ([]string, error) {
	names := map[string]bool{}
	var files []*ast.File
	for _, name := range append(append([]string{}, p.TestGoFiles...), p.XTestGoFiles...) {
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(p.Dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name == "TestMain" {
				continue
			}
			n := fn.Name.Name
			if runnable(n, "Test") || runnable(n, "Fuzz") {
				if names[n] {
					return nil, errors.New("duplicate test name")
				}
				names[n] = true
			}
		}
	}
	for _, ex := range doc.Examples(files...) {
		if ex.Output != "" || ex.EmptyOutput {
			names["Example"+ex.Name] = true
		}
	}
	out := []string{}
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}
func selectTargets(names []string, filter string) ([]string, error) {
	if filter == "" {
		return names, nil
	}
	if !regexp.MustCompile(`^\^\([A-Za-z0-9_]+(\|[A-Za-z0-9_]+)*\)\$$`).MatchString(filter) {
		return nil, errors.New("malformed selector")
	}
	want := strings.Split(filter[2:len(filter)-2], "|")
	known := map[string]bool{}
	for _, n := range names {
		known[n] = true
	}
	seen := map[string]bool{}
	for _, n := range want {
		if !known[n] || seen[n] {
			return nil, fmt.Errorf("missing or duplicate selected target %s", n)
		}
		seen[n] = true
	}
	sort.Strings(want)
	return want, nil
}
func compile(m manifest, dir string) error {
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	args := []string{"test", "-c", "-json", "-race", "-p", "2", "-timeout=20m", "-vet=off", "-count=1", "-o", dir + string(filepath.Separator)}
	for _, e := range m.Entries {
		args = append(args, e.Package)
	}
	c := command(m.Root, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return err
	}
	for _, e := range m.Entries {
		p := filepath.Join(dir, e.Binary)
		info, err := os.Stat(p)
		if e.NoTestFiles {
			if !os.IsNotExist(err) {
				return fmt.Errorf("unexpected no-test binary %s", p)
			}
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			return fmt.Errorf("missing executable test binary %s", p)
		}
	}
	return nil
}
func matchingEntry(m manifest, args []string) (entry, error) {
	base := []string{"test", "-json", "-race", "-p", "2", "-timeout=20m", "-vet=off", "-count=1"}
	if len(args) != 9 && len(args) != 11 {
		return entry{}, errors.New("unexpected test arguments")
	}
	if !reflect.DeepEqual(args[:8], base) {
		return entry{}, errors.New("test flags changed")
	}
	filter := ""
	pkg := args[len(args)-1]
	if len(args) == 11 {
		if args[8] != "-run" {
			return entry{}, errors.New("unexpected selector flag")
		}
		filter = args[9]
	}
	for _, e := range m.Entries {
		if e.Package == pkg && e.Filter == filter {
			return e, nil
		}
	}
	return entry{}, errors.New("package/selector outside fixed inventory")
}
func invoke(args []string) error {
	m, err := readManifest(os.Getenv("TRIAL_MANIFEST"))
	if err != nil {
		return err
	}
	e, err := matchingEntry(m, args)
	if err != nil {
		return err
	}
	mode := os.Getenv("TRIAL_MODE")
	if mode != "baseline" && mode != "candidate" {
		return errors.New("unknown variant")
	}
	dir := os.Getenv("TRIAL_RECORDS")
	stem := filepath.Join(dir, fmt.Sprintf("%02d", e.Index))
	lock, err := os.OpenFile(stem+".started", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("duplicate invocation: %w", err)
	}
	lock.Close()
	f, err := os.Create(stem + ".jsonl")
	if err != nil {
		return err
	}
	defer f.Close()
	var c *exec.Cmd
	if mode == "baseline" || e.NoTestFiles {
		c = command(m.Root, args...)
	} else {
		binary := filepath.Join(os.Getenv("TRIAL_BINARIES"), e.Binary)
		info, err := os.Stat(binary)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			return errors.New("missing candidate binary")
		}
		a := []string{"tool", "test2json", "-t", "-p", e.Package, binary, "-test.v=test2json", "-test.paniconexit0", "-test.count=1", "-test.timeout=20m"}
		if e.Filter != "" {
			a = append(a, "-test.run", e.Filter)
		}
		c = command(e.Dir, a...)
		c.Env = append(c.Env, "PWD="+e.Dir)
	}
	c.Stdout = io.MultiWriter(os.Stdout, f)
	c.Stderr = os.Stderr
	r := receipt{Package: e.Package, Filter: e.Filter, Mode: mode, Args: args, Start: time.Now()}
	err = c.Run()
	r.End = time.Now()
	if err != nil {
		r.Exit = 1
		var x *exec.ExitError
		if errors.As(err, &x) {
			r.Exit = x.ExitCode()
		}
	}
	if w := writeJSON(stem+".receipt.json", r); w != nil {
		return w
	}
	return err
}
func validateEvents(e entry, r io.Reader) (string, map[string]string, error) {
	expected := map[string]bool{}
	for _, n := range e.Targets {
		expected[n] = true
	}
	runs := map[string]bool{}
	done := map[string]string{}
	outputs := map[string]string{}
	packageDone := ""
	started := false
	d := json.NewDecoder(r)
	for {
		var v event
		err := d.Decode(&v)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
		if v.Package == "" {
			if v.Action == "build-output" {
				continue
			}
			return "", nil, errors.New("unattributed event")
		}
		if v.Package != e.Package {
			return "", nil, errors.New("unexpected event package")
		}
		if v.Test != "" {
			if !expected[strings.SplitN(v.Test, "/", 2)[0]] {
				return "", nil, fmt.Errorf("unassigned root target %s", v.Test)
			}
			switch v.Action {
			case "run":
				if runs[v.Test] || done[v.Test] != "" {
					return "", nil, errors.New("duplicate root run")
				}
				runs[v.Test] = true
			case "pass", "skip", "fail":
				if !runs[v.Test] || done[v.Test] != "" {
					return "", nil, errors.New("root completion without one run")
				}
				done[v.Test] = v.Action
			case "output":
				outputs[v.Test] += v.Output
			case "pause", "cont":
			default:
				return "", nil, errors.New("unknown root action")
			}
			continue
		}
		switch v.Action {
		case "start":
			if started {
				return "", nil, errors.New("duplicate package start")
			}
			started = true
		case "pass", "skip", "fail":
			if packageDone != "" {
				return "", nil, errors.New("duplicate package completion")
			}
			packageDone = v.Action
		case "output", "build-output":
		default:
			return "", nil, fmt.Errorf("unexpected package action %s", v.Action)
		}
	}
	if !started || packageDone == "" {
		return "", nil, errors.New("missing package completion")
	}
	if e.NoTestFiles {
		if packageDone != "skip" || len(runs) != 0 {
			return "", nil, errors.New("no-test package must have genuine skip completion")
		}
	} else if packageDone != "pass" {
		return "", nil, fmt.Errorf("test package did not pass: %s", packageDone)
	}
	for name := range runs {
		if done[name] == "" {
			return "", nil, fmt.Errorf("missing target completion %s", name)
		}
		if done[name] == "fail" {
			return "", nil, fmt.Errorf("target failed %s", name)
		}
		if done[name] == "skip" && !(e.Package == prefix+"internal/releasetrust" && name == "TestCosignLegacyBundleInProcessInterop" && strings.Contains(outputs[name], "release fixture job supplies pinned COSIGN_BIN")) {
			return "", nil, fmt.Errorf("unexpected skip %s", name)
		}
	}
	for _, n := range e.Targets {
		if !runs[n] || done[n] == "" {
			return "", nil, fmt.Errorf("missing root run/completion %s", n)
		}
		if done[n] == "fail" {
			return "", nil, fmt.Errorf("root failed %s", n)
		}
	}
	return packageDone, done, nil
}
func validate(m manifest, dir string) (result, error) {
	out := result{Packages: map[string]string{}, Targets: map[string]map[string]string{}}
	records := make([]receipt, len(m.Entries))
	files, err := filepath.Glob(filepath.Join(dir, "*.receipt.json"))
	if err != nil || len(files) != len(m.Entries) {
		return out, errors.New("missing or extra invocation receipts")
	}
	for _, e := range m.Entries {
		stem := filepath.Join(dir, fmt.Sprintf("%02d", e.Index))
		var r receipt
		if err := readJSON(stem+".receipt.json", &r); err != nil {
			return out, err
		}
		if r.Exit != 0 || r.Package != e.Package || r.Filter != e.Filter || !r.End.After(r.Start) {
			return out, errors.New("failed/malformed invocation receipt")
		}
		if _, err := matchingEntry(m, r.Args); err != nil {
			return out, err
		}
		records[e.Index] = r
		f, err := os.Open(stem + ".jsonl")
		if err != nil {
			return out, err
		}
		status, targets, err := validateEvents(e, f)
		f.Close()
		if err != nil {
			return out, fmt.Errorf("%s: %w", e.Package, err)
		}
		out.Packages[e.Package] = status
		out.Targets[e.Package] = targets
	}
	// The original scheduler owns dispatch. Receipts additionally prove the sole
	// serial service starts after every peer and the peer pool never exceeds two.
	var service *receipt
	var peers []receipt
	for i, e := range m.Entries {
		if e.Package == prefix+"internal/service" {
			service = &records[i]
		} else {
			peers = append(peers, records[i])
		}
	}
	if service == nil {
		return out, errors.New("missing serial service")
	}
	for _, r := range peers {
		if service.Start.Before(r.End) {
			return out, errors.New("service overlaps peer")
		}
		active := 0
		for _, p := range peers {
			if !p.Start.After(r.Start) && p.End.After(r.Start) {
				active++
			}
		}
		if active > 2 {
			return out, errors.New("peer concurrency exceeds two")
		}
	}
	return out, nil
}
