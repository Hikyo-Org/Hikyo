package main

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// stableJSON reproduces encoding/json (v1) output bytes. The planner is built
// as a lone file copied outside the module (see ci.yml), so it cannot import
// internal/jsonwire and keeps its own copy of those options; internal/lint fails
// when the two option lists differ.
var stableJSON = json.JoinOptions(
	json.Deterministic(true),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
	jsontext.EscapeForHTML(true),
	jsontext.EscapeForJS(true),
	jsontext.PreserveRawStrings(true),
)

type packageInfo struct {
	ImportPath   string
	Dir          string
	TestGoFiles  []string
	XTestGoFiles []string
	Module       *struct {
		Dir string
	}
	relativePath string
}

type fuzzTarget struct {
	packagePath string
	name        string
	relativeDir string
}

type isolationTest struct {
	name string
}

type options struct {
	root       string
	shard      int
	shardCount int
}

var preferredShards = map[string]map[string]int{
	"fuzz": {
		"internal/importer":      0,
		"internal/crypto":        1,
		"internal/crypto/backup": 1,
		"internal/compose":       2,
		"internal/samlsp":        2,
		"internal/scimproto":     2,
	},
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "analysis shards: %v\n", err)
		os.Exit(2)
	}
}

func run(args []string, output io.Writer) error {
	if len(args) == 1 && args[0] == "--supports-all-v1" {
		_, err := fmt.Fprintln(output, "analysis-shards-all-v1")
		return err
	}
	if len(args) > 0 && args[0] == "all" {
		return runAll(args[1:], output)
	}
	if len(args) == 0 || (args[0] != "race" && args[0] != "fuzz" && args[0] != "isolation" && args[0] != "isolation-race") {
		return errors.New("usage: analysis-shards race|fuzz|isolation|isolation-race --root DIR --shard N --shards N; all --root DIR --race-shards N --shards N")
	}
	kind := args[0]
	flags := flag.NewFlagSet(kind, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts options
	flags.StringVar(&opts.root, "root", ".", "repository root")
	flags.IntVar(&opts.shard, "shard", -1, "zero-based shard index")
	flags.IntVar(&opts.shardCount, "shards", 0, "total shard count")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if opts.shardCount < 1 {
		return errors.New("shards must be positive")
	}
	if opts.shard < 0 || opts.shard >= opts.shardCount {
		return fmt.Errorf("shard %d is outside [0,%d)", opts.shard, opts.shardCount)
	}

	packages, err := listPackages(opts.root)
	if err != nil {
		return err
	}
	switch kind {
	case "race":
		return writeRaceShard(output, packages, opts)
	case "fuzz":
		return writeFuzzShard(output, packages, opts)
	case "isolation":
		return writeIsolationShard(output, packages, opts, false)
	case "isolation-race":
		return writeIsolationShard(output, packages, opts, true)
	default:
		panic("unreachable analysis kind")
	}
}

type completePlan struct {
	RacePlan      [][]string `json:"race_plan"`
	FuzzPlan      [][]string `json:"fuzz_plan"`
	IsolationPlan [][]string `json:"isolation_plan"`
	RaceShards    []int      `json:"race_shards"`
	Shards        []int      `json:"shards"`
}

func runAll(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("all", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	raceCount := flags.Int("race-shards", 0, "number of race shards")
	shardCount := flags.Int("shards", 0, "number of fuzz and isolation shards")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	// Bound allocations before inventory construction. Empty assignments are
	// refused below rather than letting a matrix leg pass without work.
	if *raceCount < 1 || *raceCount > 256 || *shardCount < 1 || *shardCount > 256 {
		return errors.New("race-shards and shards must be between 1 and 256")
	}
	packages, err := listPackages(*root)
	if err != nil {
		return err
	}
	plan, err := planAll(packages, *raceCount, *shardCount)
	if err != nil {
		return err
	}
	// Construct and validate every family before writing any result. Discovery
	// errors cannot leave a partial JSON plan for downstream jobs to consume.
	b, err := json.Marshal(plan, stableJSON)
	if err != nil {
		return err
	}
	_, err = output.Write(append(b, '\n'))
	return err
}

func planAll(packages []packageInfo, raceCount, shardCount int) (completePlan, error) {
	races, err := planRace(packages, raceCount)
	if err != nil {
		return completePlan{}, err
	}
	fuzz, err := discoverFuzzTargets(packages)
	if err != nil {
		return completePlan{}, err
	}
	isolation, err := discoverIsolationTargets(packages, false)
	if err != nil {
		return completePlan{}, err
	}
	plan := completePlan{
		RacePlan: make([][]string, raceCount), FuzzPlan: make([][]string, shardCount),
		IsolationPlan: make([][]string, shardCount), RaceShards: make([]int, raceCount), Shards: make([]int, shardCount),
	}
	for index, shard := range races {
		plan.RacePlan[index] = raceShardLines(shard)
		plan.RaceShards[index] = index
	}
	for _, target := range fuzz {
		index := shardFor("fuzz", target.relativeDir, shardCount)
		plan.FuzzPlan[index] = append(plan.FuzzPlan[index], target.packagePath+"\t"+target.name)
	}
	for _, test := range isolation {
		index := shardFor("isolation", test.name, shardCount)
		plan.IsolationPlan[index] = append(plan.IsolationPlan[index], test.name)
	}
	for index := range plan.Shards {
		plan.Shards[index] = index
	}
	for _, family := range []struct {
		name   string
		shards [][]string
	}{{"race", plan.RacePlan}, {"fuzz", plan.FuzzPlan}, {"isolation", plan.IsolationPlan}} {
		for index, targets := range family.shards {
			if len(targets) == 0 {
				return completePlan{}, fmt.Errorf("%s shard %d is empty", family.name, index)
			}
		}
	}
	return plan, nil
}

func listPackages(root string) ([]packageInfo, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	goBinary := os.Getenv("GO_BIN")
	if goBinary == "" {
		goBinary = "go"
	}
	command := exec.Command(goBinary, "list", "-json", "./...")
	command.Dir = absoluteRoot
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("go list failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	decoder := jsontext.NewDecoder(&stdout)
	packages := make([]packageInfo, 0)
	for {
		var pkg packageInfo
		if err := json.UnmarshalDecode(decoder, &pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		if pkg.Module == nil || pkg.Module.Dir == "" {
			return nil, fmt.Errorf("package %s has no module root", pkg.ImportPath)
		}
		relativePath, err := filepath.Rel(pkg.Module.Dir, pkg.Dir)
		if err != nil {
			return nil, fmt.Errorf("resolve package %s path: %w", pkg.ImportPath, err)
		}
		if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("package %s escapes its module", pkg.ImportPath)
		}
		pkg.relativePath = filepath.ToSlash(relativePath)
		packages = append(packages, pkg)
	}
	if len(packages) == 0 {
		return nil, errors.New("go list returned no packages")
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].ImportPath < packages[j].ImportPath
	})
	return packages, nil
}

type raceUnit struct {
	pkg     *packageInfo
	target  string
	seconds float64
}

type raceShard struct {
	sequential float64
	pool       float64
	longest    float64
	seconds    map[string]float64
	targets    map[string][]string
}

// writeRaceShard lists one shard's packages longest first, so the scheduler's
// two-wide pool starts its long suites before the short ones.
func writeRaceShard(output io.Writer, packages []packageInfo, opts options) error {
	shards, err := planRace(packages, opts.shardCount)
	if err != nil {
		return err
	}
	for _, line := range raceShardLines(shards[opts.shard]) {
		if _, err := fmt.Fprintln(output, line); err != nil {
			return fmt.Errorf("write race shard: %w", err)
		}
	}
	return nil
}

func raceShardLines(shard raceShard) []string {
	paths := make([]string, 0, len(shard.seconds))
	for path := range shard.seconds {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		if shard.seconds[paths[i]] != shard.seconds[paths[j]] {
			return shard.seconds[paths[i]] > shard.seconds[paths[j]]
		}
		return paths[i] < paths[j]
	})
	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		line := path
		if names := shard.targets[path]; len(names) > 0 {
			sort.Strings(names)
			line += "\t^(" + strings.Join(names, "|") + ")$"
		}
		lines = append(lines, line)
	}
	return lines
}

// planRace packs whole packages and the top-level targets of the suites in
// raceTargetSeconds, longest first, each onto the shard whose modelled
// duration grows least (ties go to the lowest shard), keeping each package's
// invocation within its budget where any shard can. Split suites include fuzz
// seeds and runnable examples, which an unfiltered go test would execute too.
// The plan depends only on the source tree and the weights below.
func planRace(packages []packageInfo, shardCount int) ([]raceShard, error) {
	units := make([]raceUnit, 0, len(packages))
	for index := range packages {
		pkg := &packages[index]
		if raceExcluded[pkg.relativePath] {
			continue
		}
		weights, split := raceTargetSeconds[pkg.relativePath]
		if !split {
			units = append(units, raceUnit{pkg: pkg, seconds: raceSeconds(racePackageSeconds, pkg.relativePath)})
			continue
		}
		tests, err := discoverPackageTargets(*pkg, true)
		if err != nil {
			return nil, err
		}
		// Local target timings give proportions; scale them to the package's
		// CI total so split suites and whole packages share one unit.
		measured := 0.0
		for _, test := range tests {
			measured += raceSeconds(weights, test.name)
		}
		scale := 1.0
		if total, ok := racePackageSeconds[pkg.relativePath]; ok {
			scale = total / measured
		}
		for _, test := range tests {
			units = append(units, raceUnit{pkg: pkg, target: test.name, seconds: raceSeconds(weights, test.name) * scale})
		}
	}
	sort.SliceStable(units, func(i, j int) bool {
		return units[i].seconds > units[j].seconds
	})

	shards := make([]raceShard, shardCount)
	for index := range shards {
		shards[index] = raceShard{seconds: map[string]float64{}, targets: map[string][]string{}}
	}
	for _, unit := range units {
		path := unit.pkg.ImportPath
		best := bestRaceShard(shards, unit)
		shards[best] = shards[best].with(unit)
		shards[best].seconds[path] += unit.seconds
		if unit.target != "" {
			shards[best].targets[path] = append(shards[best].targets[path], unit.target)
		}
	}
	return shards, nil
}

// bestRaceShard prefers shards where the unit's package invocation stays
// within raceInvocationBudgetSeconds, then the least modelled duration.
func bestRaceShard(shards []raceShard, unit raceUnit) int {
	best, bestCost, bestFits := 0, 0.0, false
	for index := range shards {
		fits := shards[index].seconds[unit.pkg.ImportPath]+unit.seconds+raceProcessSetupSeconds <= raceInvocationBudgetSeconds
		cost := shards[index].with(unit).cost()
		if index == 0 || fits && !bestFits || fits == bestFits && cost < bestCost {
			best, bestCost, bestFits = index, cost, fits
		}
	}
	return best
}

// with returns the shard's totals after adding unit. It leaves the shared maps
// alone, so probing a shard never changes it; the caller records a placement.
func (s raceShard) with(unit raceUnit) raceShard {
	seconds := unit.seconds
	// Split targets share one invocation within each package on a shard.
	// Charge startup once per package, including a whole package with no tests.
	if _, exists := s.seconds[unit.pkg.ImportPath]; !exists {
		seconds += raceProcessSetupSeconds
	}
	if raceSequentialSuites[unit.pkg.relativePath] {
		s.sequential += seconds
	} else {
		s.pool += seconds
		setup := raceProcessSetupSeconds
		s.longest = max(s.longest, s.seconds[unit.pkg.ImportPath]+unit.seconds+setup)
	}
	return s
}

// cost models test-race-packages.sh: sequential suites run one after another
// once a two-wide pool of every other entry has finished, and that pool lasts
// at least as long as its longest package.
func (s raceShard) cost() float64 {
	return s.sequential + max(s.pool/2, s.longest)
}

func raceSeconds(weights map[string]float64, name string) float64 {
	if seconds, ok := weights[name]; ok {
		return seconds
	}
	return 1
}

func writeFuzzShard(output io.Writer, packages []packageInfo, opts options) error {
	targets, err := discoverFuzzTargets(packages)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return errors.New("no Fuzz* target discovered")
	}
	for _, target := range targets {
		if shardFor("fuzz", target.relativeDir, opts.shardCount) != opts.shard {
			continue
		}
		if _, err := fmt.Fprintf(output, "%s\t%s\n", target.packagePath, target.name); err != nil {
			return fmt.Errorf("write fuzz shard: %w", err)
		}
	}
	return nil
}

func writeIsolationShard(output io.Writer, packages []packageInfo, opts options, includeRaceTargets bool) error {
	tests, err := discoverIsolationTargets(packages, includeRaceTargets)
	if err != nil {
		return err
	}
	if len(tests) == 0 {
		return errors.New("no runnable target discovered in internal/isolation")
	}
	for _, test := range tests {
		if shardFor("isolation", test.name, opts.shardCount) != opts.shard {
			continue
		}
		if _, err := fmt.Fprintln(output, test.name); err != nil {
			return fmt.Errorf("write isolation shard: %w", err)
		}
	}
	return nil
}

func discoverIsolationTargets(packages []packageInfo, includeRaceTargets bool) ([]isolationTest, error) {
	for index := range packages {
		if packages[index].relativePath == "internal/isolation" {
			return discoverPackageTargets(packages[index], includeRaceTargets)
		}
	}
	return nil, errors.New("internal/isolation package was not found")
}

// discoverPackageTargets lists runnable top-level targets from source, so
// planning never depends on executing the suite it is intended to partition.
func discoverPackageTargets(pkg packageInfo, includeRaceTargets bool) ([]isolationTest, error) {
	tests := make([]isolationTest, 0)
	seen := make(map[string]string)
	files := append(append([]string(nil), pkg.TestGoFiles...), pkg.XTestGoFiles...)
	sort.Strings(files)
	for _, name := range files {
		path := filepath.Join(pkg.Dir, name)
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || (!isTestName(function.Name.Name) && !(includeRaceTargets && isFuzzName(function.Name.Name))) {
				continue
			}
			if previous, exists := seen[function.Name.Name]; exists {
				return nil, fmt.Errorf("duplicate test %s in %s and %s", function.Name.Name, previous, path)
			}
			seen[function.Name.Name] = path
			tests = append(tests, isolationTest{name: function.Name.Name})
		}
		if includeRaceTargets {
			for _, example := range doc.Examples(parsed) {
				if example.Output == "" && !example.EmptyOutput {
					continue
				}
				name := "Example" + example.Name
				if previous, exists := seen[name]; exists {
					return nil, fmt.Errorf("duplicate test %s in %s and %s", name, previous, path)
				}
				seen[name] = path
				tests = append(tests, isolationTest{name: name})
			}
		}
	}
	sort.Slice(tests, func(i, j int) bool {
		return tests[i].name < tests[j].name
	})
	return tests, nil
}

func discoverFuzzTargets(packages []packageInfo) ([]fuzzTarget, error) {
	targets := make([]fuzzTarget, 0)
	seen := make(map[string]string)
	for _, pkg := range packages {
		files := append(append([]string(nil), pkg.TestGoFiles...), pkg.XTestGoFiles...)
		sort.Strings(files)
		for _, name := range files {
			path := filepath.Join(pkg.Dir, name)
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
			for _, declaration := range parsed.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv != nil || !isFuzzName(function.Name.Name) {
					continue
				}
				key := pkg.ImportPath + "\x00" + function.Name.Name
				if previous, exists := seen[key]; exists {
					return nil, fmt.Errorf("duplicate fuzz target %s in %s and %s", function.Name.Name, previous, path)
				}
				seen[key] = path
				targets = append(targets, fuzzTarget{
					packagePath: pkg.ImportPath,
					name:        function.Name.Name,
					relativeDir: pkg.relativePath,
				})
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].packagePath == targets[j].packagePath {
			return targets[i].name < targets[j].name
		}
		return targets[i].packagePath < targets[j].packagePath
	})
	return targets, nil
}

func isFuzzName(name string) bool {
	return isGoTargetName(name, "Fuzz")
}

func isTestName(name string) bool {
	return name != "TestMain" && isGoTargetName(name, "Test")
}

func isGoTargetName(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
		return false
	}
	first, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(first)
}

func shardFor(kind, relativePath string, shardCount int) int {
	if preferred, ok := preferredShards[kind][relativePath]; ok {
		return preferred % shardCount
	}
	hash := fnv.New32a()
	_, _ = io.WriteString(hash, relativePath)
	return int(hash.Sum32() % uint32(shardCount))
}

// raceExcluded names the packages the race shards never run. The isolation
// suite has its own shards. internal/lint's tests type-check the repository
// through x/tools go/packages, whose per-package goroutines race inside Go's
// own go/types under Go 1.27 (Named.unpack against isComplete, golang/go#81122,
// open; go 1.26 is race-clean there): the detector fires on the toolchain, not
// on Hikyo code, and the analyzers themselves run on one goroutine. The package
// still runs in test_core. Re-include it once the toolchain go.mod pins carries
// the fix for #81122.
var raceExcluded = map[string]bool{
	"internal/isolation": true,
	"internal/lint":      true,
}

// raceSequentialSuites mirrors test-race-packages.sh, which runs these
// filtered suites one at a time after the concurrent pool.
var raceSequentialSuites = map[string]bool{
	"internal/app":     true,
	"internal/service": true,
}

// raceProcessSetupSeconds is a planning allowance for go test process startup,
// cached compilation and linking outside the package's reported test duration.
// It is deliberately an estimate, not a measured per-target duration. Hosted
// JSON/wall timing artifacts distinguish these costs for future recalibration.
const raceProcessSetupSeconds = 20.0

// raceInvocationBudgetSeconds caps one package's modelled go test invocation on
// a shard at half of test-race-packages.sh's -timeout=20m: hosted runs of one
// unchanged internal/app target list took from 779 s to over 1200 s. A unit
// that fits no shard goes to the cheapest one, as before. More shards help only
// split suites: a whole package nearing the budget (internal/upgradecustody,
// 558 s with setup) must be split through test-race-packages.sh's split-suite
// allowlist and raceTargetSeconds entries.
const raceInvocationBudgetSeconds = 600.0

// racePackageSeconds holds package-level JSON elapsed, summed across all
// twelve race-timings artifacts of ci-dev run 44901528773 attempt 1 on dev
// 4c7d11f6c (2026-11-21), whose twelve race shards all passed.
// Parent package events and top-level target events are separate measurements;
// subtest elapsed is never added to either. Unobserved prior package estimates
// remain available; unlisted packages weigh one second plus process startup.
var racePackageSeconds = map[string]float64{
	"api":                                 347.684,
	"api/omitzero":                        1.795,
	"cmd/bench-scan":                      1.012,
	"cmd/hikyo":                           1.162,
	"cmd/hikyo-receiver-example":          1.067,
	"internal/adapter":                    1.068,
	"internal/adapter/awssm":              2.425,
	"internal/adapter/awssm/awssmtest":    1.024,
	"internal/adapter/cloudflare":         1.048,
	"internal/adapter/forgejo":            1.297,
	"internal/adapter/githubactions":      3.278,
	"internal/adapter/gitlab":             1.337,
	"internal/adapter/sealedwebhook":      3.249,
	"internal/adapter/vaultkv":            1.419,
	"internal/admission":                  2.031,
	"internal/app":                        5912.621,
	"internal/audit":                      1.05,
	"internal/auditexport":                1.226,
	"internal/authz":                      1.166,
	"internal/authz/gen":                  3.763,
	"internal/backupreceipt":              1.498,
	"internal/bench":                      1.045,
	"internal/boundary":                   5.03,
	"internal/buildcompat":                1.087,
	"internal/cli":                        14.146,
	"internal/clientauth":                 1.046,
	"internal/clienttrust":                1.344,
	"internal/compose":                    1.445,
	"internal/config":                     3.753,
	"internal/configrollout":              19.133,
	"internal/conformance":                229.386,
	"internal/console":                    1.021,
	"internal/crypto":                     3.51,
	"internal/crypto/backup":              38.221,
	"internal/crypto/sealedhook":          8.523,
	"internal/definitions":                1.213,
	"internal/delivery":                   1.015,
	"internal/deliverytarget":             3.044,
	"internal/devupgrade":                 3.28,
	"internal/diagnostics":                1.026,
	"internal/disclose":                   1.021,
	"internal/domain":                     1.22,
	"internal/dotenv":                     1.038,
	"internal/dynamic":                    1.019,
	"internal/dynamic/postgres":           1.018,
	"internal/federationhttp":             1.285,
	"internal/filesync":                   1.763,
	"internal/hostupgrade":                1.023,
	"internal/importer":                   19.768,
	"internal/jsonwire":                   1.014,
	"internal/jwkssource":                 1.184,
	"internal/localsocket":                1.025,
	"internal/mail":                       1.432,
	"internal/mailtest":                   1.037,
	"internal/mcpserver":                  5.064,
	"internal/netpolicy":                  1.015,
	"internal/oauth2rp":                   1.048,
	"internal/oidcfed":                    1.447,
	"internal/oidcrp":                     10.452,
	"internal/oidctest":                   1.089,
	"internal/operation":                  1.023,
	"internal/operator":                   13.722,
	"internal/operator/api/v1alpha1":      7.288,
	"internal/operator/client":            1.495,
	"internal/pairing":                    111.742,
	"internal/parameters":                 1.045,
	"internal/pathutil":                   1.015,
	"internal/pki":                        14.246,
	"internal/releaseidentity":            1.018,
	"internal/releasetrust":               13.976,
	"internal/remotefetch":                1.431,
	"internal/repscan":                    1.597,
	"internal/runtimeconfig":              1.152,
	"internal/samlsp":                     2.567,
	"internal/scanning":                   1.148,
	"internal/scanning/bench":             1.015,
	"internal/scanning/gen":               1.021,
	"internal/schema":                     1.398,
	"internal/scimproto":                  1.048,
	"internal/sealedreceiver":             1.049,
	"internal/securefile":                 1.019,
	"internal/securitydeployment":         18.053,
	"internal/securityfeed":               2.298,
	"internal/securitymaintenance":        3.471,
	"internal/securitystatus":             1.053,
	"internal/selfupdate":                 9.866,
	"internal/server":                     75.33,
	"internal/service":                    1977.532,
	"internal/sshca":                      5.251,
	"internal/storagehealth":              1.346,
	"internal/store":                      2682.288,
	"internal/store/auditrow":             1.022,
	"internal/store/authn":                1.021,
	"internal/store/migrate":              285.57,
	"internal/store/tx":                   33.39,
	"internal/store/upgrade":              616.192,
	"internal/testutil/fixtureref":        1.212,
	"internal/tlspolicy":                  6.339,
	"internal/transit":                    1.106,
	"internal/updatecheck":                3.891,
	"internal/updater":                    1.107,
	"internal/upgradeassembly":            1.224,
	"internal/upgradebundle":              1.475,
	"internal/upgradecompat":              2.201,
	"internal/upgradecustody":             538.0,
	"internal/upgradegate":                1724.352,
	"internal/webauthnrp":                 1.046,
	"internal/webauthntest":               1.018,
	"scripts/acceptance/github-contract":  2.126,
	"scripts/acceptance/mcp-fixture":      1.747,
	"scripts/ci":                          255.499,
	"scripts/ci/analysis-shards-go":       29.683,
	"scripts/ci/benchmark":                3.509,
	"scripts/ci/chartdoctor":              3.13,
	"scripts/ci/chartfixture":             1.104,
	"scripts/ci/checksuites":              1.047,
	"scripts/ci/codspeed-verify":          1.042,
	"scripts/ci/feedback-reuse-size":      1.017,
	"scripts/ci/networkpolicyfixture":     1.013,
	"scripts/ci/pr-gate":                  69.588,
	"scripts/ci/prepare-lint-cache":       1.062,
	"scripts/ci/qualification":            10.118,
	"scripts/ci/queue-report":             2.187,
	"scripts/ci/retire-runs":              1.077,
	"scripts/ci/securitycomposerehearsal": 1.072,
	"scripts/ci/source-proof":             2.744,
	"scripts/ci/unattendedfixture":        1.056,
	"scripts/ci/workflow-lint":            1.028,
	"scripts/mcp-production-client":       1.11,
	"scripts/mcp-public-smoke":            1.311,
	"scripts/operator-floor":              2.869,
	"scripts/ops-floor":                   1.038,
	"scripts/release":                     1.349,
	"scripts/release/assemble-upgrade":    271.905,
	"scripts/release/embed-compatibility": 46.034,
	"scripts/release/nightly":             1.377,
	"scripts/release/stable":              17.818,
	"scripts/security/alert-route":        1.97,
	"scripts/security/artifact-harness":   1.69,
	"scripts/security/release-evidence":   1.094,
}

// raceTargetSeconds holds hosted top-level target proportions from the same
// twelve-shard JSON artifacts. Observed paused targets retain the existing
// four-wide parallel allowance; subtest events are excluded to avoid counting
// parent and child elapsed twice. Unobserved source targets keep prior weights.
// The planner discovers every target, including fuzz seeds and
// runnable examples, and gives missing weights its one-second fallback, so
// targets measured at one second or less are omitted. Each split suite is
// scaled to racePackageSeconds. Keep internal/lint whole so its repository
// load is not repeated across processes. Updating these estimates changes
// placement only, never the target union or DB scheduling.
var raceTargetSeconds = map[string]map[string]float64{
	"internal/app": {
		"TestActivatingStrictClosesClassicalKeepAliveAndServesOnlyHybrid":                 18.96,
		"TestActualDefaultDevCLIStartsAndRestarts":                                        194.38,
		"TestAdminAuthRefusesUnverifiedLegacySchema":                                      14.44,
		"TestAdminAuthResourceOwnership":                                                  37.08,
		"TestAdminAuthWarnsWhenRootRotationPending":                                       19.46,
		"TestAdminCreateAdoptsRunningServerSeedInsteadOfCommandDefaults":                  15.51,
		"TestAdminCreateRequiresFreshServerSeed":                                          20.26,
		"TestAdoptedProfileOutranksAndWarnsAboutADifferingBootstrapValue":                 15.69,
		"TestAutomaticCustodyPolicySeamOnRealSources":                                     58.55,
		"TestAutomaticPrewriteRetryMeasuresRealSQLiteAndRestoreProof":                     28.26,
		"TestAutomaticUpgradePackagedNightlyRoute":                                        198.45,
		"TestBackupExportJobRecordsFailureLoudlyWithoutPlaintext":                         18.6,
		"TestBackupExportJobRunsOnceThenGatesOnTheInterval":                               19.53,
		"TestBackupPruneJobRemovesOnlyAgedArchives":                                       19.9,
		"TestBackupRestoreDiagnosticsLevels":                                              81.08,
		"TestBootLogsEffectiveSQLitePoolSizes":                                            18.65,
		"TestBootResourceOwnershipOnFailure":                                              76.19,
		"TestBootSuccessTransfersResourceOwnershipToServer":                               15.5,
		"TestBootWarmsOpenAPIBeforeListening":                                             18.62,
		"TestBootWithRootKeyFileAndWrongKeyRefused":                                       17.82,
		"TestBootWithStrictProfileServesOnlyHybridPeers":                                  13.13,
		"TestBootstrapDeploymentBootSeedsSelectorsAndKeepsPendingRepairFenced":            20.36,
		"TestBootstrapDeploymentDatabaseProofPrecedesSignedSubmitAndTransport":            3.33,
		"TestBootstrapDeploymentEnrollmentPinsStandaloneNode":                             21.09,
		"TestBootstrapDeploymentRefusesEveryOperationOnAPreRolloverRow":                   37.37,
		"TestBootstrapDeploymentRefusesSignerThatIsNotTheEnrolledKey":                     13.04,
		"TestBootstrapDeploymentRejectsReplacedAliasAndExpiredPrivateProof":               3.28,
		"TestBootstrapDeploymentRenewalPreservesCommittedAuthorityAfterRestart":           14.47,
		"TestBootstrapDeploymentRootPrepareIsSealedButNotPersisted":                       13.12,
		"TestBootstrapDeploymentStrictProfileSignsEveryDecisionAndRefusesOldRegime":       13.07,
		"TestBootstrapInitialHASourceCarriesUnchangedCorrespondenceAndReproof":            1.99,
		"TestBootstrapSingletonTopologyRequiresActualPrerequisites":                       20.27,
		"TestCandidateConfigurationFailureKeepsRestoreRequired":                           73.72,
		"TestCandidateHealthChecksEnrolledNextRootWithoutProvider":                        13.52,
		"TestConfiguredEvidenceIsLoggedAtActivationNotPreparation":                        18.26,
		"TestCustodyBackupPolicyAgainstRealVault":                                         5.21,
		"TestDevBootGeneratesAndReusesRootKey":                                            19.6,
		"TestDevBootSeparatesPublicAndOperationalRoutes":                                  20.96,
		"TestDevelopmentControlsCannotChangeDeploymentTrustContext":                       17.48,
		"TestDevelopmentNodeControlsChangeActualRuntimeAfterActivation":                   17.26,
		"TestDevelopmentProviderPreservesRemoteStateAcrossOrdinaryReloadAndRefusesSwitch": 14.05,
		"TestDevelopmentProviderSwitchRechecksConfigurationAfterPreparation":              19.64,
		"TestEmailInvitationFailureCountedAcrossActivation":                               15.61,
		"TestEnrolledSourceLoaderAcceptsOnlyExplicitSingletonHAIdentity":                  13.04,
		"TestEscrowLocalCLIUsesAdmittedCurrentHierarchy":                                  26.05,
		"TestExplicitMigrateThenBootWithoutAutoMigrate":                                   17.36,
		"TestFreshBootstrapOriginAuthoritySurvivesBoot":                                   53.79,
		"TestLegacyDrillFixtureRefusesCLILoginEvidence":                                   41.33,
		"TestLegacyDrillFixtureRefusesPendingAuthenticationEvidence":                      19.79,
		"TestLegacyDrillFixtureRefusesProjectCapacityEvidence":                            32.48,
		"TestLiveSnapshotFloorUsesTheRowAndZeroOnlyWhenAbsent":                            41.26,
		"TestLocalCLISocketServesPublicAPIAndIsRemovedOnShutdown":                         18.44,
		"TestMigrateDiagnosticsLevelsAndNoOp":                                             75.1,
		"TestMigrateNeedsNoRootKey":                                                       18.3,
		"TestMigrationRefusesUnverifiedExistingSchemaBeforeWrites":                        36.35,
		"TestNativeEnrollmentDoesNotAdoptAnExistingInstanceIntoAnUnboundVault":            12.4,
		"TestNativeEnrollmentFailureBeforeThePinLeavesNoPin":                              43.49,
		"TestNativeEnrollmentFreshHostCreatesBoundVaultAndPin":                            36.87,
		"TestNativeEnrollmentIsIdempotent":                                                41.81,
		"TestNativeEnrollmentLeavesAnUnfinishedHostUpgradeAlone":                          46.79,
		"TestNativeEnrollmentRefusesVaultBoundToAnotherInstance":                          28.91,
		"TestNativeEnrollmentResumesAfterFailedFirstBoot":                                 33.4,
		"TestNativeEnrollmentResumesAnInterruptedMigrate":                                 57.97,
		"TestNativeEnrollmentStartsServiceLeftUnhealthyByAnInterruptedBoot":               25.28,
		"TestNativeRollbackRefusesAnythingButTheSnapshotsOwnRows":                         144.29,
		"TestNativeRollbackReleasesAnUntouchedSourceUnderALaggingJournal":                 80.21,
		"TestNativeRollbackRestoresTheHealthySourceAndNothingElse":                        38.67,
		"TestNativeRollbackResumesFromRollingBack":                                        34.06,
		"TestNativeRollbackRunsTheHookBeforeTheSourceAcceptsTheJournal":                   41.72,
		"TestNativeTLSBootServesHTTPSAndKeepsOpsPlaintext":                                18.89,
		"TestNextRootLiveSelectionKeepsRotationSeparate":                                  21.79,
		"TestNextRootPreparationRejectsUnavailableSources":                                66.54,
		"TestNextRootSeedRequiresExactEnrolledProjection":                                 18.78,
		"TestNodeRuntimeCancelledActivationReleasesNewListener":                           12.96,
		"TestNodeRuntimeImportsTLSFilesOnce":                                              17.68,
		"TestNodeRuntimeInstallsIndependentCapacityAndPolicy":                             14.38,
		"TestNodeRuntimeListenerFailureKeepsOldGraphAndReleasesReservation":               13.18,
		"TestNodeRuntimeListenerMoveDrainsOldHTTPResponse":                                14.19,
		"TestNodeRuntimeManagedRestartDoesNotReadDeletedTLSFiles":                         20.15,
		"TestNodeRuntimeMissingHANodeBootsOnlyAdministrativeRecovery":                     4.03,
		"TestNodeRuntimePostgresPoolAppliesWithoutReplacingIdentity":                      1.62,
		"TestNodeRuntimeReservesMovesAndSwapsListeners":                                   18.04,
		"TestNodeRuntimeScheduledBackupUsesInstalledDestination":                          13.98,
		"TestNodeRuntimeTLSRotationAndPlaintextTransition":                                19.3,
		"TestOperationalListenerFailureStopsWholeLifecycle":                               18.71,
		"TestOrdinaryBackupPublicAdmissionAndDataOnlyRestore":                             48.02,
		"TestOwnerRuntimeActivationFailureResumesAdministrativeGraph":                     14.82,
		"TestOwnerRuntimeCanceledSQLiteSettlementCleanupWithoutRollbackHook":              18.25,
		"TestOwnerRuntimeDrainsRequestsBeforeInstalling":                                  13.25,
		"TestOwnerRuntimeFailedTargetFencesBusinessAndPreservesHTTPRepair":                13.56,
		"TestOwnerRuntimeInstallsHTTPAuthAndRetentionGraph":                               14.27,
		"TestOwnerRuntimeJoinsCanceledSQLiteSettlement":                                   18.15,
		"TestOwnerRuntimePreparationFailureAndMailOnlyKeepActiveGraph":                    17.53,
		"TestOwnerRuntimeRefusesPasswordCostChangeAfterCredentialEstablishment":           13.9,
		"TestOwnerRuntimeReloadsProjectKeyLimit":                                          15.07,
		"TestPostgresRestoreDestinationRefusesExpiredOwnerAndOccupiedTarget":              13.6,
		"TestProductionBackupCustodyRefusesBeforeDatabaseAdmission":                       18.75,
		"TestProxyHTTPSOriginEmitsHSTSOnLoopbackBackend":                                  19.05,
		"TestRealBootKeepsAdoptedStrictWhenTheNodeIsMissingFromTheNodeDocument":           41.87,
		"TestRecoveryCapabilityExpiresWithOwnerAndArchive":                                93.42,
		"TestRecoveryPostgresRollsBackWhenMigrationOwnerDies":                             6.21,
		"TestRecoveryTransactionsUseSchema65GrantProjection":                              30.64,
		"TestRecoveryTransactionsUseSchema87ContainmentFreeProjections":                   64.52,
		"TestReleaseCompatibilityMatchesActualBothEngineSchemas":                          30.21,
		"TestRestorePostgresRollsBackWhenMigrationOwnerDies":                              2.91,
		"TestRestoreRequiredRestartExposesOnlyOperationalHealth":                          19.41,
		"TestRestoreSchemaRechecksPreflightBeforeAnyGooseWrite":                           3.12,
		"TestRollbackJournaledLeavesAJournalTheSourceAccepts":                             51.2,
		"TestRollbackJournaledResumesFromRollingBack":                                     51.56,
		"TestRollbackPostgresRefusesWhileAnotherSessionIsConnected":                       9.48,
		"TestRollbackPreflightRefusesObjectsTheRoleDoesNotOwn":                            6.84,
		"TestRollbackReadFailuresAreNotPermission":                                        38.28,
		"TestRollbackRefusesAnArchiveFromAnotherRouteBeforePublishing":                    50.38,
		"TestRollbackRefusesAnythingButTheFencedSnapshotsOwnRows":                         170.29,
		"TestRollbackRestoredRowIsCheckedByDeploymentKind":                                81.16,
		"TestRollbackRestoresTheFencedSnapshotAndNothingElse":                             47.66,
		"TestRotationSweepErrorsCarryTheirStage":                                          12.93,
		"TestRotationWorkerIsWiredAndStartedByTheGeneration":                              18.83,
		"TestRotationWorkerStopsWithItsContext":                                           18.88,
		"TestSchedulerHAThreeNodesOnePostgres":                                            2.43,
		"TestSchemaAheadOfBinaryRefusesToServe":                                           18.82,
		"TestSecurityMaintenanceAuthenticatedFeedAppliesPackagedRoute":                    324.56,
		"TestServeCancellationStopsBothListeners":                                         20.35,
		"TestServeWithReadySignalsOnlyAfterHTTPServingStarts":                             20.75,
		"TestServerDiagnosticsDoNotClaimSuccessAfterListenerFailure":                      17.64,
		"TestServerDiagnosticsLevelsAndReadiness":                                         52.16,
		"TestSingletonHARestoreBootRetainsCoordinationThroughSourceRepair":                10.47,
		"TestSingletonReplacementBootUsesActualHAMode":                                    5.12,
		"TestUnattendedCredentialSealedRepeatedChildReads":                                1.11,
		"TestUnattendedPackagedEnrollmentUpgradeAndIntermediateCredential":                351.9,
		"TestUnattendedRootCustodyFollowsEnrollment":                                      37.81,
		"TestUnattendedScratchPreservesUnownedDataAndReusesOwnedSchema":                   2.91,
		"TestUpgradeDrillActualBothEngineRecoveryAndConfigOnlyEscrow":                     68.74,
		"TestUpgradeDrillAppliedLedgerKeepsSourceAuthorityAndRotatesScratch":              36.73,
		"TestUpgradeDrillAutoCredentialScopeOnlyWhenOnlyTheProtectedOrganisationExists":   122.29,
		"TestUpgradeDrillAutoSelectionRefusesLostGrantAuthority":                          30.4,
		"TestUpgradeDrillAutoSelectionSkipsUnusableScopeBeforeCommit":                     142.23,
		"TestUpgradeDrillAutoWithoutCredentialScopeNeedsNoStoredSecret":                   103.25,
		"TestUpgradeDrillAutomaticallyProvesExistingCredentialAuthority":                  64.51,
		"TestUpgradeDrillDiagnosticsLevels":                                               159.68,
		"TestUpgradeDrillHybridBothEngineRecovery":                                        91.03,
		"TestUpgradeDrillHybridRefusesCustodyMismatch":                                    109.25,
		"TestUpgradeDrillManualSelectionStillRequiredByDefault":                           37.03,
		"TestUpgradeDrillRefusesMissingOrWrongPrivateCustody":                             197.4,
		"TestUpgradeExportDiagnosticsPreserveJSON":                                        85.95,
		"TestVerifiedMigrateSkipsBackupOnHealthyRestart":                                  19.2,
	},
	"internal/service": {
		"TestAckTokenForeignKeyringRejected":                                           1.44,
		"TestAdapterAddTargetRefusesObsoleteProviderVerification":                      1.54,
		"TestAdapterAdoptFinalWriteRechecksLiveAuthority":                              14.49,
		"TestAdapterAdoptSharedEnvironmentConsumesSingleDecisionOnce":                  14.27,
		"TestAdapterCeremonyErrorClassification":                                       2.96,
		"TestAdapterConfigureFinalWriteRechecksAfterProviderResponse":                  21.33,
		"TestAdapterConfigureFinalWriteRechecksTemporaryReveal":                        1.69,
		"TestAdapterCreateAndAddTargetRefuseBeforeCredentialOrProviderWithoutCeremony": 1.52,
		"TestAdapterCreateCeremonyRechecksAfterWriterAcquisition":                      19.42,
		"TestAdapterMoveRecoveryRechecksAfterWriterAcquisition":                        18.03,
		"TestAdapterObservationFinalWriteRechecksAfterWriterAcquisition":               1.63,
		"TestAdapterOriginMoveKeepsOldRouteAndCredentialThroughScrubBarrier":           1.04,
		"TestAdapterProviderFenceRetryRechecksSecurityClock":                           19.4,
		"TestAdapterProviderGateUsesSecurityClock":                                     19.36,
		"TestAdapterTargetAddRefusesDestinationEffectiveNameCollisionAtomically":       18.64,
		"TestAdapterTargetInPlaceClassificationAndReplacementConverge":                 2.41,
		"TestAdoptionBindingRechecksSeedFreshnessWithCurrentDatabaseClock":             15.21,
		"TestApplyTargetMutationClassifiesUpdateAndMove":                               1.59,
		"TestApplyTargetMutationConcurrentChangesUseLockedGeneration":                  13.33,
		"TestApplyTargetMutationKeepsMovePolicyInsideService":                          1.1,
		"TestApplyTargetMutationRequiresCeremonyForUpdateAndMove":                      1.57,
		"TestBackupPruneCancellationHoldsDirectoryUntilUnlinkReturns":                  14.97,
		"TestBackupRetentionCeilingAndRecovery":                                        24.99,
		"TestBackupRetentionFailuresPersist":                                           57.99,
		"TestBackupRetentionUncertaintyPreservesCandidates":                            99.86,
		"TestBuildCurrentStatePresenceEquivalence":                                     2.95,
		"TestCommittedDeploymentCarriesStrictEnvelopeAcrossRenewalBothEngines":         21.58,
		"TestCommittedDeploymentRenewsUnseenSubmitAndRestoreWithoutNewMFA":             15.57,
		"TestDeliveryPollWork": 18.97,
		"TestDeploymentRenewalRejectsChangedDecisionAndStaleRow":                       8.91,
		"TestDeveloperMintBusinessRefusalsRemainAuditedAfterRollback":                  2.28,
		"TestDeveloperPolicyClampOnlyCountsLiveCredentials":                            2.35,
		"TestEmailInvitationDispatchOutcomes":                                          20.52,
		"TestExportDirectorySyncFailurePreservesPublishedArtifact":                     125.26,
		"TestExportSyncsDirectoryAncestryWithoutOverwriting":                           20.63,
		"TestFillPageDensePaging":                                                      1.04,
		"TestGitLabAdapterPersistsTransportPolicyScopeAndFlags":                        1.21,
		"TestHostAdoptionPreservesLargeMailAndTLSSeed":                                 11.31,
		"TestHostAdoptionReadsFreshServerSeedWithoutEvaluatingCommandDefaults":         11.96,
		"TestHostAdoptionRechecksStandaloneDiscoveryBeforeBinding":                     5.29,
		"TestIncidentAdapterWorkRefusalBlocksClose":                                    1.67,
		"TestInstanceMailOutcomeSurvivesActorRevocationBothEngines":                    14.85,
		"TestInstanceMailTestConcurrentRefusalBothEngines":                             15.93,
		"TestInstanceMailTestRequiresProofAndBoundsRateBothEngines":                    16.49,
		"TestInstanceMailTestUsesTLSSinkAndDurableOutcomeBothEngines":                  59.83,
		"TestOpenJobAcrossAReEnrollmentStaysOpenAndClearsTheRuntime":                   4.47,
		"TestPreparationExportUsesLiveSessionWithoutRuntimeAdmission":                  12.28,
		"TestPreparedPublisherPreservesOrdinaryPassphraseBackup":                       43.21,
		"TestPreparedServiceFixtureStillRequiresAuthenticAdmission":                    1.37,
		"TestPreparedServiceFixtureWritesStayInTheirCopy":                              1.45,
		"TestPresenceIndexEquivalence":                                                 1.67,
		"TestProveValuesReadable":                                                      13.69,
		"TestPruneLoopStopsAfterFirstFailedUnlink":                                     19.59,
		"TestRecordBackupRefusesChangedCiphertext":                                     16.03,
		"TestRootRotationAnonymousDoesNotExposeWrapperState":                           18.36,
		"TestRootRotationAnonymousOutcomeDoesNotExposeConfigurationOrPhase":            14.0,
		"TestRootRotationBudgetRefusesBeforeLockAndRootSource":                         18.42,
		"TestRootRotationPreflightRefusesBeforeLockOrRootSource":                       18.71,
		"TestRootRotationReauthorizesAfterRootSourceRead":                              18.34,
		"TestRoutingPreflightHoldsOrgSlotBeforeProviderWork":                           1.14,
		"TestRoutingPreflightRefusalsConsumeAdapterBudget":                             19.21,
		"TestSchemaPublishStorageChecksEverySnapshotAndRollsBack":                      20.95,
		"TestSecurityMaintenanceReadAuthorizesLikeUpdateStatusAndAuditsTheRead":        13.76,
		"TestSelfConfigApplyRetryReturnsOriginalJobAfterCollection":                    11.46,
		"TestSelfConfigBootstrapLoadsItsPublishedConfiguration":                        6.32,
		"TestSelfConfigDeploymentCommitsBeforeSendingAndRequiresApplicationAck":        9.95,
		"TestSelfConfigDeploymentConcurrentFinalizeInvalidatesPreparedRoot":            12.7,
		"TestSelfConfigDeploymentRestoreAllowsRepairThenAnotherRollout":                13.0,
		"TestSelfConfigDeploymentRestoreRequiresExactMFAAndJournalsBeforeSending":      7.09,
		"TestSelfConfigDeploymentRootPersistenceRequiresExactMFA":                      24.92,
		"TestSelfConfigExistingInstanceAdoptionIsExplicitAndDurable":                   11.45,
		"TestSelfConfigExpiredApplyRetryDoesNotRestartPreparation":                     12.34,
		"TestSelfConfigHAAdoptionRejectsDifferentSeedsAtomically":                      1.15,
		"TestSelfConfigHARefusesStaleTrafficAndConvergesAfterActorRevocation":          1.23,
		"TestSelfConfigHostRecoveryNeedsQuiescenceAndRejectsNetwork":                   8.57,
		"TestSelfConfigIndependentOwnersKeepSeparateProjectsAndRuntime":                21.82,
		"TestSelfConfigInstallerActivationFailureFencesAndRetriesCommittedTarget":      10.22,
		"TestSelfConfigInstallerDisposesAbortedCandidateWithoutReplacement":            9.09,
		"TestSelfConfigInstallerDisposesAbortedSupersededAndShutdownCandidates":        7.27,
		"TestSelfConfigInstallerFailedTargetCanBeRepaired":                             12.83,
		"TestSelfConfigInstallerPreparationFailureAbortsBeforeTargetCommit":            12.21,
		"TestSelfConfigInstallerRetainsPreparationAndAcknowledgesOnlyAfterActivation":  8.98,
		"TestSelfConfigInvalidMailPublishRetainsActiveConfiguration":                   11.02,
		"TestSelfConfigInvalidOwnerValuePublishNamesTheKey":                            10.3,
		"TestSelfConfigInvalidSecretOwnerValuePublishNamesKeyOnly":                     5.52,
		"TestSelfConfigKeysAreShownInReleaseFolders":                                   9.37,
		"TestSelfConfigNextRootSelectorNeedsExactApply":                                12.53,
		"TestSelfConfigNodeSeedAdoptionImportsEncryptedLocalInputs":                    5.31,
		"TestSelfConfigNodeSeedChangeInvalidatesReviewedAdoption":                      11.91,
		"TestSelfConfigNormalKeyRotationPreservesRuntimeSnapshots":                     8.48,
		"TestSelfConfigOriginApplyRefusesRetiringPasskeyHostname":                      6.56,
		"TestSelfConfigOriginRecovery":                                                 52.88,
		"TestSelfConfigOriginReviewLoadsRetainedOriginWithoutActiveGraph":              11.35,
		"TestSelfConfigPostgresLifecycle":                                              1.32,
		"TestSelfConfigPublishNeedsExplicitReauthenticatedApply":                       11.89,
		"TestSelfConfigRepairScopeRequiresMFAInstanceAdminAndExactHierarchy":           8.74,
		"TestSelfConfigResolveRuntimeBundleDoesNotAcknowledgeOrAcceptNetworkAuthority": 11.93,
		"TestSelfConfigRestoreRequiresConfirmationBoundIntoReauthentication":           11.04,
		"TestSelfConfigRootFinalizationWaitsForRestoredRolloutRepair":                  9.21,
		"TestSelfConfigSingletonTopologyFencesOldIdentityAcrossOrdinaryApply":          13.39,
		"TestSelfConfigSingletonTopologyRestoreRequiresFreshRepair":                    12.54,
		"TestSelfConfigSuspensionKeepsRecoveryInterfaceAvailable":                      10.97,
		"TestSelfConfigTestMailChargesFivePerPrincipalPerHour":                         7.71,
		"TestSelfConfigTestMailRecordsOutcomeAfterActorRevocation":                     9.04,
		"TestSelfConfigTopologySurvivesSourceRestoreAndOrdinaryRepair":                 22.52,
		"TestSelfConfigUpgradeDeploymentRequiresExactMFAAndApplicationAck":             10.23,
		"TestSessionCompletionCreatesFactorParity":                                     12.88,
		"TestSessionCompletionPublishesOnlyCommittedAttemptTokens":                     25.68,
		"TestSessionCompletionRotationPreservesProjectionAndReplacesFactors":           18.14,
		"TestUpdateOutcomeReconcilesWithoutRequestingSession":                          18.12,
		"TestUpdateOutcomeRetryAfterAcknowledgementFailureIsIdempotent":                14.04,
		"TestUpdateRequestRefusesAndAuditsWithoutHelperContact":                        12.85,
		"TestUpdateRequestRequiresRecentHumanAuthenticationBeforeHelperContact":        12.81,
	},
	"internal/store": {
		"TestAWSMovesRefuseCrossKindCaseFoldedPendingClaim":                 17.81,
		"TestAWSSharedKindAdoptionPostgres":                                 4.65,
		"TestAWSSharedKindAdoptionSQLite":                                   36.06,
		"TestAdapterAdoptionClearsExpiredProviderFence":                     18.75,
		"TestAdapterAdoptionLedgerCollisionIsConflictPostgres":              2.47,
		"TestAdapterAdoptionLedgerCollisionIsConflictSQLite":                18.26,
		"TestAdapterAdoptionProvenancePostgres":                             2.68,
		"TestAdapterAdoptionProvenanceSQLite":                               19.23,
		"TestAdapterAdoptionRefusesStaleArtifactAndLiveProviderFence":       52.84,
		"TestAdapterAttemptsRecordRevisionErrorClassAndAttention":           18.95,
		"TestAdapterClaimDueReplaysExpiredLeaseOnly":                        18.32,
		"TestAdapterClaimDueSingleFlightsTargetAndCapsOrganizationAtFour":   18.44,
		"TestAdapterClaimSettlesCrashWindowAsUnknownOutcome":                18.93,
		"TestAdapterConcurrentAdoptionHasOneWinner":                         18.25,
		"TestAdapterConfigurationWriterAuthorityPostgres":                   2.06,
		"TestAdapterConfigurationWriterAuthoritySQLite":                     17.75,
		"TestAdapterConflictInsertIdempotentAcrossRetries":                  18.36,
		"TestAdapterConflictsHideStaleGeneration":                           18.44,
		"TestAdapterDeadCredentialScrubTerminatesAndEnumeratesOrphans":      18.75,
		"TestAdapterEnqueueClearsExpiredProviderWriteFence":                 18.83,
		"TestAdapterEnqueueRefusesAtPerTargetQueueDepth":                    18.92,
		"TestAdapterEnqueueSupersedesAndBumpsGenerationAtomically":          14.29,
		"TestAdapterEnqueueWaitsForProviderWriteFence":                      18.43,
		"TestAdapterFinishJobRequiresExactLeaseAndGeneration":               19.5,
		"TestAdapterFinishPreservesConcurrentReleasedCustody":               18.39,
		"TestAdapterGateRechecksAuthorityAndGeneration":                     18.23,
		"TestAdapterJournalCommitsIntentOutcomeAndLedgerAtomically":         17.56,
		"TestAdapterJournalFinishesUnsentIntentBeforeAuthorityAbort":        17.51,
		"TestAdapterJournalFinishesUnsentIntentBeforeGenerationAbort":       18.98,
		"TestAdapterJournalPUTNotFoundEndsEffectBeforeFreshCreateRetry":     17.67,
		"TestAdapterJournalPersistsOwnedMissingAndAuditFinding":             18.74,
		"TestAdapterJournalRefusesOwnedMissingCompletionAfterRelease":       18.41,
		"TestAdapterJournalRejectsTerminalWriteAfterLeaseLoss":              18.4,
		"TestAdapterJournalRejectsZeroCompletionWithoutDeletingLedger":      17.59,
		"TestAdapterJournalReleasedStateRetainsLedgerRow":                   19.7,
		"TestAdapterOriginReusableAfterTombstonePostgres":                   2.42,
		"TestAdapterOriginReusableAfterTombstoneSQLite":                     18.83,
		"TestAdapterPauseBlocksClaimsKeepsLedgerAndResumeCatchesUp":         18.59,
		"TestAdapterPlanArtifactAdoptionIsBoundAndAtomic":                   20.3,
		"TestAdapterProviderSwitchRefusesExistingOutboxAndConfiguration":    20.39,
		"TestAdapterProviderSwitchRequiresHostContext":                      20.71,
		"TestAdapterReserveRebindsReleasedHistoryToActivatedRoute":          12.96,
		"TestAdapterScrubLoadsLastTrustedSourceRevision":                    18.44,
		"TestAdapterStaleWorkerIsFencedAfterReclaim":                        18.63,
		"TestAdapterStampsCompareLexicallyAcrossBridges":                    18.53,
		"TestAdapterSuccessPersistsProviderWarnings":                        18.64,
		"TestAdapterTargetKeysAndHealthCounts":                              18.49,
		"TestAdapterTargetSurfacesPossibleCaptureWithoutClaimingOwnership":  18.38,
		"TestAuditCommitSeqAssignmentTakesNoCommitSeqIndexSIReadLock":       39.61,
		"TestCanonicalOriginAdoptionPostgres":                               26.46,
		"TestCanonicalOriginAdoptionSQLite":                                 260.59,
		"TestCloudflareTargetsPostgres":                                     2.35,
		"TestCloudflareTargetsSQLite":                                       18.33,
		"TestConcurrentAWSOriginAdoptionPostgres":                           3.29,
		"TestConcurrentAWSOriginAdoptionSQLite":                             18.6,
		"TestConcurrentMoveAndAdapterDeletePostgres":                        11.81,
		"TestCoordinationClaimCommitFailurePreservesProvisionalFence":       2.58,
		"TestCoordinationPostgres":                                          3.18,
		"TestCoordinationSQLite":                                            19.57,
		"TestCrashReservationReleaseIsGenerationFencedAndLeavesNoConflict":  37.9,
		"TestEveryDirectRuntimeFamilyRefusesMaintenanceAndOldGeneration":    22.87,
		"TestExportPostgresManifestUsesCopySnapshotDuringMigration":         3.09,
		"TestExportSQLiteManifestUsesArchivedSchemaDuringMigration":         19.84,
		"TestGitLabAdoptionScopePostgres":                                   4.82,
		"TestGitLabAdoptionScopeSQLite":                                     36.48,
		"TestKeyRotationInvariantsPostgres":                                 2.58,
		"TestKeyRotationInvariantsSQLite":                                   18.28,
		"TestMoveReplacementUnsentReservationsPostgres":                     20.69,
		"TestMoveReplacementUnsentReservationsSQLite":                       153.22,
		"TestOpenSQLiteCapsConnectionPools":                                 18.76,
		"TestOriginMoveEndpointIdentityPostgres":                            2.46,
		"TestOriginMoveEndpointIdentitySQLite":                              19.17,
		"TestPausedAdapterMovesPostgres":                                    15.1,
		"TestPausedAdapterMovesSQLite":                                      86.33,
		"TestPostgresPoolSessionsDropDeadClients":                           1.91,
		"TestPostgresSerializedAdmissionHoldsLockUntilSettlement":           6.67,
		"TestPostgresSourceProofBindsAliasAndExpires":                       3.15,
		"TestPostgresSourceProofRefusesRecoveryChange":                      2.57,
		"TestPostgresSourceProofRejectsClonedInstallation":                  3.04,
		"TestPostgresSourceProofRequiresRuntimePrivileges":                  3.3,
		"TestPostgresSourceProofSanitizesFailuresAndTimeout":                2.56,
		"TestPostgresTimestampsAreUTC":                                      3.25,
		"TestPreparedPostgresPoolConcurrentCoordination":                    2.6,
		"TestPreparedPostgresPoolDefaultsAndStaleCandidates":                5.72,
		"TestPreparedPostgresPoolFailuresDoNotPublish":                      2.5,
		"TestPreparedPostgresPoolPreservesTransactionsAndCoordination":      2.52,
		"TestPreparedPostgresPoolRefusesChangedAdmission":                   13.37,
		"TestPreparedPostgresPoolSQLitePolicy":                              17.92,
		"TestProviderMoveClaimsUseSecretSurfaceForConfig":                   36.99,
		"TestPublishedGenerationSupersedesWithoutStealingLiveProviderFence": 18.44,
		"TestReleasedAdoptionRecoveryPostgres":                              23.49,
		"TestReleasedAdoptionRecoverySQLite":                                235.94,
		"TestReservationReleaseCannotDropOwnedOrDispatchedCustody":          18.21,
		"TestRestoreSQLiteDirectoryDurabilityPreservesCommittedMutations":   20.76,
		"TestRuntimeOpenRequiresActualGateAndSQLiteReadsCannotWrite":        22.77,
		"TestRuntimeTransactionPanicReleasesHostExclusion":                  25.85,
		"TestSQLiteBeginFailuresReleaseOwnership":                           56.13,
		"TestSQLiteCanceledSettlementRetainsAdmission":                      41.79,
		"TestSQLiteCommitRefusesCallerCancellationBeforeSQLPropagation":     19.53,
		"TestSQLiteConcurrentSettlementJoinsNativeCommit":                   15.95,
		"TestSQLiteRuntimeReaderBlocksOtherProcessAndRejectsAliases":        29.15,
		"TestSQLiteSettlementPoolsAndErrors":                                29.12,
		"TestSQLiteSnapshotStatementsEndWithTheirTransaction":               49.73,
		"TestSelfConfigPostgres":                                            4.12,
		"TestSelfConfigSQLite":                                              23.55,
		"TestTeardownMoveRefusalPostgres":                                   11.56,
		"TestTeardownMoveRefusalSQLite":                                     69.27,
		"TestVaultCrashHoldAndVersionBoundOperatorRecoveryPostgres":         2.51,
		"TestVaultCrashHoldAndVersionBoundOperatorRecoverySQLite":           14.62,
		"TestVaultCrossSurfaceAdoptionPostgres":                             9.29,
		"TestVaultCrossSurfaceAdoptionSQLite":                               97.04,
	},
	"internal/store/upgrade": {
		"TestBackupFenceDrainsRuntimeAndSurvivesCoordinatorRestart":           5.48,
		"TestBackupFenceOperatorRotationRequiresRecoveryAndDropsOldIntent":    6.31,
		"TestBackupFenceRejectsWrongProofAndRollsBackFailedPublication":       5.9,
		"TestBuildScratchSchemaSerializesEmptyCheckWithDDL":                   5.81,
		"TestCatalogRejectsUnknownObjectsBeyondTables":                        5.2,
		"TestDomainCatalogAndSourceInspectionShareExactFingerprint":           6.8,
		"TestFreshHierarchyIsAtomicAndCannotBeReinitialized":                  7.06,
		"TestFreshHierarchyRefusesPopulationAndLegacy":                        17.3,
		"TestFreshReconciliationOnlyBeforePopulation":                         6.47,
		"TestLegacyGenesisAndMigrationTamper":                                 5.48,
		"TestLegacyProposalNonceBootstrapIsAtomic":                            9.95,
		"TestManagedConfigurationMigrationBothDatabases":                      360.69,
		"TestMigrationProcessCrashAfterIndividualCommits":                     1.65,
		"TestMigrationResumeRejectsCorruptHistoryBeforeTargetEffects":         1.55,
		"TestPostgresNonTableCatalogDriftRefuses":                             3.51,
		"TestPostgresRollbackAsAnUnprivilegedRole":                            2.42,
		"TestReleaseBackupFenceRestoresOnlyAFrozenSourceAndNothingLater":      6.49,
		"TestRestoreControlSchemaIsExactEmptyAndTransactional":                6.87,
		"TestRestoreMutationFailureDoesNotPublishSQLite":                      21.01,
		"TestRuntimeAdmissionDrainsTransactionsAndNeverRefreshesOldAuthority": 6.71,
		"TestSQLiteCatalogExcludesOnlyReservedSQLitePrefix":                   4.99,
		"TestSQLiteCatalogIncludesForgedReservedTrigger":                      3.57,
		"TestSameArchiveRestoresNewIncarnationsBeforePublication":             33.01,
		"TestSameReleaseRecoveryRequiresNewProofAndNeverRunsMigrations":       7.9,
		"TestSnapshotRejectsAppliedHistoryDrift":                              4.8,
		"TestSourceInspectionChecksActualIdentityAndStrongestEpoch":           6.61,
		"TestUnknownSchemaRefusesWithoutControlEffects":                       49.94,
	},
	"internal/upgradegate": {
		"TestActualCLIMigrateBootRestart":                                         159.94,
		"TestAuthenticatedBackupPreparationPinsFinalTargetAndRefusesMissingProof": 145.06,
		"TestFreshGateInvalidTrustAndRootHaveNoDatastoreEffects":                  21.64,
		"TestFreshGatePersistsSchemaOnlyAndRefusesDifferentResume":                51.65,
		"TestGatePopulatedProcessCrashRoutes":                                     560.88,
		"TestGateProcessCrashRestart":                                             260.05,
		"TestOperatorHistoricalRestoreRequiresCurrentPinAndEscrow":                40.44,
		"TestOperatorInstallationPinPersistsWithoutBackupEvidence":                30.71,
		"TestOperatorRotationJournalCrashResume":                                  78.97,
		"TestOperatorRotationPriorKeyAndLocalEscrow":                              30.76,
		"TestOperatorUncommittedJournalAcceptsFreshAuthenticatedReplacement":      48.87,
		"TestPackagedNightlyReleaseUpgrade":                                       198.58,
		"TestPostgresSignedGateCanonicalRendering":                                6.93,
		"TestUpgradeConfigurationPreflightPreservesInstalledAuthority":            35.06,
		"TestUpgradeMaterialFingerprintSurvivesNextVerifiedBuildAndRecoveryFloor": 46.84,
	},
}
