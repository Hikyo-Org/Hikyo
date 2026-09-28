// Package filesync is the client-side library for Hikyo's generic file
// destination (#164): an authenticated client on the destination host pulls
// one revision of a server-side file target and atomically renders it to a
// locally configured directory. The server never learns the path; nothing in
// this package talks to the network. The CLI verbs (`hikyo file-sync
// render|doctor`) are thin wiring over it, the same split internal/compose
// keeps for the Compose path.
//
// # hikyo-file-sync.yaml, the client-local binding
//
// A NON-SECRET file the operator keeps on the destination host and passes with
// --config. It binds one server file target to one local directory:
//
//	version: 1
//	instance: https://hikyo.example.internal
//	org: org_...
//	project: prj_...
//	environment: env_...
//	target: ftg_...                 # the server file target
//	refresh:
//	  mode: oneshot                 # or poll: fetch until current, then exit
//	  interval: 30s                 # poll only
//	  timeout: 10m                  # poll only
//	snapshot:
//	  offline_serve: false
//	  max_age: 168h                 # downward-only override of 7 d
//	destination:
//	  directory: /run/app/secrets   # absolute, must exist, never created
//	  mode: "0600"                  # file mode; group read is the widest allowed
//	  owner: ""                     # uid or user name; refused if chown fails
//	  group: ""                     # gid or group name
//	  require_tmpfs: false          # refuse unless the directory is tmpfs (Linux)
//	  on_removed: refuse            # refuse | retain | prune a dropped file
//	files:
//	  - name: app.env
//	    format: dotenv              # dotenv | json | yaml | raw
//	    keys: [DATABASE_URL, API_TOKEN]
//	    acknowledge_loader_control: []
//
// Parsing is STRICT (unknown fields are errors) and a credential key anywhere
// is refused: the token reaches the CLI through --token-file or HIKYO_TOKEN
// only, exactly as for hikyo-compose.yaml.
package filesync

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/schema"
	"gopkg.in/yaml.v3"
)

// Format is one supported rendering.
type Format string

const (
	FormatDotenv Format = "dotenv"
	FormatJSON   Format = "json"
	FormatYAML   Format = "yaml"
	FormatRaw    Format = "raw"
)

// RefreshMode is how one invocation converges. There is deliberately no
// resident watch mode (mvp-boundary: "Local agent daemon / resident watcher:
// Out"): continuous refresh is a systemd timer, cron or CronJob running the
// one-shot or poll mode.
type RefreshMode string

const (
	RefreshOneshot RefreshMode = "oneshot"
	RefreshPoll    RefreshMode = "poll"
)

// OnRemoved is the explicit decision for a file a previous generation
// published that the configuration no longer names.
type OnRemoved string

const (
	// RemovedRefuse fails the render, naming the file, until the operator
	// decides. It is the default because both other answers are irreversible
	// in their own way.
	RemovedRefuse OnRemoved = "refuse"
	// RemovedRetain leaves the last content in place as an unmanaged regular
	// file and forgets it.
	RemovedRetain OnRemoved = "retain"
	// RemovedPrune deletes the file, and only a file this client published.
	RemovedPrune OnRemoved = "prune"
)

const (
	// DefaultSnapshotMaxAge is the ops-spec hard maximum a config may only
	// lower (the Compose path's value).
	DefaultSnapshotMaxAge = 7 * 24 * time.Hour
	// DefaultPollInterval and DefaultPollTimeout bound the poll mode.
	DefaultPollInterval = 30 * time.Second
	DefaultPollTimeout  = 10 * time.Minute
	// MinPollInterval keeps a poll loop from becoming a fetch storm; the
	// per-principal fetch rate would refuse it anyway.
	MinPollInterval = 5 * time.Second
	// MaxFiles bounds one binding.
	MaxFiles = 64
	// DefaultMode is the file mode when the config names none.
	DefaultMode = 0o600
	// forbiddenModeBits is everything wider than owner read-write plus group
	// read: no execute bit, no group write, nothing for other.
	forbiddenModeBits = 0o137
)

// ConfigName is the conventional file name; the CLI requires --config anyway.
const ConfigName = "hikyo-file-sync.yaml"

// fileNameGrammar admits one path segment. A leading dot is refused so a file
// can never collide with the client's own `.hikyo` artifacts or be hidden.
var fileNameGrammar = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

// idGrammar is the repository id grammar (api/openapi.yaml).
var idGrammar = regexp.MustCompile(`^[a-z]{2,8}_[0-9a-fA-F-]{36}$`)

// Config is a parsed, validated hikyo-file-sync.yaml.
type Config struct {
	Version     int                 `yaml:"version"`
	Instance    string              `yaml:"instance"`
	Org         string              `yaml:"org"`
	Project     string              `yaml:"project"`
	Environment string              `yaml:"environment"`
	Target      string              `yaml:"target"`
	Refresh     RefreshSettings     `yaml:"refresh"`
	Snapshot    SnapshotSettings    `yaml:"snapshot"`
	Destination DestinationSettings `yaml:"destination"`
	Files       []File              `yaml:"files"`

	maxAge   time.Duration
	interval time.Duration
	timeout  time.Duration
	mode     uint32
}

// RefreshSettings selects the convergence mode.
type RefreshSettings struct {
	Mode     RefreshMode `yaml:"mode"`
	Interval string      `yaml:"interval"`
	Timeout  string      `yaml:"timeout"`
}

// SnapshotSettings is the offline snapshot policy.
type SnapshotSettings struct {
	OfflineServe bool   `yaml:"offline_serve"`
	MaxAge       string `yaml:"max_age"`
}

// DestinationSettings is the one local directory every file of the binding lands in.
type DestinationSettings struct {
	Directory    string    `yaml:"directory"`
	Mode         string    `yaml:"mode"`
	Owner        string    `yaml:"owner"`
	Group        string    `yaml:"group"`
	RequireTmpfs bool      `yaml:"require_tmpfs"`
	OnRemoved    OnRemoved `yaml:"on_removed"`
}

// File is one rendered file.
type File struct {
	Name                     string   `yaml:"name"`
	Format                   Format   `yaml:"format"`
	Keys                     []string `yaml:"keys"`
	AcknowledgeLoaderControl []string `yaml:"acknowledge_loader_control"`
}

// SnapshotMaxAge is the effective offline snapshot max age.
func (c *Config) SnapshotMaxAge() time.Duration { return c.maxAge }

// PollInterval and PollTimeout are the effective poll bounds.
func (c *Config) PollInterval() time.Duration { return c.interval }
func (c *Config) PollTimeout() time.Duration  { return c.timeout }

// FileMode is the effective file mode.
func (c *Config) FileMode() uint32 { return c.mode }

// KeyNames is the sorted, deduplicated union of every file's keys.
func (c *Config) KeyNames() []string {
	var out []string
	for _, f := range c.Files {
		out = append(out, f.Keys...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

var forbiddenKeys = map[string]struct{}{"token": {}, "token_file": {}, "credential": {}}

// ParseConfig strictly parses and validates a hikyo-file-sync.yaml document.
func ParseConfig(data []byte) (*Config, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigName, err)
	}
	if k := findForbiddenKey(&root); k != "" {
		return nil, fmt.Errorf("%s: %q is not allowed here; this file holds no credential, "+
			"pass the token via --token-file or HIKYO_TOKEN", ConfigName, k)
	}
	var c Config
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigName, err)
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigName, err)
	}
	return &c, nil
}

func (c *Config) validate() error {
	if c.Version != 1 {
		return fmt.Errorf("`version` must be 1, got %d", c.Version)
	}
	if err := validateOrigin(c.Instance); err != nil {
		return fmt.Errorf("`instance` %w", err)
	}
	for _, field := range []struct{ name, value string }{
		{"org", c.Org}, {"project", c.Project}, {"environment", c.Environment}, {"target", c.Target},
	} {
		if !idGrammar.MatchString(field.value) {
			return fmt.Errorf("`%s` %q is not a valid id", field.name, field.value)
		}
	}
	if !strings.HasPrefix(c.Target, "ftg_") {
		return fmt.Errorf("`target` %q is not a file target id (ftg_...)", c.Target)
	}
	if err := c.validateRefresh(); err != nil {
		return err
	}
	if err := c.validateSnapshot(); err != nil {
		return err
	}
	if err := c.validateDestination(); err != nil {
		return err
	}
	return c.validateFiles()
}

func (c *Config) validateRefresh() error {
	if c.Refresh.Mode == "" {
		c.Refresh.Mode = RefreshOneshot
	}
	switch c.Refresh.Mode {
	case RefreshOneshot:
		if c.Refresh.Interval != "" || c.Refresh.Timeout != "" {
			return errors.New("`refresh.interval` and `refresh.timeout` apply to `mode: poll` only")
		}
		return nil
	case RefreshPoll:
	default:
		return fmt.Errorf("`refresh.mode` %q must be oneshot or poll (there is no resident watch mode; run poll from a timer)", c.Refresh.Mode)
	}
	var err error
	if c.interval, err = durationOr(c.Refresh.Interval, DefaultPollInterval, "refresh.interval"); err != nil {
		return err
	}
	if c.interval < MinPollInterval {
		return fmt.Errorf("`refresh.interval` %s is below the %s minimum", c.interval, MinPollInterval)
	}
	if c.timeout, err = durationOr(c.Refresh.Timeout, DefaultPollTimeout, "refresh.timeout"); err != nil {
		return err
	}
	if c.timeout < c.interval {
		return fmt.Errorf("`refresh.timeout` %s is shorter than `refresh.interval` %s", c.timeout, c.interval)
	}
	return nil
}

func (c *Config) validateSnapshot() error {
	var err error
	if c.maxAge, err = durationOr(c.Snapshot.MaxAge, DefaultSnapshotMaxAge, "snapshot.max_age"); err != nil {
		return err
	}
	if c.maxAge > DefaultSnapshotMaxAge {
		return fmt.Errorf("`snapshot.max_age` %s exceeds the %s maximum; it may only lower it", c.maxAge, DefaultSnapshotMaxAge)
	}
	return nil
}

func (c *Config) validateDestination() error {
	d := &c.Destination
	if d.Directory == "" {
		return errors.New("`destination.directory` is required")
	}
	// The path must be written canonically: a `..` or a doubled separator is
	// refused rather than cleaned, so what the operator reviews is what is
	// opened.
	if !filepath.IsAbs(d.Directory) || filepath.Clean(d.Directory) != d.Directory {
		return fmt.Errorf("`destination.directory` %q must be an absolute, clean path", d.Directory)
	}
	if d.Directory == string(filepath.Separator) {
		return errors.New("`destination.directory` may not be the filesystem root")
	}
	c.mode = DefaultMode
	if d.Mode != "" {
		m, err := strconv.ParseUint(d.Mode, 8, 32)
		if err != nil || m > 0o777 {
			return fmt.Errorf("`destination.mode` %q is not an octal permission like \"0600\"", d.Mode)
		}
		if m&forbiddenModeBits != 0 {
			return fmt.Errorf("`destination.mode` %s is wider than 0640: no execute, group write or other access", d.Mode)
		}
		if m&0o400 == 0 {
			return fmt.Errorf("`destination.mode` %s leaves the owner unable to read", d.Mode)
		}
		c.mode = uint32(m)
	}
	switch d.OnRemoved {
	case "":
		d.OnRemoved = RemovedRefuse
	case RemovedRefuse, RemovedRetain, RemovedPrune:
	default:
		return fmt.Errorf("`destination.on_removed` %q must be refuse, retain or prune", d.OnRemoved)
	}
	for _, id := range []struct{ name, value string }{{"owner", d.Owner}, {"group", d.Group}} {
		if id.value != "" && strings.TrimSpace(id.value) != id.value {
			return fmt.Errorf("`destination.%s` %q carries surrounding whitespace", id.name, id.value)
		}
	}
	return nil
}

func (c *Config) validateFiles() error {
	if len(c.Files) == 0 {
		return errors.New("at least one entry is required under `files`")
	}
	if len(c.Files) > MaxFiles {
		return fmt.Errorf("`files` lists %d entries, at most %d are allowed", len(c.Files), MaxFiles)
	}
	names := map[string]struct{}{}
	for i := range c.Files {
		f := &c.Files[i]
		if !fileNameGrammar.MatchString(f.Name) {
			return fmt.Errorf("file name %q must be one path segment matching %s", f.Name, fileNameGrammar)
		}
		// A case-insensitive filesystem (the macOS default) would fold two
		// names into one directory entry.
		folded := strings.ToLower(f.Name)
		if _, dup := names[folded]; dup {
			return fmt.Errorf("file name %q is listed twice (names are compared case-insensitively)", f.Name)
		}
		names[folded] = struct{}{}
		switch f.Format {
		case FormatDotenv, FormatJSON, FormatYAML, FormatRaw:
		case "":
			return fmt.Errorf("file %q has no `format` (dotenv, json, yaml or raw)", f.Name)
		default:
			return fmt.Errorf("file %q format %q must be dotenv, json, yaml or raw", f.Name, f.Format)
		}
		if len(f.Keys) == 0 {
			return fmt.Errorf("file %q lists no `keys`", f.Name)
		}
		if f.Format == FormatRaw && len(f.Keys) != 1 {
			return fmt.Errorf("file %q is raw and must list exactly one key, got %d", f.Name, len(f.Keys))
		}
		seen := map[string]struct{}{}
		for _, k := range f.Keys {
			if err := schema.CheckKeyName(k); err != nil {
				return fmt.Errorf("file %q: %v", f.Name, err)
			}
			if _, dup := seen[k]; dup {
				return fmt.Errorf("file %q lists key %q twice", f.Name, k)
			}
			seen[k] = struct{}{}
		}
		if len(f.AcknowledgeLoaderControl) > 0 && f.Format != FormatDotenv {
			return fmt.Errorf("file %q: `acknowledge_loader_control` applies to dotenv files only", f.Name)
		}
		for _, k := range f.AcknowledgeLoaderControl {
			if _, ok := seen[k]; !ok {
				return fmt.Errorf("file %q acknowledges loader-control key %q it does not render", f.Name, k)
			}
		}
	}
	return nil
}

func durationOr(raw string, def time.Duration, field string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("`%s` %q is not a valid duration: %w", field, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("`%s` must be positive", field)
	}
	return d, nil
}

// validateOrigin mirrors the Compose config rule: an https origin, or loopback
// http for local development; a bare origin only.
func validateOrigin(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("is not a valid URL: %w", err)
	}
	if u.Host == "" {
		return errors.New("must include a host (an origin like https://hikyo.example)")
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("must be a bare origin (scheme + host), not %q", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("must be https (http is permitted only for loopback), got %q", raw)
	default:
		return fmt.Errorf("must use https, got scheme %q", u.Scheme)
	}
}

func findForbiddenKey(n *yaml.Node) string {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			if k := findForbiddenKey(c); k != "" {
				return k
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if _, bad := forbiddenKeys[n.Content[i].Value]; bad {
				return n.Content[i].Value
			}
			if k := findForbiddenKey(n.Content[i+1]); k != "" {
				return k
			}
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if k := findForbiddenKey(c); k != "" {
				return k
			}
		}
	}
	return ""
}
