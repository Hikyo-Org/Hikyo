//go:build darwin || linux

package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
	"github.com/gofrs/flock"
)

const enrollmentInstance = "ins_0123456789abcdef0123456789abcdef"

// enrollmentFixture is a native host whose service has not booted. Its files are
// real, and only the privileged operations (systemd, the signed download) are fakes.
type enrollmentFixture struct {
	t           *testing.T
	c           hostupgrade.Config
	environ     map[string]string
	root        []byte
	booted      bool
	healthy     bool
	phase       upgrade.Phase
	generation  int64
	legacy      bool
	directories int
	boots       []bool
	downloads   int
	failBundle  error
	failBoot    error
	failRelease error
}

func newEnrollmentFixture(t *testing.T) *enrollmentFixture {
	t.Helper()
	// The custody walk refuses symbolic links, and macOS temporary directories have one.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := hostupgrade.DefaultConfig()
	c.CustodyDirectory, c.PublicDirectory, c.StateDirectory = filepath.Join(base, "keys"), filepath.Join(base, "public"), filepath.Join(base, "state")
	c.WorkingDirectory, c.RootKeyFile, c.Binary = filepath.Join(base, "work"), filepath.Join(base, "root.key"), filepath.Join(base, "hikyo")
	for _, directory := range []string{c.PublicDirectory, c.StateDirectory} {
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	root := bytes.Repeat([]byte{7}, 32)
	if err := os.WriteFile(c.Binary, []byte("installed executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &enrollmentFixture{t: t, c: c, environ: map[string]string{"HIKYO_DB": "sqlite:hikyo.db"}, root: root}
}

func (f *enrollmentFixture) Config() hostupgrade.Config                     { return f.c }
func (f *enrollmentFixture) Environment() map[string]string                 { return f.environ }
func (f *enrollmentFixture) PreflightNewInstallation(context.Context) error { return nil }
func (f *enrollmentFixture) PrepareDirectories() error {
	f.directories++
	return os.MkdirAll(f.c.CustodyDirectory, 0o700)
}
func (f *enrollmentFixture) PublishPublicEvidence(name string, raw []byte) (string, error) {
	path := filepath.Join(f.c.PublicDirectory, name)
	return path, securefile.WriteAtomic(path, raw, 0o644)
}
func (f *enrollmentFixture) StagePublicBundle(source string) (string, error) {
	if f.failBundle != nil {
		return "", f.failBundle
	}
	destination := filepath.Join(f.c.PublicDirectory, "bundle-staged")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return "", err
	}
	return destination, os.WriteFile(filepath.Join(destination, "index.json"), []byte(source), 0o644)
}
func (f *enrollmentFixture) ConfigureRuntime(_ context.Context, e hostupgrade.RuntimeEvidence) error {
	values := map[string]string{"HIKYO_UPGRADE_BUNDLE": e.BundleDirectory, "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY": e.OperatorPublicKey, "HIKYO_UPGRADE_TARGET_MANIFEST": e.TargetManifest}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var env strings.Builder
	for _, key := range keys {
		env.WriteString(key + "=" + values[key] + "\n")
	}
	return securefile.WriteAtomic(filepath.Join(f.c.StateDirectory, "runtime.env"), []byte(env.String()), 0o600)
}
func (f *enrollmentFixture) PrunePublic(hostupgrade.RuntimeEvidence) error { return nil }
func (f *enrollmentFixture) BootNewInstallation(_ context.Context, digest string, migrate bool, _ time.Duration, _, _ io.Writer) error {
	if f.failBoot != nil {
		return f.failBoot
	}
	if digest == "" {
		f.t.Fatal("boot without the authenticated binary digest")
	}
	f.boots = append(f.boots, migrate)
	f.booted, f.healthy = true, true
	return nil
}

func (f *enrollmentFixture) enrollment() nativeEnrollment {
	return nativeEnrollment{Host: f, Out: io.Discard,
		Inspect: func(context.Context, upgrade.Config) (upgrade.State, error) {
			if !f.booted {
				return upgrade.State{}, upgrade.ErrAbsent
			}
			phase := upgrade.Healthy
			if !f.healthy {
				phase = upgrade.SchemaApplied
			}
			if f.phase != "" {
				phase = f.phase
			}
			generation := int64(1)
			if f.generation != 0 {
				generation = f.generation
			}
			fresh := nativeSecurityIdentity(releaseidentity.StableV1)
			return upgrade.State{InstanceID: enrollmentInstance, Generation: generation, Pending: &upgrade.Operation{Phase: phase, Target: fresh, RouteSource: releaseidentity.Source{Genesis: releaseidentity.FreshGenesisV1}}}, nil
		},
		Fresh: func(context.Context, upgrade.Config) error {
			if f.legacy {
				return errors.New("legacy installation")
			}
			return nil
		},
		RootKey: func(string) ([]byte, error) { return bytes.Clone(f.root), nil },
		Release: func(context.Context, hostupgrade.Config) (nativeInstalledRelease, error) {
			f.downloads++
			if f.failRelease != nil {
				return nativeInstalledRelease{}, f.failRelease
			}
			return nativeInstalledRelease{Identity: nativeSecurityIdentity(releaseidentity.StableV1), BundleDirectory: "private-bundle", BinarySHA256: strings.Repeat("a", 64)}, nil
		}}
}

func (f *enrollmentFixture) snapshot() map[string]string {
	result := map[string]string{}
	for _, directory := range []string{f.c.CustodyDirectory, f.c.PublicDirectory, f.c.StateDirectory} {
		_ = filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.Type().IsRegular() {
				raw, _ := os.ReadFile(path)
				info, _ := entry.Info()
				result[path] = info.Mode().String() + string(raw)
			}
			return nil
		})
	}
	return result
}

func requireMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != want || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("%s: mode %v, error %v, want %v", path, info, err, want)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() {
		t.Fatalf("%s is not owned by the enrolling user", path)
	}
}

func TestNativeEnrollmentFreshHostCreatesBoundVaultAndPin(t *testing.T) {
	f := newEnrollmentFixture(t)
	if err := f.enrollment().Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	published := filepath.Join(f.c.PublicDirectory, "operator.pub")
	requireMode(t, f.c.CustodyDirectory, 0o700)
	requireMode(t, filepath.Join(f.c.CustodyDirectory, "operator.age"), 0o600)
	requireMode(t, published, 0o644)
	requireMode(t, filepath.Join(f.c.StateDirectory, "runtime.env"), 0o600)
	pin, err := nativeConfiguredOperatorPin(f.c, f.environ)
	if err != nil || pin != published {
		t.Fatal("the pin is not the published operator key", pin, err)
	}
	runtimeEnv, _ := os.ReadFile(filepath.Join(f.c.StateDirectory, "runtime.env"))
	if !strings.Contains(string(runtimeEnv), "HIKYO_UPGRADE_BUNDLE="+filepath.Join(f.c.PublicDirectory, "bundle-staged")) {
		t.Fatal("runtime environment does not name the staged bundle", string(runtimeEnv))
	}
	if len(f.boots) != 1 || !f.boots[0] {
		t.Fatal("the new installation was not migrated and started exactly once", f.boots)
	}
	if err := checkNativeSecurityCustody(f.c, f.environ); err != nil {
		t.Fatal("an enrolled fresh host fails the custody check", err)
	}
	// The check never opens the vault, so prove the coordinator can: a vault left on
	// the sentinel instance would stop the first update at its own unlock.
	vault, err := upgradecustody.OpenLocal(f.c.CustodyDirectory, f.root, enrollmentInstance)
	if err != nil {
		t.Fatal("vault is not bound to the booted instance", err)
	}
	defer vault.Close()
	if public, _ := os.ReadFile(published); !bytes.Equal(public, vault.PublicKey()) {
		t.Fatal("published operator key is not the vault's")
	}
	other, err := upgradecustody.OpenLocal(f.c.CustodyDirectory, f.root, "ins_00000000000000000000000000000000")
	if err == nil {
		other.Close()
		t.Fatal("vault still opens on the unbound sentinel instance")
	}
}

func TestNativeEnrollmentKeepsHostWithExternalOperatorKeyRefused(t *testing.T) {
	for name, booted := range map[string]bool{"before boot": false, "after boot": true} {
		t.Run(name, func(t *testing.T) {
			f := newEnrollmentFixture(t)
			f.booted, f.healthy = booted, true
			external := filepath.Join(t.TempDir(), "manual-operator.pub")
			if err := os.WriteFile(external, []byte("offline operator key"), 0o644); err != nil {
				t.Fatal(err)
			}
			f.environ["HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY"] = external
			if err := f.enrollment().Prepare(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(f.snapshot()) != 0 || f.downloads != 0 || len(f.boots) != 0 {
				t.Fatal("an externally pinned host was changed", f.snapshot())
			}
			if err := checkNativeSecurityCustody(f.c, f.environ); err == nil || !strings.Contains(err.Error(), "Operator-key rotation") {
				t.Fatal("externally pinned host is not refused with its remedy", err)
			}
		})
	}
}

func TestNativeEnrollmentLeavesBootedHostWithoutOurVaultRefused(t *testing.T) {
	f := newEnrollmentFixture(t)
	f.booted, f.healthy = true, true
	if err := f.enrollment().Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.snapshot()) != 0 || len(f.boots) != 0 {
		t.Fatal("a booted host with no vault of ours was changed")
	}
	if err := checkNativeSecurityCustody(f.c, f.environ); err == nil {
		t.Fatal("booted host without a vault admitted")
	}
}

func TestNativeEnrollmentIsIdempotent(t *testing.T) {
	f := newEnrollmentFixture(t)
	if err := f.enrollment().Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	first := f.snapshot()
	if err := f.enrollment().Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	second := f.snapshot()
	// The vault is rewritten when bound, so compare what a rerun must keep.
	for path, contents := range first {
		if strings.HasSuffix(path, "operator.age") {
			continue
		}
		if second[path] != contents {
			t.Fatal("rerun changed", path)
		}
	}
	if f.downloads != 1 || len(f.boots) != 1 {
		t.Fatal("rerun downloaded or booted again", f.downloads, f.boots)
	}
	vault, err := upgradecustody.OpenLocal(f.c.CustodyDirectory, f.root, enrollmentInstance)
	if err != nil {
		t.Fatal(err)
	}
	vault.Close()
	if err := checkNativeSecurityCustody(f.c, f.environ); err != nil {
		t.Fatal(err)
	}
}

func TestNativeEnrollmentFailureBeforeThePinLeavesNoPin(t *testing.T) {
	for name, inject := range map[string]func(*enrollmentFixture, error){
		"download":       func(f *enrollmentFixture, err error) { f.failRelease = err },
		"bundle staging": func(f *enrollmentFixture, err error) { f.failBundle = err },
	} {
		t.Run(name, func(t *testing.T) {
			f := newEnrollmentFixture(t)
			inject(f, errors.New("injected failure"))
			if err := f.enrollment().Prepare(t.Context()); err == nil || !strings.Contains(err.Error(), "injected failure") {
				t.Fatal("failure not reported", err)
			}
			if _, err := os.Lstat(filepath.Join(f.c.StateDirectory, "runtime.env")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("a pin was written before its evidence existed", err)
			}
			if pin, _ := nativeConfiguredOperatorPin(f.c, f.environ); pin != "" || len(f.boots) != 0 {
				t.Fatal("partial pin or boot", pin, f.boots)
			}
			vault, _ := os.ReadFile(filepath.Join(f.c.CustodyDirectory, "operator.age"))
			public, _ := os.ReadFile(filepath.Join(f.c.PublicDirectory, "operator.pub"))
			f.failRelease, f.failBundle = nil, nil
			if err := f.enrollment().Prepare(t.Context()); err != nil {
				t.Fatal("rerun after failure", err)
			}
			// The retry keeps the key the failed attempt created, not a second one.
			if after, _ := os.ReadFile(filepath.Join(f.c.PublicDirectory, "operator.pub")); !bytes.Equal(after, public) || len(vault) == 0 {
				t.Fatal("rerun replaced the operator key")
			}
			if err := checkNativeSecurityCustody(f.c, f.environ); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeEnrollmentResumesAfterFailedFirstBoot(t *testing.T) {
	f := newEnrollmentFixture(t)
	f.failBoot = errors.New("service refused to start")
	if err := f.enrollment().Prepare(t.Context()); err == nil || !strings.Contains(err.Error(), "refused to start") {
		t.Fatal("boot failure not reported", err)
	}
	// The pin is whole, so a rerun resumes at the boot instead of starting over.
	if pin, _ := nativeConfiguredOperatorPin(f.c, f.environ); pin != filepath.Join(f.c.PublicDirectory, "operator.pub") {
		t.Fatal("pin missing after the boot step", pin)
	}
	f.failBoot = nil
	if err := f.enrollment().Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := checkNativeSecurityCustody(f.c, f.environ); err != nil {
		t.Fatal(err)
	}
	if vault, err := upgradecustody.OpenLocal(f.c.CustodyDirectory, f.root, enrollmentInstance); err != nil {
		t.Fatal(err)
	} else {
		vault.Close()
	}
}

func TestNativeEnrollmentStartsServiceLeftUnhealthyByAnInterruptedBoot(t *testing.T) {
	f := newEnrollmentFixture(t)
	if err := f.enrollment().Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.healthy = false
	if err := f.enrollment().Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.boots) != 2 || f.boots[1] {
		t.Fatal("an unhealthy booted installation must be started without migrating again", f.boots)
	}
}

func TestNativeEnrollmentRefusesVaultBoundToAnotherInstance(t *testing.T) {
	f := newEnrollmentFixture(t)
	if err := f.enrollment().Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	// A restored database keeps its own identity, so the vault must not follow it.
	rebound := f.enrollment()
	inspect := rebound.Inspect
	rebound.Inspect = func(ctx context.Context, database upgrade.Config) (upgrade.State, error) {
		state, err := inspect(ctx, database)
		state.InstanceID = "ins_fedcba9876543210fedcba9876543210"
		return state, err
	}
	if err := rebound.Prepare(t.Context()); err == nil || !strings.Contains(err.Error(), "does not open for this installation") {
		t.Fatal("vault bound to another instance admitted", err)
	}
}

func TestNativeEnrollmentResumesAnInterruptedMigrate(t *testing.T) {
	for _, phase := range []upgrade.Phase{upgrade.Prepared, upgrade.SchemaWriteStarted} {
		t.Run(string(phase), func(t *testing.T) {
			f := newEnrollmentFixture(t)
			if err := f.enrollment().Prepare(t.Context()); err != nil {
				t.Fatal(err)
			}
			// Only migrate advances these phases, and the service refuses to boot while they remain.
			f.healthy, f.phase = false, phase
			if err := f.enrollment().Prepare(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(f.boots) != 2 || !f.boots[1] {
				t.Fatal("an interrupted migrate was not finished by the rerun", f.boots)
			}
		})
	}
}

func TestNativeEnrollmentRefusesALegacyHostWithoutChangingIt(t *testing.T) {
	f := newEnrollmentFixture(t)
	f.legacy = true
	if err := f.enrollment().Prepare(t.Context()); err == nil || !strings.Contains(err.Error(), "legacy installation") {
		t.Fatal("a populated database without control objects was enrolled as new", err)
	}
	if len(f.snapshot()) != 0 || f.directories != 0 || f.downloads != 0 || len(f.boots) != 0 {
		t.Fatal("a refused host was changed", f.snapshot(), f.directories, f.downloads, f.boots)
	}
	if _, err := os.Lstat(f.c.CustodyDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused host gained a custody directory", err)
	}
}

func TestNativeEnrollmentDoesNotAdoptAnExistingInstanceIntoAnUnboundVault(t *testing.T) {
	f := newEnrollmentFixture(t)
	f.failBoot = errors.New("service refused to start")
	if err := f.enrollment().Prepare(t.Context()); err == nil {
		t.Fatal("boot failure not reported")
	}
	// The vault is unbound and pinned; a database that is not this installation's first
	// operation must not be able to take it over.
	f.failBoot, f.booted, f.healthy, f.generation = nil, true, true, 2
	before := f.snapshot()
	if err := f.enrollment().Prepare(t.Context()); err == nil || !strings.Contains(err.Error(), "not the one enrolled") {
		t.Fatal("an existing instance adopted the unbound vault", err)
	}
	for path, contents := range f.snapshot() {
		if before[path] != contents {
			t.Fatal("a refused adoption changed", path)
		}
	}
	vault, err := upgradecustody.OpenLocal(f.c.CustodyDirectory, f.root, "ins_00000000000000000000000000000000")
	if err != nil {
		t.Fatal("the vault left the unbound enrollment record", err)
	}
	vault.Close()
}

func TestNativeEnrollmentRefusesWhileAnotherOperatorUpgrades(t *testing.T) {
	f := newEnrollmentFixture(t)
	held := flock.New(filepath.Join(f.c.StateDirectory, "upgrade.lock"))
	if locked, err := held.TryLock(); err != nil || !locked {
		t.Fatal(locked, err)
	}
	defer held.Unlock()
	if err := f.enrollment().Prepare(t.Context()); err == nil || !strings.Contains(err.Error(), "already upgrading") {
		t.Fatal("enrolled beside a running upgrade", err)
	}
	if len(f.boots) != 0 || f.downloads != 0 {
		t.Fatal("a refused enrollment continued", f.boots, f.downloads)
	}
}

func TestNativeEnrollmentLeavesAnUnfinishedHostUpgradeAlone(t *testing.T) {
	writeJournal := func(t *testing.T, f *enrollmentFixture, phase string) {
		t.Helper()
		release := nativeSecurityIdentity(releaseidentity.StableV1)
		journal := &automaticJournal{Format: hostJournalFormat, Phase: phase, Target: release, Instance: enrollmentInstance, Route: releaseidentity.Hash([]byte("route")),
			Source: upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: release}, Migrations: releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}, SchemaSHA256: releaseidentity.Hash([]byte("schema"))}}
		if err := writeAutomaticJournal(filepath.Join(f.c.StateDirectory, "operation.json"), journal); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("booted and unhealthy", func(t *testing.T) {
		f := newEnrollmentFixture(t)
		if err := f.enrollment().Prepare(t.Context()); err != nil {
			t.Fatal(err)
		}
		// A manual upgrade killed after it granted the candidate a start: only that upgrade may start it.
		f.healthy = false
		writeJournal(t, f, "schema-applied")
		if err := f.enrollment().Prepare(t.Context()); err == nil || !strings.Contains(err.Error(), "unfinished host upgrade") {
			t.Fatal("resumed a boot under an unfinished host upgrade", err)
		}
		if len(f.boots) != 1 {
			t.Fatal("the candidate of an unfinished upgrade was started", f.boots)
		}
		writeJournal(t, f, "complete")
		if err := f.enrollment().Prepare(t.Context()); err != nil || len(f.boots) != 2 {
			t.Fatal("a completed journal blocked the resumed boot", err, f.boots)
		}
	})
	t.Run("database absent", func(t *testing.T) {
		// A rollback killed after it removed the database: no new installation may replace it.
		f := newEnrollmentFixture(t)
		writeJournal(t, f, "rolling-back")
		before := f.snapshot()
		if err := f.enrollment().Prepare(t.Context()); err == nil || !strings.Contains(err.Error(), "unfinished host upgrade") {
			t.Fatal("enrolled a new installation over an unfinished rollback", err)
		}
		if f.directories != 0 || f.downloads != 0 || len(f.boots) != 0 || len(f.snapshot()) != len(before) {
			t.Fatal("a refused host was changed", f.directories, f.downloads, f.boots)
		}
	})
}
