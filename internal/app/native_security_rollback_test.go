//go:build darwin || linux

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
	"github.com/gofrs/flock"
)

// rollbackDriverHost adds the parts of the host a rollback drives to the fixture's fake.
type rollbackDriverHost struct {
	*nativeDriverHost
	maintenance bool
}

func (h *rollbackDriverHost) Maintenance() (bool, error)         { return h.maintenance, nil }
func (h *rollbackDriverHost) RuntimeUID() int                    { return os.Geteuid() }
func (h *rollbackDriverHost) FenceAndStop(context.Context) error { h.maintenance = true; return nil }
func (h *rollbackDriverHost) StartCandidate(context.Context, string, bool, time.Duration) error {
	return nil
}
func (h *rollbackDriverHost) Complete(context.Context) error                      { h.maintenance = false; return nil }
func (h *rollbackDriverHost) StageCandidate(string, string) (string, error)       { return "", nil }
func (h *rollbackDriverHost) InstallBinary(context.Context, string, string) error { return nil }
func (h *rollbackDriverHost) ConfigureRuntime(context.Context, hostupgrade.RuntimeEvidence) error {
	return nil
}

func rollbackDriver(t *testing.T, phase string, record, maintenance bool) (*nativeDriverFixture, string) {
	t.Helper()
	f := newNativeDriverFixture(t)
	host := &rollbackDriverHost{nativeDriverHost: f.host, maintenance: maintenance}
	f.driver.load = func(context.Context) (nativeSecurityInstallation, error) {
		return nativeSecurityInstallation{Host: host, Database: upgrade.Config{Engine: releaseidentity.SQLite}, Inspection: f.inspection}, nil
	}
	directory := f.host.configuration.StateDirectory
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if phase != "" {
		source := f.inspection.state.Applied.Release
		journal := &automaticJournal{Format: hostJournalFormat, Phase: phase, Target: f.target.Identity, Instance: "native-fixture", Route: releaseidentity.Hash([]byte("route")),
			Source: upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: source}, Migrations: releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}, SchemaSHA256: releaseidentity.Hash([]byte("schema"))}}
		if err := writeAutomaticJournal(filepath.Join(directory, "operation.json"), journal); err != nil {
			t.Fatal(err)
		}
		if record {
			if err := writeNativeSourceRecord(directory, nativeSourceRecord{Format: nativeSourceRecordFormat, Instance: "native-fixture", Target: f.target.Identity, Route: journal.Route, Source: source,
				BinarySHA256: f.host.digest, Runtime: hostupgrade.RuntimeEvidence{BundleDirectory: directory, OperatorPublicKey: "key"}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return f, directory
}

// rollbackHost is the fixture's stateful fake, for stubs that model what the target's own rollback does to the fence.
func rollbackHost(t *testing.T, f *nativeDriverFixture) *rollbackDriverHost {
	t.Helper()
	installation, err := f.driver.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	host, ok := installation.Host.(*rollbackDriverHost)
	if !ok {
		t.Fatal("the fixture host is not the rollback fake")
	}
	return host
}

func observeSource(t *testing.T, f *nativeDriverFixture) securitymaintenance.Observation {
	t.Helper()
	source, err := f.driver.Observe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return source
}

// pinned is the operation the rollbackDriver fixture's journal and record describe.
func pinned(f *nativeDriverFixture, source securitymaintenance.Observation) securitymaintenance.Operation {
	return securitymaintenance.Operation{Target: f.target, Source: source, RouteSHA256: releaseidentity.Hash([]byte("route"))}
}

func TestNativeRestoreAbortedWithNothingJournaledReleasesThePin(t *testing.T) {
	f, _ := rollbackDriver(t, "", false, false)
	err := f.driver.RestoreAborted(t.Context(), securitymaintenance.Observation{}, pinned(f, observeSource(t, f)))
	// Nothing was stopped: the pin is released, and no outage is counted.
	if !errors.Is(err, securitymaintenance.ErrNotStarted) || errors.Is(err, securitymaintenance.ErrSourceRestored) {
		t.Fatalf("a healthy untouched source did not release the pin without an outage: %v", err)
	}
}

func TestNativeRestoreAbortedRefusesAnUnguardedUnfinishedOperation(t *testing.T) {
	f, _ := rollbackDriver(t, "schema-applied", false, true)
	err := f.driver.RestoreAborted(t.Context(), securitymaintenance.Observation{}, pinned(f, observeSource(t, f)))
	if err == nil || errors.Is(err, securitymaintenance.ErrNotStarted) {
		t.Fatalf("an operation with no source record released the pin: %v", err)
	}
}

func TestNativeRestoreAbortedRunsTheTargetsRollbackAndClearsWhatItLeft(t *testing.T) {
	f, directory := rollbackDriver(t, "schema-applied", true, true)
	var seen nativeSecurityAuthorization
	f.driver.runUpgrade = func(_ context.Context, _ releaseidentity.Identity, _ string, a nativeSecurityAuthorization, _ io.Writer) error {
		seen = a
		// What the verified target does before it reports: leave the journal where the source accepts it.
		done, err := readAutomaticJournal(filepath.Join(directory, "operation.json"))
		if err != nil {
			return err
		}
		done.Phase, done.Aborted = "preparing", "rolled back to the fenced snapshot"
		if err := writeAutomaticJournal(filepath.Join(directory, "operation.json"), done); err != nil {
			return err
		}
		// The target restarted the source and lifted the fence before it reported.
		rollbackHost(t, f).maintenance = false
		return errNativeRolledBack
	}
	err := f.driver.RestoreAborted(t.Context(), securitymaintenance.Observation{}, pinned(f, observeSource(t, f)))
	if !errors.Is(err, securitymaintenance.ErrNotStarted) || !seen.Rollback {
		t.Fatalf("err=%v authorization=%+v", err, seen)
	}
	// The abandoned attempt's journal and the source record are both spent.
	if journal, readErr := readAutomaticJournal(filepath.Join(directory, "operation.json")); readErr != nil || journal != nil {
		t.Fatalf("journal after the rollback: %+v %v", journal, readErr)
	}
	if _, err := os.Stat(filepath.Join(directory, "operation-source.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the source record survived a proven rollback: %v", err)
	}
}

// A rollback killed after it started stopping things is finished by the maintenance case, not refused as
// "past preparation but not fenced", and its source restore carries the target's own report.
func TestNativeRestoreAbortedFinishesARollbackKilledMidway(t *testing.T) {
	for name, report := range map[string]error{"snapshot restored": errNativeRolledBack, "source untouched": errNativeSourceRestored} {
		for _, lifted := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s fence lifted %t", name, lifted), func(t *testing.T) {
				f, directory := rollbackDriver(t, "rolling-back", true, true)
				var seen nativeSecurityAuthorization
				f.driver.runUpgrade = func(_ context.Context, _ releaseidentity.Identity, _ string, a nativeSecurityAuthorization, _ io.Writer) error {
					seen = a
					rollbackHost(t, f).maintenance = !lifted
					return report
				}
				err := f.driver.RestoreAborted(t.Context(), securitymaintenance.Observation{}, pinned(f, observeSource(t, f)))
				if !lifted {
					// A report without the fence lifted leaves a source a reboot would not start: keep everything.
					if err == nil || errors.Is(err, securitymaintenance.ErrNotStarted) {
						t.Fatalf("a still-fenced source released the pin: %v", err)
					}
					if journal, readErr := readAutomaticJournal(filepath.Join(directory, "operation.json")); readErr != nil || journal == nil {
						t.Fatalf("the journal went with the fence still set: %+v %v", journal, readErr)
					}
					if _, statErr := os.Stat(filepath.Join(directory, "operation-source.json")); statErr != nil {
						t.Fatalf("the record went with the fence still set: %v", statErr)
					}
					return
				}
				if !seen.Rollback || !errors.Is(err, securitymaintenance.ErrSourceRestored) || errors.Is(err, securitymaintenance.ErrRolledBack) != errors.Is(report, securitymaintenance.ErrRolledBack) {
					t.Fatalf("rollback=%t err=%v", seen.Rollback, err)
				}
			})
		}
	}
}

func TestNativeRestoreAbortedRefusesAJournalOfAnotherOperation(t *testing.T) {
	f, directory := rollbackDriver(t, "schema-applied", true, true)
	f.driver.runUpgrade = func(context.Context, releaseidentity.Identity, string, nativeSecurityAuthorization, io.Writer) error {
		t.Fatal("rolled back an operation the pin does not name")
		return nil
	}
	// A leftover guarded journal S->T2 must not drive recovery while S->T1 is pinned.
	for name, mutate := range map[string]func(*securitymaintenance.Operation){
		"another source": func(op *securitymaintenance.Operation) { op.Source.Instance = "ins_another" },
		"another target": func(op *securitymaintenance.Operation) { op.Target.Identity.Version += "-another" },
		"another route": func(op *securitymaintenance.Operation) {
			op.RouteSHA256 = releaseidentity.Hash([]byte("another route"))
		},
	} {
		op := pinned(f, observeSource(t, f))
		mutate(&op)
		if err := f.driver.RestoreAborted(t.Context(), securitymaintenance.Observation{}, op); err == nil || errors.Is(err, securitymaintenance.ErrNotStarted) {
			t.Fatalf("%s: the journal was acted on: %v", name, err)
		}
		if journal, err := readAutomaticJournal(filepath.Join(directory, "operation.json")); err != nil || journal == nil || journal.Phase != "schema-applied" {
			t.Fatalf("%s: the refused journal changed: %+v %v", name, journal, err)
		}
	}
}

// A fenced "preparing" journal is a disruptive source restore, and it is a rollback only when the journal
// says the snapshot was written back.
func TestNativeRestoreAbortedFencedPreparingReportsARollbackOnlyWhenTheJournalSaysSo(t *testing.T) {
	for aborted, rolledBack := range map[string]bool{"": false, rolledBackAborted: true} {
		f, directory := rollbackDriver(t, "", false, true)
		observed := observeSource(t, f)
		if _, err := f.driver.Prepare(t.Context(), f.target); err != nil {
			t.Fatal(err)
		}
		route := f.driver.prepared.Route
		manifest, err := route.Plan.SourceManifest(releaseidentity.SQLite)
		if err != nil {
			t.Fatal(err)
		}
		// The journal describes the row as it stands, which is what lets the source restart.
		journal := &automaticJournal{Format: hostJournalFormat, Phase: "preparing", Aborted: aborted, Target: f.target.Identity, Instance: route.Instance, Route: route.Plan.Digest(),
			Source: upgradecompat.InstalledSource{Identity: route.Plan.Source(), Migrations: manifest, SchemaSHA256: route.Plan.SourceSchemaDigest()}}
		if err := writeAutomaticJournal(filepath.Join(directory, "operation.json"), journal); err != nil {
			t.Fatal(err)
		}
		if err := writeNativeSourceRecord(directory, nativeSourceRecord{Format: nativeSourceRecordFormat, Instance: route.Instance, Target: f.target.Identity, Route: journal.Route, Source: route.Plan.Source().Release,
			BinarySHA256: f.host.digest, Runtime: hostupgrade.RuntimeEvidence{BundleDirectory: directory, OperatorPublicKey: "key"}}); err != nil {
			t.Fatal(err)
		}
		err = f.driver.RestoreAborted(t.Context(), securitymaintenance.Observation{}, securitymaintenance.Operation{Target: f.target, Source: observed, RouteSHA256: route.Plan.Digest()})
		if !errors.Is(err, securitymaintenance.ErrNotStarted) || !errors.Is(err, securitymaintenance.ErrSourceRestored) || errors.Is(err, securitymaintenance.ErrRolledBack) != rolledBack {
			t.Fatalf("aborted=%q: err=%v, want rolled back = %t", aborted, err, rolledBack)
		}
	}
}

func TestNativeRestoreAbortedKeepsThePinUnlessTheSourceIsProven(t *testing.T) {
	for name, run := range map[string]func(*nativeDriverFixture) error{
		"target failed generically": func(*nativeDriverFixture) error { return errors.New("exit status 1") },
		"rolled back but the source is not serving": func(f *nativeDriverFixture) error {
			f.host.failure = errors.New("the source is not running")
			return errNativeRolledBack
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, directory := rollbackDriver(t, "schema-applied", true, true)
			source := observeSource(t, f)
			f.driver.runUpgrade = func(context.Context, releaseidentity.Identity, string, nativeSecurityAuthorization, io.Writer) error {
				return run(f)
			}
			err := f.driver.RestoreAborted(t.Context(), securitymaintenance.Observation{}, pinned(f, source))
			if err == nil || errors.Is(err, securitymaintenance.ErrNotStarted) {
				t.Fatalf("an unproven rollback released the pin: %v", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "operation-source.json")); err != nil {
				t.Fatalf("the source record was dropped without proof: %v", err)
			}
		})
	}
}

func TestSecurityMaintenanceNativeTimeoutIsBounded(t *testing.T) {
	for seconds, ok := range map[int]bool{0: true, 1800: true, 86400: true, -1: false, 86401: false} {
		c := testSecurityMaintenanceConfiguration()
		c.TimeoutSeconds = seconds
		if err := c.Validate(); (err == nil) != ok {
			t.Fatalf("timeout_seconds=%d: %v", seconds, err)
		}
	}
}

// The record the guarded update writes is the one a rollback needs: through the production
// writer, run gets past the record and journal checks and the stop, to the operator custody.
func TestNativeRollbackRunFindsTheRecordTheUpdateWrites(t *testing.T) {
	f, directory := rollbackDriver(t, "schema-applied", false, true)
	path := filepath.Join(directory, "operation.json")
	journal, err := readAutomaticJournal(path)
	if err != nil || journal == nil {
		t.Fatal(journal, err)
	}
	journal.Runtime = hostupgrade.RuntimeEvidence{BundleDirectory: directory, OperatorPublicKey: filepath.Join(directory, "operator.pub"), EvidenceDirectory: directory, CiphertextPath: filepath.Join(directory, "backup.age")}
	if err := writeAutomaticJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	host := &rollbackDriverHost{nativeDriverHost: f.host, maintenance: true}
	reachedCustody := errors.New("reached the operator custody")
	run := nativeRollback{host: host, journalPath: path, openCustody: func(string) (*upgradecustody.Vault, error) { return nil, reachedCustody }}
	if _, _, err := run.run(t.Context()); err == nil || errors.Is(err, reachedCustody) {
		t.Fatalf("run proceeded without a source record: %v", err)
	}
	guard := nativeSecuritySourceGuard{Source: securitymaintenance.Observation{Identity: f.inspection.state.Applied.Release}, BinarySHA256: f.host.digest}
	if err := recordNativeSource(directory, journal, guard, hostupgrade.RuntimeEvidence{BundleDirectory: directory}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run.run(t.Context()); !errors.Is(err, reachedCustody) {
		t.Fatalf("run did not accept the record the update writes: %v", err)
	}
}

func TestNativeRollbackExitCodeCrossesTheHandoff(t *testing.T) {
	exit := exec.Command("sh", "-c", "exit 25").Run()
	if err := AutomaticUpgradeHandoffError(exit); !errors.Is(err, errNativeRolledBack) || AutomaticUpgradeExitCode(err) != AutomaticUpgradeRolledBackExitCode {
		t.Fatalf("handoff error = %v", err)
	}
}

// The exit status follows what the rollback did: a snapshot put back is 25, a source found untouched is 24.
func TestNativeRollbackExitCodeFollowsWhetherTheSnapshotWasRestored(t *testing.T) {
	for restored, want := range map[bool]int{true: AutomaticUpgradeRolledBackExitCode, false: AutomaticUpgradeRestoredExitCode} {
		if got := AutomaticUpgradeExitCode(nativeRestoredOutcome(restored)); got != want {
			t.Fatalf("snapshot restored=%t: exit %d, want %d", restored, got, want)
		}
	}
}

type unreadableControl struct{ *nativeDriverInspection }

func (unreadableControl) Control(context.Context) (upgrade.State, error) {
	return upgrade.State{}, errors.New("database unreadable")
}

func TestNativeNoFinalGateReadsTheControlRowThenTheJournal(t *testing.T) {
	f, _ := rollbackDriver(t, "", false, false)
	op := securitymaintenance.Operation{Source: observeSource(t, f)}
	if ok, err := f.driver.NoFinalGate(t.Context(), op); err != nil || !ok {
		t.Fatalf("the source row is short of the gate: %t %v", ok, err)
	}
	// Healthy at another release is past the gate, ready or not.
	f.inspection.state.Applied = releaseidentity.Source{Release: f.target.Identity}
	if ok, err := f.driver.NoFinalGate(t.Context(), op); err != nil || ok {
		t.Fatalf("a row healthy at the target was not past the gate: %t %v", ok, err)
	}
	for phase, want := range map[string]bool{"write-intent": true, "proved": true, "rolling-back": true, "schema-applied": false, "hop-healthy": false} {
		f, _ := rollbackDriver(t, phase, false, true)
		inspection, source := f.inspection, observeSource(t, f)
		f.driver.load = func(context.Context) (nativeSecurityInstallation, error) {
			return nativeSecurityInstallation{Host: &rollbackDriverHost{nativeDriverHost: f.host}, Database: upgrade.Config{Engine: releaseidentity.SQLite}, Inspection: unreadableControl{inspection}}, nil
		}
		op := securitymaintenance.Operation{Target: f.target, Source: source, RouteSHA256: releaseidentity.Hash([]byte("route"))}
		if ok, _ := f.driver.NoFinalGate(t.Context(), op); ok != want {
			t.Fatalf("unreadable row with journal %q: proven=%t, want %t", phase, ok, want)
		}
		// Another guarded update's journal proves nothing about the pinned one.
		op.RouteSHA256 = releaseidentity.Hash([]byte("another route"))
		if ok, err := f.driver.NoFinalGate(t.Context(), op); ok || err == nil {
			t.Fatalf("journal %q of another operation proved the pinned one short of the gate: %v", phase, err)
		}
	}
}

func TestNativeNoFinalGateRefusesAnotherInstallationsRow(t *testing.T) {
	f, _ := rollbackDriver(t, "", false, false)
	source := observeSource(t, f)
	source.Instance = "ins_another"
	if ok, err := f.driver.NoFinalGate(t.Context(), securitymaintenance.Operation{Source: source}); err == nil || ok {
		t.Fatalf("a foreign row proved no gate: %t %v", ok, err)
	}
}

// A target left running by a killed coordinator holds the lock; its journal and record must survive.
func TestNativeReleaseWaitsForATargetStillHoldingTheUpgradeLock(t *testing.T) {
	f, directory := rollbackDriver(t, "preparing", true, false)
	lock := flock.New(filepath.Join(directory, "upgrade.lock"))
	if held, err := lock.TryLock(); err != nil || !held {
		t.Fatal(held, err)
	}
	defer lock.Unlock()
	source := observeSource(t, f)
	for name, err := range map[string]error{
		"reconcile": f.driver.Reconcile(t.Context(), securitymaintenance.Observation{}),
		"restore":   f.driver.RestoreAborted(t.Context(), securitymaintenance.Observation{}, pinned(f, source)),
	} {
		if err == nil || errors.Is(err, securitymaintenance.ErrNotStarted) {
			t.Fatalf("%s went ahead under the lock: %v", name, err)
		}
	}
	if journal, err := readAutomaticJournal(filepath.Join(directory, "operation.json")); err != nil || journal == nil || journal.Phase != "preparing" {
		t.Fatalf("the journal did not survive: %+v %v", journal, err)
	}
}

// Restarted and unfenced, then killed before the journal cleared: still a rollback, and it counts.
func TestNativeRestoreAbortedCountsAFinishedRollbackUnderAnUnfencedJournal(t *testing.T) {
	f, directory := rollbackDriver(t, "preparing", true, false)
	path := filepath.Join(directory, "operation.json")
	journal, err := readAutomaticJournal(path)
	if err != nil || journal == nil {
		t.Fatal(journal, err)
	}
	journal.Aborted = rolledBackAborted
	if err := writeAutomaticJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	err = f.driver.RestoreAborted(t.Context(), securitymaintenance.Observation{}, pinned(f, observeSource(t, f)))
	if !errors.Is(err, securitymaintenance.ErrRolledBack) || !errors.Is(err, securitymaintenance.ErrSourceRestored) {
		t.Fatalf("a finished rollback was not counted: %v", err)
	}
}

func TestChownSQLiteFilesRefusesALinkLeftByTheServiceAccount(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "hikyo.db")
	outside := filepath.Join(directory, "outside")
	for _, name := range []string{path, outside} {
		if err := os.WriteFile(name, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, path+"-wal"); err != nil {
		t.Fatal(err)
	}
	if err := chownSQLiteFiles(path, os.Geteuid()); err == nil {
		t.Fatal("followed a symlink beside the database")
	}
	if err := os.Remove(path + "-wal"); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, path+"-wal"); err != nil {
		t.Fatal(err)
	}
	if err := chownSQLiteFiles(path, os.Geteuid()); err == nil || !strings.Contains(err.Error(), "multiply linked") {
		t.Fatal("changed the owner through a hard link beside the database", err)
	}
}

func TestChownSQLiteFilesToleratesMissingSidecarsAndFailsOnReal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hikyo.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := chownSQLiteFiles(path, os.Geteuid()); err != nil {
		t.Fatalf("missing -wal and -shm must be skipped: %v", err)
	}
	if os.Geteuid() != 0 {
		if err := chownSQLiteFiles(path, os.Geteuid()+1); err == nil {
			t.Fatal("an ownership change that failed was ignored")
		}
	}
}

// A retry after a rollback that finished finds the journal it left. The snapshot was restored then,
// so the report says so rather than "not-needed", and the gate's reason stays a rollback.
func TestRollbackSecurityImageReportsAnEarlierRestoreOnARetry(t *testing.T) {
	f, _ := rollbackDriver(t, "preparing", false, false)
	cfg := &config.Config{AutoMigrate: true}
	cfg.Upgrade.Unattended = true
	cfg.Upgrade.StateDirectory = t.TempDir()
	directory := filepath.Join(cfg.Upgrade.StateDirectory, "unattended")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	journal, err := readAutomaticJournal(filepath.Join(f.host.configuration.StateDirectory, "operation.json"))
	if err != nil || journal == nil {
		t.Fatal(journal, err)
	}
	journal.Format = containerJournalFormat
	for aborted, want := range map[string]string{rolledBackAborted: "restored", "": "not-needed"} {
		journal.Aborted = aborted
		if err := writeAutomaticJournal(filepath.Join(directory, "operation.json"), journal); err != nil {
			t.Fatal(err)
		}
		report, err := rollbackSecurityImage(t.Context(), cfg, f.target.Identity.ManifestSHA256)
		if err != nil || report.Result != want {
			t.Fatalf("aborted=%q: report=%+v err=%v, want %q", aborted, report, err, want)
		}
	}
	// A manifest other than the journaled one never reads as restored.
	journal.Aborted = rolledBackAborted
	if err := writeAutomaticJournal(filepath.Join(directory, "operation.json"), journal); err != nil {
		t.Fatal(err)
	}
	if report, err := rollbackSecurityImage(t.Context(), cfg, releaseidentity.Digest(strings.Repeat("e", 64))); err == nil || report.Result == "restored" {
		t.Fatalf("a different manifest was reported restored: %+v %v", report, err)
	}
}

var _ nativePreflighter = (*hostupgrade.Host)(nil)

type recordingPreflighter struct{ calls []string }

func (r *recordingPreflighter) Preflight(context.Context) error {
	r.calls = append(r.calls, "strict")
	return nil
}
func (r *recordingPreflighter) PreflightNewInstallation(context.Context) error {
	r.calls = append(r.calls, "absent-allowed")
	return nil
}

// A rollback killed after it removed the database resumes from its journal, so only that journal, read
// intact, lets the SQLite file be missing.
func TestPreflightAllowsAMissingDatabaseOnlyForARollingBackJournal(t *testing.T) {
	for name, test := range map[string]struct {
		phase   string
		corrupt bool
		want    string
	}{
		"no journal":   {want: "strict"},
		"preparing":    {phase: "preparing", want: "strict"},
		"schema":       {phase: "schema-applied", want: "strict"},
		"rolling-back": {phase: "rolling-back", want: "absent-allowed"},
		"malformed":    {phase: "rolling-back", corrupt: true, want: "strict"},
	} {
		t.Run(name, func(t *testing.T) {
			f, directory := rollbackDriver(t, test.phase, false, true)
			if test.corrupt {
				if err := os.WriteFile(filepath.Join(directory, "operation.json"), []byte(`{"phase":"rolling-back"`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var host recordingPreflighter
			if err := preflightNativeHost(t.Context(), &host, f.host.configuration.StateDirectory); err != nil || len(host.calls) != 1 || host.calls[0] != test.want {
				t.Fatalf("calls=%v err=%v, want %s", host.calls, err, test.want)
			}
		})
	}
}

// A guarded update whose coordinator died before the stop, or a rollback that restarted the source and
// died before clearing its journal, leaves a "preparing" journal under a serving source. Reconcile
// releases both; only the second is a rollback.
func TestNativeReconcileReleasesAServingSourceUnderAPreparingJournal(t *testing.T) {
	for name, test := range map[string]struct {
		aborted     string
		maintenance bool
		stopped     bool
		phase       string
		record      bool
		want        error
		released    bool
	}{
		"died before the stop":                 {record: true, phase: "preparing", released: true},
		"stopped since the observation":        {record: true, phase: "preparing", stopped: true},
		"died after the rollback restarted it": {record: true, phase: "preparing", aborted: rolledBackAborted, released: true, want: securitymaintenance.ErrRolledBack},
		"stopped under the fence":              {record: true, phase: "preparing", maintenance: true},
		"no source record":                     {phase: "preparing"},
		"past preparation":                     {record: true, phase: "schema-applied"},
		"put back by hand under rolling-back":  {record: true, phase: "rolling-back", released: true, want: securitymaintenance.ErrRolledBack},
		"rolling back and still fenced":        {record: true, phase: "rolling-back", maintenance: true},
	} {
		t.Run(name, func(t *testing.T) {
			f, directory := rollbackDriver(t, test.phase, test.record, test.maintenance)
			path := filepath.Join(directory, "operation.json")
			journal, err := readAutomaticJournal(path)
			if err != nil || journal == nil {
				t.Fatal(journal, err)
			}
			journal.Aborted = test.aborted
			if err := writeAutomaticJournal(path, journal); err != nil {
				t.Fatal(err)
			}
			if test.stopped {
				f.host.failure = errors.New("the source is not running")
			}
			err = f.driver.Reconcile(t.Context(), securitymaintenance.Observation{})
			if test.released != (err == nil || errors.Is(err, securitymaintenance.ErrRolledBack)) || test.want != nil && !errors.Is(err, test.want) || test.want == nil && test.released && err != nil {
				t.Fatalf("err=%v", err)
			}
			_, statErr := os.Stat(filepath.Join(directory, "operation-source.json"))
			// A release after a rollback keeps the record: the coordinator can still die before it records that.
			kept := !test.released || test.want != nil
			if left, readErr := readAutomaticJournal(path); readErr != nil || test.released == (left != nil) || test.record && kept == errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("journal=%+v record=%v: a released pin must clear the journal, and the record unless it rolled back", left, statErr)
			}
		})
	}
}

// rolledBackRecordOnly is the host after a rollback finished and removed its journal, with the coordinator
// killed before it saved anything.
func rolledBackRecordOnly(t *testing.T) (*nativeDriverFixture, string) {
	t.Helper()
	f, directory := rollbackDriver(t, "preparing", true, false)
	record, err := readNativeSourceRecord(directory)
	if err != nil || record == nil {
		t.Fatal(record, err)
	}
	record.RolledBack = true
	if err := writeNativeSourceRecord(directory, *record); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, "operation.json")); err != nil {
		t.Fatal(err)
	}
	return f, directory
}

func TestNativeReleaseCountsARollbackWhoseCoordinatorDiedAfterTheJournalCleared(t *testing.T) {
	f, directory := rolledBackRecordOnly(t)
	for range 2 {
		if err := f.driver.Reconcile(t.Context(), securitymaintenance.Observation{}); !errors.Is(err, securitymaintenance.ErrRolledBack) {
			t.Fatalf("the release lost the rollback: %v", err)
		}
	}
	err := f.driver.RestoreAborted(t.Context(), securitymaintenance.Observation{}, pinned(f, observeSource(t, f)))
	if !errors.Is(err, securitymaintenance.ErrRolledBack) || !errors.Is(err, securitymaintenance.ErrNotStarted) {
		t.Fatalf("the restore called the source untouched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "operation-source.json")); err != nil {
		t.Fatalf("the record went before the coordinator could save the rollback: %v", err)
	}
}

func TestNativePrepareForgetsAnEarlierRollback(t *testing.T) {
	f, directory := rolledBackRecordOnly(t)
	if _, err := f.driver.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "operation-source.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a new attempt could report the old rollback: %v", err)
	}
	if err := f.driver.Reconcile(t.Context(), securitymaintenance.Observation{}); err != nil {
		t.Fatal(err)
	}
}

func TestSettlingKeepsOnlyARecordThatRolledBack(t *testing.T) {
	for _, rolledBack := range []bool{false, true} {
		_, directory := rollbackDriver(t, "preparing", true, false)
		record, err := readNativeSourceRecord(directory)
		if err != nil || record == nil {
			t.Fatal(record, err)
		}
		record.RolledBack = rolledBack
		if err := writeNativeSourceRecord(directory, *record); err != nil {
			t.Fatal(err)
		}
		if err := settleNativeSourceRecord(directory); err != nil {
			t.Fatal(err)
		}
		if left, err := readNativeSourceRecord(directory); err != nil || (left != nil) != rolledBack {
			t.Fatalf("rolledBack=%t left=%+v err=%v", rolledBack, left, err)
		}
	}
}

func TestClearingAnAttemptKeepsTheRecordWhileTheJournalStays(t *testing.T) {
	_, directory := rollbackDriver(t, "preparing", true, false)
	unwritable := filepath.Join(directory, "missing", "operation.json")
	if err := clearNativeAttempt(unwritable, &automaticJournal{Phase: "complete"}, directory); err == nil {
		t.Fatal("a journal that could not be written was reported as cleared")
	}
	if record, err := readNativeSourceRecord(directory); err != nil || record == nil {
		t.Fatalf("the record went before its journal: %+v %v", record, err)
	}
}

func TestUpgradeAtARollingBackJournalOnlyFetchesTheSourceRelease(t *testing.T) {
	f, directory := rollbackDriver(t, "rolling-back", true, true)
	source := f.inspection.state.Applied.Release
	var fetched []releaseidentity.Identity
	fetch := func(_ context.Context, identity releaseidentity.Identity) error {
		fetched = append(fetched, identity)
		return nil
	}
	for name, expected := range map[string]*releaseidentity.Identity{"no identity": nil, "the target": &f.target.Identity} {
		err := refetchNativeRollbackSource(t.Context(), directory, expected, fetch, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "next coordinator run") || !strings.Contains(err.Error(), "--release-identity") || len(fetched) != 0 {
			t.Fatalf("%s: err=%v fetched=%v", name, err, fetched)
		}
	}
	if err := refetchNativeRollbackSource(t.Context(), directory, &source, fetch, io.Discard); err != nil || len(fetched) != 1 || fetched[0] != source {
		t.Fatalf("err=%v fetched=%v", err, fetched)
	}
	if journal, err := readAutomaticJournal(filepath.Join(directory, "operation.json")); err != nil || journal == nil || journal.Phase != "rolling-back" {
		t.Fatalf("fetching changed the journal: %+v %v", journal, err)
	}
	if err := os.Remove(filepath.Join(directory, "operation-source.json")); err != nil {
		t.Fatal(err)
	}
	if err := refetchNativeRollbackSource(t.Context(), directory, &source, fetch, io.Discard); err == nil || len(fetched) != 1 {
		t.Fatalf("err=%v fetched=%v", err, fetched)
	}
}
