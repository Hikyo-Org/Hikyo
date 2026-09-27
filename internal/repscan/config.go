package repscan

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultConfigName is the configuration a scan reads from the scan base
// (the repository top level, or the working directory outside a repository)
// when no --config is given.
const DefaultConfigName = ".hikyo-scan.toml"

// configVersion is the one configuration grammar this build reads.
const configVersion = 1

// Config is the reviewed, parsed scan configuration. It can only narrow what
// the vendored ruleset reports: there is no field that adds a rule.
type Config struct {
	ExcludePaths []string
	ExcludeRules []string
	Suppressions map[string]Suppression
}

// Suppression silences one fingerprint. The reason is mandatory so every
// suppression is reviewable; Expires (a date, inclusive) is optional.
type Suppression struct {
	Fingerprint string
	Reason      string
	Expires     string // YYYY-MM-DD, empty when it never expires
}

type fileConfig struct {
	Version      int               `toml:"version"`
	ExcludePaths []string          `toml:"exclude_paths"`
	ExcludeRules []string          `toml:"exclude_rules"`
	Suppressions []fileSuppression `toml:"suppressions"`
}

type fileSuppression struct {
	Fingerprint string    `toml:"fingerprint"`
	Reason      string    `toml:"reason"`
	Expires     time.Time `toml:"expires"`
}

var fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ParseConfig parses a scan configuration, failing closed: an unknown key, a
// missing or unknown version, an unsafe path pattern, a malformed fingerprint,
// a suppression without a reason, or a duplicate is refused by name.
//
// suppressionsOnly restricts the file to `version` and `[[suppressions]]`, for
// a dedicated suppressions file.
func ParseConfig(data []byte, name string, suppressionsOnly bool) (Config, error) {
	var raw fileConfig
	md, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&raw)
	if err != nil {
		return Config{}, configErrorf("%s: %v", name, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return Config{}, configErrorf("%s: unknown key(s): %s", name, strings.Join(keys, ", "))
	}
	if !md.IsDefined("version") {
		return Config{}, configErrorf("%s: missing version (this build reads version = %d)", name, configVersion)
	}
	if raw.Version != configVersion {
		return Config{}, configErrorf("%s: unsupported version %d (this build reads version = %d)", name, raw.Version, configVersion)
	}
	if suppressionsOnly && (md.IsDefined("exclude_paths") || md.IsDefined("exclude_rules")) {
		return Config{}, configErrorf("%s: a suppressions file carries only version and [[suppressions]]", name)
	}
	cfg := Config{Suppressions: map[string]Suppression{}}
	for _, p := range raw.ExcludePaths {
		if err := validateGlob(p); err != nil {
			return Config{}, configErrorf("%s: exclude_paths %q: %v", name, p, err)
		}
		cfg.ExcludePaths = append(cfg.ExcludePaths, p)
	}
	for _, id := range raw.ExcludeRules {
		if slices.Contains(cfg.ExcludeRules, id) {
			return Config{}, configErrorf("%s: exclude_rules lists %q twice", name, id)
		}
		cfg.ExcludeRules = append(cfg.ExcludeRules, id)
	}
	for i, s := range raw.Suppressions {
		if !fingerprintPattern.MatchString(s.Fingerprint) {
			return Config{}, configErrorf("%s: suppressions[%d]: fingerprint must be 64 lowercase hex characters", name, i)
		}
		if strings.TrimSpace(s.Reason) == "" {
			return Config{}, configErrorf("%s: suppressions[%d]: a reason is required", name, i)
		}
		if _, dup := cfg.Suppressions[s.Fingerprint]; dup {
			return Config{}, configErrorf("%s: suppressions[%d]: fingerprint %s is suppressed twice", name, i, s.Fingerprint)
		}
		entry := Suppression{Fingerprint: s.Fingerprint, Reason: s.Reason}
		if !s.Expires.IsZero() {
			entry.Expires = s.Expires.Format(time.DateOnly)
		}
		cfg.Suppressions[s.Fingerprint] = entry
	}
	return cfg, nil
}

// Merge combines two configurations. A fingerprint suppressed in both, or a
// rule excluded in both, is refused so the reviewed record stays single.
func (c Config) Merge(o Config) (Config, error) {
	out := Config{
		ExcludePaths: append(slices.Clone(c.ExcludePaths), o.ExcludePaths...),
		ExcludeRules: slices.Clone(c.ExcludeRules),
		Suppressions: make(map[string]Suppression, len(c.Suppressions)+len(o.Suppressions)),
	}
	for _, id := range o.ExcludeRules {
		if slices.Contains(out.ExcludeRules, id) {
			return Config{}, configErrorf("rule %q is excluded twice", id)
		}
		out.ExcludeRules = append(out.ExcludeRules, id)
	}
	for fp, s := range c.Suppressions {
		out.Suppressions[fp] = s
	}
	for fp, s := range o.Suppressions {
		if _, dup := out.Suppressions[fp]; dup {
			return Config{}, configErrorf("fingerprint %s is suppressed twice", fp)
		}
		out.Suppressions[fp] = s
	}
	return out, nil
}

// active reports whether a suppression applies on the given day.
func (s Suppression) active(today string) bool {
	return s.Expires == "" || today <= s.Expires
}

// validateGlob accepts a slash-separated, repository-relative pattern whose
// segments are path.Match patterns or `**` (any number of segments). A pattern
// cannot be absolute or name a parent, so an exclusion cannot reach outside the
// scanned root.
func validateGlob(pattern string) error {
	switch {
	case pattern == "":
		return fmt.Errorf("empty pattern")
	case strings.HasPrefix(pattern, "/"):
		return fmt.Errorf("must be relative to the scan base")
	case strings.Contains(pattern, `\`):
		return fmt.Errorf("use / as the separator")
	}
	for _, seg := range strings.Split(pattern, "/") {
		switch seg {
		case "":
			return fmt.Errorf("empty path segment")
		case ".", "..":
			return fmt.Errorf("segment %q is not allowed", seg)
		case "**":
			continue
		}
		if strings.Contains(seg, "**") {
			return fmt.Errorf("** must be a whole segment")
		}
		if _, err := path.Match(seg, ""); err != nil {
			return err
		}
	}
	return nil
}

// matchGlob reports whether the slash-separated relPath matches pattern.
func matchGlob(pattern, relPath string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(relPath, "/"))
}

func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			for i := 0; i <= len(name); i++ {
				if matchSegments(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

func excluded(patterns []string, relPath string) bool {
	for _, p := range patterns {
		if matchGlob(p, relPath) {
			return true
		}
	}
	return false
}
