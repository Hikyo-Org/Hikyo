package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Personal values (#1235, personal-values ADR): one value per developer for a
// declared key in an unprotected environment. The owner is always the proof's
// principal, so every method here acts on the caller's own value, except the
// member methods, which see and remove metadata only.

// MaxPersonalValuesPerProject matches the resolved-cell budget. It is a var
// only so a test can lower it; nothing in production writes it.
var MaxPersonalValuesPerProject int64 = 100_000

// Notice causes (personal-values ADR, Removal notices).
const (
	PersonalCauseAdministrator     = "administrator"
	PersonalCauseRetention         = "retention"
	PersonalCauseRestore           = "restore"
	PersonalCauseKeyRenamed        = "key_renamed"
	PersonalCauseKeyDeleted        = "key_deleted"
	PersonalCauseForbidden         = "forbidden"
	PersonalCauseQualificationLost = "qualification_lost"
	PersonalCauseEnvironmentGone   = "environment_deleted"
)

// ErrPersonalUnqualified refuses set and reveal for an account with no
// standing possession factor. It is a 403: the caller can reach the
// environment, and enrolling a factor is the remedy.
var ErrPersonalUnqualified = fmt.Errorf("%w: personal values need an authenticator app or passkey on this account", domain.ErrUnauthorized)

// ErrPersonalProtected refuses a personal value on a protected environment:
// personal values are for unprotected environments only (ADR).
var ErrPersonalProtected = fmt.Errorf("%w: personal values are for unprotected environments only", domain.ErrUnauthorized)

// PersonalValues is the personal value service.
type PersonalValues struct {
	DB      *store.DB
	Keyring *crypto.Keyring
	// Auth supplies the reveal reauthentication gate and the factor check.
	Auth *Auth
}

// PersonalEntry is one of the caller's own keys: a value, a notice, or both.
type PersonalEntry struct {
	KeyID     string
	Name      string
	Set       bool
	UpdatedAt time.Time
	// Notice is the cause of a removal the owner has not yet acknowledged.
	Notice string
	// Required marks a key that is personal_required_in this environment, so
	// a checklist row can carry no value and no notice.
	Required bool
	// Suspended is set while the owner has lost the access predicate without
	// a credential change; SuspendedAt is when the sweep first saw it.
	Suspended   bool
	SuspendedAt time.Time
	// Warnings name schema rules the owner's value does not satisfy. They
	// never refuse the value.
	Warnings []string
	// Value is set only by Reveal.
	Value string
}

// PersonalHolder is metadata about another person's value: never the value.
type PersonalHolder struct {
	Principal domain.PrincipalID
	Username  string
	Set       bool
	UpdatedAt time.Time
	Notice    string
}

// refusePersonalCaller admits only a human's own session. Developer
// credentials reach personal values only through delivery; a workspace
// session is extractable by the viewing page (multi-instance ADR); machines
// have no personal values. The refusal is the uniform nonexistent answer.
func refusePersonalCaller(caller authz.Identity) error {
	if caller.Class != domain.ClassHuman || caller.SessionID == "" ||
		caller.Artifact == authz.WorkspaceArtifact {
		return domain.ErrNotFound
	}
	return nil
}

// caller resolves the actor and admits only a human's own session; the
// operation's proof is minted at each call site.
func (s *PersonalValues) caller(ctx context.Context, az *authz.TxAuthorizer, actor Actor) (authz.Identity, error) {
	caller, err := actor.resolve(ctx, az, time.Now().UTC())
	if err != nil {
		return authz.Identity{}, err
	}
	if err := refusePersonalCaller(caller); err != nil {
		return authz.Identity{}, err
	}
	return caller, nil
}

func (s *PersonalValues) sealer(ctx context.Context, scope domain.Scope) (*crypto.ProjectSealer, error) {
	if s.Keyring == nil {
		return nil, errors.New("service: personal values require a keyring")
	}
	return s.Keyring.ForProject(ctx, string(scope.Org), string(scope.Project))
}

func personalAAD(scope domain.Scope, keyID string, owner domain.PrincipalID) crypto.PersonalValueAAD {
	return crypto.PersonalValueAAD{OrgID: string(scope.Org), ProjectID: string(scope.Project), EnvID: string(scope.Env), KeyID: keyID, OwnerPrincipalID: string(owner)}
}

func personalEvent(ctx context.Context, typ audit.EventType, actor domain.PrincipalID, key store.CatalogueKey, owner domain.PrincipalID, extra audit.Payload) (audit.Event, error) {
	payload := audit.Payload{"key_id": key.ID, "name": audit.SanitizeFreeText(key.Name), "owner_principal_id": string(owner)}
	for k, v := range extra {
		payload[k] = v
	}
	return domainEvent(ctx, typ, actor, audit.Object{Type: "key", ID: key.ID}, payload)
}

// List returns the caller's own values (masked) and notices in one environment.
func (s *PersonalValues) List(ctx context.Context, actor Actor, scope domain.Scope) ([]PersonalEntry, error) {
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) ([]PersonalEntry, error) {
		caller, err := s.caller(ctx, az, actor)
		if err != nil {
			return nil, err
		}
		p, err := az.Authorize(ctx, caller, authz.OpPersonalValueList, scope)
		if err != nil {
			return nil, err
		}
		return ownEntries(ctx, r, p)
	})
}

// ownEntries merges the proof owner's values and notices, ordered by name.
func ownEntries(ctx context.Context, r store.Repos, p authz.Proof) ([]PersonalEntry, error) {
	keys, err := r.Catalogue().List(ctx, p)
	if err != nil {
		return nil, err
	}
	values, err := r.PersonalValues().ListOwn(ctx, p)
	if err != nil {
		return nil, err
	}
	notices, err := r.PersonalValues().NoticesOwn(ctx, p)
	if err != nil {
		return nil, err
	}
	byKey := map[string]store.PersonalValue{}
	for _, v := range values {
		byKey[v.KeyID] = v
	}
	noticeByName := map[string]string{}
	for _, n := range notices {
		noticeByName[n.KeyName] = n.Cause
	}
	out := []PersonalEntry{}
	for _, k := range keys {
		v, set := byKey[k.ID]
		cause := noticeByName[k.Name]
		delete(noticeByName, k.Name)
		if !set && cause == "" {
			continue
		}
		out = append(out, PersonalEntry{KeyID: k.ID, Name: k.Name, Set: set, UpdatedAt: v.UpdatedAt, Notice: cause})
	}
	// A notice on a name no longer declared still makes `hikyo run` refuse a
	// re-declared key, so it is listed too.
	for _, n := range notices {
		if _, left := noticeByName[n.KeyName]; left {
			out = append(out, PersonalEntry{Name: n.KeyName, Notice: n.Cause})
		}
	}
	return out, nil
}

// Set stores or replaces the caller's own value for a declared key.
// No schema rule evaluates it (the oracle rule), and it is never scanned.
func (s *PersonalValues) Set(ctx context.Context, actor Actor, scope domain.Scope, keyName, value string) (PersonalEntry, error) {
	if len(value) > schema.MaxValueBytes {
		return PersonalEntry{}, invalidDetail("personal value for %q exceeds %d bytes", keyName, schema.MaxValueBytes)
	}
	sealer, err := s.sealer(ctx, scope)
	if err != nil {
		return PersonalEntry{}, err
	}
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (PersonalEntry, error) {
		caller, err := s.caller(ctx, az, actor)
		if err != nil {
			return PersonalEntry{}, err
		}
		p, err := az.Authorize(ctx, caller, authz.OpPersonalValueSet, scope)
		if err != nil {
			return PersonalEntry{}, err
		}
		if err := s.requireQualified(ctx, az, caller); err != nil {
			return PersonalEntry{}, err
		}
		keys, err := r.Catalogue().List(ctx, p)
		if err != nil {
			return PersonalEntry{}, err
		}
		key, err := keyByName(keys, keyName)
		if err != nil {
			return PersonalEntry{}, err
		}
		settings, err := az.EnvironmentReauthSettings(ctx, string(scope.Env))
		if err != nil {
			return PersonalEntry{}, err
		}
		if settings.Protected {
			return PersonalEntry{}, ErrPersonalProtected
		}
		// The cap is counted under the project lock, taken before the ceremony
		// as copy does, and replacing the caller's own value never grows the
		// count (as the pending cap in values.go).
		if err := r.Projects().Lock(ctx, p); err != nil {
			return PersonalEntry{}, err
		}
		if err := s.personalGate(ctx, az, caller, scope, key.ID); err != nil {
			return PersonalEntry{}, err
		}
		_, err = r.PersonalValues().GetOwn(ctx, p, key.ID)
		replacing := err == nil
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return PersonalEntry{}, err
		}
		count, err := r.PersonalValues().CountProject(ctx, p)
		if err != nil {
			return PersonalEntry{}, err
		}
		if !replacing && count >= MaxPersonalValuesPerProject {
			return PersonalEntry{}, fmt.Errorf("%w: a project holds at most %d personal values", domain.ErrLimitExceeded, MaxPersonalValuesPerProject)
		}
		id, err := newID("pv")
		if err != nil {
			return PersonalEntry{}, err
		}
		sealed, err := sealer.SealPersonalValue(personalAAD(scope, key.ID, caller.Principal), []byte(value))
		if err != nil {
			return PersonalEntry{}, err
		}
		if err := fenceProject(ctx, r, p, sealer, scope); err != nil {
			return PersonalEntry{}, err
		}
		at := store.CanonTime(time.Now())
		if err := r.PersonalValues().Put(ctx, p, id, key.ID, sealed, at); err != nil {
			return PersonalEntry{}, err
		}
		if _, err := r.PersonalValues().DismissNotice(ctx, p, key.Name); err != nil {
			return PersonalEntry{}, err
		}
		ev, err := personalEvent(ctx, audit.EventPersonalValueSet, caller.Principal, key, caller.Principal, nil)
		if err != nil {
			return PersonalEntry{}, err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return PersonalEntry{}, err
		}
		return PersonalEntry{KeyID: key.ID, Name: key.Name, Set: true, UpdatedAt: at}, nil
	})
}

// ErrPersonalFactorRequired refuses a window an identity provider opened: the
// owner's own possession factor, not federated MFA, guards their value.
var ErrPersonalFactorRequired = fmt.Errorf("%w: personal values need a passkey or authenticator app reauthentication", ErrReauthUnitMismatch)

// personalGate consumes the reveal reauthentication window for one key. Only
// a passkey or authenticator window counts (ADR: a standing possession factor
// is what keeps an identity-provider administrator out).
func (s *PersonalValues) personalGate(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, scope domain.Scope, keyID string) error {
	intent, err := NewDisclosureReauthIntent(PurposeReveal, []string{string(scope.Env)}, []string{keyID})
	if err != nil {
		return err
	}
	return requireCeremony(ctx, s.Auth, az, caller, intent.requiringPossession())
}

// Reveal returns the caller's own plaintext. The disclosure event commits
// before the value leaves the server.
func (s *PersonalValues) Reveal(ctx context.Context, actor Actor, scope domain.Scope, keyName string) (PersonalEntry, error) {
	sealer, err := s.sealer(ctx, scope)
	if err != nil {
		return PersonalEntry{}, err
	}
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (PersonalEntry, error) {
		caller, err := s.caller(ctx, az, actor)
		if err != nil {
			return PersonalEntry{}, err
		}
		p, err := az.Authorize(ctx, caller, authz.OpPersonalValueReveal, scope)
		if err != nil {
			return PersonalEntry{}, err
		}
		if err := s.requireQualified(ctx, az, caller); err != nil {
			return PersonalEntry{}, err
		}
		keys, err := r.Catalogue().List(ctx, p)
		if err != nil {
			return PersonalEntry{}, err
		}
		key, err := keyByName(keys, keyName)
		if err != nil {
			return PersonalEntry{}, err
		}
		row, err := r.PersonalValues().GetOwn(ctx, p, key.ID)
		if err != nil {
			return PersonalEntry{}, err
		}
		if err := s.personalGate(ctx, az, caller, scope, key.ID); err != nil {
			return PersonalEntry{}, err
		}
		ev, err := personalEvent(ctx, audit.EventPersonalValueRevealed, caller.Principal, key, caller.Principal, nil)
		if err != nil {
			return PersonalEntry{}, err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return PersonalEntry{}, err
		}
		plain, err := sealer.OpenPersonalValue(personalAAD(scope, key.ID, caller.Principal), row.Ciphertext)
		if err != nil {
			return PersonalEntry{}, fmt.Errorf("service: personal value %s: %w", row.ID, err)
		}
		return PersonalEntry{KeyID: key.ID, Name: key.Name, Set: true, UpdatedAt: row.UpdatedAt, Value: string(plain)}, nil
	})
}

// Delete removes the caller's own value and dismisses any notice on the
// name. A name the caller holds neither for is not found.
func (s *PersonalValues) Delete(ctx context.Context, actor Actor, scope domain.Scope, keyName string) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := s.caller(ctx, az, actor)
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpPersonalValueDelete, scope)
		if err != nil {
			return err
		}
		keys, err := r.Catalogue().List(ctx, p)
		if err != nil {
			return err
		}
		key, keyErr := keyByName(keys, keyName)
		if keyErr != nil && !errors.Is(keyErr, domain.ErrNotFound) {
			return keyErr
		}
		var removed int64
		if keyErr == nil {
			if removed, err = r.PersonalValues().DeleteOwn(ctx, p, key.ID); err != nil {
				return err
			}
		}
		dismissed, err := r.PersonalValues().DismissNotice(ctx, p, keyName)
		if err != nil {
			return err
		}
		if removed == 0 && dismissed == 0 {
			return domain.ErrNotFound
		}
		if removed == 0 {
			return nil
		}
		ev, err := personalEvent(ctx, audit.EventPersonalValueDeleted, caller.Principal, key, caller.Principal, audit.Payload{"cause": "owner"})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
}

// Members lists who holds a value or notice for one key: metadata only.
func (s *PersonalValues) Members(ctx context.Context, actor Actor, scope domain.Scope, keyName string) ([]PersonalHolder, error) {
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) ([]PersonalHolder, error) {
		caller, err := s.caller(ctx, az, actor)
		if err != nil {
			return nil, err
		}
		p, err := az.Authorize(ctx, caller, authz.OpPersonalValueMembers, scope)
		if err != nil {
			return nil, err
		}
		key, holders, err := personalHolders(ctx, r, az, p, keyName)
		if err != nil {
			return nil, err
		}
		ev, err := domainEvent(ctx, audit.EventPersonalValueHoldersRead, caller.Principal, audit.Object{Type: "key", ID: key.ID},
			audit.Payload{"key_id": key.ID, "name": audit.SanitizeFreeText(key.Name), "count": len(holders)})
		if err != nil {
			return nil, err
		}
		return holders, r.Audit().InsertTenant(ctx, p, ev)
	})
}

func personalHolders(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, keyName string) (store.CatalogueKey, []PersonalHolder, error) {
	keys, err := r.Catalogue().List(ctx, p)
	if err != nil {
		return store.CatalogueKey{}, nil, err
	}
	key, err := keyByName(keys, keyName)
	if err != nil {
		return store.CatalogueKey{}, nil, err
	}
	values, err := r.PersonalValues().Holders(ctx, p, key.ID)
	if err != nil {
		return key, nil, err
	}
	notices, err := r.PersonalValues().NoticesForKeyName(ctx, p, key.Name)
	if err != nil {
		return key, nil, err
	}
	byOwner := map[domain.PrincipalID]*PersonalHolder{}
	var order []domain.PrincipalID
	// A holder whose account is gone keeps its principal id; any other lookup
	// failure fails the read.
	add := func(owner domain.PrincipalID) (*PersonalHolder, error) {
		if h, ok := byOwner[owner]; ok {
			return h, nil
		}
		h := &PersonalHolder{Principal: owner}
		account, err := az.AccountByPrincipal(ctx, owner)
		switch {
		case err == nil:
			h.Username = account.Username
		case !errors.Is(err, domain.ErrNotFound):
			return nil, err
		}
		byOwner[owner] = h
		order = append(order, owner)
		return h, nil
	}
	for _, v := range values {
		h, err := add(v.Owner)
		if err != nil {
			return key, nil, err
		}
		h.Set, h.UpdatedAt = true, v.UpdatedAt
	}
	for _, n := range notices {
		h, err := add(n.Owner)
		if err != nil {
			return key, nil, err
		}
		h.Notice = n.Cause
	}
	out := make([]PersonalHolder, len(order))
	for i, o := range order {
		out[i] = *byOwner[o]
	}
	return key, out, nil
}

// DeleteMember removes another person's value and leaves them a notice.
// member names a username or principal id; a person who holds neither a value
// nor a notice for this key is answered exactly as a nonexistent one. A
// person holding only a notice succeeds with nothing to delete: they hold no
// value, and their notice stays until they dismiss it.
func (s *PersonalValues) DeleteMember(ctx context.Context, actor Actor, scope domain.Scope, keyName, member string) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := s.caller(ctx, az, actor)
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpPersonalValueDeleteMember, scope)
		if err != nil {
			return err
		}
		key, holders, err := personalHolders(ctx, r, az, p, keyName)
		if err != nil {
			return err
		}
		var target *PersonalHolder
		for i := range holders {
			if member != "" && (holders[i].Username == member || string(holders[i].Principal) == member) {
				target = &holders[i]
				break
			}
		}
		if target == nil {
			return domain.ErrNotFound
		}
		if !target.Set {
			return nil
		}
		if _, err := r.PersonalValues().DeleteMember(ctx, p, key.ID, target.Principal); err != nil {
			return err
		}
		if err := r.PersonalValues().RecordNotice(ctx, p, string(scope.Env), key.Name, target.Principal, PersonalCauseAdministrator, time.Now()); err != nil {
			return err
		}
		ev, err := personalEvent(ctx, audit.EventPersonalValueDeleted, caller.Principal, key, target.Principal, audit.Payload{"cause": PersonalCauseAdministrator})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
}

// cascadePersonalForKey removes every personal value of a key that is being
// deleted or renamed, leaving each owner a notice under the key's old name so
// `hikyo run` refuses the key rather than falling back to the shared value.
func cascadePersonalForKey(ctx context.Context, r store.Repos, p authz.Proof, actor domain.PrincipalID, key store.CatalogueKey, cause string) error {
	cells, err := r.PersonalValues().CellsForKey(ctx, p, key.ID)
	if err != nil || len(cells) == 0 {
		return err
	}
	now := time.Now().UTC()
	for _, c := range cells {
		if err := r.PersonalValues().RecordNotice(ctx, p, c.EnvironmentID, key.Name, c.Owner, cause, now); err != nil {
			return err
		}
		ev, err := personalEvent(ctx, audit.EventPersonalValueDeleted, actor, key, c.Owner, audit.Payload{"cause": cause})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
	}
	_, err = r.PersonalValues().DeleteForKey(ctx, p, key.ID)
	return err
}

// purgePersonalForEnvironment deletes an environment's personal values and
// notices and audits one deletion per owner, with the count of values lost.
func purgePersonalForEnvironment(ctx context.Context, r store.Repos, p authz.Proof, actor domain.PrincipalID, env domain.EnvID) error {
	owners, err := r.PersonalValues().DeleteForEnvironment(ctx, p)
	if err != nil {
		return err
	}
	counts := map[domain.PrincipalID]int{}
	var order []domain.PrincipalID
	for _, o := range owners {
		if counts[o] == 0 {
			order = append(order, o)
		}
		counts[o]++
	}
	for _, o := range order {
		ev, err := domainEvent(ctx, audit.EventPersonalValueDeleted, actor, audit.Object{Type: "environment", ID: string(env)},
			audit.Payload{"owner_principal_id": string(o), "cause": PersonalCauseEnvironmentGone, "count": counts[o]})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
	}
	return nil
}
