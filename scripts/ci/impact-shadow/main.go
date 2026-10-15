// impact-shadow reports a proposed Go package selection. It never controls CI jobs.
package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

type report struct {
	Version           int      `json:"version"`
	Mode              string   `json:"mode"`
	Authoritative     bool     `json:"authoritative"`
	Base              string   `json:"base"`
	Head              string   `json:"head"`
	GOOS              string   `json:"goos"`
	GOARCH            string   `json:"goarch"`
	Recommendation    string   `json:"recommendation"`
	ChangedFiles      []string `json:"changed_files"`
	ChangedPackages   []string `json:"changed_packages"`
	CandidatePackages []string `json:"candidate_packages"`
	BasePackageCount  int      `json:"base_package_count"`
	HeadPackageCount  int      `json:"head_package_count"`
	FallbackReasons   []string `json:"fallback_reasons"`
	Limits            []string `json:"limits"`
}

type packageInfo struct {
	ImportPath      string
	ForTest         string
	Dir             string
	Imports         []string
	TestImports     []string
	XTestImports    []string
	GoFiles         []string
	CgoFiles        []string
	IgnoredGoFiles  []string
	TestGoFiles     []string
	XTestGoFiles    []string
	CFiles          []string
	CXXFiles        []string
	MFiles          []string
	HFiles          []string
	FFiles          []string
	SFiles          []string
	SwigFiles       []string
	SwigCXXFiles    []string
	SysoFiles       []string
	EmbedFiles      []string
	TestEmbedFiles  []string
	XTestEmbedFiles []string
	Incomplete      bool
	Error           *json.RawMessage
	DepsErrors      []json.RawMessage
}

type graph struct {
	packages map[string]packageInfo
	owners   map[string][]string
	ignored  map[string]bool
	dirs     map[string]string
}

var immutableSHA = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "impact-shadow: %v\n", err)
		os.Exit(2)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("impact-shadow", flag.ContinueOnError)
	var repo, base, head, jsonFile, markdownFile string
	var timeout time.Duration
	flags.StringVar(&repo, "repo", ".", "Git repository containing both commits")
	flags.StringVar(&base, "base", "", "full immutable base commit SHA")
	flags.StringVar(&head, "head", "", "full immutable candidate commit SHA")
	flags.StringVar(&jsonFile, "json", "", "JSON output file (default stdout)")
	flags.StringVar(&markdownFile, "markdown", "", "optional Markdown output file")
	flags.DurationVar(&timeout, "timeout", 3*time.Minute, "total analysis deadline")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !immutableSHA.MatchString(base) || !immutableSHA.MatchString(head) || timeout <= 0 {
		return errors.New("--base and --head must be full immutable commit SHAs; --timeout must be positive")
	}
	root, err := filepath.Abs(repo)
	if err != nil {
		return errors.New("resolve repository path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r := analyze(ctx, root, base, head)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if jsonFile != "" {
		err = os.WriteFile(jsonFile, data, 0o600)
	} else {
		_, err = output.Write(data)
	}
	if err != nil {
		return errors.New("write JSON report")
	}
	if markdownFile != "" {
		if err := os.WriteFile(markdownFile, []byte(markdown(r)), 0o600); err != nil {
			return errors.New("write Markdown report")
		}
	}
	return nil
}

func analyze(ctx context.Context, repo, base, head string) report {
	r := report{
		Version: 1, Mode: "shadow", Base: base, Head: head, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Recommendation: "full", ChangedFiles: []string{}, ChangedPackages: []string{}, CandidatePackages: []string{}, FallbackReasons: []string{},
		Limits: []string{
			"Advisory only: all backend CI checks still run; candidate packages never authorize skipping jobs.",
			"Graphs cover the host GOOS/GOARCH and default build tags only; other platform and tagged packages are not proven.",
			"External HTTP, database, process, browser and other runtime integration dependencies are not inferred.",
			"Generated artifacts are not built; Git snapshots omit symlinks, and changed symlinks require full fallback.",
			"Only the root Go module is analyzed; module/workspace changes and unknown file ownership require full fallback.",
		},
	}
	for i, sha := range []string{base, head} {
		kind, err := gitOutput(ctx, repo, "cat-file", "-t", sha)
		if err != nil || strings.TrimSpace(string(kind)) != "commit" {
			r.FallbackReasons = append(r.FallbackReasons, []string{"base", "head"}[i]+" immutable commit unavailable")
			return r
		}
	}
	changed, err := gitOutput(ctx, repo, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", base, head, "--")
	if err != nil {
		r.FallbackReasons = append(r.FallbackReasons, "changed-file diff unavailable")
		return r
	}
	for _, file := range strings.Split(string(changed), "\x00") {
		if file != "" {
			r.ChangedFiles = append(r.ChangedFiles, file)
		}
	}
	if len(r.ChangedFiles) == 0 {
		r.FallbackReasons = append(r.FallbackReasons, "empty changed-file set")
	}
	tmp, err := os.MkdirTemp("", "hikyo-impact-shadow-")
	if err != nil {
		r.FallbackReasons = append(r.FallbackReasons, "temporary snapshot storage unavailable")
		return r
	}
	defer os.RemoveAll(tmp)
	var graphs [2]graph
	for i, sha := range []string{base, head} {
		label := []string{"base", "head"}[i]
		dir := filepath.Join(tmp, label)
		links, err := snapshot(ctx, repo, sha, dir)
		if err != nil {
			r.FallbackReasons = append(r.FallbackReasons, label+" Git snapshot unavailable or unsafe")
			continue
		}
		for _, file := range r.ChangedFiles {
			if links[file] {
				r.FallbackReasons = append(r.FallbackReasons, "changed symlink: "+file)
			}
			if strings.HasSuffix(file, ".go") {
				data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
				if err == nil && (bytes.Contains(data, []byte("//go:generate")) || (bytes.Contains(data, []byte("Code generated")) && bytes.Contains(data, []byte("DO NOT EDIT.")))) {
					r.FallbackReasons = append(r.FallbackReasons, "generated code or generation directive: "+file)
				}
			}
		}
		graphs[i], err = listGraph(ctx, dir)
		if err != nil {
			r.FallbackReasons = append(r.FallbackReasons, label+" Go dependency graph incomplete or unavailable")
		}
	}
	r.BasePackageCount = len(graphs[0].packages)
	r.HeadPackageCount = len(graphs[1].packages)
	var changedReasons []string
	r.ChangedPackages, r.CandidatePackages, changedReasons = selectPackages(r.ChangedFiles, graphs[0], graphs[1])
	r.FallbackReasons = append(r.FallbackReasons, changedReasons...)
	if ctx.Err() != nil {
		r.FallbackReasons = append(r.FallbackReasons, "analysis deadline exceeded")
	}
	r.FallbackReasons = sortedUnique(r.FallbackReasons)
	if len(r.FallbackReasons) == 0 {
		r.Recommendation = "packages"
	}
	return r
}

// Only explicit source/embed ownership or a package's testdata directory is
// assigned. Unknown assets and generator inputs must not imply a safe reduction.
func selectPackages(files []string, base, head graph) ([]string, []string, []string) {
	changed := map[string]bool{}
	reasons := []string{}
	for _, file := range files {
		name := path.Base(file)
		if name == "go.mod" || name == "go.sum" || name == "go.work" || name == "go.work.sum" {
			reasons = append(reasons, "module or workspace manifest changed: "+file)
		}
		owned := false
		for _, g := range []graph{base, head} {
			if g.ignored[file] {
				reasons = append(reasons, "file excluded from host/default-tag graph: "+file)
			}
			owners := g.owners[file]
			if len(owners) == 0 {
				for dir := path.Dir(file); dir != "." && dir != "/"; dir = path.Dir(dir) {
					if path.Base(dir) == "testdata" {
						if pkg := g.dirs[path.Dir(dir)]; pkg != "" {
							owners = append(owners, pkg)
						}
						break
					}
				}
			}
			for _, owner := range owners {
				changed[owner] = true
				owned = true
			}
		}
		if !owned {
			reasons = append(reasons, "unknown file ownership: "+file)
		}
	}
	// Union both graphs before closure: deletions and removed import edges must
	// still invalidate old dependants, including test-only and external-test edges.
	reverse := map[string][]string{}
	for _, g := range []graph{base, head} {
		for name, pkg := range g.packages {
			for _, imports := range [][]string{pkg.Imports, pkg.TestImports, pkg.XTestImports} {
				for _, dep := range imports {
					reverse[dep] = append(reverse[dep], name)
				}
			}
		}
	}
	changedPackages := keys(changed)
	queue := slices.Clone(changedPackages)
	for i := 0; i < len(queue); i++ {
		for _, dependant := range reverse[queue[i]] {
			if !changed[dependant] {
				changed[dependant] = true
				queue = append(queue, dependant)
			}
		}
	}
	candidates := []string{}
	for _, name := range keys(changed) {
		if _, exists := head.packages[name]; exists {
			candidates = append(candidates, name)
		}
	}
	return changedPackages, candidates, sortedUnique(reasons)
}

func listGraph(ctx context.Context, dir string) (graph, error) {
	// Go normalizes the working directory (notably /var -> /private/var on
	// macOS); compare ownership against that same physical snapshot root.
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return graph{}, err
	}
	cmd := exec.CommandContext(ctx, "go", "list", "-mod=readonly", "-e", "-json", "-test", "./...")
	cmd.Dir = root
	cmd.Env = cleanEnv(map[string]string{
		"GOWORK": "off", "GOTOOLCHAIN": "local", "GOFLAGS": "", "GOENV": "off",
		"GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH,
	})
	data, commandErr := commandOutput(cmd)
	g, graphErr := decodeGraph(bytes.NewReader(data), root)
	return g, errors.Join(commandErr, graphErr)
}

func decodeGraph(input io.Reader, root string) (graph, error) {
	g := graph{packages: map[string]packageInfo{}, owners: map[string][]string{}, ignored: map[string]bool{}, dirs: map[string]string{}}
	decoder := json.NewDecoder(input)
	var graphErr error
	for {
		var pkg packageInfo
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return g, errors.New("invalid go list output")
		}
		if pkg.ImportPath == "" || pkg.Incomplete || pkg.Error != nil || len(pkg.DepsErrors) > 0 {
			graphErr = errors.New("incomplete package graph")
		}
		// -test resolves test-only dependency failures too. Synthetic test mains
		// and variants are checked for errors but are not independently selected.
		if pkg.ForTest != "" || (strings.HasSuffix(pkg.ImportPath, ".test") && len(pkg.GoFiles) == 1 && filepath.IsAbs(pkg.GoFiles[0])) {
			continue
		}
		if pkg.Dir == "" {
			graphErr = errors.New("package directory unavailable")
			continue
		}
		rel, err := filepath.Rel(root, pkg.Dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			graphErr = errors.New("package outside root module")
			continue
		}
		rel = filepath.ToSlash(rel)
		g.packages[pkg.ImportPath] = pkg
		g.dirs[rel] = pkg.ImportPath
		for _, files := range [][]string{pkg.GoFiles, pkg.CgoFiles, pkg.TestGoFiles, pkg.XTestGoFiles, pkg.CFiles, pkg.CXXFiles, pkg.MFiles, pkg.HFiles, pkg.FFiles, pkg.SFiles, pkg.SwigFiles, pkg.SwigCXXFiles, pkg.SysoFiles, pkg.EmbedFiles, pkg.TestEmbedFiles, pkg.XTestEmbedFiles} {
			for _, file := range files {
				name := path.Join(rel, filepath.ToSlash(file))
				g.owners[name] = append(g.owners[name], pkg.ImportPath)
			}
		}
		for _, file := range pkg.IgnoredGoFiles {
			g.ignored[path.Join(rel, file)] = true
		}
	}
	if len(g.packages) == 0 {
		graphErr = errors.New("empty package graph")
	}
	return g, graphErr
}

func gitCommand(ctx context.Context, repo string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "-c", "core.hooksPath=/dev/null", "-C", repo}, args...)...)
	cmd.Env = cleanEnv(map[string]string{"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_SYSTEM": os.DevNull, "GIT_NO_REPLACE_OBJECTS": "1", "GIT_TERMINAL_PROMPT": "0"})
	return cmd
}

func gitOutput(ctx context.Context, repo string, args ...string) ([]byte, error) {
	return commandOutput(gitCommand(ctx, repo, args...))
}

// Limit captured graph/diff output and never forward command stderr, which can
// contain private module URLs or credentials from a runner's configuration.
func commandOutput(cmd *exec.Cmd) ([]byte, error) {
	var output cappedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	// A child downloader inheriting a pipe must not extend the analysis deadline
	// after the parent go/git process is cancelled.
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	return output.Bytes(), err
}

type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > 64<<20 {
		return 0, errors.New("command output exceeds limit")
	}
	return b.Buffer.Write(data)
}

func cleanEnv(overrides map[string]string) []string {
	env := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, replace := overrides[key]; !replace {
			env = append(env, value)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func snapshot(ctx context.Context, repo, sha, destination string) (map[string]bool, error) {
	tree, err := gitOutput(ctx, repo, "ls-tree", "-r", "-z", "--full-tree", sha)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return nil, err
	}
	cmd := gitCommand(ctx, repo, "archive", "--format=tar", sha)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	links, extractErr := extractSnapshot(pipe, destination)
	if extractErr != nil {
		_ = cmd.Process.Kill()
	}
	if err := errors.Join(extractErr, cmd.Wait()); err != nil {
		return links, err
	}
	// export-ignore attributes and submodules must not silently produce a
	// smaller graph than the committed tree.
	for _, entry := range bytes.Split(tree, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		metadata, name, ok := strings.Cut(string(entry), "\t")
		if !ok || strings.HasPrefix(metadata, "160000 ") || !filepath.IsLocal(name) {
			return links, errors.New("unsupported Git tree entry")
		}
		if links[name] {
			continue
		}
		file := filepath.Join(destination, filepath.FromSlash(name))
		if _, err := os.Stat(file); err != nil {
			return links, errors.New("archive omitted tracked file")
		}
		if path.Base(name) == ".gitattributes" {
			attributes, err := os.ReadFile(file)
			if err != nil || bytes.Contains(attributes, []byte("export-subst")) {
				return links, errors.New("archive may substitute tracked content")
			}
		}
	}
	return links, nil
}

func extractSnapshot(input io.Reader, destination string) (map[string]bool, error) {
	reader := tar.NewReader(input)
	links := map[string]bool{}
	var total int64
	for entries := 0; ; entries++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return links, nil
		}
		if err != nil {
			return links, err
		}
		name := strings.TrimSuffix(header.Name, "/")
		if entries > 100000 || !filepath.IsLocal(name) || strings.Contains(name, "\\") || path.Clean(name) != name {
			return links, errors.New("unsafe archive path or entry count")
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeXGlobalHeader:
			// Git writes the commit ID as a global PAX comment header.
			continue
		case tar.TypeDir:
			err = os.MkdirAll(target, 0o700)
		case tar.TypeReg:
			total += header.Size
			if total > 1<<30 {
				return links, errors.New("snapshot exceeds size limit")
			}
			if err = os.MkdirAll(filepath.Dir(target), 0o700); err == nil {
				var file *os.File
				file, err = os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
				if err == nil {
					_, copyErr := io.Copy(file, reader)
					err = errors.Join(copyErr, file.Close())
				}
			}
		case tar.TypeSymlink:
			// Never materialize links: archive content cannot redirect file writes
			// or make go list read paths outside these isolated Git snapshots.
			links[name] = true
		default:
			return links, errors.New("unsupported archive entry")
		}
		if err != nil {
			return links, err
		}
	}
}

func sortedUnique(values []string) []string {
	slices.Sort(values)
	return slices.Compact(values)
}

func keys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}

func markdown(r report) string {
	var out strings.Builder
	fmt.Fprintf(&out, "### Backend package selection (shadow only)\n\nAll backend checks still run. Recommendation: **%s**.\n\nBase: `%s`\n\nCandidate: `%s`\n\nHost graph: %s/%s; %d base packages, %d candidate packages.\n", r.Recommendation, r.Base, r.Head, r.GOOS, r.GOARCH, r.BasePackageCount, r.HeadPackageCount)
	for _, section := range []struct {
		name   string
		values []string
	}{
		{"Fallback reasons", r.FallbackReasons},
		{"Directly changed packages", r.ChangedPackages},
		{"Proposed packages (exploratory; incomplete when fallback applies)", r.CandidatePackages},
		{"Limits", r.Limits},
	} {
		fmt.Fprintf(&out, "\n**%s**\n\n", section.name)
		if len(section.values) == 0 {
			out.WriteString("None.\n")
		}
		for _, value := range section.values {
			// JSON quoting prevents newlines/control characters in Git paths from
			// introducing extra Markdown. Escape remaining HTML/Markdown syntax.
			quoted, _ := json.Marshal(value)
			safe := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "`", "&#96;", "[", "&#91;", "]", "&#93;").Replace(string(quoted))
			fmt.Fprintf(&out, "- %s\n", safe)
		}
	}
	return out.String()
}
