package tlspolicy_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

func TestStageCertificatePairProducesOwnerReadableValidatedFiles(t *testing.T) {
	certPEM, keyPEM, _ := tlstest.MintServerCert(t, "localhost")
	sourceCert, sourceKey := tlstest.WritePair(t, t.TempDir(), certPEM, keyPEM)
	if err := os.Chmod(sourceKey, 0o440); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := tlspolicy.StageCertificatePair(sourceCert, sourceKey, destination); err != nil {
		t.Fatal(err)
	}
	keyInfo, err := os.Stat(filepath.Join(destination, "tls.key"))
	if err != nil {
		t.Fatal(err)
	}
	if keyInfo.Mode().Perm() != 0o400 {
		t.Fatalf("staged key mode = %04o, want 0400", keyInfo.Mode().Perm())
	}
	if _, _, err := tlspolicy.LoadCertificate(filepath.Join(destination, "tls.crt"), filepath.Join(destination, "tls.key"), keyInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
}

func TestWatchAndStageCertificatePairPropagatesRenewal(t *testing.T) {
	sourceDir := t.TempDir()
	destination := t.TempDir()
	firstCert, firstKey, firstLeaf := tlstest.MintServerCert(t, "localhost")
	firstVersion := filepath.Join(sourceDir, "..first")
	if err := os.Mkdir(firstVersion, 0o700); err != nil {
		t.Fatal(err)
	}
	_, firstVersionKey := tlstest.WritePair(t, firstVersion, firstCert, firstKey)
	if err := os.Chmod(firstVersionKey, 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(firstVersion), filepath.Join(sourceDir, "..data")); err != nil {
		t.Fatal(err)
	}
	sourceCert := filepath.Join(sourceDir, "tls.crt")
	sourceKey := filepath.Join(sourceDir, "tls.key")
	if err := os.Symlink(filepath.Join("..data", "tls.crt"), sourceCert); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..data", "tls.key"), sourceKey); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	var mu sync.Mutex
	var watchErr error
	go func() {
		err := tlspolicy.WatchAndStageCertificatePair(ctx, sourceCert, sourceKey, destination, 20*time.Millisecond)
		mu.Lock()
		watchErr = err
		mu.Unlock()
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("watcher: %v", err)
		}
	})
	waitForSerial := func(want string) {
		t.Helper()
		mu.Lock()
		err := watchErr
		mu.Unlock()
		if err != nil {
			t.Fatalf("watcher exited before staging serial %s: %v", want, err)
		}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			_, leaf, err := tlspolicy.LoadCertificate(filepath.Join(destination, "tls.crt"), filepath.Join(destination, "tls.key"), time.Now())
			if err == nil && leaf.SerialNumber.String() == want {
				return
			}
			mu.Lock()
			watchFailed := watchErr
			mu.Unlock()
			if watchFailed != nil {
				t.Fatalf("watcher exited before staging serial %s: %v", want, watchFailed)
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("staged certificate did not reach serial %s within deadline", want)
	}
	waitForSerial(firstLeaf.SerialNumber.String())
	secondCert, secondKey, secondLeaf := tlstest.MintServerCert(t, "localhost")
	secondVersion := filepath.Join(sourceDir, "..second")
	if err := os.Mkdir(secondVersion, 0o700); err != nil {
		t.Fatal(err)
	}
	secondVersionCert, secondVersionKey := tlstest.WritePair(t, secondVersion, secondCert, secondKey)
	if err := os.Chmod(secondVersionKey, 0o440); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(secondVersionCert, future, future); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(secondVersionKey, future, future); err != nil {
		t.Fatal(err)
	}
	nextData := filepath.Join(sourceDir, "..data-next")
	if err := os.Symlink(filepath.Base(secondVersion), nextData); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(nextData, filepath.Join(sourceDir, "..data")); err != nil {
		t.Fatal(err)
	}
	waitForSerial(secondLeaf.SerialNumber.String())
}
