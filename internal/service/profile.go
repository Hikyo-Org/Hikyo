package service

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// AccountProfile is the authenticated holder's profile. Username and display
// name are editable (ProfileUpdate); Email is the verified login email, nil
// when none, written only by verified local sign-up (#608) and never here. It
// is never a recovery or linking identifier.
type AccountProfile struct {
	Username         string
	DisplayName      string
	Email            *string
	Managed          bool
	UsernameEditable bool
}

// ProfileUpdate is everything the profile surface may change. It carries no
// email by construction.
type ProfileUpdate struct {
	Username    string
	DisplayName string
}

func (s *Auth) MyProfile(ctx context.Context, presented string) (AccountProfile, error) {
	var out AccountProfile
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		id, err := az.Authenticate(ctx, presented, s.now())
		if err != nil {
			return err
		}
		account, err := az.AccountByPrincipal(ctx, id.Principal)
		if err != nil {
			return err
		}
		profile, err := az.AccountProfile(ctx, account.ID)
		if err != nil {
			return err
		}
		out = AccountProfile(profile)
		return nil
	})
	return out, err
}

func validateAccountProfile(profile ProfileUpdate) error {
	for label, value := range map[string]string{"username": profile.Username, "display name": profile.DisplayName} {
		if !utf8.ValidString(value) || strings.TrimSpace(value) != value || len(value) > 256 {
			return fmt.Errorf("%w: %s must be valid text with no surrounding whitespace and at most 256 bytes", domain.ErrInvalid, label)
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return fmt.Errorf("%w: %s contains a control character", domain.ErrInvalid, label)
			}
		}
	}
	if profile.Username == "" {
		return fmt.Errorf("%w: username is required", domain.ErrInvalid)
	}
	return nil
}

// UpdateMyProfile requires fresh proof for username changes and preserves sessions: no credential,
// assurance, immutable principal, or grant changes. Provider subject keys and
// SCIM-owned names are never rewritten through this surface.
func (s *Auth) UpdateMyProfile(ctx context.Context, presented string, profile ProfileUpdate, proof string) (AccountProfile, error) {
	if err := validateAccountProfile(profile); err != nil {
		return AccountProfile{}, err
	}
	// Resolve self before the proof helper's host-local empty-bearer exemption.
	before, err := s.MyProfile(ctx, presented)
	if err != nil {
		return AccountProfile{}, err
	}
	var evidence ReauthEvidence
	hasEvidence := before.Username != profile.Username
	if hasEvidence {
		evidence, err = s.VerifyReauthProof(ctx, presented, proof)
		if err != nil {
			if err == ErrReauthProofRequired {
				err = domain.ErrUnauthenticated
			}
			return AccountProfile{}, err
		}
	}
	var out AccountProfile
	err = tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		id, err := az.Authenticate(ctx, presented, s.now())
		if err != nil {
			return err
		}
		if err := az.LockTargetPrincipal(ctx, id.Principal); err != nil {
			return err
		}
		account, err := az.AccountByPrincipal(ctx, id.Principal)
		if err != nil {
			return err
		}
		current, err := az.AccountProfile(ctx, account.ID)
		if err != nil {
			return err
		}
		if current.Managed && (current.Username != profile.Username || current.DisplayName != profile.DisplayName) {
			return domain.ErrUnauthorized
		}
		if current.Username != profile.Username {
			if !hasEvidence || !current.UsernameEditable {
				return domain.ErrUnauthenticated
			}
			if err := s.ConsumeReauthEvidence(ctx, az, evidence, id.Principal); err != nil {
				return err
			}
		}
		next := authz.AccountProfile{Username: profile.Username, DisplayName: profile.DisplayName, Email: current.Email, Managed: current.Managed, UsernameEditable: current.UsernameEditable}
		if err := az.UpdateAccountProfile(ctx, account.ID, next); err != nil {
			return err
		}
		event, err := newAuditEvent(ctx, audit.EventAuthProfileUpdated, id.Principal, audit.Object{Type: "account", ID: account.ID}, audit.OutcomeSuccess, "", audit.Payload{"account_id": account.ID})
		if err != nil {
			return err
		}
		if err := az.RecordAuthEvent(ctx, event); err != nil {
			return err
		}
		out = AccountProfile(next)
		return nil
	})
	return out, err
}
