package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/transit"
)

// The transit data plane (#156, transit ADR D2-D9). Every operation runs the
// same core, useKey: authorize crypto-use, charge the transit budget, lock and
// read the key, apply the key's own policy, run the operation at the custody
// provider, and record exactly one transit.operation event. A refusal after the
// formula passed is recorded through the durable capture path (it survives the
// rollback the refusal causes) and never names more than its closed cause.

// transitUse is the per-operation context handed to an operation body.
type transitUse struct {
	s       *Transit
	scope   domain.Scope
	r       store.Repos
	proof   authz.Proof
	key     store.TransitKeyRecord
	op      string
	refusal func(outcome audit.Outcome, cause string, version uint32, err error) error
}

// useResult is what an operation body reports for its audit record.
type useResult struct {
	version  uint32
	outBytes int
}

func (s *Transit) chargeTransit(ctx context.Context, charged *bool, principal domain.PrincipalID, org domain.OrgID, now time.Time) error {
	if *charged {
		return nil
	}
	if s.Shared != nil {
		window := now.UTC().Truncate(time.Minute)
		for _, bucket := range []struct {
			name, subject string
			limit         int64
		}{
			{"transit-principal", string(principal), BudgetTransitRatePerMin},
			{"transit-org", string(org), BudgetTransitOrgRatePerMin},
		} {
			n, err := s.Shared.BumpWindow(ctx, bucket.name, bucket.subject, window)
			if err != nil {
				// Fail closed: a limiter that cannot count admits nothing.
				return fmt.Errorf("%w: transit rate limiter unavailable", errTransitOverloaded)
			}
			if n > bucket.limit {
				return fmt.Errorf("%w: transit rate limit", errTransitOverloaded)
			}
		}
		*charged = true
		return nil
	}
	return s.Budget.chargeOnce(charged, budgetTransit, budgetKeys{Principal: principal, Org: org})
}

// useKey is the one data-plane core.
func (s *Transit) useKey(ctx context.Context, actor Actor, scope domain.Scope, name, op string, inBytes int,
	body func(ctx context.Context, u *transitUse) (useResult, error),
) error {
	if err := requireKeyAddress(scope, name); err != nil {
		return err
	}
	if s.Custody == nil {
		return ErrTransitCustodyUnavailable
	}
	charged := false
	now := store.CanonTime(s.now())
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		az.AttributeDenials(audit.Object{Type: "transit-key", ID: name})
		caller, proof, err := authorize(ctx, az, actor, authz.OpTransitUse, scope, now)
		if err != nil {
			return err
		}
		if err := s.chargeTransit(ctx, &charged, caller.Principal, scope.Org, now); err != nil {
			return err
		}
		key, err := r.Transit().GetKeyForUse(ctx, proof, name)
		if err != nil {
			return err
		}
		event := func(outcome audit.Outcome, version uint32, outBytes int, cause string) (audit.Event, error) {
			payload := audit.Payload{"operation": op, "input_bytes": int64(inBytes), "output_bytes": int64(outBytes)}
			if version > 0 {
				payload["key_version"] = int64(version)
			}
			if caller.CredentialID != "" {
				payload["credential_id"] = caller.CredentialID
			}
			if cause != "" {
				payload["refusal"] = cause
			}
			return newAuditEvent(ctx, audit.EventTransitOperation, caller.Principal,
				audit.Object{Type: "transit-key", ID: key.ID}, outcome, "", payload)
		}
		u := &transitUse{s: s, scope: scope, r: r, proof: proof, key: key, op: op}
		u.refusal = func(outcome audit.Outcome, cause string, version uint32, causeErr error) error {
			ev, err := event(outcome, version, 0, cause)
			if err != nil {
				return err
			}
			az.CaptureAudit(audit.TrailTenant, scope, ev)
			return causeErr
		}
		if !slices.Contains(key.AllowedOperations, op) {
			return u.refusal(audit.OutcomeDenied, "operation", 0, ErrTransitForbidden)
		}
		callers, err := r.Transit().ListCallers(ctx, proof, key.ID)
		if err != nil {
			return err
		}
		if len(callers) > 0 {
			i := slices.IndexFunc(callers, func(c store.TransitCaller) bool { return c.PrincipalID == string(caller.Principal) })
			if i < 0 || !slices.Contains(callers[i].Operations, op) {
				return u.refusal(audit.OutcomeDenied, "caller", 0, ErrTransitForbidden)
			}
		}
		switch {
		case key.State == TransitStateActive:
		case key.State == TransitStateRetired && !transitProducing[op]:
		default:
			return u.refusal(audit.OutcomeDenied, "state", 0, &transitRefusal{cause: "state"})
		}
		res, err := body(ctx, u)
		if err != nil {
			return err
		}
		ev, err := event(audit.OutcomeSuccess, res.version, res.outBytes, "")
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, proof, ev)
	})
}

// errTransitOverloaded is the transit rate refusal: the uniform 429 every
// budget refusal renders.
var errTransitOverloaded = transitOverloaded{}

type transitOverloaded struct{}

func (transitOverloaded) Error() string { return "service: transit rate budget exhausted" }
func (transitOverloaded) Unwrap() error { return admission.ErrOverloaded }

// producingVersion resolves the version a producing operation uses: the
// requested one, or the latest, inside [min_encrypt_version, latest] and above
// the compromise mark.
func (u *transitUse) producingVersion(requested uint32) (uint32, error) {
	v := requested
	if v == 0 {
		v = u.key.LatestVersion
	}
	if v < u.key.MinEncryptVersion || v > u.key.LatestVersion {
		return 0, u.refusal(audit.OutcomeDenied, "version", v, &transitRefusal{cause: "version"})
	}
	if v <= u.key.CompromisedThroughVersion {
		return 0, u.refusal(audit.OutcomeDenied, "compromised", v, &transitRefusal{cause: "compromised"})
	}
	return v, nil
}

// consumingVersion checks a version named by presented ciphertext, a
// signature or a MAC. verify and hmac-verify additionally refuse compromised
// versions: an attacker holding the material can forge them.
func (u *transitUse) consumingVersion(v uint32, forgeable bool) error {
	if v < u.key.MinDecryptVersion || v > u.key.LatestVersion {
		return u.refusal(audit.OutcomeDenied, "version", v, &transitRefusal{cause: "version"})
	}
	if forgeable && v <= u.key.CompromisedThroughVersion {
		return u.refusal(audit.OutcomeDenied, "compromised", v, &transitRefusal{cause: "compromised"})
	}
	return nil
}

// material loads one version and its custody provider. A version whose row is
// gone (trimmed, or created after a restored backup) or whose material the
// provider no longer holds is an explicit version refusal.
func (u *transitUse) material(ctx context.Context, v uint32) (transit.Custody, transit.Target, transit.Version, error) {
	fail := func(err error) (transit.Custody, transit.Target, transit.Version, error) {
		return nil, transit.Target{}, transit.Version{}, err
	}
	m, err := u.r.Transit().VersionMaterial(ctx, u.proof, u.key.ID, v)
	if errors.Is(err, store.ErrNotFound) || (err == nil && len(m.Sealed) == 0 && m.ExternalRef == "") {
		return fail(u.refusal(audit.OutcomeDenied, "version", v, &transitRefusal{cause: "version"}))
	}
	if err != nil {
		return fail(err)
	}
	t, err := transitTarget(u.scope, u.key, v, m.ID)
	if err != nil {
		return fail(err)
	}
	kind, err := transit.ParseCustodyKind(u.key.Custody)
	if err != nil {
		return fail(err)
	}
	p, err := u.s.Custody.Resolve(kind)
	if err == nil {
		err = p.Available(ctx)
	}
	if err != nil {
		return fail(u.refusal(audit.OutcomeFailure, "custody-unavailable", v, ErrTransitCustodyUnavailable))
	}
	return p, t, transitVersion(m), nil
}

// custodyErr maps a provider error inside an operation body.
func (u *transitUse) custodyErr(v uint32, err error) error {
	switch {
	case errors.Is(err, transit.ErrUnavailable):
		return u.refusal(audit.OutcomeFailure, "custody-unavailable", v, ErrTransitCustodyUnavailable)
	case errors.Is(err, transit.ErrMaterialMissing):
		return u.refusal(audit.OutcomeDenied, "version", v, &transitRefusal{cause: "version"})
	}
	return err
}

func (u *transitUse) badInput(v uint32, detail string) error {
	return u.refusal(audit.OutcomeFailure, "invalid-input", v, invalidTransit("%s", detail))
}

func checkTransitInput(what string, b []byte, bound int) error {
	if len(b) > bound {
		return invalidTransit("%s exceeds %d bytes", what, bound)
	}
	return nil
}

// Encrypt seals plaintext under the key (ADR D4). keyVersion 0 means latest.
func (s *Transit) Encrypt(ctx context.Context, actor Actor, scope domain.Scope, name string, plaintext, aad []byte, keyVersion uint32) (TransitOutput, error) {
	if err := checkTransitInput("plaintext", plaintext, crypto.MaxTransitPlaintextBytes); err != nil {
		return TransitOutput{}, err
	}
	if err := checkTransitInput("context", aad, crypto.MaxTransitContextBytes); err != nil {
		return TransitOutput{}, err
	}
	var out TransitOutput
	err := s.useKey(ctx, actor, scope, name, TransitOpEncrypt, len(plaintext), func(ctx context.Context, u *transitUse) (useResult, error) {
		v, err := u.producingVersion(keyVersion)
		if err != nil {
			return useResult{}, err
		}
		p, t, ver, err := u.material(ctx, v)
		if err != nil {
			return useResult{}, err
		}
		rec, err := p.Encrypt(ctx, t, ver, plaintext, aad)
		if err != nil {
			return useResult{}, u.custodyErr(v, err)
		}
		out = TransitOutput{KeyVersion: v, Value: crypto.FormatTransitValue(v, rec)}
		return useResult{version: v, outBytes: len(out.Value)}, nil
	})
	return out, err
}

// open parses and decrypts a presented ciphertext, applying the consuming
// version checks. The caller zeroes the plaintext.
func (u *transitUse) open(ctx context.Context, ciphertext string, aad []byte) ([]byte, uint32, error) {
	v, rec, err := crypto.ParseTransitCiphertext(ciphertext, u.key.ID)
	if err != nil {
		return nil, 0, u.badInput(0, "ciphertext is not a transit ciphertext of this key")
	}
	if err := u.consumingVersion(v, false); err != nil {
		return nil, v, err
	}
	p, t, ver, err := u.material(ctx, v)
	if err != nil {
		return nil, v, err
	}
	pt, err := p.Decrypt(ctx, t, ver, rec, aad)
	if errors.Is(err, crypto.ErrDecrypt) {
		return nil, v, u.badInput(v, "ciphertext does not open under this key, version and context")
	}
	if err != nil {
		return nil, v, u.custodyErr(v, err)
	}
	return pt, v, nil
}

// Decrypt opens a ciphertext (ADR D4). The plaintext is display-once: Hikyo
// neither stores nor logs it.
func (s *Transit) Decrypt(ctx context.Context, actor Actor, scope domain.Scope, name, ciphertext string, aad []byte) (TransitOutput, error) {
	if len(ciphertext) > crypto.MaxTransitWireBytes {
		return TransitOutput{}, invalidTransit("ciphertext exceeds %d bytes", crypto.MaxTransitWireBytes)
	}
	if err := checkTransitInput("context", aad, crypto.MaxTransitContextBytes); err != nil {
		return TransitOutput{}, err
	}
	var out TransitOutput
	err := s.useKey(ctx, actor, scope, name, TransitOpDecrypt, len(ciphertext), func(ctx context.Context, u *transitUse) (useResult, error) {
		pt, v, err := u.open(ctx, ciphertext, aad)
		if err != nil {
			return useResult{}, err
		}
		out = TransitOutput{KeyVersion: v, Plaintext: pt}
		return useResult{version: v, outBytes: len(pt)}, nil
	})
	if err != nil {
		crypto.Zero(out.Plaintext)
		return TransitOutput{}, err
	}
	return out, nil
}

// Rewrap moves a ciphertext onto the key's latest producing version without
// returning the plaintext (ADR D5). The audit record names the new version.
func (s *Transit) Rewrap(ctx context.Context, actor Actor, scope domain.Scope, name, ciphertext string, aad []byte) (TransitOutput, error) {
	if len(ciphertext) > crypto.MaxTransitWireBytes {
		return TransitOutput{}, invalidTransit("ciphertext exceeds %d bytes", crypto.MaxTransitWireBytes)
	}
	if err := checkTransitInput("context", aad, crypto.MaxTransitContextBytes); err != nil {
		return TransitOutput{}, err
	}
	var out TransitOutput
	err := s.useKey(ctx, actor, scope, name, TransitOpRewrap, len(ciphertext), func(ctx context.Context, u *transitUse) (useResult, error) {
		pt, _, err := u.open(ctx, ciphertext, aad)
		if err != nil {
			return useResult{}, err
		}
		defer crypto.Zero(pt)
		v, err := u.producingVersion(0)
		if err != nil {
			return useResult{}, err
		}
		p, t, ver, err := u.material(ctx, v)
		if err != nil {
			return useResult{}, err
		}
		rec, err := p.Encrypt(ctx, t, ver, pt, aad)
		if err != nil {
			return useResult{}, u.custodyErr(v, err)
		}
		out = TransitOutput{KeyVersion: v, Value: crypto.FormatTransitValue(v, rec)}
		return useResult{version: v, outBytes: len(out.Value)}, nil
	})
	return out, err
}

// DataKey draws a fresh data key and returns it wrapped under the key, plus
// the plaintext when reveal is set and the key allows datakey-plaintext (ADR
// D4). The plaintext is display-once.
func (s *Transit) DataKey(ctx context.Context, actor Actor, scope domain.Scope, name string, bits int, aad []byte, reveal bool) (TransitOutput, error) {
	if bits == 0 {
		bits = DefaultTransitDataKeyBits
	}
	if bits != 128 && bits != 256 && bits != 512 {
		return TransitOutput{}, invalidTransit("bits must be 128, 256 or 512")
	}
	if err := checkTransitInput("context", aad, crypto.MaxTransitContextBytes); err != nil {
		return TransitOutput{}, err
	}
	op := TransitOpDataKey
	if reveal {
		op = TransitOpDataKeyPlaintext
	}
	var out TransitOutput
	err := s.useKey(ctx, actor, scope, name, op, 0, func(ctx context.Context, u *transitUse) (useResult, error) {
		v, err := u.producingVersion(0)
		if err != nil {
			return useResult{}, err
		}
		p, t, ver, err := u.material(ctx, v)
		if err != nil {
			return useResult{}, err
		}
		dk, err := crypto.NewTransitDataKey(bits)
		if err != nil {
			return useResult{}, err
		}
		rec, err := p.Encrypt(ctx, t, ver, dk, aad)
		if err != nil {
			crypto.Zero(dk)
			return useResult{}, u.custodyErr(v, err)
		}
		out = TransitOutput{KeyVersion: v, Value: crypto.FormatTransitValue(v, rec)}
		if reveal {
			out.Plaintext = dk
		} else {
			crypto.Zero(dk)
		}
		return useResult{version: v, outBytes: len(out.Value) + len(out.Plaintext)}, nil
	})
	if err != nil {
		crypto.Zero(out.Plaintext)
		return TransitOutput{}, err
	}
	return out, nil
}

// Sign signs message with the key (ADR D4). keyVersion 0 means latest.
func (s *Transit) Sign(ctx context.Context, actor Actor, scope domain.Scope, name string, message []byte, keyVersion uint32) (TransitOutput, error) {
	if err := checkTransitInput("message", message, crypto.MaxTransitPlaintextBytes); err != nil {
		return TransitOutput{}, err
	}
	var out TransitOutput
	err := s.useKey(ctx, actor, scope, name, TransitOpSign, len(message), func(ctx context.Context, u *transitUse) (useResult, error) {
		v, err := u.producingVersion(keyVersion)
		if err != nil {
			return useResult{}, err
		}
		p, t, ver, err := u.material(ctx, v)
		if err != nil {
			return useResult{}, err
		}
		sig, err := p.Sign(ctx, t, ver, message)
		if err != nil {
			return useResult{}, u.custodyErr(v, err)
		}
		out = TransitOutput{KeyVersion: v, Value: crypto.FormatTransitValue(v, sig)}
		return useResult{version: v, outBytes: len(out.Value)}, nil
	})
	return out, err
}

// Verify checks a signature against the version's stored public key. It needs
// no custody provider: the public key is public metadata. An invalid signature
// is a successful operation answering Valid=false.
func (s *Transit) Verify(ctx context.Context, actor Actor, scope domain.Scope, name string, message []byte, signature string) (TransitOutput, error) {
	if err := checkTransitInput("message", message, crypto.MaxTransitPlaintextBytes); err != nil {
		return TransitOutput{}, err
	}
	if len(signature) > crypto.MaxTransitWireBytes {
		return TransitOutput{}, invalidTransit("signature exceeds %d bytes", crypto.MaxTransitWireBytes)
	}
	var out TransitOutput
	err := s.useKey(ctx, actor, scope, name, TransitOpVerify, len(message), func(ctx context.Context, u *transitUse) (useResult, error) {
		v, sig, err := crypto.ParseTransitValue(signature)
		if err != nil {
			return useResult{}, u.badInput(0, "signature is not a transit signature")
		}
		if err := u.consumingVersion(v, true); err != nil {
			return useResult{}, err
		}
		m, err := u.r.Transit().VersionMaterial(ctx, u.proof, u.key.ID, v)
		if errors.Is(err, store.ErrNotFound) || (err == nil && len(m.PublicKey) == 0) {
			return useResult{}, u.refusal(audit.OutcomeDenied, "version", v, &transitRefusal{cause: "version"})
		}
		if err != nil {
			return useResult{}, err
		}
		out = TransitOutput{KeyVersion: v, Valid: crypto.VerifyTransitSignature(m.PublicKey, message, sig)}
		return useResult{version: v}, nil
	})
	return out, err
}

// HMAC computes a MAC with the key (ADR D4). keyVersion 0 means latest.
func (s *Transit) HMAC(ctx context.Context, actor Actor, scope domain.Scope, name string, message []byte, keyVersion uint32) (TransitOutput, error) {
	if err := checkTransitInput("message", message, crypto.MaxTransitPlaintextBytes); err != nil {
		return TransitOutput{}, err
	}
	var out TransitOutput
	err := s.useKey(ctx, actor, scope, name, TransitOpHMAC, len(message), func(ctx context.Context, u *transitUse) (useResult, error) {
		v, err := u.producingVersion(keyVersion)
		if err != nil {
			return useResult{}, err
		}
		p, t, ver, err := u.material(ctx, v)
		if err != nil {
			return useResult{}, err
		}
		mac, err := p.MAC(ctx, t, ver, message)
		if err != nil {
			return useResult{}, u.custodyErr(v, err)
		}
		out = TransitOutput{KeyVersion: v, Value: crypto.FormatTransitValue(v, mac)}
		return useResult{version: v, outBytes: len(out.Value)}, nil
	})
	return out, err
}

// VerifyHMAC recomputes the MAC and compares in constant time. A mismatch is a
// successful operation answering Valid=false.
func (s *Transit) VerifyHMAC(ctx context.Context, actor Actor, scope domain.Scope, name string, message []byte, mac string) (TransitOutput, error) {
	if err := checkTransitInput("message", message, crypto.MaxTransitPlaintextBytes); err != nil {
		return TransitOutput{}, err
	}
	if len(mac) > crypto.MaxTransitWireBytes {
		return TransitOutput{}, invalidTransit("mac exceeds %d bytes", crypto.MaxTransitWireBytes)
	}
	var out TransitOutput
	err := s.useKey(ctx, actor, scope, name, TransitOpHMACVerify, len(message), func(ctx context.Context, u *transitUse) (useResult, error) {
		v, presented, err := crypto.ParseTransitValue(mac)
		if err != nil {
			return useResult{}, u.badInput(0, "mac is not a transit mac")
		}
		if err := u.consumingVersion(v, true); err != nil {
			return useResult{}, err
		}
		p, t, ver, err := u.material(ctx, v)
		if err != nil {
			return useResult{}, err
		}
		computed, err := p.MAC(ctx, t, ver, message)
		if err != nil {
			return useResult{}, u.custodyErr(v, err)
		}
		out = TransitOutput{KeyVersion: v, Valid: crypto.EqualTransitMAC(computed, presented)}
		return useResult{version: v}, nil
	})
	return out, err
}

// ---- Scheduler -----------------------------------------------------------

// RotateDue appends a version to every active key whose rotation period has
// elapsed since its latest version (ADR D5). It runs under scheduler authority
// and records a rotation event with actor class system. A key whose external
// custody is unavailable is skipped and retried next run; it keeps serving
// under its current version, which is the documented fail-closed direction
// (no version is invented, no custody substituted).
func (s *Transit) RotateDue(ctx context.Context) (rotated int, err error) {
	now := s.now()
	return s.sweepDue(ctx, func(ctx context.Context, r store.Repos, p authz.Proof, after string) ([]store.TransitDueKey, error) {
		return r.Transit().SelectRotationDue(ctx, p, after, transitSweepBatch)
	}, func(d store.TransitDueKey) (bool, error) {
		if !transitRotationDue(d.TransitKeyRecord, d.LatestCreatedAt, now) {
			return false, nil
		}
		scope := domain.Scope{Org: domain.OrgID(d.OrgID), Project: domain.ProjectID(d.ProjectID), Env: domain.EnvID(d.EnvironmentID)}
		_, err := s.appendVersion(ctx, scope, d.TransitKeyRecord, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, _ time.Time) (authz.Proof, domain.PrincipalID, error) {
			p, err := az.ScopedSystemAuthority(ctx, authz.SiteScheduler, scope)
			return p, "", err
		}, "schedule")
		return err == nil, err
	})
}

// sweepDue walks every page of a scheduler read, keyset-paged by key id, so a
// page of keys that are not due or keep failing never hides the keys after it.
// act reports whether it changed the key; its errors are collected per key and
// the sweep continues.
func (s *Transit) sweepDue(ctx context.Context, read func(context.Context, store.Repos, authz.Proof, string) ([]store.TransitDueKey, error), act func(store.TransitDueKey) (bool, error)) (int, error) {
	var (
		done  int
		errs  []error
		after string
	)
	for {
		var page []store.TransitDueKey
		err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
			if err != nil {
				return err
			}
			page, err = read(ctx, r, p, after)
			return err
		})
		if err != nil {
			return done, errors.Join(append(errs, err)...)
		}
		for _, d := range page {
			changed, err := act(d)
			if err != nil {
				errs = append(errs, fmt.Errorf("transit key %s: %w", d.ID, err))
				continue
			}
			if changed {
				done++
			}
		}
		if len(page) < transitSweepBatch {
			return done, errors.Join(errs...)
		}
		after = page[len(page)-1].ID
	}
}

// PurgeDue destroys every key whose deletion delay has elapsed (ADR D6):
// external material is destroyed at its provider first, then every version's
// material is erased and the key tombstoned in one transaction. An unavailable
// external provider leaves the key pending-deletion (unusable either way) for
// the next run.
func (s *Transit) PurgeDue(ctx context.Context) (destroyed int, err error) {
	now := store.CanonTime(s.now())
	return s.sweepDue(ctx, func(ctx context.Context, r store.Repos, p authz.Proof, after string) ([]store.TransitDueKey, error) {
		return r.Transit().SelectDeletionDue(ctx, p, now, after, transitSweepBatch)
	}, func(d store.TransitDueKey) (bool, error) {
		scope := domain.Scope{Org: domain.OrgID(d.OrgID), Project: domain.ProjectID(d.ProjectID), Env: domain.EnvID(d.EnvironmentID)}
		err := s.purgeOne(ctx, scope, d.TransitKeyRecord, now)
		return err == nil, err
	})
}

func (s *Transit) purgeOne(ctx context.Context, scope domain.Scope, k store.TransitKeyRecord, now time.Time) error {
	var external []store.TransitVersionMaterial
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.ScopedSystemAuthority(ctx, authz.SiteScheduler, scope)
		if err != nil {
			return err
		}
		external, err = r.Transit().DestroyVersions(ctx, p, k.ID, now)
		return err
	})
	if err != nil {
		return err
	}
	if len(external) > 0 {
		p, err := s.Custody.Resolve(transit.CustodyExternal)
		if err != nil {
			return ErrTransitCustodyUnavailable
		}
		for _, m := range external {
			t, err := transitTarget(scope, k, m.Version, m.ID)
			if err != nil {
				return err
			}
			if err := p.Destroy(ctx, t, transitVersion(m)); err != nil {
				return err
			}
		}
	}
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.ScopedSystemAuthority(ctx, authz.SiteScheduler, scope)
		if err != nil {
			return err
		}
		erased, err := r.Transit().Destroy(ctx, p, k.ID, now)
		if err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventTransitKeyDestroyed, "",
			audit.Object{Type: "transit-key", ID: k.ID}, audit.Payload{
				"custody": k.Custody, "versions_erased": erased,
			})
		if err != nil {
			return err
		}
		ev.Actor.Class = audit.ActorSystem
		ev.OccurredAt = now
		return r.Audit().InsertTenant(ctx, p, ev)
	})
}
