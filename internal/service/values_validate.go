package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/parameters"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// surfaceValidate labels a scanner finding surfaced by `value.validate`. Like
// `definitions check`, validate never persists, never mints an acknowledgement
// and emits no scanning.* event, so this is a wire surface only.
const surfaceValidate = "validate"

// ValidatedChange is the verdict on one proposed change (mcp-write ADR § 1).
// It carries what publish would refuse, in the same safe wording publish uses.
// A schema failure on a config key may quote the offending fragment, as it
// does over REST; a secret key's engine text never carries instance data.
type ValidatedChange struct {
	KeyID          string
	Name           string
	Classification string
	Operation      string
	// Valid is false when publish would refuse this change today.
	Valid bool
	// Problems are the publish-time refusals, each in wire-safe wording.
	Problems []string
	// ValidationDeferred reports a config value that references environment
	// parameters: its final schema check runs at delivery with the caller's
	// parameters, so structural validity is all that can be reported here.
	ValidationDeferred bool
	// Findings are the secret-scanner matches over a config value. None
	// carries an acknowledgement: validate is a diagnostic, not an ingress.
	Findings []Finding
}

// ValidateSet evaluates staging `value` into `keyName` without staging it. It
// requires exactly the authority `Set` requires (`edit@env`) and is audited as
// an authority-bearing action; it mutates nothing else.
func (s *Values) ValidateSet(ctx context.Context, actor Actor, scope domain.Scope, keyName, value string) (ValidatedChange, error) {
	return s.validate(ctx, actor, scope, keyName, store.PendingSet, value)
}

// ValidateUnset evaluates clearing `keyName` without staging the clear.
func (s *Values) ValidateUnset(ctx context.Context, actor Actor, scope domain.Scope, keyName string) (ValidatedChange, error) {
	return s.validate(ctx, actor, scope, keyName, store.PendingUnset, "")
}

func (s *Values) validate(ctx context.Context, actor Actor, scope domain.Scope, keyName string,
	operation store.PendingOperation, value string) (ValidatedChange, error) {
	if scope.Env == "" {
		return ValidatedChange{}, fmt.Errorf("%w: a value addresses an environment", domain.ErrInvalid)
	}
	// A write transaction only because the audit event commits here; the
	// operation's store ops are otherwise reads (authz registry pins them).
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (ValidatedChange, error) {
		caller, p, err := authorize(ctx, az, actor, authz.OpValueValidate, scope, time.Now().UTC())
		if err != nil {
			return ValidatedChange{}, err
		}
		key, err := findKey(ctx, r.Catalogue(), p, keyName)
		if err != nil {
			return ValidatedChange{}, err
		}
		rows, err := r.Catalogue().ListPresence(ctx, p)
		if err != nil {
			return ValidatedChange{}, err
		}
		rules := presenceOfKey(key, rows)
		envID := string(scope.Env)
		out := ValidatedChange{
			KeyID: key.ID, Name: key.Name, Classification: key.Classification,
			Operation: string(operation), Valid: true,
		}
		problem := func(err error) error {
			if err == nil {
				return nil
			}
			var detail interface{ SafeDetail() string }
			if !errors.As(err, &detail) {
				return err
			}
			out.Valid = false
			out.Problems = append(out.Problems, detail.SafeDetail())
			return nil
		}
		switch operation {
		case store.PendingUnset:
			if rules.Required.Covers(envID) {
				if err := problem(invalidDetail("key %q is `required_in` environment %s and resolves to absent: publish is vetoed",
					key.Name, envID)); err != nil {
					return ValidatedChange{}, err
				}
			}
		case store.PendingSet:
			if err := problem(checkNotForbidden(key, rules, envID)); err != nil {
				return ValidatedChange{}, err
			}
			declarations, err := environmentParameters(ctx, r.Environments(), p)
			if err != nil {
				return ValidatedChange{}, err
			}
			if err := problem(validateValueWithParameters(key, value, declarations)); err != nil {
				return ValidatedChange{}, err
			}
			if out.Valid && key.Classification == string(schema.Config) && len(declarations) > 0 {
				refs, err := parameters.References(value)
				out.ValidationDeferred = err == nil && len(refs) > 0
			}
			if out.Findings, err = scanValueForValidate(ctx, s.Scan, key.ID, key.Classification, []byte(schema.Normalize(value))); err != nil {
				return ValidatedChange{}, err
			}
		default:
			return ValidatedChange{}, fmt.Errorf("%w: unknown change operation %q", domain.ErrInvalid, operation)
		}
		ev, err := domainEvent(ctx, audit.EventValueChangeValidated, caller.Principal,
			audit.Object{Type: "key", ID: key.ID}, audit.Payload{
				"key_id":         key.ID,
				"name":           audit.SanitizeFreeText(key.Name),
				"classification": key.Classification,
				"operation":      string(operation),
				"valid":          out.Valid,
				"problems":       len(out.Problems),
				"findings":       len(out.Findings),
			})
		if err != nil {
			return ValidatedChange{}, err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return ValidatedChange{}, err
		}
		return out, nil
	})
}
