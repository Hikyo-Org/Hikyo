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
	if len(args) == 0 || (args[0] != "race" && args[0] != "fuzz" && args[0] != "isolation") {
		return errors.New("usage: analysis-shards race|fuzz|isolation --root DIR --shard N --shards N")
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
		return writeIsolationShard(output, packages, opts)
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
		if pkg.relativePath == "internal/isolation" {
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
	if raceSequentialSuites[unit.pkg.relativePath] {
		s.sequential += unit.seconds
	} else {
		s.pool += unit.seconds
		s.longest = max(s.longest, s.seconds[unit.pkg.ImportPath]+unit.seconds)
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

func writeIsolationShard(output io.Writer, packages []packageInfo, opts options) error {
	tests, err := discoverIsolationTests(packages)
	if err != nil {
		return err
	}
	if len(tests) == 0 {
		return errors.New("no Test* target discovered in internal/isolation")
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

func discoverIsolationTests(packages []packageInfo) ([]isolationTest, error) {
	for index := range packages {
		if packages[index].relativePath == "internal/isolation" {
			return discoverPackageTests(packages[index])
		}
	}
	return nil, errors.New("internal/isolation package was not found")
}

// discoverPackageTests lists one package's top-level Test functions from its
// source, so a shard plan never depends on running the package first.
func discoverPackageTests(pkg packageInfo) ([]isolationTest, error) {
	return discoverPackageTargets(pkg, false)
}

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

// raceSequentialSuites mirrors test-race-packages.sh, which runs these
// filtered suites one at a time after the concurrent pool.
var raceSequentialSuites = map[string]bool{
	"internal/app":     true,
	"internal/service": true,
}

// racePackageSeconds holds each package's race duration on a CI runner, split
// suites summed across shards; unlisted packages weigh one second. Regenerate
// from the race shard logs of a green main run:
//
//	gh api --paginate '/repos/Hikyo-Org/Hikyo/actions/runs/RUN/jobs?per_page=100' --jq '.jobs[] | select(.name | test("race shard")) | .id' |
//	  xargs -n 1 gh run view --repo Hikyo-Org/Hikyo --log --job |
//	  awk '($(NF-2) == "ok" || $(NF-2) == "FAIL") && $NF ~ /^[0-9.]+s$/ { sub(".*/hikyo/", "", $(NF-1)); total[$(NF-1)] += $NF }
//	    END { for (p in total) if (total[p] >= 5) printf "\t\"%s\": %d,\n", p, total[p] + 0.5 }' | sort
var racePackageSeconds = map[string]float64{
	"api":                                 16,
	"internal/app":                        1428,
	"internal/cli":                        8,
	"internal/conformance":                163,
	"internal/crypto/backup":              23,
	"internal/importer":                   17,
	"internal/lint":                       307,
	"internal/mcpserver":                  6,
	"internal/operator":                   7,
	"internal/releasetrust":               6,
	"internal/selfupdate":                 10,
	"internal/server":                     69,
	"internal/service":                    1034,
	"internal/store":                      704,
	"internal/store/migrate":              84,
	"internal/store/tx":                   19,
	"internal/store/upgrade":              371,
	"internal/upgradecustody":             195,
	"internal/upgradegate":                608,
	"scripts/release/assemble-upgrade":    124,
	"scripts/release/embed-compatibility": 7,
	"scripts/release/stable":              8,
}

// raceTargetSeconds names the suites split across shards by top-level target
// and holds each target's local race duration, a quarter of it for t.Parallel
// targets (CI runs four at once); unlisted targets weigh one second. Only the
// proportions matter: the planner scales each suite to racePackageSeconds.
// test-race-packages.sh accepts a target filter for exactly these suites.
// Keep internal/lint whole: it loads the repository once per process.
// Regenerate against the PostgreSQL service used by the race_shard job:
//
//	HIKYO_TEST_POSTGRES_DSN=postgres://... go test -race -json -count=1 -p 1 -timeout=60m \
//	  ./internal/app ./internal/service ./internal/store ./internal/store/upgrade ./internal/upgradegate |
//	  jq -rs '[.[] | select(.Action == "pause") | [.Package, .Test]] as $parallel
//	    | [.[] | select((.Action == "pass" or .Action == "fail") and .Test and (.Test | contains("/") | not))
//	    | {p: (.Package | sub(".*/hikyo/"; "")), t: .Test,
//	      s: (.Elapsed / (if [.Package, .Test] | IN($parallel[]) then 4 else 1 end) | round)} | select(.s >= 2)]
//	    | group_by(.p)[] | "\t\"\(.[0].p)\": {", (sort_by(.t)[] | "\t\t\"\(.t)\": \(.s),"), "\t},"'
var raceTargetSeconds = map[string]map[string]float64{
	"internal/app": {
		"TestActualDefaultDevCLIStartsAndRestarts":                                        27,
		"TestAdminAuthRefusesUnverifiedLegacySchema":                                      5,
		"TestAdminAuthResourceOwnership":                                                  12,
		"TestAdminAuthWarnsWhenRootRotationPending":                                       6,
		"TestAdminCreateAdoptsRunningServerSeedInsteadOfCommandDefaults":                  8,
		"TestAdminCreateRequiresFreshServerSeed":                                          6,
		"TestAutomaticDiscoveryResolvesShallowAlternativesBeforeLongerHistory":            2,
		"TestAutomaticPrewriteRetryMeasuresRealSQLiteAndRestoreProof":                     9,
		"TestAutomaticUpgradePackagedNightlyRoute":                                        84,
		"TestBackupExportJobRecordsFailureLoudlyWithoutPlaintext":                         6,
		"TestBackupExportJobRunsOnceThenGatesOnTheInterval":                               6,
		"TestBackupPruneJobRemovesOnlyAgedArchives":                                       11,
		"TestBackupRestoreDiagnosticsLevels":                                              17,
		"TestBootLogsEffectiveSQLitePoolSizes":                                            7,
		"TestBootResourceOwnershipOnFailure":                                              29,
		"TestBootSuccessTransfersResourceOwnershipToServer":                               5,
		"TestBootWarmsOpenAPIBeforeListening":                                             5,
		"TestBootWithRootKeyFileAndWrongKeyRefused":                                       5,
		"TestBootstrapDeploymentBootSeedsSelectorsAndKeepsPendingRepairFenced":            6,
		"TestBootstrapDeploymentDatabaseProofPrecedesSignedSubmitAndTransport":            2,
		"TestBootstrapDeploymentEnrollmentPinsStandaloneNode":                             5,
		"TestBootstrapDeploymentRejectsReplacedAliasAndExpiredPrivateProof":               2,
		"TestBootstrapDeploymentRenewalPreservesCommittedAuthorityAfterRestart":           5,
		"TestBootstrapDeploymentRootPrepareIsSealedButNotPersisted":                       5,
		"TestBootstrapInitialHASourceCarriesUnchangedCorrespondenceAndReproof":            2,
		"TestBootstrapSingletonTopologyRequiresActualPrerequisites":                       7,
		"TestCandidateConfigurationFailureKeepsRestoreRequired":                           24,
		"TestCandidateHealthChecksEnrolledNextRootWithoutProvider":                        6,
		"TestDevBootGeneratesAndReusesRootKey":                                            6,
		"TestDevBootSeparatesPublicAndOperationalRoutes":                                  5,
		"TestDevelopmentControlsCannotChangeDeploymentTrustContext":                       5,
		"TestDevelopmentNodeControlsChangeActualRuntimeAfterActivation":                   7,
		"TestDevelopmentProviderPreservesRemoteStateAcrossOrdinaryReloadAndRefusesSwitch": 6,
		"TestDevelopmentProviderSwitchRechecksConfigurationAfterPreparation":              7,
		"TestEnrolledSourceLoaderAcceptsOnlyExplicitSingletonHAIdentity":                  5,
		"TestEscrowLocalCLIUsesAdmittedCurrentHierarchy":                                  11,
		"TestExplicitMigrateThenBootWithoutAutoMigrate":                                   5,
		"TestMigrateDiagnosticsLevelsAndNoOp":                                             23,
		"TestMigrateNeedsNoRootKey":                                                       5,
		"TestMigrationRefusesUnverifiedExistingSchemaBeforeWrites":                        10,
		"TestNativeTLSBootServesHTTPSAndKeepsOpsPlaintext":                                6,
		"TestNextRootLiveSelectionKeepsRotationSeparate":                                  8,
		"TestNextRootPreparationRejectsUnavailableSources":                                26,
		"TestNextRootSeedRequiresExactEnrolledProjection":                                 5,
		"TestNodeRuntimeCancelledActivationReleasesNewListener":                           5,
		"TestNodeRuntimeImportsTLSFilesOnce":                                              5,
		"TestNodeRuntimeInstallsIndependentCapacityAndPolicy":                             5,
		"TestNodeRuntimeListenerFailureKeepsOldGraphAndReleasesReservation":               6,
		"TestNodeRuntimeListenerMoveDrainsOldHTTPResponse":                                5,
		"TestNodeRuntimeManagedRestartDoesNotReadDeletedTLSFiles":                         6,
		"TestNodeRuntimeMissingHANodeBootsOnlyAdministrativeRecovery":                     3,
		"TestNodeRuntimePostgresPoolAppliesWithoutReplacingIdentity":                      2,
		"TestNodeRuntimeReservesMovesAndSwapsListeners":                                   5,
		"TestNodeRuntimeScheduledBackupUsesInstalledDestination":                          6,
		"TestNodeRuntimeTLSRotationAndPlaintextTransition":                                6,
		"TestOperationalListenerFailureStopsWholeLifecycle":                               6,
		"TestOrdinaryBackupPublicAdmissionAndDataOnlyRestore":                             24,
		"TestOwnerRuntimeActivationFailureResumesAdministrativeGraph":                     5,
		"TestOwnerRuntimeDrainsRequestsBeforeInstalling":                                  6,
		"TestOwnerRuntimeFailedTargetFencesBusinessAndPreservesHTTPRepair":                6,
		"TestOwnerRuntimeInstallsHTTPAuthAndRetentionGraph":                               5,
		"TestOwnerRuntimePreparationFailureAndMailOnlyKeepActiveGraph":                    5,
		"TestPostgresRestoreDestinationRefusesExpiredOwnerAndOccupiedTarget":              14,
		"TestProductionBackupCustodyRefusesBeforeDatabaseAdmission":                       6,
		"TestProxyHTTPSOriginEmitsHSTSOnLoopbackBackend":                                  5,
		"TestRecoveryCapabilityExpiresWithOwnerAndArchive":                                28,
		"TestRecoveryPostgresRollsBackWhenMigrationOwnerDies":                             6,
		"TestReleaseCompatibilityMatchesActualBothEngineSchemas":                          11,
		"TestRestorePostgresRollsBackWhenMigrationOwnerDies":                              4,
		"TestRestoreRequiredRestartExposesOnlyOperationalHealth":                          6,
		"TestRestoreSchemaRechecksPreflightBeforeAnyGooseWrite":                           3,
		"TestSchedulerHAThreeNodesOnePostgres":                                            2,
		"TestSchemaAheadOfBinaryRefusesToServe":                                           5,
		"TestServeCancellationStopsBothListeners":                                         5,
		"TestServeWithReadySignalsOnlyAfterHTTPServingStarts":                             5,
		"TestServerDiagnosticsDoNotClaimSuccessAfterListenerFailure":                      5,
		"TestServerDiagnosticsLevelsAndReadiness":                                         22,
		"TestSingletonHARestoreBootRetainsCoordinationThroughSourceRepair":                8,
		"TestSingletonReplacementBootUsesActualHAMode":                                    4,
		"TestUnattendedPackagedEnrollmentUpgradeAndIntermediateCredential":                118,
		"TestUnattendedScratchPreservesUnownedDataAndReusesOwnedSchema":                   2,
		"TestUpgradeDrillActualBothEngineRecoveryAndConfigOnlyEscrow":                     32,
		"TestUpgradeDrillAppliedLedgerKeepsSourceAuthorityAndRotatesScratch":              22,
		"TestUpgradeDrillAutoSelectionRefusesLostGrantAuthority":                          9,
		"TestUpgradeDrillAutoSelectionSkipsUnusableScopeBeforeCommit":                     57,
		"TestUpgradeDrillAutomaticallyProvesExistingCredentialAuthority":                  29,
		"TestUpgradeDrillDiagnosticsLevels":                                               37,
		"TestUpgradeDrillManualSelectionStillRequiredByDefault":                           9,
		"TestUpgradeDrillRefusesMissingOrWrongPrivateCustody":                             44,
		"TestUpgradeExportDiagnosticsPreserveJSON":                                        12,
		"TestVerifiedMigrateSkipsBackupOnHealthyRestart":                                  6,
	},
	"internal/service": {
		"TestAckSetCrossSurfaceReplayRejected":                                                5,
		"TestAckSetOpensEachPresentedTokenOnce":                                               5,
		"TestAckSetRejectionPrecedenceAndInputOrder":                                          5,
		"TestAckSetStaleContentAndVersionRejected":                                            5,
		"TestAckSetSurplusReported":                                                           5,
		"TestAckTokenExpires":                                                                 5,
		"TestAckTokenForeignKeyringRejected":                                                  11,
		"TestAckTokenOpaqueNoPlaintext":                                                       5,
		"TestAckTokenRoundTrip":                                                               5,
		"TestAckTokenTamperRejected":                                                          5,
		"TestAdapterAdoptRequiresRevealAcrossEveryAdapterEnvironment":                         5,
		"TestAdapterAttentionTargetReplacementResumesActivation":                              5,
		"TestAdapterCeremonyErrorClassification":                                              21,
		"TestAdapterCreateAndAddTargetRefuseBeforeCredentialOrProviderWithoutCeremony":        10,
		"TestAdapterCreateAtomicallyBootstrapsCredentialAndFirstTarget":                       5,
		"TestAdapterCredentialReplaceAndRevokeFenceWithoutAutoConverge":                       6,
		"TestAdapterDeleteQueuesEveryScrubAndRetainsCredentialUntilLastTerminal":              5,
		"TestAdapterFullTargetUpdateAuditsTransactionAuthorityTransition":                     5,
		"TestAdapterManualSyncRequiresTargetRevealAndSupersedesNewest":                        6,
		"TestAdapterMoveCredentialFailureRequiresAttentionAndCancelReconvergesOldRoute":       5,
		"TestAdapterOriginMoveCredentialFailureRequiresAttentionAndCancelReconvergesOldRoute": 6,
		"TestAdapterOriginMoveKeepsOldRouteAndCredentialThroughScrubBarrier":                  6,
		"TestAdapterPendingOriginReplacementAuditsTransactionAuthorityTransition":             5,
		"TestAdapterPlanPersistsProviderConflictArtifactAndInspectReturnsIt":                  5,
		"TestAdapterRoutingStateRoundTripsFromStore":                                          6,
		"TestAdapterTargetAddAuditsTransactionAuthorityTransition":                            5,
		"TestAdapterTargetAddRefusesDestinationEffectiveNameCollisionAtomically":              6,
		"TestAdapterTargetAddRequiresRevealAcrossEveryAdapterEnvironmentBeforeCredentialOpen": 5,
		"TestAdapterTargetConnectionReauthorizesEveryProviderRequest":                         6,
		"TestAdapterTargetDestinationMoveKeepRemoteReleasesBeforeActivation":                  5,
		"TestAdapterTargetDestinationMoveScrubsOldRouteBeforePendingRouteActivation":          5,
		"TestAdapterTargetInPlaceClassificationAndReplacementConverge":                        16,
		"TestAdapterTargetKeepRemoteReleasesAndEnumeratesCustodyWithoutReveal":                5,
		"TestAdapterTargetMoveActivationTestsPendingRouteThenEnqueuesConverge":                6,
		"TestAdapterTargetMoveDeadCredentialReleasesOldCustodyThenActivates":                  5,
		"TestAdapterTargetMoveScrubCompletionQueuesPendingRouteActivation":                    5,
		"TestAdapterTargetWidenRequiresRevealAcrossEveryAdapterEnvironment":                   5,
		"TestAdoptionBindingRechecksSeedFreshnessWithCurrentDatabaseClock":                    17,
		"TestApplyTargetMutationClassifiesUpdateAndMove":                                      11,
		"TestApplyTargetMutationConcurrentChangesUseLockedGeneration":                         5,
		"TestApplyTargetMutationKeepsMovePolicyInsideService":                                 10,
		"TestApplyTargetMutationRequiresCeremonyForUpdateAndMove":                             10,
		"TestCommittedDeploymentRenewsUnseenSubmitAndRestoreWithoutNewMFA":                    22,
		"TestCompleteRestoreClearsRestoredAdapterCredential":                                  5,
		"TestDeploymentRenewalRejectsChangedDecisionAndStaleRow":                              11,
		"TestEnvironmentCreatePersistsGenerationFenceAndCorrelatedAudit":                      5,
		"TestExportDirectorySyncFailurePreservesPublishedArtifact":                            19,
		"TestExportSyncsDirectoryAncestryWithoutOverwriting":                                  6,
		"TestFindingCapFailsClosed":                                                           5,
		"TestHostAdoptionPreservesLargeMailAndTLSSeed":                                        10,
		"TestHostAdoptionReadsFreshServerSeedWithoutEvaluatingCommandDefaults":                9,
		"TestHostAdoptionRechecksStandaloneDiscoveryBeforeBinding":                            8,
		"TestOrganizationSelectedRepositoryIDsAreVerifiedBeforeRoutingCommit":                 5,
		"TestPreparationExportUsesLiveSessionWithoutRuntimeAdmission":                         6,
		"TestPreparedPublisherPreservesOrdinaryPassphraseBackup":                              31,
		"TestProveValuesReadable":                                                             5,
		"TestPruneLoopStopsAfterFirstFailedUnlink":                                            5,
		"TestScanRejectionsNamedByClass":                                                      6,
		"TestSchemaPublishStorageChecksEverySnapshotAndRollsBack":                             7,
		"TestSelfConfigApplyRetryReturnsOriginalJobAfterCollection":                           13,
		"TestSelfConfigBootstrapLoadsItsPublishedConfiguration":                               9,
		"TestSelfConfigDeploymentCommitsBeforeSendingAndRequiresApplicationAck":               11,
		"TestSelfConfigDeploymentConcurrentFinalizeInvalidatesPreparedRoot":                   10,
		"TestSelfConfigDeploymentRestoreAllowsRepairThenAnotherRollout":                       10,
		"TestSelfConfigDeploymentRestoreRequiresExactMFAAndJournalsBeforeSending":             10,
		"TestSelfConfigDeploymentRootPersistenceRequiresExactMFA":                             39,
		"TestSelfConfigExistingInstanceAdoptionIsExplicitAndDurable":                          9,
		"TestSelfConfigExpiredApplyRetryDoesNotRestartPreparation":                            11,
		"TestSelfConfigHostRecoveryNeedsQuiescenceAndRejectsNetwork":                          11,
		"TestSelfConfigIndependentOwnersKeepSeparateProjectsAndRuntime":                       19,
		"TestSelfConfigInstallerActivationFailureFencesAndRetriesCommittedTarget":             10,
		"TestSelfConfigInstallerDisposesAbortedCandidateWithoutReplacement":                   11,
		"TestSelfConfigInstallerDisposesAbortedSupersededAndShutdownCandidates":               10,
		"TestSelfConfigInstallerFailedTargetCanBeRepaired":                                    11,
		"TestSelfConfigInstallerPreparationFailureAbortsBeforeTargetCommit":                   10,
		"TestSelfConfigInstallerRetainsPreparationAndAcknowledgesOnlyAfterActivation":         9,
		"TestSelfConfigInvalidMailPublishRetainsActiveConfiguration":                          10,
		"TestSelfConfigInvalidOwnerValuePublishNamesTheKey":                                   9,
		"TestSelfConfigInvalidSecretOwnerValuePublishNamesKeyOnly":                            9,
		"TestSelfConfigNextRootSelectorNeedsExactApply":                                       11,
		"TestSelfConfigNodeSeedAdoptionImportsEncryptedLocalInputs":                           11,
		"TestSelfConfigNodeSeedChangeInvalidatesReviewedAdoption":                             10,
		"TestSelfConfigNormalKeyRotationPreservesRuntimeSnapshots":                            11,
		"TestSelfConfigOriginApplyRefusesRetiringPasskeyHostname":                             9,
		"TestSelfConfigOriginRecovery":                                                        28,
		"TestSelfConfigOriginReviewLoadsRetainedOriginWithoutActiveGraph":                     10,
		"TestSelfConfigPublishNeedsExplicitReauthenticatedApply":                              9,
		"TestSelfConfigRepairScopeRequiresMFAInstanceAdminAndExactHierarchy":                  11,
		"TestSelfConfigResolveRuntimeBundleDoesNotAcknowledgeOrAcceptNetworkAuthority":        11,
		"TestSelfConfigRestoreRequiresConfirmationBoundIntoReauthentication":                  9,
		"TestSelfConfigRootFinalizationWaitsForRestoredRolloutRepair":                         10,
		"TestSelfConfigSingletonTopologyFencesOldIdentityAcrossOrdinaryApply":                 12,
		"TestSelfConfigSingletonTopologyRestoreRequiresFreshRepair":                           9,
		"TestSelfConfigSuspensionKeepsRecoveryInterfaceAvailable":                             9,
		"TestSelfConfigTestMailChargesFivePerPrincipalPerHour":                                10,
		"TestSelfConfigTestMailRecordsOutcomeAfterActorRevocation":                            10,
		"TestSelfConfigTopologySurvivesSourceRestoreAndOrdinaryRepair":                        22,
		"TestSelfConfigUpgradeDeploymentRequiresExactMFAAndApplicationAck":                    11,
		"TestSessionCompletionCreatesFactorParity":                                            5,
		"TestSessionCompletionPublishesOnlyCommittedAttemptTokens":                            10,
		"TestSessionCompletionRotationPreservesProjectionAndReplacesFactors":                  5,
		"TestTargetedKeyDeleteCascadesMembershipAndQueuesOwnedSlotPrune":                      5,
		"TestUpdateOutcomeReconcilesWithoutRequestingSession":                                 5,
		"TestUpdateOutcomeRetryAfterAcknowledgementFailureIsIdempotent":                       5,
		"TestUpdateRequestRefusesAndAuditsWithoutHelperContact":                               5,
		"TestUpdateRequestRequiresRecentHumanAuthenticationBeforeHelperContact":               5,
	},
	"internal/store": {
		"TestAdapterAdoptionClearsExpiredProviderFence":                     5,
		"TestAdapterAdoptionLedgerCollisionIsConflictPostgres":              2,
		"TestAdapterAdoptionLedgerCollisionIsConflictSQLite":                5,
		"TestAdapterAdoptionRefusesStaleArtifactAndLiveProviderFence":       16,
		"TestAdapterAttemptsRecordRevisionErrorClassAndAttention":           5,
		"TestAdapterClaimDueReplaysExpiredLeaseOnly":                        5,
		"TestAdapterClaimDueSingleFlightsTargetAndCapsOrganizationAtFour":   5,
		"TestAdapterClaimSettlesCrashWindowAsUnknownOutcome":                5,
		"TestAdapterConcurrentAdoptionHasOneWinner":                         6,
		"TestAdapterConflictInsertIdempotentAcrossRetries":                  5,
		"TestAdapterConflictsHideStaleGeneration":                           5,
		"TestAdapterDeadCredentialScrubTerminatesAndEnumeratesOrphans":      5,
		"TestAdapterEnqueueClearsExpiredProviderWriteFence":                 5,
		"TestAdapterEnqueueRefusesAtPerTargetQueueDepth":                    5,
		"TestAdapterEnqueueSupersedesAndBumpsGenerationAtomically":          5,
		"TestAdapterEnqueueWaitsForProviderWriteFence":                      6,
		"TestAdapterFinishJobRequiresExactLeaseAndGeneration":               5,
		"TestAdapterFinishPreservesConcurrentReleasedCustody":               5,
		"TestAdapterGateRechecksAuthorityAndGeneration":                     5,
		"TestAdapterJournalCommitsIntentOutcomeAndLedgerAtomically":         5,
		"TestAdapterJournalFinishesUnsentIntentBeforeAuthorityAbort":        5,
		"TestAdapterJournalFinishesUnsentIntentBeforeGenerationAbort":       5,
		"TestAdapterJournalPUTNotFoundEndsEffectBeforeFreshCreateRetry":     5,
		"TestAdapterJournalPersistsOwnedMissingAndAuditFinding":             5,
		"TestAdapterJournalRefusesOwnedMissingCompletionAfterRelease":       5,
		"TestAdapterJournalRejectsTerminalWriteAfterLeaseLoss":              5,
		"TestAdapterJournalRejectsZeroCompletionWithoutDeletingLedger":      6,
		"TestAdapterJournalReleasedStateRetainsLedgerRow":                   5,
		"TestAdapterOriginReusableAfterTombstonePostgres":                   2,
		"TestAdapterOriginReusableAfterTombstoneSQLite":                     5,
		"TestAdapterPauseBlocksClaimsKeepsLedgerAndResumeCatchesUp":         5,
		"TestAdapterPlanArtifactAdoptionIsBoundAndAtomic":                   6,
		"TestAdapterProviderSwitchRefusesExistingOutboxAndConfiguration":    5,
		"TestAdapterProviderSwitchRequiresHostContext":                      6,
		"TestAdapterReserveRebindsReleasedHistoryToActivatedRoute":          5,
		"TestAdapterStaleWorkerIsFencedAfterReclaim":                        5,
		"TestAdapterStampsCompareLexicallyAcrossBridges":                    5,
		"TestAdapterSuccessPersistsProviderWarnings":                        5,
		"TestAdapterTargetKeysAndHealthCounts":                              5,
		"TestAdapterTargetSurfacesPossibleCaptureWithoutClaimingOwnership":  5,
		"TestCoordinationClaimCommitFailurePreservesProvisionalFence":       2,
		"TestCoordinationPostgres":                                          3,
		"TestCoordinationSQLite":                                            6,
		"TestCrashReservationReleaseIsGenerationFencedAndLeavesNoConflict":  11,
		"TestEveryDirectRuntimeFamilyRefusesMaintenanceAndOldGeneration":    7,
		"TestExportPostgresManifestUsesCopySnapshotDuringMigration":         2,
		"TestExportSQLiteManifestUsesArchivedSchemaDuringMigration":         6,
		"TestKeyRotationInvariantsPostgres":                                 2,
		"TestKeyRotationInvariantsSQLite":                                   6,
		"TestOpenSQLiteCapsConnectionPools":                                 5,
		"TestPostgresSourceProofBindsAliasAndExpires":                       2,
		"TestPostgresSourceProofRefusesRecoveryChange":                      2,
		"TestPostgresSourceProofRejectsClonedInstallation":                  2,
		"TestPostgresSourceProofRequiresRuntimePrivileges":                  3,
		"TestPostgresSourceProofSanitizesFailuresAndTimeout":                2,
		"TestPostgresTimestampsAreUTC":                                      3,
		"TestPreparedPostgresPoolConcurrentCoordination":                    2,
		"TestPreparedPostgresPoolDefaultsAndStaleCandidates":                3,
		"TestPreparedPostgresPoolPreservesTransactionsAndCoordination":      2,
		"TestPreparedPostgresPoolRefusesChangedAdmission":                   8,
		"TestPreparedPostgresPoolSQLitePolicy":                              5,
		"TestPublishedGenerationSupersedesWithoutStealingLiveProviderFence": 5,
		"TestReservationReleaseCannotDropOwnedOrDispatchedCustody":          5,
		"TestRestoreSQLiteDirectoryDurabilityPreservesCommittedMutations":   6,
		"TestRuntimeOpenRequiresActualGateAndSQLiteReadsCannotWrite":        5,
		"TestRuntimeTransactionPanicReleasesHostExclusion":                  5,
		"TestSQLiteRuntimeReaderBlocksOtherProcessAndRejectsAliases":        7,
		"TestSQLiteSnapshotStatementsEndWithTheirTransaction":               10,
		"TestSelfConfigPostgres":                                            2,
		"TestSelfConfigSQLite":                                              8,
	},
	"internal/store/upgrade": {
		"TestBackupFenceDrainsRuntimeAndSurvivesCoordinatorRestart":           7,
		"TestBackupFenceOperatorRotationRequiresRecoveryAndDropsOldIntent":    5,
		"TestBackupFenceRejectsWrongProofAndRollsBackFailedPublication":       5,
		"TestBuildScratchSchemaSerializesEmptyCheckWithDDL":                   5,
		"TestCatalogRejectsUnknownObjectsBeyondTables":                        5,
		"TestDomainCatalogAndSourceInspectionShareExactFingerprint":           7,
		"TestFreshHierarchyIsAtomicAndCannotBeReinitialized":                  5,
		"TestFreshHierarchyRefusesPopulationAndLegacy":                        14,
		"TestFreshReconciliationOnlyBeforePopulation":                         6,
		"TestLegacyGenesisAndMigrationTamper":                                 6,
		"TestLegacyProposalNonceBootstrapIsAtomic":                            9,
		"TestManagedConfigurationMigrationBothDatabases":                      119,
		"TestMigrationProcessCrashAfterIndividualCommits":                     2,
		"TestMigrationResumeRejectsCorruptHistoryBeforeTargetEffects":         2,
		"TestPostgresNonTableCatalogDriftRefuses":                             4,
		"TestRestoreControlSchemaIsExactEmptyAndTransactional":                6,
		"TestRestoreMutationFailureDoesNotPublishSQLite":                      6,
		"TestRuntimeAdmissionDrainsTransactionsAndNeverRefreshesOldAuthority": 9,
		"TestSameArchiveRestoresNewIncarnationsBeforePublication":             13,
		"TestSameReleaseRecoveryRequiresNewProofAndNeverRunsMigrations":       6,
		"TestSnapshotRejectsAppliedHistoryDrift":                              6,
		"TestSourceInspectionChecksActualIdentityAndStrongestEpoch":           9,
		"TestUnknownSchemaRefusesWithoutControlEffects":                       31,
	},
	"internal/upgradegate": {
		"TestActualCLIMigrateBootRestart":                                         25,
		"TestAuthenticatedBackupPreparationPinsFinalTargetAndRefusesMissingProof": 43,
		"TestFreshGateInvalidTrustAndRootHaveNoDatastoreEffects":                  10,
		"TestFreshGatePersistsSchemaOnlyAndRefusesDifferentResume":                20,
		"TestGatePopulatedProcessCrashRoutes":                                     178,
		"TestGateProcessCrashRestart":                                             90,
		"TestOperatorHistoricalRestoreRequiresCurrentPinAndEscrow":                17,
		"TestOperatorInstallationPinPersistsWithoutBackupEvidence":                35,
		"TestOperatorRotationJournalCrashResume":                                  25,
		"TestOperatorRotationPriorKeyAndLocalEscrow":                              29,
		"TestOperatorUncommittedJournalAcceptsFreshAuthenticatedReplacement":      17,
		"TestPackagedNightlyReleaseUpgrade":                                       60,
		"TestUpgradeConfigurationPreflightPreservesInstalledAuthority":            17,
		"TestUpgradeMaterialFingerprintSurvivesNextVerifiedBuildAndRecoveryFloor": 19,
	},
}
