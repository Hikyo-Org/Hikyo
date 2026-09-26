package filesync

import (
	"strings"
	"testing"
	"time"
)

const validConfig = `version: 1
instance: https://hikyo.example
org: org_00000000-0000-0000-0000-000000000001
project: prj_00000000-0000-0000-0000-000000000002
environment: env_00000000-0000-0000-0000-000000000003
target: ftg_00000000-0000-0000-0000-000000000004
destination:
  directory: /run/app/secrets
files:
  - name: app.env
    format: dotenv
    keys: [DATABASE_URL, API_TOKEN]
  - name: db-password
    format: raw
    keys: [DB_PASSWORD]
`

func TestParseConfigDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := ParseConfig([]byte(validConfig))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Refresh.Mode != RefreshOneshot || cfg.FileMode() != 0o600 || cfg.Destination.OnRemoved != RemovedRefuse {
		t.Fatalf("defaults = %+v mode %o", cfg.Refresh, cfg.FileMode())
	}
	if cfg.SnapshotMaxAge() != DefaultSnapshotMaxAge {
		t.Fatalf("max age = %s", cfg.SnapshotMaxAge())
	}
	if got := strings.Join(cfg.KeyNames(), ","); got != "API_TOKEN,DATABASE_URL,DB_PASSWORD" {
		t.Fatalf("key names = %s", got)
	}
}

func TestParseConfigPoll(t *testing.T) {
	t.Parallel()
	cfg, err := ParseConfig([]byte(validConfig + "refresh:\n  mode: poll\n  interval: 10s\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval() != 10*time.Second || cfg.PollTimeout() != DefaultPollTimeout {
		t.Fatalf("poll = %s/%s", cfg.PollInterval(), cfg.PollTimeout())
	}
}

func TestParseConfigRefusals(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ from, to, want string }{
		"credential":        {"version: 1\n", "version: 1\ntoken: hik_x\n", "holds no credential"},
		"nested credential": {"  directory: /run/app/secrets\n", "  directory: /run/app/secrets\n  token_file: /x\n", "holds no credential"},
		"unknown field":     {"version: 1\n", "version: 1\nextra: 1\n", "field extra not found"},
		"relative dir":      {"directory: /run/app/secrets", "directory: run/app", "absolute, clean path"},
		"unclean dir":       {"directory: /run/app/secrets", "directory: /run/app/../secrets", "absolute, clean path"},
		"root dir":          {"directory: /run/app/secrets", "directory: /", "filesystem root"},
		"raw two keys":      {"keys: [DB_PASSWORD]", "keys: [DB_PASSWORD, API_TOKEN]", "exactly one key"},
		"bad format":        {"format: raw", "format: toml", "must be dotenv, json, yaml or raw"},
		"bad key":           {"keys: [DB_PASSWORD]", "keys: [db-password]", "canonical grammar"},
		"dup key":           {"keys: [DATABASE_URL, API_TOKEN]", "keys: [API_TOKEN, API_TOKEN]", "twice"},
		"traversal name":    {"name: db-password", "name: ../x", "one path segment"},
		"hidden name":       {"name: db-password", "name: .hikyo-gen", "one path segment"},
		"case dup name":     {"name: db-password", "name: APP.env", "listed twice"},
		"wide mode":         {"directory: /run/app/secrets\n", "directory: /run/app/secrets\n  mode: \"0644\"\n", "wider than 0640"},
		"exec mode":         {"directory: /run/app/secrets\n", "directory: /run/app/secrets\n  mode: \"0700\"\n", "wider than 0640"},
		"watch mode":        {"version: 1\n", "version: 1\nrefresh:\n  mode: watch\n", "no resident watch mode"},
		"oneshot interval":  {"version: 1\n", "version: 1\nrefresh:\n  interval: 5s\n", "apply to `mode: poll` only"},
		"fast poll":         {"version: 1\n", "version: 1\nrefresh:\n  mode: poll\n  interval: 1s\n", "below the"},
		"long max age":      {"version: 1\n", "version: 1\nsnapshot:\n  max_age: 200h\n", "may only lower it"},
		"http instance":     {"https://hikyo.example", "http://hikyo.example", "must be https"},
		"non-file target":   {"target: ftg_", "target: tgt_", "not a file target id"},
		"bad on_removed":    {"directory: /run/app/secrets\n", "directory: /run/app/secrets\n  on_removed: keep\n", "refuse, retain or prune"},
		"ack non-dotenv":    {"keys: [DB_PASSWORD]\n", "keys: [DB_PASSWORD]\n    acknowledge_loader_control: [DB_PASSWORD]\n", "dotenv files only"},
		"ack unrendered":    {"keys: [DATABASE_URL, API_TOKEN]\n", "keys: [DATABASE_URL, API_TOKEN]\n    acknowledge_loader_control: [PATH]\n", "does not render"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := strings.Replace(validConfig, tc.from, tc.to, 1)
			if doc == validConfig {
				t.Fatalf("case did not modify the document")
			}
			_, err := ParseConfig([]byte(doc))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestResolvePolicyNumericIDs(t *testing.T) {
	t.Parallel()
	cfg, err := ParseConfig([]byte(strings.Replace(validConfig, "directory: /run/app/secrets\n",
		"directory: /run/app/secrets\n  mode: \"0640\"\n  owner: \"1000\"\n  group: \"1001\"\n", 1)))
	if err != nil {
		t.Fatal(err)
	}
	p, err := ResolvePolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.UID != 1000 || p.GID != 1001 || p.Mode != 0o640 {
		t.Fatalf("policy = %+v", p)
	}
	cfg.Destination.Owner = "no-such-user-hikyo"
	if _, err := ResolvePolicy(cfg); err == nil {
		t.Fatal("unknown owner resolved")
	}
}
