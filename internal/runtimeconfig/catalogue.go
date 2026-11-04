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
		"HIKYO_DEFAULT_KEYS_PER_PROJECT":   "Inherited project key limit, from 1 to the instance maximum. Defaults to 1000. Project settings can override it.",
		"HIKYO_MAX_KEYS_PER_PROJECT":       "Instance ceiling for project key limits, from 1 to 10000. Defaults to 10000. Lowering it caps retained overrides without deleting keys; the 100000-cell project budget also applies.",
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
		"HIKYO_PAIRING_SPKI_PIN":           "Optional base64 SHA-256 public-key fingerprint of this instance's external HTTPS endpoint. Use for a self-signed reverse proxy or when automatic certificate discovery cannot reach the public origin. Applies only to new pairing invitations.",
		"HIKYO_MCP_ALLOWED_ORIGINS":        "Optional comma-separated exact browser origins such as https://app.example.com, not IPs or CIDRs. Requires MCP enabled.",
		"HIKYO_MCP_ENABLED":                "Enable the MCP endpoint. Requires an HTTPS public origin outside loopback development.",
		"HIKYO_MCP_WRITE_ENABLED":          "Install the MCP stage-and-validate write tools. Requires MCP enabled; grants no authority by itself.",
		"HIKYO_REAUTH_WINDOW_SECONDS":      "Disclosure reauthentication window in seconds, from 0 to 86400. Apply still requires its own ceremony.",
		"HIKYO_TLS_PROFILE":                "Native listener key-exchange policy. `compat` keeps today's TLS behavior and is the default. `strict` requires TLS 1.3 and the hybrid X25519MLKEM768 group and needs a TLS certificate pair. It protects handshake confidentiality only; certificate authentication is separate and stays classical. Each node enforces its own activated profile, and a node that is behind the desired generation keeps serving at its previous profile, so one node being strict does not make the fleet strict.",
		"HIKYO_SECOND_FACTOR":              "Sign-in second-factor policy. `required` gates a password login on an account with no factor into enrolment; `optional` keeps the local floor.",
	}
	for _, name := range config.ManagedOwnerKeys() {
		key := text(name, descriptions[name], name == "HIKYO_DIRECTORY_PROXY")
		switch name {
		case "HIKYO_DEFAULT_KEYS_PER_PROJECT", "HIKYO_MAX_KEYS_PER_PROJECT":
			minimum, maximum := int64(1), int64(config.MaxKeysPerProjectCeiling)
			key.Declaration.Rule.Type = schema.TypeInteger
			key.Declaration.Rule.Min, key.Declaration.Rule.Max = &minimum, &maximum
		case "HIKYO_ARGON2_MEMORY_KIB", "HIKYO_ARGON2_TIME", "HIKYO_ARGON2_PARALLELISM", "HIKYO_AUDIT_ACCESS_RETAIN_DAYS", "HIKYO_AUDIT_SECURITY_RETAIN_DAYS", "HIKYO_BACKUP_RETAIN_COUNT", "HIKYO_BACKUP_RETAIN_DAYS", "HIKYO_REAUTH_WINDOW_SECONDS":
			key.Declaration.Rule.Type = schema.TypeInteger
		case "HIKYO_MCP_ENABLED", "HIKYO_MCP_WRITE_ENABLED":
			key.Declaration.Rule.Type = schema.TypeBoolean
		case "HIKYO_SECOND_FACTOR":
			key.Declaration.Rule.Type = schema.TypeEnum
			key.Declaration.Rule.Members = []string{"required", "optional"}
		case "HIKYO_TLS_PROFILE":
			key.Declaration.Rule.Type = schema.TypeEnum
			key.Declaration.Rule.Members = []string{config.TLSProfileCompat, config.TLSProfileStrict}
		}
		keys = append(keys, key)
	}
	keys = append(keys, text(config.ManagedNodeOverridesKey, "Versioned per-node configuration, including TLS private-key contents. Each admitted node requires its own exact entry.", true))
	keys = append(keys, text(config.ManagedBootstrapSourcesKey, "Installed database, root-key and upgrade-custody source aliases and enrolled singleton topology (HA mode and node identity). Changes require a reviewed controlled rollout and fresh authentication.", false))
	return keys
}
