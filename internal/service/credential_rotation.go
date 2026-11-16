package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// CredentialRotations owns scheduled credential rotation configuration (#148,
// slice S1). It records WHAT rotates and WHO is the rotation's authority; it
// executes nothing and holds no secret. The run state machine arrives with S2,
// which acts as the recorded authority principal and re-authorizes it on every
// transition, never as a system proof. (The name avoids the existing Rotation
// service, which rotates Hikyo's own key hierarchy.)
type CredentialRotations struct {
	DB  *store.DB
	Now func() time.Time
}

func (s *CredentialRotations) now() time.Time { return nowOr(s.Now) }

var (
	// ErrRotationKeyInUse refuses a second live rotation for a key, so two
	// rotations never fight over one value.
	ErrRotationKeyInUse = fmt.Errorf("%w: another live rotation already drives one of these keys", domain.ErrConflict)
	// ErrRotationStale refuses a state change whose expected generation is not
	// the rotation's current generation.
	ErrRotationStale = fmt.Errorf("%w: the rotation changed since it was read; read it again", domain.ErrConflict)
	// ErrRotationAuthorityLost refuses a resume while the recorded authority
	// principal no longer holds the access the rotation needs.
	ErrRotationAuthorityLost = fmt.Errorf("%w: the rotation's recorded authority principal no longer holds edit, publish and reveal on its keys; create the rotation again", domain.ErrConflict)
)

// RotationView is the metadata surface of one rotation. It carries no secret.
type RotationView struct {
	store.RotationRecord
}

// CreateRotationRequest configures one rotation. The keys must already exist;
// KeyID receives the generated password and UsernameKeyID the generated role
// name, so a consumer always reads a matching pair.
type CreateRotationRequest struct {
	ProviderID             string
	KeyID                  string
	UsernameKeyID          string
	IntervalSeconds        int64
	RevocationDelaySeconds int64
}

// RotationStateRequest is a pause, resume or delete. ExpectedGeneration 0 means
// "the current generation"; a non-zero value must match it.
type RotationStateRequest struct {
	RotationID         string
	ExpectedGeneration int64
}

// ValidateRotationBounds enforces the named interval and delay bounds. A run
// supersedes the previous credential only after its overlap, so the delay must
// also be shorter than the interval or two superseded credentials would be live
// at once.
func ValidateRotationBounds(intervalSeconds, delaySeconds int64) error {
	const second = int64(time.Second)
	switch {
	case intervalSeconds < int64(domain.MinRotationInterval)/second:
		return fmt.Errorf("%w: interval_seconds is below the %s minimum rotation interval", domain.ErrInvalid, domain.MinRotationInterval)
	case intervalSeconds > int64(domain.MaxRotationInterval)/second:
		return fmt.Errorf("%w: interval_seconds is above the %s maximum rotation interval", domain.ErrInvalid, domain.MaxRotationInterval)
	case delaySeconds < int64(domain.MinRevocationDelay)/second:
		return fmt.Errorf("%w: revocation_delay_seconds is below the %s minimum revocation delay", domain.ErrInvalid, domain.MinRevocationDelay)
	case delaySeconds > int64(domain.MaxRevocationDelay)/second:
		return fmt.Errorf("%w: revocation_delay_seconds is above the %s maximum revocation delay", domain.ErrInvalid, domain.MaxRevocationDelay)
	case delaySeconds >= intervalSeconds:
		return fmt.Errorf("%w: revocation_delay_seconds must be shorter than interval_seconds", domain.ErrInvalid)
	}
	return nil
}

func requireRotationEnv(scope domain.Scope, what string, ids ...string) error {
	return requireEnvScope(scope, "rotation "+what+" requires environment scope"+idsHint(ids), ids...)
}

func idsHint(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return " and a rotation id"
}

// Create configures a rotation. The caller must be a human who currently holds
// edit, publish and reveal on both keys; they are recorded as the rotation's
// authority principal. A machine, developer or other non-human caller is
// refused: a rotation is a standing delegation of a person's authority.
func (s *CredentialRotations) Create(ctx context.Context, actor Actor, scope domain.Scope, req CreateRotationRequest) (RotationView, error) {
	if err := requireRotationEnv(scope, "create"); err != nil {
		return RotationView{}, err
	}
	if req.ProviderID == "" || req.KeyID == "" || req.UsernameKeyID == "" {
		return RotationView{}, fmt.Errorf("%w: rotation create requires provider_id, key_id and username_key_id", domain.ErrInvalid)
	}
	if req.KeyID == req.UsernameKeyID {
		return RotationView{}, fmt.Errorf("%w: key_id and username_key_id must be different keys", domain.ErrInvalid)
	}
	if err := ValidateRotationBounds(req.IntervalSeconds, req.RevocationDelaySeconds); err != nil {
		return RotationView{}, err
	}
	id, err := newID("rot")
	if err != nil {
		return RotationView{}, err
	}
	now := store.CanonTime(s.now())
	var out RotationView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		// Both keys are authorized as individual keys so a key-narrowed member
		// access rule applies; an invisible key is the uniform not-found.
		caller, proof, err := authorizeKey(ctx, az, actor, authz.OpRotationCreate, scope, authz.KeyByID(req.KeyID), now)
		if err != nil {
			return err
		}
		if _, _, err := authorizeKey(ctx, az, actor, authz.OpRotationCreate, scope, authz.KeyByID(req.UsernameKeyID), now); err != nil {
			return err
		}
		// Local host authority carries no class and acts as a named human
		// principal, so it is admitted beside a human session; a machine,
		// developer or other class is not, because the rotation delegates a
		// person's own access.
		if caller.Class != domain.ClassHuman && caller.Class != "" {
			return fmt.Errorf("%w: a rotation is created by a human who holds the access it will exercise", domain.ErrInvalid)
		}
		provider, err := r.Dynamic().GetProvider(ctx, proof, req.ProviderID)
		if err != nil {
			return err
		}
		if !provider.CredentialPresent || provider.State != "active" {
			return fmt.Errorf("%w: the dynamic provider has no admin credential set, so it cannot mint", domain.ErrInvalid)
		}
		key, err := r.Catalogue().Get(ctx, proof, req.KeyID)
		if err != nil {
			return err
		}
		if key.Classification != string(schema.Secret) {
			return fmt.Errorf("%w: key_id must name a secret-classified key, because it receives a generated password", domain.ErrInvalid)
		}
		if _, err := r.Catalogue().Get(ctx, proof, req.UsernameKeyID); err != nil {
			return err
		}
		record, err := r.Rotations().Create(ctx, proof, store.RotationCreate{
			ID: id, ProviderID: req.ProviderID, KeyID: req.KeyID, UsernameKeyID: req.UsernameKeyID,
			IntervalSeconds: req.IntervalSeconds, RevocationDelaySeconds: req.RevocationDelaySeconds,
			AuthorityPrincipalID: string(caller.Principal),
			NextRunAt:            now.Add(time.Duration(req.IntervalSeconds) * time.Second), At: now,
		})
		if err != nil {
			if errors.Is(err, domain.ErrConflict) || errors.Is(err, store.ErrConflict) {
				return ErrRotationKeyInUse
			}
			return err
		}
		out = RotationView{RotationRecord: record}
		return insertRotationConfigured(ctx, r, proof, caller.Principal, "created", record)
	})
	return out, err
}

func insertRotationConfigured(ctx context.Context, r store.Repos, proof authz.Proof, principal domain.PrincipalID, action string, rec store.RotationRecord) error {
	payload := audit.Payload{
		"action": action, "rotation_id": rec.ID, "provider_id": rec.ProviderID, "key_id": rec.KeyID,
		"interval_seconds": rec.IntervalSeconds, "revocation_delay_seconds": rec.RevocationDelaySeconds,
		"authority_principal_id": rec.AuthorityPrincipalID, "generation": rec.Generation,
	}
	if rec.UsernameKeyID != "" {
		payload["username_key_id"] = rec.UsernameKeyID
	}
	ev, err := domainEvent(ctx, audit.EventRotationConfigured, principal, audit.Object{Type: "rotation", ID: rec.ID}, payload)
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, proof, ev)
}

// List returns every rotation in the environment, disabled history included.
func (s *CredentialRotations) List(ctx context.Context, actor Actor, scope domain.Scope) ([]RotationView, error) {
	if err := requireRotationEnv(scope, "list"); err != nil {
		return nil, err
	}
	var out []RotationView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpRotationInspect, scope, s.now())
		if err != nil {
			return err
		}
		rows, err := r.Rotations().List(ctx, proof)
		if err != nil {
			return err
		}
		out = make([]RotationView, 0, len(rows))
		for _, row := range rows {
			out = append(out, RotationView{RotationRecord: row})
		}
		return nil
	})
	return out, err
}

// Get returns one rotation.
func (s *CredentialRotations) Get(ctx context.Context, actor Actor, scope domain.Scope, rotationID string) (RotationView, error) {
	if err := requireRotationEnv(scope, "show", rotationID); err != nil {
		return RotationView{}, err
	}
	var out RotationView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpRotationInspect, scope, s.now())
		if err != nil {
			return err
		}
		rec, err := r.Rotations().Get(ctx, proof, rotationID)
		out = RotationView{RotationRecord: rec}
		return err
	})
	return out, err
}

// Pause stops scheduling. S1 has no run, so nothing is in flight to finish.
func (s *CredentialRotations) Pause(ctx context.Context, actor Actor, scope domain.Scope, req RotationStateRequest) (RotationView, error) {
	return s.transition(ctx, actor, scope, req, authz.OpRotationPause, "paused")
}

// Resume re-arms a paused rotation and recomputes its next run from now. It
// first proves the recorded authority principal still holds the access the
// rotation exercises, so a rotation never resumes into a guaranteed halt.
func (s *CredentialRotations) Resume(ctx context.Context, actor Actor, scope domain.Scope, req RotationStateRequest) (RotationView, error) {
	return s.transition(ctx, actor, scope, req, authz.OpRotationResume, "resumed")
}

// Delete disables a rotation. It is soft: the row stays as history. A rotation
// with an in-flight run is refused (S1 has none; the check is the S2 contract).
func (s *CredentialRotations) Delete(ctx context.Context, actor Actor, scope domain.Scope, req RotationStateRequest) (RotationView, error) {
	return s.transition(ctx, actor, scope, req, authz.OpRotationDelete, "deleted")
}

func (s *CredentialRotations) transition(ctx context.Context, actor Actor, scope domain.Scope, req RotationStateRequest, op authz.Operation, action string) (RotationView, error) {
	if err := requireRotationEnv(scope, action, req.RotationID); err != nil {
		return RotationView{}, err
	}
	now := store.CanonTime(s.now())
	var out RotationView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, op, scope, now)
		if err != nil {
			return err
		}
		cur, err := r.Rotations().Get(ctx, proof, req.RotationID)
		if err != nil {
			return err
		}
		if req.ExpectedGeneration != 0 && req.ExpectedGeneration != cur.Generation {
			return ErrRotationStale
		}
		next := store.RotationSetState{RotationID: cur.ID, ExpectedGeneration: cur.Generation, At: now}
		switch action {
		case "paused":
			if cur.State != store.RotationStateActive {
				return fmt.Errorf("%w: only an active rotation can be paused (state is %s)", domain.ErrConflict, cur.State)
			}
			next.State = store.RotationStatePaused
		case "resumed":
			if cur.State != store.RotationStatePaused {
				return fmt.Errorf("%w: only a paused rotation can be resumed (state is %s)", domain.ErrConflict, cur.State)
			}
			if err := s.reauthorizeAuthority(ctx, az, scope, cur, now); err != nil {
				return err
			}
			next.State = store.RotationStateActive
			next.NextRunAt = now.Add(time.Duration(cur.IntervalSeconds) * time.Second)
		case "deleted":
			if cur.State == store.RotationStateDisabled {
				return fmt.Errorf("%w: the rotation is already deleted", domain.ErrConflict)
			}
			open, err := r.Rotations().CountOpenRuns(ctx, proof, cur.ID)
			if err != nil {
				return err
			}
			if open > 0 {
				return fmt.Errorf("%w: the rotation has a run in flight; let it finish or quarantine it first", domain.ErrConflict)
			}
			next.State = store.RotationStateDisabled
		}
		rec, err := r.Rotations().SetState(ctx, proof, next)
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrRotationStale
			}
			return err
		}
		out = RotationView{RotationRecord: rec}
		return insertRotationConfigured(ctx, r, proof, caller.Principal, action, rec)
	})
	return out, err
}

// reauthorizeAuthority proves the rotation's authority principal still holds
// the create formula on both keys, with its own grants and rules. It is the
// per-transition re-authorization S2 repeats before every phase.
func (s *CredentialRotations) reauthorizeAuthority(ctx context.Context, az *authz.TxAuthorizer, scope domain.Scope, rec store.RotationRecord, now time.Time) error {
	authority := authz.Identity{Principal: domain.PrincipalID(rec.AuthorityPrincipalID), Class: domain.ClassHuman}
	az.SetClock(now)
	for _, id := range []string{rec.KeyID, rec.UsernameKeyID} {
		if id == "" {
			continue
		}
		if _, err := az.AuthorizeKey(ctx, authority, authz.OpRotationCreate, scope, authz.KeyByID(id)); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return ErrRotationAuthorityLost
			}
			return err
		}
	}
	return nil
}
