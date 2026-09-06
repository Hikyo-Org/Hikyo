package runtimeconfig_test

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"

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

func TestPreparedOwnerValuesAreIndependentAndExcludeComponentSettings(t *testing.T) {
	values := map[string]string{"HIKYO_ARGON2_TIME": "4", "HIKYO_UPDATE_CHANNEL": "nightly"}
	bundle, err := runtimeconfig.Prepare(values)
	if err != nil {
		t.Fatal(err)
	}
	values["HIKYO_ARGON2_TIME"] = "5"
	owned := bundle.OwnerValues()
	if len(owned) != 1 || owned["HIKYO_ARGON2_TIME"] != "4" {
		t.Fatal("owner values include components or alias preparation input")
	}
	owned["HIKYO_ARGON2_TIME"] = "6"
	if bundle.OwnerValues()["HIKYO_ARGON2_TIME"] != "4" {
		t.Fatal("caller mutated prepared owner settings")
	}
}

func TestPrepareForConfigEnforcesNodeContextAfterIndependentPreparation(t *testing.T) {
	_, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"HIKYO_BACKUP_RECIPIENTS": recipient}
	if _, err := runtimeconfig.Prepare(values); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeconfig.PrepareForConfig(values, &config.Config{}); err == nil {
		t.Fatal("enabled backup without this node's destination")
	}
	if _, err := runtimeconfig.PrepareForConfig(values, &config.Config{BackupDir: "/not-opened"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeconfig.PrepareForConfig(values, nil); err == nil {
		t.Fatal("accepted missing node context")
	}
}

func TestCatalogueCoversEveryOwnerApplicationSetting(t *testing.T) {
	catalogue := make(map[string]runtimeconfig.Key)
	for _, key := range runtimeconfig.Catalogue() {
		if _, duplicate := catalogue[key.Name]; duplicate {
			t.Fatalf("duplicate catalogue key %s", key.Name)
		}
		if key.Description == "" || key.Declaration.Rule == nil {
			t.Fatalf("incomplete declaration for %s", key.Name)
		}
		catalogue[key.Name] = key
	}
	for _, name := range config.ManagedOwnerKeys() {
		if _, exists := catalogue[name]; !exists {
			t.Fatalf("owner setting missing from catalogue: %s", name)
		}
	}
	if !catalogue["HIKYO_DIRECTORY_PROXY"].Secret {
		t.Fatal("proxy credentials have a non-secret declaration")
	}
	first := runtimeconfig.Catalogue()
	first[0].Declaration.Rule.AllowEmpty = true
	if runtimeconfig.Catalogue()[0].Declaration.Rule.AllowEmpty {
		t.Fatal("catalogue declaration aliases a prior result")
	}
}

func TestOwnerPreparationEnforcesCryptographicPolicy(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"memory floor":      {"HIKYO_ARGON2_MEMORY_KIB": "8"},
		"time floor":        {"HIKYO_ARGON2_TIME": "2"},
		"parallelism floor": {"HIKYO_ARGON2_PARALLELISM": "1"},
		"invalid recipient": {"HIKYO_BACKUP_RECIPIENTS": "do-not-disclose"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runtimeconfig.Prepare(values)
			if err == nil || strings.Contains(err.Error(), "do-not-disclose") {
				t.Fatalf("cryptographic policy result: %v", err)
			}
		})
	}
	values := map[string]string{"HIKYO_ARGON2_MEMORY_KIB": "524288"}
	if _, err := runtimeconfig.Prepare(values); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeconfig.PrepareForConfig(values, &config.Config{AdmissionBudgetMiB: 272}); err == nil {
		t.Fatal("insufficient actual admission capacity accepted")
	}
	if _, err := runtimeconfig.PrepareForConfig(values, &config.Config{AdmissionBudgetMiB: 1040}); err != nil {
		t.Fatal(err)
	}
}
