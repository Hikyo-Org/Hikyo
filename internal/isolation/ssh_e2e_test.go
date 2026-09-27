package isolation

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/sshca"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// sshHarness is one "node": a service instance over a shared datastore. Two
// harnesses over one database are two nodes of a #146 cluster.
type sshHarness struct {
	svc *service.SSH
	// skew shifts this node's clock from real time (reauth windows are
	// stamped with real time, so a frozen clock would predate them).
	skew time.Duration
}

func (h *sshHarness) now() time.Time { return time.Now().UTC().Add(h.skew) }

func sshAuth(t *testing.T, db *store.DB) *service.Auth {
	t.Helper()
	auth := authService(t, db)
	auth.ReauthWindow, auth.ReauthHardCap = 5*time.Minute, time.Hour
	return auth
}

func newSSHNode(t *testing.T, db *store.DB) *sshHarness {
	t.Helper()
	h := &sshHarness{}
	h.svc = &service.SSH{DB: db, Keyring: probeKeyring(t, db), Auth: sshAuth(t, db), Runtime: store.NewSSHRuntime(db), Now: h.now}
	return h
}

// krlHasSerial reports whether an encoded KRL lists serial (a big-endian
// uint64 inside a serial-list subsection). The OpenSSH interop test in
// internal/sshca proves the encoding against ssh-keygen and sshd.
func krlHasSerial(krl []byte, serial int64) bool {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(serial))
	return bytes.Contains(krl[48:], b[:])
}

func parseIssued(t *testing.T, text string) *ssh.Certificate {
	t.Helper()
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(text))
	if err != nil {
		t.Fatalf("parse issued certificate: %v", err)
	}
	cert, ok := pub.(*ssh.Certificate)
	if !ok {
		t.Fatal("issued text is not a certificate")
	}
	return cert
}

// trustedBy is the host's decision: the certificate's signing key must be in
// the trust bundle (CertChecker.CheckCert alone does not consult authority),
// and the certificate must verify for the principal now.
func trustedBy(t *testing.T, bundle string, cert *ssh.Certificate, principal string) error {
	t.Helper()
	trusted := false
	for _, line := range strings.Split(strings.TrimSpace(bundle), "\n") {
		pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err == nil && bytes.Equal(pub.Marshal(), cert.SignatureKey.Marshal()) {
			trusted = true
		}
	}
	if !trusted {
		return errors.New("signing key is not in the trust bundle")
	}
	return (&ssh.CertChecker{}).CheckCert(principal, cert)
}

func TestSSHCertificatesLifecycle(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) { runSSHLifecycle(t, db, true) })
}

// runSSHLifecycle drives the #155 surface end to end through the real
// service, store and sweeper. The audit suite calls it with full=false for
// the registry-emitter obligation.
func runSSHLifecycle(t *testing.T, db *store.DB, full bool) {
	t.Helper()
	ctx := tctx(t)
	env := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
	admin := service.LocalPrincipal(alice)

	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_ssh_manage','usr_alice','manage-identities','org_a','prj_a1',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_ssh_read','usr_alice','read','org_a','prj_a1','env_a1',`+ts+`)`)

	ident := identitySvc(db)
	sa, err := ident.CreateServiceAccount(ctx, admin, prjScope(), "ssh-deployer", domain.ClassWorkload)
	if err != nil {
		t.Fatalf("create workload: %v", err)
	}
	minted, err := ident.MintCredential(ctx, admin, prjScope(), sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatalf("mint workload credential: %v", err)
	}
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_ssh_sa_read','`+string(sa.Principal)+`','read','org_a','prj_a1','env_a1',`+ts+`)`)
	workload := service.Bearer(minted.Value)

	nodeA := newSSHNode(t, db)
	nodeB := newSSHNode(t, db)

	ca, err := nodeA.svc.CreateCA(ctx, admin, env, service.CreateSSHCARequest{Name: "prod-users"})
	if err != nil {
		t.Fatalf("create CA: %v", err)
	}
	if len(ca.Keys) != 1 || ca.Keys[0].State != "active" || !ca.Trusted[0] || ca.Keys[0].Origin != "generated" {
		t.Fatalf("new CA keys = %+v", ca.Keys)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM ssh_ca_keys WHERE private_key_ciphertext IS NOT NULL AND state='active'"); got != 1 {
		t.Fatalf("sealed active keys = %d", got)
	}
	if _, err := nodeA.svc.CreateCA(ctx, admin, env, service.CreateSSHCARequest{Name: "prod-users"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate CA name: %v", err)
	}
	// A workload cannot administer CAs.
	if _, err := nodeA.svc.CreateCA(ctx, workload, env, service.CreateSSHCARequest{Name: "rogue"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("workload CA create: %v", err)
	}

	profile, err := nodeA.svc.CreateProfile(ctx, admin, env, service.SSHProfileRequest{
		CAID: ca.ID, Name: "deploy", Principals: []string{"deploy", "ops"}, SourceAddresses: []string{"10.0.0.0/8"},
		Extensions: []string{"permit-pty", "permit-port-forwarding"}, KeyAlgorithms: []string{"ed25519", "ecdsa-p256"},
		DefaultTTLSeconds: 3600, MaxTTLSeconds: 8 * 3600, Enabled: true,
		Requesters: []string{string(sa.Principal), string(alice)},
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if _, err := nodeA.svc.CreateProfile(ctx, admin, env, service.SSHProfileRequest{
		CAID: ca.ID, Name: "ghost", Principals: []string{"x"}, KeyAlgorithms: []string{"ed25519"},
		DefaultTTLSeconds: 60, MaxTTLSeconds: 60, Enabled: true, Requesters: []string{"usr_does_not_exist"},
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown requester accepted: %v", err)
	}

	// Machine issuance with a supplied public key.
	userKey, _ := sshca.GenerateKey(sshca.AlgorithmEd25519)
	userPub, _ := sshca.PublicKeyOf(userKey)
	issued, err := nodeA.svc.Issue(ctx, workload, env, service.IssueSSHCertificateRequest{
		ProfileID: profile.ID, PublicKey: sshca.AuthorizedKey(userPub, "me@laptop"), Principals: []string{"deploy"},
	})
	if err != nil {
		t.Fatalf("workload issue: %v", err)
	}
	if issued.PrivateKey != "" {
		t.Fatal("a supplied-key issuance returned a private key")
	}
	cert := parseIssued(t, issued.CertificateText)
	bundle, err := nodeA.svc.TrustedKeys(ctx, workload, env, ca.ID)
	if err != nil {
		t.Fatalf("workload trust bundle: %v", err)
	}
	if err := trustedBy(t, bundle, cert, "deploy"); err != nil {
		t.Fatalf("issued certificate does not verify against the trust bundle: %v", err)
	}
	if cert.CriticalOptions["source-address"] != "10.0.0.0/8" || !strings.Contains(cert.KeyId, string(sa.Principal)) {
		t.Fatalf("certificate content: %+v %q", cert.CriticalOptions, cert.KeyId)
	}
	if got := time.Duration(cert.ValidBefore-cert.ValidAfter) * time.Second; got != time.Hour+sshca.ClockSkew {
		t.Fatalf("default TTL window = %s", got)
	}
	if issued.Certificate.Status != "active" || issued.Certificate.KeyOrigin != "supplied" {
		t.Fatalf("issued view = %+v", issued.Certificate)
	}

	// Profile bounds are enforced.
	for name, req := range map[string]service.IssueSSHCertificateRequest{
		"foreign principal": {ProfileID: profile.ID, Principals: []string{"root"}},
		"ttl over max":      {ProfileID: profile.ID, Principals: []string{"deploy"}, TTLSeconds: 9 * 3600},
		"ttl wraps":         {ProfileID: profile.ID, Principals: []string{"deploy"}, TTLSeconds: 3600 + 1<<55}, // *time.Second wraps to 1h
		"wider source":      {ProfileID: profile.ID, Principals: []string{"deploy"}, SourceAddresses: []string{"0.0.0.0/0"}},
		"extension":         {ProfileID: profile.ID, Principals: []string{"deploy"}, Extensions: []string{"permit-agent-forwarding"}},
		"algorithm":         {ProfileID: profile.ID, Principals: []string{"deploy"}, KeyAlgorithm: "rsa-3072"},
	} {
		if _, err := nodeA.svc.Issue(ctx, workload, env, req); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}

	// A principal with read but not on the requester list is told nothing.
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_ssh_reader','usr_reader','read','org_a','prj_a1','env_a1',`+ts+`)`)
	if _, err := nodeA.svc.Issue(ctx, service.LocalPrincipal(reader), env, service.IssueSSHCertificateRequest{ProfileID: profile.ID, Principals: []string{"deploy"}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("non-requester issue: %v", err)
	}
	if !full {
		finishSSHLifecycle(t, db, nodeA, admin, env, ca.ID, profile.ID, issued.Certificate.ID)
		return
	}

	// A human requester needs the mint ceremony.
	if _, err := nodeA.svc.Issue(ctx, admin, env, service.IssueSSHCertificateRequest{ProfileID: profile.ID, Principals: []string{"ops"}}); !errors.Is(err, service.ErrReauthRequired) {
		t.Fatalf("human issue without ceremony: %v", err)
	}
	human := service.Bearer(mintReauthedSession(t, db, alice, string(envA1)))
	generated, err := nodeA.svc.Issue(ctx, human, env, service.IssueSSHCertificateRequest{ProfileID: profile.ID, Principals: []string{"ops"}, KeyAlgorithm: "ecdsa-p256"})
	if err != nil {
		t.Fatalf("human issue with ceremony: %v", err)
	}
	priv, err := ssh.ParsePrivateKey([]byte(generated.PrivateKey))
	if err != nil {
		t.Fatalf("generated private key does not parse: %v", err)
	}
	humanCert := parseIssued(t, generated.CertificateText)
	if !bytes.Equal(priv.PublicKey().Marshal(), humanCert.Key.Marshal()) {
		t.Fatal("generated private key does not match the certified key")
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM ssh_certificates WHERE id='"+generated.Certificate.ID+"' AND key_origin='generated'"); got != 1 {
		t.Fatal("generated issuance not recorded")
	}

	// Explicit revoke: the requester may revoke its own; the serial enters
	// the KRL, and a second revoke is a no-op.
	revoked, err := nodeA.svc.RevokeCertificate(ctx, workload, env, issued.Certificate.ID)
	if err != nil {
		t.Fatalf("revoke own certificate: %v", err)
	}
	if revoked.Status != "revoked" || !revoked.InKRL || revoked.RevocationReason != "explicit" {
		t.Fatalf("revoked view = %+v", revoked)
	}
	krl, err := nodeB.svc.KRL(ctx, workload, env, ca.ID)
	if err != nil {
		t.Fatalf("KRL: %v", err)
	}
	if !krlHasSerial(krl, revoked.Serial) {
		t.Fatal("revoked serial missing from the KRL")
	}
	if _, err := nodeA.svc.RevokeCertificate(ctx, workload, env, issued.Certificate.ID); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
	// Someone else's certificate needs manage-identities.
	if _, err := nodeA.svc.RevokeCertificate(ctx, workload, env, generated.Certificate.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("workload revoked another requester's certificate: %v", err)
	}

	// Natural expiry: past valid_before the certificate is expired and leaves
	// the KRL (there is nothing left for a host to refuse).
	nodeB.skew = 2 * time.Hour
	view, err := nodeB.svc.GetCertificate(ctx, admin, env, issued.Certificate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "revoked" || view.InKRL {
		t.Fatalf("expired revoked certificate still in KRL: %+v", view)
	}
	if expired, _ := nodeB.svc.GetCertificate(ctx, admin, env, generated.Certificate.ID); expired.Status != "expired" {
		t.Fatalf("unrevoked certificate past valid_before = %q", expired.Status)
	}
	if krl, _ = nodeB.svc.KRL(ctx, workload, env, ca.ID); krlHasSerial(krl, revoked.Serial) {
		t.Fatal("expired serial still published")
	}
	nodeB.skew = 0

	// Rotation on node B with a bounded overlap; node A (no cache) signs with
	// the new key immediately, and the old key's certificates stay trusted
	// until the overlap ends.
	before, err := nodeA.svc.Issue(ctx, workload, env, service.IssueSSHCertificateRequest{ProfileID: profile.ID, PublicKey: sshca.AuthorizedKey(userPub, ""), Principals: []string{"deploy"}})
	if err != nil {
		t.Fatalf("issue before rotation: %v", err)
	}
	overlap := int64(1800)
	rotated, err := nodeB.svc.RotateCA(ctx, admin, env, ca.ID, service.RotateSSHCARequest{Algorithm: "rsa-3072", OverlapSeconds: &overlap})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if len(rotated.Keys) != 2 || rotated.Keys[0].State != "active" || rotated.Keys[1].State != "retiring" || !rotated.Trusted[1] {
		t.Fatalf("rotated keys = %+v trusted=%v", rotated.Keys, rotated.Trusted)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM ssh_ca_keys WHERE state='retiring' AND private_key_ciphertext IS NOT NULL"); got != 0 {
		t.Fatal("a retiring key kept its private material")
	}
	after, err := nodeA.svc.Issue(ctx, workload, env, service.IssueSSHCertificateRequest{ProfileID: profile.ID, PublicKey: sshca.AuthorizedKey(userPub, ""), Principals: []string{"deploy"}})
	if err != nil {
		t.Fatalf("issue after rotation: %v", err)
	}
	if after.Certificate.CAKeyID != rotated.Keys[0].ID {
		t.Fatal("node A signed with a stale key after node B rotated")
	}
	bundle, _ = nodeA.svc.TrustedKeys(ctx, workload, env, ca.ID)
	if err := trustedBy(t, bundle, parseIssued(t, before.CertificateText), "deploy"); err != nil {
		t.Fatalf("old-key certificate untrusted during overlap: %v", err)
	}
	if err := trustedBy(t, bundle, parseIssued(t, after.CertificateText), "deploy"); err != nil {
		t.Fatalf("new-key certificate untrusted: %v", err)
	}
	nodeA.skew = time.Duration(overlap+1) * time.Second
	if v, _ := nodeA.svc.GetCertificate(ctx, admin, env, before.Certificate.ID); v.Status != "untrusted" {
		t.Fatalf("old-key certificate after overlap = %q", v.Status)
	}
	nodeA.skew = 0
	if _, err := nodeA.svc.RetireCAKey(ctx, admin, env, ca.ID, rotated.Keys[1].ID); err != nil {
		t.Fatalf("retire: %v", err)
	}
	bundle, _ = nodeA.svc.TrustedKeys(ctx, workload, env, ca.ID)
	if strings.Count(bundle, "\n") != 1 || trustedBy(t, bundle, parseIssued(t, before.CertificateText), "deploy") == nil {
		t.Fatal("retired key still in the trust bundle")
	}

	// Profile disable on node A fences issuance on node B at once.
	disabled := service.SSHProfileRequest{
		CAID: ca.ID, Name: "deploy", Principals: profile.Principals, SourceAddresses: profile.SourceAddresses,
		Extensions: profile.Extensions, KeyAlgorithms: profile.KeyAlgorithms,
		DefaultTTLSeconds: profile.DefaultTTLSeconds, MaxTTLSeconds: profile.MaxTTLSeconds,
		Enabled: false, Requesters: profile.Requesters,
	}
	if _, err := nodeA.svc.UpdateProfile(ctx, admin, env, profile.ID, disabled); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := nodeB.svc.Issue(ctx, workload, env, service.IssueSSHCertificateRequest{ProfileID: profile.ID, PublicKey: sshca.AuthorizedKey(userPub, ""), Principals: []string{"deploy"}}); !errors.Is(err, service.ErrSSHProfileDisabled) {
		t.Fatalf("issue through a disabled profile: %v", err)
	}
	if v, _ := nodeB.svc.GetCertificate(ctx, admin, env, after.Certificate.ID); v.Status != "active" {
		t.Fatalf("disable revoked an issued certificate: %q", v.Status)
	}
	disabled.Enabled = true
	if _, err := nodeA.svc.UpdateProfile(ctx, admin, env, profile.ID, disabled); err != nil {
		t.Fatalf("re-enable: %v", err)
	}

	// Removing a requester revokes its live certificates in the same write.
	disabled.Requesters = []string{string(alice)}
	if _, err := nodeA.svc.UpdateProfile(ctx, admin, env, profile.ID, disabled); err != nil {
		t.Fatalf("remove requester: %v", err)
	}
	if v, _ := nodeA.svc.GetCertificate(ctx, admin, env, after.Certificate.ID); v.Status != "revoked" || v.RevocationReason != "authority-withdrawn" || !v.InKRL {
		t.Fatalf("removed requester's certificate = %+v", v)
	}
	disabled.Requesters = []string{string(alice), string(sa.Principal)}
	if _, err := nodeA.svc.UpdateProfile(ctx, admin, env, profile.ID, disabled); err != nil {
		t.Fatalf("restore requester: %v", err)
	}

	// Grant revocation: the sweeper (any node, idempotently) revokes a
	// certificate whose requester lost read@environment.
	live, err := nodeA.svc.Issue(ctx, workload, env, service.IssueSSHCertificateRequest{ProfileID: profile.ID, PublicKey: sshca.AuthorizedKey(userPub, ""), Principals: []string{"deploy"}})
	if err != nil {
		t.Fatalf("issue before grant revocation: %v", err)
	}
	if n, err := nodeA.svc.RunSweep(ctx); err != nil || n != 0 {
		t.Fatalf("sweep with authority intact revoked %d (%v)", n, err)
	}
	execRaw(t, db, `DELETE FROM grants WHERE id='g_ssh_sa_read'`)
	n1, err := nodeA.svc.RunSweep(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	n2, err := nodeB.svc.RunSweep(ctx)
	if err != nil || n1 != 1 || n2 != 0 {
		t.Fatalf("sweeps revoked %d then %d (%v), want 1 then 0", n1, n2, err)
	}
	if v, _ := nodeA.svc.GetCertificate(ctx, admin, env, live.Certificate.ID); v.Status != "revoked" || v.RevocationReason != "authority-withdrawn" {
		t.Fatalf("swept certificate = %+v", v)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='ssh.certificate_revoked' AND actor_class='system' AND object_id='"+live.Certificate.ID+"'"); got != 1 {
		t.Fatalf("sweeper audit rows = %d", got)
	}

	// Deleting a profile without revoke_issued leaves its live certificates
	// valid: the sweeper must not read the tombstone as a withdrawal.
	kept, err := nodeA.svc.CreateProfile(ctx, admin, env, service.SSHProfileRequest{
		CAID: ca.ID, Name: "kept", Principals: []string{"ops"}, KeyAlgorithms: []string{"ed25519"},
		DefaultTTLSeconds: 3600, MaxTTLSeconds: 3600, Enabled: true, Requesters: []string{string(alice)},
	})
	if err != nil {
		t.Fatalf("create kept profile: %v", err)
	}
	keptCert, err := nodeA.svc.Issue(ctx, human, env, service.IssueSSHCertificateRequest{ProfileID: kept.ID, Principals: []string{"ops"}})
	if err != nil {
		t.Fatalf("issue through kept profile: %v", err)
	}
	if n, err := nodeA.svc.DeleteProfile(ctx, admin, env, kept.ID, false); err != nil || n != 0 {
		t.Fatalf("delete without revoke: revoked %d (%v)", n, err)
	}
	if n, err := nodeB.svc.RunSweep(ctx); err != nil || n != 0 {
		t.Fatalf("sweep after delete without revoke revoked %d (%v)", n, err)
	}
	if v, _ := nodeA.svc.GetCertificate(ctx, admin, env, keptCert.Certificate.ID); v.Status != "active" {
		t.Fatalf("certificate of a profile deleted without revoke = %q", v.Status)
	}

	// Restart: a fresh node with a reloaded keyring still signs.
	restarted := &sshHarness{}
	restarted.svc = &service.SSH{DB: db, Keyring: reloadProbeKeyring(t, db), Auth: sshAuth(t, db), Runtime: store.NewSSHRuntime(db), Now: restarted.now}
	if _, err := restarted.svc.Issue(ctx, human, env, service.IssueSSHCertificateRequest{ProfileID: profile.ID, Principals: []string{"ops"}}); err != nil {
		t.Fatalf("issue after restart: %v", err)
	}

	finishSSHLifecycle(t, db, nodeA, admin, env, ca.ID, profile.ID, "")
}

// finishSSHLifecycle is the teardown both variants share: CA delete refusals,
// profile delete with revocation, CA delete, and the environment-delete guard.
func finishSSHLifecycle(t *testing.T, db *store.DB, node *sshHarness, admin service.Actor, env domain.Scope, caID, profileID, revokeID string) {
	t.Helper()
	ctx := tctx(t)
	if revokeID != "" {
		if _, err := node.svc.RevokeCertificate(ctx, admin, env, revokeID); err != nil {
			t.Fatalf("admin revoke: %v", err)
		}
	}
	if _, err := node.svc.ListCAs(ctx, admin, env); err != nil {
		t.Fatalf("workload CA list: %v", err)
	}
	if _, err := node.svc.ListProfiles(ctx, admin, env); err != nil {
		t.Fatalf("workload profile list: %v", err)
	}
	if _, err := node.svc.ListCertificates(ctx, admin, env); err != nil {
		t.Fatalf("certificate list: %v", err)
	}
	if err := node.svc.DeleteCA(ctx, admin, env, caID); !errors.Is(err, service.ErrSSHCAHasProfiles) {
		t.Fatalf("CA delete with live profiles: %v", err)
	}
	rotated, err := node.svc.RotateCA(ctx, admin, env, caID, service.RotateSSHCARequest{})
	if err != nil {
		t.Fatalf("rotate with default overlap: %v", err)
	}
	// The default overlap is the longest live profile max_ttl.
	retiring := rotated.Keys[1]
	if retiring.State != "retiring" || retiring.RetireAfter == "" {
		t.Fatalf("default-overlap rotation left %+v", retiring)
	}
	if _, err := node.svc.RetireCAKey(ctx, admin, env, caID, retiring.ID); err != nil {
		t.Fatalf("retire after default rotation: %v", err)
	}
	n, err := node.svc.DeleteProfile(ctx, admin, env, profileID, true)
	if err != nil {
		t.Fatalf("profile delete: %v", err)
	}
	if left := queryInt(t, db, "SELECT COUNT(*) FROM ssh_certificates WHERE profile_id='"+profileID+"' AND state='issued' AND valid_before > '"+store.CanonTime(node.now()).Format(time.RFC3339Nano)+"'"); left != 0 {
		t.Fatalf("profile delete left %d live certificates (revoked %d)", left, n)
	}
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
	if err := envs.Delete(ctx, admin, env); !errors.Is(err, store.ErrSSHLiveCA) {
		t.Fatalf("environment delete with a live SSH CA: %v", err)
	}
	if err := node.svc.DeleteCA(ctx, admin, env, caID); err != nil {
		t.Fatalf("CA delete: %v", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM ssh_ca_keys WHERE ca_id='"+caID+"' AND (state<>'retired' OR private_key_ciphertext IS NOT NULL)"); got != 0 {
		t.Fatalf("CA delete left %d keys trusted or holding material", got)
	}
	if _, err := node.svc.TrustedKeys(ctx, admin, env, caID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted CA trust bundle: %v", err)
	}
	for _, typ := range []string{
		"ssh.ca_configured", "ssh.ca_rotated", "ssh.ca_key_retired", "ssh.ca_deleted", "ssh.profile_configured", "ssh.profile_deleted",
		"ssh.certificate_issued", "ssh.certificate_revoked",
	} {
		if got := queryInt(t, db, fmt.Sprintf("SELECT COUNT(*) FROM audit_tenant_events WHERE type='%s'", typ)); got == 0 {
			t.Errorf("ssh lifecycle did not emit %s", typ)
		}
	}
}
