// alert-route is an external, operator-run reference consumer. It is not linked
// into Hikyo and grants no collection authority. See README.md for launch holds.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"regexp"
	"time"
)

const (
	maxFile   = 32 << 20
	window    = 5 * time.Minute
	maxLag    = 15 * time.Minute
	retention = 24 * time.Hour
	maxEvents = 50000
	maxAlerts = 5000
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var labelPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
var envPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

type source struct {
	ID       string `json:"id"`
	Instance string `json:"instance"`
	Trail    string `json:"trail"`
	Org      string `json:"org,omitempty"`
	Project  string `json:"project,omitempty"`
	Database string `json:"database"`
}

type config struct {
	Enabled            bool     `json:"enabled"`
	Destination        string   `json:"destination"`
	Receiver           string   `json:"receiver"`
	TokenEnv           string   `json:"token_env"`
	EscalationOwner    string   `json:"escalation_owner"`
	Sources            []source `json:"sources"`
	WatchedCredentials []string `json:"watched_credentials"`
}

func (c config) validate() error {
	u, err := url.Parse(c.Destination)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("destination must be an explicit HTTPS URL without credentials, query or fragment")
	}
	if !c.Enabled || !labelPattern.MatchString(c.Receiver) || !labelPattern.MatchString(c.EscalationOwner) || !envPattern.MatchString(c.TokenEnv) || len(c.Sources) == 0 || len(c.Sources) > 16 || len(c.WatchedCredentials) > 100 {
		return errors.New("enabled configuration requires receiver, token_env, escalation_owner and 1..16 sources")
	}
	seen := map[string]bool{}
	for _, s := range c.Sources {
		if seen[s.ID] || !labelPattern.MatchString(s.ID) || !labelPattern.MatchString(s.Instance) || (s.Trail != "instance" && s.Trail != "tenant") || s.Database == "" {
			return errors.New("invalid or duplicate source")
		}
		if (s.Trail == "tenant" && !uuidPattern.MatchString(s.Org)) || (s.Trail == "instance" && (s.Org != "" || s.Project != "")) || (s.Project != "" && !uuidPattern.MatchString(s.Project)) {
			return errors.New("source must bind the exact trail and tenant scope")
		}
		seen[s.ID] = true
	}
	for _, id := range c.WatchedCredentials {
		if !uuidPattern.MatchString(id) {
			return errors.New("watched credentials must be UUIDs")
		}
	}
	return nil
}

func decode(data []byte, v interface{}, strict bool) error {
	d := json.NewDecoder(bytes.NewReader(data))
	if strict {
		d.DisallowUnknownFields()
	}
	if err := d.Decode(v); err != nil {
		return errors.New("invalid JSON contract")
	}
	if err := d.Decode(new(interface{})); err != io.EOF {
		return errors.New("expected one JSON document")
	}
	return nil
}

func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("input unavailable")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil || len(b) > maxFile {
		return nil, errors.New("input exceeds size limit or cannot be read")
	}
	return b, nil
}

func digest(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func identity(parts ...string) string {
	b, _ := json.Marshal(parts)
	return digest(b)
}
