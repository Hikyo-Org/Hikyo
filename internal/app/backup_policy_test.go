//go:build unix

package app

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
)

func hybridRecipients(t *testing.T, n int) []string {
	t.Helper()
	out := make([]string, n)
	for i := range out {
		var err error
		if _, out[i], err = backup.GenerateHybridIdentity(); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// storedProjection is the saved-configuration catalogue an adopted instance
// carries, built from the same catalogue the candidate check compares against.
func storedProjection(t *testing.T) *upgrade.CandidateConfiguration {
	t.Helper()
	projection := &upgrade.CandidateConfiguration{SchemaVersion: runtimeconfig.SchemaVersion}
	for _, key := range runtimeconfig.Catalogue() {
		compiled, err := schema.CompileClassified(key.Classification, key.Declaration)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := compiled.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		projection.Catalogue = append(projection.Catalogue, upgrade.CandidateConfigurationKey{Name: key.Name, Classification: string(key.Classification), Declaration: string(canonical), RequiredMode: string(schema.PresenceNone), ForbiddenMode: string(schema.PresenceNone)})
	}
	return projection
}

func bootstrapConfig(t *testing.T, recipients ...string) *config.Config {
	t.Helper()
	// LoadBootstrap blanks the destination and moves owner settings such as
	// the recipients to ManagedInputs, so the CLI path sees neither.
	values := map[string]string{"HIKYO_DB": "sqlite:" + filepath.Join(t.TempDir(), "hikyo.db"), "HIKYO_BACKUP_DIR": t.TempDir()}
	if len(recipients) > 0 {
		values["HIKYO_BACKUP_RECIPIENTS"] = strings.Join(recipients, ",")
	}
	cfg, _, err := config.LoadBootstrap("server", []string{"--dev"}, func(key string) string { return values[key] }, nil)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// config.LoadBootstrap moves owner settings into ManagedInputs, so the field a
// naive check would read is empty in a production process. The effective
// policy comes from the managed bundle, which is also what refuses a mixed set.
func TestBootstrapBlanksOwnerPolicyAndEffectivePolicyRecoversIt(t *testing.T) {
	hybrid := hybridRecipients(t, 2)
	_, classical, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	cfg := bootstrapConfig(t, hybrid[0])
	if len(cfg.BackupRecipients) != 0 || cfg.ManagedInputs["HIKYO_BACKUP_RECIPIENTS"] != hybrid[0] {
		t.Fatalf("LoadBootstrap kept the owner policy in BackupRecipients=%v", cfg.BackupRecipients)
	}
	// The bug this guards: a check on cfg.BackupRecipients sees nothing.
	if err := requireCustodyMatchesBackupPolicy(cfg.BackupRecipients, classical); err != nil {
		t.Fatalf("setup: an empty field refused: %v", err)
	}
	effective, err := resolveCandidateConfiguration(t.Context(), cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(effective.BackupRecipients) != 1 || effective.BackupRecipients[0] != hybrid[0] {
		t.Fatalf("effective policy = %v, want the managed hybrid recipient", effective.BackupRecipients)
	}
	if err := requireCustodyMatchesBackupPolicy(effective.BackupRecipients, classical); !errors.Is(err, ErrHybridPolicyClassicalCustody) {
		t.Fatalf("effective hybrid policy over X25519 custody = %v, want a refusal", err)
	}

	// A mixed managed policy fails where the managed bundle is prepared, which is
	// the startup guard for managed values. The recipient is not echoed.
	mixed := bootstrapConfig(t, hybrid[0], classical)
	if _, err := resolveCandidateConfiguration(t.Context(), mixed, nil, nil); !errors.Is(err, backup.ErrMixedRecipients) || !strings.Contains(err.Error(), "HIKYO_BACKUP_RECIPIENTS") {
		t.Fatalf("mixed managed policy = %v, want ErrMixedRecipients naming the variable", err)
	}
}

// After adoption the stored managed value is the policy: a hybrid value set
// there refuses even with an empty environment, and a stale hybrid line in the
// environment does not refuse an X25519 stored policy.
func TestStoredManagedPolicyOverridesTheEnvironmentSeed(t *testing.T) {
	hybrid := hybridRecipients(t, 1)[0]
	_, classical, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	projection := storedProjection(t)
	resolve := func(cfg *config.Config, stored string) []string {
		t.Helper()
		values := map[string]string{"HIKYO_BACKUP_RECIPIENTS": stored}
		effective, err := resolveCandidateConfiguration(t.Context(), cfg, projection, values)
		if err != nil {
			t.Fatal(err)
		}
		return effective.BackupRecipients
	}
	// Stored hybrid, environment empty.
	stored := resolve(bootstrapConfig(t), hybrid)
	if err := requireCustodyMatchesBackupPolicy(stored, classical); !errors.Is(err, ErrHybridPolicyClassicalCustody) {
		t.Fatalf("stored hybrid policy over X25519 custody = %v, want a refusal", err)
	}
	// Stored X25519, stale hybrid line in the environment.
	stored = resolve(bootstrapConfig(t, hybrid), classical)
	if err := requireCustodyMatchesBackupPolicy(stored, classical); err != nil {
		t.Fatalf("a stale environment hybrid line refused an X25519 stored policy: %v", err)
	}
}

// The automatic path's check on the same shapes, driven with a stored
// projection the way the gate hands it over. The environment (cfg) only speaks
// when nothing is stored.
func TestAutomaticPolicyCheckReadsTheStoredValue(t *testing.T) {
	hybrid := hybridRecipients(t, 1)[0]
	_, classical, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	projection := storedProjection(t)
	env := func(recipients ...string) *config.Config { return &config.Config{BackupRecipients: recipients} }
	stored := func(value string) map[string]string {
		values := map[string]string{}
		if value != "" {
			values["HIKYO_BACKUP_RECIPIENTS"] = value
		}
		return values
	}
	for name, tc := range map[string]struct {
		cfg    *config.Config
		stored map[string]string
		refuse bool
	}{
		"stored hybrid, environment empty":        {env(), stored(hybrid), true},
		"stored hybrid, environment X25519":       {env(classical), stored(hybrid), true},
		"stored X25519, stale hybrid environment": {env(hybrid), stored(classical), false},
		"stored empty, stale hybrid environment":  {env(hybrid), stored(""), false},
		"stored X25519, environment empty":        {env(), stored(classical), false},
	} {
		t.Run(name, func(t *testing.T) {
			err := automaticPolicyCheck(tc.cfg, classical)(t.Context(), projection, tc.stored)
			if tc.refuse != errors.Is(err, ErrHybridPolicyClassicalCustody) || (!tc.refuse && err != nil) {
				t.Fatalf("refusal = %v, want refuse=%t", err, tc.refuse)
			}
		})
	}
	// Nothing stored: the environment seed is the policy.
	if err := automaticPolicyCheck(env(hybrid), classical)(t.Context(), nil, nil); !errors.Is(err, ErrHybridPolicyClassicalCustody) {
		t.Fatalf("unadopted hybrid environment = %v, want a refusal", err)
	}
	if err := automaticPolicyCheck(env(), classical)(t.Context(), nil, nil); err != nil {
		t.Fatalf("unadopted empty environment refused: %v", err)
	}
}

// The custody check runs against a real vault, which enrollment always mints as
// X25519: a hybrid policy is refused, every other policy is untouched.
func TestCustodyBackupPolicyAgainstRealVault(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(base, "custody")
	vault, err := upgradecustody.CreateLocal(directory, bytes.Repeat([]byte{0x42}, 32), "ins_22222222222222222222222222222222")
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	if strings.HasPrefix(vault.Recipient(), "age1pq1") {
		t.Fatal("enrollment minted a hybrid identity: this check must be revisited")
	}
	_, classical, _ := backup.GenerateIdentity()
	hybrid := hybridRecipients(t, 1)
	for name, tc := range map[string]struct {
		configured []string
		refuse     bool
	}{
		"no policy":            {},
		"x25519 policy":        {configured: []string{classical}},
		"x25519 same as vault": {configured: []string{vault.Recipient()}},
		"hybrid policy":        {configured: hybrid, refuse: true},
		"mixed policy":         {configured: []string{classical, hybrid[0]}, refuse: true},
	} {
		t.Run(name, func(t *testing.T) {
			err := requireCustodyMatchesBackupPolicy(tc.configured, vault.Recipient())
			if tc.refuse != errors.Is(err, ErrHybridPolicyClassicalCustody) || (!tc.refuse && err != nil) {
				t.Fatalf("refusal = %v, want refuse=%t", err, tc.refuse)
			}
		})
	}
	// A hybrid custody recipient satisfies a hybrid policy.
	if err := requireCustodyMatchesBackupPolicy(hybrid, hybrid[0]); err != nil {
		t.Fatalf("hybrid custody refused: %v", err)
	}
	if !strings.Contains(ErrHybridPolicyClassicalCustody.Error(), "upgrade-export") || !strings.Contains(ErrHybridPolicyClassicalCustody.Error(), "upgrade-drill") {
		t.Fatal("the refusal does not point at the manual upgrade route")
	}
}

// The automatic path's seam, against real SQLite sources: a ledger source reads
// its stored managed configuration, and a legacy source (no ledger) falls back
// to the environment seed, which is then the only policy there is.
func TestAutomaticCustodyPolicySeamOnRealSources(t *testing.T) {
	hybrid := hybridRecipients(t, 1)[0]
	_, classical, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	env := func(recipients ...string) *config.Config {
		// config.Load keeps the owner policy in BackupRecipients, as the root
		// process of `sudo hikyo upgrade` does.
		values := map[string]string{"HIKYO_DB": "sqlite:" + filepath.Join(t.TempDir(), "x.db"), "HIKYO_BACKUP_DIR": t.TempDir()}
		if len(recipients) > 0 {
			values["HIKYO_BACKUP_RECIPIENTS"] = strings.Join(recipients, ",")
		}
		cfg, _, err := config.Load("upgrade", []string{"--dev"}, func(key string) string { return values[key] }, nil)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	root := bytes.Repeat([]byte{61}, 32)

	t.Run("ledger source", func(t *testing.T) {
		source := upgradeDrillDatabase(t, store.EngineSQLite)
		database := backupGateConfig(source)
		gatefixture.PrepareWithMaterial(t, database, store.MigrationsFS, "migrations/sqlite", bytes.Clone(root))
		if err := requireAutomaticCustodyMatchesPolicy(t.Context(), env(hybrid), database, bytes.Clone(root), classical); !errors.Is(err, ErrHybridPolicyClassicalCustody) {
			t.Fatalf("hybrid policy over X25519 custody on a ledger source = %v, want a refusal", err)
		}
		if err := requireAutomaticCustodyMatchesPolicy(t.Context(), env(classical), database, bytes.Clone(root), classical); err != nil {
			t.Fatalf("X25519 policy refused: %v", err)
		}
		if err := requireAutomaticCustodyMatchesPolicy(t.Context(), env(), database, bytes.Clone(root), classical); err != nil {
			t.Fatalf("empty policy refused: %v", err)
		}
	})
	t.Run("legacy source", func(t *testing.T) {
		f := newUpgradeDrillFixture(t, store.EngineSQLite, false, true)
		database := upgrade.Config{Engine: releaseidentity.SQLite, Path: f.cfg.Path}
		if err := requireAutomaticCustodyMatchesPolicy(t.Context(), env(hybrid), database, bytes.Clone(f.root), classical); !errors.Is(err, ErrHybridPolicyClassicalCustody) {
			t.Fatalf("hybrid environment policy over X25519 custody on a legacy source = %v, want a refusal", err)
		}
		if err := requireAutomaticCustodyMatchesPolicy(t.Context(), env(classical), database, bytes.Clone(f.root), classical); err != nil {
			t.Fatalf("X25519 policy refused: %v", err)
		}
	})
}

// RunAutomaticUpgrade needs Linux, root and systemd, so it cannot run in this
// suite. This keeps its wiring honest: the function must call the policy seam,
// and must do so before it journals, publishes evidence or stops the service.
func TestRunAutomaticUpgradeCallsThePolicySeamBeforeAnyEffect(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "automatic_upgrade.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var positions = map[string]token.Pos{}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "RunAutomaticUpgrade" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			}
			// First sighting, except FenceAndStop: the early restore-required
			// guard also stops the service and is unrelated to the export path.
			if _, seen := positions[name]; !seen || name == "FenceAndStop" {
				positions[name] = call.Pos()
			}
			return true
		})
	}
	seam, ok := positions["requireAutomaticCustodyMatchesPolicy"]
	if !ok {
		t.Fatal("RunAutomaticUpgrade no longer checks the custody identity against the effective backup policy")
	}
	for _, effect := range []string{"openAutomaticCustody", "writeAutomaticJournal", "PublishPublicEvidence", "FenceAndStop", "prepareAutomaticEvidence"} {
		if at, present := positions[effect]; !present {
			t.Fatalf("expected RunAutomaticUpgrade to call %s", effect)
		} else if effect == "openAutomaticCustody" && at > seam {
			t.Fatal("the policy seam runs before custody is opened")
		} else if effect != "openAutomaticCustody" && at < seam {
			t.Fatalf("%s runs before the policy seam", effect)
		}
	}
}
