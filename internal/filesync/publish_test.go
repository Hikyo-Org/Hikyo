//go:build unix

package filesync

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

const testTarget = "ftg_00000000-0000-0000-0000-000000000004"

func testKeys(t *testing.T) *crypto.LocalKeys {
	t.Helper()
	keys, err := crypto.LoadOrCreateLocalKey(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

type fixture struct {
	t    *testing.T
	dir  string
	keys *crypto.LocalKeys
	pol  Policy
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{t: t, dir: t.TempDir(), keys: testKeys(t), pol: Policy{Mode: 0o600, UID: -1, GID: -1, OnRemoved: RemovedRefuse}}
}

func (f *fixture) plan(files ...Rendered) Plan {
	return Plan{Target: testTarget, Stamp: GenerationStamp(f.keys, testTarget, f.pol, files), Files: files, Policy: f.pol}
}

func (f *fixture) publish(probe Probe, files ...Rendered) (Result, error) {
	f.t.Helper()
	d, err := OpenDestination(f.dir, false)
	if err != nil {
		return Result{}, err
	}
	defer d.Close()
	return d.Publish(context.Background(), f.plan(files...), probe)
}

func (f *fixture) mustPublish(files ...Rendered) Result {
	f.t.Helper()
	res, err := f.publish(nil, files...)
	if err != nil {
		f.t.Fatal(err)
	}
	return res
}

func (f *fixture) read(name string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

// set reads every name through ONE resolution of `current`, the way a reader
// that needs a consistent set does, and returns name=content pairs.
func (f *fixture) set() string {
	f.t.Helper()
	gen, err := os.Readlink(filepath.Join(f.dir, currentLink))
	if err != nil {
		f.t.Fatal(err)
	}
	dir := filepath.Join(f.dir, genDir, gen)
	entries, err := os.ReadDir(dir)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.Name() == completeName || e.Name() == previousName {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			f.t.Fatal(err)
		}
		out = append(out, e.Name()+"="+string(b))
	}
	return strings.Join(out, ";")
}

func file(name, content string) Rendered { return Rendered{Name: name, Content: []byte(content)} }

func TestPublishCreatesLinkedGeneration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	res := f.mustPublish(file("a.env", "A=1\n"), file("b.json", "{}\n"))
	if !res.Changed || res.Generation == "" {
		t.Fatalf("result = %+v", res)
	}
	if f.read("a.env") != "A=1\n" || f.read("b.json") != "{}\n" {
		t.Fatal("content mismatch")
	}
	for _, name := range []string{"a.env", "b.json"} {
		fi, err := os.Lstat(filepath.Join(f.dir, name))
		if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
			t.Fatalf("%s is not a link: %v", name, err)
		}
		real, err := os.Stat(filepath.Join(f.dir, name))
		if err != nil || real.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, %v", name, real.Mode(), err)
		}
	}
	d, err := OpenDestination(f.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	st, err := d.Current()
	if err != nil || st.Target != testTarget || st.Stamp != res.Stamp || !slices.Equal(st.Files, []string{"a.env", "b.json"}) {
		t.Fatalf("state = %+v, %v", st, err)
	}
	ok, err := d.Verify(f.plan(file("a.env", "A=1\n"), file("b.json", "{}\n")))
	if err != nil || !ok {
		t.Fatalf("verify = %v %v", ok, err)
	}
	ok, err = d.Verify(f.plan(file("a.env", "A=2\n"), file("b.json", "{}\n")))
	if err != nil || ok {
		t.Fatalf("verify of different content = %v %v", ok, err)
	}
}

func TestPublishIdempotentAndSwaps(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.mustPublish(file("a", "1"))
	again := f.mustPublish(file("a", "1"))
	if again.Changed || again.Generation != first.Generation {
		t.Fatalf("identical publish changed: %+v", again)
	}
	second := f.mustPublish(file("a", "2"))
	if !second.Changed || f.read("a") != "2" {
		t.Fatalf("swap = %+v content %q", second, f.read("a"))
	}
	third := f.mustPublish(file("a", "3"))
	// Collection keeps the new generation and its predecessor only.
	entries, err := os.ReadDir(filepath.Join(f.dir, genDir))
	if err != nil {
		t.Fatal(err)
	}
	var gens []string
	for _, e := range entries {
		if genGrammar.MatchString(e.Name()) {
			gens = append(gens, e.Name())
		}
	}
	slices.Sort(gens)
	want := []string{second.Generation, third.Generation}
	slices.Sort(want)
	if !slices.Equal(gens, want) {
		t.Fatalf("generations = %v, want %v", gens, want)
	}
}

func TestPublishModeChangeRebuilds(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "1"))
	f.pol.Mode = 0o640
	res := f.mustPublish(file("a", "1"))
	if !res.Changed {
		t.Fatal("mode change did not rebuild")
	}
	fi, err := os.Stat(filepath.Join(f.dir, "a"))
	if err != nil || fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v %v", fi.Mode(), err)
	}
	gi, err := os.Stat(filepath.Join(f.dir, genDir, res.Generation))
	if err != nil || gi.Mode().Perm() != 0o750 {
		t.Fatalf("generation dir mode = %v %v", gi.Mode(), err)
	}
}

// A recreated state dir (new local key) keeps the destination bytes identical
// but changes the keyed stamp. Publish must rebuild under the new stamp rather
// than reuse the old generation, or Intact can never match it again.
func TestPublishStampChangeRebuilds(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.mustPublish(file("a", "1"))
	f.keys = testKeys(t)
	res := f.mustPublish(file("a", "1"))
	if !res.Changed || res.Generation == first.Generation {
		t.Fatalf("stamp change reused generation: %+v", res)
	}
	d, err := OpenDestination(f.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, ok, err := d.Intact(f.keys, testTarget, f.pol, []string{"a"}); err != nil || !ok {
		t.Fatalf("rebuilt generation not intact under the new key: %v", err)
	}
}

func TestPublishRefusesForeignFilesAndSymlinks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	outside := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "victim"), filepath.Join(f.dir, "a")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.publish(nil, file("a", "secret")); !errors.Is(err, ErrForeign) {
		t.Fatalf("foreign symlink: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(outside, "victim")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("wrote through a foreign symlink")
	}
	if err := os.Remove(filepath.Join(f.dir, "a")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "a"), []byte("operator's"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.publish(nil, file("a", "secret")); !errors.Is(err, ErrForeign) {
		t.Fatalf("foreign regular file: %v", err)
	}
	if f.read("a") != "operator's" {
		t.Fatal("foreign file replaced")
	}
	// Nothing was staged for the refused publication.
	if _, err := os.Stat(filepath.Join(f.dir, currentLink)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("refused publication committed a generation")
	}
}

func TestOpenDestinationRefusesSymlinkedDirectory(t *testing.T) {
	t.Parallel()
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDestination(link, false); err == nil || !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("symlinked directory: %v", err)
	}
	if _, err := OpenDestination(filepath.Join(real, "missing"), false); err == nil {
		t.Fatal("missing directory opened (it must never be created)")
	}
}

func TestPublishRefusesForeignCurrentLink(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "1"))
	cur := filepath.Join(f.dir, currentLink)
	if err := os.Remove(cur); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../etc", cur); err != nil {
		t.Fatal(err)
	}
	if _, err := f.publish(nil, file("a", "2")); !errors.Is(err, ErrForeign) {
		t.Fatalf("tampered current: %v", err)
	}
}

func TestPublishRefusesOtherTargetsDirectory(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "1"))
	d, err := OpenDestination(f.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	other := f.plan(file("a", "2"))
	other.Target = "ftg_00000000-0000-0000-0000-00000000000f"
	if _, err := d.Publish(context.Background(), other, nil); !errors.Is(err, ErrBinding) {
		t.Fatalf("other target: %v", err)
	}
}

type faultProbe struct {
	writeErr  error
	failWrite string
	beforeErr error
	afterErr  error
}

func (p *faultProbe) BeforeWrite(name string) error {
	if name == p.failWrite {
		return p.writeErr
	}
	return nil
}
func (p *faultProbe) BeforeCommit() error { return p.beforeErr }
func (p *faultProbe) AfterCommit() error  { return p.afterErr }

var errCrash = errors.New("simulated crash")

func TestPublishDiskFullKeepsPriorGeneration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	old := f.mustPublish(file("a", "old-a"), file("b", "old-b"))
	_, err := f.publish(&faultProbe{failWrite: "b", writeErr: syscall.ENOSPC}, file("a", "new-a"), file("b", "new-b"))
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("err = %v", err)
	}
	if f.set() != "a=old-a;b=old-b" || f.read("a") != "old-a" {
		t.Fatalf("prior generation disturbed: %s", f.set())
	}
	assertOnlyGenerations(t, f.dir, old.Generation)
}

func TestPublishCrashBeforeCommitThenRecover(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	old := f.mustPublish(file("a", "old-a"), file("b", "old-b"))
	_, err := f.publish(&faultProbe{beforeErr: errCrash}, file("a", "new-a"), file("b", "new-b"))
	if !errors.Is(err, errCrash) {
		t.Fatalf("err = %v", err)
	}
	// A crash leaves the complete staged generation on disk but `current`
	// still names the old one: readers see the old set, never a mix.
	if f.set() != "a=old-a;b=old-b" || f.read("b") != "old-b" {
		t.Fatalf("readers see %s", f.set())
	}
	// A torn staging directory (no .complete) from a harder crash is also
	// left; the next run removes it and publishes.
	torn := filepath.Join(f.dir, genDir, "v1-00000000000000000000000000000000-0000000000000000")
	if err := os.Mkdir(torn, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(torn, "a"), []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := f.mustPublish(file("a", "new-a"), file("b", "new-b"))
	if f.set() != "a=new-a;b=new-b" {
		t.Fatalf("after recovery: %s", f.set())
	}
	if _, err := os.Stat(torn); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("torn generation survived recovery")
	}
	assertOnlyGenerations(t, f.dir, old.Generation, res.Generation)
}

// A commit that fails before the rename for any reason, not only a refusal,
// removes the staged generation: it already carries .complete, so recover()
// would keep it and its plaintext forever.
func TestPublishFailedCommitRemovesStagedGeneration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	old := f.mustPublish(file("a", "old-a"))
	current := filepath.Join(f.dir, genDir, "current")
	// A non-empty directory at `current` makes the commit's rename fail.
	probe := &beforeCommitProbe{run: func() {
		if err := os.Remove(current); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(current, "keep"), 0o700); err != nil {
			t.Fatal(err)
		}
	}}
	res, err := f.publish(probe, file("a", "new-a"))
	if err == nil || errors.Is(err, ErrForeign) || res.Changed {
		t.Fatalf("publish = %+v, %v; want a plain commit failure", res, err)
	}
	assertOnlyGenerations(t, f.dir, old.Generation)
}

func TestPublishCrashAfterCommitIsNewGeneration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "old-a"))
	// The new file b has no link yet when the crash hits; a reads new.
	_, err := f.publish(&faultProbe{afterErr: errCrash}, file("a", "new-a"), file("b", "new-b"))
	if !errors.Is(err, errCrash) {
		t.Fatalf("err = %v", err)
	}
	if f.set() != "a=new-a;b=new-b" || f.read("a") != "new-a" {
		t.Fatalf("after-commit crash: %s", f.set())
	}
	res := f.mustPublish(file("a", "new-a"), file("b", "new-b"))
	if res.Changed || f.read("b") != "new-b" {
		t.Fatalf("recovery run = %+v", res)
	}
}

func TestPublishCancelledBeforeCommit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	old := f.mustPublish(file("a", "old"))
	d, err := OpenDestination(f.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Publish(ctx, f.plan(file("a", "new")), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if f.read("a") != "old" {
		t.Fatal("cancelled publication committed")
	}
	assertOnlyGenerations(t, f.dir, old.Generation)
}

func TestCancelledPolicyChangePreservesSharedGenerationAccess(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.pol.Mode = 0o640
	f.mustPublish(file("a", "old"))
	f.pol.Mode = 0o600

	d, err := OpenDestination(f.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Publish(ctx, f.plan(file("a", "new")), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	fi, err := os.Stat(filepath.Join(f.dir, genDir))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o711 {
		t.Fatalf("shared generation directory mode = %o, want 711", got)
	}
	if f.read("a") != "old" {
		t.Fatal("cancelled publication changed the committed generation")
	}
}

func TestConcurrentClientRefused(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	d, err := OpenDestination(f.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDestination(f.dir, false); !errors.Is(err, ErrBusy) {
		t.Fatalf("second client: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d2, err := OpenDestination(f.dir, false)
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	d2.Close()
}

func TestPublishRemovedFileDecisions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "A"), file("b", "B"))
	if _, err := f.publish(nil, file("a", "A2")); !errors.Is(err, ErrRemoved) || !strings.Contains(err.Error(), "b") {
		t.Fatalf("refuse: %v", err)
	}
	if f.read("a") != "A" {
		t.Fatal("refused publication changed a")
	}

	f.pol.OnRemoved = RemovedRetain
	res := f.mustPublish(file("a", "A2"))
	if !slices.Equal(res.Retained, []string{"b"}) {
		t.Fatalf("retained = %v", res.Retained)
	}
	fi, err := os.Lstat(filepath.Join(f.dir, "b"))
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 || f.read("b") != "B" {
		t.Fatalf("retained b = %v %v", fi, err)
	}

	f.pol.OnRemoved = RemovedRefuse
	f.mustPublish(file("a", "A3"), file("c", "C"))
	f.pol.OnRemoved = RemovedPrune
	res = f.mustPublish(file("a", "A4"))
	if !slices.Equal(res.Pruned, []string{"c"}) {
		t.Fatalf("pruned = %v", res.Pruned)
	}
	if _, err := os.Lstat(filepath.Join(f.dir, "c")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("pruned link survived")
	}
	// The unmanaged retained file is never pruned.
	if f.read("b") != "B" {
		t.Fatal("prune touched an unmanaged file")
	}
}

func TestReadersNeverSeeMixedSet(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "0"), file("b", "0"))
	var stop atomic.Bool
	var mixed atomic.Int64
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				// A consistent reader resolves `current` once.
				root, err := os.OpenRoot(filepath.Join(f.dir, currentLink))
				if err != nil {
					continue // the generation it resolved was collected
				}
				a, errA := root.ReadFile("a")
				b, errB := root.ReadFile("b")
				root.Close()
				if errA == nil && errB == nil && string(a) != string(b) {
					mixed.Add(1)
				}
			}
		}()
	}
	for i := 1; i <= 30; i++ {
		v := strings.Repeat("x", i)
		f.mustPublish(file("a", v), file("b", v))
	}
	stop.Store(true)
	wg.Wait()
	if mixed.Load() != 0 {
		t.Fatalf("%d mixed reads", mixed.Load())
	}
}

func TestPublishOwnership(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.pol.UID, f.pol.GID = 4242, 4243
	_, err := f.publish(nil, file("a", "1"))
	if os.Geteuid() != 0 {
		// An unprivileged client cannot assign ownership and must not fall
		// back to its own.
		if err == nil || !strings.Contains(err.Error(), "chown") {
			t.Fatalf("unprivileged chown: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(f.dir, "a")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatal("published despite refused ownership")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(f.dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if uid, gid := ownerIDs(fi); uid != 4242 || gid != 4243 {
		t.Fatalf("owner = %d:%d", uid, gid)
	}
}

func TestRequireTmpfs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ok, err := isTmpfs(dir)
	d, openErr := OpenDestination(dir, true)
	if openErr == nil {
		d.Close()
	}
	switch {
	case err != nil:
		if !errors.Is(openErr, ErrNotTmpfs) {
			t.Fatalf("unverifiable tmpfs opened: %v", openErr)
		}
	case ok:
		if openErr != nil {
			t.Fatalf("tmpfs refused: %v", openErr)
		}
	default:
		if !errors.Is(openErr, ErrNotTmpfs) {
			t.Fatalf("non-tmpfs opened: %v", openErr)
		}
	}
}

func TestNoPlaintextOutsideDestination(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.pol.OnRemoved = RemovedPrune
	f.mustPublish(file("a", "SENTINEL-PLAINTEXT-1"), file("b", "SENTINEL-PLAINTEXT-2"))
	f.mustPublish(file("a", "SENTINEL-PLAINTEXT-3"))
	// Every byte of plaintext lives in a generation under .hikyo-gen; the
	// destination's top level holds only links and the client's own dir.
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == genDir {
			continue
		}
		if e.Type()&fs.ModeSymlink == 0 {
			t.Fatalf("unexpected non-link %s in destination", e.Name())
		}
	}
	err = filepath.WalkDir(f.dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || e.Type()&fs.ModeSymlink != 0 {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "SENTINEL") && !strings.HasPrefix(p, filepath.Join(f.dir, genDir)+string(filepath.Separator)) {
			t.Errorf("plaintext at %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertOnlyGenerations(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, genDir))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		if e.Name() != "binding" && e.Name() != "current" {
			got = append(got, e.Name())
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("generation entries = %v, want %v", got, want)
	}
}

func TestIntactDetectsTamperingAndPolicyChange(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	files := []Rendered{file("a", "1"), file("b", "2")}
	f.mustPublish(files...)
	d, err := OpenDestination(f.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, ok, err := d.Intact(f.keys, testTarget, f.pol, []string{"b", "a"}); err != nil || !ok {
		t.Fatalf("fresh publication not intact: %v", err)
	}
	if _, ok, _ := d.Intact(f.keys, testTarget, f.pol, []string{"a"}); ok {
		t.Fatal("a different name set read as intact")
	}
	other := f.pol
	other.Mode = 0o640
	if _, ok, _ := d.Intact(f.keys, testTarget, other, []string{"a", "b"}); ok {
		t.Fatal("a changed policy read as intact")
	}
	real, err := filepath.EvalSymlinks(filepath.Join(f.dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := d.Intact(f.keys, testTarget, f.pol, []string{"a", "b"}); ok {
		t.Fatal("tampered content read as intact")
	}
}

func TestEnsureLinksPreservesForeignEntries(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	d, err := OpenDestination(f.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := os.WriteFile(filepath.Join(f.dir, "a"), []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.ensureLinks([]Rendered{file("a", "secret")}); !errors.Is(err, ErrForeign) {
		t.Fatalf("foreign entry: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(f.dir, "a"))
	if err != nil || string(got) != "foreign" {
		t.Fatalf("foreign entry changed: %q, %v", got, err)
	}
}

// genDirEntries lists the generation directories under .hikyo-gen.
func genDirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, genDir))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if genGrammar.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestPublishRefusesUnsafeGenDir(t *testing.T) {
	t.Parallel()
	// 0o320 is also unreadable by its owner: the refusal must come from the
	// custody check, not from a failed open.
	for _, mode := range []os.FileMode{0o777, 0o320} {
		f := newFixture(t)
		gen := filepath.Join(f.dir, genDir)
		if err := os.Mkdir(gen, 0o700); err != nil {
			t.Fatal(err)
		}
		// Mkdir is subject to the umask; set the mode explicitly.
		if err := os.Chmod(gen, mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(gen, 0o700) })
		if _, err := f.publish(nil, file("a", "1")); !errors.Is(err, ErrForeign) {
			t.Fatalf("group-writable %s (%v): %v", genDir, mode, err)
		}
		if _, err := os.Lstat(filepath.Join(f.dir, "a")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("published into an unsafe generation directory (%v)", mode)
		}
	}
}

func TestPublishRefusesForeignOwnedGenDir(t *testing.T) {
	t.Parallel()
	if os.Geteuid() != 0 {
		t.Skip("assigning a foreign owner needs root")
	}
	f := newFixture(t)
	gen := filepath.Join(f.dir, genDir)
	if err := os.Mkdir(gen, 0o711); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(gen, 4242, 4243); err != nil {
		t.Fatal(err)
	}
	if _, err := f.publish(nil, file("a", "1")); !errors.Is(err, ErrForeign) {
		t.Fatalf("recipient-owned %s: %v", genDir, err)
	}
}

// swapProbe replaces the staging generation directory with a symlink to
// victim (a directory inside the destination) once staging has begun, the way
// a recipient with write access to the destination would.
type swapProbe struct {
	faultProbe
	t      *testing.T
	dir    string
	victim string
	done   bool
}

func (p *swapProbe) BeforeWrite(string) error {
	if p.done {
		return nil
	}
	p.done = true
	gens := genDirEntries(p.t, p.dir)
	if len(gens) != 1 {
		p.t.Fatalf("staging generations = %v", gens)
	}
	staged := filepath.Join(p.dir, genDir, gens[0])
	if err := os.Rename(staged, staged+"-moved"); err != nil {
		p.t.Fatal(err)
	}
	return os.Symlink(filepath.Join("..", p.victim), staged)
}

func TestPublishGenerationSwapDoesNotChmodTarget(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	victim := filepath.Join(f.dir, "victim")
	if err := os.Mkdir(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	probe := &swapProbe{t: t, dir: f.dir, victim: "victim"}
	res, err := f.publish(probe, file("a", "1"))
	if !probe.done {
		t.Fatal("probe never ran")
	}
	// The staged files live in the moved directory; `current` would name the
	// symlink, so readers could not open them. That is not a publication.
	if err == nil || res.Changed {
		t.Fatalf("publish through a swapped generation = %+v, %v; want a refusal", res, err)
	}
	if _, err := os.Lstat(filepath.Join(f.dir, "a")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("public path a exists after a refused publication: %v", err)
	}
	fi, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("victim directory mode = %v; the publisher followed the swapped generation symlink", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(victim); len(entries) != 0 {
		t.Fatalf("publisher wrote %d entries through the swapped generation symlink", len(entries))
	}
}

// retainFixture publishes a and b, then returns the path of b inside the
// current generation, ready for a retain-on-removed publication of a alone.
func retainFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	f := newFixture(t)
	f.mustPublish(file("a", "A"), file("b", "B"))
	f.pol.OnRemoved = RemovedRetain
	gen, err := os.Readlink(filepath.Join(f.dir, currentLink))
	if err != nil {
		t.Fatal(err)
	}
	return f, filepath.Join(f.dir, genDir, gen, "b")
}

func assertNotLeaked(t *testing.T, f *fixture, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("retain accepted a planted generation entry")
	}
	if !errors.Is(err, ErrForeign) {
		t.Fatalf("retain over a planted generation entry = %v, want ErrForeign", err)
	}
	if fi, lerr := os.Lstat(filepath.Join(f.dir, "b")); lerr == nil && fi.Mode().IsRegular() {
		t.Fatalf("regular file b was materialized: %q", f.read("b"))
	}
}

type beforeCommitProbe struct {
	faultProbe
	run func()
}

func (p *beforeCommitProbe) BeforeCommit() error { p.run(); return nil }

func TestRetainRefusesSymlinkPlantedInOldGeneration(t *testing.T) {
	t.Parallel()
	f, old := retainFixture(t)
	// Planted after generationFiles has accepted the generation and before
	// retain copies it. The target is a readable sibling inside .hikyo-gen, so
	// only readGenFile's own guard can refuse it; a target outside .hikyo-gen
	// would also fail on os.Root's escape check.
	probe := &beforeCommitProbe{run: func() {
		if err := os.Remove(old); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("a", old); err != nil {
			t.Fatal(err)
		}
	}}
	_, err := f.publish(probe, file("a", "A2"))
	assertNotLeaked(t, f, err)
}

func TestRetainRefusesHardLinkInOldGeneration(t *testing.T) {
	t.Parallel()
	f, old := retainFixture(t)
	secret := filepath.Join(f.dir, "secret")
	if err := os.WriteFile(secret, []byte("ROOT-ONLY"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(secret, old); err != nil {
		t.Fatal(err)
	}
	_, err := f.publish(nil, file("a", "A2"))
	assertNotLeaked(t, f, err)
}

func TestRetainCapsModeAtConfigLimit(t *testing.T) {
	t.Parallel()
	f, old := retainFixture(t)
	if err := os.Chmod(old, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := f.publish(nil, file("a", "A2")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(filepath.Join(f.dir, "b"))
	if err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("retained b = %v %v", fi, err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("retained mode = %v, want the 0640 cap", fi.Mode().Perm())
	}
}

// currentGenPath is the path of name inside the current generation.
func currentGenPath(t *testing.T, f *fixture, name string) (gen, p string) {
	t.Helper()
	gen, err := os.Readlink(filepath.Join(f.dir, genDir, currentName))
	if err != nil {
		t.Fatal(err)
	}
	return gen, filepath.Join(f.dir, genDir, gen, name)
}

func TestPublishRepublishesOverHardLinkPlantedInCurrentGeneration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "A"))
	_, planted := currentGenPath(t, f, "a")
	secret := filepath.Join(f.dir, "secret")
	if err := os.WriteFile(secret, []byte("ROOT-ONLY"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(planted); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(secret, planted); err != nil {
		t.Fatal(err)
	}
	res, err := f.publish(nil, file("a", "A"))
	if err != nil || !res.Changed {
		t.Fatalf("publish over a planted hard link = %+v, %v; want a fresh generation", res, err)
	}
	if got := f.read("a"); got != "A" {
		t.Fatalf("a = %q, want the published content", got)
	}
}

func TestRetainFailureDoesNotLeakStagedGeneration(t *testing.T) {
	t.Parallel()
	f, old := retainFixture(t)
	oldGen, _ := currentGenPath(t, f, "b")
	secret := filepath.Join(f.dir, "secret")
	if err := os.WriteFile(secret, []byte("ROOT-ONLY"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(secret, old); err != nil {
		t.Fatal(err)
	}
	// Every poll fails the same way; none may leave another plaintext generation.
	for range 3 {
		if _, err := f.publish(nil, file("a", "A2")); err == nil {
			t.Fatal("retain accepted a hard-linked generation entry")
		}
		assertOnlyGenerations(t, f.dir, oldGen)
	}
}

// bounded runs fn and fails the test, instead of hanging the suite, when a
// planted FIFO makes it block.
func bounded(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("blocked on a recipient-planted FIFO")
	}
}

func plantFIFO(t *testing.T, f *fixture, name string) {
	t.Helper()
	_, p := currentGenPath(t, f, name)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFIFOInPlaceOfCompleteMarkerDoesNotBlock(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "A"))
	plantFIFO(t, f, completeName)
	bounded(t, func() {
		d, err := OpenDestination(f.dir, false)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = d.Close() }()
		if _, err := d.Current(); !errors.Is(err, ErrForeign) {
			t.Errorf("Current over a FIFO marker = %v, want ErrForeign", err)
		}
	})
	bounded(t, func() {
		if res, err := f.publish(nil, file("a", "A")); err != nil || !res.Changed {
			t.Errorf("publish over a FIFO marker = %+v, %v; want a fresh generation", res, err)
		}
	})
}

func TestFIFOInPlaceOfPreviousMarkerDoesNotBlock(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "A"))
	plantFIFO(t, f, previousName)
	bounded(t, func() {
		if res, err := f.publish(nil, file("a", "A")); err != nil || !res.Changed {
			t.Errorf("publish over a FIFO predecessor = %+v, %v; want a fresh generation", res, err)
		}
	})
}

func TestOversizedMarkerIsForeignAndRepublished(t *testing.T) {
	t.Parallel()
	for _, marker := range []string{completeName, previousName} {
		f := newFixture(t)
		f.mustPublish(file("a", "A"))
		_, p := currentGenPath(t, f, marker)
		if err := os.WriteFile(p, bytes.Repeat([]byte("x"), markerLimit+1), 0o600); err != nil {
			t.Fatal(err)
		}
		if marker == completeName {
			d, err := OpenDestination(f.dir, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Current(); !errors.Is(err, ErrForeign) {
				t.Errorf("Current over an oversized marker = %v, want ErrForeign", err)
			}
			_ = d.Close()
		}
		if res, err := f.publish(nil, file("a", "A")); err != nil || !res.Changed {
			t.Errorf("publish over an oversized %s = %+v, %v; want a fresh generation", marker, res, err)
		}
	}
}

// Every invalid predecessor marker must take the same ErrForeign recovery as an
// oversized one, or the same-content path refuses on every poll.
func TestInvalidPredecessorIsForeignAndRepublished(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		content func(current string) string
	}{
		{"just over the name limit", func(string) string { return strings.Repeat("x", predecessorLimit+1) }},
		{"not a generation name", func(string) string { return "zzz\n" }},
		{"names itself", func(current string) string { return current + "\n" }},
	} {
		f := newFixture(t)
		f.mustPublish(file("a", "A"))
		current, p := currentGenPath(t, f, previousName)
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(tc.content(current)), 0o600); err != nil {
			t.Fatal(err)
		}
		if res, err := f.publish(nil, file("a", "A")); err != nil || !res.Changed {
			t.Errorf("%s: publish over the predecessor = %+v, %v; want a fresh generation", tc.name, res, err)
		}
	}
}

// A FIFO swapped in for .hikyo-gen after pinGen's Lstat must fail the open,
// not block the publisher while it holds the destination lock.
func TestOpenDirRootRefusesFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, genDir), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	bounded(t, func() {
		if gen, err := openDirRoot(root, genDir); err == nil {
			_ = gen.Close()
			t.Error("opened a FIFO as the generation directory")
		}
	})
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	gen, err := openDirRoot(root, "real")
	if err != nil {
		t.Fatalf("a real directory must still open: %v", err)
	}
	_ = gen.Close()
}

// A symlink planted at a still-configured name is replaced from the plan
// without being read, like a planted hard link.
func TestPublishRepublishesOverSymlinkPlantedInCurrentGeneration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "A"), file("b", "B"))
	_, planted := currentGenPath(t, f, "a")
	if err := os.Remove(planted); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("b", planted); err != nil {
		t.Fatal(err)
	}
	res, err := f.publish(nil, file("a", "A"), file("b", "B"))
	if err != nil || !res.Changed {
		t.Fatalf("publish over a planted symlink = %+v, %v; want a fresh generation", res, err)
	}
	if got := f.read("a"); got != "A" {
		t.Fatalf("a = %q, want the published content", got)
	}
}

// A symlink at a removed name is never copied when retaining it.
func TestRetainRefusesSymlinkPlantedBeforePublication(t *testing.T) {
	t.Parallel()
	f, old := retainFixture(t)
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", old); err != nil {
		t.Fatal(err)
	}
	_, err := f.publish(nil, file("a", "A2"))
	assertNotLeaked(t, f, err)
}

// A Destination reused across Intact and Publish (one CLI pass) must not vouch
// for a .hikyo-gen that moved after the first pin: the public links resolve
// through the name, not the handle.
func TestReusedDestinationRefusesMovedGenDir(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "A"))
	d, err := OpenDestination(f.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	for range 2 { // the second call reuses the pinned handle
		if _, ok, err := d.Intact(f.keys, testTarget, f.pol, []string{"a"}); err != nil || !ok {
			t.Fatalf("intact on an unchanged destination = %v, %v; want true", ok, err)
		}
	}
	if err := os.Rename(filepath.Join(f.dir, genDir), filepath.Join(f.dir, "away")); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := d.Intact(f.keys, testTarget, f.pol, []string{"a"}); !errors.Is(err, ErrForeign) || ok {
		t.Fatalf("intact after %s moved = %v, %v; want ErrForeign", genDir, ok, err)
	}
	res, err := d.Publish(context.Background(), f.plan(file("a", "A")), nil)
	if !errors.Is(err, ErrForeign) || res.Changed {
		t.Fatalf("same-content publish after %s moved = %+v, %v; want ErrForeign", genDir, res, err)
	}
}

// Moving .hikyo-gen after pinGen leaves the pinned handle on the moved
// directory while the public links still resolve through the name. Committing
// there would report a publication no reader can see.
func TestPublishRefusesCommitThroughMovedGenDir(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustPublish(file("a", "A"))
	oldGen, _ := currentGenPath(t, f, "a")
	away := filepath.Join(f.dir, "away")
	if err := os.Mkdir(away, 0o700); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(away, genDir)
	probe := &beforeCommitProbe{run: func() {
		if err := os.Rename(filepath.Join(f.dir, genDir), moved); err != nil {
			t.Fatal(err)
		}
	}}
	res, err := f.publish(probe, file("a", "A2"))
	if !errors.Is(err, ErrForeign) || res.Changed {
		t.Fatalf("publish through a moved %s = %+v, %v; want ErrForeign", genDir, res, err)
	}
	if got, err := os.Readlink(filepath.Join(moved, currentName)); err != nil || got != oldGen {
		t.Fatalf("moved current = %q, %v; want the untouched %s", got, err, oldGen)
	}
	if _, err := os.ReadFile(filepath.Join(f.dir, "a")); err == nil {
		t.Fatal("public path a resolved after its generation directory moved")
	}
	// The refused staged generation is removed, not left to leak plaintext.
	assertOnlyGenerations(t, away, oldGen)
}
