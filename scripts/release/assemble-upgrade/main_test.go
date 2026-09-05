//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"golang.org/x/sys/unix"
)

type assemblyFixture struct {
	opts    options
	trust   *testfixture.Fixture
	release testfixture.SignedRelease
}

func put(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T) assemblyFixture {
	t.Helper()
	dir := t.TempDir()
	o := options{root: filepath.Join(dir, "root.json"), recovery: filepath.Join(dir, "recovery.pub"), snapshot: filepath.Join(dir, "snapshot"), keys: filepath.Join(dir, "keys"), output: filepath.Join(dir, "bundle"), releases: directories{filepath.Join(dir, "release")}}
	f := testfixture.New(t)
	_, declaration, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	declaration.Version, declaration.Commit = "1.0.0", strings.Repeat("a", 40)
	release := f.AddStable(t, declaration.Version, 1, declaration.Commit, testfixture.JSON(t, declaration))
	put(t, o.root, f.Pinned.Root)
	put(t, o.recovery, f.Pinned.RecoveryPublicKey)
	put(t, filepath.Join(o.keys, "primary.pub"), f.PrimaryPublic)
	for name, raw := range map[string][]byte{"release-manifest.json": release.Material.Manifest, "release-manifest.sigstore.json": release.Material.ManifestSignature, "release-candidate.json": release.Material.Candidate, "upgrade-compatibility.json": release.Material.Compatibility} {
		put(t, filepath.Join(o.releases[0], name), raw)
	}
	fixture := assemblyFixture{o, f, release}
	fixture.snapshot(t)
	return fixture
}

func (f assemblyFixture) snapshot(t *testing.T) {
	t.Helper()
	m := f.trust.Material(t)
	for name, raw := range map[string][]byte{"metadata.json": m.Metadata, "metadata.sigstore.json": m.MetadataSignature, "catalog.json": m.Catalog, "catalog.sigstore.json": m.CatalogSignature} {
		put(t, filepath.Join(f.opts.snapshot, name), raw)
	}
}

func TestAssemblyAuthenticatesAndPreservesExactPublicBytes(t *testing.T) {
	f := newFixture(t)
	o := f.opts
	args := []string{"--root", o.root, "--recovery-key", o.recovery, "--snapshot", o.snapshot, "--keys", o.keys, "--release", o.releases[0], "--out", o.output}
	if err := run(context.Background(), args, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := upgradebundle.Load(context.Background(), o.output, f.trust.Pinned, releaseidentity.SnapshotFloor{}); err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string][]byte{"manifest.json": f.release.Material.Manifest, "manifest.sigstore.json": f.release.Material.ManifestSignature, "release-candidate.json": f.release.Material.Candidate, "upgrade-compatibility.json": f.release.Material.Compatibility} {
		got, err := os.ReadFile(filepath.Join(o.output, "releases", string(f.release.Identity.ManifestSHA256), name))
		if err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("exact bytes changed for %s: %v", name, err)
		}
	}
	var count int
	if err := filepath.WalkDir(o.output, func(_ string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			count++
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if count != 10 {
		t.Fatalf("unexpected public inventory: %d files", count)
	}
}

func TestAssemblyRefusesUntrustedIncompleteAndUnsafeInputs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, *assemblyFixture)
	}{
		{"wrong recovery", func(t *testing.T, f *assemblyFixture) {
			put(t, f.opts.recovery, testfixture.New(t).Pinned.RecoveryPublicKey)
		}},
		{"changed catalog", func(t *testing.T, f *assemblyFixture) {
			put(t, filepath.Join(f.opts.snapshot, "catalog.json"), []byte(`{}`))
		}},
		{"changed manifest signature", func(t *testing.T, f *assemblyFixture) {
			put(t, filepath.Join(f.opts.releases[0], "release-manifest.sigstore.json"), []byte(`{}`))
		}},
		{"changed compatibility", func(t *testing.T, f *assemblyFixture) {
			put(t, filepath.Join(f.opts.releases[0], "upgrade-compatibility.json"), []byte(`{}`))
		}},
		{"missing candidate", func(t *testing.T, f *assemblyFixture) {
			if err := os.Remove(filepath.Join(f.opts.releases[0], "release-candidate.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"extra private member", func(t *testing.T, f *assemblyFixture) {
			put(t, filepath.Join(f.opts.releases[0], "operator.key"), []byte("not accepted"))
		}},
		{"unexpected snapshot member", func(t *testing.T, f *assemblyFixture) {
			put(t, filepath.Join(f.opts.snapshot, "index.json"), []byte(`{}`))
		}},
		{"duplicate release", func(_ *testing.T, f *assemblyFixture) { f.opts.releases = append(f.opts.releases, f.opts.releases[0]) }},
		{"symlink member", func(t *testing.T, f *assemblyFixture) {
			p := filepath.Join(f.opts.releases[0], "release-candidate.json")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(f.opts.root, p); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink directory", func(t *testing.T, f *assemblyFixture) {
			p := f.opts.keys + "-link"
			if err := os.Symlink(f.opts.keys, p); err != nil {
				t.Fatal(err)
			}
			f.opts.keys = p
		}},
		{"symlink directory trailing slash", func(t *testing.T, f *assemblyFixture) {
			p := f.opts.keys + "-link"
			if err := os.Symlink(f.opts.keys, p); err != nil {
				t.Fatal(err)
			}
			f.opts.keys = p + string(filepath.Separator)
		}},
		{"fifo member", func(t *testing.T, f *assemblyFixture) {
			p := filepath.Join(f.opts.releases[0], "release-candidate.json")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := unix.Mkfifo(p, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"escaping signed locator", func(t *testing.T, f *assemblyFixture) {
			f.trust.Metadata.PrimaryKeys[0].PublicKey = "../primary.pub"
			f.snapshot(t)
		}},
		{"omitted authorized bridge", func(t *testing.T, f *assemblyFixture) {
			f.trust.Catalog.Bridges = append(f.trust.Catalog.Bridges, releaseidentity.Hash([]byte("missing bridge")))
			f.snapshot(t)
		}},
		{"unlisted bridge", func(t *testing.T, f *assemblyFixture) {
			d := filepath.Join(filepath.Dir(f.opts.output), "bridge")
			raw := []byte(`{}`)
			put(t, filepath.Join(d, "statement.json"), raw)
			put(t, filepath.Join(d, "statement.sigstore.json"), testfixture.Sign(t, f.trust.RecoverySigner, raw))
			f.opts.bridges = directories{d}
		}},
		{"rollback floor", func(t *testing.T, f *assemblyFixture) {
			f.opts.floor = filepath.Join(filepath.Dir(f.opts.output), "floor.json")
			put(t, f.opts.floor, testfixture.JSON(t, releaseidentity.SnapshotFloor{MetadataSequence: 2, MetadataSHA256: releaseidentity.Hash([]byte("later metadata")), CatalogSequence: 2, CatalogSHA256: releaseidentity.Hash([]byte("later catalog")), HighestReleaseSequence: 2}))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.mutate(t, &f)
			if err := assemble(context.Background(), f.opts); err == nil {
				t.Fatal("unsafe input accepted")
			}
			if _, err := os.Lstat(f.opts.output); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("output published on refusal: %v", err)
			}
			stages, err := filepath.Glob(filepath.Join(filepath.Dir(f.opts.output), ".hikyo-upgrade-assembly-*"))
			if err != nil || len(stages) != 0 {
				t.Fatalf("staging left behind: %v %v", stages, err)
			}
		})
	}
}

func TestConcurrentAssemblyNeverReplacesAnOutput(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- assemble(context.Background(), f.opts) })
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("wanted one publisher, got %d", success)
	}
	if _, err := upgradebundle.Load(context.Background(), f.opts.output, f.trust.Pinned, releaseidentity.SnapshotFloor{}); err != nil {
		t.Fatal(err)
	}
	// An empty existing directory must not be overwritten either, even when it
	// appears between the initial absence check and the final atomic rename.
	stage, output := t.TempDir(), t.TempDir()
	put(t, filepath.Join(stage, "sentinel"), []byte("preserve"))
	if err := publishDirectory(stage, output); err == nil {
		t.Fatal("replaced existing directory")
	}
	if _, err := os.Stat(filepath.Join(stage, "sentinel")); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledAssemblyDoesNotPublish(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := assemble(ctx, f.opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation: %v", err)
	}
	if _, err := os.Lstat(f.opts.output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published canceled assembly")
	}
}
