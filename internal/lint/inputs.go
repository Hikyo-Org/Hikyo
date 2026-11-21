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
	"io"
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
// does not run its declared-input checks (appendonly's per-package Go scan still
// reports; it reads no declared input).
var (
	ErrUndeclaredInput = errors.New("undeclared input")
	ErrInputDigest     = errors.New("input digest mismatch")
)

// source is how an input-reading analyzer reaches the disk: through a verified
// Inputs under the vet driver, or straight from disk (osSource) for the helpers
// that run outside it.
type source interface {
	// ReadFile reads a regular file.
	ReadFile(path string) ([]byte, error)
	// ReadDir lists the names of the files directly in dir.
	ReadDir(dir string) ([]string, error)
	// WalkFiles lists every file under dir with one of the extensions.
	WalkFiles(dir string, exts []string) ([]string, error)
}

// osSource reads the disk directly, for the exported helpers outside the vet
// driver (ParseQueries, ParseScopeDirectives) and
// for checkUnanalyzedGoFiles, which runs in the test process.
// shortcut: it has no root to confine reads to, so a symlinked parent directory
// is followed (only the final directory component and the file's own directory
// are checked), and a walk skips a symlinked subdirectory; Inputs, which has the
// root, refuses both. Confine it with a root per caller if one of these helpers
// ever reads a path it does not control.
type osSource struct{}

// ReadFile reads through a handle to the file's directory, so a symlink that
// leaves it is refused (pathutil.ReadFileWithin).
func (osSource) ReadFile(path string) ([]byte, error) {
	return pathutil.ReadFileWithin(filepath.Dir(path), path)
}

// requireDir refuses a declared directory that is absent (an error wrapping
// fs.ErrNotExist), a symlink or not a directory, as Inputs does.
func requireDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory (a symlink or another file type is refused)", dir)
	}
	return nil
}

func (osSource) ReadDir(dir string) ([]string, error) {
	if err := requireDir(dir); err != nil {
		return nil, err
	}
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
	if err := requireDir(dir); err != nil {
		return nil, err
	}
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
	digest  string
	files   map[string]inputFile
	dirs    map[string]inputState // declared directory -> its state
	lists   map[string][]string   // declared directory -> files in it (absolute, sorted)
	recurse map[string]bool
}

// SnapshotInputs reads the declared inputs under root and computes their digest.
// Every read goes through a handle to root (os.OpenRoot), and any symlink on a
// declared path or inside a declared directory is refused, whether it points
// inside or outside the root.
func SnapshotInputs(root string, specs []InputSpec) (*Inputs, error) {
	root = filepath.Clean(root)
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rootHandle.Close() }()
	in := &Inputs{files: map[string]inputFile{}, dirs: map[string]inputState{}, lists: map[string][]string{}, recurse: map[string]bool{}}
	var lines []string
	read := func(path string) error {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, state, err := rootLstat(rootHandle, rel)
		switch {
		case err != nil:
			return err
		case state != inputPresent:
			in.files[path] = inputFile{state: state}
		case !info.Mode().IsRegular():
			in.files[path] = inputFile{state: inputIrregular}
		default:
			data, err := readSnapshotFile(rootHandle, rel, info)
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
		case spec.Dir != "" && spec.File == "":
			dir := filepath.Join(root, filepath.FromSlash(spec.Dir))
			if _, dup := in.dirs[dir]; dup {
				return nil, fmt.Errorf("input spec %+v names directory %s twice", spec, spec.Dir)
			}
			state, err := inputDirState(rootHandle, filepath.FromSlash(spec.Dir))
			if err != nil {
				return nil, err
			}
			var names []string
			if state == inputPresent {
				list, err := listInputDir(rootHandle, root, filepath.FromSlash(spec.Dir), spec)
				if err != nil {
					return nil, err
				}
				for _, path := range list {
					if err := read(path); err != nil {
						return nil, err
					}
					names = append(names, path)
				}
			}
			in.dirs[dir] = state
			in.lists[dir] = names
			in.recurse[dir] = spec.Recursive
			lines = append(lines, fmt.Sprintf("dir %s state=%d exts=%s recursive=%t", spec.Dir, state, strings.Join(spec.Exts, ","), spec.Recursive))
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

// rootLstat classifies rel under the root handle without following a symlink at
// any component: absent (some component does not exist), irregular (a parent
// component is a symlink or not a directory) or present, with the final
// component's Lstat info. The handle's own escape check backs the reads that
// follow.
func rootLstat(root *os.Root, rel string) (os.FileInfo, inputState, error) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, inputAbsent, nil
		case err != nil:
			return nil, 0, err
		case i == len(parts)-1:
			return info, inputPresent, nil
		case !info.IsDir():
			return nil, inputIrregular, nil
		}
	}
	return nil, 0, fmt.Errorf("empty input path %q", rel)
}

// readSnapshotFile reads a regular file through the root handle: it opens the
// path, checks the handle is the regular file Lstat saw, and reads from that
// handle, so a swap between the check and the read is refused instead of followed
// (and a swap to a symlink leaving the root is refused by the handle).
// shortcut: a FIFO swapped in between Lstat and Open would block the open; a
// non-regular file present at Lstat time is refused before any open. The declared
// inputs are repository files, so the swap is not defended; upgrade with
// O_NONBLOCK if the root ever becomes untrusted.
func readSnapshotFile(root *os.Root, rel string, lstat os.FileInfo) ([]byte, error) {
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(lstat, info) {
		return nil, fmt.Errorf("%s changed while the inputs were read", rel)
	}
	return io.ReadAll(f)
}

// inputDirState classifies a declared directory: absent, a directory, or
// irregular (a symlink, a symlinked parent or another file type, never
// followed). The state is part of the digest, so an absent directory differs
// from an empty one and either differs from a symlink.
func inputDirState(root *os.Root, rel string) (inputState, error) {
	info, state, err := rootLstat(root, rel)
	switch {
	case err != nil:
		return 0, err
	case state != inputPresent:
		return state, nil
	case !info.IsDir():
		return inputIrregular, nil
	}
	return inputPresent, nil
}

// listInputDir lists the files (absolute) a Dir spec selects in a directory that
// exists, walking through the root handle.
func listInputDir(root *os.Root, rootPath, rel string, spec InputSpec) ([]string, error) {
	var out []string
	base := filepath.ToSlash(rel)
	err := fs.WalkDir(root.FS(), base, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case !d.IsDir() && !d.Type().IsRegular():
			// A symlinked subdirectory would otherwise be skipped unhashed.
			return fmt.Errorf("declared directory %s holds a symlink or irregular entry %s", rel, path)
		case d.IsDir():
			if path != base && !spec.Recursive {
				return fs.SkipDir
			}
			return nil
		case hasExt(d.Name(), spec.Exts):
			out = append(out, filepath.Join(rootPath, filepath.FromSlash(path)))
		}
		return nil
	})
	slices.Sort(out)
	return out, err
}

// inputDigest is the digest the driver passes as the analyzer's -inputs flag.
func inputDigest(root string, specs []InputSpec) (string, error) {
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

// declaredDir returns the files of a declared directory, or the refusal: an
// undeclared directory, an absent one (wrapping fs.ErrNotExist, as the disk
// reader does) or one that is a symlink or not a directory.
func (in *Inputs) declaredDir(dir string) ([]string, error) {
	state, ok := in.dirs[dir]
	if !ok {
		return nil, fmt.Errorf("%w: directory %s", ErrUndeclaredInput, dir)
	}
	switch state {
	case inputAbsent:
		return nil, &fs.PathError{Op: "read", Path: dir, Err: fs.ErrNotExist}
	case inputIrregular:
		return nil, fmt.Errorf("%s is not a directory (a symlink or another file type is refused)", dir)
	}
	return in.lists[dir], nil
}

func (in *Inputs) ReadDir(dir string) ([]string, error) {
	dir = filepath.Clean(dir)
	list, err := in.declaredDir(dir)
	if err != nil {
		return nil, err
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
	if !in.recurse[dir] {
		return nil, fmt.Errorf("%w: recursive directory %s", ErrUndeclaredInput, dir)
	}
	list, err := in.declaredDir(dir)
	if err != nil {
		return nil, err
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

// inputDeclarations maps each input-reading analyzer to its declaration: the
// one source of truth the analyzer reads through and the driver test hashes.
var inputDeclarations = map[string]specsFunc{
	"containmentgates":  queryInputs,
	"appendonly":        appendOnlySpecs,
	"denialwriter":      queryInputs,
	"grantlock":         queryInputs,
	"rawsql":            rawSQLSpecs,
	"reencryptcoverage": reencryptSpecs,
	"sqlpredicate":      sqlPredicateSpecs,
	"stablecopies":      stableCopiesSpecs,
}

// inputDeclaration returns an input-reading analyzer's declared inputs under
// root, or false for an analyzer that reads none.
func inputDeclaration(analyzer, root string) ([]InputSpec, bool, error) {
	specs, ok := inputDeclarations[analyzer]
	if !ok {
		return nil, false, nil
	}
	out, err := specs(root)
	return out, true, err
}

// inputAnalyzers lists the analyzers that read declared inputs.
func inputAnalyzers() []string {
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
	// run returns the findings, or a refusal (an undeclared input) that is not
	// a finding about the code.
	run func(src source, root string) ([]string, error)
	// flags registers analyzer-specific flags run reads, if any.
	flags func(a *analysis.Analyzer)
	// pkg is an optional per-package part that reads the package's own Go files.
	pkg func(pass *analysis.Pass, root string)
}

// analyzer builds the vet analyzer.
func (c fileCheck) analyzer() *analysis.Analyzer {
	a := &analysis.Analyzer{Name: c.name, Doc: c.doc}
	anchor := c.anchor
	a.Flags.StringVar(&anchor, "anchor", c.anchor, "package the file checks run on")
	if c.flags != nil {
		c.flags(a)
	}
	in := flagInputs(a)
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
		if err := anchorUnderRoot(pass, root); err != nil {
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

// anchorUnderRoot refuses a -root that does not contain every file of the anchor
// package, so a digest of an unrelated tree's inputs cannot be paired with this
// anchor. An enclosing directory also contains the anchor and passes; its inputs
// are then read at its own paths, normally absent (so reported, not clean)
// unless that directory is itself a checkout, as with a nested worktree.
// Containment is by os.SameFile along each file's parent directories, not by
// string prefix, so a symlinked root, ".." segments or a case-insensitive
// filesystem cannot bypass it.
func anchorUnderRoot(pass *analysis.Pass, root string) error {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return err
	}
	for _, f := range pass.Files {
		name, err := filepath.Abs(pass.Fset.PositionFor(f.Pos(), false).Filename)
		if err != nil {
			return err
		}
		if !fileUnder(rootInfo, name) {
			return fmt.Errorf("-root does not contain the anchor package file %s", name)
		}
	}
	return nil
}

func fileUnder(root os.FileInfo, name string) bool {
	for dir := filepath.Dir(name); ; dir = filepath.Dir(dir) {
		if info, err := os.Stat(dir); err == nil && os.SameFile(root, info) {
			return true
		}
		if filepath.Dir(dir) == dir {
			return false
		}
	}
}

// pkgApplies is whether the per-package part runs on path: the anchor's module
// subtree, external test packages included.
func (c fileCheck) pkgApplies(anchor, path string) bool {
	base := strings.TrimSuffix(anchor, "/")
	return path == base || strings.HasPrefix(path, base+"/") || path == base+"_test"
}
