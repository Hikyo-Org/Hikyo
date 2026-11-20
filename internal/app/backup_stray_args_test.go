package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
)

// `--recipient A B` parses as one recipient and a stray B. The export used to
// succeed with only A, so the operator believed B could open an archive it
// could not (#1213).
func TestBackupExportRefusesStrayArguments(t *testing.T) {
	_, a, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"second recipient without its flag": {"--recipient", a, b},
		"argument before the flags":         {b, "--recipient", a},
	} {
		t.Run(name, func(t *testing.T) {
			destination := t.TempDir()
			cfg := bootstrapConfig(t)
			err := RunBackup(t.Context(), cfg, quietLogger(), append([]string{"export", "--out", destination}, args...), &bytes.Buffer{}, nil, nil)
			if err == nil || !strings.Contains(err.Error(), "unexpected argument") || !strings.Contains(err.Error(), `"`+b+`"`) {
				t.Fatalf("export = %v, want a refusal naming %q", err, b)
			}
			if entries, _ := os.ReadDir(destination); len(entries) != 0 {
				t.Fatalf("a refused export left %d entries behind", len(entries))
			}
		})
	}
}

func TestBackupKeygenRefusesStrayArguments(t *testing.T) {
	output := filepath.Join(t.TempDir(), "identity.txt")
	err := RunBackup(t.Context(), bootstrapConfig(t), quietLogger(), []string{"keygen", "--output-file", output, "extra"}, &bytes.Buffer{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), `"extra"`) {
		t.Fatalf("keygen = %v, want a refusal naming the stray argument", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("a refused keygen created %s (%v)", output, statErr)
	}
}

// RunBackup loads and verifies the release bundle before it hands upgrade-export
// and upgrade-drill their arguments, so a unit test cannot reach the flag check
// through it. These call the two verbs the way RunUpgradeBackup does; the stray
// check runs before either touches the trust context.
func TestUpgradeVerbsNameStrayArguments(t *testing.T) {
	cfg := bootstrapConfig(t)
	for name, tc := range map[string]struct {
		run  func(args []string) error
		args []string
	}{
		"upgrade-export": {
			run: func(args []string) error {
				return runUpgradeExport(t.Context(), cfg, args, &bytes.Buffer{}, TrustContext{})
			},
			args: []string{"--out", t.TempDir(), "--recipient", "age1first", "age1second"},
		},
		"upgrade-drill": {
			run: func(args []string) error {
				return runUpgradeDrill(t.Context(), args, &bytes.Buffer{}, TrustContext{})
			},
			args: []string{"--out", t.TempDir(), "--from", "archive", "age1second"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.run(tc.args)
			if err == nil || !strings.Contains(err.Error(), `"age1second"`) || !strings.Contains(err.Error(), "unexpected argument") {
				t.Fatalf("%s = %v, want a refusal naming the stray argument", name, err)
			}
			if strings.Contains(err.Error(), "requires") {
				t.Fatalf("%s = %v, want the stray argument, not the missing-flag message", name, err)
			}
		})
	}
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
