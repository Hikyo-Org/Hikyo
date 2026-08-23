package tlspolicy_test

import (
	"context"
	"os"
	"path/filepath"
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
	sourceCert, sourceKey := tlstest.WritePair(t, sourceDir, firstCert, firstKey)
	if err := os.Chmod(sourceKey, 0o440); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- tlspolicy.WatchAndStageCertificatePair(ctx, sourceCert, sourceKey, destination, 20*time.Millisecond)
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("watcher: %v", err)
		}
	})
	waitForSerial := func(want string) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			_, leaf, err := tlspolicy.LoadCertificate(filepath.Join(destination, "tls.crt"), filepath.Join(destination, "tls.key"), time.Now())
			if err == nil && leaf.SerialNumber.String() == want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("staged certificate did not reach serial %s", want)
	}
	waitForSerial(firstLeaf.SerialNumber.String())
	secondCert, secondKey, secondLeaf := tlstest.MintServerCert(t, "localhost")
	if err := os.WriteFile(sourceCert, secondCert, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceKey, secondKey, 0o440); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(sourceCert, future, future); err != nil {
		t.Fatal(err)
	}
	waitForSerial(secondLeaf.SerialNumber.String())
}
