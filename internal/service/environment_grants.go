package service

import (
	"context"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// releaseEnvironmentGrants is part of authorized environment deletion, not an
// independent grant-revocation surface. An environment's scoped authority ends
// with the environment, including every origin that held that authority alive.
// Project, organization and sibling-environment grants remain untouched.
func releaseEnvironmentGrants(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, actor domain.PrincipalID, scope domain.Scope) error {
	lines, err := az.GrantLinesInProject(ctx, string(scope.Org), string(scope.Project))
	if err != nil {
		return err
	}
	var scoped []authz.GrantLine
	var principals []domain.PrincipalID
	for _, line := range lines {
		if line.Grant.Scope != scope {
			continue
		}
		scoped = append(scoped, line)
		principals = append(principals, line.Principal)
	}
	slices.Sort(principals)
	principals = slices.Compact(principals)
	for _, principal := range principals {
		if err := az.LockTargetPrincipal(ctx, principal); err != nil {
			return err
		}
	}
	var changedPrincipals []domain.PrincipalID
	for _, line := range scoped {
		// Re-read origins after acquiring the principal lock: another grant
		// writer may have attached an origin since the initial census.
		origins, err := az.GrantOriginsFor(ctx, line.ID)
		if err != nil {
			return err
		}
		var releasedKinds []string
		for _, origin := range origins {
			released, err := az.ReleaseGrantOrigin(ctx, line.ID, line.Principal, origin)
			if err != nil {
				return err
			}
			if released {
				releasedKinds = append(releasedKinds, string(origin.Kind))
			}
		}
		deleted, err := az.DeleteGrantRow(ctx, line.ID, line.Principal)
		if err != nil {
			return err
		}
		if !deleted {
			continue
		}
		changedPrincipals = append(changedPrincipals, line.Principal)
		slices.Sort(releasedKinds)
		releasedKinds = slices.Compact(releasedKinds)
		if err := insertGrantEvent(ctx, r, p, actor, domain.LevelEnv, grantEventInput{
			typ:     audit.EventGrantRevoked,
			object:  audit.Object{Type: "grant", ID: line.ID},
			payload: revokePayload(GrantSpec{Target: line.Principal, Capability: line.Grant.Capability, Scope: scope}, actor, releasedKinds, 0, audit.EventGrantRevoked),
		}); err != nil {
			return err
		}
	}
	slices.Sort(changedPrincipals)
	for _, principal := range slices.Compact(changedPrincipals) {
		if err := invalidateGrantChange(ctx, az, principal); err != nil {
			return err
		}
	}
	return nil
}
