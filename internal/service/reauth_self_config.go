package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// SelfConfigReauthTarget is an exact administrative decision, never a disclosure
// window. Empty fields are canonical and required so no omitted field broadens it.
type SelfConfigReauthTarget struct {
	Action                     string `json:"action"`
	OwnerInstanceID            string `json:"owner_instance_id"`
	Revision                   int64  `json:"revision"`
	SchemaVersion              int    `json:"schema_version"`
	ExpectedGeneration         int64  `json:"expected_generation"`
	PreviewToken               string `json:"preview_token"`
	To                         string `json:"to"`
	ConfirmRestoredCredentials bool   `json:"confirm_restored_credentials"`
}

type selfConfigChallenge struct {
	Purpose       string                 `json:"purpose"`
	EnvironmentID string                 `json:"environment_id"`
	Target        SelfConfigReauthTarget `json:"self_config"`
}

func NewSelfConfigReauthIntent(target SelfConfigReauthTarget) (ReauthIntent, error) {
	if target.OwnerInstanceID == "" || strings.ContainsAny(target.OwnerInstanceID, "\r\n") || target.SchemaVersion < 1 || target.ExpectedGeneration < 0 {
		return ReauthIntent{}, domain.ErrInvalid
	}
	var variant reauthIntentVariant
	switch target.Action {
	case "adopt":
		if target.PreviewToken == "" || target.Revision != 0 || target.ExpectedGeneration != 0 || target.To != "" || target.ConfirmRestoredCredentials {
			return ReauthIntent{}, domain.ErrInvalid
		}
		variant = intentSelfConfigAdopt
	case "apply":
		if target.Revision < 1 || target.PreviewToken != "" || target.To != "" {
			return ReauthIntent{}, domain.ErrInvalid
		}
		variant = intentSelfConfigApply
	case "mail-test":
		if target.Revision < 1 || target.PreviewToken != "" || target.To == "" || target.ConfirmRestoredCredentials {
			return ReauthIntent{}, domain.ErrInvalid
		}
		variant = intentSelfConfigTest
	default:
		return ReauthIntent{}, domain.ErrInvalid
	}
	slot := "instance:" + target.OwnerInstanceID
	raw, err := json.Marshal(selfConfigChallenge{Purpose: "self-config", EnvironmentID: slot, Target: target})
	if err != nil {
		return ReauthIntent{}, err
	}
	digest := sha256.Sum256(raw)
	return ReauthIntent{variant: variant, environmentID: slot, environmentIDs: []string{slot}, environmentSet: slot, keySet: hex.EncodeToString(digest[:]), selfConfigBinding: string(raw)}, nil
}

func (i ReauthIntent) isSelfConfig() bool {
	return i.variant == intentSelfConfigAdopt || i.variant == intentSelfConfigApply || i.variant == intentSelfConfigTest
}

func parseSelfConfigBinding(raw string) (ReauthIntent, bool, error) {
	var value selfConfigChallenge
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return ReauthIntent{}, false, err
	}
	if value.Purpose != "self-config" {
		return ReauthIntent{}, false, nil
	}
	intent, err := NewSelfConfigReauthIntent(value.Target)
	if err != nil || intent.selfConfigBinding != raw || intent.environmentID != value.EnvironmentID {
		return ReauthIntent{}, false, ErrReauthUnitMismatch
	}
	return intent, true, nil
}

func authorizeSelfConfigCeremony(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, slot string) error {
	owner, err := az.InstanceIdentity(ctx)
	if err != nil {
		return err
	}
	if slot != "instance:"+owner {
		return domain.ErrNotFound
	}
	_, err = az.Authorize(ctx, caller, authz.OpSelfConfigStatus, domain.Scope{})
	return err
}

// ConsumeSelfConfigReauth spends only a fresh, exact single-decision window in
// the action's final transaction. It cannot consume disclosure or sliding gates.
func (s *Auth) ConsumeSelfConfigReauth(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, intent ReauthIntent, now time.Time) error {
	if !intent.isSelfConfig() || caller.SessionID == "" {
		return ErrReauthUnitMismatch
	}
	if err := authorizeSelfConfigCeremony(ctx, az, caller, intent.environmentID); err != nil {
		return err
	}
	binding, err := intent.bindingFor("")
	if err != nil {
		return err
	}
	return s.consumeReauthWindow(ctx, az, caller.SessionID, binding, now)
}
