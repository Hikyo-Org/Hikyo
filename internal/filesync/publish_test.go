//go:build unix

package filesync

import (
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
		if e.Name() == completeName {
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
