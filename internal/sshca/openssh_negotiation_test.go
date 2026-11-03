package sshca

import (
	"crypto/mldsa"
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// Qualification of the SSH CA route with the real OpenSSH client and server
// (#1134, E-02): which key exchange protects the session and which signature
// families authenticate the host and the user. The session key exchange belongs
// to the peer's OpenSSH build and sshd_config, not to Hikyo; the certificate
// signature is the CA family Hikyo issues. These tests record both and pin the
// downgrade behavior an operator controls. They skip when OpenSSH is absent or
// older than the default hybrid key exchange (10.0).

var (
	kexLine     = regexp.MustCompile(`kex: algorithm: (\S+)`)
	hostKeyLine = regexp.MustCompile(`kex: host key algorithm: (\S+)`)
	versionRE   = regexp.MustCompile(`OpenSSH_(\d+)\.(\d+)`)
)

func requireHybridKexDefault(t *testing.T, sshBin string) string {
	t.Helper()
	out, _ := exec.Command(sshBin, "-V").CombinedOutput()
	m := versionRE.FindStringSubmatch(string(out))
	if m == nil {
		t.Skipf("cannot read the OpenSSH version from %q", out)
	}
	if major, _ := strconv.Atoi(m[1]); major < 10 {
		t.Skipf("OpenSSH %s.%s predates the default mlkem768x25519-sha256 key exchange", m[1], m[2])
	}
	t.Logf("client: %s", strings.TrimSpace(string(out)))
	return strings.TrimSpace(string(out))
}

func TestOpenSSHRecordsKexHostAndCertificateFamilies(t *testing.T) {
	h := startSSHD(t)
	requireHybridKexDefault(t, h.ssh)
	ca := mustCA(t, AlgorithmEd25519)
	h.trust(ca)
	now := time.Now()
	c := h.issue("neg", ca, 3001, now.Add(-ClockSkew), now.Add(time.Hour), nil)
	out, err := h.loginWith(c, "-vv")
	if err != nil {
		t.Fatal(err)
	}
	kex := kexLine.FindStringSubmatch(out)
	host := hostKeyLine.FindStringSubmatch(out)
	if kex == nil || host == nil {
		t.Fatalf("ssh -vv did not report the negotiation:\n%s", out)
	}
	t.Logf("negotiated kex=%s hostkey=%s ca=%s", kex[1], host[1], ca.PublicKey().Type())
	if kex[1] != "mlkem768x25519-sha256" {
		t.Fatalf("session key exchange = %s, want the OpenSSH 10 default mlkem768x25519-sha256", kex[1])
	}
	// The host key and the CA/user signatures stay classical: no PQ signature
	// is negotiated or issued by this route.
	if host[1] != "ssh-ed25519" || ca.PublicKey().Type() != "ssh-ed25519" || !strings.Contains(out, "ED25519-CERT") {
		t.Fatalf("unexpected signature families: hostkey=%s ca=%s\n%s", host[1], ca.PublicKey().Type(), out)
	}
	if strings.Contains(strings.ToLower(out), "mldsa") {
		t.Fatal("an ML-DSA algorithm appeared in a Hikyo-issued route")
	}
}

func TestOpenSSHDowngradeBehaviorIsTheServersKexAlgorithms(t *testing.T) {
	now := time.Now()
	classical := []string{"-o", "KexAlgorithms=curve25519-sha256"}

	// Default sshd: a client limited to a classical exchange still logs in. The
	// route has no downgrade protection unless the operator configures it.
	h := startSSHD(t)
	requireHybridKexDefault(t, h.ssh)
	ca := mustCA(t, AlgorithmEd25519)
	h.trust(ca)
	c := h.issue("compat", ca, 3002, now.Add(-ClockSkew), now.Add(time.Hour), nil)
	out, err := h.loginWith(c, append([]string{"-vv"}, classical...)...)
	if err != nil {
		t.Fatalf("default sshd refused a classical-only client: %v", err)
	}
	if got := kexLine.FindStringSubmatch(out); got == nil || got[1] != "curve25519-sha256" {
		t.Fatalf("classical client negotiated %v", got)
	}

	// Operator control: sshd restricted to the hybrid exchange refuses the same
	// client before authentication, and still serves the default client.
	strict := startSSHD(t, "KexAlgorithms mlkem768x25519-sha256")
	strict.trust(ca)
	c = strict.issue("strict", ca, 3003, now.Add(-ClockSkew), now.Add(time.Hour), nil)
	out, err = strict.loginWith(c, classical...)
	if err == nil {
		t.Fatal("a hybrid-only sshd served a classical-only client")
	}
	if !strings.Contains(out, "no matching key exchange method") || strings.Contains(out, "hikyo-ok") {
		t.Fatalf("refusal was not a key exchange failure: %s", out)
	}
	if err := strict.login(c); err != nil {
		t.Fatalf("hybrid-only sshd refused the default client: %v", err)
	}
}

// E-02: Hikyo issues no ML-DSA SSH key or certificate. The closed set refuses
// the vendor-named ML-DSA algorithms that golang.org/x/crypto/ssh can parse.
func TestParseAlgorithmRefusesMLDSA(t *testing.T) {
	for _, name := range []string{"mldsa65", "ssh-mldsa-65", "ssh-mldsa-65-cert-v01@golang.org", "ssh-mldsa44-ed25519"} {
		if _, err := ParseAlgorithm(name); err == nil {
			t.Fatalf("ParseAlgorithm(%q) accepted a post-quantum signature name", name)
		}
	}
}

func TestParsePublicKeyRefusesAnMLDSAKey(t *testing.T) {
	priv, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(priv.PublicKey())
	if err != nil {
		t.Skipf("x/crypto/ssh cannot wrap an ML-DSA key: %v", err)
	}
	if _, _, err := ParsePublicKey(AuthorizedKey(pub, "")); !errors.Is(err, ErrUnsupportedKey) {
		t.Fatalf("ParsePublicKey(%s) error = %v, want ErrUnsupportedKey", pub.Type(), err)
	}
}
