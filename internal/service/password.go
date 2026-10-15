package service

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// SetPassword sets the first password or replaces the existing one. Policy
// validation precedes credential work, and every session and window is retired
// atomically with the write. The acting session receives only the proof class.
func (s *Auth) SetPassword(ctx context.Context, presented, password, proof string) (LoginResult, error) {
	if err := CheckPassword(password); err != nil {
		return LoginResult{}, err
	}
	account, evidence, err := s.verifyIdentityProof(ctx, presented, proof)
	if errors.Is(err, ErrNoProofCredential) && proof == "" {
		account, evidence, err = s.verifyLocalCreationProof(ctx, presented, "", "")
	}
	if err != nil {
		return LoginResult{}, err
	}
	var existing authz.PasswordCredential
	var first bool
	err = tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		var err error
		existing, err = az.PasswordCredentialFor(ctx, account.ID)
		if errors.Is(err, domain.ErrNotFound) {
			first = true
			return nil
		}
		return err
	})
	if err != nil {
		return LoginResult{}, err
	}
	release, err := s.enterFactorBudget(ctx, account.ID)
	if err != nil {
		return LoginResult{}, err
	}
	defer release()
	sealed, params, dekVersion, err := s.sealVerifier(account.ID, password)
	if err != nil {
		return LoginResult{}, err
	}
	return writeCommittedLoginResult(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, result *LoginResult) error {
		now := s.now()
		live, err := az.Authenticate(ctx, presented, now)
		if err != nil {
			return err
		}
		if live.Principal != account.PrincipalID {
			return domain.ErrUnauthenticated
		}
		if evidence.kind != reauthEvidenceEstablish {
			if err := az.LockTargetPrincipal(ctx, live.Principal); err != nil {
				return err
			}
		}
		if err := s.consumeLocalCreationProof(ctx, az, evidence, live, account.ID); err != nil {
			return err
		}
		current, err := az.PasswordCredentialFor(ctx, account.ID)
		if first {
			if err == nil {
				return ErrCredentialRace
			}
			if !errors.Is(err, domain.ErrNotFound) {
				return err
			}
		} else {
			if err != nil {
				return err
			}
			if current.RowVersion != existing.RowVersion {
				return ErrCredentialRace
			}
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if err := az.AssertActiveInstanceDEKVersion(ctx, dekVersion); err != nil {
			return err
		}
		cred := authz.PasswordCredential{AccountID: account.ID, Verifier: sealed, KDF: params, DEKVersion: dekVersion, CredentialEpoch: epoch, RowVersion: existing.RowVersion}
		if first {
			if err := az.WritePasswordCredential(ctx, cred, now); err != nil {
				return err
			}
		} else {
			ok, err := az.ReplacePasswordCredential(ctx, cred, now)
			if err != nil {
				return err
			}
			if !ok {
				return ErrCredentialRace
			}
		}
		if err := revokeDeveloperScope(ctx, az, account.PrincipalID, account.PrincipalID, domain.Scope{}, "password-changed", now); err != nil {
			return err
		}
		*result, err = s.reissueAccountSecurity(ctx, az, account, evidence.factorClass(), live, now)
		if err != nil {
			return err
		}
		event, err := newAuditEvent(ctx, audit.EventAuthPasswordChanged, account.PrincipalID, audit.Object{Type: "account", ID: account.ID}, audit.OutcomeSuccess, "", audit.Payload{"account_id": account.ID, "first": first, "authorizing_credential": evidence.factorClass()})
		if err != nil {
			return err
		}
		return az.RecordAuthEvent(ctx, event)
	})
}
