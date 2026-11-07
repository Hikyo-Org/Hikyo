package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// nativeJournaledRollback is journaledRollback for the host-fenced route: a host
// journal, the native basis and a hook standing for the source reinstall.
func (f *rollbackFixture) nativeJournaledRollback(t *testing.T, phase string, hook func(directory string) error, mutate func(*automaticJournal)) (restored bool, directory string, err error) {
	t.Helper()
	directory = t.TempDir()
	evidence := filepath.Join(directory, "evidence")
	if err := os.Mkdir(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"receipt.json", "attestation.json", "attestation.sigstore.json"} {
		if err := os.WriteFile(filepath.Join(evidence, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := f.plan.SourceManifest(f.cfg.Engine)
	if err != nil {
		t.Fatal(err)
	}
	journal := &automaticJournal{Format: hostJournalFormat, Phase: phase, Target: f.plan.Target(), Instance: f.basis.Instance, Route: f.plan.Digest(), Hop: 1,
		Source:  upgradecompat.InstalledSource{Identity: f.plan.Source(), Migrations: manifest, SchemaSHA256: f.plan.SourceSchemaDigest()},
		Runtime: hostupgrade.RuntimeEvidence{EvidenceDirectory: evidence, CiphertextPath: filepath.Join(directory, "backup.age"), BundleDirectory: directory}}
	if mutate != nil {
		mutate(journal)
	}
	path := filepath.Join(directory, "operation.json")
	if err := writeAutomaticJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	basis := f.basis
	basis.Rolling, basis.Database = phase == "rolling-back", journal.RollbackDatabase
	cfg := &config.Config{Store: config.Datastore{Engine: config.Engine(f.cfg.Engine), Path: f.cfg.Path}}
	var release func(context.Context) error
	if hook != nil {
		release = func(context.Context) error { return hook(directory) }
	}
	restored, err = rollbackJournaled(t.Context(), cfg, f.cfg, path, journal, f.archive, f.plan, basis, release)
	return restored, directory, err
}

func nativeFixture(t *testing.T) *rollbackFixture {
	t.Helper()
	return newRollbackFixtureFor(t, store.EngineSQLite, true)
}

func TestNativeRollbackRestoresTheHealthySourceAndNothingElse(t *testing.T) {
	f := nativeFixture(t)
	f.moveOn(t, nil)
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(f.cfg.Path+suffix, []byte("stale"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := f.frozen
	if needed, err := f.rollback(t.Context(), f.basis); err != nil || !needed {
		t.Fatalf("needed=%t err=%v", needed, err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(f.cfg.Path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale %s survived: %v", suffix, err)
		}
	}
	after, err := upgrade.InspectControl(t.Context(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	// The restored row is the source itself, healthy, with the same instance, incarnation and epoch.
	if after.Generation != before.Generation || after.RestoreEpoch != before.RestoreEpoch || after.RecoveryIncarnation != before.RecoveryIncarnation || after.InstanceID != before.InstanceID ||
		after.Pending.Phase != upgrade.Healthy || after.Maintenance || after.Pending.Invalidated || after.Applied != before.Applied {
		t.Fatalf("restored row is not the healthy source: %+v", after)
	}
	if ids := orgIDs(t, f.cfg); ids != "org_drill," {
		t.Fatalf("data after rollback = %q", ids)
	}
	if needed, err := f.rollback(t.Context(), f.basis); err != nil || needed {
		t.Fatalf("a restored source asked for another restore: needed=%t err=%v", needed, err)
	}
}

func TestNativeRollbackRefusesAnythingButTheSnapshotsOwnRows(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*rollbackFixture, *upgrade.State)
		basis  func(*upgrade.RollbackBasis)
	}{
		{name: "row of another backup", basis: func(b *upgrade.RollbackBasis) { b.BackupID = string(releaseidentity.Hash([]byte("another backup"))) }},
		{name: "past the final gate: healthy at the target", mutate: func(f *rollbackFixture, s *upgrade.State) {
			s.Maintenance, s.Pending.Phase, s.Applied = false, upgrade.Healthy, upgrade.Source{Release: s.Pending.Target}
		}},
		{name: "another instance", basis: func(b *upgrade.RollbackBasis) { b.Instance = "another-instance" }},
		{name: "another restore epoch", basis: func(b *upgrade.RollbackBasis) { b.RestoreEpoch++ }},
		{name: "another incarnation", basis: func(b *upgrade.RollbackBasis) { b.RecoveryIncarnation = "00" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := nativeFixture(t)
			f.moveOn(t, func(s *upgrade.State) {
				if test.mutate != nil {
					test.mutate(f, s)
				}
			})
			basis := f.basis
			if test.basis != nil {
				test.basis(&basis)
			}
			if needed, err := f.rollback(t.Context(), basis); err == nil || needed {
				t.Fatalf("rolled back: needed=%t err=%v", needed, err)
			}
			if ids := orgIDs(t, f.cfg); ids != "org_after,org_drill," {
				t.Fatalf("a refusal replaced the database: %q", ids)
			}
		})
	}
}

// The restored row must be the deployment kind's own source: a native archive is not
// published under the containers' rule, and a container fence is not published under native's.
func TestRollbackRestoredRowIsCheckedByDeploymentKind(t *testing.T) {
	native := nativeFixture(t)
	native.moveOn(t, nil)
	asContainer := native.basis
	asContainer.HostFenced = false
	if _, err := native.rollback(t.Context(), asContainer); err == nil {
		t.Fatal("a healthy native snapshot was published as a container fence")
	}
	if ids := orgIDs(t, native.cfg); ids != "org_after,org_drill," {
		t.Fatalf("the refusal replaced the database: %q", ids)
	}
	container := newRollbackFixture(t, store.EngineSQLite)
	container.moveOn(t, nil)
	asNative := container.basis
	asNative.HostFenced = true
	if _, err := container.rollback(t.Context(), asNative); err == nil {
		t.Fatal("a container fence was published as a native source")
	}
	if ids := orgIDs(t, container.cfg); ids != "org_after,org_drill," {
		t.Fatalf("the refusal replaced the database: %q", ids)
	}
}

func TestNativeRollbackRunsTheHookBeforeTheSourceAcceptsTheJournal(t *testing.T) {
	f := nativeFixture(t)
	f.moveOn(t, nil)
	var phaseDuringHook string
	var stateDuringHook upgrade.State
	hook := func(directory string) error {
		journal, err := readAutomaticJournal(filepath.Join(directory, "operation.json"))
		if err != nil {
			return err
		}
		phaseDuringHook = journal.Phase
		stateDuringHook, err = upgrade.InspectControl(t.Context(), f.cfg)
		return err
	}
	restored, directory, err := f.nativeJournaledRollback(t, "schema-applied", hook, nil)
	if err != nil || !restored {
		t.Fatalf("restored=%t err=%v", restored, err)
	}
	// The source binary is reinstalled while the journal still refuses the source and the row is already restored.
	if phaseDuringHook != "rolling-back" || stateDuringHook.Pending.Phase != upgrade.Healthy || stateDuringHook.Maintenance {
		t.Fatalf("hook saw journal %q row %+v", phaseDuringHook, stateDuringHook)
	}
	written, err := readAutomaticJournal(filepath.Join(directory, "operation.json"))
	state, stateErr := upgrade.InspectControl(t.Context(), f.cfg)
	if err != nil || stateErr != nil || written.Phase != "preparing" || written.Aborted == "" || written.RollbackDatabase != "" || !written.preparationNeverWrote(state, nil) {
		t.Fatalf("journal after rollback: %+v err=%v %v", written, err, stateErr)
	}
	if _, err := os.Stat(filepath.Join(directory, "evidence", "attestation.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the spent attestation survived: %v", err)
	}
}

// A crash at "rolling-back", or a hook that fails, repeats the whole rollback from the durable state.
func TestNativeRollbackResumesFromRollingBack(t *testing.T) {
	f := nativeFixture(t)
	f.moveOn(t, nil)
	var identity string
	if err := upgrade.WithRollbackLock(t.Context(), f.cfg, func(rb *upgrade.Rollback) (err error) {
		identity, err = rb.Identity(t.Context())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("source binary cannot be installed")
	restored, directory, err := f.nativeJournaledRollback(t, "write-intent", func(string) error { return failure }, nil)
	if !errors.Is(err, failure) || restored {
		t.Fatalf("a failed hook finished the rollback: restored=%t err=%v", restored, err)
	}
	stuck, readErr := readAutomaticJournal(filepath.Join(directory, "operation.json"))
	if readErr != nil || stuck.Phase != "rolling-back" {
		t.Fatalf("the journal left rolling-back before the source was installed: %+v %v", stuck, readErr)
	}
	// The database was already published, so the resume sees an untouched row and must still reinstall.
	calls := 0
	restored, directory, err = f.nativeJournaledRollback(t, "rolling-back", func(string) error { calls++; return nil }, func(j *automaticJournal) { j.RollbackDatabase = identity })
	if err != nil || !restored || calls != 1 {
		t.Fatalf("resume: restored=%t calls=%d err=%v", restored, calls, err)
	}
	if written, err := readAutomaticJournal(filepath.Join(directory, "operation.json")); err != nil || written.Phase != "preparing" {
		t.Fatalf("journal after the resume: %+v %v", written, err)
	}
	if ids := orgIDs(t, f.cfg); ids != "org_drill," {
		t.Fatalf("data after the resume = %q", ids)
	}
}

// A coordinator killed after "write-intent" and before the first migration step leaves the
// untouched source under a journal past "preparing". Both owners release it.
func TestNativeRollbackReleasesAnUntouchedSourceUnderALaggingJournal(t *testing.T) {
	f := nativeFixture(t)
	calls := 0
	restored, directory, err := f.nativeJournaledRollback(t, "write-intent", func(string) error { calls++; return nil }, nil)
	// Nothing was restored (the row is untouched), but the lagging journal is still released.
	if err != nil || restored || calls != 1 {
		t.Fatalf("restored=%t calls=%d err=%v", restored, calls, err)
	}
	written, err := readAutomaticJournal(filepath.Join(directory, "operation.json"))
	state, stateErr := upgrade.InspectControl(t.Context(), f.cfg)
	if err != nil || stateErr != nil || !written.preparationNeverWrote(state, nil) {
		t.Fatalf("the lagging journal was not released: %+v %v %v", written, err, stateErr)
	}
	if ids := orgIDs(t, f.cfg); ids != "org_drill," {
		t.Fatalf("data = %q", ids)
	}
	// The container route releases it the same way, and its source accepts the result.
	container := newRollbackFixture(t, store.EngineSQLite)
	restored, input, _, err := container.journaledRollback(t, "write-intent", nil)
	journal, readErr := readAutomaticJournal(filepath.Join(input.Runtime.BundleDirectory, "operation.json"))
	state, stateErr = upgrade.InspectControl(t.Context(), container.cfg)
	if err != nil || readErr != nil || restored || journal.Phase != "preparing" || journal.Aborted == "" || !journal.preparationNeverWrote(state, stateErr) {
		t.Fatalf("container: restored=%t journal=%+v err=%v %v state=%v", restored, journal, err, readErr, stateErr)
	}
}

// Native uses the floor the live row retains, like containers: a read that is not "absent"
// is an error, never a silent zero floor.
func TestLiveSnapshotFloorUsesTheRowAndZeroOnlyWhenAbsent(t *testing.T) {
	f := nativeFixture(t)
	state, err := upgrade.InspectControl(t.Context(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if floor, err := liveSnapshotFloor(t.Context(), f.cfg); err != nil || floor != state.Floor {
		t.Fatalf("floor = %+v %v, want the row's %+v", floor, err, state.Floor)
	}
	absent := f.cfg
	absent.Path = filepath.Join(t.TempDir(), "absent.db")
	if floor, err := liveSnapshotFloor(t.Context(), absent); err != nil || floor != (releaseidentity.SnapshotFloor{}) {
		t.Fatalf("absent row: %+v %v", floor, err)
	}
	broken := f.cfg
	broken.Path = filepath.Join(t.TempDir(), "broken.db")
	if err := os.WriteFile(broken.Path, []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := liveSnapshotFloor(t.Context(), broken); err == nil {
		t.Fatal("an unreadable row produced a floor")
	}
}
