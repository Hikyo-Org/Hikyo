package main

import (
	"bytes"
	"encoding/json"
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
	if len(args) == 0 || (args[0] != "race" && args[0] != "fuzz" && args[0] != "isolation" && args[0] != "isolation-race") {
		return errors.New("usage: analysis-shards race|fuzz|isolation|isolation-race --root DIR --shard N --shards N")
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

	decoder := json.NewDecoder(&stdout)
	packages := make([]packageInfo, 0)
	for {
		var pkg packageInfo
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
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
	shard := shards[opts.shard]
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
	for _, path := range paths {
		line := path
		if names := shard.targets[path]; len(names) > 0 {
			sort.Strings(names)
			line += "\t^(" + strings.Join(names, "|") + ")$"
		}
		if _, err := fmt.Fprintln(output, line); err != nil {
			return fmt.Errorf("write race shard: %w", err)
		}
	}
	return nil
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
// twelve shards of successful group run 38279650054 attempt 1 (2026-10-12).
// Parent package events and top-level target events are separate measurements;
// subtest elapsed is never added to either. Unobserved prior package estimates
// remain available; unlisted packages weigh one second plus process startup.
var racePackageSeconds = map[string]float64{
	"api":                                 21.659,
	"cmd/bench-scan":                      1.007,
	"cmd/hikyo":                           1.196,
	"cmd/hikyo-receiver-example":          1.024,
	"internal/adapter":                    1.043,
	"internal/adapter/awssm":              1.293,
	"internal/adapter/cloudflare":         1.024,
	"internal/adapter/forgejo":            1.333,
	"internal/adapter/githubactions":      1.12,
	"internal/adapter/gitlab":             1.162,
	"internal/adapter/sealedwebhook":      1.782,
	"internal/adapter/vaultkv":            1.222,
	"internal/admission":                  1.38,
	"internal/app":                        2922.37,
	"internal/audit":                      1.03,
	"internal/authz":                      1.168,
	"internal/authz/gen":                  5.73,
	"internal/backupreceipt":              1.357,
	"internal/bench":                      1.042,
	"internal/boundary":                   6.023,
	"internal/buildcompat":                1.123,
	"internal/cli":                        10.122,
	"internal/compose":                    1.461,
	"internal/config":                     4.448,
	"internal/configrollout":              3.359,
	"internal/conformance":                227.14,
	"internal/console":                    1.008,
	"internal/crypto":                     2.863,
	"internal/crypto/backup":              22.915,
	"internal/crypto/sealedhook":          1.118,
	"internal/definitions":                1.241,
	"internal/delivery":                   1.008,
	"internal/deliverytarget":             4.918,
	"internal/devupgrade":                 4.15,
	"internal/diagnostics":                1.01,
	"internal/disclose":                   1.02,
	"internal/domain":                     1.226,
	"internal/dotenv":                     1.044,
	"internal/dynamic":                    1.012,
	"internal/dynamic/postgres":           1.012,
	"internal/federationhttp":             1.257,
	"internal/filesync":                   1.614,
	"internal/hostupgrade":                1.023,
	"internal/importer":                   19.169,
	"internal/jwkssource":                 1.022,
	"internal/localsocket":                1.016,
	"internal/mail":                       1.46,
	"internal/mcpserver":                  5.308,
	"internal/netpolicy":                  1.008,
	"internal/oauth2rp":                   1.044,
	"internal/oidcfed":                    1.375,
	"internal/oidcrp":                     10.415,
	"internal/oidctest":                   1.085,
	"internal/operation":                  1.008,
	"internal/operator":                   12.163,
	"internal/operator/api/v1alpha1":      29.104,
	"internal/operator/client":            1.669,
	"internal/parameters":                 1.036,
	"internal/pathutil":                   1.012,
	"internal/pki":                        1.369,
	"internal/releaseidentity":            1.017,
	"internal/releasetrust":               5.495,
	"internal/remotefetch":                1.188,
	"internal/repscan":                    1.464,
	"internal/runtimeconfig":              1.067,
	"internal/samlsp":                     1.809,
	"internal/scanning":                   1.078,
	"internal/scanning/bench":             1.013,
	"internal/scanning/gen":               1.011,
	"internal/schema":                     1.338,
	"internal/scimproto":                  1.033,
	"internal/securefile":                 1.013,
	"internal/selfupdate":                 10.38,
	"internal/server":                     69.557,
	"internal/service":                    1608.74,
	"internal/sshca":                      5.287,
	"internal/storagehealth":              1.194,
	"internal/store":                      2179.531,
	"internal/store/authn":                1.014,
	"internal/store/migrate":              155.896,
	"internal/store/tx":                   24.059,
	"internal/store/upgrade":              509.727,
	"internal/testutil/fixtureref":        1.226,
	"internal/tlspolicy":                  1.066,
	"internal/transit":                    1.025,
	"internal/updatecheck":                1.05,
	"internal/updater":                    1.043,
	"internal/upgradeassembly":            1.258,
	"internal/upgradebundle":              1.421,
	"internal/upgradecompat":              3.628,
	"internal/upgradecustody":             205.309,
	"internal/upgradegate":                1157.177,
	"internal/webauthntest":               1.015,
	"scripts/ci":                          20.82,
	"scripts/ci/analysis-shards-go":       2.206,
	"scripts/ci/benchmark":                1.56,
	"scripts/ci/chartfixture":             1.094,
	"scripts/ci/feedback-reuse-size":      1.013,
	"scripts/ci/pr-gate":                  5.211,
	"scripts/ci/prepare-lint-cache":       1.109,
	"scripts/ci/unattendedfixture":        1.054,
	"scripts/ci/workflow-lint":            1.014,
	"scripts/mcp-production-client":       1.109,
	"scripts/mcp-public-smoke":            1.197,
	"scripts/operator-floor":              3.405,
	"scripts/ops-floor":                   1.035,
	"scripts/release":                     1.013,
	"scripts/release/assemble-upgrade":    118.472,
	"scripts/release/embed-compatibility": 2.933,
	"scripts/release/nightly":             1.336,
	"scripts/release/stable":              4.842,
}

// raceTargetSeconds holds hosted top-level target proportions from the same
// twelve-shard JSON artifacts. Observed paused targets retain the existing
// four-wide parallel allowance; subtest events are excluded to avoid counting
// parent and child elapsed twice. Unobserved source targets keep prior weights.
// The unchanged planner discovers every target, including fuzz seeds and
// runnable examples, and gives missing weights its one-second fallback.
// Each split suite is scaled to racePackageSeconds. Keep internal/lint whole
// so its repository load is not repeated across processes. Updating these
// estimates changes placement only, never the target union or DB scheduling.
var raceTargetSeconds = map[string]map[string]float64{
	"internal/app": {
		"TestActualDefaultDevCLIStartsAndRestarts":                                        180.78,
		"TestAdminAuthRefusesUnverifiedLegacySchema":                                      18.74,
		"TestAdminAuthResourceOwnership":                                                  37.85,
		"TestAdminAuthWarnsWhenRootRotationPending":                                       11.03,
		"TestAdminCreateAdoptsRunningServerSeedInsteadOfCommandDefaults":                  24.11,
		"TestAdminCreateRequiresFreshServerSeed":                                          24.17,
		"TestAutomaticPrewriteRetryMeasuresRealSQLiteAndRestoreProof":                     18.12,
		"TestAutomaticUpgradePackagedNightlyRoute":                                        123.85,
		"TestBackupExportJobRecordsFailureLoudlyWithoutPlaintext":                         19.39,
		"TestBackupExportJobRunsOnceThenGatesOnTheInterval":                               20.34,
		"TestBackupPruneJobRemovesOnlyAgedArchives":                                       19.05,
		"TestBackupRestoreDiagnosticsLevels":                                              43.43,
		"TestBootLogsEffectiveSQLitePoolSizes":                                            18.94,
		"TestBootResourceOwnershipOnFailure":                                              45.88,
		"TestBootSuccessTransfersResourceOwnershipToServer":                               22.59,
		"TestBootWarmsOpenAPIBeforeListening":                                             10.56,
		"TestBootWithRootKeyFileAndWrongKeyRefused":                                       19.55,
		"TestBootstrapDeploymentBootSeedsSelectorsAndKeepsPendingRepairFenced":            20.77,
		"TestBootstrapDeploymentDatabaseProofPrecedesSignedSubmitAndTransport":            3.27,
		"TestBootstrapDeploymentEnrollmentPinsStandaloneNode":                             19.23,
		"TestBootstrapDeploymentRejectsReplacedAliasAndExpiredPrivateProof":               3.28,
		"TestBootstrapDeploymentRenewalPreservesCommittedAuthorityAfterRestart":           11.51,
		"TestBootstrapDeploymentRootPrepareIsSealedButNotPersisted":                       19.67,
		"TestBootstrapInitialHASourceCarriesUnchangedCorrespondenceAndReproof":            4.08,
		"TestBootstrapSingletonTopologyRequiresActualPrerequisites":                       13.14,
		"TestCandidateConfigurationFailureKeepsRestoreRequired":                           77.67,
		"TestCandidateHealthChecksEnrolledNextRootWithoutProvider":                        12.74,
		"TestDevBootGeneratesAndReusesRootKey":                                            20.21,
		"TestDevBootSeparatesPublicAndOperationalRoutes":                                  22.65,
		"TestDevelopmentControlsCannotChangeDeploymentTrustContext":                       19.27,
		"TestDevelopmentNodeControlsChangeActualRuntimeAfterActivation":                   19.25,
		"TestDevelopmentProviderPreservesRemoteStateAcrossOrdinaryReloadAndRefusesSwitch": 19.29,
		"TestDevelopmentProviderSwitchRechecksConfigurationAfterPreparation":              22.09,
		"TestEnrolledSourceLoaderAcceptsOnlyExplicitSingletonHAIdentity":                  10.57,
		"TestEscrowLocalCLIUsesAdmittedCurrentHierarchy":                                  29.41,
		"TestExplicitMigrateThenBootWithoutAutoMigrate":                                   19.98,
		"TestLocalCLISocketServesPublicAPIAndIsRemovedOnShutdown":                         19.24,
		"TestMigrateDiagnosticsLevelsAndNoOp":                                             77.49,
		"TestMigrateNeedsNoRootKey":                                                       10.59,
		"TestMigrationRefusesUnverifiedExistingSchemaBeforeWrites":                        37.74,
		"TestNativeTLSBootServesHTTPSAndKeepsOpsPlaintext":                                11.32,
		"TestNextRootLiveSelectionKeepsRotationSeparate":                                  24.24,
		"TestNextRootPreparationRejectsUnavailableSources":                                95.77,
		"TestNextRootSeedRequiresExactEnrolledProjection":                                 19.51,
		"TestNodeRuntimeCancelledActivationReleasesNewListener":                           19.2,
		"TestNodeRuntimeImportsTLSFilesOnce":                                              13.21,
		"TestNodeRuntimeInstallsIndependentCapacityAndPolicy":                             19.4,
		"TestNodeRuntimeListenerFailureKeepsOldGraphAndReleasesReservation":               19.28,
		"TestNodeRuntimeListenerMoveDrainsOldHTTPResponse":                                19.11,
		"TestNodeRuntimeManagedRestartDoesNotReadDeletedTLSFiles":                         20.66,
		"TestNodeRuntimeMissingHANodeBootsOnlyAdministrativeRecovery":                     4.94,
		"TestNodeRuntimePostgresPoolAppliesWithoutReplacingIdentity":                      3.12,
		"TestNodeRuntimeReservesMovesAndSwapsListeners":                                   19.66,
		"TestNodeRuntimeScheduledBackupUsesInstalledDestination":                          11.5,
		"TestNodeRuntimeTLSRotationAndPlaintextTransition":                                19.89,
		"TestOperationalListenerFailureStopsWholeLifecycle":                               10.63,
		"TestOrdinaryBackupPublicAdmissionAndDataOnlyRestore":                             31.64,
		"TestOwnerRuntimeActivationFailureResumesAdministrativeGraph":                     19.75,
		"TestOwnerRuntimeDrainsRequestsBeforeInstalling":                                  19.46,
		"TestOwnerRuntimeFailedTargetFencesBusinessAndPreservesHTTPRepair":                20,
		"TestOwnerRuntimeInstallsHTTPAuthAndRetentionGraph":                               10.7,
		"TestOwnerRuntimePreparationFailureAndMailOnlyKeepActiveGraph":                    21.89,
		"TestOwnerRuntimeRefusesPasswordCostChangeAfterCredentialEstablishment":           20.48,
		"TestPostgresRestoreDestinationRefusesExpiredOwnerAndOccupiedTarget":              20.81,
		"TestProductionBackupCustodyRefusesBeforeDatabaseAdmission":                       19.21,
		"TestProxyHTTPSOriginEmitsHSTSOnLoopbackBackend":                                  19.39,
		"TestRecoveryCapabilityExpiresWithOwnerAndArchive":                                47.62,
		"TestRecoveryPostgresRollsBackWhenMigrationOwnerDies":                             4.82,
		"TestRecoveryTransactionsUseSchema65GrantProjection":                              30.34,
		"TestReleaseCompatibilityMatchesActualBothEngineSchemas":                          38.5,
		"TestRestorePostgresRollsBackWhenMigrationOwnerDies":                              5.7,
		"TestRestoreRequiredRestartExposesOnlyOperationalHealth":                          19.8,
		"TestRestoreSchemaRechecksPreflightBeforeAnyGooseWrite":                           5.16,
		"TestSchedulerHAThreeNodesOnePostgres":                                            2.93,
		"TestSchemaAheadOfBinaryRefusesToServe":                                           11.34,
		"TestServeCancellationStopsBothListeners":                                         22.46,
		"TestServeWithReadySignalsOnlyAfterHTTPServingStarts":                             22.94,
		"TestServerDiagnosticsDoNotClaimSuccessAfterListenerFailure":                      11.2,
		"TestServerDiagnosticsLevelsAndReadiness":                                         43.19,
		"TestSingletonHARestoreBootRetainsCoordinationThroughSourceRepair":                14.55,
		"TestSingletonReplacementBootUsesActualHAMode":                                    3.44,
		"TestUnattendedCredentialSealedRepeatedChildReads":                                1.1,
		"TestUnattendedPackagedEnrollmentUpgradeAndIntermediateCredential":                254.06,
		"TestUnattendedScratchPreservesUnownedDataAndReusesOwnedSchema":                   3.06,
		"TestUpgradeDrillActualBothEngineRecoveryAndConfigOnlyEscrow":                     76.45,
		"TestUpgradeDrillAppliedLedgerKeepsSourceAuthorityAndRotatesScratch":              48.86,
		"TestUpgradeDrillAutoSelectionRefusesLostGrantAuthority":                          31.57,
		"TestUpgradeDrillAutoSelectionSkipsUnusableScopeBeforeCommit":                     90.36,
		"TestUpgradeDrillAutomaticallyProvesExistingCredentialAuthority":                  44.48,
		"TestUpgradeDrillDiagnosticsLevels":                                               126.31,
		"TestUpgradeDrillManualSelectionStillRequiredByDefault":                           31.25,
		"TestUpgradeDrillRefusesMissingOrWrongPrivateCustody":                             154.51,
		"TestUpgradeExportDiagnosticsPreserveJSON":                                        34.77,
		"TestVerifiedMigrateSkipsBackupOnHealthyRestart":                                  19.51,
	},
	"internal/service": {
		"TestAdapterAddTargetRefusesObsoleteProviderVerification":                             2.28,
		"TestAdapterAdoptFinalWriteRechecksLiveAuthority":                                     12.14,
		"TestAdapterAdoptRefusesSealedWebhookTargets":                                         19.09,
		"TestAdapterAdoptSharedEnvironmentConsumesSingleDecisionOnce":                         19.45,
		"TestAdapterCeremonyErrorClassification":                                              2,
		"TestAdapterConfigureFinalWriteRechecksAfterProviderResponse":                         12.93,
		"TestAdapterConfigureFinalWriteRechecksTemporaryReveal":                               19.88,
		"TestAdapterCreateAndAddTargetRefuseBeforeCredentialOrProviderWithoutCeremony":        1.64,
		"TestAdapterCreateCeremonyRechecksAfterWriterAcquisition":                             11.25,
		"TestAdapterCreateNormalizesOriginBeforeProbeAndPersistence":                          18.94,
		"TestAdapterCredentialReplaceAndRevokeFenceWithoutAutoConverge":                       1.02,
		"TestAdapterMoveRecoveryRechecksAfterWriterAcquisition":                               23.63,
		"TestAdapterObservationFinalWriteRechecksAfterWriterAcquisition":                      19.97,
		"TestAdapterOriginMoveCredentialFailureRequiresAttentionAndCancelReconvergesOldRoute": 1.1,
		"TestAdapterOriginMoveKeepsOldRouteAndCredentialThroughScrubBarrier":                  1.12,
		"TestAdapterProviderFenceRetryRechecksSecurityClock":                                  19.58,
		"TestAdapterRoutingStateRoundTripsFromStore":                                          14.74,
		"TestAdapterTargetInPlaceClassificationAndReplacementConverge":                        1.6,
		"TestAdoptionBindingRechecksSeedFreshnessWithCurrentDatabaseClock":                    24.593,
		"TestApplyTargetMutationClassifiesUpdateAndMove":                                      1.5,
		"TestApplyTargetMutationKeepsMovePolicyInsideService":                                 1.6,
		"TestApplyTargetMutationRequiresCeremonyForUpdateAndMove":                             1.71,
		"TestCommittedDeploymentRenewsUnseenSubmitAndRestoreWithoutNewMFA":                    8.928,
		"TestCompleteRestoreClearsRestoredAdapterCredential":                                  5,
		"TestDeploymentRenewalRejectsChangedDecisionAndStaleRow":                              11.982,
		"TestDeveloperCredentialMintCapAndRevocation":                                         1.02,
		"TestDeveloperMintBusinessRefusalsRemainAuditedAfterRollback":                         3.5,
		"TestDeveloperPolicyClampOnlyCountsLiveCredentials":                                   2.12,
		"TestExportDirectorySyncFailurePreservesPublishedArtifact":                            37,
		"TestExportSyncsDirectoryAncestryWithoutOverwriting":                                  21.3,
		"TestGitLabAdapterPersistsTransportPolicyScopeAndFlags":                               1.27,
		"TestHostAdoptionPreservesLargeMailAndTLSSeed":                                        9.515,
		"TestHostAdoptionReadsFreshServerSeedWithoutEvaluatingCommandDefaults":                11.42,
		"TestHostAdoptionRechecksStandaloneDiscoveryBeforeBinding":                            12.81,
		"TestInstanceMailOutcomeSurvivesActorRevocationBothEngines":                           13.27,
		"TestInstanceMailTestConcurrentRefusalBothEngines":                                    22.13,
		"TestInstanceMailTestRequiresProofAndBoundsRateBothEngines":                           12.47,
		"TestInstanceMailTestUsesTLSSinkAndDurableOutcomeBothEngines":                         88.14,
		"TestPreparationExportUsesLiveSessionWithoutRuntimeAdmission":                         8.17,
		"TestPreparedPublisherPreservesOrdinaryPassphraseBackup":                              25.85,
		"TestPreparedServiceFixtureStillRequiresAuthenticAdmission":                           1.27,
		"TestPreparedServiceFixtureWritesStayInTheirCopy":                                     1.57,
		"TestProveValuesReadable":                                                             18.42,
		"TestPruneLoopStopsAfterFirstFailedUnlink":                                            18.38,
		"TestRootRotationAnonymousDoesNotExposeWrapperState":                                  18.77,
		"TestRootRotationAnonymousOutcomeDoesNotExposeConfigurationOrPhase":                   10.45,
		"TestRootRotationBudgetRefusesBeforeLockAndRootSource":                                18.8,
		"TestRootRotationPreflightRefusesBeforeLockOrRootSource":                              18.16,
		"TestRootRotationReauthorizesAfterRootSourceRead":                                     18.67,
		"TestRoutingPreflightHoldsOrgSlotBeforeProviderWork":                                  1.21,
		"TestRoutingPreflightRefusalsConsumeAdapterBudget":                                    1.02,
		"TestSchemaPublishStorageChecksEverySnapshotAndRollsBack":                             21.99,
		"TestSelfConfigApplyRetryReturnsOriginalJobAfterCollection":                           9.777,
		"TestSelfConfigBootstrapLoadsItsPublishedConfiguration":                               8.592,
		"TestSelfConfigDeploymentCommitsBeforeSendingAndRequiresApplicationAck":               7.793,
		"TestSelfConfigDeploymentConcurrentFinalizeInvalidatesPreparedRoot":                   13.17,
		"TestSelfConfigDeploymentRestoreAllowsRepairThenAnotherRollout":                       7.775,
		"TestSelfConfigDeploymentRestoreRequiresExactMFAAndJournalsBeforeSending":             13.715,
		"TestSelfConfigDeploymentRootPersistenceRequiresExactMFA":                             23.668,
		"TestSelfConfigExistingInstanceAdoptionIsExplicitAndDurable":                          12.595,
		"TestSelfConfigExpiredApplyRetryDoesNotRestartPreparation":                            7.277,
		"TestSelfConfigHAAdoptionRejectsDifferentSeedsAtomically":                             1.353,
		"TestSelfConfigHARefusesStaleTrafficAndConvergesAfterActorRevocation":                 1.42,
		"TestSelfConfigHostRecoveryNeedsQuiescenceAndRejectsNetwork":                          13.23,
		"TestSelfConfigIndependentOwnersKeepSeparateProjectsAndRuntime":                       22.663,
		"TestSelfConfigInstallerActivationFailureFencesAndRetriesCommittedTarget":             12.113,
		"TestSelfConfigInstallerDisposesAbortedCandidateWithoutReplacement":                   12.062,
		"TestSelfConfigInstallerDisposesAbortedSupersededAndShutdownCandidates":               7.555,
		"TestSelfConfigInstallerFailedTargetCanBeRepaired":                                    7.635,
		"TestSelfConfigInstallerPreparationFailureAbortsBeforeTargetCommit":                   5.893,
		"TestSelfConfigInstallerRetainsPreparationAndAcknowledgesOnlyAfterActivation":         5.605,
		"TestSelfConfigInvalidMailPublishRetainsActiveConfiguration":                          7.907,
		"TestSelfConfigInvalidOwnerValuePublishNamesTheKey":                                   11.412,
		"TestSelfConfigInvalidSecretOwnerValuePublishNamesKeyOnly":                            10.383,
		"TestSelfConfigNextRootSelectorNeedsExactApply":                                       9.77,
		"TestSelfConfigNodeSeedAdoptionImportsEncryptedLocalInputs":                           11.682,
		"TestSelfConfigNodeSeedChangeInvalidatesReviewedAdoption":                             12.648,
		"TestSelfConfigNodeSeedHAKeepsDifferentInputs":                                        1.262,
		"TestSelfConfigNormalKeyRotationPreservesRuntimeSnapshots":                            12.335,
		"TestSelfConfigOriginApplyRefusesRetiringPasskeyHostname":                             7.475,
		"TestSelfConfigOriginRecovery":                                                        68.688,
		"TestSelfConfigOriginReviewLoadsRetainedOriginWithoutActiveGraph":                     5.44,
		"TestSelfConfigPublishNeedsExplicitReauthenticatedApply":                              5.022,
		"TestSelfConfigRepairScopeRequiresMFAInstanceAdminAndExactHierarchy":                  7.383,
		"TestSelfConfigResolveRuntimeBundleDoesNotAcknowledgeOrAcceptNetworkAuthority":        13.05,
		"TestSelfConfigRestoreRequiresConfirmationBoundIntoReauthentication":                  2.817,
		"TestSelfConfigRootFinalizationWaitsForRestoredRolloutRepair":                         13.79,
		"TestSelfConfigSingletonTopologyFencesOldIdentityAcrossOrdinaryApply":                 13.998,
		"TestSelfConfigSingletonTopologyRestoreRequiresFreshRepair":                           12.582,
		"TestSelfConfigSuspensionKeepsRecoveryInterfaceAvailable":                             4.992,
		"TestSelfConfigTestMailChargesFivePerPrincipalPerHour":                                8.213,
		"TestSelfConfigTestMailRecordsOutcomeAfterActorRevocation":                            6.388,
		"TestSelfConfigTopologySurvivesSourceRestoreAndOrdinaryRepair":                        21.715,
		"TestSelfConfigUpgradeDeploymentRequiresExactMFAAndApplicationAck":                    8.595,
		"TestSessionCompletionCreatesFactorParity":                                            18.78,
		"TestSessionCompletionPublishesOnlyCommittedAttemptTokens":                            21.09,
		"TestSessionCompletionRotationPreservesProjectionAndReplacesFactors":                  10.22,
		"TestUpdateOutcomeReconcilesWithoutRequestingSession":                                 16.29,
		"TestUpdateOutcomeRetryAfterAcknowledgementFailureIsIdempotent":                       18.35,
		"TestUpdateRequestRefusesAndAuditsWithoutHelperContact":                               18.34,
		"TestUpdateRequestRequiresRecentHumanAuthenticationBeforeHelperContact":               10.74,
	},
	"internal/store": {
		"TestAWSMovesRefuseCrossKindCaseFoldedPendingClaim":                 19.66,
		"TestAWSSharedKindAdoptionPostgres":                                 6.19,
		"TestAWSSharedKindAdoptionSQLite":                                   39.23,
		"TestAdapterAdoptionClearsExpiredProviderFence":                     22.36,
		"TestAdapterAdoptionLedgerCollisionIsConflictPostgres":              3.42,
		"TestAdapterAdoptionLedgerCollisionIsConflictSQLite":                19.85,
		"TestAdapterAdoptionProvenancePostgres":                             2.52,
		"TestAdapterAdoptionProvenanceSQLite":                               22.09,
		"TestAdapterAdoptionRefusesStaleArtifactAndLiveProviderFence":       58.2,
		"TestAdapterAttemptsRecordRevisionErrorClassAndAttention":           19.79,
		"TestAdapterClaimDueReplaysExpiredLeaseOnly":                        11.37,
		"TestAdapterClaimDueSingleFlightsTargetAndCapsOrganizationAtFour":   18.48,
		"TestAdapterClaimSettlesCrashWindowAsUnknownOutcome":                19.37,
		"TestAdapterConcurrentAdoptionHasOneWinner":                         18.25,
		"TestAdapterConfigurationWriterAuthorityPostgres":                   4.52,
		"TestAdapterConfigurationWriterAuthoritySQLite":                     16.33,
		"TestAdapterConflictInsertIdempotentAcrossRetries":                  20.11,
		"TestAdapterConflictsHideStaleGeneration":                           20.23,
		"TestAdapterDeadCredentialScrubTerminatesAndEnumeratesOrphans":      10.84,
		"TestAdapterEnqueueClearsExpiredProviderWriteFence":                 21.72,
		"TestAdapterEnqueueRefusesAtPerTargetQueueDepth":                    20.64,
		"TestAdapterEnqueueSupersedesAndBumpsGenerationAtomically":          19.24,
		"TestAdapterEnqueueWaitsForProviderWriteFence":                      19.8,
		"TestAdapterFinishJobRequiresExactLeaseAndGeneration":               19.43,
		"TestAdapterFinishPreservesConcurrentReleasedCustody":               10.34,
		"TestAdapterGateRechecksAuthorityAndGeneration":                     22.95,
		"TestAdapterJournalCommitsIntentOutcomeAndLedgerAtomically":         12.22,
		"TestAdapterJournalFinishesUnsentIntentBeforeAuthorityAbort":        18.87,
		"TestAdapterJournalFinishesUnsentIntentBeforeGenerationAbort":       24.83,
		"TestAdapterJournalPUTNotFoundEndsEffectBeforeFreshCreateRetry":     19.62,
		"TestAdapterJournalPersistsOwnedMissingAndAuditFinding":             19.53,
		"TestAdapterJournalRefusesOwnedMissingCompletionAfterRelease":       10.33,
		"TestAdapterJournalRejectsTerminalWriteAfterLeaseLoss":              19.36,
		"TestAdapterJournalRejectsZeroCompletionWithoutDeletingLedger":      10.95,
		"TestAdapterJournalReleasedStateRetainsLedgerRow":                   12.15,
		"TestAdapterOriginReusableAfterTombstonePostgres":                   3.18,
		"TestAdapterOriginReusableAfterTombstoneSQLite":                     20.01,
		"TestAdapterPauseBlocksClaimsKeepsLedgerAndResumeCatchesUp":         19.81,
		"TestAdapterPlanArtifactAdoptionIsBoundAndAtomic":                   16.93,
		"TestAdapterProviderSwitchRefusesExistingOutboxAndConfiguration":    18.4,
		"TestAdapterProviderSwitchRequiresHostContext":                      18.12,
		"TestAdapterReserveRebindsReleasedHistoryToActivatedRoute":          19.26,
		"TestAdapterScrubLoadsLastTrustedSourceRevision":                    19.3,
		"TestAdapterStaleWorkerIsFencedAfterReclaim":                        15.39,
		"TestAdapterStampsCompareLexicallyAcrossBridges":                    18.29,
		"TestAdapterSuccessPersistsProviderWarnings":                        21.26,
		"TestAdapterTargetKeysAndHealthCounts":                              20.18,
		"TestAdapterTargetSurfacesPossibleCaptureWithoutClaimingOwnership":  11.89,
		"TestCanonicalOriginAdoptionPostgres":                               63.22,
		"TestCanonicalOriginAdoptionSQLite":                                 217.77,
		"TestCloudflareTargetsPostgres":                                     3.05,
		"TestCloudflareTargetsSQLite":                                       19.6,
		"TestConcurrentAWSOriginAdoptionPostgres":                           1.63,
		"TestConcurrentAWSOriginAdoptionSQLite":                             21.05,
		"TestConcurrentMoveAndAdapterDeletePostgres":                        22.96,
		"TestCoordinationClaimCommitFailurePreservesProvisionalFence":       3.14,
		"TestCoordinationPostgres":                                          3.81,
		"TestCoordinationSQLite":                                            21.16,
		"TestCrashReservationReleaseIsGenerationFencedAndLeavesNoConflict":  36.42,
		"TestEveryDirectRuntimeFamilyRefusesMaintenanceAndOldGeneration":    21.71,
		"TestExportPostgresManifestUsesCopySnapshotDuringMigration":         4.13,
		"TestExportSQLiteManifestUsesArchivedSchemaDuringMigration":         21.27,
		"TestGitLabAdoptionScopePostgres":                                   6.09,
		"TestGitLabAdoptionScopeSQLite":                                     33.66,
		"TestKeyRotationInvariantsPostgres":                                 3.45,
		"TestKeyRotationInvariantsSQLite":                                   22.56,
		"TestOpenSQLiteCapsConnectionPools":                                 20.46,
		"TestOriginMoveEndpointIdentityPostgres":                            3.31,
		"TestOriginMoveEndpointIdentitySQLite":                              10.78,
		"TestPausedAdapterMovesPostgres":                                    28.2,
		"TestPausedAdapterMovesSQLite":                                      95.56,
		"TestPostgresSerializedAdmissionHoldsLockUntilSettlement":           8.94,
		"TestPostgresSourceProofBindsAliasAndExpires":                       3.2,
		"TestPostgresSourceProofRefusesRecoveryChange":                      3.78,
		"TestPostgresSourceProofRejectsClonedInstallation":                  3.86,
		"TestPostgresSourceProofRequiresRuntimePrivileges":                  4.53,
		"TestPostgresSourceProofSanitizesFailuresAndTimeout":                2.37,
		"TestPostgresTimestampsAreUTC":                                      4.1,
		"TestPreparedPostgresPoolConcurrentCoordination":                    3.38,
		"TestPreparedPostgresPoolDefaultsAndStaleCandidates":                6.88,
		"TestPreparedPostgresPoolFailuresDoNotPublish":                      4.3,
		"TestPreparedPostgresPoolPreservesTransactionsAndCoordination":      3.38,
		"TestPreparedPostgresPoolRefusesChangedAdmission":                   13.79,
		"TestPreparedPostgresPoolSQLitePolicy":                              19.15,
		"TestProviderMoveClaimsUseSecretSurfaceForConfig":                   22.48,
		"TestPublishedGenerationSupersedesWithoutStealingLiveProviderFence": 19.24,
		"TestReleasedAdoptionRecoveryPostgres":                              36.66,
		"TestReleasedAdoptionRecoverySQLite":                                148.67,
		"TestReservationReleaseCannotDropOwnedOrDispatchedCustody":          19.3,
		"TestRestoreSQLiteDirectoryDurabilityPreservesCommittedMutations":   23.91,
		"TestRuntimeOpenRequiresActualGateAndSQLiteReadsCannotWrite":        27.38,
		"TestRuntimeTransactionPanicReleasesHostExclusion":                  28.94,
		"TestSQLiteRuntimeReaderBlocksOtherProcessAndRejectsAliases":        18.25,
		"TestSQLiteSnapshotStatementsEndWithTheirTransaction":               40.94,
		"TestSelfConfigPostgres":                                            7.38,
		"TestSelfConfigSQLite":                                              22.29,
		"TestTeardownMoveRefusalPostgres":                                   11.03,
		"TestTeardownMoveRefusalSQLite":                                     102.02,
		"TestVaultCrashHoldAndVersionBoundOperatorRecoveryPostgres":         3.93,
		"TestVaultCrashHoldAndVersionBoundOperatorRecoverySQLite":           11.72,
	},
	"internal/store/upgrade": {
		"TestBackupFenceDrainsRuntimeAndSurvivesCoordinatorRestart":           8.72,
		"TestBackupFenceOperatorRotationRequiresRecoveryAndDropsOldIntent":    8.55,
		"TestBackupFenceRejectsWrongProofAndRollsBackFailedPublication":       8.58,
		"TestBuildScratchSchemaSerializesEmptyCheckWithDDL":                   5.48,
		"TestCatalogRejectsUnknownObjectsBeyondTables":                        8.77,
		"TestDomainCatalogAndSourceInspectionShareExactFingerprint":           8.61,
		"TestFreshHierarchyIsAtomicAndCannotBeReinitialized":                  8.96,
		"TestFreshHierarchyRefusesPopulationAndLegacy":                        23.9,
		"TestFreshReconciliationOnlyBeforePopulation":                         8.57,
		"TestLegacyGenesisAndMigrationTamper":                                 7.9,
		"TestLegacyProposalNonceBootstrapIsAtomic":                            15.34,
		"TestManagedConfigurationMigrationBothDatabases":                      211.95,
		"TestMigrationProcessCrashAfterIndividualCommits":                     2.08,
		"TestPostgresNonTableCatalogDriftRefuses":                             2.94,
		"TestRestoreControlSchemaIsExactEmptyAndTransactional":                10.04,
		"TestRestoreMutationFailureDoesNotPublishSQLite":                      21.43,
		"TestRuntimeAdmissionDrainsTransactionsAndNeverRefreshesOldAuthority": 8.7,
		"TestSQLiteCatalogExcludesOnlyReservedSQLitePrefix":                   6.23,
		"TestSQLiteCatalogIncludesForgedReservedTrigger":                      5.96,
		"TestSameArchiveRestoresNewIncarnationsBeforePublication":             36.46,
		"TestSameReleaseRecoveryRequiresNewProofAndNeverRunsMigrations":       7.55,
		"TestSnapshotRejectsAppliedHistoryDrift":                              8.61,
		"TestSourceInspectionChecksActualIdentityAndStrongestEpoch":           9.52,
		"TestUnknownSchemaRefusesWithoutControlEffects":                       47.63,
	},
	"internal/upgradegate": {
		"TestActualCLIMigrateBootRestart":                                         11.77,
		"TestAuthenticatedBackupPreparationPinsFinalTargetAndRefusesMissingProof": 122.16,
		"TestFreshGateInvalidTrustAndRootHaveNoDatastoreEffects":                  16.92,
		"TestFreshGatePersistsSchemaOnlyAndRefusesDifferentResume":                46.37,
		"TestGatePopulatedProcessCrashRoutes":                                     315.22,
		"TestGateProcessCrashRestart":                                             280.46,
		"TestOperatorHistoricalRestoreRequiresCurrentPinAndEscrow":                46.91,
		"TestOperatorInstallationPinPersistsWithoutBackupEvidence":                27.32,
		"TestOperatorRotationJournalCrashResume":                                  67.65,
		"TestOperatorRotationPriorKeyAndLocalEscrow":                              48.97,
		"TestOperatorUncommittedJournalAcceptsFreshAuthenticatedReplacement":      42.95,
		"TestPackagedNightlyReleaseUpgrade":                                       25.95,
		"TestUpgradeConfigurationPreflightPreservesInstalledAuthority":            49.79,
		"TestUpgradeMaterialFingerprintSurvivesNextVerifiedBuildAndRecoveryFloor": 47.06,
	},
}
