//go:build linux || darwin

package devupgrade

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

func parentDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func inventory(t *testing.T, dir string) map[string]releaseidentity.Digest {
	t.Helper()
	files := map[string]releaseidentity.Digest{}
	err := filepath.WalkDir(dir, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		want := os.FileMode(0600)
		if entry.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			t.Errorf("unexpected permissions %s %v", name, info.Mode())
		}
		if !entry.IsDir() {
			raw, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(dir, name)
			if err != nil {
				return err
			}
			files[relative] = releaseidentity.Hash(raw)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestFreshAndRestartUseActualSignedBundle(t *testing.T) {
	parent := parentDir(t)
	first, err := Open(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	before := inventory(t, filepath.Join(parent, custodyName))
	bundle, err := upgradebundle.Load(context.Background(), first.Directory, first.Pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	node, err := bundle.MatchBuild(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := buildcompat.VerifyDevelopment(node); err != nil {
		t.Fatal(err)
	}
	source := node.GenesisSources(releaseidentity.SQLite)[0]
	plan, err := bundle.Plan(source, node.Identity())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps()) != 1 {
		t.Fatal("fresh development plan did not use ordinary gate")
	}
	second, err := Open(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	if first.Directory != second.Directory || string(first.Pinned.Root) != string(second.Pinned.Root) {
		t.Fatal("custody changed on restart")
	}
	after := inventory(t, filepath.Join(parent, custodyName))
	if len(before) != len(after) {
		t.Fatal("restart changed inventory")
	}
	for name, digest := range before {
		if after[name] != digest {
			t.Fatal("restart changed saved evidence", name)
		}
	}
	other, err := Open(context.Background(), parentDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(other.Pinned.Root) == string(first.Pinned.Root) {
		t.Fatal("installations reused custody")
	}
}

func TestMalformedCustodyRefusesWithoutRepair(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"missing key": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "primary.key")); err != nil {
				t.Fatal(err)
			}
		},
		"wrong key": func(t *testing.T, root string) {
			raw, err := newPrivate()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "primary.key"), raw, 0600); err != nil {
				t.Fatal(err)
			}
		},
		"signature": func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "bundle/catalog.sigstore.json"), []byte(`{"base64Signature":"AAAA"}`), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"declaration": func(t *testing.T, root string) {
			matches, err := filepath.Glob(filepath.Join(root, "bundle/releases/*/upgrade-compatibility.json"))
			if err != nil || len(matches) != 1 {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(matches[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(matches[0], append(raw, ' '), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"unknown file": func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "unknown"), []byte("unrecognized"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"unknown empty directory": func(t *testing.T, root string) {
			if err := os.Mkdir(filepath.Join(root, "unknown"), 0700); err != nil {
				t.Fatal(err)
			}
		},
		"permissions": func(t *testing.T, root string) {
			if err := os.Chmod(filepath.Join(root, "primary.key"), 0644); err != nil {
				t.Fatal(err)
			}
		},
		"hardlink": func(t *testing.T, root string) {
			if err := os.Link(filepath.Join(root, "primary.key"), filepath.Join(filepath.Dir(root), "extra-key")); err != nil {
				t.Fatal(err)
			}
		},
		"symlink file": func(t *testing.T, root string) {
			name := filepath.Join(root, "primary.key")
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("recovery.key", name); err != nil {
				t.Fatal(err)
			}
		},
		"symlink directory": func(t *testing.T, root string) {
			if err := os.Rename(filepath.Join(root, "bundle"), filepath.Join(root, "saved")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("saved", filepath.Join(root, "bundle")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, damage := range cases {
		t.Run(name, func(t *testing.T) {
			parent := parentDir(t)
			if _, err := Open(context.Background(), parent); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(parent, custodyName)
			damage(t, root)
			saved, err := os.ReadFile(filepath.Join(root, "root.json"))
			if err != nil {
				t.Fatal(err)
			}
			material, err := Open(context.Background(), parent)
			if err == nil || material.Directory != "" {
				t.Fatal("malformed custody accepted")
			}
			after, err := os.ReadFile(filepath.Join(root, "root.json"))
			if err != nil || string(saved) != string(after) {
				t.Fatal("malformed custody replaced")
			}
		})
	}
}

func TestConcurrentCreationConverges(t *testing.T) {
	parent := parentDir(t)
	const count = 12
	var group sync.WaitGroup
	results := make([]Material, count)
	errs := make([]error, count)
	for i := range count {
		group.Go(func() { results[i], errs[i] = Open(context.Background(), parent) })
	}
	group.Wait()
	for i := range count {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if string(results[i].Pinned.Root) != string(results[0].Pinned.Root) {
			t.Fatal("concurrent creation diverged")
		}
	}
	inventory(t, filepath.Join(parent, custodyName))
}

func TestFailureCleanupAndPublishedRecovery(t *testing.T) {
	t.Run("before publication", func(t *testing.T) {
		parent := parentDir(t)
		injected := errors.New("injected data sync failure")
		result, err := open(context.Background(), parent, func(f *os.File) error {
			if strings.HasSuffix(f.Name(), ".key") {
				return injected
			}
			return f.Sync()
		})
		if !errors.Is(err, injected) || result.Directory != "" {
			t.Fatal("missing sync failure", err)
		}
		entries, err := os.ReadDir(parent)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != ".development-upgrade.lock" {
			t.Fatal("failed private staging was not cleaned")
		}
		if _, err := Open(context.Background(), parent); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("after publication", func(t *testing.T) {
		parent := parentDir(t)
		injected := errors.New("injected parent sync failure")
		result, err := open(context.Background(), parent, func(f *os.File) error {
			if f.Name() == filepath.Base(parent) {
				return injected
			}
			return f.Sync()
		})
		if !errors.Is(err, ErrDurabilityUnconfirmed) || result.Directory != "" {
			t.Fatal("published failure returned authority", err)
		}
		before := inventory(t, filepath.Join(parent, custodyName))
		if _, err := Open(context.Background(), parent); err != nil {
			t.Fatal(err)
		}
		after := inventory(t, filepath.Join(parent, custodyName))
		for name, digest := range before {
			if after[name] != digest {
				t.Fatal("recovery replaced evidence")
			}
		}
	})
}

func TestRefuseUnknownExistingPathsAndCancellation(t *testing.T) {
	t.Run("empty existing child", func(t *testing.T) {
		parent := parentDir(t)
		if err := os.Mkdir(filepath.Join(parent, custodyName), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), parent); err == nil {
			t.Fatal("adopted unknown existing directory")
		}
	})
	t.Run("crashed staging", func(t *testing.T) {
		parent := parentDir(t)
		if err := os.Mkdir(filepath.Join(parent, ".development-upgrade-stage-old"), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), parent); err == nil {
			t.Fatal("replaced interrupted custody")
		}
	})
	t.Run("symlink parent", func(t *testing.T) {
		parent := parentDir(t)
		link := filepath.Join(parentDir(t), "alias")
		if err := os.Symlink(parent, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), link); err == nil {
			t.Fatal("followed symlink parent")
		}
	})
	t.Run("loose parent", func(t *testing.T) {
		parent := parentDir(t)
		if err := os.Chmod(parent, 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), parent); err == nil {
			t.Fatal("accepted loose parent")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		parent := parentDir(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := Open(ctx, parent); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(parent)
		if err != nil || len(entries) != 0 {
			t.Fatal("cancelled operation mutated parent")
		}
	})
}

func TestConcurrentProcessesUseOneCustody(t *testing.T) {
	if parent := os.Getenv("HIKYO_DEVUPGRADE_TEST_CHILD"); parent != "" {
		if _, err := Open(context.Background(), parent); err != nil {
			t.Fatal(err)
		}
		return
	}
	parent := parentDir(t)
	commands := make([]*exec.Cmd, 4)
	for i := range commands {
		cmd := exec.Command(os.Args[0], "-test.run=^TestConcurrentProcessesUseOneCustody$")
		cmd.Env = append(os.Environ(), "HIKYO_DEVUPGRADE_TEST_CHILD="+parent)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands[i] = cmd
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Open(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	inventory(t, filepath.Join(parent, custodyName))
}

func TestLockRefusalAndCancellation(t *testing.T) {
	t.Run("nonempty lock", func(t *testing.T) {
		parent := parentDir(t)
		if err := os.WriteFile(filepath.Join(parent, ".development-upgrade.lock"), []byte("unknown"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), parent); err == nil {
			t.Fatal("adopted malformed lock")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		parent := parentDir(t)
		target := filepath.Join(parent, "unrelated")
		if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(parent, ".development-upgrade.lock")); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), parent); err == nil {
			t.Fatal("followed lock symlink")
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != "preserve" {
			t.Fatal("modified unrelated file")
		}
	})
	t.Run("cancel while locked", func(t *testing.T) {
		parent := parentDir(t)
		lock, err := os.OpenFile(filepath.Join(parent, ".development-upgrade.lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
			t.Fatal(err)
		}
		defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := Open(ctx, parent); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(parent, custodyName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("waiting process created custody")
		}
	})
}

func TestReviewCleanupPreservesReplacedStagingDirectory(t *testing.T) {
	parent := parentDir(t)
	replacement := ""
	injected := errors.New("forced sync failure after staging rename")
	_, err := open(t.Context(), parent, func(file *os.File) error {
		if replacement != "" {
			return injected
		}
		entries, err := os.ReadDir(parent)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".development-upgrade-stage-") {
				continue
			}
			original := filepath.Join(parent, entry.Name())
			if err := os.Rename(original, original+"-retained"); err != nil {
				return err
			}
			if err := os.Mkdir(original, 0700); err != nil {
				return err
			}
			replacement = filepath.Join(original, "primary.key")
			if err := os.WriteFile(replacement, []byte("unrelated replacement must survive"), 0600); err != nil {
				return err
			}
			return injected
		}
		return errors.New("no active staging directory")
	})
	if !errors.Is(err, injected) {
		t.Fatal("injection did not execute", err)
	}
	if _, err := os.Stat(replacement); err != nil {
		t.Fatalf("failure cleanup removed replacement inode: %v", err)
	}
}
