// Package securitystatus is the one-way channel from the security-maintenance
// coordinator to the serving application: a small public file the coordinator
// writes and the server only reads. The file is readable by every local user on a
// host and by everyone who can read the namespace's ConfigMaps in a cluster. It
// carries no credentials or operation records. It does carry what an instance
// administrator needs to act: the exact command in Action, whose confirmation
// value is a manifest digest or a recovery incarnation, which only proves that
// the person ran it knowingly, and Detail, the failure text after Redact.
package securitystatus

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
)

const (
	Format = "hikyo.security-maintenance-status/v1"
	// MaxBytes bounds what the server reads from a file it does not own.
	MaxBytes = 64 << 10
	maxIDs   = 2048
)

// Public is the whole file. Every field is display data for administrators.
type Public struct {
	Format            string    `json:"format"`
	Mode              string    `json:"mode,omitempty"`
	Channel           string    `json:"channel,omitempty"`
	Enrolled          bool      `json:"enrolled"`
	Phase             string    `json:"phase"`
	Reason            string    `json:"reason,omitempty"`
	ProtectionGap     bool      `json:"protection_gap"`
	Applicable        []string  `json:"applicable"`
	Coverage          []string  `json:"coverage"`
	CoveredComponents []string  `json:"covered_components"`
	CoverageThrough   string    `json:"coverage_through,omitempty"`
	CatalogSequence   uint64    `json:"catalog_sequence"`
	LastAttempt       time.Time `json:"last_attempt,omitzero"`
	LastVerified      time.Time `json:"last_verified,omitzero"`
	KnownUntil        time.Time `json:"known_until,omitzero"`
	RunningVersion    string    `json:"running_version,omitempty"`
	TargetVersion     string    `json:"target_version,omitempty"`
	Action            string    `json:"action,omitempty"`
	Detail            string    `json:"detail,omitempty"`
}

// Redact removes values from diagnostic text and keeps the reasons. Docker Compose
// echoes a malformed environment value in double quotes and other tools quote
// refused input, so failure text can carry secrets.
// shortcut: an unquoted, unlabeled short value still passes; upgrade if a tool
// prints one.
func Redact(text string) string {
	for _, scrubber := range scrubbers {
		text = scrubber.pattern.ReplaceAllString(text, scrubber.replacement)
	}
	return text
}

// Detail is failure text fit for the public file: scrubbed, then cut to max bytes
// at a character boundary.
func Detail(text string, max int) string {
	text = Redact(strings.ToValidUTF8(text, ""))
	if len(text) > max {
		text = strings.ToValidUTF8(text[:max], "")
	}
	return text
}

var scrubbers = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	// A quote not closed on its line opens a multiline value or one the length bound cut
	// short, and pairing it with a later quote could expose text, so the rest is redacted.
	{regexp.MustCompile(`"(?:[^"\n]*"|(?s:.*))`), `"[redacted]"`},
	// An apostrophe follows a letter or digit, so it never opens a value.
	{regexp.MustCompile(`(^|[^\pL\pN])'(?:[^'\n]*'|(?s:.*))`), `${1}'[redacted]'`},
	{regexp.MustCompile(`://[^/\s]*@`), `://[redacted]@`},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+\S+`), `$1 [redacted]`},
	{regexp.MustCompile(`(?i)\b([a-z0-9_.-]*(?:key|token|secret|password|passwd|pwd|credential|dsn)[a-z0-9_.-]*)\s*[=:]\s*\S+`), `$1=[redacted]`},
	{regexp.MustCompile(`\b([A-Z][A-Z0-9_]*)=\S+`), `$1=[redacted]`},
	{regexp.MustCompile(`[A-Za-z0-9+_=-]{32,}`), `[redacted]`},
}

// Report is what the server can honestly say: nothing configured or written
// yet, a file it cannot trust, or the coordinator's last published status.
type Report struct {
	Reported   bool
	Unreadable bool
	Status     Public
}

// Load never guesses: any failure other than an absent file is Unreadable.
func Load(path string) Report {
	if path == "" {
		return Report{}
	}
	status, err := read(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Report{}
	case err != nil:
		return Report{Unreadable: true}
	}
	return Report{Reported: true, Status: status}
}

func read(path string) (Public, error) {
	var status Public
	// Kubernetes projects a ConfigMap as a symlink into its own volume. What matters
	// is the content reached: a regular file within the size bound, opened as stat'd.
	info, err := os.Stat(path)
	if err != nil {
		return status, err
	}
	if !info.Mode().IsRegular() {
		return status, errors.New("security maintenance status is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return status, err
	}
	defer f.Close()
	if opened, err := f.Stat(); err != nil || !os.SameFile(info, opened) {
		return status, errors.New("security maintenance status changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return status, err
	}
	if len(raw) > MaxBytes {
		return status, errors.New("security maintenance status exceeds its size bound")
	}
	if err := definitions.DecodeStrict(raw, &status); err != nil {
		return status, err
	}
	if status.Format != Format || status.Phase == "" || len(status.Applicable) > maxIDs || len(status.Coverage) > maxIDs || len(status.CoveredComponents) > maxIDs {
		return status, errors.New("invalid security maintenance status")
	}
	return status, nil
}
