package service

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// recordDeveloperRevocations attributes each terminal invalidation to the
// security mutation's actor and preserves the human who delegated its access.
// The caller records these events in the same transaction as the mutation.
func recordDeveloperRevocations(ctx context.Context, az *authz.TxAuthorizer, actor domain.PrincipalID, cause string, rows []authz.DeveloperCredential) error {
	for _, credential := range rows {
		event, err := developerRevocationEvent(ctx, actor, credential, cause)
		if err != nil {
			return err
		}
		if actor == "" {
			event.Actor.Class = audit.ActorBreakGlass
			event.Origin = audit.OriginCLI
		}
		event.AuthorityID = string(credential.AuthorityPrincipal)
		if err := az.RecordAuthEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func revokeDeveloperProvenance(ctx context.Context, az *authz.TxAuthorizer, actor domain.PrincipalID, kind, id string, now time.Time) error {
	rows, err := az.RevokeDeveloperCredentialsByProvenance(ctx, kind, id, now)
	if err != nil {
		return err
	}
	return recordDeveloperRevocations(ctx, az, actor, kind+"-security-revoked", rows)
}

func revokeDeveloperScope(ctx context.Context, az *authz.TxAuthorizer, actor, authority domain.PrincipalID, scope domain.Scope, cause string, now time.Time) error {
	rows, err := az.RevokeDeveloperCredentials(ctx, authority, scope, "", now)
	if err != nil {
		return err
	}
	return recordDeveloperRevocations(ctx, az, actor, cause, rows)
}
