package repscan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/scanning"
	"github.com/Hikyo-Org/hikyo/internal/scanning/corpus"
)

// The planted credentials are syntactically valid for their rules and not
// live: filler characters, the same construction the scanning corpus uses.
var (
	githubPAT = "ghp_" + strings.Repeat("A0b1C2d3E4", 3) + "ZZZZZZ"
	awsKey    = "AKIAIOSFODNN7EXAMPLE"
	pemBlock  = "-----BEGIN RSA PRIVATE KEY-----\nMIIBOgIBAAJBAKj34GkxFhD90vcNLYLInFEX6Ppy1tPf9Cnzj4p4WGeKLs1Pt8Qu\n-----END RSA PRIVATE KEY-----\n"
)

func loadRules(t *testing.T) *scanning.Ruleset {
	t.Helper()
	rs, err := scanning.Load()
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func write(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func pathsOptions(dir string, paths ...string) Options {
	return Options{Mode: ModePaths, Workdir: dir, Paths: paths, Limits: DefaultLimits()}
}

func scan(t *testing.T, opts Options) *Report {
	t.Helper()
	r, err := Run(context.Background(), loadRules(t), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return r
}

func refusal(t *testing.T, opts Options, kind Kind, want string) {
	t.Helper()
	_, err := Run(context.Background(), loadRules(t), opts)
	if err == nil {
		t.Fatalf("Run succeeded, want a refusal containing %q", want)
	}
	if got := ErrorKind(err); got != kind {
		t.Fatalf("error kind = %d (%v), want %d", got, err, kind)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err, want)
	}
}

type loc struct {
	rule, path string
	line       int
	commit     string
	suppressed string
}

func locs(r *Report) []loc {
	out := make([]loc, 0, len(r.Findings))
	for _, f := range r.Findings {
		out = append(out, loc{f.RuleID, f.Path, f.Line, f.Commit, f.Suppressed})
	}
	return out
}

func TestFingerprintIsStableAndLocationOnly(t *testing.T) {
	// Known answer: the construction is part of the reviewed suppression
	// contract, so it may not drift silently.
	got := Fingerprint("digest", "a/b.txt", 3, "worktree")
	// printf 'digest\0a/b.txt\0003\0worktree' | sha256sum
	const want = "71c3e968dd2caf9019407f69dc1a889ee3f250583433d600b85f4639435ccf0d"
	if got != want {
		t.Fatalf("Fingerprint = %s, want %s", got, want)
	}
	for _, other := range []string{
		Fingerprint("digest2", "a/b.txt", 3, "worktree"),
		Fingerprint("digest", "a/c.txt", 3, "worktree"),
		Fingerprint("digest", "a/b.txt", 4, "worktree"),
		Fingerprint("digest", "a/b.txt", 3, "0123"),
		Fingerprint("digest", "a/b.txt3", 0, "worktree"),
	} {
		if other == got {
			t.Fatal("fingerprint ignores a component or its separator")
		}
	}
}

func TestPathsScanFindsAndLocalizes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "src/config.txt", "one\ntwo\ntoken = "+githubPAT+"\nfour\n")
	write(t, dir, "src/crlf.txt", "a\r\nb\r\nkey="+awsKey+"\r\n")
	write(t, dir, "keys/id_rsa", "header\n"+pemBlock)
	write(t, dir, "two.txt", githubPAT+"\nclean\n"+githubPAT+" and "+awsKey+"\n")
	write(t, dir, "clean.txt", "nothing to see\n")

	r := scan(t, pathsOptions(dir))
	want := []loc{
		{rule: "private-key", path: "keys/id_rsa", line: 2},
		{rule: "github-pat", path: "src/config.txt", line: 3},
		{rule: "aws-access-token", path: "src/crlf.txt", line: 3},
		{rule: "github-pat", path: "two.txt", line: 1},
		{rule: "aws-access-token", path: "two.txt", line: 3},
		{rule: "github-pat", path: "two.txt", line: 3},
	}
	if got := locs(r); !slices.Equal(got, want) {
		t.Fatalf("findings:\n got %v\nwant %v", got, want)
	}
	if r.Summary.Findings != len(want) || r.Summary.FilesScanned != 5 {
		t.Fatalf("summary = %+v", r.Summary)
	}
	for _, f := range r.Findings {
		digest, _ := loadRules(t).SemanticDigest(f.RuleID)
		if f.RuleDigest != digest || f.Fingerprint != Fingerprint(digest, f.Path, f.Line, worktreeScope) {
			t.Fatalf("finding identity does not follow the documented construction: %+v", f)
		}
	}
}

func TestSkipsAreCountedNeverScanned(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "secret.txt", githubPAT+"\n")
	write(t, dir, "bin.dat", "\x00\x01"+githubPAT)
	write(t, dir, "bundle.zip", githubPAT)
	write(t, dir, "sneaky.txt", "\x1f\x8b"+githubPAT)
	write(t, dir, "big.txt", strings.Repeat("x", MaxFileBytes)+githubPAT)
	write(t, dir, ".git/config", githubPAT)
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "escape.txt")); err != nil {
		if runtime.GOOS == "windows" {
			t.Log("symlinks unavailable on this Windows runner; the symlink leg is skipped")
		} else {
			t.Fatal(err)
		}
	}
	r := scan(t, pathsOptions(dir))
	if len(r.Findings) != 0 {
		t.Fatalf("skipped content produced findings: %v", locs(r))
	}
	s := r.Summary.Skipped
	if s.Binary != 1 || s.Archive != 2 || s.Oversize != 1 {
		t.Fatalf("skipped = %+v", s)
	}
	if runtime.GOOS != "windows" && s.Symlink != 1 {
		t.Fatalf("symlink not counted: %+v", s)
	}
	if r.Summary.FilesScanned != 0 {
		t.Fatalf("files scanned = %d, want 0", r.Summary.FilesScanned)
	}
}

func TestReadWithinRefusesSymlinkSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	dir := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "secret.txt", githubPAT)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	// An intermediate directory replaced by a symlink leaving the root: the
	// rooted read refuses it rather than following it out.
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	data, reason, err := readWithin(root, filepath.Join("link", "secret.txt"), MaxFileBytes)
	if err == nil && reason == scanIt {
		t.Fatalf("read escaped the root: %q", data)
	}
}

func TestInlineSuppressionIsScopedToOneRuleOnOneLine(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", strings.Join([]string{
		githubPAT + " # hikyo-scan:ignore github-pat",
		githubPAT + " " + awsKey + " # hikyo-scan:ignore github-pat",
		"# hikyo-scan:ignore github-pat",
		githubPAT,
		githubPAT + " # hikyo-scan:ignore github-pat-extra",
		"",
	}, "\n"))
	r := scan(t, pathsOptions(dir))
	want := []loc{
		{rule: "github-pat", path: "a.txt", line: 1, suppressed: SuppressedInline},
		{rule: "aws-access-token", path: "a.txt", line: 2},
		{rule: "github-pat", path: "a.txt", line: 2, suppressed: SuppressedInline},
		{rule: "github-pat", path: "a.txt", line: 4},
		{rule: "github-pat", path: "a.txt", line: 5},
	}
	if got := locs(r); !slices.Equal(got, want) {
		t.Fatalf("findings:\n got %v\nwant %v", got, want)
	}
	if r.Summary.Findings != 3 || r.Summary.Suppressed != 2 {
		t.Fatalf("summary = %+v", r.Summary)
	}
}

func TestFingerprintSuppressionAndExpiry(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", githubPAT+"\n"+awsKey+"\n")
	first := scan(t, pathsOptions(dir))
	if len(first.Findings) != 2 {
		t.Fatalf("findings = %v", locs(first))
	}
	write(t, dir, DefaultConfigName, `version = 1

[[suppressions]]
fingerprint = "`+first.Findings[0].Fingerprint+`"
reason = "test fixture, not a live token"

[[suppressions]]
fingerprint = "`+first.Findings[1].Fingerprint+`"
reason = "expired review"
expires = 2020-01-01
`)
	opts := pathsOptions(dir)
	opts.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	r := scan(t, opts)
	if r.Findings[0].Suppressed != SuppressedFingerprint || r.Findings[1].Suppressed != "" {
		t.Fatalf("findings = %v", locs(r))
	}
	if r.Summary.Suppressed != 1 || r.Summary.Findings != 1 || r.Summary.ExpiredSuppressions != 1 {
		t.Fatalf("summary = %+v", r.Summary)
	}
	var sarif bytes.Buffer
	if err := WriteSARIF(&sarif, r, "test"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sarif.String(), "test fixture, not a live token") {
		t.Fatal("SARIF does not carry the suppression justification")
	}
}

func TestExclusions(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "vendor/lib/a.txt", githubPAT)
	write(t, dir, "docs/b.md", githubPAT)
	write(t, dir, "src/c.txt", githubPAT+"\n"+awsKey)
	write(t, dir, DefaultConfigName, "version = 1\nexclude_paths = [\"vendor/**\"]\n")
	opts := pathsOptions(dir)
	opts.ExcludePaths = []string{"**/*.md"}
	opts.ExcludeRules = []string{"aws-access-token"}
	r := scan(t, opts)
	want := []loc{{rule: "github-pat", path: "src/c.txt", line: 1}}
	if got := locs(r); !slices.Equal(got, want) {
		t.Fatalf("findings = %v", got)
	}
	if r.Summary.Skipped.Excluded != 2 {
		t.Fatalf("excluded = %d, want 2", r.Summary.Skipped.Excluded)
	}
	if !slices.Equal(r.Ruleset.ExcludedRules, []string{"aws-access-token"}) {
		t.Fatalf("excluded rules not reported: %v", r.Ruleset.ExcludedRules)
	}
	for _, rule := range r.Ruleset.Rules {
		if rule.ID == "aws-access-token" {
			t.Fatal("an excluded rule is still listed as active")
		}
	}
}

func TestConfigFailsClosed(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"unknown key":         {"version = 1\ncustom_rules = []\n", "unknown key"},
		"unknown nested key":  {"version = 1\n[[suppressions]]\nfingerprint = \"" + strings.Repeat("a", 64) + "\"\nreason = \"r\"\npattern = \"x\"\n", "unknown key"},
		"missing version":     {"exclude_rules = []\n", "missing version"},
		"future version":      {"version = 2\n", "unsupported version"},
		"bad fingerprint":     {"version = 1\n[[suppressions]]\nfingerprint = \"abc\"\nreason = \"r\"\n", "64 lowercase hex"},
		"no reason":           {"version = 1\n[[suppressions]]\nfingerprint = \"" + strings.Repeat("a", 64) + "\"\n", "reason is required"},
		"duplicate":           {"version = 1\n[[suppressions]]\nfingerprint = \"" + strings.Repeat("a", 64) + "\"\nreason = \"r\"\n[[suppressions]]\nfingerprint = \"" + strings.Repeat("a", 64) + "\"\nreason = \"r\"\n", "suppressed twice"},
		"parent glob":         {"version = 1\nexclude_paths = [\"../x\"]\n", "not allowed"},
		"absolute glob":       {"version = 1\nexclude_paths = [\"/etc\"]\n", "relative"},
		"unknown rule":        {"version = 1\nexclude_rules = [\"no-such-rule\"]\n", "unknown rule"},
		"string expiry":       {"version = 1\n[[suppressions]]\nfingerprint = \"" + strings.Repeat("a", 64) + "\"\nreason = \"r\"\nexpires = \"soon\"\n", DefaultConfigName},
		"malformed toml":      {"version = \n", DefaultConfigName},
		"every rule excluded": {"version = 1\nexclude_rules = " + allRulesTOML(t) + "\n", "at least one rule"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, DefaultConfigName, tc.body)
			refusal(t, pathsOptions(dir), KindConfig, tc.want)
		})
	}
	t.Run("missing explicit config", func(t *testing.T) {
		opts := pathsOptions(t.TempDir())
		opts.ConfigFile = "nope.toml"
		refusal(t, opts, KindConfig, "nope.toml")
	})
	t.Run("suppressions file may not exclude", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "s.toml", "version = 1\nexclude_rules = [\"github-pat\"]\n")
		opts := pathsOptions(dir)
		opts.SuppressionsFile = "s.toml"
		refusal(t, opts, KindConfig, "only version and [[suppressions]]")
	})
	t.Run("unknown flag rule", func(t *testing.T) {
		opts := pathsOptions(t.TempDir())
		opts.ExcludeRules = []string{"github-pat-typo"}
		refusal(t, opts, KindConfig, "unknown rule")
	})
	t.Run("bad flag glob", func(t *testing.T) {
		opts := pathsOptions(t.TempDir())
		opts.ExcludePaths = []string{"a/**b"}
		refusal(t, opts, KindConfig, "whole segment")
	})
}

func allRulesTOML(t *testing.T) string {
	ids := loadRules(t).RuleIDs()
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = `"` + id + `"`
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func TestBudgetsRefuseByName(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", githubPAT+"\n")
	write(t, dir, "b.txt", githubPAT+"\n")
	for name, tc := range map[string]struct {
		tweak func(*Limits)
		want  string
	}{
		"files":    {func(l *Limits) { l.MaxFiles = 1 }, "more than 1 files"},
		"bytes":    {func(l *Limits) { l.MaxTotalBytes = int64(len(githubPAT)) + 2 }, "bytes scanned in total"},
		"findings": {func(l *Limits) { l.MaxFindings = 1 }, "more than 1 findings"},
		"runtime":  {func(l *Limits) { l.Timeout = time.Nanosecond }, "runtime budget"},
	} {
		t.Run(name, func(t *testing.T) {
			opts := pathsOptions(dir)
			tc.tweak(&opts.Limits)
			refusal(t, opts, KindRefused, tc.want)
		})
	}
}

// TestParityWithValueEntry: the local scanner's verdict on the value-entry
// fixture corpus is the value-entry scanner's verdict, rule id for rule id,
// and every compiled rule is reachable from a local scan.
func TestParityWithValueEntry(t *testing.T) {
	rs := loadRules(t)
	fixtures, err := corpus.All()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	want := map[string][]string{}
	seen := map[string]bool{}
	n := 0
	for _, f := range fixtures {
		for _, item := range append(slices.Clone(f.TP), f.FP...) {
			name := "corpus/" + f.RuleID + "/item-" + strconv.Itoa(n) + ".txt"
			n++
			write(t, dir, name, item)
			found, err := rs.Scan(context.Background(), []byte(item))
			if err != nil {
				t.Fatal(err)
			}
			for _, fd := range found {
				want[name] = append(want[name], fd.RuleID)
			}
		}
	}
	r := scan(t, pathsOptions(dir))
	got := map[string][]string{}
	for _, f := range r.Findings {
		if f.Line == 0 {
			t.Errorf("%s: %s matched but could not be localized", f.Path, f.RuleID)
		}
		if !slices.Contains(got[f.Path], f.RuleID) {
			got[f.Path] = append(got[f.Path], f.RuleID)
		}
		seen[f.RuleID] = true
	}
	for name, ids := range want {
		slices.Sort(ids)
		g := got[name]
		slices.Sort(g)
		if !slices.Equal(ids, g) {
			t.Errorf("%s: local rules %v, value-entry rules %v", name, g, ids)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s: local scan found %v, value-entry scan found nothing", name, got[name])
		}
	}
	for _, id := range rs.RuleIDs() {
		if !seen[id] {
			t.Errorf("rule %s never fired locally on its true-positive fixtures", id)
		}
	}
}

// TestOutputsCarryNoPlaintext: neither JSON nor SARIF contains any byte of a
// planted credential.
func TestOutputsCarryNoPlaintext(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", githubPAT+"\n"+awsKey+"\n"+pemBlock)
	r := scan(t, pathsOptions(dir))
	if len(r.Findings) != 3 {
		t.Fatalf("findings = %v", locs(r))
	}
	var js, sarif bytes.Buffer
	if err := json.NewEncoder(&js).Encode(r); err != nil {
		t.Fatal(err)
	}
	if err := WriteSARIF(&sarif, r, "test"); err != nil {
		t.Fatal(err)
	}
	for _, out := range [][]byte{js.Bytes(), sarif.Bytes()} {
		for _, secret := range []string{githubPAT, awsKey, "MIIBOgIBAAJBAKj34GkxFhD90vcNLYLInFEX6Ppy1tPf9Cnzj4p4WGeKLs1Pt8Qu", githubPAT[4:20]} {
			if bytes.Contains(out, []byte(secret)) {
				t.Fatalf("output carries planted plaintext %q", secret)
			}
		}
	}
}

// TestSARIFShape asserts the SARIF 2.1.0 members a code-scanning consumer
// requires, and that the rule table and result indices agree.
func TestSARIFShape(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "dir with space/a b.txt", "x\n"+githubPAT+"\n")
	r := scan(t, pathsOptions(dir))
	var out bytes.Buffer
	if err := WriteSARIF(&out, r, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	var log struct {
		Schema  string `json:"$schema"`
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name    string `json:"name"`
					Version string `json:"version"`
					Rules   []struct {
						ID         string            `json:"id"`
						Properties map[string]string `json:"properties"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID              string                `json:"ruleId"`
				RuleIndex           int                   `json:"ruleIndex"`
				Level               string                `json:"level"`
				Message             struct{ Text string } `json:"message"`
				PartialFingerprints map[string]string     `json:"partialFingerprints"`
				Locations           []struct {
					PhysicalLocation struct {
						ArtifactLocation struct{ URI string } `json:"artifactLocation"`
						Region           struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(out.Bytes(), &log); err != nil {
		t.Fatal(err)
	}
	if log.Version != "2.1.0" || log.Schema == "" || len(log.Runs) != 1 {
		t.Fatalf("log header = %q %q %d", log.Version, log.Schema, len(log.Runs))
	}
	run := log.Runs[0]
	if run.Tool.Driver.Name != "hikyo-scan" || run.Tool.Driver.Version != "1.2.3" || len(run.Tool.Driver.Rules) != len(loadRules(t).RuleIDs()) {
		t.Fatalf("driver = %+v", run.Tool.Driver)
	}
	for _, rule := range run.Tool.Driver.Rules {
		if len(rule.Properties["semanticDigest"]) != 64 {
			t.Fatalf("rule %s lacks its semantic digest", rule.ID)
		}
	}
	if len(run.Results) != 1 {
		t.Fatalf("results = %d", len(run.Results))
	}
	res := run.Results[0]
	if run.Tool.Driver.Rules[res.RuleIndex].ID != res.RuleID || res.Level != "error" || res.Message.Text == "" {
		t.Fatalf("result = %+v", res)
	}
	if res.PartialFingerprints[SARIFFingerprintKey] != r.Findings[0].Fingerprint {
		t.Fatal("partial fingerprint differs from the report fingerprint")
	}
	pl := res.Locations[0].PhysicalLocation
	if pl.ArtifactLocation.URI != "dir%20with%20space/a%20b.txt" || pl.Region.StartLine != 2 {
		t.Fatalf("location = %+v", pl)
	}
}

func TestUntrustedFileNamesStayData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows file names cannot carry control characters")
	}
	dir := t.TempDir()
	name := "evil\nFAKE:1\tgithub-pat.txt"
	write(t, dir, name, githubPAT)
	r := scan(t, pathsOptions(dir))
	if len(r.Findings) != 1 || r.Findings[0].Path != name {
		t.Fatalf("findings = %v", locs(r))
	}
	var sarif bytes.Buffer
	if err := WriteSARIF(&sarif, r, ""); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sarif.Bytes(), []byte("\nFAKE")) {
		t.Fatal("a raw newline from a file name reached the SARIF output")
	}
}

func TestMissingPathIsUsage(t *testing.T) {
	refusal(t, pathsOptions(t.TempDir(), "does-not-exist"), KindConfig, "does-not-exist")
}

func TestRangeValidation(t *testing.T) {
	for _, bad := range []string{"", "--all", "-n1..HEAD", "HEAD", "a..b c", "a..\nb"} {
		opts := Options{Mode: ModeRange, Workdir: t.TempDir(), Range: bad, Limits: DefaultLimits()}
		refusal(t, opts, KindConfig, "--range")
	}
}

func TestLimitsMustBePositive(t *testing.T) {
	opts := pathsOptions(t.TempDir())
	opts.Limits.MaxFindings = 0
	refusal(t, opts, KindConfig, "positive")
}

func TestErrorKindDefaultsToRefused(t *testing.T) {
	if ErrorKind(errors.New("boom")) != KindRefused {
		t.Fatal("an unclassified failure must never read as anything but a refusal")
	}
}

func TestGlobMatching(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"vendor/**", "vendor/a/b.go", true},
		{"vendor/**", "src/vendor/a.go", false},
		{"**/*.md", "README.md", true},
		{"**/*.md", "docs/x/y.md", true},
		{"*.md", "docs/y.md", false},
		{"docs/*.txt", "docs/a.txt", true},
		{"docs/*.txt", "docs/sub/a.txt", false},
		{"**", "anything/at/all", true},
	} {
		if got := matchGlob(tc.pattern, tc.path); got != tc.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

// TestSymlinkedRootIsScanned: a working directory reached through a symlink
// is the selected root and is scanned, not skipped as a symlink (which would
// report a clean tree that was never read).
func TestSymlinkedRootIsScanned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	real := t.TempDir()
	write(t, real, "a.txt", githubPAT+"\n")
	link := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	r := scan(t, pathsOptions(link))
	want := []loc{{rule: "github-pat", path: "a.txt", line: 1}}
	if got := locs(r); !slices.Equal(got, want) {
		t.Fatalf("findings = %v", got)
	}
}
