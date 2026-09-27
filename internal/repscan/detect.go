package repscan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"

	"github.com/Hikyo-Org/hikyo/internal/scanning"
)

// Finding is one redacted local finding. It carries no matched byte, offset,
// length or excerpt, the same non-disclosure shape as the server's finding.
type Finding struct {
	RuleID     string `json:"rule_id"`
	RuleDigest string `json:"rule_digest"`
	Path       string `json:"path"`
	// Line is the 1-based line the match starts on. 0 means the ruleset
	// matched the content as a whole but no single line range reproduced it.
	Line int `json:"line"`
	// Commit is the commit that introduced the content; empty for the working
	// tree and the index.
	Commit      string `json:"commit,omitempty"`
	Fingerprint string `json:"fingerprint"`
	// Suppressed is "inline" or "fingerprint" when a reviewed suppression
	// covers the finding, empty otherwise.
	Suppressed string `json:"suppressed,omitempty"`
}

// Suppression kinds.
const (
	SuppressedInline      = "inline"
	SuppressedFingerprint = "fingerprint"
)

// worktreeScope is the fingerprint scope of content that is not (yet) a
// commit: the working tree and the index share it, so a finding a pre-commit
// run suppresses is the finding the next working-tree run sees.
const worktreeScope = "worktree"

// Fingerprint is the stable finding identity:
//
//	hex(sha256(rule semantic digest ‖ 0x00 ‖ path ‖ 0x00 ‖ line ‖ 0x00 ‖ scope))
//
// where scope is the commit id, or "worktree". It hashes no matched byte: a
// digest of a low-entropy token would be an offline cracking oracle, and the
// server's keyed value fingerprint is not available off the server by design.
// Moving the line or the file changes the fingerprint, as does a ruleset bump
// that changes the rule's semantics.
func Fingerprint(ruleDigest, relPath string, line int, scope string) string {
	h := sha256.New()
	h.Write([]byte(ruleDigest))
	h.Write([]byte{0})
	h.Write([]byte(relPath))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(line)))
	h.Write([]byte{0})
	h.Write([]byte(scope))
	return hex.EncodeToString(h.Sum(nil))
}

// inlineDirective is the one inline suppression: it names exactly one rule
// and covers only the line it sits on.
var inlineDirective = regexp.MustCompile(`hikyo-scan:ignore[ \t]+([a-z0-9][a-z0-9-]*)`)

// detector runs the ruleset over one item and localizes each matched rule.
type detector struct {
	rules   *scanning.Ruleset
	singles map[string]*scanning.Ruleset
}

func newDetector(rules *scanning.Ruleset) (*detector, error) {
	d := &detector{rules: rules, singles: map[string]*scanning.Ruleset{}}
	for _, id := range rules.RuleIDs() {
		single, err := rules.Subset([]string{id})
		if err != nil {
			return nil, err
		}
		d.singles[id] = single
	}
	return d, nil
}

// match is one localized rule hit before identity is assigned.
type match struct {
	ruleID string
	line   int
	inline bool
}

// detect returns every (rule, start line) hit in data. The verdict is the
// ruleset's own over the whole content; localization only assigns lines, by
// re-running the one matched rule over line-aligned windows. Windows start
// and end on line boundaries, so boundary-sensitive rules see the same
// neighbouring bytes they see in the whole content.
func (d *detector) detect(ctx context.Context, data []byte) ([]match, error) {
	found, err := d.rules.Scan(ctx, data)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	starts := lineStarts(data)
	var out []match
	for _, f := range found {
		lines, err := locate(ctx, d.singles[f.RuleID], data, starts)
		if err != nil {
			return nil, err
		}
		if len(lines) == 0 {
			out = append(out, match{ruleID: f.RuleID})
			continue
		}
		for _, line := range lines {
			out = append(out, match{
				ruleID: f.RuleID,
				line:   line,
				inline: inlineSuppresses(lineText(data, starts, line-1), f.RuleID),
			})
		}
	}
	return out, nil
}

func lineStarts(data []byte) []int {
	starts := []int{0}
	for i, b := range data {
		if b == '\n' && i+1 < len(data) {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func lineEnd(data []byte, starts []int, i int) int {
	if i+1 < len(starts) {
		return starts[i+1]
	}
	return len(data)
}

func lineText(data []byte, starts []int, i int) []byte {
	return data[starts[i]:lineEnd(data, starts, i)]
}

// locate returns the 1-based start line of each match of the single-rule
// ruleset. For each match it binary-searches the shortest line-aligned prefix
// that matches (the end line), then the latest start line that still matches
// up to that end, and resumes after that start line. The end line is found by
// galloping forward from the current start (windows of 1, 2, 4, ... lines)
// and then binary-searching the last bracket, so each finding costs work
// proportional to its distance from the previous one, not to the file size;
// only the final, match-free tail is scanned to the end once.
func locate(ctx context.Context, single *scanning.Ruleset, data []byte, starts []int) ([]int, error) {
	n := len(starts)
	var scanErr error
	matches := func(from, to int) bool {
		if scanErr != nil {
			return false
		}
		found, err := single.Scan(ctx, data[starts[from]:lineEnd(data, starts, to)])
		if err != nil {
			scanErr = err
			return false
		}
		return len(found) > 0
	}
	var lines []int
	for from := 0; from < n; {
		prev, hi := from-1, -1
		for step := 1; ; step *= 2 {
			cand := min(from+step-1, n-1)
			if matches(from, cand) {
				hi = cand
				break
			}
			if cand == n-1 {
				break
			}
			prev = cand
		}
		if hi < 0 {
			break
		}
		lo := prev + 1
		for lo < hi {
			mid := lo + (hi-lo)/2
			if matches(from, mid) {
				hi = mid
			} else {
				lo = mid + 1
			}
		}
		end := lo
		lo, hi = from, end
		for lo < hi {
			mid := lo + (hi-lo+1)/2
			if matches(mid, end) {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		lines = append(lines, lo+1)
		from = lo + 1
	}
	if scanErr != nil {
		return nil, scanErr
	}
	return lines, nil
}

func inlineSuppresses(line []byte, ruleID string) bool {
	for _, m := range inlineDirective.FindAllSubmatch(line, -1) {
		if bytes.Equal(m[1], []byte(ruleID)) {
			return true
		}
	}
	return false
}
