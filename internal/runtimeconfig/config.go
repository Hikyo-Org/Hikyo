// Package runtimeconfig owns the closed, versioned catalogue consumed by the
// running Hikyo instance. Published values are prepared without side effects.
package runtimeconfig

import (
	"context"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/mail"
	"github.com/Hikyo-Org/hikyo/internal/schema"
)

const SchemaVersion = 1

// Bundle contains one immutable prepared configuration. Callers atomically
// replace the whole bundle and retain the captured bundle for in-flight work.
type Bundle struct {
	updateChannel string
	mailer        *mail.Client
}

func Prepare(values map[string]string) (*Bundle, error) {
	known := make(map[string]bool, 9)
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
	return &Bundle{updateChannel: channel, mailer: mailer}, nil
}

func (b *Bundle) UpdateChannel() string { return b.updateChannel }
func (b *Bundle) MailConfigured() bool  { return b.mailer.Configured() }

// Send uses this exact prepared bundle, including when a newer revision is
// activated concurrently. Runtime fencing and test budgets belong to callers.
func (b *Bundle) Send(ctx context.Context, to, subject, body string) error {
	return b.mailer.Send(ctx, to, subject, body)
}
