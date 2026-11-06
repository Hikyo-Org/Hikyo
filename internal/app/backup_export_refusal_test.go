//go:build unix

package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
)

// A process configured the way production is (LoadBootstrap, with both backup
// variables set) still has to be told the recipient and the destination on the
// command line, and the refusals have to say so rather than point at the
// environment.
func TestExportRefusalsNameFlagsNotEnvironment(t *testing.T) {
	_, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	for name, environment := range map[string][]string{"no environment": nil, "environment set": {recipient}} {
		t.Run(name, func(t *testing.T) {
			cfg := bootstrapConfig(t, environment...)

			_, err := exportOptions(cfg, nil, "")
			if !errors.Is(err, backup.ErrNoRecipients) || !strings.Contains(err.Error(), "pass --recipient AGE-RECIPIENT (the CLI does not read HIKYO_BACKUP_RECIPIENTS)") {
				t.Fatalf("exportOptions with no flags = %v, want the --recipient instruction", err)
			}

			err = RunBackup(t.Context(), cfg, quietLogger(), []string{"export", "--recipient", recipient}, &bytes.Buffer{}, nil, nil)
			if err == nil || err.Error() != "no destination: pass --out DIR (the CLI does not read HIKYO_BACKUP_DIR)" {
				t.Fatalf("export without --out = %v, want the --out instruction", err)
			}
		})
	}
}
