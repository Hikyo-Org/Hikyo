package cli

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Per-instance CLI settings: typed, validated client preferences kept next to
// the trust store. They hold no credential and grant no authority, so they are
// plain 0600 state like contexts; a value the CLI does not know is refused,
// never ignored, so a typo cannot silently weaken a choice.

// ReauthMethod is how a reauthentication ceremony is carried out.
type ReauthMethod string

const (
	// ReauthAuto is the default: an authenticator code at the terminal first,
	// the browser approval page when a passkey is required or no code is possible.
	ReauthAuto ReauthMethod = "auto"
	// ReauthBrowser always uses the browser approval page.
	ReauthBrowser ReauthMethod = "browser"
)

// settingReauthMethod is the one key the settings file knows.
const settingReauthMethod = "reauth.method"

var settingKeys = []string{settingReauthMethod}

// ParseReauthMethod parses a setting value. The empty string is not a value.
func ParseReauthMethod(raw string) (ReauthMethod, error) {
	switch m := ReauthMethod(raw); m {
	case ReauthAuto, ReauthBrowser:
		return m, nil
	}
	return "", failf(ExitUsage, "%s must be auto or browser, got %q", settingReauthMethod, raw)
}

// InstanceSettings is one instance's stored settings. A zero value is the
// default. The pointer tells an absent member from an explicit empty value,
// which is invalid.
type InstanceSettings struct {
	ReauthMethod *string `json:"reauth.method,omitempty"`
}

func (s *State) settingsPath() string { return filepath.Join(s.dir, "settings.json") }

// Settings reads every instance's settings and refuses an unknown member or an
// invalid value anywhere in the file.
func (s *State) Settings() (map[string]InstanceSettings, error) {
	raw, err := os.ReadFile(s.settingsPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]InstanceSettings{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out map[string]InstanceSettings
	if err := decodeStrictDocument(bytes.NewReader(raw), &out); err != nil {
		return nil, fmt.Errorf("settings file at %s is unreadable: %w; known keys: %s", s.settingsPath(), err, strings.Join(settingKeys, ", "))
	}
	// A null member decodes as absent; an explicit null is as invalid as "".
	var members map[string]map[string]jsontext.Value
	if err := decodeStrictDocument(bytes.NewReader(raw), &members); err == nil {
		for instance, m := range members {
			if v, ok := m[settingReauthMethod]; ok && string(v) == "null" {
				return nil, fmt.Errorf("settings file at %s: instance %q: reauth.method must be auto or browser, got null", s.settingsPath(), instance)
			}
		}
	}
	for instance, settings := range out {
		if settings.ReauthMethod == nil {
			continue
		}
		if _, err := ParseReauthMethod(*settings.ReauthMethod); err != nil {
			return nil, fmt.Errorf("settings file at %s: instance %q: reauth.method must be auto or browser, got %q", s.settingsPath(), instance, *settings.ReauthMethod)
		}
	}
	if out == nil {
		out = map[string]InstanceSettings{}
	}
	return out, nil
}

// ReauthMethodFor is the instance's stored method, auto when none is stored.
func (s *State) ReauthMethodFor(instance string) (ReauthMethod, error) {
	all, err := s.Settings()
	if err != nil {
		return "", err
	}
	if m := all[instance].ReauthMethod; m != nil {
		return ReauthMethod(*m), nil
	}
	return ReauthAuto, nil
}

// SetReauthMethod stores the instance's method. Auto is the default, so it
// removes the member instead of recording it.
func (s *State) SetReauthMethod(instance string, m ReauthMethod) error {
	unlock, err := lockStateDir(s.dir)
	if err != nil {
		return err
	}
	defer unlock()
	all, err := s.Settings()
	if err != nil {
		return err
	}
	settings := all[instance]
	value := string(m)
	settings.ReauthMethod = &value
	if m == ReauthAuto {
		settings.ReauthMethod = nil
	}
	if settings == (InstanceSettings{}) {
		delete(all, instance)
	} else {
		all[instance] = settings
	}
	return s.writeJSON(s.settingsPath(), all)
}

// ForgetInstanceSettings drops an instance's stored settings, as when its trust
// entry is forgotten, so a later instance of the same name starts at defaults.
func (s *State) ForgetInstanceSettings(instance string) error {
	unlock, err := lockStateDir(s.dir)
	if err != nil {
		return err
	}
	defer unlock()
	all, err := s.Settings()
	if err != nil {
		return err
	}
	if _, ok := all[instance]; !ok {
		return nil
	}
	delete(all, instance)
	return s.writeJSON(s.settingsPath(), all)
}

// approveInBrowser reports whether a ceremony against this instance must use
// the browser: the per-invocation --approve-in-browser flag, or the instance's
// reauth.method setting.
func (s *State) approveInBrowser(instance string) (bool, error) {
	// Read the setting first so a damaged settings.json is refused even when the
	// flag alone would have decided.
	m, err := s.ReauthMethodFor(instance)
	if err != nil {
		return false, err
	}
	return s.forceBrowser || m == ReauthBrowser, nil
}

// readProof prompts for an account-security proof. These proofs are not
// reauthentication windows and have no browser handoff, so --approve-in-browser
// does not apply to them; say so rather than ignore it silently.
func (s *State) readProof(ios IO, prompt string) (string, error) {
	if s.forceBrowser {
		_, _ = fmt.Fprintln(ios.Stderr, "--approve-in-browser does not apply to this account-security proof; enter it here.")
	}
	return ios.readPassword(prompt)
}

func runConfig(_ context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("config", args, "get", "set")
	if err != nil {
		return err
	}
	st, flags, err := parseCommon("config "+sub, ios, rest, nil)
	if err != nil {
		return err
	}
	pos := flags.positionals
	if len(pos) == 0 || pos[0] != settingReauthMethod {
		return failf(ExitUsage, "usage: hikyo config get|set %s [auto|browser] [--instance REF]; known keys: %s", settingReauthMethod, strings.Join(settingKeys, ", "))
	}
	if sub == "get" && len(pos) != 1 {
		return failf(ExitUsage, "usage: hikyo config get %s [--instance REF]", settingReauthMethod)
	}
	var method ReauthMethod
	if sub == "set" {
		if len(pos) != 2 {
			return failf(ExitUsage, "usage: hikyo config set %s auto|browser [--instance REF]", settingReauthMethod)
		}
		if method, err = ParseReauthMethod(pos[1]); err != nil {
			return err
		}
	}
	resolved, err := Resolve(st, ios.Env, flags.Flags, ios.Workdir)
	if err != nil {
		return err
	}
	instance, err := instanceName(st, resolved)
	if err != nil {
		return err
	}
	if _, err := st.Trust().Lookup(instance); err != nil {
		return err
	}
	if sub == "get" {
		m, err := st.ReauthMethodFor(instance)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(ios.Stdout, m)
		return nil
	}
	return st.SetReauthMethod(instance, method)
}
