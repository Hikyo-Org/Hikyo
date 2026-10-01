package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPinnedClientReplacesPKIVerificationWithExactLeafIdentity(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	pin, err := FetchIdentity(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if want := SPKIFingerprint(server.Certificate()); pin != want {
		t.Fatalf("fetched pin = %q, want %q", pin, want)
	}
	if err := validateCredentialTransport(TrustEntry{Name: "local-tls", Origin: server.URL, SPKIPin: pin}); err != nil {
		t.Fatalf("pinned loopback HTTPS credential transport refused: %v", err)
	}

	client, err := NewClient(TrustEntry{Name: "test", Origin: server.URL, SPKIPin: pin}, "")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.HTTP.Get(server.URL)
	if err != nil {
		t.Fatalf("exact pinned identity refused: %v", err)
	}
	response.Body.Close()

	client, err = NewClient(TrustEntry{Name: "test", Origin: server.URL, SPKIPin: "wrong-pin"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if response, err = client.HTTP.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("different leaf identity passed the recorded SPKI pin")
	}
}

func TestFetchIdentityBoundsStalledTLSHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			defer conn.Close()
			<-time.After(time.Second)
		}
	}()
	started := time.Now()
	_, err = fetchIdentity(t.Context(), "https://"+listener.Addr().String(), 50*time.Millisecond)
	if err == nil {
		t.Fatal("stalled TLS handshake succeeded")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("stalled TLS handshake took %s, want bounded refusal", elapsed)
	}
}

func TestNewStateRejectsRelativeSecurityBoundary(t *testing.T) {
	_, err := NewState(Env{Getenv: func(name string) string {
		if name == "HIKYO_STATE_DIR" {
			return ".hikyo-state"
		}
		return ""
	}})
	if err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("NewState error = %v, want absolute-path refusal", err)
	}
}

func TestTrustStoreRejectsUncontrolledFilesystemObjects(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix ownership and mode contract")
	}
	valid := map[string]TrustEntry{
		"local": {Name: "local", Origin: "http://127.0.0.1:8080"},
	}
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("symlink file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "attacker.json")
		if err := os.WriteFile(target, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, "trust.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := (&TrustStore{dir: dir}).Load(); err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("Load error = %v, want symlink refusal", err)
		}
	})

	t.Run("shared directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "trust.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := (&TrustStore{dir: dir}).Load(); err == nil || !strings.Contains(err.Error(), "0700") {
			t.Fatalf("Load error = %v, want directory-mode refusal", err)
		}
	})

	t.Run("shared file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "trust.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := (&TrustStore{dir: dir}).Load(); err == nil || !strings.Contains(err.Error(), "0600") {
			t.Fatalf("Load error = %v, want file-mode refusal", err)
		}
	})
}

func TestTrustStoreValidatesLoadedEntry(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	raw := []byte(`{"local":{"name":"local","origin":"https://EXAMPLE.com/","spki_pin":"not-a-pin"}}`)
	if err := os.WriteFile(filepath.Join(dir, "trust.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&TrustStore{dir: dir}).Load(); err == nil || !strings.Contains(err.Error(), "invalid entry") {
		t.Fatalf("Load error = %v, want invalid-entry refusal", err)
	}
}

func TestTrustStoreSerializesConcurrentMutations(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	store := &TrustStore{dir: dir}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("local-%d", i)
			errs <- store.Put(TrustEntry{Name: name, Origin: fmt.Sprintf("http://127.0.0.1:%d", 8000+i)})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 16 {
		t.Fatalf("concurrent trust entries = %d, want 16", len(entries))
	}
}

func TestStateSerializesConcurrentSessionAndContextMutations(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	state := &State{dir: dir}
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := range 12 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("instance-%d", i)
			errs <- state.PutSession(SessionArtifact{Instance: name, Origin: "https://example.test", Token: "token"})
		}()
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("context-%d", i)
			errs <- state.PutContext(Context{Name: name, Instance: "local"})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	sessions, err := state.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	contexts, err := state.Contexts()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 12 || len(contexts) != 12 {
		t.Fatalf("concurrent state = %d sessions, %d contexts; want 12 each", len(sessions), len(contexts))
	}
}
