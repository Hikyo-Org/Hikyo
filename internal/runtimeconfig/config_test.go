package runtimeconfig_test

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
)

func TestAbsentManagedValuesUseDocumentedDefaults(t *testing.T) {
	bundle, err := runtimeconfig.Prepare(nil)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.MailConfigured() || bundle.UpdateChannel() != "stable" {
		t.Fatal("unexpected managed defaults")
	}
}

func TestManagedPreparationRejectsUnknownAndInvalidValues(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"unknown":         {"HIKYO_ROOT_KEY": "do-not-disclose"},
		"password path":   {"HIKYO_MAIL_PASSWORD_FILE": "/do-not-read"},
		"CA path":         {"HIKYO_MAIL_CA_FILE": "/do-not-read"},
		"partial mail":    {"HIKYO_MAIL_FROM": "hikyo@example.com"},
		"blank mail":      {"HIKYO_MAIL_ADDR": ""},
		"invalid channel": {"HIKYO_UPDATE_CHANNEL": "do-not-disclose"},
		"blank channel":   {"HIKYO_UPDATE_CHANNEL": ""},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runtimeconfig.Prepare(values)
			if err == nil {
				t.Fatal("invalid managed values accepted")
			}
			if strings.Contains(err.Error(), "do-not-") {
				t.Fatal("validation error disclosed input")
			}
		})
	}
}

func TestPreparedBundleDoesNotChangeWithInput(t *testing.T) {
	values := map[string]string{"HIKYO_UPDATE_CHANNEL": "nightly", "HIKYO_MAIL_ADDR": "no-such-relay.invalid:465", "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "hikyo@example.com"}
	bundle, err := runtimeconfig.Prepare(values)
	if err != nil {
		t.Fatal(err)
	}
	clear(values)
	if bundle.UpdateChannel() != "nightly" || !bundle.MailConfigured() {
		t.Fatal("prepared bundle did not retain independent configuration")
	}
}
