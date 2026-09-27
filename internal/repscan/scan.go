package repscan

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/pathutil"
	"github.com/Hikyo-Org/hikyo/internal/scanning"
)

// Mode selects what one run scans.
type Mode string

const (
	// ModePaths scans named files and directories on disk.
	ModePaths Mode = "paths"
	// ModeStaged scans the index: exactly what the next commit would record.
	ModeStaged Mode = "staged"
	// ModeUnstaged scans working-tree changes not yet staged, untracked
	// (non-ignored) files included.
	ModeUnstaged Mode = "unstaged"
	// ModeHistory scans every blob every reachable commit introduced.
	ModeHistory Mode = "history"
	// ModeRange scans the blobs the commits of an explicit range introduced.
	ModeRange Mode = "range"
)

// ReportSchema names the JSON document shape. It is additive-only.
const ReportSchema = "hikyo.scan/v1"

// Options is one run's selection and configuration.
type Options struct {
	Mode Mode
	// Workdir resolves relative paths and is where git is run.
	Workdir string
	// Paths are the ModePaths roots; empty means the working directory.
	Paths []string
	// Range is the ModeRange revision range, A..B.
	Range string
	// ConfigFile is an explicit configuration; empty reads DefaultConfigName
	// from the scan base when present.
	ConfigFile string
	// SuppressionsFile is an additional suppressions-only file.
	SuppressionsFile string
	// ExcludePaths and ExcludeRules add to the configuration's exclusions.
	ExcludePaths []string
	ExcludeRules []string
	Limits       Limits
	// Git is the git executable; empty looks up "git" on PATH.
	Git string
	// Now is the clock suppression expiry reads; nil means time.Now.
	Now func() time.Time
}

// Report is the result of one run, and the `-o json` document.
type Report struct {
	Schema   string      `json:"schema"`
	Mode     Mode        `json:"mode"`
	Ruleset  RulesetInfo `json:"ruleset"`
	Findings []Finding   `json:"findings"`
	Summary  Summary     `json:"summary"`

	// reasons are the reviewed reasons of the suppressions that applied, for
	// SARIF's suppression justification.
	reasons map[string]string
}

// RulesetInfo is the provenance of the rules that ran.
type RulesetInfo struct {
	Snapshot      string     `json:"snapshot"`
	Rules         []RuleInfo `json:"rules"`
	ExcludedRules []string   `json:"excluded_rules"`
}

// RuleInfo is one active rule and its semantic digest.
type RuleInfo struct {
	ID             string `json:"id"`
	SemanticDigest string `json:"semantic_digest"`
}

// Summary is the run's coverage: what was scanned, what was skipped and why,
// and the budgets it ran under.
type Summary struct {
	FilesScanned        int        `json:"files_scanned"`
	BytesScanned        int64      `json:"bytes_scanned"`
	CommitsScanned      int        `json:"commits_scanned"`
	Findings            int        `json:"findings"`
	Suppressed          int        `json:"suppressed"`
	ExpiredSuppressions int        `json:"expired_suppressions"`
	Skipped             Skipped    `json:"skipped"`
	Limits              LimitsInfo `json:"limits"`
}

// LimitsInfo echoes the budgets so a report shows what bounded it.
type LimitsInfo struct {
	MaxFiles       int   `json:"max_files"`
	MaxFileBytes   int64 `json:"max_file_bytes"`
	MaxTotalBytes  int64 `json:"max_total_bytes"`
	MaxFindings    int   `json:"max_findings"`
	MaxCommits     int   `json:"max_commits"`
	TimeoutSeconds int64 `json:"timeout_seconds"`
}

// Unsuppressed returns the findings no suppression covers.
func (r *Report) Unsuppressed() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Suppressed == "" {
			out = append(out, f)
		}
	}
	return out
}

// rangePattern admits a revision range that cannot be read as an option and
// carries no whitespace or control character. It is passed after
// --end-of-options as well.
var rangePattern = regexp.MustCompile(`^[^-\s\x00-\x1f\x7f][^\s\x00-\x1f\x7f]*$`)

type run struct {
	opts    Options
	rules   *scanning.Ruleset
	det     *detector
	cfg     Config
	budget  budget
	report  *Report
	today   string
	seen    map[string]bool
	baseDir string
}

// Run executes one scan. The returned error is an *Error for every expected
// failure; findings are not an error.
func Run(ctx context.Context, rules *scanning.Ruleset, opts Options) (*Report, error) {
	if err := opts.Limits.validate(); err != nil {
		return nil, err
	}
	switch opts.Mode {
	case ModePaths:
	case ModeStaged, ModeUnstaged, ModeHistory:
		if len(opts.Paths) > 0 {
			return nil, configErrorf("--%s takes no paths", opts.Mode)
		}
	case ModeRange:
		if len(opts.Paths) > 0 {
			return nil, configErrorf("--range takes no paths")
		}
		if !rangePattern.MatchString(opts.Range) || !strings.Contains(opts.Range, "..") {
			return nil, configErrorf("--range must be a revision range A..B")
		}
	default:
		return nil, configErrorf("unknown scan mode %q", opts.Mode)
	}
	for _, p := range opts.ExcludePaths {
		if err := validateGlob(p); err != nil {
			return nil, configErrorf("--exclude-path %q: %v", p, err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Limits.Timeout)
	defer cancel()

	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	r := &run{
		opts:   opts,
		budget: budget{limits: opts.Limits},
		today:  now().UTC().Format(time.DateOnly),
		seen:   map[string]bool{},
	}

	var g git
	if opts.Mode != ModePaths {
		exe, err := lookGit(opts.Git)
		if err != nil {
			return nil, err
		}
		g = git{exe: exe, dir: opts.Workdir}
		if err := g.requireVersion(ctx); err != nil {
			return nil, err
		}
		top, err := g.toplevel(ctx)
		if err != nil {
			return nil, err
		}
		g.dir = top
		r.baseDir = top
	} else {
		r.baseDir = physical(findRepoBase(opts.Workdir))
	}

	if err := r.loadConfig(); err != nil {
		return nil, err
	}
	if err := r.selectRules(rules); err != nil {
		return nil, err
	}

	var err error
	switch opts.Mode {
	case ModePaths:
		err = r.scanPaths(ctx)
	case ModeStaged:
		err = r.scanStaged(ctx, g)
	case ModeUnstaged:
		err = r.scanUnstaged(ctx, g)
	case ModeHistory:
		err = r.scanCommits(ctx, g, "--all")
	case ModeRange:
		err = r.scanCommits(ctx, g, "--end-of-options", opts.Range)
	}
	if err != nil {
		var scanErr *Error
		if errors.As(err, &scanErr) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, timeoutRefusal(ctx.Err())
		}
		return nil, refusedf("scan refused: %v", err)
	}
	return r.report, nil
}

// findRepoBase is the nearest ancestor of dir holding a .git entry, or dir
// itself outside a repository. Paths are reported relative to it so a paths
// scan and a git-mode scan of the same file agree on its path, and so on its
// fingerprint.
func findRepoBase(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	for d := abs; ; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return abs
		}
		d = parent
	}
}

// physical resolves symlinks in a path the user selected (a named root, or the
// working directory), so a symlinked checkout is scanned rather than skipped
// and its paths are reported against the same physical base. Symlinks found
// while walking are never followed.
func physical(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

func (r *run) resolve(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(r.opts.Workdir, p)
}

func (r *run) loadConfig() error {
	cfg := Config{Suppressions: map[string]Suppression{}}
	configPath, explicit := r.opts.ConfigFile, r.opts.ConfigFile != ""
	if explicit {
		configPath = r.resolve(configPath)
	} else {
		configPath = filepath.Join(r.baseDir, DefaultConfigName)
	}
	if data, present, err := readConfigFile(configPath, explicit); err != nil {
		return err
	} else if present {
		if cfg, err = ParseConfig(data, filepath.Base(configPath), false); err != nil {
			return err
		}
	}
	if r.opts.SuppressionsFile != "" {
		p := r.resolve(r.opts.SuppressionsFile)
		data, _, err := readConfigFile(p, true)
		if err != nil {
			return err
		}
		extra, err := ParseConfig(data, filepath.Base(p), true)
		if err != nil {
			return err
		}
		if cfg, err = cfg.Merge(extra); err != nil {
			return err
		}
	}
	flags := Config{ExcludePaths: r.opts.ExcludePaths, ExcludeRules: r.opts.ExcludeRules}
	merged, err := cfg.Merge(flags)
	if err != nil {
		return err
	}
	r.cfg = merged
	return nil
}

// readConfigFile reads a bounded regular configuration file. A missing
// default file is simply absent; a missing explicit one is a usage error.
func readConfigFile(p string, explicit bool) ([]byte, bool, error) {
	info, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !explicit {
			return nil, false, nil
		}
		return nil, false, configErrorf("reading scan configuration %s: %v", p, err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, configErrorf("scan configuration %s is not a regular file", p)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, false, configErrorf("reading scan configuration %s: %v", p, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxConfigBytes+1))
	if err != nil {
		return nil, false, configErrorf("reading scan configuration %s: %v", p, err)
	}
	if len(data) > MaxConfigBytes {
		return nil, false, configErrorf("scan configuration %s exceeds %d bytes", p, MaxConfigBytes)
	}
	return data, true, nil
}

// selectRules narrows the ruleset by the reviewed exclusions. Every excluded
// id must exist, and at least one rule must remain: a configuration cannot
// quietly turn the scanner into one that finds nothing.
func (r *run) selectRules(rules *scanning.Ruleset) error {
	all := rules.RuleIDs()
	drop := map[string]bool{}
	for _, id := range r.cfg.ExcludeRules {
		if _, ok := rules.SemanticDigest(id); !ok {
			return configErrorf("cannot exclude unknown rule %q", id)
		}
		drop[id] = true
	}
	var keep []string
	for _, id := range all {
		if !drop[id] {
			keep = append(keep, id)
		}
	}
	if len(keep) == 0 {
		return configErrorf("the exclusions remove every rule; at least one rule must run")
	}
	active, err := rules.Subset(keep)
	if err != nil {
		return refusedf("scan refused: %v", err)
	}
	det, err := newDetector(active)
	if err != nil {
		return refusedf("scan refused: %v", err)
	}
	r.rules, r.det = active, det

	info := RulesetInfo{Snapshot: active.SnapshotVersion(), ExcludedRules: []string{}}
	for _, id := range keep {
		d, _ := active.SemanticDigest(id)
		info.Rules = append(info.Rules, RuleInfo{ID: id, SemanticDigest: d})
	}
	info.ExcludedRules = append(info.ExcludedRules, r.cfg.ExcludeRules...)
	sort.Strings(info.ExcludedRules)
	l := r.opts.Limits
	r.report = &Report{
		Schema:   ReportSchema,
		Mode:     r.opts.Mode,
		Ruleset:  info,
		Findings: []Finding{},
		reasons:  map[string]string{},
		Summary: Summary{Limits: LimitsInfo{
			MaxFiles: l.MaxFiles, MaxFileBytes: l.MaxFileBytes, MaxTotalBytes: l.MaxTotalBytes,
			MaxFindings: l.MaxFindings, MaxCommits: l.MaxCommits, TimeoutSeconds: int64(l.Timeout / time.Second),
		}},
	}
	return nil
}

// admit decides whether a listed path is scanned at all: exclusions are
// counted, and every admitted path is charged against the file budget.
func (r *run) admit(relPath string) (bool, error) {
	if excluded(r.cfg.ExcludePaths, relPath) {
		r.report.Summary.Skipped.Excluded++
		return false, nil
	}
	if err := r.budget.file(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *run) skip(reason skipReason) { r.report.Summary.Skipped.count(reason) }

// scanContent runs the detector over one item and records its findings.
func (r *run) scanContent(ctx context.Context, relPath, commit string, data []byte) error {
	if reason := classify(relPath, data); reason != scanIt {
		r.skip(reason)
		return nil
	}
	if err := r.budget.scanned(len(data)); err != nil {
		return err
	}
	r.report.Summary.FilesScanned++
	r.report.Summary.BytesScanned += int64(len(data))
	matches, err := r.det.detect(ctx, data)
	if err != nil {
		return err
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].line != matches[j].line {
			return matches[i].line < matches[j].line
		}
		return matches[i].ruleID < matches[j].ruleID
	})
	scope := commit
	if scope == "" {
		scope = worktreeScope
	}
	for _, m := range matches {
		if err := r.budget.finding(); err != nil {
			return err
		}
		digest, _ := r.rules.SemanticDigest(m.ruleID)
		f := Finding{
			RuleID: m.ruleID, RuleDigest: digest, Path: relPath, Line: m.line, Commit: commit,
			Fingerprint: Fingerprint(digest, relPath, m.line, scope),
		}
		switch s, ok := r.cfg.Suppressions[f.Fingerprint]; {
		case m.inline:
			f.Suppressed = SuppressedInline
		case ok && s.active(r.today):
			f.Suppressed = SuppressedFingerprint
			r.report.reasons[f.Fingerprint] = s.Reason
		case ok:
			r.report.Summary.ExpiredSuppressions++
		}
		if f.Suppressed != "" {
			r.report.Summary.Suppressed++
		} else {
			r.report.Summary.Findings++
		}
		r.report.Findings = append(r.report.Findings, f)
	}
	return nil
}

// displayPath is the slash-separated path relative to the scan base, or the
// cleaned absolute path when the file lies outside it.
func (r *run) displayPath(abs string) string {
	if pathutil.Within(r.baseDir, abs) {
		if rel, err := filepath.Rel(r.baseDir, abs); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filepath.Clean(abs))
}

// scanPaths walks each named root. A root the user named is resolved to its
// physical path; symlinks found inside it are never followed (each is
// counted), `.git` directories are not descended, and every read goes through
// an os.Root so no path, however it is mutated mid-walk, escapes the root.
func (r *run) scanPaths(ctx context.Context) error {
	paths := r.opts.Paths
	if len(paths) == 0 {
		paths = []string{"."}
	}
	for _, p := range paths {
		if _, err := os.Lstat(r.resolve(p)); err != nil {
			return configErrorf("cannot scan %s: %v", p, err)
		}
		abs := physical(r.resolve(p))
		info, err := os.Lstat(abs)
		if err != nil {
			return configErrorf("cannot scan %s: %v", p, err)
		}
		switch {
		case info.IsDir():
			if err := r.walkDir(ctx, abs); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := r.scanFileIn(ctx, filepath.Dir(abs), filepath.Base(abs)); err != nil {
				return err
			}
		default:
			r.skip(skipNotRegular)
		}
	}
	return nil
}

func (r *run) walkDir(ctx context.Context, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return refusedf("scan refused: cannot open %s: %v", dir, err)
	}
	defer root.Close()
	return fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				r.skip(skipVanished)
				return nil
			}
			return refusedf("scan refused: cannot read %s: %v", r.displayPath(filepath.Join(dir, filepath.FromSlash(p))), err)
		}
		if d.IsDir() {
			if p != "." && d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		return r.scanEntry(ctx, root, dir, p, d.Type())
	})
}

func (r *run) scanFileIn(ctx context.Context, dir, name string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return refusedf("scan refused: cannot open %s: %v", dir, err)
	}
	defer root.Close()
	return r.scanEntry(ctx, root, dir, name, 0)
}

func (r *run) scanEntry(ctx context.Context, root *os.Root, dir, slashRel string, typ fs.FileMode) error {
	display := r.displayPath(filepath.Join(dir, filepath.FromSlash(slashRel)))
	if r.seen[display] {
		return nil
	}
	r.seen[display] = true
	admitted, err := r.admit(display)
	if err != nil || !admitted {
		return err
	}
	if typ&fs.ModeSymlink != 0 {
		r.skip(skipSymlink)
		return nil
	}
	data, reason, err := readWithin(root, filepath.FromSlash(slashRel), r.opts.Limits.MaxFileBytes)
	if err != nil {
		return refusedf("scan refused: cannot read %s: %v", display, err)
	}
	if reason != scanIt {
		r.skip(reason)
		return nil
	}
	return r.scanContent(ctx, display, "", data)
}

// scanStaged scans the index blobs of every added, modified or type-changed
// path: what the next commit would record, independent of the working tree.
func (r *run) scanStaged(ctx context.Context, g git) error {
	var entries []blobEntry
	err := g.stream(ctx, "diff --cached", nil, func(out io.Reader) error {
		return parseRaw(out, func(e blobEntry) error {
			return r.collect(&entries, e)
		})
	}, "diff", "--cached", "--no-ext-diff", "--raw", "-z", "--no-renames", "--no-abbrev", "--diff-filter=AMT")
	if err != nil {
		return err
	}
	return r.readBlobs(ctx, g, entries)
}

// scanUnstaged scans working-tree content that differs from the index, and
// untracked files git does not ignore, reading them through an os.Root at the
// repository top level.
func (r *run) scanUnstaged(ctx context.Context, g git) error {
	var err error
	g, err = g.withoutFilters(ctx)
	if err != nil {
		return err
	}
	var entries []blobEntry
	err = g.stream(ctx, "diff", nil, func(out io.Reader) error {
		return parseRaw(out, func(e blobEntry) error {
			return r.collect(&entries, e)
		})
	}, "diff-files", "--no-ext-diff", "--raw", "-z", "--no-renames", "--no-abbrev", "--diff-filter=AMT")
	if err != nil {
		return err
	}
	err = g.stream(ctx, "ls-files", nil, func(out io.Reader) error {
		return readNulFields(out, func(p string) error {
			if strings.HasSuffix(p, "/") {
				// An untracked embedded repository: never descended.
				r.skip(skipSubmodule)
				return nil
			}
			return r.collect(&entries, blobEntry{mode: modeFile, path: p})
		})
	}, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(g.dir)
	if err != nil {
		return refusedf("scan refused: cannot open %s: %v", g.dir, err)
	}
	defer root.Close()
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, reason, err := readWithin(root, filepath.FromSlash(e.path), r.opts.Limits.MaxFileBytes)
		if err != nil {
			return refusedf("scan refused: cannot read %s: %v", e.path, err)
		}
		if reason != scanIt {
			r.skip(reason)
			continue
		}
		if err := r.scanContent(ctx, e.path, "", data); err != nil {
			return err
		}
	}
	return nil
}

// readNulFields streams NUL-terminated fields. An unterminated final field
// means the output was cut short, which is a refusal, never a clean listing.
func readNulFields(r io.Reader, emit func(string) error) error {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		f, err := br.ReadString(0)
		if err == io.EOF {
			if f != "" {
				return refusedf("scan refused: truncated git output")
			}
			return nil
		}
		if err != nil {
			return err
		}
		if f = strings.TrimSuffix(f, "\x00"); f == "" {
			continue
		}
		if err := emit(f); err != nil {
			return err
		}
	}
}

// collect admits one listed entry. Symlinks and submodules are counted and
// never read; a path git reports that would leave the root is refused.
func (r *run) collect(entries *[]blobEntry, e blobEntry) error {
	if !safeGitPath(e.path) {
		return refusedf("scan refused: git reported an unsafe path")
	}
	key := e.blob + "\x00" + e.path
	if e.blob != "" && !isZeroID(e.blob) {
		if r.seen[key] {
			return nil
		}
		r.seen[key] = true
	}
	admitted, err := r.admit(e.path)
	if err != nil || !admitted {
		return err
	}
	switch e.mode {
	case modeFile, modeExecutable:
		*entries = append(*entries, e)
	case modeSymlink:
		r.skip(skipSymlink)
	case modeGitlink:
		r.skip(skipSubmodule)
	default:
		r.skip(skipNotRegular)
	}
	return nil
}

// safeGitPath refuses an absolute path or a parent segment. Git itself never
// records either, so seeing one means the output is not what it claims.
func safeGitPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." || seg == "" {
			return false
		}
	}
	return true
}

// scanCommits scans the blobs each selected commit introduced (merges against
// each parent for history, only combined changes for ranges), each distinct
// path/blob pair once, attributed to the first
// commit in topological order that introduced it.
func (r *run) scanCommits(ctx context.Context, g git, selection ...string) error {
	commits, err := g.revList(ctx, r.opts.Limits.MaxCommits, selection...)
	if err != nil {
		return err
	}
	r.report.Summary.CommitsScanned = len(commits)
	if len(commits) == 0 {
		return nil
	}
	mergeFormat := "-m"
	if r.opts.Mode == ModeRange {
		mergeFormat = "-c"
	}
	var entries []blobEntry
	err = g.stream(ctx, "diff-tree", commits, func(out io.Reader) error {
		return parseRaw(out, func(e blobEntry) error {
			if e.commit == "" {
				return refusedf("scan refused: unexpected git diff-tree output")
			}
			return r.collect(&entries, e)
		})
	}, "diff-tree", "--stdin", "-r", "-z", "--root", mergeFormat, "--no-renames", "--no-abbrev", "--diff-filter=AMT")
	if err != nil {
		return err
	}
	return r.readBlobs(ctx, g, entries)
}

func (r *run) readBlobs(ctx context.Context, g git, entries []blobEntry) error {
	if len(entries) == 0 {
		return nil
	}
	cat, err := g.catFile(ctx)
	if err != nil {
		return err
	}
	defer cat.close()
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !isObjectID(e.blob) || isZeroID(e.blob) {
			return refusedf("scan refused: unexpected git object id")
		}
		data, reason, err := cat.read(e.blob, r.opts.Limits.MaxFileBytes)
		if err != nil {
			return err
		}
		if reason != scanIt {
			r.skip(reason)
			continue
		}
		if err := r.scanContent(ctx, e.path, e.commit, data); err != nil {
			return err
		}
	}
	return nil
}

// String renders a one-line coverage summary for stderr.
func (s Summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "scanned %d file(s), %d byte(s)", s.FilesScanned, s.BytesScanned)
	if s.CommitsScanned > 0 {
		fmt.Fprintf(&b, " across %d commit(s)", s.CommitsScanned)
	}
	fmt.Fprintf(&b, "; %d finding(s), %d suppressed", s.Findings, s.Suppressed)
	if s.ExpiredSuppressions > 0 {
		fmt.Fprintf(&b, ", %d expired suppression(s) no longer applied", s.ExpiredSuppressions)
	}
	if k := s.Skipped; k.Total() > 0 {
		var parts []string
		for _, c := range []struct {
			n    int
			name string
		}{
			{k.Binary, "binary"}, {k.Archive, "archive"}, {k.Symlink, "symlink"}, {k.Oversize, "oversize"},
			{k.NotRegular, "not regular"}, {k.Vanished, "vanished"}, {k.Submodule, "submodule"}, {k.Excluded, "excluded"},
		} {
			if c.n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", c.n, c.name))
			}
		}
		fmt.Fprintf(&b, "; skipped %s", strings.Join(parts, ", "))
	}
	return b.String()
}
