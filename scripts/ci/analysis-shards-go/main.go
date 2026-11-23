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
// duration grows least (ties go to the lowest shard). Split suites include fuzz
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
		best, bestCost := 0, 0.0
		for index := range shards {
			if cost := shards[index].with(unit).cost(); index == 0 || cost < bestCost {
				best, bestCost = index, cost
			}
		}
		path := unit.pkg.ImportPath
		shards[best] = shards[best].with(unit)
		shards[best].seconds[path] += unit.seconds
		if unit.target != "" {
			shards[best].targets[path] = append(shards[best].targets[path], unit.target)
		}
	}
	return shards, nil
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

// racePackageSeconds holds package-level JSON elapsed, summed across all
// twelve shards of cancelled ci-dev run 45154290942 (2026-11-23). Shard 9 was
// cancelled inside its internal/app invocation, so that invocation has no
// package event: internal/app adds its 764.5 s of observed wall (a lower bound)
// and, for targets that never started, their elapsed from successful run
// 44707819137 (2026-11-20). Parent package events and top-level target events
// are separate measurements; subtest elapsed is never added to either.
// Unobserved prior package estimates remain available; unlisted packages weigh
// one second plus process startup.
var racePackageSeconds = map[string]float64{
	"api":                                 388.483,
	"api/omitzero":                        1.561,
	"cmd/bench-scan":                      1.015,
	"cmd/hikyo":                           1.236,
	"cmd/hikyo-receiver-example":          1.059,
	"internal/adapter":                    1.035,
	"internal/adapter/awssm":              2.429,
	"internal/adapter/awssm/awssmtest":    1.056,
	"internal/adapter/cloudflare":         1.056,
	"internal/adapter/forgejo":            1.432,
	"internal/adapter/githubactions":      3.329,
	"internal/adapter/gitlab":             1.271,
	"internal/adapter/sealedwebhook":      4.278,
	"internal/adapter/vaultkv":            1.28,
	"internal/admission":                  2.134,
	"internal/app":                        6199.232,
	"internal/audit":                      1.047,
	"internal/auditexport":                1.227,
	"internal/authz":                      1.219,
	"internal/authz/gen":                  4.269,
	"internal/backupreceipt":              1.522,
	"internal/bench":                      1.055,
	"internal/boundary":                   7.285,
	"internal/buildcompat":                1.093,
	"internal/cli":                        15.48,
	"internal/clientauth":                 1.035,
	"internal/clienttrust":                1.336,
	"internal/compose":                    1.459,
	"internal/config":                     5.243,
	"internal/configrollout":              16.476,
	"internal/conformance":                158.208,
	"internal/console":                    1.014,
	"internal/crypto":                     2.938,
	"internal/crypto/backup":              38.694,
	"internal/crypto/sealedhook":          8.272,
	"internal/definitions":                1.24,
	"internal/delivery":                   1.016,
	"internal/deliverytarget":             3.038,
	"internal/devupgrade":                 3.398,
	"internal/diagnostics":                1.019,
	"internal/disclose":                   1.017,
	"internal/domain":                     1.234,
	"internal/dotenv":                     1.043,
	"internal/dynamic":                    1.027,
	"internal/dynamic/postgres":           1.02,
	"internal/federationhttp":             1.284,
	"internal/filesync":                   1.753,
	"internal/hostupgrade":                1.031,
	"internal/importer":                   20.055,
	"internal/jsonwire":                   1.025,
	"internal/jwkssource":                 1.149,
	"internal/localsocket":                1.027,
	"internal/mail":                       1.389,
	"internal/mailtest":                   1.039,
	"internal/mcpserver":                  3.481,
	"internal/netpolicy":                  1.017,
	"internal/oauth2rp":                   1.055,
	"internal/oidcfed":                    1.741,
	"internal/oidcrp":                     14.014,
	"internal/oidctest":                   1.038,
	"internal/operation":                  1.021,
	"internal/operator":                   15.82,
	"internal/operator/api/v1alpha1":      7.077,
	"internal/operator/client":            1.524,
	"internal/pairing":                    116.397,
	"internal/parameters":                 1.045,
	"internal/pathutil":                   1.016,
	"internal/pki":                        14.312,
	"internal/releaseidentity":            1.016,
	"internal/releasetrust":               13.765,
	"internal/remotefetch":                1.367,
	"internal/repscan":                    1.588,
	"internal/runtimeconfig":              1.146,
	"internal/samlsp":                     2.2,
	"internal/scanning":                   1.137,
	"internal/scanning/bench":             1.018,
	"internal/scanning/gen":               1.015,
	"internal/schema":                     1.44,
	"internal/scimproto":                  1.049,
	"internal/sealedreceiver":             1.048,
	"internal/securefile":                 1.019,
	"internal/securitydeployment":         18.837,
	"internal/securityfeed":               2.913,
	"internal/securitymaintenance":        3.32,
	"internal/securitystatus":             1.068,
	"internal/selfupdate":                 7.093,
	"internal/server":                     71.151,
	"internal/service":                    2024.542,
	"internal/sshca":                      6.589,
	"internal/storagehealth":              1.375,
	"internal/store":                      2637.607,
	"internal/store/auditrow":             1.021,
	"internal/store/authn":                1.022,
	"internal/store/migrate":              305.975,
	"internal/store/tx":                   39.127,
	"internal/store/upgrade":              508.753,
	"internal/testutil/fixtureref":        1.263,
	"internal/tlspolicy":                  6.424,
	"internal/transit":                    1.134,
	"internal/updatecheck":                4.406,
	"internal/updater":                    1.071,
	"internal/upgradeassembly":            1.303,
	"internal/upgradebundle":              1.427,
	"internal/upgradecompat":              2.227,
	"internal/upgradecustody":             568.992,
	"internal/upgradegate":                1849.277,
	"internal/webauthnrp":                 1.054,
	"internal/webauthntest":               1.027,
	"scripts/acceptance/github-contract":  2.08,
	"scripts/acceptance/mcp-fixture":      1.751,
	"scripts/ci":                          259.887,
	"scripts/ci/analysis-shards-go":       30.965,
	"scripts/ci/benchmark":                4.415,
	"scripts/ci/chartdoctor":              4.048,
	"scripts/ci/chartfixture":             1.07,
	"scripts/ci/checksuites":              1.029,
	"scripts/ci/codspeed-verify":          1.049,
	"scripts/ci/feedback-reuse-size":      1.014,
	"scripts/ci/networkpolicyfixture":     1.022,
	"scripts/ci/pr-gate":                  62.369,
	"scripts/ci/prepare-lint-cache":       1.051,
	"scripts/ci/qualification":            10.311,
	"scripts/ci/queue-report":             2.19,
	"scripts/ci/retire-runs":              1.112,
	"scripts/ci/securitycomposerehearsal": 1.075,
	"scripts/ci/source-proof":             2.111,
	"scripts/ci/unattendedfixture":        1.051,
	"scripts/ci/workflow-lint":            1.034,
	"scripts/mcp-production-client":       1.111,
	"scripts/mcp-public-smoke":            1.272,
	"scripts/operator-floor":              2.826,
	"scripts/ops-floor":                   1.041,
	"scripts/release":                     1.352,
	"scripts/release/assemble-upgrade":    283.886,
	"scripts/release/embed-compatibility": 49.877,
	"scripts/release/nightly":             1.619,
	"scripts/release/stable":              18.129,
	"scripts/security/alert-route":        1.66,
	"scripts/security/artifact-harness":   1.726,
	"scripts/security/release-evidence":   1.169,
}

// raceTargetSeconds holds top-level target proportions for the source targets
// of each split suite. Each comes from run 45154290942, else from run
// 44707819137 when that run's shard 9 was cancelled before starting it (seven
// internal/app targets), else from the elapsed of the subtests that finished
// before the cancel, a lower bound (TestRollbackRefusesAnythingButTheFenced
// SnapshotsOwnRows: three of its subtests). Subtest events are excluded to
// avoid counting parent and child elapsed twice, and targets absent from the
// source tree are dropped. The planner discovers every target, including fuzz
// seeds and runnable examples, and gives missing weights its one-second
// fallback. Each split suite is scaled to racePackageSeconds. Keep
// internal/lint whole so its repository load is not repeated across processes.
// Updating these estimates changes placement only, never the target union or
// DB scheduling.
var raceTargetSeconds = map[string]map[string]float64{
	"internal/app": {
		"TestAWSSTSUsesItsOwnExactOriginEgressPolicy":                                0,
		"TestAWSWorkloadIdentityFollowsNodePolicy":                                   0,
		"TestAbortMarksOnlyAJournalStillPreparing":                                   0,
		"TestAbortingAPreparationRemovesItsCopiesAndParksTheRetry":                   0.01,
		"TestActivatingStrictClosesClassicalKeepAliveAndServesOnlyHybrid":            18.85,
		"TestActualDefaultDevCLIStartsAndRestarts":                                   284.27,
		"TestAdapterActivationLoaderRefusesBeforePendingCredentialOpen":              0,
		"TestAdapterEgressOriginDropsProviderAPIPaths":                               0,
		"TestAdapterFactoryHoldsLegacyOriginsBeforeProviderConstruction":             0,
		"TestAdapterLoaderCleanupCapturesAttemptContextAndAlwaysZerosCredential":     0,
		"TestAdapterLoaderGatesBeforeLoadAndEveryPlaintextOpen":                      0,
		"TestAdapterLoaderRejectsUnknownProviderBeforeCredentialOpen":                0,
		"TestAdapterModuleFactoryDispatchesCompiledInProviders":                      0,
		"TestAdapterModuleFactoryRegistryIsTotal":                                    0,
		"TestAdapterModuleFactoryReleasesPartialConstructionOnce":                    0,
		"TestAdapterModuleFactorySelectsPrivateEgressByExactOrigin":                  0,
		"TestAdapterModuleLeaseReleasesSuccessOnce":                                  0,
		"TestAdminAuthRefusesUnverifiedLegacySchema":                                 15.02,
		"TestAdminAuthResourceOwnership":                                             37.15,
		"TestAdminAuthWarnsWhenRootRotationPending":                                  11.67,
		"TestAdminCreateAdoptsRunningServerSeedInsteadOfCommandDefaults":             21.89,
		"TestAdminCreateRequiresFreshServerSeed":                                     12.54,
		"TestAdoptedProfileOutranksAndWarnsAboutADifferingBootstrapValue":            20.07,
		"TestApplyMemoryLimit":                                                       0,
		"TestAutomaticBundleFormatDoesNotBlameTheReleaseForAProbeFailure":            0.2,
		"TestAutomaticBundleFormatRejectsHistoricalCandidateBeforeApplication":       0.01,
		"TestAutomaticCleanupFailureDoesNotStopCompletedService":                     0.01,
		"TestAutomaticCustodyPolicySeamOnRealSources":                                58.18,
		"TestAutomaticDiscoveryCompleteResumeBundleStillPreparesSelectedExecutables": 0.46,
		"TestAutomaticDiscoveryLegacyAndInterruptedRouteKeepExactBridge":             0.47,
		"TestAutomaticDiscoveryNoopDoesNotFetchHistoricalPredecessors":               0.09,
		"TestAutomaticDiscoveryPreservesDeterministicRouteTieBreak":                  0.7,
		"TestAutomaticDiscoveryRecentUpgradeDoesNotFetchAncientHistory":              0.19,
		"TestAutomaticDiscoveryRejectsSnapshotBelowInstalledFloorBeforeFetching":     0.13,
		"TestAutomaticDiscoveryResolvesShallowAlternativesBeforeLongerHistory":       0.48,
		"TestAutomaticFailuresNeverRestartOldExecutable":                             0.03,
		"TestAutomaticFinalHealthyResumeRestartsBeforeCompletion":                    0.01,
		"TestAutomaticHealthFailureIsTerminalWithoutRewritingHealthyDB":              0.01,
		"TestAutomaticIntermediatePreAdmissionCrashRetainsFullOriginalRoute":         0.04,
		"TestAutomaticPendingRejectsMismatchedHopAndBindings":                        0.04,
		"TestAutomaticPolicyCheckReadsTheStoredValue":                                0.01,
		"TestAutomaticPrewriteRetryMeasuresRealSQLiteAndRestoreProof":                38.26,
		"TestAutomaticPublicBundleCleanupChecksJournalPublication":                   0.03,
		"TestAutomaticRouteMaintainsFenceAcrossHopsAndPersistsExactIntent":           0.02,
		"TestAutomaticUnstartedSecurityRefusalRestoresPriorJournal":                  0.02,
		"TestAutomaticUpgradePackagedNightlyRoute":                                   200.2,
		"TestAutomaticWriteIntentReconcilesOnlyProvenPrewriteOrAppliedStates":        0.04,
		"TestBackupExportJobRecordsFailureLoudlyWithoutPlaintext":                    18.67,
		"TestBackupExportJobRunsOnceThenGatesOnTheInterval":                          20.07,
		"TestBackupKeygenHybridReservesDestinationFirst":                             0,
		"TestBackupKeygenSelectsAlgorithmOnlyByFlag":                                 0.01,
		"TestBackupPruneJobRemovesOnlyAgedArchives":                                  20,
		"TestBackupRestoreDiagnosticsLevels":                                         83.34,
		"TestBackupScheduledGuardsJobRegistration":                                   0,
		"TestBootGuard":                        0,
		"TestBootLogsEffectiveSQLitePoolSizes": 18.34,
		"TestBootNodeConfigurationCarriesAdoptedStrictIntoTheRecoveryBaseline":            0.01,
		"TestBootRefusesConfiguredUpdaterBeforeOpeningResources":                          0,
		"TestBootResourceOwnershipOnFailure":                                              75.6,
		"TestBootSuccessTransfersResourceOwnershipToServer":                               16.64,
		"TestBootWarmsOpenAPIBeforeListening":                                             18.38,
		"TestBootWithRootKeyFileAndWrongKeyRefused":                                       10.99,
		"TestBootWithStrictProfileServesOnlyHybridPeers":                                  17.78,
		"TestBootstrapBlanksOwnerPolicyAndEffectivePolicyRecoversIt":                      0.01,
		"TestBootstrapDeploymentBootSeedsSelectorsAndKeepsPendingRepairFenced":            20.05,
		"TestBootstrapDeploymentDatabaseProofPrecedesSignedSubmitAndTransport":            3.32,
		"TestBootstrapDeploymentEnrollmentPinsStandaloneNode":                             12.97,
		"TestBootstrapDeploymentRefusesEveryOperationOnAPreRolloverRow":                   37.22,
		"TestBootstrapDeploymentRefusesSignerThatIsNotTheEnrolledKey":                     14.57,
		"TestBootstrapDeploymentRefusesUninitializedHAEnrollmentBeforeReadingCustody":     0,
		"TestBootstrapDeploymentRejectsReplacedAliasAndExpiredPrivateProof":               3.22,
		"TestBootstrapDeploymentRenewalPreservesCommittedAuthorityAfterRestart":           15.13,
		"TestBootstrapDeploymentRootPrepareIsSealedButNotPersisted":                       17.58,
		"TestBootstrapDeploymentSignerFileIsClosedSet":                                    0.01,
		"TestBootstrapDeploymentSignerUsesStrictInstalledPEM":                             0,
		"TestBootstrapDeploymentStrictProfileSignsEveryDecisionAndRefusesOldRegime":       17.55,
		"TestBootstrapInitialHASourceCarriesUnchangedCorrespondenceAndReproof":            2.91,
		"TestBootstrapSingletonTopologyRequiresActualPrerequisites":                       21.71,
		"TestCandidateConfigurationFailureKeepsRestoreRequired":                           76.22,
		"TestCandidateHealthChecksEnrolledNextRootWithoutProvider":                        19.23,
		"TestCandidateHealthValidatesSavedNodeBeforeRetiredBootstrap":                     0,
		"TestChownSQLiteFilesRefusesALinkLeftByTheServiceAccount":                         0,
		"TestChownSQLiteFilesToleratesMissingSidecarsAndFailsOnReal":                      0,
		"TestClearingAnAttemptKeepsTheRecordWhileTheJournalStays":                         0.01,
		"TestConcurrentDevelopmentBootsKeepOneRoot":                                       0.31,
		"TestConfigAndPolicyVocabulariesAgree":                                            0,
		"TestConfiguredEvidenceIsLoggedAtActivationNotPreparation":                        18.95,
		"TestCoordinatorAfterRunFollowsHostAndClusterButNotNative":                        0,
		"TestCoordinatorBindsTheImageTheOwnerObservedToTheSignedRelease":                  0,
		"TestCoordinatorFailureIsRetriedAndNeverChangesTheEngineResult":                   0,
		"TestCoordinatorFollowsOnlyAnOlderRelease":                                        0,
		"TestCoordinatorProductionBindingRefusesAnUnauthenticatedRelease":                 0,
		"TestCoordinatorRefusesAReleaseTheCatalogWithdrew":                                0,
		"TestCoordinatorSkipsAnExecutableItCannotOwnWithoutDownloading":                   0,
		"TestCoordinatorWaitsForAQuietHealthyJournal":                                     0,
		"TestCustodyBackupPolicyAgainstRealVault":                                         5.19,
		"TestDefaultDevelopmentCustodyRefusesSymlink":                                     0,
		"TestDeploymentModuleNeverInfersProviderFromCredential":                           0,
		"TestDeploymentModuleRefusesClassicGitHubPAT":                                     0,
		"TestDevAWSAPIConditionalPromotionReplayAndDeletion":                              0,
		"TestDevAWSFactoryKeepsOtherProvidersOnTheirExistingFake":                         0,
		"TestDevAWSMarkerDoesNotEnableAProductionFallback":                                0,
		"TestDevAWSModuleAdoptsOnlyReviewedResourceAndVersion":                            0.01,
		"TestDevAWSNamespaceAndSeedAreStableAcrossLeases":                                 0,
		"TestDevAWSPerKeyPlannedDeletionRequiresAdoptionAndRestoresIdentity":              0,
		"TestDevAWSSeedNeverOverwritesAnExistingResource":                                 0,
		"TestDevAWSUnownedFixtureSurvivesWorkerBeforePlan":                                0,
		"TestDevAWSWorkerDoesNotSeedWithoutAuthorizedValidDelivery":                       0,
		"TestDevAdmissionOverrideCoversDiscoveryTraffic":                                  0.01,
		"TestDevBootGeneratesAndReusesRootKey":                                            19.52,
		"TestDevBootSeparatesPublicAndOperationalRoutes":                                  25.73,
		"TestDevelopmentControlsCannotChangeDeploymentTrustContext":                       18.56,
		"TestDevelopmentNodeControlsChangeActualRuntimeAfterActivation":                   10.19,
		"TestDevelopmentProviderPreservesRemoteStateAcrossOrdinaryReloadAndRefusesSwitch": 15.89,
		"TestDevelopmentProviderSwitchRechecksConfigurationAfterPreparation":              20.89,
		"TestDevelopmentRootRefusesSymlinkAndPreservesTarget":                             0,
		"TestEmailInvitationFailureCountedAcrossActivation":                               17.13,
		"TestEnrolledSourceLoaderAcceptsOnlyExplicitSingletonHAIdentity":                  17.52,
		"TestEscrowLocalCLIRefusesUnstampedProductionBeforeDatabase":                      0,
		"TestEscrowLocalCLIUsesAdmittedCurrentHierarchy":                                  27.53,
		"TestExplicitMigrateThenBootWithoutAutoMigrate":                                   16.52,
		"TestExportRefusalsNameFlagsNotEnvironment":                                       0,
		"TestExportRefusesBadHybridPolicyBeforeAnyOutput":                                 0.03,
		"TestFailedRestoreDrillCleanupPreservesExistingTarget":                            0.02,
		"TestFreshBootstrapOriginAuthoritySurvivesBoot":                                   57.03,
		"TestHTTPServerSlowClientLimitsConfigured":                                        0,
		"TestInvalidPublicTrustDoesNotCreateSQLiteForBackupOrRestoreStatus":               0,
		"TestLastBackupSurvivesFailedAttemptsUntilItsReplacementIsJournaled":              0.02,
		"TestLegacyDrillFixtureRefusesCLILoginEvidence":                                   42.69,
		"TestLegacyDrillFixtureRefusesPendingAuthenticationEvidence":                      13.47,
		"TestLegacyDrillFixtureRefusesProjectCapacityEvidence":                            34.12,
		"TestListenerReportsConfiguredAndObservedEvidenceSeparately":                      0.01,
		"TestLiveSnapshotFloorUsesTheRowAndZeroOnlyWhenAbsent":                            40.89,
		"TestLocalCLISocketServesPublicAPIAndIsRemovedOnShutdown":                         18.8,
		"TestLocalMintPreparationFailurePrecedesStateAccess":                              0,
		"TestMCPGracefulShutdownCancelsCallWhenDrainExpires":                              0.05,
		"TestMCPGracefulShutdownLetsActiveCallComplete":                                   0,
		"TestMaintenanceBootHonorsBootstrapOperationalBinding":                            0.02,
		"TestManagedCertificateExpiryMetricsAndExpiredPairRefusal":                        0,
		"TestManagedCertificateValidatesPairAndRetainsImmutableCertificate":               0,
		"TestMigrateDiagnosticsLevelsAndNoOp":                                             74.65,
		"TestMigrateDiagnosticsRefusalDoesNotClaimWrites":                                 0,
		"TestMigrateNeedsNoRootKey":                                                       18.28,
		"TestMigrationRefusesUnverifiedExistingSchemaBeforeWrites":                        36.57,
		"TestMinRestoreSchemaVersionMatchesTheMigration":                                  0,
		"TestNativeEnrollmentDoesNotAdoptAnExistingInstanceIntoAnUnboundVault":            12.47,
		"TestNativeEnrollmentFailureBeforeThePinLeavesNoPin":                              57.82,
		"TestNativeEnrollmentFreshHostCreatesBoundVaultAndPin":                            37.83,
		"TestNativeEnrollmentIsIdempotent":                                                25.03,
		"TestNativeEnrollmentKeepsHostWithExternalOperatorKeyRefused":                     0,
		"TestNativeEnrollmentLeavesAnUnfinishedHostUpgradeAlone":                          47.64,
		"TestNativeEnrollmentLeavesBootedHostWithoutOurVaultRefused":                      0,
		"TestNativeEnrollmentRefusesALegacyHostWithoutChangingIt":                         0,
		"TestNativeEnrollmentRefusesVaultBoundToAnotherInstance":                          43.04,
		"TestNativeEnrollmentRefusesWhileAnotherOperatorUpgrades":                         0,
		"TestNativeEnrollmentResumesAfterFailedFirstBoot":                                 22.48,
		"TestNativeEnrollmentResumesAnInterruptedMigrate":                                 58.79,
		"TestNativeEnrollmentStartsServiceLeftUnhealthyByAnInterruptedBoot":               33.45,
		"TestNativeListenerProfilesAgainstRealPeers":                                      0.03,
		"TestNativeNoFinalGateReadsTheControlRowThenTheJournal":                           0.11,
		"TestNativeNoFinalGateRefusesAnotherInstallationsRow":                             0.02,
		"TestNativePrepareForgetsAnEarlierRollback":                                       0.04,
		"TestNativeReconcileReleasesAServingSourceUnderAPreparingJournal":                 0.1,
		"TestNativeReleaseCountsARollbackWhoseCoordinatorDiedAfterTheJournalCleared":      0.04,
		"TestNativeReleaseWaitsForATargetStillHoldingTheUpgradeLock":                      0.02,
		"TestNativeRestoreAbortedCountsAFinishedRollbackUnderAnUnfencedJournal":           0.03,
		"TestNativeRestoreAbortedFencedPreparingReportsARollbackOnlyWhenTheJournalSaysSo": 0.09,
		"TestNativeRestoreAbortedFinishesARollbackKilledMidway":                           0.11,
		"TestNativeRestoreAbortedKeepsThePinUnlessTheSourceIsProven":                      0.05,
		"TestNativeRestoreAbortedRefusesAJournalOfAnotherOperation":                       0.04,
		"TestNativeRestoreAbortedRefusesAnUnguardedUnfinishedOperation":                   0.02,
		"TestNativeRestoreAbortedRunsTheTargetsRollbackAndClearsWhatItLeft":               0.04,
		"TestNativeRestoreAbortedWithNothingJournaledReleasesThePin":                      0.03,
		"TestNativeRollbackExitCodeCrossesTheHandoff":                                     0,
		"TestNativeRollbackExitCodeFollowsWhetherTheSnapshotWasRestored":                  0,
		"TestNativeRollbackRefusesAnythingButTheSnapshotsOwnRows":                         206.42,
		"TestNativeRollbackReleasesAnUntouchedSourceUnderALaggingJournal":                 82.87,
		"TestNativeRollbackRestoresTheHealthySourceAndNothingElse":                        23.7,
		"TestNativeRollbackResumesFromRollingBack":                                        34.51,
		"TestNativeRollbackRunFindsTheRecordTheUpdateWrites":                              0.01,
		"TestNativeRollbackRunsTheHookBeforeTheSourceAcceptsTheJournal":                   41.41,
		"TestNativeSecurityCoordinatorClassifiesFailureBeforeFirstFence":                  0.04,
		"TestNativeSecurityCustodyRefusesOperatorKeyOutsideItsVault":                      0,
		"TestNativeSecurityDriverCarriesExactRouteAndDeadlineThroughHandoff":              0.06,
		"TestNativeSecurityDriverObservesInstalledSourceWithoutVersionLookup":             0.02,
		"TestNativeSecurityDriverPrestopRefusalsPreserveHealthySource":                    0.2,
		"TestNativeSecurityDriverRejectsSchemaRootAndRuntimeSubstitution":                 0.06,
		"TestNativeSecurityDriverStagesSignedRouteBeforeApplication":                      0.06,
		"TestNativeSecurityEligibilityExpiredAfterStagingRefusesMaintenance":              0,
		"TestNativeSecurityExactSelectionDoesNotFollowLatest":                             0,
		"TestNativeSecurityExactSelectionRejectsDiscoverySubstitution":                    0,
		"TestNativeSecurityExactSelectionRejectsPreparedIdentitySubstitution":             0,
		"TestNativeSecurityFinalAuthorizationCannotCrossAdmissionDeadline":                0,
		"TestNativeSecurityFirstUseRefusalRetainsRunningSourceProof":                      0.04,
		"TestNativeSecurityGuardRefusesRestoredSourceAndChangedAuthority":                 0.05,
		"TestNativeSecurityHandoffPreservesCompleteIdentity":                              0,
		"TestNativeSecurityHandoffPreservesDefiniteRefusalAndAmbiguousFailure":            0.01,
		"TestNativeSecurityHandoffPreservesStagedRouteAndEligibility":                     0,
		"TestNativeSecurityIdentityFlagsRefuseAmbiguousInputBeforeHostAccess":             0,
		"TestNativeSecurityIdentityParsingKeepsDefaultAndExactProfiles":                   0,
		"TestNativeSecurityParseRefusalsUnderMaintenanceSourceAreNotStarted":              0,
		"TestNativeSecurityPendingOperationKeepsExactIdentity":                            0,
		"TestNativeSecurityPolicyFlagsRequireExactRelease":                                0,
		"TestNativeSecurityReconcileRefusesUnfinishedHostJournal":                         0.03,
		"TestNativeSecurityRoutePinRejectsSameTargetWithChangedRoute":                     0.01,
		"TestNativeSecuritySourceProofUnavailableDoesNotQueryReleaseDiscovery":            0,
		"TestNativeSourceEvidenceCommandQuotesTheIdentity":                                0,
		"TestNativeSourceEvidenceRetainsTheBundleRuntimeEnvironmentNames":                 0,
		"TestNativeSourceIsRestartedOnlyAfterAProvenPreWriteFailure":                      0.01,
		"TestNativeTLSBootServesHTTPSAndKeepsOpsPlaintext":                                18.81,
		"TestNextRootLiveSelectionKeepsRotationSeparate":                                  22.96,
		"TestNextRootPreparationRejectsUnavailableSources":                                89.75,
		"TestNextRootSeedRequiresExactEnrolledProjection":                                 19.45,
		"TestNodeRuntimeCancelledActivationReleasesNewListener":                           18.48,
		"TestNodeRuntimeImportsTLSFilesOnce":                                              10.99,
		"TestNodeRuntimeInstallsIndependentCapacityAndPolicy":                             14.49,
		"TestNodeRuntimeListenerFailureKeepsOldGraphAndReleasesReservation":               17.77,
		"TestNodeRuntimeListenerMoveDrainsOldHTTPResponse":                                14.52,
		"TestNodeRuntimeManagedRestartDoesNotReadDeletedTLSFiles":                         20.06,
		"TestNodeRuntimeMissingHANodeBootsOnlyAdministrativeRecovery":                     4.06,
		"TestNodeRuntimePostgresPoolAppliesWithoutReplacingIdentity":                      2.4,
		"TestNodeRuntimeReservesMovesAndSwapsListeners":                                   19.12,
		"TestNodeRuntimeScheduledBackupUsesInstalledDestination":                          18.91,
		"TestNodeRuntimeTLSRotationAndPlaintextTransition":                                19.07,
		"TestOperationalListenerFailureStopsWholeLifecycle":                               18.76,
		"TestOrdinaryBackupPublicAdmissionAndDataOnlyRestore":                             49.83,
		"TestOwnerRuntimeActivationFailureResumesAdministrativeGraph":                     15.09,
		"TestOwnerRuntimeCanceledSQLiteSettlementCleanupWithoutRollbackHook":              18.93,
		"TestOwnerRuntimeDrainsRequestsBeforeInstalling":                                  18.9,
		"TestOwnerRuntimeFailedTargetFencesBusinessAndPreservesHTTPRepair":                19.42,
		"TestOwnerRuntimeInstallsHTTPAuthAndRetentionGraph":                               14.42,
		"TestOwnerRuntimeJoinsCanceledSQLiteSettlement":                                   11.03,
		"TestOwnerRuntimePreparationFailureAndMailOnlyKeepActiveGraph":                    10.66,
		"TestOwnerRuntimeRefusesPasswordCostChangeAfterCredentialEstablishment":           18.87,
		"TestOwnerRuntimeReloadsProjectKeyLimit":                                          16.13,
		"TestPKIWorkerWaitsAfterPartialProgressError":                                     0.03,
		"TestPairingIdentityObservesNativeTLSAndNeverInfersPublicKey":                     0.09,
		"TestPairingIdentityOverridePrecedesNativeCertificateAndCapturesGeneration":       0,
		"TestPairingIdentityRequiresExplicitHTTPSOriginWithoutIO":                         0,
		"TestParseCgroupMemory":                                                           0,
		"TestPendingMigrationsWithAutoMigrateOffRefusesToServe":                           0.93,
		"TestPostgresRestoreDestinationRefusesExpiredOwnerAndOccupiedTarget":              13.57,
		"TestPreflightAllowsAMissingDatabaseOnlyForARollingBackJournal":                   0.06,
		"TestPreparationNeverWroteOnlyForTheUntouchedSource":                              0,
		"TestPrivacyCLIRefusesBeforeBoot":                                                 0,
		"TestPrivacyReceiptClosedSchema":                                                  0,
		"TestPrivacyReceiptFileBoundary":                                                  0,
		"TestProductionBackupCustodyRefusesBeforeDatabaseAdmission":                       18.65,
		"TestProductionBootWithoutRootKeyRefuses":                                         0,
		"TestProductionWiringUsesGuardedSAMLProviderConstructor":                          0.16,
		"TestProfileChangeReplacesTheListenerTransport":                                   0,
		"TestProxyHTTPSOriginEmitsHSTSOnLoopbackBackend":                                  18.69,
		"TestRealBootKeepsAdoptedStrictWhenTheNodeIsMissingFromTheNodeDocument":           41.78,
		"TestRecoveryCapabilityExpiresWithOwnerAndArchive":                                94.22,
		"TestRecoveryPostgresRollsBackWhenMigrationOwnerDies":                             6.59,
		"TestRecoveryTransactionsUseSchema65GrantProjection":                              43.84,
		"TestReleaseCompatibilityMatchesActualBothEngineSchemas":                          30.78,
		"TestRestartedTargetIdlesAtRollingBack":                                           0,
		"TestRestoreDiagnosticsAuthenticationFailureStopsBeforeTarget":                    0,
		"TestRestorePostgresRollsBackWhenMigrationOwnerDies":                              4.22,
		"TestRestoreRequiredRestartExposesOnlyOperationalHealth":                          19.36,
		"TestRestoreRunRefusesMixedHybridHeaderBeforeTarget":                              0,
		"TestRestoreSchemaRechecksPreflightBeforeAnyGooseWrite":                           3.23,
		"TestRestoreUnlockReadsEitherIdentityFile":                                        0,
		"TestRollbackJournaledLeavesAJournalTheSourceAccepts":                             50.89,
		"TestRollbackJournaledResumesFromRollingBack":                                     33.64,
		"TestRollbackPostgresRefusesWhileAnotherSessionIsConnected":                       9.62,
		"TestRollbackPreflightRefusesObjectsTheRoleDoesNotOwn":                            7.54,
		"TestRollbackReadFailuresAreNotPermission":                                        38.53,
		"TestRollbackRefusesAnArchiveFromAnotherRouteBeforePublishing":                    50.73,
		"TestRollbackRefusesAnythingButTheFencedSnapshotsOwnRows":                         124.42,
		"TestRollbackRestoredRowIsCheckedByDeploymentKind":                                83.1,
		"TestRollbackRestoresTheFencedSnapshotAndNothingElse":                             29.14,
		"TestRollbackSecurityImageReportsAnEarlierRestoreOnARetry":                        0.04,
		"TestRunAutomaticUpgradeCallsThePolicySeamBeforeAnyEffect":                        0.01,
		"TestRunPreparedUnattendedRefusesAnUnreadableJournal":                             0,
		"TestSchedulerDeadlineAndStaleSuccessAreLoudOpsLogs":                              0.01,
		"TestSchedulerExposesLastPruneSuccessOnOpsLog":                                    0,
		"TestSchedulerHADropsLeadershipWhenRenewBlocks":                                   0.03,
		"TestSchedulerHADropsLeadershipWhenRenewLoses":                                    0.04,
		"TestSchedulerHALeaderRunsStartupAndReleasesOnStop":                               0.01,
		"TestSchedulerHAOnlyLeaderRunsJobs":                                               0.06,
		"TestSchedulerHAReleasesProvisionalClaimOnStop":                                   0.01,
		"TestSchedulerHAThreeNodesOnePostgres":                                            2.41,
		"TestSchedulerRunsAtStartupAndOnTicker":                                           0.01,
		"TestSchemaAheadOfBinaryRefusesToServe":                                           18.9,
		"TestSealedWebhookActivationRefusesUnconfirmedTrustBoundary":                      0.01,
		"TestSealedWebhookFactoryBuildsOnlyInstanceAdminOrigins":                          0,
		"TestSealedWebhookMixedRegistryActivatesEachTargetUnderItsOwnProtocol":            0.01,
		"TestSealedWebhookProtocolTwoActivationRefusesBeforeServing":                      0.08,
		"TestSecurityCoordinatorConfigurationRequiresTheRootOperatorOutsideTheCluster":    0,
		"TestSecurityCoordinatorFollowerIsRebuiltWhenTheConfigurationChanges":             0,
		"TestSecurityDeploymentDeadlineDoesNotStopAnotherInstallation":                    0,
		"TestSecurityDeploymentDeadlineGivesTheProofItsOwnWindow":                         0,
		"TestSecurityDeploymentDeadlineReadsTheTargetAgainBeforeStoppingIt":               0.01,
		"TestSecurityDeploymentDriverIsFoundUnderThePolicyGuard":                          0,
		"TestSecurityDeploymentDriverReconcilePreservesObservation":                       0,
		"TestSecurityDeploymentDriverStatusCopyNeverFailsTheRun":                          0,
		"TestSecurityDeploymentFinalPreparationCannotAdoptChangedSource":                  0,
		"TestSecurityDeploymentProvesAnAbortedSourceOnAFreshWindowAfterTheRestore":        0,
		"TestSecurityDeploymentProvesTheSourceOnAFreshWindowAfterTheOneShot":              0,
		"TestSecurityDeploymentRestoresAnAbortedSourceFromAPersistedOperation":            0,
		"TestSecurityDeploymentRestoresTheSourceOnlyAfterProvingItUntouched":              0,
		"TestSecurityDeploymentRollbackThatRestoredNothingIsNotReportedAsARestore":        0,
		"TestSecurityDeploymentRollsBackAFailedTargetAndProvesTheSource":                  0,
		"TestSecurityDeploymentSourceWaitEndsOnShutdown":                                  0,
		"TestSecurityDeploymentWaitRejectsRestoreAndNeverReadyTarget":                     0,
		"TestSecurityDeploymentWaitsForActualNewProcessReadiness":                         0,
		"TestSecurityEnrollHookSucceedsForAnEnrolledInstallationWithoutPinningOrTheLock":  0,
		"TestSecurityEnrollRecordsAPinTheOperatorRemoved":                                 0,
		"TestSecurityFeedAdvisoryAddsNewFixesToExistingTargets":                           0.01,
		"TestSecurityFeedAdvisoryDerivesTargetsAndComputesFixes":                          0.03,
		"TestSecurityFeedAdvisoryErrorsAreSpecific":                                       0.03,
		"TestSecurityFeedAdvisoryRerunKeepsOtherProfiles":                                 0.02,
		"TestSecurityFeedCriticalFlawRoundTripThroughRealClient":                          0.41,
		"TestSecurityFeedFlagsRejectAmbiguousAuthorityAndPositionals":                     0,
		"TestSecurityFeedInitRootIsExplicitAndDoesNotReplaceRoot":                         0.02,
		"TestSecurityFeedInvalidCatalogNeverCallsReleaseVerifier":                         0.01,
		"TestSecurityFeedOutputIsCheckedWithRealClient":                                   0.15,
		"TestSecurityFeedPublishRefusesNonAdvancingCatalogAndAllowsResigning":             0.16,
		"TestSecurityFeedPublishRequiresIndependentVerificationBeforeOutput":              0.05,
		"TestSecurityFeedRefreshBoundsAndRefusals":                                        0.09,
		"TestSecurityFeedRootConfigRejectsDuplicateUnknownAndInvalidThreshold":            0,
		"TestSecurityFeedWithdrawalIsNotBlockedByUnavailableRelease":                      0.1,
		"TestSecurityGateProofFailsClosedWithoutAProver":                                  0,
		"TestSecurityImagePlatformDigestComesFromUnambiguousIndex":                        0,
		"TestSecurityMaintenanceAuthenticatedFeedAppliesPackagedRoute":                    327.45,
		"TestSecurityMaintenanceConfigurationRefusesOwnerAndOfflineConflicts":             0,
		"TestSecurityMaintenanceHelpAvoidsAuthorityAndDatastore":                          0,
		"TestSecurityMaintenanceKubernetesConfigurations":                                 0,
		"TestSecurityMaintenanceLocalPolicyChangeCannotRaceStagedApply":                   0,
		"TestSecurityMaintenanceNativeTimeoutIsBounded":                                   0,
		"TestSecurityMaintenanceStatusFileIsOptionalAndKeepsPolicyBinding":                0,
		"TestSecurityMaintenanceStoreOnWorldWritableVolumeNeedsAcceptance":                0,
		"TestSecurityOperationalHealthUsesLiveLoopbackReadiness":                          0,
		"TestSecurityPolicyDigestIsIndependentOfConfigurationStructShape":                 0,
		"TestSecurityPublicationTargetsCheckEveryBinaryPlatform":                          0,
		"TestSecurityPublicationTargetsDedupeReleaseVerificationButCheckEachImage":        0,
		"TestSecurityStageDirectoryKeepsSourceAndIsPerTarget":                             0,
		"TestServeCancellationStopsBothListeners":                                         20.69,
		"TestServeWithReadySignalsOnlyAfterHTTPServingStarts":                             20.79,
		"TestServerDiagnosticsDoNotClaimSuccessAfterListenerFailure":                      18.62,
		"TestServerDiagnosticsLevelsAndReadiness":                                         70.4,
		"TestServiceBudgetCanOnlyBeDisabledByExplicitDevConfig":                           0,
		"TestSettlingKeepsOnlyARecordThatRolledBack":                                      0.03,
		"TestSingletonHARestoreBootRetainsCoordinationThroughSourceRepair":                7.15,
		"TestSingletonReplacementBootUsesActualHAMode":                                    5.09,
		"TestStoredManagedPolicyOverridesTheEnvironmentSeed":                              0.01,
		"TestStrictProfileRefusesAListenerWithoutCertificate":                             0,
		"TestSweepKeepsOnlyWhatTheJournalReferences":                                      0,
		"TestTargetOwnsControlRowOnlyBetweenFirstWriteAndFinalGate":                       0,
		"TestUnattendedCoordinatorChild":                                                  0,
		"TestUnattendedCoordinatorDisabledNeedsNoCustody":                                 0,
		"TestUnattendedPackagedEnrollmentUpgradeAndIntermediateCredential":                352.17,
		"TestUnattendedPreflightRequiresEverySelectedExecutable":                          0.03,
		"TestUnattendedScratchPreservesUnownedDataAndReusesOwnedSchema":                   2.9,
		"TestUnattendedScratchRefusesLiveAliasesAndChangedDSN":                            0.28,
		"TestUnknownProfileWithoutCertificateFailsClosed":                                 0,
		"TestUpgradeAtARollingBackJournalOnlyFetchesTheSourceRelease":                     0.01,
		"TestUpgradeCLISignsActualDrill":                                                  0,
		"TestUpgradeDrillActualBothEngineRecoveryAndConfigOnlyEscrow":                     70.34,
		"TestUpgradeDrillAppliedLedgerKeepsSourceAuthorityAndRotatesScratch":              50.3,
		"TestUpgradeDrillAutoCredentialScopeOnlyWhenOnlyTheProtectedOrganisationExists":   127.94,
		"TestUpgradeDrillAutoSelectionRefusesLostGrantAuthority":                          30.5,
		"TestUpgradeDrillAutoSelectionSkipsUnusableScopeBeforeCommit":                     159.51,
		"TestUpgradeDrillAutoWithoutCredentialScopeNeedsNoStoredSecret":                   106.06,
		"TestUpgradeDrillAutomaticallyProvesExistingCredentialAuthority":                  92.14,
		"TestUpgradeDrillDiagnosticsLevels":                                               160.08,
		"TestUpgradeDrillHybridBothEngineRecovery":                                        91.35,
		"TestUpgradeDrillHybridRefusesCustodyMismatch":                                    155.8,
		"TestUpgradeDrillManualSelectionStillRequiredByDefault":                           22.59,
		"TestUpgradeDrillRefusesMissingOrWrongPrivateCustody":                             196.76,
		"TestUpgradeExportDiagnosticsPreserveJSON":                                        85.24,
		"TestUpgradeSelectionResolvesOnlyExactEnrolledStartupTuple":                       0.01,
		"TestUpgradeWindowHonorsTheStrictProfile":                                         0.01,
		"TestUpgradeWindowRejectsInvalidTLSBeforeBinding":                                 0,
		"TestUpgradeWindowTLSStatusReadinessAndSocketHandoff":                             0.01,
		"TestVerifiedMigrateSkipsBackupOnHealthyRestart":                                  19.07,
	},
	"internal/service": {
		"TestAWSRoutingRefusalIsCallerCorrectableBeforeProvider":                              0,
		"TestAccessBypassReauthIntent":                                                        0,
		"TestAccessCapabilitiesWithin":                                                        0,
		"TestAccessInteractiveHuman":                                                          0,
		"TestAccessReason":                                                                    0.01,
		"TestAckSetCrossSurfaceReplayRejected":                                                0.72,
		"TestAckSetOpensEachPresentedTokenOnce":                                               0.74,
		"TestAckSetRejectionPrecedenceAndInputOrder":                                          0.73,
		"TestAckSetStaleContentAndVersionRejected":                                            0.71,
		"TestAckSetSurplusReported":                                                           0.75,
		"TestAckTokenExpires":                                                                 0.45,
		"TestAckTokenForeignKeyringRejected":                                                  1.46,
		"TestAckTokenOpaqueNoPlaintext":                                                       0.57,
		"TestAckTokenRoundTrip":                                                               0.72,
		"TestAckTokenTamperRejected":                                                          0.73,
		"TestAdapterAddTargetRefusesObsoleteProviderVerification":                             2.06,
		"TestAdapterAdoptFinalWriteRechecksLiveAuthority":                                     19.64,
		"TestAdapterAdoptRefusesSealedWebhookTargets":                                         0.73,
		"TestAdapterAdoptRequiresRevealAcrossEveryAdapterEnvironment":                         0.81,
		"TestAdapterAdoptSharedEnvironmentConsumesSingleDecisionOnce":                         15.82,
		"TestAdapterAttentionTargetReplacementResumesActivation":                              0.89,
		"TestAdapterCeremonyErrorClassification":                                              2.98,
		"TestAdapterConfigureFinalWriteRechecksAfterProviderResponse":                         20.93,
		"TestAdapterConfigureFinalWriteRechecksTemporaryReveal":                               1.69,
		"TestAdapterCreateAndAddTargetRefuseBeforeCredentialOrProviderWithoutCeremony":        1.51,
		"TestAdapterCreateAtomicallyBootstrapsCredentialAndFirstTarget":                       0.62,
		"TestAdapterCreateCeremonyRechecksAfterWriterAcquisition":                             19.39,
		"TestAdapterCreateNormalizesOriginBeforeProbeAndPersistence":                          0.65,
		"TestAdapterCreateRejectsUnknownProviderBeforeCredentialUse":                          0,
		"TestAdapterCredentialReplaceAndRevokeFenceWithoutAutoConverge":                       0.93,
		"TestAdapterDeleteQueuesEveryScrubAndRetainsCredentialUntilLastTerminal":              0.84,
		"TestAdapterFullTargetUpdateAuditsTransactionAuthorityTransition":                     0.64,
		"TestAdapterManualSyncRequiresTargetRevealAndSupersedesNewest":                        0.85,
		"TestAdapterMoveCredentialFailureRequiresAttentionAndCancelReconvergesOldRoute":       0.9,
		"TestAdapterMoveRecoveryRechecksAfterWriterAcquisition":                               18.89,
		"TestAdapterObservationFinalWriteRechecksAfterWriterAcquisition":                      1.22,
		"TestAdapterOriginMoveCredentialFailureRequiresAttentionAndCancelReconvergesOldRoute": 1.01,
		"TestAdapterOriginMoveKeepsOldRouteAndCredentialThroughScrubBarrier":                  1.07,
		"TestAdapterPendingOriginReplacementAuditsTransactionAuthorityTransition":             0.84,
		"TestAdapterPlanPersistsProviderConflictArtifactAndInspectReturnsIt":                  0.9,
		"TestAdapterProviderFenceRetryRechecksSecurityClock":                                  19.54,
		"TestAdapterProviderGateUsesSecurityClock":                                            13.01,
		"TestAdapterRoutingStateRoundTripsFromStore":                                          0.77,
		"TestAdapterTargetAddAuditsTransactionAuthorityTransition":                            0.87,
		"TestAdapterTargetAddRefusesDestinationEffectiveNameCollisionAtomically":              18.72,
		"TestAdapterTargetAddRequiresRevealAcrossEveryAdapterEnvironmentBeforeCredentialOpen": 0.75,
		"TestAdapterTargetConnectionReauthorizesEveryProviderRequest":                         0.87,
		"TestAdapterTargetDestinationMoveKeepRemoteReleasesBeforeActivation":                  0.81,
		"TestAdapterTargetDestinationMoveScrubsOldRouteBeforePendingRouteActivation":          0.83,
		"TestAdapterTargetInPlaceClassificationAndReplacementConverge":                        2.42,
		"TestAdapterTargetKeepRemoteReleasesAndEnumeratesCustodyWithoutReveal":                0.75,
		"TestAdapterTargetMoveActivationTestsPendingRouteThenEnqueuesConverge":                0.87,
		"TestAdapterTargetMoveDeadCredentialReleasesOldCustodyThenActivates":                  0.57,
		"TestAdapterTargetMoveScrubCompletionQueuesPendingRouteActivation":                    0.84,
		"TestAdapterTargetWidenRequiresRevealAcrossEveryAdapterEnvironment":                   0.81,
		"TestAdoptionBindingRechecksSeedFreshnessWithCurrentDatabaseClock":                    66.26,
		"TestAdvisoryBurstDropsAndReconnectDoesNotReplay":                                     0,
		"TestAdvisoryConnectionCaps":                                                          0,
		"TestApplyTargetMutationClassifiesUpdateAndMove":                                      1.63,
		"TestApplyTargetMutationConcurrentChangesUseLockedGeneration":                         18.72,
		"TestApplyTargetMutationKeepsMovePolicyInsideService":                                 1.45,
		"TestApplyTargetMutationRequiresCeremonyForUpdateAndMove":                             1.57,
		"TestAssessSAMLMetadataReturnsCompleteDiffAndOnlyRequiresNewTrust":                    0.02,
		"TestAttemptedFetchFailsLoudForNilVariants":                                           0,
		"TestAttemptedFetchKeepsFailuresAndRejectsNotAttemptedResults":                        0,
		"TestAuditExportChargesExpensiveBudget":                                               0,
		"TestAuditExportShortWriteIsNotACompleteEvent":                                        0,
		"TestAuthenticatedAPIRequestBucket":                                                   0,
		"TestAuthenticatedAPITrackingBound":                                                   0.02,
		"TestBackupCiphertextVerificationStopsBetweenReads":                                   0,
		"TestBackupHealthVerdicts":                                                            0,
		"TestBackupPruneCancellationHoldsDirectoryUntilUnlinkReturns":                         19.97,
		"TestBackupRetentionCeilingAndRecovery":                                               24.96,
		"TestBackupRetentionFailuresPersist":                                                  35.99,
		"TestBackupRetentionHealthCrossesCeilingWithoutPrune":                                 0,
		"TestBackupRetentionSchedulingDoesNotHideObservedState":                               0,
		"TestBackupRetentionUncertaintyPreservesCandidates":                                   101.61,
		"TestBudgetChargeOnceIsRetryIdempotent":                                               0,
		"TestBudgetConcurrencyReleases":                                                       0,
		"TestBudgetDefaultEnforces":                                                           0,
		"TestBudgetDeliveryTargetBucket":                                                      0.01,
		"TestBudgetDevelopmentEnforcementRetainsRateAndOutstandingSlots":                      0,
		"TestBudgetInstanceConcurrencyIsSeparateFromOrg":                                      0,
		"TestBudgetInstanceExportIgnoresOrgBound":                                             0,
		"TestBudgetMachineFetchAggregates":                                                    0.01,
		"TestBudgetNilIsNoop":                                                                 0,
		"TestBudgetRateRefusalCarriesItsWindowWait":                                           0,
		"TestBudgetRateWindowSlides":                                                          0,
		"TestBudgetRefusalDoesNotConsumeRate":                                                 0,
		"TestBudgetSignupInstanceWide":                                                        0,
		"TestBudgetWeightedRateChargesEachUnitAtomically":                                     0,
		"TestBudgetWithClockRequiresClock":                                                    0,
		"TestBuildCurrentStatePresenceEquivalence":                                            2.98,
		"TestBundleLeafCoverageMatrix":                                                        0,
		"TestCLIReauthPurposeOperationIncludesApprovalDecisions":                              0,
		"TestCLIRedirectIsExactEphemeralLoopbackCallback":                                     0,
		"TestCanonicalOriginRequiresHTTPSOutsideLoopback":                                     0,
		"TestCaptureScanRefusalNil":                                                           0,
		"TestCaptureScanRefusalOneEventPerBlockedFinding":                                     0,
		"TestCaptureScanRefusalRejectionsOnly":                                                0,
		"TestClaimAdmitted":                                                                   0,
		"TestClassifyResetTarget":                                                             0,
		"TestCommittedDeploymentCarriesStrictEnvelopeAcrossRenewalBothEngines":                49.68,
		"TestCommittedDeploymentRenewsUnseenSubmitAndRestoreWithoutNewMFA":                    68.06,
		"TestCommonListIsPinnedTop100KCorpus":                                                 0.02,
		"TestCompleteRestoreClearsEveryRestoredOutboundCredential":                            0.81,
		"TestConfiguredDefinitionsCapsAndResolvedCells":                                       0.02,
		"TestConfiguredProjectKeyGrowth":                                                      0,
		"TestConflictingIdentitiesUsesOnlyAttemptedListings":                                  0,
		"TestConsumeReauthEvidenceRefusesZeroValue":                                           0,
		"TestCopySourceKeyResolverQueriesCatalogueOnce":                                       0.75,
		"TestCoverageWalkReachesDeepLeaves":                                                   0,
		"TestDefinitionsPlanViewKeepsPlanTimeProtectedNames":                                  0,
		"TestDefinitionsRevisionPinsSeparateTopologyFromValueDrift":                           0,
		"TestDeliveryPollWork":                                                                19.85,
		"TestDeploymentRenewalRejectsChangedDecisionAndStaleRow":                              50.42,
		"TestDeveloperCredentialListNamesTheDelegator":                                        0.85,
		"TestDeveloperCredentialLowerCeilingDoesNotReviveOnRaise":                             0.81,
		"TestDeveloperCredentialMintCapAndRevocation":                                         0.93,
		"TestDeveloperCredentialMintRequiresFreshCeremony":                                    0.78,
		"TestDeveloperCredentialReauthRequiresConcretePositiveLifetime":                       0,
		"TestDeveloperMintBindsConcreteLifetimeAcrossPolicyChanges":                           0.83,
		"TestDeveloperMintBusinessRefusalsRemainAuditedAfterRollback":                         3.05,
		"TestDeveloperPolicyClampOnlyCountsLiveCredentials":                                   3.13,
		"TestDeveloperReauthFullCatalogueBinding":                                             0.05,
		"TestDisclosureReauthIntentRefusesMultipleEnvironments":                               0,
		"TestEmailInvitationDispatchOutcomes":                                                 20.68,
		"TestEmailInvitationSendRefusesWithoutSendSet":                                        0,
		"TestEnvironmentCreatePersistsGenerationFenceAndCorrelatedAudit":                      0.84,
		"TestEvaluateAssuranceRejectsEmptyOrMalformedPolicyValues":                            0,
		"TestEvaluateAssuranceRequiresPresentedEvidence":                                      0,
		"TestExportDirectorySyncFailurePreservesPublishedArtifact":                            79.46,
		"TestExportSyncsDirectoryAncestryWithoutOverwriting":                                  21.09,
		"TestFetchGateCoverageCountsOnlyAttemptedResults":                                     0,
		"TestFillPageDensePaging":                                                             1.19,
		"TestFillPageKeepFilter":                                                              0,
		"TestFilterPageScanWindow":                                                            0,
		"TestFindingCapFailsClosed":                                                           0.72,
		"TestGateAttemptsMapIsHardBounded":                                                    0.01,
		"TestGateAttemptsWindow":                                                              0,
		"TestGateSubjectKeysDoNotCollide":                                                     0,
		"TestGitLabAdapterPersistsTransportPolicyScopeAndFlags":                               1.24,
		"TestGitLabDisablingHiddenRequiresCeremony":                                           0.81,
		"TestGitLabOnlyOptionsAreRefusedForOtherProviders":                                    0.59,
		"TestGroupIndexBuildScansEachInputRowOnce":                                            0,
		"TestGroupIndexPhaseReadsCatalogueAndPresenceOnce":                                    0.76,
		"TestGroupIndexRefusesBrokenInputRelationships":                                       0,
		"TestGroupIndexResolvedEnvironmentPermutations":                                       0,
		"TestGroupIndexStaticPresencePermutations":                                            0,
		"TestGroupMembershipIndexDoesNotReadPresence":                                         0.57,
		"TestHealthStorageWarnBoundary":                                                       0,
		"TestHostAdoptionPreservesLargeMailAndTLSSeed":                                        49.25,
		"TestHostAdoptionReadsFreshServerSeedWithoutEvaluatingCommandDefaults":                37.1,
		"TestHostAdoptionRechecksStandaloneDiscoveryBeforeBinding":                            22.15,
		"TestInstanceMailOutcomeSurvivesActorRevocationBothEngines":                           20.13,
		"TestInstanceMailTestConcurrentRefusalBothEngines":                                    19.08,
		"TestInstanceMailTestRequiresProofAndBoundsRateBothEngines":                           18.15,
		"TestInstanceMailTestUsesTLSSinkAndDurableOutcomeBothEngines":                         84.74,
		"TestIssuerEventCABundleDigest":                                                       0,
		"TestMCPCleanupContextSurvivesRequestCancellation":                                    0,
		"TestMachineFetchPrincipalBucket":                                                     0,
		"TestMachineFetchPrincipalTrackingBound":                                              0.02,
		"TestNameAndFolderPathGrammar":                                                        0,
		"TestNameWritersRefuseDirectionControls":                                              0,
		"TestNewSAMLProvidersBuildsProductionMetadataPolicy":                                  0,
		"TestOAuth2RegistrationClaimRefused":                                                  0,
		"TestOpenJobAcrossAReEnrollmentStaysOpenAndClearsTheRuntime":                          39.01,
		"TestOrganizationSelectedRepositoryIDsAreVerifiedBeforeRoutingCommit":                 0.8,
		"TestParameterContractVersionCompatibility":                                           0,
		"TestParameterContractsResolveOnlyCapturedConfig":                                     0,
		"TestParameterValidationPreservesLiteralSchemas":                                      0,
		"TestPasswordPolicy":                                                                  0.03,
		"TestPersonalValuesSchemaVersionMatchesTheMigration":                                  0,
		"TestPlanPruneCeilingBoundaryAndEarlierThreshold":                                     0,
		"TestPlanPruneKeepsNewestAndDeletesOldestFirst":                                       0,
		"TestPlanPruneOrdersByNameNotListing":                                                 0,
		"TestPlanPruneProtectsPersistedNewestAfterClockRollback":                              0,
		"TestPlanPruneProtectsRecordedNewestWhateverItsAge":                                   0,
		"TestPreparationExportUsesLiveSessionWithoutRuntimeAdmission":                         8.82,
		"TestPreparedPublisherPreservesOrdinaryPassphraseBackup":                              28,
		"TestPreparedServiceFixtureStillRequiresAuthenticAdmission":                           1.85,
		"TestPreparedServiceFixtureWritesStayInTheirCopy":                                     1.45,
		"TestPresenceIndexEquivalence":                                                        1.7,
		"TestPresenceIndexModesAndSnapshotLifetime":                                           0,
		"TestProveValuesReadable":                                                             15.61,
		"TestProviderBrand":                                                                   0,
		"TestPruneLoopStopsAfterFirstFailedUnlink":                                            19.58,
		"TestReauthIntentDerivesCanonicalBinding":                                             0.01,
		"TestRecipientSetNeedsCeremony":                                                       0,
		"TestRecordBackupRefusesChangedCiphertext":                                            17.58,
		"TestReduceGroupPatchAppliesMembershipCommandsInOrder":                                0,
		"TestReduceUserPatchAppliesCommandsInOrder":                                           0,
		"TestReducersDoNotMutateCurrentDesiredState":                                          0,
		"TestRefusalWorkBoundsRejectFullQueueAndHonorDeadline":                                0.1,
		"TestRefusePersonalCallerAdmitsOnlyAnOrdinaryHumanSession":                            0,
		"TestRegistrationMailPredicate":                                                       0,
		"TestRenderTotalRefusesAnOversizedTarget":                                             0.01,
		"TestRequestableCapabilitiesReturnsIndependentSlice":                                  0,
		"TestResolveKeySelectionIsASnapshot":                                                  0,
		"TestResolveKeySelectionRefusesLoudly":                                                0,
		"TestResolveKeySelectionUnionsExplicitNamesAndBoundedPatterns":                        0,
		"TestResolveProjectCapacity":                                                          0,
		"TestResolvedCellBudgetComposesByConstruction":                                        0,
		"TestRootRotationAnonymousDoesNotExposeWrapperState":                                  18.46,
		"TestRootRotationAnonymousOutcomeDoesNotExposeConfigurationOrPhase":                   14.82,
		"TestRootRotationBudgetRefusesBeforeLockAndRootSource":                                20.44,
		"TestRootRotationPreflightRefusesBeforeLockOrRootSource":                              19.86,
		"TestRootRotationReauthorizesAfterRootSourceRead":                                     18.58,
		"TestRoutingPreflightAndCommitChargeOnce":                                             0.87,
		"TestRoutingPreflightHoldsOrgSlotBeforeProviderWork":                                  1.15,
		"TestRoutingPreflightRefusalsConsumeAdapterBudget":                                    19.79,
		"TestSAMLAllPurposesCarryCrossSiteInitiatorBinding":                                   0,
		"TestSAMLAuditPayloadSurfacesExpiredPinnedCertificate":                                0,
		"TestSAMLCeremonyAuditDetailsPreserveRefusalContext":                                  0.01,
		"TestSAMLEvaluateAssuranceUsesAcceptedContextSet":                                     0,
		"TestSAMLMetadataGuardedNetworkSeamReturnsInjectedResponse":                           0.01,
		"TestSAMLMetadataOversizedResponseIsRejected":                                         0.01,
		"TestSAMLMetadataRedirectPivotIsRejected":                                             0.01,
		"TestSAMLMetadataRedirectRevalidatesReboundHost":                                      0.01,
		"TestSAMLMetadataRedirectRevalidatesURLShape":                                         0.01,
		"TestSAMLMetadataSlowResponseIsBounded":                                               0.03,
		"TestSAMLMetadataTransportFallsBackAcrossValidatedAddresses":                          0,
		"TestSAMLMetadataTransportPinsPublicIPThroughConfiguredProxy":                         0,
		"TestSAMLMetadataTransportRefusesPrivateResolutionBeforeProxy":                        0,
		"TestSAMLMetadataTransportUsesInjectedDialerForHTTPSProxy":                            0.01,
		"TestSAMLMetadataURLRefusesPrivateNetworkTargets":                                     0,
		"TestSAMLMetadataURLRequiresHTTPS":                                                    0,
		"TestSAMLProviderViewRefusesCorruptStoredTrustState":                                  0.01,
		"TestSAMLProviderWarningsAreServerAuthoritativeAndDeterministic":                      0.07,
		"TestSAMLProvidersExposeNoHTTPClientOverride":                                         0,
		"TestSAMLSessionsInvalidatedOnlyBySecurityRelevantProviderChanges":                    0,
		"TestSAMLStoredSubjectMatchesCurrentNameIDPolicy":                                     0,
		"TestSAMLSubjectEnforcesNameIDFormatPolicy":                                           0,
		"TestSAMLSubjectPreservesByteExactNameIDIdentity":                                     0,
		"TestSCIMAttributePatchMatchesStoredCase":                                             0,
		"TestSCIMAttributePatchRejectsDuplicateExtensionLeaves":                               0,
		"TestSCIMAttributePatchScalesLinearly":                                                0.08,
		"TestSCIMAttributeUniquenessMatchesUnicodeFold":                                       0,
		"TestSCIMPreserveSubjectSourceKeepsOneCaseVariantLeaf":                                0,
		"TestSCIMSubjectAttributesAreCaseInsensitiveAndUnambiguous":                           0,
		"TestSanitizeDefinitionsProvenance":                                                   0,
		"TestScanRejectionsNamedByClass":                                                      0.73,
		"TestSchemaPublishStorageChecksEverySnapshotAndRollsBack":                             20.96,
		"TestSecurityMaintenanceReadAuthorizesLikeUpdateStatusAndAuditsTheRead":               14.79,
		"TestSelfConfigApplyRetryReturnsOriginalJobAfterCollection":                           48.89,
		"TestSelfConfigBootstrapLoadsItsPublishedConfiguration":                               44.83,
		"TestSelfConfigDeploymentCommitsBeforeSendingAndRequiresApplicationAck":               55.27,
		"TestSelfConfigDeploymentConcurrentFinalizeInvalidatesPreparedRoot":                   50.29,
		"TestSelfConfigDeploymentRestoreAllowsRepairThenAnotherRollout":                       52.33,
		"TestSelfConfigDeploymentRestoreRequiresExactMFAAndJournalsBeforeSending":             51.47,
		"TestSelfConfigDeploymentRootPersistenceRequiresExactMFA":                             129.07,
		"TestSelfConfigExistingInstanceAdoptionIsExplicitAndDurable":                          49.84,
		"TestSelfConfigExpiredApplyRetryDoesNotRestartPreparation":                            49.52,
		"TestSelfConfigHAAdoptionRejectsDifferentSeedsAtomically":                             3.72,
		"TestSelfConfigHARefusesStaleTrafficAndConvergesAfterActorRevocation":                 6.74,
		"TestSelfConfigHostRecoveryNeedsQuiescenceAndRejectsNetwork":                          37.81,
		"TestSelfConfigIndependentOwnersKeepSeparateProjectsAndRuntime":                       88.04,
		"TestSelfConfigInstallerActivationFailureFencesAndRetriesCommittedTarget":             34.27,
		"TestSelfConfigInstallerDisposesAbortedCandidateWithoutReplacement":                   27.36,
		"TestSelfConfigInstallerDisposesAbortedSupersededAndShutdownCandidates":               36.14,
		"TestSelfConfigInstallerFailedTargetCanBeRepaired":                                    52.07,
		"TestSelfConfigInstallerPreparationFailureAbortsBeforeTargetCommit":                   44.84,
		"TestSelfConfigInstallerRetainsPreparationAndAcknowledgesOnlyAfterActivation":         28.19,
		"TestSelfConfigIntentCannotWiden":                                                     0,
		"TestSelfConfigInvalidMailPublishRetainsActiveConfiguration":                          43.72,
		"TestSelfConfigInvalidOwnerValuePublishNamesTheKey":                                   45.16,
		"TestSelfConfigInvalidSecretOwnerValuePublishNamesKeyOnly":                            42.02,
		"TestSelfConfigKeysAreShownInReleaseFolders":                                          45.18,
		"TestSelfConfigNextRootSelectorNeedsExactApply":                                       49.83,
		"TestSelfConfigNodeSeedAdoptionImportsEncryptedLocalInputs":                           22.25,
		"TestSelfConfigNodeSeedChangeInvalidatesReviewedAdoption":                             37.42,
		"TestSelfConfigNodeSeedHAKeepsDifferentInputs":                                        4.31,
		"TestSelfConfigNormalKeyRotationPreservesRuntimeSnapshots":                            36.47,
		"TestSelfConfigOriginApplyRefusesRetiringPasskeyHostname":                             27.64,
		"TestSelfConfigOriginRecovery":                                                        163.59,
		"TestSelfConfigOriginReviewCannotCrossDecisionBoundary":                               0,
		"TestSelfConfigOriginReviewLoadsRetainedOriginWithoutActiveGraph":                     27.06,
		"TestSelfConfigOriginReviewUsesStillActiveRPAfterInstallationFailure":                 0,
		"TestSelfConfigPostgresLifecycle":                                                     3.73,
		"TestSelfConfigPublishNeedsExplicitReauthenticatedApply":                              45.35,
		"TestSelfConfigReauthRequiresFreshSupportedFactorAndSingleUse":                        0,
		"TestSelfConfigRepairScopeRequiresMFAInstanceAdminAndExactHierarchy":                  36.31,
		"TestSelfConfigResolveRuntimeBundleDoesNotAcknowledgeOrAcceptNetworkAuthority":        48.75,
		"TestSelfConfigRestoreRequiresConfirmationBoundIntoReauthentication":                  22.48,
		"TestSelfConfigRolloutRestoreIntentIsDistinct":                                        0,
		"TestSelfConfigRootFinalizationWaitsForRestoredRolloutRepair":                         51.97,
		"TestSelfConfigSeedEnvelopeFitsAllBoundedValues":                                      0.72,
		"TestSelfConfigSingletonTopologyFencesOldIdentityAcrossOrdinaryApply":                 42.57,
		"TestSelfConfigSingletonTopologyRestoreRequiresFreshRepair":                           53.92,
		"TestSelfConfigSuspensionKeepsRecoveryInterfaceAvailable":                             44.82,
		"TestSelfConfigTestMailChargesFivePerPrincipalPerHour":                                16.08,
		"TestSelfConfigTestMailRecordsOutcomeAfterActorRevocation":                            49.24,
		"TestSelfConfigTopologySurvivesSourceRestoreAndOrdinaryRepair":                        55.85,
		"TestSelfConfigUpgradeDeploymentRequiresExactMFAAndApplicationAck":                    51.36,
		"TestSessionCompletionCreatesFactorParity":                                            18.12,
		"TestSessionCompletionPublishesOnlyCommittedAttemptTokens":                            34.37,
		"TestSessionCompletionRejectsInvalidVariantFields":                                    0,
		"TestSessionCompletionRotationPreservesProjectionAndReplacesFactors":                  19.83,
		"TestSignupDisplayName":                                                               0,
		"TestSurface2FieldCoverageMatrix":                                                     0,
		"TestTargetedKeyDeleteCascadesMembershipAndQueuesOwnedSlotPrune":                      0.86,
		"TestTransitBudgetRateLimit":                                                          0.03,
		"TestTransitCallerIDGrammar":                                                          0,
		"TestTransitRotationPeriodRejectsOverflow":                                            0,
		"TestTransitSharedRateLimit":                                                          0,
		"TestUnheldGrantReachMatchesMayGrantUnheld":                                           0,
		"TestUpdateOutcomeReconcilesWithoutRequestingSession":                                 19.5,
		"TestUpdateOutcomeRetryAfterAcknowledgementFailureIsIdempotent":                       16.2,
		"TestUpdateRequestRefusesAndAuditsWithoutHelperContact":                               17.25,
		"TestUpdateRequestRequiresRecentHumanAuthenticationBeforeHelperContact":               17.14,
		"TestValidateAccessPolicyInput":                                                       0.01,
		"TestValidateAccountProfile":                                                          0,
		"TestValidateAssurancePolicyCanonicalizesAndBoundsCollections":                        0,
		"TestValidateConfigValueReturnsSchemaLeafInCallerSafeNamedDetail":                     0,
		"TestValidateFinalDefinitionsNamesAndCaps":                                            0,
		"TestValidateProjectRetentionAgainstOrgCap":                                           0,
		"TestValidateProjectRetentionAllowsBoundedOverrideUnderUnlimitedOrg":                  0,
		"TestValidateProviderOriginRejectsMultiHost":                                          0,
		"TestValidateSecretValueReturnsCallerSafeNamedDetail":                                 0,
		"TestVaultKVAdapterCreatePersistsMountIdentityAndMapsPaths":                           0.86,
		"TestVerifiedEmail":                                   0,
		"TestWindowBindingKind":                               0,
		"TestWorkspacePKCESyntax":                             0,
		"TestZeroReauthIntentDoesNotExposeEmptyDerivedValues": 0,
	},
	"internal/store": {
		"TestAWSMovesRefuseCrossKindCaseFoldedPendingClaim":                18.47,
		"TestAWSSharedKindAdoptionPostgres":                                3.4,
		"TestAWSSharedKindAdoptionSQLite":                                  37.41,
		"TestAdapterAdoptionClearsExpiredProviderFence":                    12.01,
		"TestAdapterAdoptionLedgerCollisionIsConflictPostgres":             2.68,
		"TestAdapterAdoptionLedgerCollisionIsConflictSQLite":               18.88,
		"TestAdapterAdoptionProvenancePostgres":                            2.67,
		"TestAdapterAdoptionProvenanceSQLite":                              22.56,
		"TestAdapterAdoptionRefusesStaleArtifactAndLiveProviderFence":      32.47,
		"TestAdapterAttemptsRecordRevisionErrorClassAndAttention":          19.3,
		"TestAdapterClaimDueReplaysExpiredLeaseOnly":                       18.45,
		"TestAdapterClaimDueSingleFlightsTargetAndCapsOrganizationAtFour":  11.7,
		"TestAdapterClaimSettlesCrashWindowAsUnknownOutcome":               11.75,
		"TestAdapterConcurrentAdoptionHasOneWinner":                        11.79,
		"TestAdapterConfigurationWriterAuthorityPostgres":                  1.99,
		"TestAdapterConfigurationWriterAuthoritySQLite":                    18.51,
		"TestAdapterConflictInsertIdempotentAcrossRetries":                 20.56,
		"TestAdapterConflictsHideStaleGeneration":                          19.47,
		"TestAdapterDeadCredentialScrubTerminatesAndEnumeratesOrphans":     11.83,
		"TestAdapterEnqueueClearsExpiredProviderWriteFence":                20.36,
		"TestAdapterEnqueueRefusesAtPerTargetQueueDepth":                   20.7,
		"TestAdapterEnqueueSupersedesAndBumpsGenerationAtomically":         14.34,
		"TestAdapterEnqueueWaitsForProviderWriteFence":                     11.67,
		"TestAdapterFinishJobRequiresExactLeaseAndGeneration":              21.18,
		"TestAdapterFinishPreservesConcurrentReleasedCustody":              11.64,
		"TestAdapterGateRechecksAuthorityAndGeneration":                    11.63,
		"TestAdapterJournalCommitsIntentOutcomeAndLedgerAtomically":        10.49,
		"TestAdapterJournalFinishesUnsentIntentBeforeAuthorityAbort":       10.14,
		"TestAdapterJournalFinishesUnsentIntentBeforeGenerationAbort":      18.97,
		"TestAdapterJournalPUTNotFoundEndsEffectBeforeFreshCreateRetry":    10.77,
		"TestAdapterJournalPersistsOwnedMissingAndAuditFinding":            19.42,
		"TestAdapterJournalRefusesOwnedMissingCompletionAfterRelease":      20.72,
		"TestAdapterJournalRejectsTerminalWriteAfterLeaseLoss":             11.9,
		"TestAdapterJournalRejectsZeroCompletionWithoutDeletingLedger":     18.88,
		"TestAdapterJournalReleasedStateRetainsLedgerRow":                  19.6,
		"TestAdapterMoveAccessorsPreserveConstraintErrorBoundary":          0.01,
		"TestAdapterMovePostgresAccessorsPreserveConstraintErrorBoundary":  0,
		"TestAdapterOriginReusableAfterTombstonePostgres":                  1.65,
		"TestAdapterOriginReusableAfterTombstoneSQLite":                    20.94,
		"TestAdapterPauseBlocksClaimsKeepsLedgerAndResumeCatchesUp":        11.72,
		"TestAdapterPlanArtifactAdoptionIsBoundAndAtomic":                  22.14,
		"TestAdapterProviderSwitchRefusesExistingOutboxAndConfiguration":   21.8,
		"TestAdapterProviderSwitchRequiresHostContext":                     22.4,
		"TestAdapterReserveRebindsReleasedHistoryToActivatedRoute":         18.29,
		"TestAdapterScrubLoadsLastTrustedSourceRevision":                   19.15,
		"TestAdapterStaleWorkerIsFencedAfterReclaim":                       12.35,
		"TestAdapterStampsCompareLexicallyAcrossBridges":                   19.62,
		"TestAdapterSuccessPersistsProviderWarnings":                       19.4,
		"TestAdapterTargetIgnoresUnusedRetryTimestamp":                     0,
		"TestAdapterTargetKeysAndHealthCounts":                             18.9,
		"TestAdapterTargetSurfacesPossibleCaptureWithoutClaimingOwnership": 20.66,
		"TestAuditActorLookupFailuresAndLifetime":                          0,
		"TestAuditFilterMatches":                                           0,
		"TestAuditMatcherSnapshot":                                         0,
		"TestAuditPageSizeIsClampedToTheCap":                               0,
		"TestCanonicalOriginAdoptionPostgres":                              19.8,
		"TestCanonicalOriginAdoptionSQLite":                                209.53,
		"TestCheckedPGInt32KeepsBoundariesAndRejectsWrapping":              0,
		"TestCloudflareTargetsPostgres":                                    2.54,
		"TestCloudflareTargetsSQLite":                                      12.12,
		"TestCompiledAuditGlobEquivalence":                                 0,
		"TestConcurrentAWSOriginAdoptionPostgres":                          2.38,
		"TestConcurrentAWSOriginAdoptionSQLite":                            18.87,
		"TestConcurrentMoveAndAdapterDeletePostgres":                       8.04,
		"TestCoordinationClaimCommitFailurePreservesProvisionalFence":      2.63,
		"TestCoordinationPostgres":                                         3.37,
		"TestCoordinationSQLite":                                           20.22,
		"TestCrashReservationReleaseIsGenerationFencedAndLeavesNoConflict": 38.39,
		"TestEveryDirectRuntimeFamilyRefusesMaintenanceAndOldGeneration":   25.32,
		"TestExportPostgresManifestUsesCopySnapshotDuringMigration":        1.71,
		"TestExportSQLiteManifestUsesArchivedSchemaDuringMigration":        21.05,
		"TestGitLabAdoptionScopePostgres":                                  3.45,
		"TestGitLabAdoptionScopeSQLite":                                    37.78,
		"TestKeyRotationInvariantsPostgres":                                2.56,
		"TestKeyRotationInvariantsSQLite":                                  11.87,
		"TestMapRowsPreservesNilAndDiscardsPartialResults":                 0,
		"TestMatchGlob": 0,
		"TestMoveReplacementUnsentReservationsPostgres":                     21.44,
		"TestMoveReplacementUnsentReservationsSQLite":                       96.61,
		"TestNormalizedOutcomeAndActorName":                                 0,
		"TestOpenPostgresRejectsNegativePoolMaximum":                        0,
		"TestOpenSQLiteCapsConnectionPools":                                 20.49,
		"TestOriginMoveEndpointIdentityPostgres":                            2.71,
		"TestOriginMoveEndpointIdentitySQLite":                              19.96,
		"TestPausedAdapterMovesPostgres":                                    16.63,
		"TestPausedAdapterMovesSQLite":                                      92.15,
		"TestPostgresDynamicClaimRefusesAttemptOverflowBeforeWrite":         0,
		"TestPostgresPoolSessionsDropDeadClients":                           2.14,
		"TestPostgresRestoreSequenceBoundsUseTargetSchemaCatalog":           0.03,
		"TestPostgresSerializedAdmissionHoldsLockUntilSettlement":           5.36,
		"TestPostgresSourceProofBindsAliasAndExpires":                       3.36,
		"TestPostgresSourceProofRefusesRecoveryChange":                      1.93,
		"TestPostgresSourceProofRejectsClonedInstallation":                  3.3,
		"TestPostgresSourceProofRequiresRuntimePrivileges":                  5.94,
		"TestPostgresSourceProofSanitizesFailuresAndTimeout":                2.82,
		"TestPostgresTimestampsAreUTC":                                      3.42,
		"TestPreparedPostgresPoolConcurrentCoordination":                    1.83,
		"TestPreparedPostgresPoolDefaultsAndStaleCandidates":                3.31,
		"TestPreparedPostgresPoolFailuresDoNotPublish":                      2.86,
		"TestPreparedPostgresPoolPreservesTransactionsAndCoordination":      2.74,
		"TestPreparedPostgresPoolRefusesChangedAdmission":                   13.16,
		"TestPreparedPostgresPoolSQLitePolicy":                              19.22,
		"TestProviderMoveClaimsUseSecretSurfaceForConfig":                   24.16,
		"TestPublishedGenerationSupersedesWithoutStealingLiveProviderFence": 20.9,
		"TestReleasedAdoptionRecoveryPostgres":                              25.42,
		"TestReleasedAdoptionRecoverySQLite":                                355.32,
		"TestReservationReleaseCannotDropOwnedOrDispatchedCustody":          18.51,
		"TestRestoreOnlyReplacesUntouchedDiagnosticsSeed":                   0.07,
		"TestRestoreOnlyReplacesUntouchedRefusalSeed":                       0.06,
		"TestRestoreSQLiteCleansOwnedStagingOnFailure":                      0.02,
		"TestRestoreSQLiteConcurrentAttemptsOwnDistinctStaging":             0.01,
		"TestRestoreSQLiteDirectoryDurabilityPreservesCommittedMutations":   22.06,
		"TestRestoreSQLitePublishesPrivateDatabase":                         0,
		"TestRestoreSequencePositionsReserveCapacity":                       0,
		"TestRevisionSnapshotMappersEnforceCollectionTriple":                0,
		"TestRuntimeOpenRequiresActualGateAndSQLiteReadsCannotWrite":        25.92,
		"TestRuntimeSQLiteChildExclusion":                                   0,
		"TestRuntimeTransactionPanicReleasesHostExclusion":                  18.36,
		"TestSQLiteBeginFailuresReleaseOwnership":                           58.79,
		"TestSQLiteCanceledSettlementRetainsAdmission":                      27.71,
		"TestSQLiteCommitRefusesCallerCancellationBeforeSQLPropagation":     13.37,
		"TestSQLiteConcurrentSettlementJoinsNativeCommit":                   18.16,
		"TestSQLiteRuntimeReaderBlocksOtherProcessAndRejectsAliases":        20.35,
		"TestSQLiteRuntimeStampKeepsFixedMicrosecondWidth":                  0,
		"TestSQLiteSettlementAfterAutomaticDiscard":                         0,
		"TestSQLiteSettlementPoolsAndErrors":                                27.77,
		"TestSQLiteSettlementPreservesDriverErrors":                         0,
		"TestSQLiteSnapshotStatementsEndWithTheirTransaction":               52.54,
		"TestSelfConfigPostgres":                                            2.79,
		"TestSelfConfigSQLite":                                              24.79,
		"TestStoredDEKVersionRefusesWrapping":                               0,
		"TestStoredReencryptVersionsRefuseWrapping":                         0,
		"TestTeardownMoveRefusalPostgres":                                   8.06,
		"TestTeardownMoveRefusalSQLite":                                     75.31,
		"TestTransitKeyReadsRefuseCorruptVersionBounds":                     0,
		"TestTransitMaterialReadsRefuseWrapping":                            0,
		"TestVaultCrashHoldAndVersionBoundOperatorRecoveryPostgres":         2.89,
		"TestVaultCrashHoldAndVersionBoundOperatorRecoverySQLite":           18.67,
		"TestVaultCrossSurfaceAdoptionPostgres":                             9.98,
		"TestVaultCrossSurfaceAdoptionSQLite":                               149.14,
		"TestVerifyPGDurability":                                            0,
	},
	"internal/store/upgrade": {
		"TestAbruptProcessExitLeavesAbsentOrCompleteBootstrap":                0.64,
		"TestAutomaticDrillRefusesLiveRecoveryAuthority":                      0,
		"TestBackupFenceDrainsRuntimeAndSurvivesCoordinatorRestart":           4.98,
		"TestBackupFenceOperatorRotationRequiresRecoveryAndDropsOldIntent":    5.92,
		"TestBackupFenceRejectsWrongProofAndRollsBackFailedPublication":       8.84,
		"TestBootstrapAtomicCommitBoundaries":                                 0.27,
		"TestBootstrapCrashChild":                                             0,
		"TestBuildScratchSchemaRefusesExistingObjectsWithoutGooseWrites":      0.18,
		"TestBuildScratchSchemaSerializesEmptyCheckWithDDL":                   5.13,
		"TestCASRefusesEveryStaleIdentityAndIllegalPhase":                     0.22,
		"TestCASUsesDurableTimeRepresentation":                                0.18,
		"TestCandidateConfigurationProjectionBindsOwnerScopeAndSession":       0.31,
		"TestCandidateKeyInventoryIsReadOnlyAndBoundToItsOperation":           0.19,
		"TestCatalogRejectsUnknownObjectsBeyondTables":                        5.36,
		"TestConfigurationMigrationOptionalAbsenceAtVersionFour":              0.01,
		"TestConfigurationMigrationPreview":                                   0.01,
		"TestConfigurationMigrationsUsePerSettingDefaults":                    0.01,
		"TestControlInspectionAllowsPostWriteSchemaAndRefusesPartialControl":  0.39,
		"TestCorruptControlStateRefuses":                                      0.95,
		"TestDomainCatalogAndSourceInspectionShareExactFingerprint":           6.76,
		"TestDowngradeAndGenerationOverflowRefuse":                            0.15,
		"TestFreshBootstrapAndAllPhases":                                      0.28,
		"TestFreshHierarchyIsAtomicAndCannotBeReinitialized":                  9.85,
		"TestFreshHierarchyRefusesPopulationAndLegacy":                        17.6,
		"TestFreshReconciliationOnlyBeforePopulation":                         4.93,
		"TestHealthyRestartRefreshesTrustWithoutBackupOrGeneration":           0.29,
		"TestIntermediateHealthyKeepsWholeRouteMaintenance":                   0.2,
		"TestInvalidAcceptanceCannotPrepare":                                  0.77,
		"TestLegacyGenesisAndMigrationTamper":                                 4.69,
		"TestLegacyProposalNonceBootstrapIsAtomic":                            11.16,
		"TestLockSessionDropsDeadClient":                                      0.1,
		"TestManagedConfigurationMigrationBothDatabases":                      246.87,
		"TestMigrationCommitCrashChild":                                       0,
		"TestMigrationConnectorCannotOpenOrReopenPhysicalConnection":          0,
		"TestMigrationExclusionCancellationAndConcurrentBootstrap":            0.35,
		"TestMigrationProcessCrashAfterIndividualCommits":                     1.68,
		"TestMigrationResumeAcceptsCompleteTargetWithoutReplayingEffects":     0.23,
		"TestMigrationResumeRejectsCorruptHistoryBeforeTargetEffects":         1.51,
		"TestMissingMigrationNamesSetting":                                    0,
		"TestNightlySequenceDoesNotRequireAStableRelease":                     0.27,
		"TestNonceAndTrustFloorsCommitWithPending":                            0.26,
		"TestOperatorTransactionDrainsAfterBackendTermination":                0.18,
		"TestPairingMigrationCatalogueDefinesAbsentDefault":                   0,
		"TestPhaseCommitUncertaintyReconstructsWriteBoundary":                 0.15,
		"TestPostgresCatalogCanonicalRendering":                               0.26,
		"TestPostgresCatalogCleanupFailureInvalidates":                        0.25,
		"TestPostgresCatalogInFlightCancellation":                             0.25,
		"TestPostgresCatalogNativeTransactionsAndErrors":                      0.12,
		"TestPostgresCatalogPooledConnections":                                0.14,
		"TestPostgresCatalogPreservesCallerTransaction":                       0.18,
		"TestPostgresCatalogSettingsRowsCloseErrors":                          0.09,
		"TestPostgresNonTableCatalogDriftRefuses":                             3.82,
		"TestPostgresRollbackAsAnUnprivilegedRole":                            2.4,
		"TestReleaseBackupFenceRestoresOnlyAFrozenSourceAndNothingLater":      6.5,
		"TestRestoreControlSchemaIsExactEmptyAndTransactional":                7.91,
		"TestRestoreEntropyFailureLeavesTransactionUnchanged":                 0.22,
		"TestRestoreMutationFailureDoesNotPublishSQLite":                      20.97,
		"TestRollbackAdmitRequiresTheDecidedBasis":                            0,
		"TestRuntimeAdmissionDrainsTransactionsAndNeverRefreshesOldAuthority": 7.33,
		"TestSQLiteCatalogExcludesOnlyReservedSQLitePrefix":                   3.55,
		"TestSQLiteCatalogIncludesForgedReservedTrigger":                      3.53,
		"TestSQLiteHardLinksAndReplacedIdentityRefuse":                        0.02,
		"TestSameArchiveRestoresNewIncarnationsBeforePublication":             34.51,
		"TestSameReleaseRecoveryRequiresNewProofAndNeverRunsMigrations":       9.24,
		"TestSessionMigrationBackendTerminationCannotReconnect":               0.22,
		"TestSessionMigrationCancellationKeepsPostWriteMarker":                0.26,
		"TestSessionMigrationsRequireDurableBoundaryAndExactBytes":            0.37,
		"TestSnapshotRejectsAppliedHistoryDrift":                              5.33,
		"TestSourceInspectionChecksActualIdentityAndStrongestEpoch":           5.77,
		"TestTrustDomainIsExplicitAndDurable":                                 0.11,
		"TestUnknownSchemaRefusesWithoutControlEffects":                       49.2,
	},
	"internal/upgradegate": {
		"TestActualCLIMigrateBootRestart":                                         240,
		"TestAuthenticatedBackupPreparationPinsFinalTargetAndRefusesMissingProof": 147.05,
		"TestFreshGateInvalidTrustAndRootHaveNoDatastoreEffects":                  23.58,
		"TestFreshGatePersistsSchemaOnlyAndRefusesDifferentResume":                37.2,
		"TestGateCrashChild":                                                      0,
		"TestGatePopulatedProcessCrashRoutes":                                     601.57,
		"TestGateProcessCrashRestart":                                             278.1,
		"TestInstalledOperatorOwnedByChecksTheNamedOwner":                         0.01,
		"TestInstalledOperatorOwnedByReadsAnotherOwnersCustody":                   0,
		"TestOperatorCustodyRejectsSymlinksAndUnsafeMode":                         0.01,
		"TestOperatorHistoricalRestoreRequiresCurrentPinAndEscrow":                24.27,
		"TestOperatorInstallationPinPersistsWithoutBackupEvidence":                31.5,
		"TestOperatorRotationJournalCrashResume":                                  46.81,
		"TestOperatorRotationPriorKeyAndLocalEscrow":                              45.38,
		"TestOperatorUncommittedJournalAcceptsFreshAuthenticatedReplacement":      32.79,
		"TestPackagedNightlyReleaseUpgrade":                                       197.4,
		"TestPostgresSignedGateCanonicalRendering":                                10.55,
		"TestUpgradeConfigurationPreflightPreservesInstalledAuthority":            52.53,
		"TestUpgradeMaterialFingerprintSurvivesNextVerifiedBuildAndRecoveryFloor": 72.54,
	},
}
