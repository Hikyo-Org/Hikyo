package sshca

import (
	"crypto/mldsa"
	"errors"
	"fmt"
	"os"
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
	offerLine   = regexp.MustCompile(`Offering public key: \S+ (\S+) SHA256:`)
	signingCA   = regexp.MustCompile(`Signing CA: (\S+) SHA256:\S+ \(using (\S+)\)`)
	versionRE   = regexp.MustCompile(`OpenSSH_(\d+)\.(\d+)`)
)

// requireHybridKexDefault skips when the client predates the default hybrid
// exchange. HIKYO_SSHE2E_REQUIRED=1 turns that skip into a failure, like a
// missing binary. No workflow or script sets the variable, so no hosted lane is
// known to run these tests; they are local evidence only.
func requireHybridKexDefault(t *testing.T, sshBin string) {
	t.Helper()
	out, _ := exec.Command(sshBin, "-V").CombinedOutput()
	m := versionRE.FindStringSubmatch(string(out))
	reason := ""
	if m == nil {
		reason = fmt.Sprintf("cannot read the OpenSSH version from %q", out)
	} else if major, _ := strconv.Atoi(m[1]); major < 10 {
		reason = fmt.Sprintf("OpenSSH %s.%s predates the default mlkem768x25519-sha256 key exchange", m[1], m[2])
	}
	if reason != "" {
		if os.Getenv("HIKYO_SSHE2E_REQUIRED") == "1" {
			t.Fatalf("OpenSSH end-to-end test required but %s", reason)
		}
		t.Skip(reason)
	}
	t.Logf("client: %s", strings.TrimSpace(string(out)))
}

// TestOpenSSHRecordsKexHostAndCertificateFamilies reads three things from the
// real client and keygen, per CA algorithm: the negotiated key exchange and host
// key from the client's negotiation lines, the user key type from the line that
// offers the certificate, and the CA signature family from ssh-keygen -L. It
// asserts only on those lines: the client's own offer list (also printed by -vv)
// may name post-quantum algorithms on newer OpenSSH and says nothing about what
// was negotiated or issued.
func TestOpenSSHRecordsKexHostAndCertificateFamilies(t *testing.T) {
	cases := []struct {
		alg       Algorithm
		caFamily  string
		caSigning string
	}{
		{AlgorithmEd25519, "ED25519", "ssh-ed25519"},
		{AlgorithmECDSAP256, "ECDSA", "ecdsa-sha2-nistp256"},
		{AlgorithmRSA3072, "RSA", "rsa-sha2-512"},
	}
	for _, tc := range cases {
		t.Run(string(tc.alg), func(t *testing.T) {
			h := startSSHD(t)
			requireHybridKexDefault(t, h.ssh)
			ca := mustCA(t, tc.alg)
			h.trust(ca)
			now := time.Now()
			c := h.issue("neg", ca, 3001, now.Add(-ClockSkew), now.Add(time.Hour), nil)
			out, err := h.loginWith(c, "-vv")
			if err != nil {
				t.Fatal(err)
			}
			kex, host, offer := kexLine.FindStringSubmatch(out), hostKeyLine.FindStringSubmatch(out), offerLine.FindStringSubmatch(out)
			if kex == nil || host == nil || offer == nil {
				t.Fatalf("ssh -vv did not report the negotiation:\n%s", out)
			}
			keygen, err := exec.Command(h.keygen, "-L", "-f", c.certPath).CombinedOutput()
			if err != nil {
				t.Fatalf("ssh-keygen -L: %v: %s", err, keygen)
			}
			ca2 := signingCA.FindStringSubmatch(string(keygen))
			if ca2 == nil {
				t.Fatalf("ssh-keygen -L did not report the signing CA:\n%s", keygen)
			}
			t.Logf("kex=%s hostkey=%s userkey=%s signingCA=%s using %s", kex[1], host[1], offer[1], ca2[1], ca2[2])
			if kex[1] != "mlkem768x25519-sha256" {
				t.Fatalf("session key exchange = %s, want the OpenSSH 10 default mlkem768x25519-sha256", kex[1])
			}
			// The harness host key is Ed25519, so the host family is a property of
			// the fixture, not of Hikyo.
			if host[1] != "ssh-ed25519" || offer[1] != "ED25519-CERT" {
				t.Fatalf("host key %s, user key %s: want ssh-ed25519 and ED25519-CERT", host[1], offer[1])
			}
			// The CA signature family is what Hikyo issues and is classical.
			if ca2[1] != tc.caFamily || ca2[2] != tc.caSigning {
				t.Fatalf("signing CA %s using %s, want %s using %s", ca2[1], ca2[2], tc.caFamily, tc.caSigning)
			}
		})
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
// the vendor-named ML-DSA key golang.org/x/crypto/ssh can parse.
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
