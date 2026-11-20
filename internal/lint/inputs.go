package lint

// Declared non-Go inputs (lint-analysis driver ADR, decision 2). The vet build
// cache keys a package on its Go sources and on analyzer flag values, not on
// the other files an analyzer reads from disk, so an analyzer that reads .sql
// text, pin JSON or the scripts/ci programs declares them, the driver hashes
// the declaration and passes the digest as a flag, and the analyzer reads
// through Inputs only. Inputs reads the declared files once, refuses when
// their digest is not the one the driver passed, and refuses any path that was
// not declared, so a stale or incomplete declaration fails closed instead of
// replaying an old result.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/Hikyo-Org/hikyo/internal/pathutil"
)

// InputSpec declares one input: a single File, or the files of a Dir with the
// given extensions (all of them when Exts is empty), recursively when
// Recursive. Paths are relative to the repository root.
type InputSpec struct {
	File      string
	Dir       string
	Exts      []string
	Recursive bool
}

// Refusals. Both are fail-closed: the analyzer reports them as a finding and
// does not analyze.
var (
	ErrUndeclaredInput = errors.New("undeclared input")
	ErrInputDigest     = errors.New("input digest mismatch")
)

// source is how an input-reading analyzer reaches the disk: through a verified
// Inputs under the vet driver, or straight from disk for the old harness.
type source interface {
	// ReadFile reads a regular file.
	ReadFile(path string) ([]byte, error)
	// ReadDir lists the names of the files directly in dir.
	ReadDir(dir string) ([]string, error)
	// WalkFiles lists every file under dir with one of the extensions.
	WalkFiles(dir string, exts []string) ([]string, error)
}

// osSource reads the disk directly: the old harness and the helpers' tests.
type osSource struct{}

// ReadFile reads through a handle to the file's directory, so a symlink that
// leaves it is refused (pathutil.ReadFileWithin).
func (osSource) ReadFile(path string) ([]byte, error) {
	return pathutil.ReadFileWithin(filepath.Dir(path), path)
}

func (osSource) ReadDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func (osSource) WalkFiles(dir string, exts []string) ([]string, error) {
	var out []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && hasExt(path, exts) {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func hasExt(name string, exts []string) bool {
	return len(exts) == 0 || slices.ContainsFunc(exts, func(ext string) bool { return strings.HasSuffix(name, ext) })
}

type inputState int

const (
	inputPresent inputState = iota
	inputAbsent
	inputIrregular
)

type inputFile struct {
	data  []byte
	state inputState
}

// Inputs is a snapshot of the declared inputs. Every read is served from the
// snapshot the digest was computed over, so a file edited after the check
// cannot change what the analyzer sees.
type Inputs struct {
	root     string
	digest   string
	files    map[string]inputFile
	lists    map[string][]string // declared directory -> files in it (absolute, sorted)
	recurse  map[string]bool
	declared map[string]bool // declared files: those named by a File spec
}

// SnapshotInputs reads the declared inputs under root and computes their digest.
func SnapshotInputs(root string, specs []InputSpec) (*Inputs, error) {
	root = filepath.Clean(root)
	in := &Inputs{root: root, files: map[string]inputFile{}, lists: map[string][]string{}, recurse: map[string]bool{}, declared: map[string]bool{}}
	var lines []string
	read := func(path string) error {
		info, err := os.Lstat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			in.files[path] = inputFile{state: inputAbsent}
		case err != nil:
			return err
		case !info.Mode().IsRegular():
			in.files[path] = inputFile{state: inputIrregular}
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			in.files[path] = inputFile{data: data}
		}
		return nil
	}
	for _, spec := range specs {
		switch {
		case spec.File != "" && spec.Dir == "":
			path := filepath.Join(root, filepath.FromSlash(spec.File))
			if err := read(path); err != nil {
				return nil, err
			}
			in.declared[path] = true
		case spec.Dir != "" && spec.File == "":
			dir := filepath.Join(root, filepath.FromSlash(spec.Dir))
			var names []string
			list, err := listInputDir(dir, spec)
			if err != nil {
				return nil, err
			}
			for _, path := range list {
				if err := read(path); err != nil {
					return nil, err
				}
				names = append(names, path)
			}
			in.lists[dir] = names
			in.recurse[dir] = spec.Recursive
			lines = append(lines, fmt.Sprintf("dir %s exts=%s recursive=%t", spec.Dir, strings.Join(spec.Exts, ","), spec.Recursive))
		default:
			return nil, fmt.Errorf("input spec %+v must name exactly one of File and Dir", spec)
		}
	}
	for path, f := range in.files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(f.data)
		lines = append(lines, fmt.Sprintf("file %s state=%d sha256=%x", filepath.ToSlash(rel), f.state, sum))
	}
	slices.Sort(lines)
	total := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	in.digest = hex.EncodeToString(total[:])
	return in, nil
}

// listInputDir lists the files a Dir spec selects. A directory that does not
// exist selects nothing and hashes as empty, so creating it changes the digest.
func listInputDir(dir string, spec InputSpec) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist) && path == dir:
			return fs.SkipAll
		case err != nil:
			return err
		case d.IsDir():
			if path != dir && !spec.Recursive {
				return fs.SkipDir
			}
			return nil
		case hasExt(d.Name(), spec.Exts):
			out = append(out, path)
		}
		return nil
	})
	slices.Sort(out)
	return out, err
}

// InputDigest is the digest the driver passes as the analyzer's -inputs flag.
func InputDigest(root string, specs []InputSpec) (string, error) {
	in, err := SnapshotInputs(root, specs)
	if err != nil {
		return "", err
	}
	return in.digest, nil
}

// OpenInputs snapshots the declared inputs and refuses unless their digest is
// want, the one the driver computed before vet ran.
func OpenInputs(root, want string, specs []InputSpec) (*Inputs, error) {
	in, err := SnapshotInputs(root, specs)
	if err != nil {
		return nil, err
	}
	if in.digest != want {
		return nil, fmt.Errorf("%w: the declared inputs hash to %s, the driver passed %s", ErrInputDigest, in.digest, want)
	}
	return in, nil
}

func (in *Inputs) ReadFile(path string) ([]byte, error) {
	path = filepath.Clean(path)
	f, ok := in.files[path]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUndeclaredInput, path)
	}
	switch f.state {
	case inputAbsent:
		return nil, &fs.PathError{Op: "read", Path: path, Err: fs.ErrNotExist}
	case inputIrregular:
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return f.data, nil
}

func (in *Inputs) ReadDir(dir string) ([]string, error) {
	dir = filepath.Clean(dir)
	list, ok := in.lists[dir]
	if !ok {
		return nil, fmt.Errorf("%w: directory %s", ErrUndeclaredInput, dir)
	}
	var names []string
	for _, path := range list {
		if filepath.Dir(path) == dir {
			names = append(names, filepath.Base(path))
		}
	}
	return names, nil
}

func (in *Inputs) WalkFiles(dir string, exts []string) ([]string, error) {
	dir = filepath.Clean(dir)
	list, ok := in.lists[dir]
	if !ok || !in.recurse[dir] {
		return nil, fmt.Errorf("%w: recursive directory %s", ErrUndeclaredInput, dir)
	}
	var out []string
	for _, path := range list {
		if hasExt(path, exts) {
			out = append(out, path)
		}
	}
	return out, nil
}

// inputProvider yields the source an analyzer reads and the repository root it
// resolves declared paths against, or the refusal. root is the root alone, for
// a per-package part that reads only the package's own Go files.
type inputProvider struct {
	open func() (source, string, error)
	root func() string
}

// specsFunc declares an analyzer's inputs under a root.
type specsFunc func(root string) ([]InputSpec, error)

// inputDeclarations maps each input-reading analyzer to its declaration.
var inputDeclarations = map[string]specsFunc{}

// declareInputs registers an analyzer's declaration; the driver test reads it
// back to hash the inputs.
func declareInputs(analyzer string, specs specsFunc) { inputDeclarations[analyzer] = specs }

// InputDeclaration returns an input-reading analyzer's declared inputs under
// root, or false for an analyzer that reads none.
func InputDeclaration(analyzer, root string) ([]InputSpec, bool, error) {
	specs, ok := inputDeclarations[analyzer]
	if !ok {
		return nil, false, nil
	}
	out, err := specs(root)
	return out, true, err
}

// InputAnalyzers lists the analyzers that read declared inputs.
func InputAnalyzers() []string {
	names := make([]string, 0, len(inputDeclarations))
	for name := range inputDeclarations {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// flagInputs registers -root and -inputs on the analyzer and returns the
// provider that opens its declared inputs against them. Both are part of the
// vet cache key; a missing flag is a refusal, never a default.
func flagInputs(a *analysis.Analyzer) inputProvider {
	var root, digest string
	a.Flags.StringVar(&root, "root", "", "repository root the declared inputs live under (set by the driver)")
	a.Flags.StringVar(&digest, "inputs", "", "digest of the declared inputs (set by the driver; part of the vet cache key)")
	return inputProvider{root: func() string { return root }, open: func() (source, string, error) {
		if root == "" || digest == "" {
			return nil, "", errors.New("the driver did not pass -root and -inputs")
		}
		specs, ok := inputDeclarations[a.Name]
		if !ok {
			return nil, "", fmt.Errorf("analyzer %s declares no inputs", a.Name)
		}
		declared, err := specs(root)
		if err != nil {
			return nil, "", err
		}
		in, err := OpenInputs(root, digest, declared)
		if err != nil {
			return nil, "", err
		}
		return in, root, nil
	}}
}

// diskInputs is the old harness's provider: the disk under root, unverified.
func diskInputs(root string) func(*analysis.Analyzer) inputProvider {
	return func(*analysis.Analyzer) inputProvider {
		return inputProvider{root: func() string { return root }, open: func() (source, string, error) { return osSource{}, root, nil }}
	}
}

// reportRefusal reports a refused input as a finding at the package clause, so
// the driver test fails on it and a `// want` fixture can state it.
func reportRefusal(pass *analysis.Pass, err error) {
	pass.Report(analysis.Diagnostic{Pos: packagePos(pass), Message: "inputs refused: " + err.Error()})
}

// fileCheck is an analyzer over declared files. It runs once, on its anchor
// package, and reports each finding at the anchor's package clause; the
// anchor's target summary makes a renamed or unloaded anchor a finding.
type fileCheck struct {
	name, doc string
	anchor    string
	specs     specsFunc
	// run returns the findings, in the wording the old harness used, or a
	// refusal (an undeclared input) that is not a finding about the code.
	run func(src source, root string) ([]string, error)
	// flags registers analyzer-specific flags run reads, if any.
	flags func(a *analysis.Analyzer)
	// pkg is an optional per-package part that reads the package's own Go files.
	pkg func(pass *analysis.Pass, root string)
}

// analyzer builds the vet analyzer; provide chooses where its inputs come from.
func (c fileCheck) analyzer(provide func(*analysis.Analyzer) inputProvider) *analysis.Analyzer {
	a := &analysis.Analyzer{Name: c.name, Doc: c.doc}
	anchor := c.anchor
	a.Flags.StringVar(&anchor, "anchor", c.anchor, "package the file checks run on")
	if c.flags != nil {
		c.flags(a)
	}
	in := provide(a)
	a.Run = func(pass *analysis.Pass) (any, error) {
		path := pass.Pkg.Path()
		if c.pkg != nil && c.pkgApplies(anchor, path) {
			if root := in.root(); root != "" {
				c.pkg(pass, root)
			} else {
				reportRefusal(pass, errors.New("the driver did not pass -root"))
			}
		}
		if path != anchor {
			return nil, nil
		}
		reportTarget(pass)
		src, root, err := in.open()
		if err != nil {
			reportRefusal(pass, err)
			return nil, nil
		}
		findings, err := c.run(src, root)
		if err != nil {
			reportRefusal(pass, err)
			return nil, nil
		}
		for _, finding := range findings {
			pass.Report(analysis.Diagnostic{Pos: packagePos(pass), Message: finding})
		}
		return nil, nil
	}
	return a
}

// pkgApplies is whether the per-package part runs on path: the anchor's module
// subtree, external test packages included.
func (c fileCheck) pkgApplies(anchor, path string) bool {
	base := strings.TrimSuffix(anchor, "/")
	return path == base || strings.HasPrefix(path, base+"/") || path == base+"_test"
}
