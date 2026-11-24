//go:build unix

package compose

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// foreignUID makes every directory this test creates look foreign-owned
// without needing root: the effective uid the package compares against is
// shifted.
func foreignUID(t *testing.T) {
	t.Helper()
	orig := effectiveUID
	effectiveUID = func() int { return orig() + 1 }
	t.Cleanup(func() { effectiveUID = orig })
}

func TestRuntimeCustodyRefusesForeignOwnedRuntimeDir(t *testing.T) {
	runtime := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	k := testKeys(t)
	rl := begin(t, t.TempDir(), nil)
	// Same pre-existing dir, owned by us: accepted.
	stamp, _, err := rl.WriteGeneration(runtime, k, "api", []byte("A=1\n"))
	if err != nil {
		t.Fatalf("own dir refused: %v", err)
	}
	cs := CursorState{Cursor: "v1:abc", Binding: baseBinding(), GenerationStamps: map[string]string{"api": stamp}}
	current := map[string]string{"api": stamp}
	if _, ok := EligibleCursor(&cs, baseBinding(), current, runtime); !ok {
		t.Fatal("precondition: cursor eligible in an owned dir")
	}

	foreignUID(t)
	if _, _, err := rl.WriteGeneration(runtime, k, "api", []byte("A=2\n")); err == nil || !strings.Contains(err.Error(), "owned by uid") {
		t.Fatalf("WriteGeneration err=%v, want foreign-owner refusal", err)
	}
	if _, ok := EligibleCursor(&cs, baseBinding(), current, runtime); ok {
		t.Error("EligibleCursor accepted a foreign-owned runtime dir")
	}
	if err := rl.Recover(runtime); err == nil || !strings.Contains(err.Error(), "owned by uid") {
		t.Errorf("Recover err=%v, want foreign-owner refusal", err)
	}
	if err := RuntimeDirCustody(runtime); !errors.Is(err, ErrRuntimeCustody) {
		t.Errorf("RuntimeDirCustody err=%v, want ErrRuntimeCustody", err)
	}
}

func TestRuntimeCustodyRefusesUnsafeParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(parent, "runtime")
	k := testKeys(t)
	rl := begin(t, t.TempDir(), nil)

	for name, mode := range map[string]os.FileMode{"group-writable": 0o770, "world-writable": 0o777} {
		if err := os.Chmod(parent, mode); err != nil {
			t.Fatal(err)
		}
		_, _, err := rl.WriteGeneration(runtime, k, "api", []byte("A=1\n"))
		if err == nil || !strings.Contains(err.Error(), "sticky") {
			t.Errorf("%s parent: err=%v, want sticky-bit refusal", name, err)
		}
	}
	// Sticky (like /tmp and /dev/shm) is accepted.
	if err := os.Chmod(parent, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rl.WriteGeneration(runtime, k, "api", []byte("A=1\n")); err != nil {
		t.Fatalf("sticky parent refused: %v", err)
	}
}

// An ancestor above the parent can rename the parent away and substitute its
// own, so the parent rule applies to every ancestor, also through a symlinked
// ancestor's target hierarchy.
func TestRuntimeCustodyRefusesReplaceableAncestor(t *testing.T) {
	base := t.TempDir()
	shared := filepath.Join(base, "shared")
	parent := filepath.Join(shared, "private")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(parent, alias); err != nil {
		t.Fatal(err)
	}
	k := testKeys(t)
	rl := begin(t, t.TempDir(), nil)
	for name, dir := range map[string]string{"direct": parent, "through a symlink": alias} {
		runtime := filepath.Join(dir, "runtime")
		if _, _, err := rl.WriteGeneration(runtime, k, "api", []byte("A=1\n")); !errors.Is(err, ErrRuntimeCustody) {
			t.Errorf("%s: WriteGeneration err=%v, want ErrRuntimeCustody", name, err)
		}
		if err := os.MkdirAll(runtime, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := RuntimeDirCustody(runtime); !errors.Is(err, ErrRuntimeCustody) {
			t.Errorf("%s: RuntimeDirCustody err=%v, want ErrRuntimeCustody", name, err)
		}
	}
	// The same hierarchy with a sticky shared directory is accepted.
	if err := os.Chmod(shared, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rl.WriteGeneration(filepath.Join(alias, "runtime"), k, "api", []byte("A=1\n")); err != nil {
		t.Fatalf("sticky ancestor refused: %v", err)
	}
}

// An ancestor above the parent that no other user can write is still refused
// when it is owned by neither the effective uid nor root: its owner can rename
// the hierarchy below it.
func TestRuntimeCustodyRefusesForeignOwnedAncestor(t *testing.T) {
	if os.Geteuid() == 0 {
		// Root owns the hierarchy, and a root-owned ancestor is always accepted.
		t.Skip("needs a non-root owner for the shifted effective uid to refuse")
	}
	grand := filepath.Join(t.TempDir(), "team")
	runtime := filepath.Join(grand, "private", "runtime")
	if err := os.MkdirAll(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(grand, 0o755); err != nil {
		t.Fatal(err)
	}
	// Control: owned by the effective uid, the hierarchy above the parent passes.
	if err := checkRuntimeAncestors(runtime, grand, map[string]bool{}, 0); err != nil {
		t.Fatalf("own non-writable ancestors refused: %v", err)
	}
	foreignUID(t)
	err := checkRuntimeAncestors(runtime, grand, map[string]bool{}, 0)
	if !errors.Is(err, ErrRuntimeCustody) || !strings.Contains(err.Error(), "is owned by uid") {
		t.Fatalf("foreign-owned ancestor err=%v, want an ownership refusal", err)
	}
}

// A hierarchy moved between the open and the ancestor walk leaves the walk
// vetting another directory, so the path must still name the opened root.
func TestRuntimeCustodyRefusesRuntimeDirMovedDuringCheck(t *testing.T) {
	base := t.TempDir()
	runtime := filepath.Join(base, "a", "run")
	if err := os.MkdirAll(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := openRuntimeRoot(runtime, false)
	if err != nil {
		t.Fatalf("unmoved runtime dir refused: %v", err)
	}
	_ = root.Close()
	t.Cleanup(func() { runtimeOpened = func() {} })
	runtimeOpened = func() {
		if err := os.Rename(filepath.Join(base, "a"), filepath.Join(base, "old")); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(runtime, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	root, err = openRuntimeRoot(runtime, false)
	if err == nil {
		_ = root.Close()
	}
	if !errors.Is(err, ErrRuntimeCustody) || !strings.Contains(err.Error(), "moved during the custody check") {
		t.Fatalf("runtime dir moved during the check: err=%v, want a custody refusal", err)
	}
}

// The ancestor walk vets the cleaned path, but the kernel resolves `..` after
// a symlink, so link/.. may traverse a hierarchy the walk never saw.
func TestRuntimeCustodyRefusesUncleanRuntimeDir(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "a", "run"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "a"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	unclean := filepath.Join(base, "link") + "/../a/run"
	root, err := openRuntimeRoot(unclean, false)
	if err == nil {
		_ = root.Close()
	}
	if !errors.Is(err, ErrRuntimeCustody) || !strings.Contains(err.Error(), "not a clean absolute path") {
		t.Fatalf("openRuntimeRoot(%s) err=%v, want a custody refusal", unclean, err)
	}
	for _, path := range []string{unclean, base + "/a/run/", base + "/a//run", base + "/a/./run", "../a/run"} {
		if err := RuntimeDirCustody(path); !errors.Is(err, ErrRuntimeCustody) {
			t.Fatalf("RuntimeDirCustody(%s) = %v, want a custody refusal", path, err)
		}
	}
}

func TestRuntimeCustodyMissingDirIsNotAFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	if err := RuntimeDirCustody(missing); err != nil {
		t.Fatalf("RuntimeDirCustody(missing) = %v", err)
	}
	root, entries, err := openRuntimeEntries(missing)
	if err != nil || root != nil || entries != nil {
		t.Fatalf("openRuntimeEntries(missing) = %v %v %v", root, entries, err)
	}
}

func TestRuntimeDirCustodyReportsNonDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{file, link} {
		if err := RuntimeDirCustody(p); !errors.Is(err, ErrRuntimeCustody) {
			t.Errorf("RuntimeDirCustody(%s) = %v, want ErrRuntimeCustody", p, err)
		}
	}
}

func TestWriteFileFsyncRootRefusesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	planted := filepath.Join(dir, "api.env")
	if err := os.WriteFile(planted, []byte("planted"), 0o666); err != nil {
		t.Fatal(err)
	}
	err = writeFileFsyncRoot(root, "api.env", []byte("secret"), 0o600)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("err=%v, want os.ErrExist for a pre-existing target", err)
	}
	if got, _ := os.ReadFile(planted); string(got) != "planted" {
		t.Fatalf("planted file was written through: %q", got)
	}
}

type fakeOwnedInfo struct {
	os.FileInfo
	uid  uint32
	mode os.FileMode
}

func (f fakeOwnedInfo) Sys() any          { return &syscall.Stat_t{Uid: f.uid} }
func (f fakeOwnedInfo) Mode() os.FileMode { return f.mode }

// TestCheckRuntimeCustodyOwners isolates each owner rule, which the
// directory-level tests cannot do without a second real uid.
func TestCheckRuntimeCustodyOwners(t *testing.T) {
	me := uint32(os.Geteuid())
	base, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info := func(uid uint32, mode os.FileMode) os.FileInfo { return fakeOwnedInfo{base, uid, mode} }
	for name, tc := range map[string]struct {
		dir, parent os.FileInfo
		want        string // substring of the refusal, "" = accepted
	}{
		"own dir, own parent":            {info(me, os.ModeDir|0o700), info(me, os.ModeDir|0o755), ""},
		"own dir, root parent":           {info(me, os.ModeDir|0o700), info(0, os.ModeDir|0o755), ""},
		"foreign dir":                    {info(me+1, os.ModeDir|0o700), info(me, os.ModeDir|0o755), "runtime dir /r is owned by uid"},
		"foreign parent":                 {info(me, os.ModeDir|0o700), info(me+1, os.ModeDir|0o755), "parent of runtime dir"},
		"world-writable parent":          {info(me, os.ModeDir|0o700), info(me, os.ModeDir|0o777), "sticky"},
		"group-writable parent":          {info(me, os.ModeDir|0o700), info(me, os.ModeDir|0o770), "sticky"},
		"sticky world-writable root dir": {info(me, os.ModeDir|0o700), info(0, os.ModeDir|os.ModeSticky|0o777), ""},
	} {
		err := checkRuntimeCustody("/r", tc.dir, tc.parent)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: refused: %v", name, err)
		case tc.want != "" && (!errors.Is(err, ErrRuntimeCustody) || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: err=%v, want %q", name, err, tc.want)
		}
	}
}
