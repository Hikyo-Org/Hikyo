// Package repscan is local repository and CI secret scanning (#153).
//
// It runs the same vendored, pinned ruleset the server's scan-on-value-entry
// runs (internal/scanning), over files, directories, staged or unstaged Git
// changes, the full Git history, or an explicit commit range. Nothing leaves the
// machine: there is no client, no network, and no finding upload.
//
// The non-disclosure discipline of the value-entry scanner carries over. A
// Finding names a rule, its semantic digest, a repository-relative path, a line
// and a fingerprint, and never any matched byte. The fingerprint is a hash over
// the rule digest and the location only, so it cannot be used as an oracle for
// the credential itself.
//
// Every dimension is bounded (files, bytes per file, total bytes, findings,
// commits, wall clock). Exceeding a budget refuses the scan by name: a scan
// that stops early is never reported as clean.
package repscan

import (
	"errors"
	"fmt"
	"time"
)

// Defaults for the scan budgets, recorded in the ops-spec bound registry (row
// 15a). Tests narrow them through Limits; the CLI exposes the wall clock alone.
const (
	// MaxFiles bounds the files (or history blobs) one scan inspects.
	MaxFiles = 50_000
	// MaxFileBytes is the largest file scanned. A larger file is skipped and
	// counted, never truncated: a partial read reported as scanned would hide
	// whatever sits past the cut.
	MaxFileBytes = 1 << 20
	// MaxTotalBytes bounds the bytes scanned across the whole run.
	MaxTotalBytes = 1 << 30
	// MaxFindings bounds the findings one run may produce, suppressed ones
	// included.
	MaxFindings = 10_000
	// MaxCommits bounds the commits a history or range scan walks.
	MaxCommits = 50_000
	// DefaultTimeout is the whole-run wall clock.
	DefaultTimeout = 10 * time.Minute
	// MaxConfigBytes bounds a scan configuration or suppressions file.
	MaxConfigBytes = 1 << 20
)

// Limits carries the budgets of one run.
type Limits struct {
	MaxFiles      int
	MaxFileBytes  int64
	MaxTotalBytes int64
	MaxFindings   int
	MaxCommits    int
	Timeout       time.Duration
}

// DefaultLimits returns the registry defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxFiles:      MaxFiles,
		MaxFileBytes:  MaxFileBytes,
		MaxTotalBytes: MaxTotalBytes,
		MaxFindings:   MaxFindings,
		MaxCommits:    MaxCommits,
		Timeout:       DefaultTimeout,
	}
}

func (l Limits) validate() error {
	if l.MaxFiles <= 0 || l.MaxFileBytes <= 0 || l.MaxTotalBytes <= 0 ||
		l.MaxFindings <= 0 || l.MaxCommits <= 0 || l.Timeout <= 0 {
		return configErrorf("every scan budget must be positive")
	}
	return nil
}

// Kind classifies a scan failure for the caller's exit-code mapping.
type Kind int

const (
	// KindConfig is invalid usage or configuration.
	KindConfig Kind = iota + 1
	// KindRefused is a scan the scanner declined or could not complete: a
	// budget exceeded, a malformed repository, an unreadable root.
	KindRefused
	// KindUnavailable is the git executable being absent.
	KindUnavailable
)

// Error is the only error type Run returns for an expected failure.
type Error struct {
	Kind Kind
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func configErrorf(format string, args ...any) error {
	return &Error{Kind: KindConfig, Err: fmt.Errorf(format, args...)}
}

func refusedf(format string, args ...any) error {
	return &Error{Kind: KindRefused, Err: fmt.Errorf(format, args...)}
}

// ErrorKind reports the Kind of err, defaulting to KindRefused: an unexpected
// failure is still a scan that did not complete, never a clean one.
func ErrorKind(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindRefused
}

// budget meters one run against its Limits. Every charge refuses by name.
type budget struct {
	limits   Limits
	files    int
	bytes    int64
	findings int
}

func (b *budget) file() error {
	b.files++
	if b.files > b.limits.MaxFiles {
		return refusedf("scan refused: more than %d files; narrow the paths, the range, or exclude_paths", b.limits.MaxFiles)
	}
	return nil
}

func (b *budget) scanned(n int) error {
	b.bytes += int64(n)
	if b.bytes > b.limits.MaxTotalBytes {
		return refusedf("scan refused: more than %d bytes scanned in total; narrow the paths or the range", b.limits.MaxTotalBytes)
	}
	return nil
}

func (b *budget) finding() error {
	b.findings++
	if b.findings > b.limits.MaxFindings {
		return refusedf("scan refused: more than %d findings", b.limits.MaxFindings)
	}
	return nil
}
