package service

import (
	"context"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// ProjectCapacity resolves the saved project policy against the current owner
// configuration. A saved override survives a lower instance maximum.
type ProjectCapacity struct {
	KeyLimitOverride *int
	InstanceDefault  int
	InstanceMaximum  int
	EffectiveLimit   int
	KeyCount         int64
	EnvironmentCount int64
	Revision         int64
	Inherited        bool
	Capped           bool
}

// CapacityUpdate binds a settings write to both the project revision and the
// operator policy displayed in the editor. Nil resets the project to inherit.
type CapacityUpdate struct {
	KeyLimitOverride        *int
	ExpectedRevision        int64
	ExpectedInstanceDefault int
	ExpectedInstanceMaximum int
}

func resolveProjectCapacity(saved store.ProjectCapacity, instanceDefault, instanceMaximum int) (ProjectCapacity, error) {
	defaultLimit := config.EffectiveDefaultKeysPerProject(instanceDefault)
	maximum := config.EffectiveMaxKeysPerProject(instanceMaximum)
	if defaultLimit < 1 || maximum < defaultLimit || maximum > config.MaxKeysPerProjectCeiling {
		return ProjectCapacity{}, fmt.Errorf("%w: invalid instance key capacity policy", domain.ErrInvalid)
	}
	out := ProjectCapacity{InstanceDefault: defaultLimit, InstanceMaximum: maximum, EffectiveLimit: defaultLimit, Revision: saved.Revision, Inherited: saved.KeyLimitOverride == nil}
	if saved.KeyLimitOverride != nil {
		n := *saved.KeyLimitOverride
		if n < 1 || n > config.MaxKeysPerProjectCeiling {
			return ProjectCapacity{}, fmt.Errorf("%w: invalid saved project key capacity", domain.ErrInvalid)
		}
		out.KeyLimitOverride = &n
		out.EffectiveLimit = n
	}
	if out.EffectiveLimit > maximum {
		out.EffectiveLimit = maximum
		out.Capped = true
	}
	return out, nil
}

func projectKeyLimit(ctx context.Context, projects store.ProjectReader, p authz.Proof, instanceDefault, instanceMaximum int) (int, error) {
	saved, err := projects.GetCapacity(ctx, p)
	if err != nil {
		return 0, err
	}
	capacity, err := resolveProjectCapacity(saved, instanceDefault, instanceMaximum)
	return capacity.EffectiveLimit, err
}

func readProjectCapacity(ctx context.Context, r store.ReadRepos, p authz.Proof, instanceDefault, instanceMaximum int) (ProjectCapacity, error) {
	saved, err := r.Projects().GetCapacity(ctx, p)
	if err != nil {
		return ProjectCapacity{}, err
	}
	out, err := resolveProjectCapacity(saved, instanceDefault, instanceMaximum)
	if err != nil {
		return ProjectCapacity{}, err
	}
	out.KeyCount, err = r.Catalogue().Count(ctx, p)
	if err != nil {
		return ProjectCapacity{}, err
	}
	out.EnvironmentCount, err = r.Environments().Count(ctx, p)
	return out, err
}

// GetCapacity reads metadata and usage under project-settings authority.
func (s *ProjectSettings) GetCapacity(ctx context.Context, actor Actor, scope domain.Scope) (ProjectCapacity, error) {
	var out ProjectCapacity
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, p, err := authorize(ctx, az, actor, authz.OpProjectCapacityRead, scope, s.now())
		if err != nil {
			return err
		}
		out, err = readProjectCapacity(ctx, r, p, s.DefaultKeysPerProject, s.MaxKeysPerProject)
		return err
	})
	return out, err
}

// SetCapacity serializes with key admission, rejects stale editors, and audits
// actual policy changes. Lowering capacity preserves all existing keys.
func (s *ProjectSettings) SetCapacity(ctx context.Context, actor Actor, scope domain.Scope, want CapacityUpdate) (ProjectCapacity, error) {
	var out ProjectCapacity
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpProjectCapacitySet, scope, s.now())
		if err != nil {
			return err
		}
		if err := r.Projects().Lock(ctx, p); err != nil {
			return err
		}
		saved, err := r.Projects().GetCapacity(ctx, p)
		if err != nil {
			return err
		}
		before, err := resolveProjectCapacity(saved, s.DefaultKeysPerProject, s.MaxKeysPerProject)
		if err != nil {
			return err
		}
		if want.ExpectedRevision != before.Revision || want.ExpectedInstanceDefault != before.InstanceDefault || want.ExpectedInstanceMaximum != before.InstanceMaximum {
			return fmt.Errorf("%w: key capacity policy changed; reload project settings", domain.ErrConflict)
		}
		same := saved.KeyLimitOverride == nil && want.KeyLimitOverride == nil || saved.KeyLimitOverride != nil && want.KeyLimitOverride != nil && *saved.KeyLimitOverride == *want.KeyLimitOverride
		if want.KeyLimitOverride != nil && (*want.KeyLimitOverride < 1 || *want.KeyLimitOverride > before.InstanceMaximum) {
			return fmt.Errorf("%w: project key limit must be between 1 and the instance maximum %d", domain.ErrInvalid, before.InstanceMaximum)
		}
		if !same {
			if err := r.Projects().SetCapacity(ctx, p, want.KeyLimitOverride, saved.Revision); err != nil {
				return err
			}
			after, err := resolveProjectCapacity(store.ProjectCapacity{KeyLimitOverride: want.KeyLimitOverride, Revision: saved.Revision + 1}, s.DefaultKeysPerProject, s.MaxKeysPerProject)
			if err != nil {
				return err
			}
			previousMode, previousOverride := capacityAuditOverride(saved.KeyLimitOverride)
			mode, override := capacityAuditOverride(want.KeyLimitOverride)
			ev, err := domainEvent(ctx, audit.EventProjectCapacityChanged, caller.Principal, audit.Object{Type: "project", ID: string(scope.Project)}, audit.Payload{
				"previous_mode": previousMode, "previous_override": previousOverride,
				"mode": mode, "override": override,
				"previous_effective_limit": before.EffectiveLimit, "effective_limit": after.EffectiveLimit,
				"instance_default": after.InstanceDefault, "instance_maximum": after.InstanceMaximum,
			})
			if err != nil {
				return err
			}
			if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
				return err
			}
		}
		// Repos exposes the read aggregates with narrower return types only via
		// its read view, so read the counts explicitly under this same lock.
		current, err := r.Projects().GetCapacity(ctx, p)
		if err != nil {
			return err
		}
		out, err = resolveProjectCapacity(current, s.DefaultKeysPerProject, s.MaxKeysPerProject)
		if err != nil {
			return err
		}
		out.KeyCount, err = r.Catalogue().Count(ctx, p)
		if err != nil {
			return err
		}
		out.EnvironmentCount, err = r.Environments().Count(ctx, p)
		return err
	})
	return out, err
}

func capacityAuditOverride(override *int) (string, int) {
	if override == nil {
		return "inherit", 0
	}
	return "custom", *override
}
