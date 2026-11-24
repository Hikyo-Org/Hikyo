//go:build darwin || linux

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/testutil/reflectx"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// These cover every refusal that precedes the signed-build checks, so none of
// them can reach the gate, the database or any state write. The signed positive
// path and the refusals that need a real release run in
// TestUnattendedPackagedEnrollmentUpgradeAndIntermediateCredential.
func TestResolveUnattendedOperatorRefusals(t *testing.T) {
	type installation struct {
		cfg       *config.Config
		directory string
	}
	identity := preparationIdentity("1.0.0", 1)
	enrolled := func(t *testing.T) installation {
		t.Helper()
		state, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(state, 0700); err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(state, "unattended")
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := writeUnattendedEnrollment(filepath.Join(directory, "enrollment.json"), unattendedEnrollment{Format: "hikyo.unattended-enrollment/v1", Target: identity, Instance: "installation", Operator: releaseidentity.Hash([]byte("pin"))}); err != nil {
			t.Fatal(err)
		}
		if err := recordUnattendedImage(directory, selfupdate.PreparedNightly{Identity: identity, BundleDirectory: filepath.Join(state, "bundle"), BinarySHA256: releaseidentity.Hash([]byte("not this executable"))}); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{Upgrade: config.UpgradeConfiguration{Unattended: true, StateDirectory: state}, AutoMigrate: true}
		return installation{cfg: cfg, directory: directory}
	}
	for _, test := range []struct {
		name   string
		change func(*testing.T, installation)
		want   string
	}{
		{name: "development", change: func(_ *testing.T, i installation) { i.cfg.Dev = true }, want: "signed production release"},
		{name: "relative state directory", change: func(_ *testing.T, i installation) { i.cfg.Upgrade.StateDirectory = "state" }, want: "absolute HIKYO_UPGRADE_STATE_DIR"},
		{name: "manual bundle", change: func(_ *testing.T, i installation) { i.cfg.Upgrade.BundleDirectory = "/bundle" }, want: "HIKYO_UPGRADE_BUNDLE belongs to manual"},
		{name: "manual operator key", change: func(_ *testing.T, i installation) { i.cfg.Upgrade.OperatorPublicKeyFile = "/operator.pub" }, want: "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY belongs to manual"},
		{name: "manual target", change: func(_ *testing.T, i installation) { i.cfg.Upgrade.TargetManifestSHA256 = "x" }, want: "HIKYO_UPGRADE_TARGET_MANIFEST belongs to manual"},
		{name: "manual evidence", change: func(_ *testing.T, i installation) { i.cfg.Upgrade.EvidenceDirectory = "/evidence" }, want: "HIKYO_UPGRADE_EVIDENCE belongs to manual"},
		{name: "manual legacy writers claim", change: func(_ *testing.T, i installation) { i.cfg.Upgrade.LegacyWritersStopped = true }, want: "HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED belongs to manual"},
		{name: "manual ciphertext", change: func(_ *testing.T, i installation) { i.cfg.Upgrade.CiphertextPath = "/backup" }, want: "HIKYO_UPGRADE_BACKUP belongs to manual"},
		{name: "never enrolled", change: func(t *testing.T, i installation) {
			if err := os.RemoveAll(i.directory); err != nil {
				t.Fatal(err)
			}
		}, want: "operator commands never enroll"},
		{name: "enrollment record missing", change: func(t *testing.T, i installation) {
			if err := os.Remove(filepath.Join(i.directory, "enrollment.json")); err != nil {
				t.Fatal(err)
			}
		}, want: "operator commands never enroll"},
		{name: "enrollment record malformed", change: func(t *testing.T, i installation) {
			if err := os.WriteFile(filepath.Join(i.directory, "enrollment.json"), []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
		}, want: "invalid unattended enrollment"},
		{name: "state directory not private", change: func(t *testing.T, i installation) {
			if err := os.Chmod(i.directory, 0755); err != nil {
				t.Fatal(err)
			}
		}, want: "private real directory"},
		{name: "unfinished enrollment", change: func(t *testing.T, i installation) {
			if err := writeUnattendedEnrollment(filepath.Join(i.directory, "enrollment.json"), unattendedEnrollment{Format: "hikyo.unattended-enrollment/v1", Target: identity, Operator: releaseidentity.Hash([]byte("pin"))}); err != nil {
				t.Fatal(err)
			}
		}, want: "enrollment is unfinished"},
		{name: "unfinished upgrade", change: func(t *testing.T, i installation) {
			manifest := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}
			journal := &automaticJournal{Format: containerJournalFormat, Phase: "restore-required", Target: identity, Route: releaseidentity.Hash([]byte("route")), Source: upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: identity}, Migrations: manifest, SchemaSHA256: releaseidentity.Hash([]byte("schema"))}}
			if err := writeAutomaticJournal(filepath.Join(i.directory, "operation.json"), journal); err != nil {
				t.Fatal(err)
			}
		}, want: `unfinished (phase "restore-required")`},
		{name: "unreadable journal", change: func(t *testing.T, i installation) {
			if err := os.WriteFile(filepath.Join(i.directory, "operation.json"), []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
		}, want: "invalid private upgrade journal"},
		{name: "image record missing", change: func(t *testing.T, i installation) {
			if err := os.Remove(filepath.Join(i.directory, "image.json")); err != nil {
				t.Fatal(err)
			}
		}, want: "no usable image record"},
		{name: "different executable", change: func(*testing.T, installation) {}, want: "executable differs from the image the installation last admitted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			i := enrolled(t)
			test.change(t, i)
			before := *i.cfg
			err := resolveUnattendedOperator(t.Context(), i.cfg, os.Args[0])
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
			if !reflectx.Equal(*i.cfg, before) {
				t.Fatalf("a refused resolution changed the configuration: %+v", i.cfg.Upgrade)
			}
		})
	}
}

func TestResolveUnattendedOperatorLeavesManualInstallationsAlone(t *testing.T) {
	cfg := &config.Config{AutoMigrate: true, Upgrade: config.UpgradeConfiguration{BundleDirectory: "/bundle", OperatorPublicKeyFile: "/operator.pub", StateDirectory: "/state"}}
	before := *cfg
	if err := ResolveUnattendedOperator(t.Context(), cfg); err != nil || !reflectx.Equal(*cfg, before) {
		t.Fatalf("a manual installation was changed or refused: %v %+v", err, cfg.Upgrade)
	}
}

func TestRequireHealthyAdmittedState(t *testing.T) {
	healthy := func() upgrade.State { return upgrade.State{Pending: &upgrade.Operation{Phase: upgrade.Healthy}} }
	if err := requireHealthyAdmittedState(healthy()); err != nil {
		t.Fatalf("healthy state refused: %v", err)
	}
	for name, change := range map[string]func(*upgrade.State){
		"no pending operation": func(s *upgrade.State) { s.Pending = nil },
		"restore required":     func(s *upgrade.State) { s.Pending.Phase = upgrade.RestoreRequired },
		"mid upgrade":          func(s *upgrade.State) { s.Pending.Phase = upgrade.SchemaApplied },
		"maintenance":          func(s *upgrade.State) { s.Maintenance = true },
		"invalidated":          func(s *upgrade.State) { s.Pending.Invalidated = true },
	} {
		t.Run(name, func(t *testing.T) {
			state := healthy()
			change(&state)
			if err := requireHealthyAdmittedState(state); err == nil || !strings.Contains(err.Error(), "not in a healthy admitted state") {
				t.Fatalf("got %v", err)
			}
		})
	}
}
