package filesync

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

// Publication layout (#164 § atomic multi-file generation).
//
// Every binding publishes the same way, one file or sixty-four:
//
//	<dir>/.hikyo-gen/binding          the target id this directory belongs to
//	<dir>/.hikyo-gen/<gen>/<name>     immutable generation, fsynced, then .complete
//	<dir>/.hikyo-gen/current -> <gen> the commit point: one rename of a symlink
//	<dir>/<name> -> .hikyo-gen/current/<name>   stable, created once
//
// A reader that opens <dir>/<name> resolves `current` at open time, so it sees
// either the prior complete generation or the new complete generation; a crash
// or cancellation at any step leaves `current` naming a complete generation,
// because nothing ever writes into a generation `current` names. This is the
// Kubernetes projected-volume shape, chosen over per-file renames because
// renames alone cannot make a SET of files change atomically.
//
// Everything below goes through an os.Root opened on <dir>: no path the client
// writes can escape the directory, the only symlinks followed are the ones the
// client itself created inside it, and a foreign symlink or regular file at a
// configured name is refused rather than replaced.

const (
	genDir        = ".hikyo-gen"
	bindingFile   = genDir + "/binding"
	currentLink   = genDir + "/current"
	completeName  = ".complete"
	previousName  = ".previous"
	tmpLinkPrefix = ".hikyo-link-"
	retainPrefix  = ".hikyo-retain-"
	// keptGenerations beyond the new one: the previous generation survives one
	// publication so a reader that resolved `current` just before the swap can
	// finish reading.
	keptGenerations = 1
	stampDomain     = "hikyo-file-sync-v1\x00"
)

// genGrammar is a generation directory name: the keyed stamp plus a random
// suffix, so a rebuilt generation never reuses a name a reader may hold.
var genGrammar = regexp.MustCompile(`^v1-[0-9a-f]{32}-[0-9a-f]{16}$`)

var (
	// ErrForeign refuses to replace something the client did not create.
	ErrForeign = errors.New("filesync: destination holds a file this client did not publish")
	// ErrRemoved refuses a publication that drops a previously published file
	// while on_removed is refuse.
	ErrRemoved = errors.New("filesync: a previously published file is no longer configured")
	// ErrBinding refuses a directory that belongs to a different file target.
	ErrBinding = errors.New("filesync: destination directory belongs to a different file target")
	// ErrBusy refuses a second concurrent client on the same directory.
	ErrBusy = errors.New("filesync: another hikyo file-sync process holds this destination")
	// ErrNotTmpfs refuses a require_tmpfs destination that is not in memory.
	ErrNotTmpfs = errors.New("filesync: destination directory is not on tmpfs")
	// ErrUnsupported is a platform that cannot honour the requested policy.
	ErrUnsupported = errors.New("filesync: not supported on this platform")
)

// Probe is the fault seam. Production passes nil; tests inject errors at each
// durability boundary (a write that fails like a full disk, a crash before or
// after the commit) and assert the reader-visible invariant.
type Probe interface {
	BeforeWrite(name string) error
	BeforeCommit() error
	AfterCommit() error
}

// Policy is the ownership, mode and removal policy applied to one publication.
type Policy struct {
	Mode      os.FileMode
	UID, GID  int // -1 leaves the process's own
	OnRemoved OnRemoved
}

// Destination is an opened, locked destination directory.
type Destination struct {
	dir  string
	root *os.Root
	lock *os.File
}

// OpenDestination opens dir root-bound and takes the non-blocking directory
// lock. dir must be an existing directory and not itself a symlink; it is
// never created, so a typo cannot materialize plaintext somewhere new.
func OpenDestination(dir string, requireTmpfs bool) (*Destination, error) {
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("filesync: destination directory: %w", err)
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("filesync: destination directory %s is a symlink; name the real directory", dir)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("filesync: destination %s is not a directory", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("filesync: open destination: %w", err)
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(fi, opened) {
		root.Close()
		return nil, fmt.Errorf("filesync: destination %s changed while it was being opened", dir)
	}
	lock, err := lockDirectory(dir, fi)
	if err != nil {
		root.Close()
		return nil, err
	}
	if requireTmpfs {
		ok, err := isTmpfsFile(lock)
		if err != nil {
			_ = unlockDirectory(lock)
			root.Close()
			return nil, fmt.Errorf("%w: %s: %v", ErrNotTmpfs, dir, err)
		}
		if !ok {
			_ = unlockDirectory(lock)
			root.Close()
			return nil, fmt.Errorf("%w: %s", ErrNotTmpfs, dir)
		}
	}
	return &Destination{dir: dir, root: root, lock: lock}, nil
}

// Close releases the lock and the root.
func (d *Destination) Close() error {
	return errors.Join(unlockDirectory(d.lock), d.root.Close())
}

// Dir is the destination path.
func (d *Destination) Dir() string { return d.dir }

// GenerationStamp is the keyed stamp over one complete publication: the
// target, the policy and every (name, content) pair. Keyed, never a bare
// digest, because it is a function of secret plaintext (compose ADR).
func GenerationStamp(keys *crypto.LocalKeys, target string, policy Policy, files []Rendered) string {
	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(a, b Rendered) int { return strings.Compare(a.Name, b.Name) })
	var buf bytes.Buffer
	buf.WriteString(stampDomain)
	fmt.Fprintf(&buf, "%s\x00%o\x00%d\x00%d\x00", target, policy.Mode.Perm(), policy.UID, policy.GID)
	for _, f := range sorted {
		fmt.Fprintf(&buf, "%d:%s%d:", len(f.Name), f.Name, len(f.Content))
		buf.Write(f.Content)
	}
	return keys.Stamp(buf.Bytes())
}

// Plan is one publication.
type Plan struct {
	Target string
	Stamp  string
	Files  []Rendered
	Policy Policy
}

// Result reports what one publication did.
type Result struct {
	Changed    bool
	Generation string
	Stamp      string
	Pruned     []string
	Retained   []string
}

// State is the published state read back from the directory.
type State struct {
	Target     string
	Generation string
	Stamp      string
	Files      []string
}

// Current reads the committed state. A directory never published to returns
// a zero State and no error.
func (d *Destination) Current() (State, error) {
	var st State
	binding, err := d.root.ReadFile(bindingFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return st, nil
	case err != nil:
		return st, err
	}
	st.Target = strings.TrimSpace(string(binding))
	gen, err := d.currentGeneration()
	if err != nil || gen == "" {
		return st, err
	}
	st.Generation = gen
	stamp, err := d.root.ReadFile(path.Join(genDir, gen, completeName))
	if err != nil {
		return st, fmt.Errorf("filesync: current generation %s is incomplete: %w", gen, err)
	}
	st.Stamp = strings.TrimSpace(string(stamp))
	st.Files, err = d.generationFiles(gen)
	return st, err
}

// Verify reports whether every configured name resolves, through this
// client's own link, to the current generation's file with the given content,
// mode and ownership. It is the "destination intact" test that decides whether
// a stored cursor may be presented.
func (d *Destination) Verify(plan Plan) (bool, error) {
	st, err := d.Current()
	if err != nil || st.Generation == "" || st.Target != plan.Target {
		return false, err
	}
	if !slices.Equal(st.Files, sortedNames(plan.Files)) {
		return false, nil
	}
	for _, f := range plan.Files {
		if ok, err := d.linkIsOurs(f.Name); err != nil || !ok {
			return false, err
		}
	}
	return d.generationMatches(st.Generation, plan)
}

// Intact re-derives the current generation's keyed stamp from the bytes on
// disk and the policy, and reports whether it equals the stamp the generation
// was committed under, the generation holds exactly names, every name links
// to it, and every file carries the policy's mode and ownership. A wiped
// tmpfs, a hand-edited file or a changed policy all read as not intact, which
// is what stops a stored cursor from claiming "current" over them.
func (d *Destination) Intact(keys *crypto.LocalKeys, target string, policy Policy, names []string) (State, bool, error) {
	st, err := d.Current()
	if err != nil || st.Generation == "" || st.Target != target {
		return st, false, err
	}
	want := slices.Clone(names)
	slices.Sort(want)
	if !slices.Equal(st.Files, want) {
		return st, false, nil
	}
	files := make([]Rendered, 0, len(want))
	defer func() {
		for _, f := range files {
			crypto.Zero(f.Content)
		}
	}()
	for _, name := range want {
		if ok, err := d.linkIsOurs(name); err != nil || !ok {
			return st, false, err
		}
		p := path.Join(genDir, st.Generation, name)
		fi, err := d.root.Lstat(p)
		if err != nil {
			return st, false, err
		}
		if fi.Mode().Perm() != policy.Mode.Perm() || !ownedAs(fi, policy.UID, policy.GID) {
			return st, false, nil
		}
		content, err := d.root.ReadFile(p)
		if err != nil {
			return st, false, err
		}
		files = append(files, Rendered{Name: name, Content: content})
	}
	return st, GenerationStamp(keys, target, policy, files) == st.Stamp, nil
}

// OnTmpfs reports whether dir is on tmpfs; off Linux it cannot tell and
// returns ErrUnsupported.
func OnTmpfs(dir string) (bool, error) { return isTmpfs(dir) }

// Publish commits plan as one generation. On any error before the commit the
// previously current generation is untouched; after the commit the new one is
// current even if link creation, pruning or collection then fails.
func (d *Destination) Publish(ctx context.Context, plan Plan, probe Probe) (Result, error) {
	res := Result{Stamp: plan.Stamp}
	if err := crypto.ParseStamp(plan.Stamp); err != nil {
		return res, err
	}
	if len(plan.Files) == 0 {
		return res, errors.New("filesync: a publication needs at least one file")
	}
	for _, f := range plan.Files {
		if !fileNameGrammar.MatchString(f.Name) {
			return res, fmt.Errorf("filesync: refusing file name %q", f.Name)
		}
	}
	if err := d.claimBinding(plan.Target); err != nil {
		return res, err
	}
	if err := d.ensureGenDirAccess(); err != nil {
		return res, err
	}
	if err := d.recover(); err != nil {
		return res, err
	}
	// Preflight every configured name BEFORE staging anything: a foreign file
	// is refused while the directory is still exactly as it was.
	for _, f := range plan.Files {
		ok, err := d.linkIsOurs(f.Name)
		if err != nil {
			return res, err
		}
		if !ok {
			if _, err := d.root.Lstat(f.Name); err == nil {
				return res, fmt.Errorf("%w: %s; move it away (or delete it) and render again", ErrForeign, path.Join(d.dir, f.Name))
			} else if !errors.Is(err, fs.ErrNotExist) {
				return res, err
			}
		}
	}
	oldGen, err := d.currentGeneration()
	if err != nil {
		return res, err
	}
	var previous []string
	if oldGen != "" {
		if previous, err = d.generationFiles(oldGen); err != nil {
			return res, err
		}
	}
	removed := setMinus(previous, sortedNames(plan.Files))
	// A previous attempt may have committed current before pruning. Its new
	// manifest no longer lists removed names; exact managed links retain them.
	desiredNames := sortedNames(plan.Files)
	entries, err := d.readDir(".")
	if err != nil {
		return res, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !fileNameGrammar.MatchString(name) || slices.Contains(desiredNames, name) || slices.Contains(removed, name) {
			continue
		}
		ours, err := d.linkIsOurs(name)
		if err != nil {
			return res, err
		}
		if ours {
			removed = append(removed, name)
		}
	}
	slices.Sort(removed)
	if len(removed) > 0 && plan.Policy.OnRemoved == RemovedRefuse {
		return res, fmt.Errorf("%w: %s; set destination.on_removed to retain or prune", ErrRemoved, strings.Join(removed, ", "))
	}
	if oldGen != "" {
		same, err := d.generationMatches(oldGen, plan)
		if err != nil {
			return res, err
		}
		if same {
			prior, err := d.predecessor(oldGen)
			if err == nil {
				res.Generation = oldGen
				if err := d.ensureLinks(plan.Files); err != nil {
					return res, err
				}
				if err := d.pruneRemoved(plan, removed, &res); err != nil {
					return res, err
				}
				return res, d.collect(oldGen, prior)
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return res, err
			}
			// Legacy generations have no durable predecessor. Republish the
			// same content once, preserving this known current as predecessor.
		}
	}

	suffix, err := randomHex(8)
	if err != nil {
		return res, err
	}
	newGen := plan.Stamp + "-" + suffix
	if err := d.stage(newGen, plan, probe, oldGen); err != nil {
		// A staging failure (a full disk, a refused chown) removes the partial
		// generation; `current` was never touched.
		_ = d.root.RemoveAll(path.Join(genDir, newGen))
		return res, err
	}
	if err := ctx.Err(); err != nil {
		_ = d.root.RemoveAll(path.Join(genDir, newGen))
		return res, fmt.Errorf("filesync: cancelled before commit: %w", err)
	}
	if probe != nil {
		if err := probe.BeforeCommit(); err != nil {
			return res, err
		}
	}
	if plan.Policy.OnRemoved == RemovedRetain {
		for _, name := range removed {
			if err := d.retain(oldGen, name); err != nil {
				return res, err
			}
			res.Retained = append(res.Retained, name)
		}
	}
	if err := d.swapCurrent(newGen); err != nil {
		return res, err
	}
	res.Changed, res.Generation = true, newGen
	if probe != nil {
		if err := probe.AfterCommit(); err != nil {
			return res, err
		}
	}
	if err := d.ensureLinks(plan.Files); err != nil {
		return res, err
	}
	if err := d.pruneRemoved(plan, removed, &res); err != nil {
		return res, err
	}
	return res, d.collect(newGen, oldGen)
}

func (d *Destination) predecessor(current string) (string, error) {
	name := path.Join(genDir, current, previousName)
	info, err := d.root.Lstat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 64 {
		return "", errors.New("filesync: invalid predecessor metadata")
	}
	file, err := d.root.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 65))
	if err != nil {
		return "", err
	}
	prior := strings.TrimSpace(string(raw))
	if len(raw) > 64 || (prior != "" && (!genGrammar.MatchString(prior) || prior == current)) {
		return "", errors.New("filesync: invalid predecessor generation")
	}
	return prior, nil
}

func (d *Destination) pruneRemoved(plan Plan, removed []string, res *Result) error {
	if plan.Policy.OnRemoved == RemovedPrune {
		for _, name := range removed {
			ok, err := d.linkIsOurs(name)
			if err != nil {
				return err
			}
			if ok {
				if err := d.root.Remove(name); err != nil {
					return err
				}
				res.Pruned = append(res.Pruned, name)
			}
		}
		if err := d.syncDir("."); err != nil {
			return err
		}
	}
	return nil
}

// claimBinding records the target on first use and refuses a directory that
// already belongs to another one: two bindings sharing a directory would
// otherwise overwrite each other's `current`.
func (d *Destination) claimBinding(target string) error {
	fi, err := d.root.Lstat(genDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := d.root.Mkdir(genDir, 0o711); err != nil {
			return err
		}
		if err := d.writeFileSynced(bindingFile, []byte(target+"\n"), 0o600); err != nil {
			return err
		}
		if err := d.syncDir(genDir); err != nil {
			return err
		}
		return d.syncDir(".")
	case err != nil:
		return err
	case !fi.IsDir():
		return fmt.Errorf("%w: %s is not a directory", ErrForeign, path.Join(d.dir, genDir))
	}
	raw, err := d.root.ReadFile(bindingFile)
	if errors.Is(err, fs.ErrNotExist) {
		// The binding is written right after the directory; a crash between
		// the two leaves an empty directory the first run may claim.
		entries, rerr := d.readDir(genDir)
		if rerr != nil {
			return rerr
		}
		if len(entries) > 0 {
			return fmt.Errorf("%w: %s has no binding record", ErrForeign, path.Join(d.dir, genDir))
		}
		if err := d.writeFileSynced(bindingFile, []byte(target+"\n"), 0o600); err != nil {
			return err
		}
		return d.syncDir(genDir)
	}
	if err != nil {
		return err
	}
	if got := strings.TrimSpace(string(raw)); got != target {
		return fmt.Errorf("%w: %s is bound to %s, not %s", ErrBinding, d.dir, got, target)
	}
	return nil
}

// recover removes what a crash can leave behind: a generation without its
// .complete marker that `current` does not name, and temporary links.
func (d *Destination) recover() error {
	current, err := d.currentGeneration()
	if err != nil {
		return err
	}
	entries, err := d.readDir(genDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "current.tmp-"):
			if err := d.root.Remove(path.Join(genDir, name)); err != nil {
				return err
			}
		case genGrammar.MatchString(name) && name != current:
			if _, err := d.root.Lstat(path.Join(genDir, name, completeName)); errors.Is(err, fs.ErrNotExist) {
				if err := d.root.RemoveAll(path.Join(genDir, name)); err != nil {
					return err
				}
			}
		}
	}
	top, err := d.readDir(".")
	if err != nil {
		return err
	}
	for _, e := range top {
		stale := (strings.HasPrefix(e.Name(), tmpLinkPrefix) && e.Type()&fs.ModeSymlink != 0) ||
			(strings.HasPrefix(e.Name(), retainPrefix) && strings.HasSuffix(e.Name(), ".tmp") && e.Type().IsRegular())
		if stale {
			if err := d.root.Remove(e.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}

// currentGeneration reads the `current` link, refusing anything but a link to
// a well-formed sibling generation.
func (d *Destination) currentGeneration() (string, error) {
	fi, err := d.root.Lstat(currentLink)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if fi.Mode()&fs.ModeSymlink == 0 {
		return "", fmt.Errorf("%w: %s is not a link", ErrForeign, path.Join(d.dir, currentLink))
	}
	target, err := d.root.Readlink(currentLink)
	if err != nil {
		return "", err
	}
	if !genGrammar.MatchString(target) {
		return "", fmt.Errorf("%w: %s points at %q", ErrForeign, path.Join(d.dir, currentLink), target)
	}
	return target, nil
}

// generationFiles lists a generation's published names, sorted.
func (d *Destination) generationFiles(gen string) ([]string, error) {
	entries, err := d.readDir(path.Join(genDir, gen))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.Name() == completeName || e.Name() == previousName {
			continue
		}
		if !e.Type().IsRegular() {
			return nil, fmt.Errorf("%w: %s in generation %s is not a regular file", ErrForeign, e.Name(), gen)
		}
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names, nil
}

// generationMatches reports whether gen was committed under plan's keyed
// stamp and holds exactly plan's files with the planned content, mode and
// ownership. The stamp check matters: a recreated local key or a policy
// change that ownedAs tolerates (owner unset -> the process's own uid) keeps
// the bytes identical but changes the stamp, and reusing the generation would
// leave a .complete that Intact can never match again.
func (d *Destination) generationMatches(gen string, plan Plan) (bool, error) {
	stamp, err := d.root.ReadFile(path.Join(genDir, gen, completeName))
	if err != nil {
		return false, fmt.Errorf("filesync: generation %s is incomplete: %w", gen, err)
	}
	if strings.TrimSpace(string(stamp)) != plan.Stamp {
		return false, nil
	}
	names, err := d.generationFiles(gen)
	if err != nil {
		return false, err
	}
	if !slices.Equal(names, sortedNames(plan.Files)) {
		return false, nil
	}
	for _, f := range plan.Files {
		p := path.Join(genDir, gen, f.Name)
		fi, err := d.root.Lstat(p)
		if err != nil {
			return false, err
		}
		if fi.Mode().Perm() != plan.Policy.Mode.Perm() || !ownedAs(fi, plan.Policy.UID, plan.Policy.GID) {
			return false, nil
		}
		got, err := d.root.ReadFile(p)
		if err != nil {
			return false, err
		}
		same := bytes.Equal(got, f.Content)
		crypto.Zero(got)
		if !same {
			return false, nil
		}
	}
	return true, nil
}

// stage writes a complete, fsynced generation. Each file is created O_EXCL
// with owner-only mode, chowned and chmodded BEFORE any byte is written, so
// plaintext never exists under a wider mode or the wrong owner.
func (d *Destination) stage(gen string, plan Plan, probe Probe, previous string) error {
	dir := path.Join(genDir, gen)
	if err := d.root.Mkdir(dir, 0o700); err != nil {
		return err
	}
	for _, f := range plan.Files {
		if probe != nil {
			if err := probe.BeforeWrite(f.Name); err != nil {
				return fmt.Errorf("filesync: write %s: %w", f.Name, err)
			}
		}
		if err := d.writeOwned(path.Join(dir, f.Name), f.Content, plan.Policy); err != nil {
			return fmt.Errorf("filesync: write %s: %w", f.Name, err)
		}
	}
	if err := d.writeFileSynced(path.Join(dir, previousName), []byte(previous+"\n"), 0o600); err != nil {
		return err
	}
	if err := d.writeFileSynced(path.Join(dir, completeName), []byte(plan.Stamp+"\n"), 0o600); err != nil {
		return err
	}
	// The generation directory must let the configured owner and group reach
	// the files it holds; it never grants more than the file mode does.
	dirMode := os.FileMode(0o700)
	if plan.Policy.Mode&0o040 != 0 {
		dirMode = 0o750
	}
	if plan.Policy.UID >= 0 || plan.Policy.GID >= 0 {
		if err := d.root.Chown(dir, plan.Policy.UID, plan.Policy.GID); err != nil {
			return fmt.Errorf("filesync: chown generation: %w", err)
		}
	}
	if err := d.root.Chmod(dir, dirMode); err != nil {
		return err
	}
	if err := d.syncDir(dir); err != nil {
		return err
	}
	return d.syncDir(genDir)
}

// ensureGenDirAccess keeps the shared generation directory independent from
// any one generation's owner and mode. Execute-only access lets each
// generation enforce its own policy without exposing the directory listing.
func (d *Destination) ensureGenDirAccess() error {
	return d.root.Chmod(genDir, 0o711)
}

func (d *Destination) writeOwned(name string, content []byte, policy Policy) error {
	f, err := d.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if policy.UID >= 0 || policy.GID >= 0 {
		if err := f.Chown(policy.UID, policy.GID); err != nil {
			f.Close()
			return fmt.Errorf("chown to %d:%d refused (%w); run as a user that may assign it, or drop owner/group", policy.UID, policy.GID, err)
		}
	}
	if err := f.Chmod(policy.Mode.Perm()); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (d *Destination) writeFileSynced(name string, content []byte, perm os.FileMode) error {
	f, err := d.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// retain turns a dropped name into an unmanaged regular file holding its last
// published content, replacing the link in one rename.
func (d *Destination) retain(gen, name string) error {
	ok, err := d.linkIsOurs(name)
	if err != nil || !ok {
		return err
	}
	src := path.Join(genDir, gen, name)
	fi, err := d.root.Lstat(src)
	if err != nil {
		return err
	}
	content, err := d.root.ReadFile(src)
	if err != nil {
		return err
	}
	defer crypto.Zero(content)
	suffix, err := randomHex(8)
	if err != nil {
		return err
	}
	tmp := retainPrefix + suffix + ".tmp"
	uid, gid := ownerIDs(fi)
	if err := d.writeOwned(tmp, content, Policy{Mode: fi.Mode().Perm(), UID: uid, GID: gid}); err != nil {
		_ = d.root.Remove(tmp)
		return err
	}
	if err := d.root.Rename(tmp, name); err != nil {
		_ = d.root.Remove(tmp)
		return err
	}
	return d.syncDir(".")
}

// swapCurrent is the commit point: a fresh link renamed over `current`.
func (d *Destination) swapCurrent(gen string) error {
	suffix, err := randomHex(8)
	if err != nil {
		return err
	}
	tmp := currentLink + ".tmp-" + suffix
	if err := d.root.Symlink(gen, tmp); err != nil {
		return err
	}
	if err := d.root.Rename(tmp, currentLink); err != nil {
		_ = d.root.Remove(tmp)
		return err
	}
	return d.syncDir(genDir)
}

// ensureLinks creates the stable per-name links that do not exist yet. Each is
// created atomically without replacing anything that appeared since preflight.
func (d *Destination) ensureLinks(files []Rendered) error {
	created := false
	for _, f := range files {
		ok, err := d.linkIsOurs(f.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		// Symlink creation is atomic and refuses an existing name. A
		// check followed by rename would overwrite a concurrently added file.
		if err := d.root.Symlink(linkTarget(f.Name), f.Name); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("%w: %s", ErrForeign, path.Join(d.dir, f.Name))
			}
			return err
		}
		created = true
	}
	if created {
		return d.syncDir(".")
	}
	return nil
}

func linkTarget(name string) string { return currentLink + "/" + name }

// linkIsOurs reports whether name is exactly the link this client creates.
func (d *Destination) linkIsOurs(name string) (bool, error) {
	fi, err := d.root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if fi.Mode()&fs.ModeSymlink == 0 {
		return false, nil
	}
	target, err := d.root.Readlink(name)
	if err != nil {
		return false, err
	}
	return target == linkTarget(name), nil
}

// collect removes every generation except the new one and keptGenerations
// predecessors (the old current).
func (d *Destination) collect(newGen, oldGen string) error {
	entries, err := d.readDir(genDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if !genGrammar.MatchString(name) || name == newGen || (keptGenerations > 0 && name == oldGen) {
			continue
		}
		if err := d.root.RemoveAll(path.Join(genDir, name)); err != nil {
			return err
		}
	}
	return d.syncDir(genDir)
}

func (d *Destination) readDir(name string) ([]fs.DirEntry, error) {
	f, err := d.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

func (d *Destination) syncDir(name string) error {
	return syncRootDir(d.root, name)
}

func sortedNames(files []Rendered) []string {
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}
	slices.Sort(names)
	return names
}

func setMinus(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
