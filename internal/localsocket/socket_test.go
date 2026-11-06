//go:build linux || darwin

package localsocket

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSameUserSocketRoundTripAndCustody(t *testing.T) {
	directory, err := os.MkdirTemp("", "hks-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "hikyo.sock")
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, want socket 0600", info.Mode())
	}

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_, err = conn.Write([]byte("ok"))
		done <- err
	}()
	conn, err := DialContext(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(conn)
	conn.Close()
	if err != nil || string(raw) != "ok" {
		t.Fatalf("read = %q, %v", raw, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSocketRefusesReplaceableAncestorsBeforeCreatingFiles(t *testing.T) {
	for _, mode := range []os.FileMode{0o777, 0o770} {
		t.Run(mode.String(), func(t *testing.T) {
			ancestor := t.TempDir()
			if err := os.Chmod(ancestor, mode); err != nil {
				t.Fatal(err)
			}
			parent := filepath.Join(ancestor, "private")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "hikyo.sock")
			if _, err := Listen(path); err == nil || !strings.Contains(err.Error(), "ancestor") {
				t.Fatalf("replaceable ancestor error = %v", err)
			}
			for _, file := range []string{path, path + ".lock"} {
				if _, err := os.Lstat(file); !os.IsNotExist(err) {
					t.Fatalf("refused startup changed %s: %v", file, err)
				}
			}
		})
	}
}

func TestSocketAllowsTrustedStickyAncestor(t *testing.T) {
	ancestor := t.TempDir()
	if err := os.Chmod(ancestor, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(ancestor, "private")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePath(filepath.Join(parent, "hikyo.sock")); err != nil {
		t.Fatal(err)
	}
}

// Kubelet leaves a volume root group-writable (fsGroup). Only the named volume
// root is exempt, never another ancestor, and its owner is still checked.
func TestVolumeRootIsTheOnlyExemptWritableAncestor(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	volume := filepath.Join(base, "volume")
	state := filepath.Join(volume, "state")
	for _, dir := range []string{volume, state} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	socket := filepath.Join(state, "hikyo.sock")
	for _, mode := range []os.FileMode{0o2775, 0o775, 0o777} {
		if err := os.Chmod(volume, mode); err != nil {
			t.Fatal(err)
		}
		if err := ValidatePath(socket); err == nil {
			t.Fatalf("mode %o volume root accepted without being named", mode)
		}
		worldWritable := mode&0o002 != 0
		if err := ValidatePathOnVolume(socket, volume); worldWritable != (err != nil) {
			t.Fatalf("mode %o volume root: %v", mode, err)
		}
		if err := ValidatePathOnWorldWritableVolume(socket, volume); err != nil {
			t.Fatalf("mode %o volume root refused despite the override: %v", mode, err)
		}
		for _, validate := range []func(string, string) error{ValidatePathOnVolume, ValidatePathOnWorldWritableVolume} {
			if err := validate(socket, filepath.Join(base, "other")); err == nil {
				t.Fatal("an unrelated volume name exempted the writable ancestor")
			}
		}
	}
	if err := os.Chmod(volume, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePathOnVolume(socket, volume); err == nil {
		t.Fatal("a writable ancestor above the volume root was exempted")
	}
}

// A world-writable volume root without the sticky bit lets another local user
// rename the state directory, so it needs the explicit override; sticky does not.
func TestWorldWritableVolumeRootNeedsTheOverride(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	volume := filepath.Join(base, "volume")
	state := filepath.Join(volume, "state")
	for _, dir := range []string{volume, state} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	socket := filepath.Join(state, "hikyo.sock")
	if err := os.Chmod(volume, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePathOnVolume(socket, volume); err == nil || !strings.Contains(err.Error(), "accept_world_writable_volume_root") || !strings.Contains(err.Error(), "chmod o-w") {
		t.Fatalf("world-writable volume root error = %v", err)
	}
	if err := os.Chmod(volume, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePathOnVolume(socket, volume); err != nil {
		t.Fatalf("sticky world-writable volume root refused: %v", err)
	}
}

func TestSocketRefusesHiddenReplaceableSymlinkHop(t *testing.T) {
	base := t.TempDir()
	safe := filepath.Join(base, "safe")
	shared := filepath.Join(base, "shared")
	parent := filepath.Join(safe, "private")
	for _, dir := range []string{safe, shared, parent} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	hop := filepath.Join(shared, "hop")
	if err := os.Symlink(safe, hop); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{hop, "../shared/hop"} {
		t.Run(target, func(t *testing.T) {
			alias := filepath.Join(safe, "alias")
			if err := os.Symlink(target, alias); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Remove(alias) })
			path := filepath.Join(alias, "private", "hikyo.sock")
			if _, err := Listen(path); err == nil || !strings.Contains(err.Error(), "ancestor") {
				t.Fatalf("hidden replaceable hop error = %v", err)
			}
			for _, file := range []string{filepath.Join(parent, "hikyo.sock"), filepath.Join(parent, "hikyo.sock.lock")} {
				if _, err := os.Lstat(file); !os.IsNotExist(err) {
					t.Fatalf("refused startup changed %s: %v", file, err)
				}
			}
		})
	}
}

func TestSocketClosePreservesReplacementPath(t *testing.T) {
	directory, err := os.MkdirTemp("", "hks-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "hikyo.sock")
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "replacement" {
		t.Fatalf("close removed replacement: %q, %v", content, err)
	}
}

func TestSocketRestartRecoversStaleSocketButPreservesLiveListener(t *testing.T) {
	directory, err := os.MkdirTemp("", "hks-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "hikyo.sock")
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate two restart attempts after the unclean exit. Exactly one may
	// reclaim the stale socket; the loser must not remove the winner's listener.
	start := make(chan struct{})
	listeners := make(chan net.Listener, 2)
	var attempts sync.WaitGroup
	for range 2 {
		attempts.Go(func() {
			<-start
			listener, err := Listen(path)
			if err == nil {
				listeners <- listener
			}
		})
	}
	close(start)
	attempts.Wait()
	close(listeners)
	var live net.Listener
	for listener := range listeners {
		defer listener.Close()
		if live != nil {
			t.Fatal("both restart attempts bound the socket")
		}
		live = listener
	}
	if live == nil {
		t.Fatal("no restart attempt recovered the stale socket")
	}
	if _, err := Listen(path); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("live socket error = %v", err)
	}
	conn, err := DialContext(t.Context(), path)
	if err != nil {
		t.Fatalf("live listener was replaced: %v", err)
	}
	_ = conn.Close()
}

func TestSocketStartupRefusesSymlinkLock(t *testing.T) {
	directory, err := os.MkdirTemp("", "hks-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "hikyo.sock")
	if err := os.Symlink(filepath.Join(directory, "elsewhere"), path+".lock"); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path); err == nil {
		t.Fatal("symlink startup lock accepted")
	}
}

func TestSocketRefusesWeakParentExistingPathAndWrongPeer(t *testing.T) {
	directory, err := os.MkdirTemp("", "hks-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "hikyo.sock")
	if err := ValidatePath(path); err == nil || !strings.Contains(err.Error(), "0700") {
		t.Fatalf("weak parent error = %v", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing path error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	clientDone := make(chan error, 1)
	go func() {
		conn, err := net.Dial("unix", path)
		if err == nil {
			defer conn.Close()
			clientDone <- requirePeerUID(conn, ^uint32(0))
			return
		}
		clientDone <- err
	}()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	if err := <-clientDone; err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong peer error = %v", err)
	}
}
