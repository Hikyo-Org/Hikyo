package service

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	hcrypto "github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/sshca"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// SSH owns the SSH user-certificate surface (#155): environment-scoped CAs
// (generated or imported, private key sealed under the project DEK and never
// returned), profiles that bound what a certificate may say and who may ask,
// issuance, revocation, the host trust bundle and the Key Revocation List.
// Design: docs/handoff/155-ssh-certificates.md.
type SSH struct {
	DB      *store.DB
	Auth    *Auth
	Keyring *hcrypto.Keyring
	Budget  *Budget
	Runtime *store.SSHRuntime
	Now     func() time.Time
}

func (s *SSH) now() time.Time { return nowOr(s.Now) }

const (
	// sshMaxOverlap bounds a rotation overlap.
	sshMaxOverlap = 30 * 24 * time.Hour
	// sshMaxRequesters bounds one profile's requester list.
	sshMaxRequesters = 256
	// sshCertificateListLimit bounds one certificate listing.
	sshCertificateListLimit = 500
	// sshSweepPage is the sweeper's page size.
	sshSweepPage     = 200
	maxSSHNameLength = 64
)

var (
	// ErrSSHProfileDisabled refuses issuance through a disabled profile. It is
	// only reachable by a listed requester, so it discloses nothing.
	ErrSSHProfileDisabled = fmt.Errorf("%w: the SSH profile is disabled", domain.ErrConflict)
	// ErrSSHCAHasProfiles refuses a CA delete while live profiles use it.
	ErrSSHCAHasProfiles = fmt.Errorf("%w: the SSH CA still has profiles; delete them first", domain.ErrConflict)
	// ErrSSHKRLTooLarge reports a KRL over its bound: refused, never truncated.
	ErrSSHKRLTooLarge = fmt.Errorf("%w: the key revocation list exceeds its bound of %d serials", domain.ErrConflict, sshca.MaxKRLSerials)
)

// sshCAKeyAAD binds a sealed CA private key to its key row.
func sshCAKeyAAD(orgID, projectID, keyID string) hcrypto.ProjectFieldAAD {
	return hcrypto.ProjectFieldAAD{
		OrgID: orgID, ProjectID: projectID,
		OwnerTable: "ssh_ca_keys", OwnerRowID: keyID, FieldTag: "private_key",
	}
}

func requireEnvScope(scope domain.Scope, msg string, ids ...string) error {
	bad := scope.Org == "" || scope.Project == "" || scope.Env == ""
	for _, id := range ids {
		bad = bad || id == ""
	}
	if bad {
		return fmt.Errorf("%w: %s", domain.ErrInvalid, msg)
	}
	return nil
}

func validSSHName(name string) error {
	if name == "" || len(name) > maxSSHNameLength {
		return fmt.Errorf("%w: name must be 1 to %d characters", domain.ErrInvalid, maxSSHNameLength)
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return fmt.Errorf("%w: name may contain only lowercase letters, digits, '-', '_' and '.'", domain.ErrInvalid)
		}
	}
	return nil
}

func sshInvalid(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sshca.ErrConstraint) || errors.Is(err, sshca.ErrUnsupportedKey) || errors.Is(err, sshca.ErrEncryptedKey) {
		return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	return err
}

// ---- Views ------------------------------------------------------------------

// SSHCAView is a CA and its keys, with each key's trust derived at read time.
type SSHCAView struct {
	store.SSHCA
	// Trusted lists, in the same order as Keys, whether hosts following the
	// trust bundle accept that key now.
	Trusted []bool
}

// SSHProfileView is one profile.
type SSHProfileView struct{ store.SSHProfile }

// SSHCertificateView is a certificate record with its derived status.
type SSHCertificateView struct {
	store.SSHCertificate
	// Status is revoked | expired | untrusted | active.
	Status string
	// InKRL is whether the serial is published in the CA's revocation list.
	InKRL bool
}

// SSHIssueResult carries a new certificate and, for a generated key, the
// private key exactly once.
type SSHIssueResult struct {
	Certificate     SSHCertificateView
	CertificateText string
	PublicKey       string
	PrivateKey      string
}

func keyTrusted(state, retireAfter string, now time.Time) bool {
	switch state {
	case "active":
		return true
	case "retiring":
		t, err := time.Parse(time.RFC3339Nano, retireAfter)
		return err == nil && now.Before(t)
	default:
		return false
	}
}

func caView(ca store.SSHCA, now time.Time) SSHCAView {
	view := SSHCAView{SSHCA: ca, Trusted: make([]bool, len(ca.Keys))}
	for i, k := range ca.Keys {
		view.Trusted[i] = ca.State == "active" && keyTrusted(k.State, k.RetireAfter, now)
	}
	return view
}

func certificateView(c store.SSHCertificate, now time.Time) SSHCertificateView {
	view := SSHCertificateView{SSHCertificate: c}
	validBefore, err := time.Parse(time.RFC3339Nano, c.ValidBefore)
	expired := err != nil || !now.Before(validBefore)
	trusted := c.CAState == "active" && keyTrusted(c.CAKeyState, c.CAKeyRetireAfter, now)
	switch {
	case c.State == "revoked":
		view.Status = "revoked"
		view.InKRL = !expired && trusted
	case expired:
		view.Status = "expired"
	case !trusted:
		view.Status = "untrusted"
	default:
		view.Status = "active"
	}
	return view
}

// ---- CA key material ----------------------------------------------------------

// sshKeyMaterial is a CA key ready to store: public parts plus the sealed
// private key. The plaintext never leaves newCAKeyMaterial.
type sshKeyMaterial struct {
	id, algorithm, publicKey, fingerprint, origin string
	sealed                                        []byte
}

// newCAKeyMaterial generates or parses a CA key and seals it under the
// project DEK, returning the sealer it used.
//
// fence:delegated: returns the sealed key and the sealer to a caller that
// fences on the sealer's active version (fenceProject) inside the write
// transaction, before the key row is written.
func (s *SSH) newCAKeyMaterial(ctx context.Context, actor Actor, op authz.Operation, scope domain.Scope, algorithm string, imported []byte) (sshKeyMaterial, *hcrypto.ProjectSealer, error) {
	var (
		key    crypto.Signer
		alg    sshca.Algorithm
		origin = "generated"
		err    error
	)
	if len(imported) > 0 {
		origin = "imported"
		key, alg, err = sshca.ParseImportedKey(imported)
		if err != nil {
			return sshKeyMaterial{}, nil, sshInvalid(err)
		}
		if algorithm != "" && algorithm != string(alg) {
			return sshKeyMaterial{}, nil, fmt.Errorf("%w: imported key is %s, not %s", domain.ErrInvalid, alg, algorithm)
		}
	} else {
		if algorithm == "" {
			algorithm = string(sshca.AlgorithmEd25519)
		}
		if alg, err = sshca.ParseAlgorithm(algorithm); err != nil {
			return sshKeyMaterial{}, nil, sshInvalid(err)
		}
		if key, err = sshca.GenerateKey(alg); err != nil {
			return sshKeyMaterial{}, nil, err
		}
	}
	pub, err := sshca.PublicKeyOf(key)
	if err != nil {
		return sshKeyMaterial{}, nil, err
	}
	der, err := sshca.MarshalCAKey(key)
	if err != nil {
		return sshKeyMaterial{}, nil, sshInvalid(err)
	}
	defer hcrypto.Zero(der)
	keyID, err := newID("sck")
	if err != nil {
		return sshKeyMaterial{}, nil, err
	}
	sealer, err := sealerFor(ctx, s.DB, s.Keyring, actor, op, scope)
	if err != nil {
		return sshKeyMaterial{}, nil, err
	}
	sealed, err := sealer.SealField(sshCAKeyAAD(string(scope.Org), string(scope.Project), keyID), der)
	if err != nil {
		return sshKeyMaterial{}, nil, err
	}
	return sshKeyMaterial{
		id: keyID, algorithm: string(alg), origin: origin, sealed: sealed,
		publicKey: sshca.AuthorizedKey(pub, ""), fingerprint: sshca.Fingerprint(pub),
	}, sealer, nil
}

// ---- CAs --------------------------------------------------------------------

// CreateSSHCARequest creates a CA with a generated key, or imports one.
// PrivateKey is write-only protected input; the caller zeroes it.
type CreateSSHCARequest struct {
	Name       string
	Algorithm  string
	PrivateKey []byte
}

func (s *SSH) CreateCA(ctx context.Context, actor Actor, scope domain.Scope, req CreateSSHCARequest) (SSHCAView, error) {
	if err := requireEnvScope(scope, "ssh CA create requires environment scope"); err != nil {
		return SSHCAView{}, err
	}
	if err := validSSHName(req.Name); err != nil {
		return SSHCAView{}, err
	}
	release, err := chargeDefaultAtEntry(ctx, s.DB, s.Budget, actor, authz.OpSSHCAConfigure, authz.OpSSHCAConfigure, scope, s.now)
	if err != nil {
		return SSHCAView{}, err
	}
	defer release()
	material, sealer, err := s.newCAKeyMaterial(ctx, actor, authz.OpSSHCAConfigure, scope, req.Algorithm, req.PrivateKey)
	if err != nil {
		return SSHCAView{}, err
	}
	caID, err := newID("sca")
	if err != nil {
		return SSHCAView{}, err
	}
	now := store.CanonTime(s.now())
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (SSHCAView, error) {
		caller, proof, err := authorize(ctx, az, actor, authz.OpSSHCAConfigure, scope, now)
		if err != nil {
			return SSHCAView{}, err
		}
		if err := fenceProject(ctx, r, proof, sealer, scope); err != nil {
			return SSHCAView{}, err
		}
		if err := r.SSH().CreateCA(ctx, proof, store.SSHCACreate{ID: caID, Name: req.Name, AuthorityPrincipalID: string(caller.Principal), At: now}); err != nil {
			return SSHCAView{}, err
		}
		if err := r.SSH().InsertCAKey(ctx, proof, store.SSHCAKeyCreate{
			ID: material.id, CAID: caID, Algorithm: material.algorithm, PublicKey: material.publicKey,
			Fingerprint: material.fingerprint, Origin: material.origin, Ciphertext: material.sealed, At: now,
		}); err != nil {
			return SSHCAView{}, err
		}
		ev, err := domainEvent(ctx, audit.EventSSHCAConfigured, caller.Principal, audit.Object{Type: "ssh-ca", ID: caID}, audit.Payload{
			"origin": material.origin, "algorithm": material.algorithm, "fingerprint": material.fingerprint, "authority": string(caller.Principal),
		})
		if err != nil {
			return SSHCAView{}, err
		}
		if err := r.Audit().InsertTenant(ctx, proof, ev); err != nil {
			return SSHCAView{}, err
		}
		ca, err := r.SSH().GetCA(ctx, proof, caID)
		if err != nil {
			return SSHCAView{}, err
		}
		return caView(ca, now), nil
	})
}

func (s *SSH) ListCAs(ctx context.Context, actor Actor, scope domain.Scope) ([]SSHCAView, error) {
	if err := requireEnvScope(scope, "ssh CA list requires environment scope"); err != nil {
		return nil, err
	}
	now := s.now()
	var out []SSHCAView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpSSHCAInspect, scope, now)
		if err != nil {
			return err
		}
		rows, err := r.SSH().ListCAs(ctx, proof)
		if err != nil {
			return err
		}
		out = make([]SSHCAView, 0, len(rows))
		for _, ca := range rows {
			out = append(out, caView(ca, now))
		}
		return nil
	})
	return out, err
}

func (s *SSH) GetCA(ctx context.Context, actor Actor, scope domain.Scope, caID string) (SSHCAView, error) {
	if err := requireEnvScope(scope, "ssh CA show requires environment scope and CA id", caID); err != nil {
		return SSHCAView{}, err
	}
	now := s.now()
	var out SSHCAView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpSSHCAInspect, scope, now)
		if err != nil {
			return err
		}
		ca, err := r.SSH().GetCA(ctx, proof, caID)
		if err != nil {
			return err
		}
		out = caView(ca, now)
		return nil
	})
	return out, err
}

// TrustedKeys renders the CA's trust bundle in TrustedUserCAKeys format: the
// active key and every retiring key still inside its overlap.
func (s *SSH) TrustedKeys(ctx context.Context, actor Actor, scope domain.Scope, caID string) (string, error) {
	view, err := s.GetCA(ctx, actor, scope, caID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, key := range view.Keys {
		if !view.Trusted[i] {
			continue
		}
		b.WriteString(key.PublicKey)
		b.WriteString(" hikyo-ssh-ca:")
		b.WriteString(view.Name)
		b.WriteString(":")
		b.WriteString(key.ID)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// KRL renders the CA's OpenSSH Key Revocation List: revoked, unexpired
// serials signed by keys hosts still trust. Over its bound it fails loud.
func (s *SSH) KRL(ctx context.Context, actor Actor, scope domain.Scope, caID string) ([]byte, error) {
	if err := requireEnvScope(scope, "ssh KRL requires environment scope and CA id", caID); err != nil {
		return nil, err
	}
	now := s.now()
	var (
		ca      store.SSHCA
		serials []store.SSHRevokedSerial
	)
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpSSHCAInspect, scope, now)
		if err != nil {
			return err
		}
		if ca, err = r.SSH().GetCA(ctx, proof, caID); err != nil {
			return err
		}
		serials, err = r.SSH().RevokedSerials(ctx, proof, caID, now, sshca.MaxKRLSerials+1)
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(serials) > sshca.MaxKRLSerials {
		return nil, ErrSSHKRLTooLarge
	}
	sections := map[string]*sshca.KRLSection{}
	var order []string
	for _, entry := range serials {
		section, ok := sections[entry.CAKeyPublicKey]
		if !ok {
			pub, _, err := sshca.ParsePublicKey(entry.CAKeyPublicKey)
			if err != nil {
				return nil, fmt.Errorf("service: stored ssh CA public key is malformed: %w", err)
			}
			section = &sshca.KRLSection{CAKey: pub}
			sections[entry.CAKeyPublicKey] = section
			order = append(order, entry.CAKeyPublicKey)
		}
		section.Serials = append(section.Serials, uint64(entry.Serial))
	}
	krl := sshca.KRL{Version: uint64(now.Unix()), GeneratedAt: now, Comment: "hikyo ssh-ca " + ca.ID}
	for _, k := range order {
		krl.Sections = append(krl.Sections, *sections[k])
	}
	out, err := sshca.EncodeKRL(krl)
	if errors.Is(err, sshca.ErrKRLTooLarge) {
		return nil, ErrSSHKRLTooLarge
	}
	return out, err
}

// RotateSSHCARequest rotates a CA to a new generated or imported key. A nil
// OverlapSeconds takes the longest max_ttl among the CA's live profiles.
type RotateSSHCARequest struct {
	Algorithm      string
	PrivateKey     []byte
	OverlapSeconds *int64
}

// sshSeconds converts a caller-supplied second count, bounded to [0, limit]
// before the multiplication so an oversized count cannot wrap into range.
func sshSeconds(field string, seconds int64, limit time.Duration) (time.Duration, error) {
	if seconds < 0 || seconds > int64(limit/time.Second) {
		return 0, fmt.Errorf("%w: %s must be between 0 and %d seconds", domain.ErrInvalid, field, int64(limit/time.Second))
	}
	return time.Duration(seconds) * time.Second, nil
}

func (s *SSH) RotateCA(ctx context.Context, actor Actor, scope domain.Scope, caID string, req RotateSSHCARequest) (SSHCAView, error) {
	if err := requireEnvScope(scope, "ssh CA rotate requires environment scope and CA id", caID); err != nil {
		return SSHCAView{}, err
	}
	if req.OverlapSeconds != nil {
		if _, err := sshSeconds("overlap", *req.OverlapSeconds, sshMaxOverlap); err != nil {
			return SSHCAView{}, err
		}
	}
	release, err := chargeDefaultAtEntry(ctx, s.DB, s.Budget, actor, authz.OpSSHCARotate, authz.OpSSHCARotate, scope, s.now)
	if err != nil {
		return SSHCAView{}, err
	}
	defer release()
	material, sealer, err := s.newCAKeyMaterial(ctx, actor, authz.OpSSHCARotate, scope, req.Algorithm, req.PrivateKey)
	if err != nil {
		return SSHCAView{}, err
	}
	now := store.CanonTime(s.now())
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (SSHCAView, error) {
		caller, proof, err := authorize(ctx, az, actor, authz.OpSSHCARotate, scope, now)
		if err != nil {
			return SSHCAView{}, err
		}
		if err := fenceProject(ctx, r, proof, sealer, scope); err != nil {
			return SSHCAView{}, err
		}
		before, err := r.SSH().GetCA(ctx, proof, caID)
		if err != nil {
			return SSHCAView{}, err
		}
		var previous string
		for _, k := range before.Keys {
			if k.State == "active" {
				previous = k.Fingerprint
			}
		}
		overlap := int64(0)
		if req.OverlapSeconds != nil {
			overlap = *req.OverlapSeconds
		} else {
			if overlap, err = r.SSH().MaxProfileTTLForCA(ctx, proof, caID); err != nil {
				return SSHCAView{}, err
			}
			overlap = min(overlap, int64(sshMaxOverlap/time.Second))
		}
		if _, err := r.SSH().RetireActiveCAKey(ctx, proof, caID, now.Add(time.Duration(overlap)*time.Second), now); err != nil {
			return SSHCAView{}, err
		}
		if err := r.SSH().InsertCAKey(ctx, proof, store.SSHCAKeyCreate{
			ID: material.id, CAID: caID, Algorithm: material.algorithm, PublicKey: material.publicKey,
			Fingerprint: material.fingerprint, Origin: material.origin, Ciphertext: material.sealed, At: now,
		}); err != nil {
			return SSHCAView{}, err
		}
		ev, err := domainEvent(ctx, audit.EventSSHCARotated, caller.Principal, audit.Object{Type: "ssh-ca", ID: caID}, audit.Payload{
			"origin": material.origin, "algorithm": material.algorithm, "fingerprint": material.fingerprint,
			"previous_fingerprint": previous, "overlap_seconds": overlap,
		})
		if err != nil {
			return SSHCAView{}, err
		}
		if err := r.Audit().InsertTenant(ctx, proof, ev); err != nil {
			return SSHCAView{}, err
		}
		ca, err := r.SSH().GetCA(ctx, proof, caID)
		if err != nil {
			return SSHCAView{}, err
		}
		return caView(ca, now), nil
	})
}

// RetireCAKey ends a retiring key's overlap now: hosts drop it on their next
// trust-bundle refresh.
func (s *SSH) RetireCAKey(ctx context.Context, actor Actor, scope domain.Scope, caID, keyID string) (SSHCAView, error) {
	if err := requireEnvScope(scope, "ssh CA key retire requires environment scope, CA id and key id", caID, keyID); err != nil {
		return SSHCAView{}, err
	}
	now := store.CanonTime(s.now())
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (SSHCAView, error) {
		caller, proof, err := authorize(ctx, az, actor, authz.OpSSHCARetireKey, scope, now)
		if err != nil {
			return SSHCAView{}, err
		}
		ca, err := r.SSH().GetCA(ctx, proof, caID)
		if err != nil {
			return SSHCAView{}, err
		}
		var fingerprint string
		for _, k := range ca.Keys {
			if k.ID == keyID {
				fingerprint = k.Fingerprint
			}
		}
		if err := r.SSH().RetireCAKey(ctx, proof, caID, keyID, now); err != nil {
			return SSHCAView{}, err
		}
		ev, err := domainEvent(ctx, audit.EventSSHCAKeyRetired, caller.Principal, audit.Object{Type: "ssh-ca", ID: caID}, audit.Payload{"fingerprint": fingerprint})
		if err != nil {
			return SSHCAView{}, err
		}
		if err := r.Audit().InsertTenant(ctx, proof, ev); err != nil {
			return SSHCAView{}, err
		}
		after, err := r.SSH().GetCA(ctx, proof, caID)
		if err != nil {
			return SSHCAView{}, err
		}
		return caView(after, now), nil
	})
}

// DeleteCA tombstones a CA and destroys its signing material. This is record
// deletion, not revocation: certificates it signed stay valid on any host
// that still lists its keys in TrustedUserCAKeys until they expire.
func (s *SSH) DeleteCA(ctx context.Context, actor Actor, scope domain.Scope, caID string) error {
	if err := requireEnvScope(scope, "ssh CA delete requires environment scope and CA id", caID); err != nil {
		return err
	}
	now := store.CanonTime(s.now())
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpSSHCADelete, scope, now)
		if err != nil {
			return err
		}
		ca, err := r.SSH().GetCA(ctx, proof, caID)
		if err != nil {
			return err
		}
		live, err := r.SSH().CountLiveProfilesForCA(ctx, proof, caID)
		if err != nil {
			return err
		}
		if live > 0 {
			return ErrSSHCAHasProfiles
		}
		if err := r.SSH().DeleteCA(ctx, proof, caID, now); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventSSHCADeleted, caller.Principal, audit.Object{Type: "ssh-ca", ID: caID}, audit.Payload{"name": ca.Name})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, proof, ev)
	})
}

// ---- Profiles -----------------------------------------------------------------

// SSHProfileRequest creates or replaces a profile.
type SSHProfileRequest struct {
	CAID              string
	Name              string
	Principals        []string
	ForceCommand      string
	SourceAddresses   []string
	Extensions        []string
	KeyAlgorithms     []string
	DefaultTTLSeconds int64
	MaxTTLSeconds     int64
	Enabled           bool
	Requesters        []string
}

// normalizeProfile validates a request through internal/sshca and returns its
// canonical stored form (sorted, de-duplicated lists).
func normalizeProfile(req SSHProfileRequest) (store.SSHProfileWrite, error) {
	if err := validSSHName(req.Name); err != nil {
		return store.SSHProfileWrite{}, err
	}
	if req.CAID == "" {
		return store.SSHProfileWrite{}, fmt.Errorf("%w: a profile names its CA", domain.ErrInvalid)
	}
	prefixes, err := sshca.ParseSourceAddresses(req.SourceAddresses)
	if err != nil {
		return store.SSHProfileWrite{}, sshInvalid(err)
	}
	defaultTTL, err := sshSeconds("default_ttl_seconds", req.DefaultTTLSeconds, sshca.MaxTTL)
	if err != nil {
		return store.SSHProfileWrite{}, err
	}
	maxTTL, err := sshSeconds("max_ttl_seconds", req.MaxTTLSeconds, sshca.MaxTTL)
	if err != nil {
		return store.SSHProfileWrite{}, err
	}
	policy := sshca.Profile{
		Principals: req.Principals, ForceCommand: req.ForceCommand, SourceAddresses: prefixes,
		DefaultTTL: defaultTTL, MaxTTL: maxTTL,
	}
	for _, e := range req.Extensions {
		ext, err := sshca.ParseExtension(e)
		if err != nil {
			return store.SSHProfileWrite{}, sshInvalid(err)
		}
		if !slices.Contains(policy.Extensions, ext) {
			policy.Extensions = append(policy.Extensions, ext)
		}
	}
	for _, a := range req.KeyAlgorithms {
		alg, err := sshca.ParseAlgorithm(a)
		if err != nil {
			return store.SSHProfileWrite{}, sshInvalid(err)
		}
		if !slices.Contains(policy.KeyAlgorithms, alg) {
			policy.KeyAlgorithms = append(policy.KeyAlgorithms, alg)
		}
	}
	if err := sshca.ValidateProfile(policy); err != nil {
		return store.SSHProfileWrite{}, sshInvalid(err)
	}
	if len(req.Requesters) > sshMaxRequesters {
		return store.SSHProfileWrite{}, fmt.Errorf("%w: at most %d requesters", domain.ErrInvalid, sshMaxRequesters)
	}
	out := store.SSHProfileWrite{
		CAID: req.CAID, Name: req.Name, Principals: slices.Clone(req.Principals), ForceCommand: req.ForceCommand,
		DefaultTTLSeconds: req.DefaultTTLSeconds, MaxTTLSeconds: req.MaxTTLSeconds, Enabled: req.Enabled,
	}
	slices.Sort(out.Principals)
	for _, p := range prefixes {
		out.SourceAddresses = append(out.SourceAddresses, p.String())
	}
	slices.Sort(out.SourceAddresses)
	for _, e := range policy.Extensions {
		out.Extensions = append(out.Extensions, string(e))
	}
	slices.Sort(out.Extensions)
	for _, a := range policy.KeyAlgorithms {
		out.KeyAlgorithms = append(out.KeyAlgorithms, string(a))
	}
	slices.Sort(out.KeyAlgorithms)
	out.Requesters = slices.Clone(req.Requesters)
	slices.Sort(out.Requesters)
	out.Requesters = slices.Compact(out.Requesters)
	for _, r := range out.Requesters {
		if r == "" {
			return store.SSHProfileWrite{}, fmt.Errorf("%w: empty requester id", domain.ErrInvalid)
		}
	}
	return out, nil
}

// profilePolicy rebuilds the sshca policy from a stored profile.
func profilePolicy(p store.SSHProfile) (sshca.Profile, error) {
	prefixes, err := sshca.ParseSourceAddresses(p.SourceAddresses)
	if err != nil {
		return sshca.Profile{}, err
	}
	out := sshca.Profile{
		Principals: p.Principals, ForceCommand: p.ForceCommand, SourceAddresses: prefixes,
		DefaultTTL: time.Duration(p.DefaultTTLSeconds) * time.Second, MaxTTL: time.Duration(p.MaxTTLSeconds) * time.Second,
	}
	for _, e := range p.Extensions {
		out.Extensions = append(out.Extensions, sshca.Extension(e))
	}
	for _, a := range p.KeyAlgorithms {
		out.KeyAlgorithms = append(out.KeyAlgorithms, sshca.Algorithm(a))
	}
	return out, nil
}

func (s *SSH) CreateProfile(ctx context.Context, actor Actor, scope domain.Scope, req SSHProfileRequest) (SSHProfileView, error) {
	return s.writeProfile(ctx, actor, scope, "", req)
}

func (s *SSH) UpdateProfile(ctx context.Context, actor Actor, scope domain.Scope, profileID string, req SSHProfileRequest) (SSHProfileView, error) {
	if profileID == "" {
		return SSHProfileView{}, fmt.Errorf("%w: profile id required", domain.ErrInvalid)
	}
	return s.writeProfile(ctx, actor, scope, profileID, req)
}

func (s *SSH) writeProfile(ctx context.Context, actor Actor, scope domain.Scope, profileID string, req SSHProfileRequest) (SSHProfileView, error) {
	if err := requireEnvScope(scope, "ssh profile write requires environment scope"); err != nil {
		return SSHProfileView{}, err
	}
	write, err := normalizeProfile(req)
	if err != nil {
		return SSHProfileView{}, err
	}
	mutation := "update"
	if profileID == "" {
		mutation = "create"
		if profileID, err = newID("spf"); err != nil {
			return SSHProfileView{}, err
		}
	}
	write.ID = profileID
	now := store.CanonTime(s.now())
	write.At = now
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (SSHProfileView, error) {
		caller, proof, err := authorize(ctx, az, actor, authz.OpSSHProfileConfigure, scope, now)
		if err != nil {
			return SSHProfileView{}, err
		}
		for _, requester := range write.Requesters {
			if _, err := az.PrincipalClass(ctx, domain.PrincipalID(requester)); err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					return SSHProfileView{}, fmt.Errorf("%w: requester %s is not a principal", domain.ErrInvalid, requester)
				}
				return SSHProfileView{}, err
			}
		}
		if _, err := r.SSH().GetCA(ctx, proof, write.CAID); err != nil {
			return SSHProfileView{}, err
		}
		if mutation == "create" {
			err = r.SSH().CreateProfile(ctx, proof, write)
		} else {
			err = r.SSH().UpdateProfile(ctx, proof, write)
		}
		if err != nil {
			return SSHProfileView{}, err
		}
		ev, err := domainEvent(ctx, audit.EventSSHProfileConfigured, caller.Principal, audit.Object{Type: "ssh-profile", ID: profileID}, audit.Payload{
			"mutation": mutation, "enabled": write.Enabled, "requester_count": int64(len(write.Requesters)), "max_ttl_seconds": write.MaxTTLSeconds,
		})
		if err != nil {
			return SSHProfileView{}, err
		}
		if err := r.Audit().InsertTenant(ctx, proof, ev); err != nil {
			return SSHProfileView{}, err
		}
		if mutation == "update" {
			// Removing a requester withdraws its authority in this same
			// transaction: its live certificates enter the KRL now.
			revoked, err := r.SSH().RevokeProfileCertificates(ctx, proof, profileID, "authority-withdrawn", write.Requesters, false, now)
			if err != nil {
				return SSHProfileView{}, err
			}
			if err := auditRevocations(ctx, r, proof, caller.Principal, revoked, "authority-withdrawn"); err != nil {
				return SSHProfileView{}, err
			}
		}
		profile, err := r.SSH().GetProfile(ctx, proof, profileID)
		if err != nil {
			return SSHProfileView{}, err
		}
		return SSHProfileView{SSHProfile: profile}, nil
	})
}

func auditRevocations(ctx context.Context, r store.Repos, proof authz.Proof, principal domain.PrincipalID, revoked []store.SSHRevokedCertificate, reason string) error {
	for _, c := range revoked {
		ev, err := domainEvent(ctx, audit.EventSSHCertificateRevoked, principal, audit.Object{Type: "ssh-certificate", ID: c.ID}, audit.Payload{
			"serial": strconv.FormatInt(c.Serial, 10), "reason": reason,
		})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, proof, ev); err != nil {
			return err
		}
	}
	return nil
}

func (s *SSH) ListProfiles(ctx context.Context, actor Actor, scope domain.Scope) ([]SSHProfileView, error) {
	if err := requireEnvScope(scope, "ssh profile list requires environment scope"); err != nil {
		return nil, err
	}
	var out []SSHProfileView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpSSHProfileInspect, scope, s.now())
		if err != nil {
			return err
		}
		rows, err := r.SSH().ListProfiles(ctx, proof)
		if err != nil {
			return err
		}
		out = make([]SSHProfileView, 0, len(rows))
		for _, p := range rows {
			out = append(out, SSHProfileView{SSHProfile: p})
		}
		return nil
	})
	return out, err
}

func (s *SSH) GetProfile(ctx context.Context, actor Actor, scope domain.Scope, profileID string) (SSHProfileView, error) {
	if err := requireEnvScope(scope, "ssh profile show requires environment scope and profile id", profileID); err != nil {
		return SSHProfileView{}, err
	}
	var out SSHProfileView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpSSHProfileInspect, scope, s.now())
		if err != nil {
			return err
		}
		p, err := r.SSH().GetProfile(ctx, proof, profileID)
		out = SSHProfileView{SSHProfile: p}
		return err
	})
	return out, err
}

// DeleteProfile tombstones a profile (record deletion). With revokeIssued it
// also revokes every live certificate issued through it; without, those
// certificates stay valid until they expire. It returns the revoked count.
func (s *SSH) DeleteProfile(ctx context.Context, actor Actor, scope domain.Scope, profileID string, revokeIssued bool) (int, error) {
	if err := requireEnvScope(scope, "ssh profile delete requires environment scope and profile id", profileID); err != nil {
		return 0, err
	}
	now := store.CanonTime(s.now())
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (int, error) {
		caller, proof, err := authorize(ctx, az, actor, authz.OpSSHProfileDelete, scope, now)
		if err != nil {
			return 0, err
		}
		if _, err := r.SSH().GetProfile(ctx, proof, profileID); err != nil {
			return 0, err
		}
		var revoked []store.SSHRevokedCertificate
		if revokeIssued {
			if revoked, err = r.SSH().RevokeProfileCertificates(ctx, proof, profileID, "profile-deleted", nil, true, now); err != nil {
				return 0, err
			}
			if err := auditRevocations(ctx, r, proof, caller.Principal, revoked, "profile-deleted"); err != nil {
				return 0, err
			}
		}
		if err := r.SSH().DeleteProfile(ctx, proof, profileID, now); err != nil {
			return 0, err
		}
		ev, err := domainEvent(ctx, audit.EventSSHProfileDeleted, caller.Principal, audit.Object{Type: "ssh-profile", ID: profileID},
			audit.Payload{"revoked_certificate_count": int64(len(revoked))})
		if err != nil {
			return 0, err
		}
		return len(revoked), r.Audit().InsertTenant(ctx, proof, ev)
	})
}

// ---- Certificates ---------------------------------------------------------------

// IssueSSHCertificateRequest asks for one certificate. Exactly one of
// PublicKey (sign the caller's key) or a generated key (PublicKey empty,
// KeyAlgorithm naming the algorithm, default ed25519) applies. Extensions nil
// takes the profile's default; an empty non-nil slice asks for none.
type IssueSSHCertificateRequest struct {
	ProfileID       string
	PublicKey       string
	KeyAlgorithm    string
	Principals      []string
	SourceAddresses []string
	Extensions      []string
	TTLSeconds      int64
}

func (s *SSH) Issue(ctx context.Context, actor Actor, scope domain.Scope, req IssueSSHCertificateRequest) (SSHIssueResult, error) {
	if err := requireEnvScope(scope, "ssh certificate issue requires environment scope and profile id", req.ProfileID); err != nil {
		return SSHIssueResult{}, err
	}
	ttl, err := sshSeconds("ttl_seconds", req.TTLSeconds, sshca.MaxTTL)
	if err != nil {
		return SSHIssueResult{}, err
	}
	prefixes, err := sshca.ParseSourceAddresses(req.SourceAddresses)
	if err != nil {
		return SSHIssueResult{}, sshInvalid(err)
	}
	request := sshca.Request{
		Principals: req.Principals, SourceAddresses: prefixes, ExtensionsSet: req.Extensions != nil,
		TTL: ttl,
	}
	for _, e := range req.Extensions {
		ext, err := sshca.ParseExtension(e)
		if err != nil {
			return SSHIssueResult{}, sshInvalid(err)
		}
		request.Extensions = append(request.Extensions, ext)
	}
	release, err := chargeDefaultAtEntry(ctx, s.DB, s.Budget, actor, authz.OpSSHCertIssue, authz.OpSSHCertIssue, scope, s.now)
	if err != nil {
		return SSHIssueResult{}, err
	}
	defer release()

	// The user key is fixed before the transaction: parsed, or generated (the
	// RSA case is slow and must not hold a write transaction open).
	var (
		userPub     sshca.PublicKey
		userAlg     sshca.Algorithm
		userPrivate crypto.Signer
		keyOrigin   = "supplied"
	)
	if req.PublicKey != "" {
		if req.KeyAlgorithm != "" {
			return SSHIssueResult{}, fmt.Errorf("%w: key_algorithm applies only to a generated key", domain.ErrInvalid)
		}
		if userPub, userAlg, err = sshca.ParsePublicKey(req.PublicKey); err != nil {
			return SSHIssueResult{}, sshInvalid(err)
		}
	} else {
		keyOrigin = "generated"
		algorithm := req.KeyAlgorithm
		if algorithm == "" {
			algorithm = string(sshca.AlgorithmEd25519)
		}
		if userAlg, err = sshca.ParseAlgorithm(algorithm); err != nil {
			return SSHIssueResult{}, sshInvalid(err)
		}
		if userPrivate, err = sshca.GenerateKey(userAlg); err != nil {
			return SSHIssueResult{}, err
		}
		if userPub, err = sshca.PublicKeyOf(userPrivate); err != nil {
			return SSHIssueResult{}, err
		}
	}
	serial, err := sshca.NewSerial()
	if err != nil {
		return SSHIssueResult{}, err
	}
	certID, err := newID("scr")
	if err != nil {
		return SSHIssueResult{}, err
	}
	sealer, err := s.Keyring.ForProject(ctx, string(scope.Org), string(scope.Project))
	if err != nil {
		return SSHIssueResult{}, err
	}

	// One transaction authorizes, gates, re-reads the active key and the
	// enabled profile, signs, and records. The certificate leaves this
	// function only from the attempt whose commit succeeded: a node racing a
	// rotation, disable or requester removal elsewhere cannot release one.
	type issued struct {
		view SSHCertificateView
		text string
	}
	result, err := tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (issued, error) {
		now := store.CanonTime(s.now())
		caller, proof, err := authorize(ctx, az, actor, authz.OpSSHCertIssue, scope, now)
		if err != nil {
			return issued{}, err
		}
		listed, err := r.SSH().IsRequester(ctx, proof, req.ProfileID, string(caller.Principal))
		if err != nil {
			return issued{}, err
		}
		if !listed {
			return issued{}, domain.ErrNotFound
		}
		profile, err := r.SSH().GetProfile(ctx, proof, req.ProfileID)
		if err != nil {
			return issued{}, err
		}
		if profile.State != "enabled" {
			return issued{}, ErrSSHProfileDisabled
		}
		if err := s.issueCeremony(ctx, az, caller, scope, now); err != nil {
			return issued{}, err
		}
		policy, err := profilePolicy(profile)
		if err != nil {
			return issued{}, err
		}
		if err := sshca.CheckKeyAlgorithm(policy, userAlg); err != nil {
			return issued{}, sshInvalid(err)
		}
		resolved, err := sshca.Resolve(policy, request)
		if err != nil {
			return issued{}, sshInvalid(err)
		}
		active, err := r.SSH().ActiveCAKey(ctx, proof, profile.CAID)
		if err != nil {
			return issued{}, err
		}
		der, err := sealer.OpenField(sshCAKeyAAD(string(scope.Org), string(scope.Project), active.KeyID), active.Ciphertext)
		if err != nil {
			return issued{}, err
		}
		signer, _, err := sshca.SignerFromCAKey(der)
		hcrypto.Zero(der)
		if err != nil {
			return issued{}, err
		}
		validAfter := now.Add(-sshca.ClockSkew)
		validBefore := now.Add(resolved.TTL)
		keyID := "hikyo:" + string(scope.Env) + ":" + certID + ":" + string(caller.Principal)
		cert, err := sshca.Sign(signer, userPub, sshca.Template{
			Serial: serial, KeyID: keyID, Resolved: resolved, ValidAfter: validAfter, ValidBefore: validBefore,
		})
		if err != nil {
			return issued{}, err
		}
		fingerprint := sshca.Fingerprint(userPub)
		if err := r.SSH().InsertCertificate(ctx, proof, store.SSHCertificateCreate{
			ID: certID, CAID: profile.CAID, CAKeyID: active.KeyID, ProfileID: profile.ID, Serial: int64(serial), KeyID: keyID,
			Principals: resolved.Principals, PublicKeyFingerprint: fingerprint, KeyAlgorithm: string(userAlg), KeyOrigin: keyOrigin,
			ValidAfter: time.Unix(int64(cert.ValidAfter), 0), ValidBefore: time.Unix(int64(cert.ValidBefore), 0),
			RequesterPrincipalID: string(caller.Principal), RequesterClass: string(caller.Class), At: now,
		}); err != nil {
			return issued{}, err
		}
		ev, err := domainEvent(ctx, audit.EventSSHCertificateIssued, caller.Principal, audit.Object{Type: "ssh-certificate", ID: certID}, audit.Payload{
			"serial": strconv.FormatUint(serial, 10), "profile_id": profile.ID, "principals": resolved.Principals,
			"key_origin": keyOrigin, "key_fingerprint": fingerprint, "principal_class": string(caller.Class),
			"valid_before": time.Unix(int64(cert.ValidBefore), 0).UTC().Format(time.RFC3339),
		})
		if err != nil {
			return issued{}, err
		}
		if err := r.Audit().InsertTenant(ctx, proof, ev); err != nil {
			return issued{}, err
		}
		record, err := r.SSH().GetCertificate(ctx, proof, certID)
		if err != nil {
			return issued{}, err
		}
		return issued{view: certificateView(record, now), text: sshca.AuthorizedKey(cert, keyID)}, nil
	})
	if err != nil {
		return SSHIssueResult{}, err
	}
	out := SSHIssueResult{Certificate: result.view, CertificateText: result.text, PublicKey: sshca.AuthorizedKey(userPub, "")}
	if userPrivate != nil {
		if out.PrivateKey, err = sshca.MarshalUserPrivateKey(userPrivate, certID); err != nil {
			return SSHIssueResult{}, err
		}
	}
	return out, nil
}

// issueCeremony consumes a fresh keyless mint window for a human requester;
// machine requesters have no session and are gated by the requester list.
func (s *SSH) issueCeremony(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, scope domain.Scope, now time.Time) error {
	if domain.IsServiceAccountKind(caller.Class) {
		return nil
	}
	if s.Auth == nil {
		return errors.New("service: ssh certificates have no reauthentication seam wired")
	}
	intent, err := NewMintReauthIntent(string(scope.Env), nil)
	if err != nil {
		return err
	}
	if err := s.Auth.ConsumeReauthWindow(ctx, az, caller.SessionID, intent, now); err != nil {
		switch {
		case errors.Is(err, ErrNoReauthWindow), errors.Is(err, ErrReauthWindowExpired),
			errors.Is(err, ErrReauthUnitMismatch), errors.Is(err, ErrReauthWindowSpent):
			return fmt.Errorf("%w (ssh certificate issue)", ErrReauthRequired)
		default:
			return err
		}
	}
	return nil
}

func (s *SSH) ListCertificates(ctx context.Context, actor Actor, scope domain.Scope) ([]SSHCertificateView, error) {
	if err := requireEnvScope(scope, "ssh certificate list requires environment scope"); err != nil {
		return nil, err
	}
	now := s.now()
	var out []SSHCertificateView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpSSHCertInspect, scope, now)
		if err != nil {
			return err
		}
		rows, err := r.SSH().ListCertificates(ctx, proof, sshCertificateListLimit)
		if err != nil {
			return err
		}
		out = make([]SSHCertificateView, 0, len(rows))
		for _, c := range rows {
			out = append(out, certificateView(c, now))
		}
		return nil
	})
	return out, err
}

func (s *SSH) GetCertificate(ctx context.Context, actor Actor, scope domain.Scope, certID string) (SSHCertificateView, error) {
	if err := requireEnvScope(scope, "ssh certificate show requires environment scope and certificate id", certID); err != nil {
		return SSHCertificateView{}, err
	}
	now := s.now()
	var out SSHCertificateView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpSSHCertInspect, scope, now)
		if err != nil {
			return err
		}
		c, err := r.SSH().GetCertificate(ctx, proof, certID)
		out = certificateView(c, now)
		return err
	})
	return out, err
}

// RevokeCertificate adds a certificate to its CA's KRL. The requester may
// revoke its own certificate; anyone else needs manage-identities over the
// project. Revoking an already-revoked certificate is a no-op success.
func (s *SSH) RevokeCertificate(ctx context.Context, actor Actor, scope domain.Scope, certID string) (SSHCertificateView, error) {
	if err := requireEnvScope(scope, "ssh certificate revoke requires environment scope and certificate id", certID); err != nil {
		return SSHCertificateView{}, err
	}
	now := store.CanonTime(s.now())
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (SSHCertificateView, error) {
		caller, proof, err := authorize(ctx, az, actor, authz.OpSSHCertRevoke, scope, now)
		if err != nil {
			return SSHCertificateView{}, err
		}
		cert, err := r.SSH().GetCertificate(ctx, proof, certID)
		if err != nil {
			return SSHCertificateView{}, err
		}
		if cert.RequesterPrincipalID != string(caller.Principal) {
			grants, err := az.GrantRowsForPrincipal(ctx, caller.Principal)
			if err != nil {
				return SSHCertificateView{}, err
			}
			if !holds(grants, domain.CapManageIdentities, domain.Scope{Org: scope.Org, Project: scope.Project}) {
				return SSHCertificateView{}, domain.ErrNotFound
			}
		}
		changed, err := r.SSH().RevokeCertificate(ctx, proof, certID, "explicit", now)
		if err != nil {
			return SSHCertificateView{}, err
		}
		if changed {
			if err := auditRevocations(ctx, r, proof, caller.Principal, []store.SSHRevokedCertificate{{ID: cert.ID, Serial: cert.Serial}}, "explicit"); err != nil {
				return SSHCertificateView{}, err
			}
		}
		after, err := r.SSH().GetCertificate(ctx, proof, certID)
		if err != nil {
			return SSHCertificateView{}, err
		}
		return certificateView(after, now), nil
	})
}

// ---- Sweeper ------------------------------------------------------------------

// RunSweep re-decides every live certificate's requester authority and
// revokes, durably and with a system audit row, each whose requester was
// deleted, lost its grant, or was removed from the profile. A transient error
// never revokes: only a definite refusal does. It returns how many it revoked.
func (s *SSH) RunSweep(ctx context.Context) (int, error) {
	if s.Runtime == nil {
		return 0, errors.New("service: ssh sweeper requires its runtime")
	}
	revoked := 0
	cursor := ""
	// A row that fails is skipped, never revoked, and reported at the end: one
	// persistently failing certificate must not shield every later one.
	var rowErrs []error
	for {
		now := s.now()
		page, err := s.Runtime.ListLiveCertificates(ctx, now, cursor, sshSweepPage)
		if err != nil {
			return revoked, errors.Join(append(rowErrs, err)...)
		}
		for _, c := range page {
			cursor = c.ID
			changed, err := s.sweepOne(ctx, c)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return revoked, errors.Join(append(rowErrs, ctxErr)...)
				}
				rowErrs = append(rowErrs, fmt.Errorf("ssh sweep %s: %w", c.ID, err))
				continue
			}
			if changed {
				revoked++
			}
		}
		if len(page) < sshSweepPage {
			return revoked, errors.Join(rowErrs...)
		}
	}
}

// sweepOne revokes c if its requester's authority is definitively withdrawn.
func (s *SSH) sweepOne(ctx context.Context, c store.SSHSweepCandidate) (bool, error) {
	withdrawn := !c.RequesterListed
	if !withdrawn {
		var err error
		if withdrawn, err = s.authorityWithdrawn(ctx, c); err != nil || !withdrawn {
			return false, err
		}
	}
	return s.Runtime.RevokeForAuthority(ctx, c, store.CanonTime(s.now()))
}

func (s *SSH) authorityWithdrawn(ctx context.Context, c store.SSHSweepCandidate) (bool, error) {
	var withdrawn bool
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		caller := authz.Identity{Principal: domain.PrincipalID(c.RequesterPrincipalID), Class: domain.PrincipalClass(c.RequesterClass)}
		scope := domain.Scope{Org: domain.OrgID(c.OrgID), Project: domain.ProjectID(c.ProjectID), Env: domain.EnvID(c.EnvironmentID)}
		_, err := az.Authorize(ctx, caller, authz.OpSSHCertIssue, scope)
		switch {
		case err == nil:
			withdrawn = false
		case errors.Is(err, domain.ErrNotFound):
			withdrawn = true
		default:
			return err
		}
		return nil
	})
	return withdrawn, err
}

// SSHGauges is the /metrics source.
func (s *SSH) Gauges(ctx context.Context) (active, krlEntries int64, err error) {
	if s.Runtime == nil {
		return 0, 0, errors.New("service: ssh gauges require the runtime")
	}
	return s.Runtime.Gauges(ctx, s.now())
}
