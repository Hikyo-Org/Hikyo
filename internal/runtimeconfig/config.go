// Package runtimeconfig owns the closed, versioned catalogue consumed by the
// running Hikyo instance. Published values are prepared without side effects.
package runtimeconfig

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"

	"github.com/Hikyo-Org/hikyo/internal/mail"
	"github.com/Hikyo-Org/hikyo/internal/schema"
)

const SchemaVersion = 1

// Bundle contains one immutable prepared configuration. Callers atomically
// replace the whole bundle and retain the captured bundle for in-flight work.
type Bundle struct {
	updateChannel    string
	mailer           *mail.Client
	ownerValues      map[string]string
	nodeValues       map[string]map[string]string
	bootstrapSources config.ManagedBootstrapSources
}

func Prepare(values map[string]string) (*Bundle, error) {
	known := make(map[string]bool, len(Catalogue()))
	for _, key := range Catalogue() {
		known[key.Name] = true
	}
	for key, value := range values {
		if !known[key] {
			return nil, errors.New("runtime configuration contains an unsupported key")
		}
		if value == "" {
			return nil, fmt.Errorf("%s must be absent or nonempty", key)
		}
		if len(value) > schema.MaxValueBytes {
			return nil, fmt.Errorf("%s exceeds the value size limit", key)
		}
	}
	ownerValues := make(map[string]string)
	for _, key := range config.ManagedOwnerKeys() {
		if value, present := values[key]; present {
			ownerValues[key] = value
		}
	}
	policy, err := config.ParseManagedOwnerPolicy(ownerValues)
	if err != nil {
		return nil, err
	}
	params := crypto.PasswordParams{MemoryKiB: policy.Argon2MemoryKiB, Time: policy.Argon2Time, Parallelism: policy.Argon2Parallelism}
	if err := params.CheckFloor(); err != nil {
		return nil, errors.New("managed Argon2 parameters are below the authentication floor")
	}
	if len(policy.BackupRecipients) > 0 {
		if err := (backup.Options{Recipients: policy.BackupRecipients}).Validate(); err != nil {
			return nil, errors.New("HIKYO_BACKUP_RECIPIENTS contains an invalid public recipient")
		}
	}
	var nodeValues map[string]map[string]string
	if raw, present := values[config.ManagedNodeOverridesKey]; present {
		nodeValues, err = config.ParseManagedNodeOverrides(raw)
		if err != nil {
			return nil, err
		}
	}
	var bootstrapSources config.ManagedBootstrapSources
	if raw, present := values[config.ManagedBootstrapSourcesKey]; present {
		bootstrapSources, err = config.ParseManagedBootstrapSources(raw)
		if err != nil {
			return nil, err
		}
	}
	channel, present := values["HIKYO_UPDATE_CHANNEL"]
	if !present {
		channel = "stable"
	}
	if channel != "stable" && channel != "nightly" && channel != "off" {
		return nil, errors.New("HIKYO_UPDATE_CHANNEL must be stable, nightly or off")
	}
	mailer, err := mail.New(mail.Config{
		Addr: values["HIKYO_MAIL_ADDR"], TLS: values["HIKYO_MAIL_TLS"], User: values["HIKYO_MAIL_USER"],
		Password: values["HIKYO_MAIL_PASSWORD"], From: values["HIKYO_MAIL_FROM"], EHLO: values["HIKYO_MAIL_EHLO"],
		AllowedCIDRs: values["HIKYO_MAIL_ALLOWED_CIDRS"], CAPEM: values["HIKYO_MAIL_CA_PEM"],
	})
	if err != nil {
		return nil, err
	}
	return &Bundle{updateChannel: channel, mailer: mailer, ownerValues: maps.Clone(ownerValues), nodeValues: nodeValues, bootstrapSources: bootstrapSources}, nil
}

// PrepareForConfig additionally proves that the owner settings fit this node's
// bootstrap/deployment context. It performs no activation or I/O.
func PrepareForConfig(values map[string]string, base *config.Config) (*Bundle, error) {
	bundle, err := Prepare(values)
	if err != nil {
		return nil, err
	}
	if bundle.HasNodeValues() {
		return nil, errors.New("managed node configuration requires an explicit node identity")
	}
	effective, err := config.ApplyManagedOwnerValues(base, bundle.ownerValues)
	if err != nil {
		return nil, err
	}
	if _, err := admission.New(admission.Config{BudgetMiB: effective.AdmissionBudgetMiB, ArgonMemoryKiB: effective.Argon2MemoryKiB}); err != nil {
		return nil, errors.New("managed Argon2 memory exceeds this node's admission budget")
	}
	return bundle, nil
}

// OwnerValues returns independent application settings only. Mail and release
// notifications have separate runtime owners and are omitted from this map.
func (b *Bundle) OwnerValues() map[string]string { return maps.Clone(b.ownerValues) }

func (b *Bundle) UpdateChannel() string { return b.updateChannel }
func (b *Bundle) MailConfigured() bool  { return b.mailer.Configured() }

// Send uses this exact prepared bundle, including when a newer revision is
// activated concurrently. Runtime fencing and test budgets belong to callers.
func (b *Bundle) Send(ctx context.Context, to, subject, body string) error {
	return b.mailer.Send(ctx, to, subject, body)
}

// BootstrapSources returns value-only installed source selections.
func (b *Bundle) BootstrapSources() config.ManagedBootstrapSources { return b.bootstrapSources }
