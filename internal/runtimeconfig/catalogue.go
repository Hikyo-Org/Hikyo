package runtimeconfig

import (
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/schema"
)

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
	keys := []Key{
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
	descriptions := map[string]string{
		"HIKYO_ARGON2_MEMORY_KIB":          "Argon2id memory in KiB. Must meet the authentication floor and each node's admission budget.",
		"HIKYO_ARGON2_TIME":                "Argon2id time cost. Minimum 3.",
		"HIKYO_ARGON2_PARALLELISM":         "Argon2id parallelism. Minimum 2, maximum 255.",
		"HIKYO_AUDIT_ACCESS_RETAIN_DAYS":   "Access audit retention in days, from 1 to 3650.",
		"HIKYO_AUDIT_SECURITY_RETAIN_DAYS": "Security audit retention in days. At least access retention, at most 3650.",
		"HIKYO_BACKUP_INTERVAL":            "Scheduled backup interval, at least 1h. Requires a complete backup policy.",
		"HIKYO_BACKUP_RECIPIENTS":          "Comma-separated public age recipients. Each node must have a backup destination.",
		"HIKYO_BACKUP_RETAIN_COUNT":        "Backup retention count, from 1 to 100000. Requires a complete backup policy.",
		"HIKYO_BACKUP_RETAIN_DAYS":         "Backup retention in days, from 1 to 180. Requires a complete backup policy.",
		"HIKYO_BACKUP_RPO":                 "Backup recovery point objective, at least the backup interval.",
		"HIKYO_BACKUP_RTO_TARGET":          "Positive backup recovery time objective, for example 30m.",
		"HIKYO_DIRECTORY_PROXY":            "Optional HTTPS fleet directory proxy. Credentials are treated as secret.",
		"HIKYO_EXTERNAL_ORIGIN":            "Exact public HTTP(S) origin, without credentials, path, query or fragment.",
		"HIKYO_MCP_ALLOWED_ORIGINS":        "Optional comma-separated exact browser origins. Requires MCP enabled.",
		"HIKYO_MCP_ENABLED":                "Enable the MCP endpoint. Requires an HTTPS public origin outside loopback development.",
		"HIKYO_REAUTH_WINDOW_SECONDS":      "Disclosure reauthentication window in seconds, from 0 to 86400. Apply still requires its own ceremony.",
	}
	for _, name := range config.ManagedOwnerKeys() {
		key := text(name, descriptions[name], name == "HIKYO_DIRECTORY_PROXY")
		switch name {
		case "HIKYO_ARGON2_MEMORY_KIB", "HIKYO_ARGON2_TIME", "HIKYO_ARGON2_PARALLELISM", "HIKYO_AUDIT_ACCESS_RETAIN_DAYS", "HIKYO_AUDIT_SECURITY_RETAIN_DAYS", "HIKYO_BACKUP_RETAIN_COUNT", "HIKYO_BACKUP_RETAIN_DAYS", "HIKYO_REAUTH_WINDOW_SECONDS":
			key.Declaration.Rule.Type = schema.TypeInteger
		case "HIKYO_MCP_ENABLED":
			key.Declaration.Rule.Type = schema.TypeBoolean
		}
		keys = append(keys, key)
	}
	keys = append(keys, text(config.ManagedNodeOverridesKey, "Versioned per-node configuration, including TLS private-key contents. Each admitted node requires its own exact entry.", true))
	keys = append(keys, text(config.ManagedBootstrapSourcesKey, "Installed database and root-key source aliases. Changes require a reviewed controlled rollout and fresh authentication.", false))
	return keys
}
