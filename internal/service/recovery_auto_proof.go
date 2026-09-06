package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

var errNoDrillCandidate = errors.New("scratch principal has no eligible existing credential-management grant")

// AutoCredentialProof selects one existing principal in the restored scratch
// database, reconciles it and exercises the normal credential lifecycle. No
// method on the live Restore surface performs automatic reconciliation.
func (s *Recovery) AutoCredentialProof(ctx context.Context, kr *crypto.Keyring) error {
	if s == nil || s.DB == nil {
		return errors.New("automatic credential proof requires authenticated scratch custody")
	}
	if err := s.DB.CheckScratch(ctx); err != nil {
		return err
	}
	status, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if len(status.Pending) == 0 || len(status.Pending) > 1024 {
		return errors.New("automatic scratch credential proof requires 1 to 1024 pending principals")
	}
	slices.SortFunc(status.Pending, func(a, b authz.PrincipalRef) int { return strings.Compare(string(a.ID), string(b.ID)) })
	for _, candidate := range status.Pending {
		var scope domain.Scope
		// Reconciliation makes grants readable. If the principal is unsuitable,
		// rollback its reconciliation and audit event in this same transaction.
		_, err := restoreReconcile(ctx, func(ctx context.Context, fn tx.RestoreFn) error {
			return tx.RecoveryWrite(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				if err := fn(ctx, az); err != nil {
					return err
				}
				grants, err := az.GrantsOf(ctx, candidate.ID)
				if err != nil {
					return err
				}
				if len(grants) > 4096 {
					return errors.New("scratch principal grant inventory exceeds automatic proof bound")
				}
				eligible := make([]domain.Scope, 0)
				for _, grant := range grants {
					// Instance-only authority does not invent an org enumeration.
					if grant.Capability == domain.CapManageIdentities && grant.Scope.Org != "" && grant.Scope.Env == "" {
						eligible = append(eligible, grant.Scope)
					}
				}
				if len(eligible) == 0 {
					return errNoDrillCandidate
				}
				slices.SortFunc(eligible, func(a, b domain.Scope) int {
					// Existing project scopes avoid requesting broader navigation.
					if (a.Project == "") != (b.Project == "") {
						if a.Project != "" {
							return -1
						}
						return 1
					}
					if c := strings.Compare(string(a.Org), string(b.Org)); c != 0 {
						return c
					}
					return strings.Compare(string(a.Project), string(b.Project))
				})
				caller, err := LocalPrincipal(candidate.ID).resolve(ctx, az, time.Now().UTC())
				if err != nil {
					return err
				}
				for _, grantScope := range eligible {
					resolved, found, err := automaticDrillProject(ctx, r.Projects(), az, caller, grantScope)
					if err != nil {
						return err
					}
					if found {
						scope = resolved
						return nil
					}
				}
				return errNoDrillCandidate
			})
		}, candidate.ID)
		if errors.Is(err, errNoDrillCandidate) {
			continue
		}
		if err != nil {
			return fmt.Errorf("select scratch credential principal: %w", err)
		}
		return s.MintAndRevoke(ctx, kr, candidate.ID, scope)
	}
	return errNoDrillCandidate
}

// Resolve project eligibility before committing the candidate's reconciliation.
// Empty or unauthorized scopes are candidates to skip; operational errors abort
// the drill. The ordinary authorizer and proof-gated repository own every read.
func automaticDrillProject(ctx context.Context, projects store.ProjectReader, az *authz.TxAuthorizer, caller authz.Identity, scope domain.Scope) (domain.Scope, bool, error) {
	if scope.Project != "" {
		held, err := az.CallerHolds(ctx, caller, authz.OpServiceAccountCreate, scope)
		return scope, held, err
	}
	canList, err := az.CallerHolds(ctx, caller, authz.OpProjectList, scope)
	if err != nil || !canList {
		return domain.Scope{}, false, err
	}
	proof, err := az.Authorize(ctx, caller, authz.OpProjectList, scope)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Scope{}, false, nil
	}
	if err != nil {
		return domain.Scope{}, false, err
	}
	rows, err := projects.List(ctx, proof)
	if err != nil {
		return domain.Scope{}, false, err
	}
	if len(rows) > 4096 {
		return domain.Scope{}, false, errors.New("scratch project inventory exceeds automatic proof bound")
	}
	slices.SortFunc(rows, func(a, b store.Project) int { return strings.Compare(a.ID, b.ID) })
	for _, project := range rows {
		candidate := domain.Scope{Org: scope.Org, Project: domain.ProjectID(project.ID)}
		held, err := az.CallerHolds(ctx, caller, authz.OpServiceAccountCreate, candidate)
		if err != nil {
			return domain.Scope{}, false, err
		}
		if held {
			return candidate, true, nil
		}
	}
	return domain.Scope{}, false, nil
}
