package tlspolicy_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

func TestLoadCertificateRejectsMismatchedPairAndUnsafeKeyMode(t *testing.T) {
	certPEM, keyPEM, _ := tlstest.MintServerCert(t, "localhost")
	_, otherKey, _ := tlstest.MintServerCert(t, "localhost")
	certPath, keyPath := tlstest.WritePair(t, t.TempDir(), certPEM, otherKey)
	if _, _, err := tlspolicy.LoadCertificate(certPath, keyPath, time.Now()); err == nil {
		t.Fatal("mismatched certificate and key must refuse")
	}

	certPath, keyPath = tlstest.WritePair(t, t.TempDir(), certPEM, keyPEM)
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tlspolicy.LoadCertificate(certPath, keyPath, time.Now()); err == nil || !strings.Contains(err.Error(), "0400 or 0600") {
		t.Fatalf("unsafe key mode: err = %v", err)
	}
}

func TestLoadCertificateRejectsExpiredAndFutureDatedLeaf(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name      string
		notBefore time.Time
		notAfter  time.Time
		want      string
	}{
		{name: "expired", notBefore: now.Add(-48 * time.Hour), notAfter: now.Add(-time.Hour), want: "expired"},
		{name: "future", notBefore: now.Add(time.Hour), notAfter: now.Add(90 * 24 * time.Hour), want: "not valid before"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			certPEM, keyPEM, _ := tlstest.MintServerCertWithValidity(t, tc.notBefore, tc.notAfter, "localhost")
			certPath, keyPath := tlstest.WritePair(t, t.TempDir(), certPEM, keyPEM)
			if _, _, err := tlspolicy.LoadCertificate(certPath, keyPath, now); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadCertificate error = %v, want %q", err, tc.want)
			}
		})
	}
}
