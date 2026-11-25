package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/schema"
)

// config is a leaf package, so its bootstrap seed inlines the write-time trim
// instead of calling schema.Normalize; this pins the two together, for inline
// values and imported files.
func TestManagedSeedTrimMatchesSchemaNormalize(t *testing.T) {
	for _, raw := range []string{
		" secret ", "\tsecret\r\n", "\u00a0secret\u00a0", "\u0085secret\u2003", "\u3000secret\u205f",
		// U+200B is not whitespace, so neither side may trim it.
		"\u200bsecret\u200b", "in ner",
	} {
		file := filepath.Join(t.TempDir(), "password")
		if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		for _, inputs := range []map[string]string{
			{"HIKYO_MAIL_PASSWORD": raw},
			{"HIKYO_MAIL_PASSWORD_FILE": file},
		} {
			cfg := &config.Config{ManagedInputs: inputs}
			values, err := cfg.ManagedSeed()
			if err != nil {
				t.Fatalf("ManagedSeed(%q): %v", raw, err)
			}
			if got, want := values["HIKYO_MAIL_PASSWORD"], schema.Normalize(raw); got != want {
				t.Errorf("seed from %v trimmed %q to %q, schema.Normalize gives %q", inputs, raw, got, want)
			}
		}
	}
}
