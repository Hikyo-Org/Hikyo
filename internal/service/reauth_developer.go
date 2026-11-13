package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

const PurposeDeveloperCredential ReauthPurpose = "developer-credential"

type DeveloperCredentialReauthTarget struct {
	Lifetime                time.Duration `json:"lifetime"`
	ConsentCurrentAndFuture bool          `json:"consent_current_and_future"`
	// PersonalValues binds the personal-value consent into the ceremony, so
	// an approval never covers a delivery the person was not shown.
	PersonalValues bool `json:"personal_values,omitzero"`
}

// developerCredentialReauthTargetWire keeps the v1 encoding of Lifetime: a
// nanosecond integer. encoding/json/v2 has no default for time.Duration.
type developerCredentialReauthTargetWire struct {
	Lifetime                int64 `json:"lifetime"`
	ConsentCurrentAndFuture bool  `json:"consent_current_and_future"`
	PersonalValues          bool  `json:"personal_values,omitzero"`
}

func (t DeveloperCredentialReauthTarget) MarshalJSON() ([]byte, error) {
	return json.Marshal(developerCredentialReauthTargetWire{int64(t.Lifetime), t.ConsentCurrentAndFuture, t.PersonalValues}, jsonwire.Stable)
}

func (t *DeveloperCredentialReauthTarget) UnmarshalJSON(raw []byte) error {
	var w developerCredentialReauthTargetWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	*t = DeveloperCredentialReauthTarget{time.Duration(w.Lifetime), w.ConsentCurrentAndFuture, w.PersonalValues}
	return nil
}

type developerCredentialBinding struct {
	Purpose       string                          `json:"purpose"`
	EnvironmentID string                          `json:"environment_id"`
	KeyIDs        []string                        `json:"key_ids"`
	Target        DeveloperCredentialReauthTarget `json:"target"`
}

// NewPersonalDeveloperCredentialReauthIntent also binds whether the credential
// will deliver the minting person's personal values.
func NewPersonalDeveloperCredentialReauthIntent(environmentID string, keyIDs []string, ttl time.Duration, consent, personal bool) (ReauthIntent, error) {
	if len(keyIDs) > config.MaxKeysPerProjectCeiling || ttl <= 0 || ttl > 8*time.Hour || ttl%time.Second != 0 || !consent {
		return ReauthIntent{}, domain.ErrInvalid
	}
	intent, err := newDisclosureReauthIntent(intentDeveloperCredential, []string{environmentID}, canonicalSet(keyIDs))
	if err != nil {
		return ReauthIntent{}, err
	}
	raw, err := json.Marshal(developerCredentialBinding{Purpose: string(PurposeDeveloperCredential), EnvironmentID: environmentID, KeyIDs: intent.keyIDs, Target: DeveloperCredentialReauthTarget{ttl, consent, personal}}, jsonwire.Stable)
	if err != nil {
		return ReauthIntent{}, err
	}
	digest := sha256.Sum256(raw)
	intent.developerBinding = string(raw)
	intent.keySet = hex.EncodeToString(digest[:])
	return intent, nil
}
func (i ReauthIntent) isDeveloperCredential() bool { return i.variant == intentDeveloperCredential }
func parseDeveloperCredentialBinding(raw string) (ReauthIntent, error) {
	var b developerCredentialBinding
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return ReauthIntent{}, err
	}
	if b.Purpose != string(PurposeDeveloperCredential) {
		return ReauthIntent{}, ErrReauthUnitMismatch
	}
	intent, err := NewPersonalDeveloperCredentialReauthIntent(b.EnvironmentID, b.KeyIDs, b.Target.Lifetime, b.Target.ConsentCurrentAndFuture, b.Target.PersonalValues)
	if err != nil || intent.developerBinding != raw {
		return ReauthIntent{}, ErrReauthUnitMismatch
	}
	return intent, nil
}
func authorizeDeveloperCredentialCeremony(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, envID string) error {
	if caller.Class != domain.ClassHuman || caller.SessionID == "" || caller.Artifact == authz.WorkspaceArtifact {
		return domain.ErrNotFound
	}
	return authorizeEnvironmentRead(ctx, az, caller, envID)
}
func (s *Auth) ConsumeDeveloperCredentialReauth(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, intent ReauthIntent, now time.Time) error {
	if !intent.isDeveloperCredential() || caller.SessionID == "" {
		return ErrReauthUnitMismatch
	}
	binding, err := intent.bindingFor("")
	if err != nil {
		return err
	}
	return s.consumeReauthWindow(ctx, az, caller.SessionID, binding, now)
}

func tryDeveloperCredentialBinding(raw string) (ReauthIntent, bool, error) {
	if raw == "" {
		return ReauthIntent{}, false, nil
	}
	var b developerCredentialBinding
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return ReauthIntent{}, false, err
	}
	if b.Purpose != string(PurposeDeveloperCredential) {
		return ReauthIntent{}, false, nil
	}
	i, err := parseDeveloperCredentialBinding(raw)
	return i, true, err
}

// A dedicated decision retains the environment's passkey requirement at both
// opening and consumption, including policy changes between those transactions.
func (s *Auth) requireDeveloperTOTPWindow(ctx context.Context, az *authz.TxAuthorizer, environmentID string) error {
	window, err := s.effectiveReauthWindow(ctx, az, environmentID)
	if err != nil {
		return err
	}
	if window <= 0 {
		return ErrReauthWindowClosed
	}
	return nil
}
