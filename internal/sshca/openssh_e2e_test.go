package sshca

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// The real-OpenSSH interop test: a Hikyo-signed certificate and KRL must mean
// to sshd exactly what this package claims. It runs sshd on a loopback port
// and authenticates with the real ssh client. It skips when the OpenSSH
// binaries are absent, unless HIKYO_SSHE2E_REQUIRED=1 makes absence a failure.

type opensshHarness struct {
	t       *testing.T
	dir     string
	port    int
	user    string
	ssh     string
	keygen  string
	trusted string
	krl     string
}

func requireOpenSSH(t *testing.T) (sshd, sshBin, keygen string) {
	t.Helper()
	var missing []string
	find := func(name string) string {
		p, err := exec.LookPath(name)
		if err != nil {
			for _, dir := range []string{"/usr/sbin", "/usr/local/sbin", "/sbin"} {
				if _, statErr := os.Stat(filepath.Join(dir, name)); statErr == nil {
					return filepath.Join(dir, name)
				}
			}
			missing = append(missing, name)
			return ""
		}
		abs, _ := filepath.Abs(p)
		return abs
	}
	sshd, sshBin, keygen = find("sshd"), find("ssh"), find("ssh-keygen")
	if len(missing) > 0 {
		if os.Getenv("HIKYO_SSHE2E_REQUIRED") == "1" {
			t.Fatalf("OpenSSH end-to-end test required but %v not installed", missing)
		}
		t.Skipf("OpenSSH binaries %v not installed", missing)
	}
	return sshd, sshBin, keygen
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startSSHD(t *testing.T) *opensshHarness {
	t.Helper()
	sshd, sshBin, keygen := requireOpenSSH(t)
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	h := &opensshHarness{t: t, dir: dir, port: freePort(t), user: u.Username, ssh: sshBin, keygen: keygen,
		trusted: filepath.Join(dir, "trusted_user_ca_keys"), krl: filepath.Join(dir, "revoked_keys")}
	hostKey, err := GenerateKey(AlgorithmEd25519)
	if err != nil {
		t.Fatal(err)
	}
	hostPEM, err := MarshalUserPrivateKey(hostKey, "host")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "host_key"), hostPEM)
	writeFile(t, h.trusted, "")
	h.writeKRL()
	if os.Geteuid() == 0 {
		// Privilege separation needs its empty chroot directory when root.
		_ = os.MkdirAll("/run/sshd", 0o755)
	}
	config := fmt.Sprintf(`Port %d
ListenAddress 127.0.0.1
HostKey %s
PidFile %s
AuthorizedKeysFile none
TrustedUserCAKeys %s
RevokedKeys %s
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin yes
UsePAM no
StrictModes no
LogLevel VERBOSE
`, h.port, filepath.Join(dir, "host_key"), filepath.Join(dir, "sshd.pid"), h.trusted, h.krl)
	// OpenSSH 9.8+ penalises a source after a failed authentication and drops
	// its next connections; the refusal checks here fail on purpose, so exempt
	// loopback where the option exists (older sshd rejects it as unknown).
	writeFile(t, filepath.Join(dir, "sshd_config"), config+"PerSourcePenalties no\n")
	if err := exec.Command(sshd, "-t", "-f", filepath.Join(dir, "sshd_config")).Run(); err != nil {
		writeFile(t, filepath.Join(dir, "sshd_config"), config)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var logs bytes.Buffer
	cmd := exec.CommandContext(ctx, sshd, "-D", "-e", "-f", filepath.Join(dir, "sshd_config"))
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start sshd: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("sshd log:\n%s", logs.String())
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", h.port), 200*time.Millisecond)
		if err == nil {
			c.Close()
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("sshd did not listen: %v\n%s", err, logs.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (h *opensshHarness) trust(cas ...ssh.Signer) {
	var lines []string
	for _, ca := range cas {
		lines = append(lines, AuthorizedKey(ca.PublicKey(), "hikyo-ca"))
	}
	writeFile(h.t, h.trusted, strings.Join(lines, "\n")+"\n")
}

func (h *opensshHarness) writeKRL(sections ...KRLSection) {
	out, err := EncodeKRL(KRL{Version: uint64(time.Now().UnixNano()), GeneratedAt: time.Now(), Comment: "hikyo-e2e", Sections: sections})
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(h.krl, out, 0o600); err != nil {
		h.t.Fatal(err)
	}
}

type issuedCert struct {
	keyPath, certPath string
	cert              *ssh.Certificate
}

func (h *opensshHarness) issue(name string, ca ssh.Signer, serial uint64, validAfter, validBefore time.Time, sources []string) issuedCert {
	h.t.Helper()
	key, err := GenerateKey(AlgorithmEd25519)
	if err != nil {
		h.t.Fatal(err)
	}
	pub, _ := PublicKeyOf(key)
	prefixes, err := ParseSourceAddresses(sources)
	if err != nil {
		h.t.Fatal(err)
	}
	cert, err := Sign(ca, pub, Template{
		Serial: serial, KeyID: "hikyo:e2e:" + name,
		Resolved:   Resolved{Principals: []string{h.user}, SourceAddresses: prefixes, Extensions: []Extension{ExtPTY}},
		ValidAfter: validAfter, ValidBefore: validBefore,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	privText, err := MarshalUserPrivateKey(key, name)
	if err != nil {
		h.t.Fatal(err)
	}
	out := issuedCert{keyPath: filepath.Join(h.dir, name), certPath: filepath.Join(h.dir, name+"-cert.pub"), cert: cert}
	writeFile(h.t, out.keyPath, privText)
	writeFile(h.t, out.certPath, AuthorizedKey(cert, name)+"\n")
	return out
}

func (h *opensshHarness) login(c issuedCert) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.ssh, "-F", "/dev/null",
		"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=none", "-o", "PreferredAuthentications=publickey",
		"-o", "ConnectTimeout=10",
		"-i", c.keyPath, "-o", "CertificateFile="+c.certPath,
		"-p", fmt.Sprint(h.port), h.user+"@127.0.0.1", "echo", "hikyo-ok")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "hikyo-ok") {
		return fmt.Errorf("ssh ran but printed %q", out)
	}
	return nil
}

func TestOpenSSHAuthenticationKRLExpiryAndRotation(t *testing.T) {
	h := startSSHD(t)
	now := time.Now()
	caOld, caNew := mustCA(t, AlgorithmEd25519), mustCA(t, AlgorithmRSA3072)

	h.trust(caOld)
	good := h.issue("good", caOld, 1001, now.Add(-ClockSkew), now.Add(time.Hour), nil)
	if err := h.login(good); err != nil {
		t.Fatalf("valid certificate refused: %v", err)
	}

	// ssh-keygen agrees with our KRL encoding.
	revoked := h.issue("revoked", caOld, 1002, now.Add(-ClockSkew), now.Add(time.Hour), nil)
	if err := h.login(revoked); err != nil {
		t.Fatalf("certificate refused before revocation: %v", err)
	}
	h.writeKRL(KRLSection{CAKey: caOld.PublicKey(), Serials: []uint64{1002}})
	q, _ := exec.Command(h.keygen, "-Q", "-f", h.krl, revoked.certPath).CombinedOutput()
	if !strings.Contains(string(q), "REVOKED") {
		t.Fatalf("ssh-keygen -Q does not see the revocation: %s", q)
	}
	q, _ = exec.Command(h.keygen, "-Q", "-f", h.krl, good.certPath).CombinedOutput()
	if !strings.Contains(string(q), "ok") {
		t.Fatalf("ssh-keygen -Q reports an unrevoked certificate as %s", q)
	}
	if err := h.login(revoked); err == nil {
		t.Fatal("revoked certificate authenticated")
	}
	if err := h.login(good); err != nil {
		t.Fatalf("unrevoked certificate refused once a KRL is present: %v", err)
	}

	expired := h.issue("expired", caOld, 1003, now.Add(-2*time.Hour), now.Add(-time.Hour), nil)
	if err := h.login(expired); err == nil {
		t.Fatal("expired certificate authenticated")
	}

	elsewhere := h.issue("elsewhere", caOld, 1004, now.Add(-ClockSkew), now.Add(time.Hour), []string{"10.9.9.9/32"})
	if err := h.login(elsewhere); err == nil {
		t.Fatal("certificate bound to another source address authenticated")
	}

	// Rotation overlap: both keys trusted, both authenticate; the new key's
	// RSA signature is rsa-sha2-512.
	h.trust(caNew, caOld)
	fresh := h.issue("fresh", caNew, 2001, now.Add(-ClockSkew), now.Add(time.Hour), nil)
	if err := h.login(fresh); err != nil {
		t.Fatalf("new-key certificate refused during overlap: %v", err)
	}
	if err := h.login(good); err != nil {
		t.Fatalf("old-key certificate refused during overlap: %v", err)
	}
	// Retirement: the old key leaves the trust bundle.
	h.trust(caNew)
	if err := h.login(good); err == nil {
		t.Fatal("certificate from a retired CA key authenticated")
	}
	if err := h.login(fresh); err != nil {
		t.Fatalf("active-key certificate refused after retirement: %v", err)
	}
}
