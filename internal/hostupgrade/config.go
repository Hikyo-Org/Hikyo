// Package hostupgrade provides the bounded, operator-invoked systemd adapter.
// It never accepts a shell command, service name, or destination from a release.
package hostupgrade

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const ConfigPath = "/etc/hikyo/upgrade.json"

// Config is operator-owned deployment configuration, never release metadata.
type Config struct {
	Unit                  string `json:"unit"`
	User                  string `json:"user"`
	Group                 string `json:"group"`
	WorkingDirectory      string `json:"working_directory"`
	EnvironmentFile       string `json:"environment_file"`
	RootKeyFile           string `json:"root_key_file"`
	Binary                string `json:"binary"`
	HealthURL             string `json:"health_url"`
	PublicDirectory       string `json:"public_directory"`
	InstallationDirectory string `json:"installation_directory"`
	StateDirectory        string `json:"state_directory"`
	CustodyDirectory      string `json:"custody_directory"`
	CandidateDirectory    string `json:"candidate_directory"`
	Principal             string `json:"principal,omitempty"`
	Project               string `json:"project,omitempty"`
}

func DefaultConfig() Config {
	return Config{Unit: "hikyo.service", User: "hikyo", Group: "hikyo", WorkingDirectory: "/var/lib/hikyo", EnvironmentFile: "/etc/hikyo/hikyo.env", RootKeyFile: "/etc/hikyo/root.key", Binary: "/usr/bin/hikyo", HealthURL: "http://127.0.0.1:8081/readyz", PublicDirectory: "/var/lib/hikyo-upgrade", InstallationDirectory: "/var/lib/hikyo-installation", StateDirectory: "/var/lib/hikyo-upgrader", CustodyDirectory: "/etc/hikyo/upgrade-keys", CandidateDirectory: "/var/lib/hikyo-upgrade-candidates"}
}

var namePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,63}$`)
var pathPattern = regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`)

func (c Config) Validate() error {
	if !strings.HasPrefix(c.Unit, "hikyo") || !strings.HasSuffix(c.Unit, ".service") || !namePattern.MatchString(strings.TrimSuffix(c.Unit, ".service")) {
		return errors.New("host upgrade requires a hikyo systemd service unit")
	}
	if !namePattern.MatchString(c.User) || !namePattern.MatchString(c.Group) || c.User == "root" || c.Group == "root" {
		return errors.New("host upgrade requires an unprivileged runtime user and group")
	}
	paths := []string{c.WorkingDirectory, c.EnvironmentFile, c.RootKeyFile, c.Binary, c.PublicDirectory, c.InstallationDirectory, c.StateDirectory, c.CustodyDirectory, c.CandidateDirectory}
	for _, p := range paths {
		if !safePath(p) {
			return fmt.Errorf("invalid host upgrade path %q", p)
		}
	}
	// Runtime-writable directories cannot contain privileged controller files.
	for _, private := range []string{c.StateDirectory, c.CustodyDirectory, c.CandidateDirectory, c.EnvironmentFile, c.RootKeyFile, c.Binary} {
		for _, public := range []string{c.WorkingDirectory, c.PublicDirectory, c.InstallationDirectory} {
			if within(public, private) {
				return fmt.Errorf("privileged path %s is inside runtime-writable %s", private, public)
			}
		}
	}
	for i, p := range paths {
		for j, q := range paths {
			if i != j && p == q {
				return errors.New("host upgrade paths must be distinct")
			}
		}
	}
	directories := []string{c.WorkingDirectory, c.PublicDirectory, c.InstallationDirectory, c.StateDirectory, c.CustodyDirectory, c.CandidateDirectory}
	for i, p := range directories {
		for j, q := range directories {
			if i != j && within(p, q) {
				return errors.New("upgrade directories must not contain one another")
			}
		}
	}
	u, err := url.Parse(c.HealthURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/readyz" {
		return errors.New("health URL must be a loopback HTTP /readyz endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return errors.New("health URL must use a literal loopback IP address")
	}
	return nil
}

func safePath(p string) bool {
	return len(p) > 1 && filepath.IsAbs(p) && filepath.Clean(p) == p && pathPattern.MatchString(p)
}
func within(base, p string) bool {
	return p == base || strings.HasPrefix(p, base+string(filepath.Separator))
}

// LoadConfig refuses unknown fields, symbolic links, and writable authority.
func LoadConfig(path string) (Config, error) {
	var c Config
	if err := trustedFile(path); err != nil {
		return c, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if len(b) > 65536 {
		return c, errors.New("host upgrade config exceeds 64 KiB")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, fmt.Errorf("host upgrade config: %w", err)
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return c, errors.New("host upgrade config contains trailing JSON")
	}
	return c, c.Validate()
}

// InitializeConfig creates the default configuration once, without replacing
// existing operator configuration. It does not start or stop the deployment.
func InitializeConfig(path string) (Config, error) {
	if os.Geteuid() != 0 {
		return Config{}, errors.New("host upgrade initialization requires root")
	}
	if _, err := os.Lstat(path); err == nil {
		return LoadConfig(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	if err := trustedDirectory(filepath.Dir(path)); err != nil {
		return Config{}, err
	}
	c := DefaultConfig()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return c, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return c, err
	}
	_, writeErr := f.Write(append(b, '\n'))
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return c, err
	}
	return c, syncDirectory(filepath.Dir(path))
}

// ParseEnvironmentFile implements a deliberately bounded subset of systemd
// EnvironmentFile syntax: one assignment per line, optional whole-value quotes,
// no continuations or escapes. Unsupported syntax fails instead of being
// interpreted differently by the updater and the service. No shell is used.
func ParseEnvironmentFile(data []byte) (map[string]string, error) {
	if len(data) > 1<<20 {
		return nil, errors.New("environment file exceeds 1 MiB")
	}
	values := make(map[string]string)
	for n, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || !envName.MatchString(key) || strings.ContainsAny(value, "\\\x00\r") {
			return nil, fmt.Errorf("unsupported environment syntax on line %d", n+1)
		}
		if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
			quote := value[0]
			if len(value) < 2 || value[len(value)-1] != quote || strings.ContainsRune(value[1:len(value)-1], rune(quote)) {
				return nil, fmt.Errorf("unsupported environment quoting on line %d", n+1)
			}
			value = value[1 : len(value)-1]
		} else if strings.ContainsAny(value, "\"'") {
			return nil, fmt.Errorf("unsupported environment quoting on line %d", n+1)
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("duplicate environment variable %s", key)
		}
		values[key] = value
	}
	return values, nil
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
