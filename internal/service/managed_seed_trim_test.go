package service

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/schema"
)

// config is a leaf package, so its bootstrap seed inlines the write-time trim
// instead of calling schema.Normalize; this pins the two together.
func TestManagedSeedTrimMatchesSchemaNormalize(t *testing.T) {
	for _, raw := range []string{
		" secret ", "\tsecret\r\n", " secret ", "\u0085secret ", "　secret ", "​secret​", "in ner",
	} {
		cfg := &config.Config{ManagedInputs: map[string]string{"HIKYO_MAIL_PASSWORD": raw}}
		values, err := cfg.ManagedSeed()
		if err != nil {
			t.Fatalf("ManagedSeed(%q): %v", raw, err)
		}
		if got, want := values["HIKYO_MAIL_PASSWORD"], schema.Normalize(raw); got != want {
			t.Errorf("seed trimmed %q to %q, schema.Normalize gives %q", raw, got, want)
		}
	}
}
