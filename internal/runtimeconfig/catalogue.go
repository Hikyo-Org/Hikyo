package runtimeconfig

import "github.com/Hikyo-Org/hikyo/internal/schema"

// Key describes the runtime-owned normal project declaration. Presence is
// optional; Prepare enforces dependencies between the mail keys at activation.
type Key struct {
	Name           string
	Description    string
	Secret         bool
	Classification schema.Classification
	Declaration    schema.Declaration
}

// Catalogue returns independent metadata on every call, so an editor cannot
// mutate the runtime schema through a retained slice or declaration pointer.
func Catalogue() []Key {
	text := func(name, description string, secret bool) Key {
		classification := schema.Config
		if secret {
			classification = schema.Secret
		}
		return Key{Name: name, Description: description, Secret: secret, Classification: classification,
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString, AllowEmpty: secret}}}
	}
	choice := func(name, description string, members ...string) Key {
		return Key{Name: name, Description: description, Classification: schema.Config,
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeEnum, Members: members}}}
	}
	return []Key{
		text("HIKYO_MAIL_ADDR", "SMTP host:port. Remove every mail value to disable email.", false),
		choice("HIKYO_MAIL_TLS", "Mandatory SMTP TLS mode.", "implicit", "starttls"),
		text("HIKYO_MAIL_USER", "Optional SMTP username. Requires a password.", false),
		text("HIKYO_MAIL_PASSWORD", "SMTP password, preserved byte-for-byte. Requires a username.", true),
		text("HIKYO_MAIL_FROM", "One RFC 5322 sender address, required when email is enabled.", false),
		text("HIKYO_MAIL_EHLO", "Optional hostname sent in SMTP EHLO.", false),
		text("HIKYO_MAIL_ALLOWED_CIDRS", "Optional comma-separated private-network exceptions for SMTP only.", false),
		text("HIKYO_MAIL_CA_PEM", "Optional certificate PEM trust bundle, at most 64 KiB. No private keys.", false),
		choice("HIKYO_UPDATE_CHANNEL", "Release notification channel. Defaults to stable; does not install updates.", "stable", "nightly", "off"),
	}
}
