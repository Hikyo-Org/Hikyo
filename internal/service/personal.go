package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

// MaxPersonalValuesPerProject matches the resolved-cell budget.
const MaxPersonalValuesPerProject int64 = 100_000

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
	PersonalCauseErasure           = "erasure"
)

// ErrPersonalUnqualified refuses set and reveal for an account that does not
// qualify (personal_qualification.go). It is a 403: the caller can reach the
// environment, and enrolling a factor is the remedy.
var ErrPersonalUnqualified = fmt.Errorf("%w: personal values need an authenticator app or passkey on this account, or a passwordless account that signs in through an identity provider with an assurance policy", domain.ErrUnauthorized)

// ErrPersonalProtected refuses a personal value on a protected environment:
// personal values are for unprotected environments only (ADR).
var ErrPersonalProtected = fmt.Errorf("%w: personal values are for unprotected environments only", domain.ErrUnauthorized)

// PersonalValues is the personal value service.
type PersonalValues struct {
	DB      *store.DB
	Keyring *crypto.Keyring
	// Auth supplies the reveal reauthentication gate and the factor check.
	Auth *Auth
	// Now is the retention sweep's clock; nil means time.Now. Tests inject it.
	Now func() time.Time
	// MaxPerProject caps a project's personal values; zero means
	// MaxPersonalValuesPerProject. Tests lower it.
	MaxPerProject int64
}

func (s *PersonalValues) maxPerProject() int64 {
	if s.MaxPerProject > 0 {
		return s.MaxPerProject
	}
	return MaxPersonalValuesPerProject
}

func (s *PersonalValues) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
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
// It writes and audits nothing, so it runs on a read transaction.
func (s *PersonalValues) List(ctx context.Context, actor Actor, scope domain.Scope) ([]PersonalEntry, error) {
	var out []PersonalEntry
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, err := s.caller(ctx, az, actor)
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpPersonalValueList, scope)
		if err != nil {
			return err
		}
		// An owner admitted only by the owner fallback (no read and reveal
		// here any more) sees just what they hold, not the required checklist.
		access, err := az.PersonalOwnerHasAccess(ctx, caller.Principal, scope)
		if err != nil {
			return err
		}
		out, err = ownEntries(ctx, r, p, scope.Env, access)
		return err
	})
	return out, err
}

// ownEntries merges the proof owner's values and notices with the
// environment's checklist of personally required keys, ordered by name. The
// checklist is included only when withChecklist is set.
func ownEntries(ctx context.Context, r store.ReadRepos, p authz.Proof, env domain.EnvID, withChecklist bool) ([]PersonalEntry, error) {
	keys, err := r.Catalogue().List(ctx, p)
	if err != nil {
		return nil, err
	}
	required, err := r.Catalogue().PersonalRequired(ctx, p)
	if err != nil {
		return nil, err
	}
	requiredHere := map[string]bool{}
	for _, row := range required {
		if withChecklist && row.EnvironmentID == string(env) {
			requiredHere[row.KeyID] = true
		}
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
		if !set && cause == "" && !requiredHere[k.ID] {
			continue
		}
		out = append(out, PersonalEntry{KeyID: k.ID, Name: k.Name, Set: set, UpdatedAt: v.UpdatedAt, Notice: cause,
			Required: requiredHere[k.ID], Suspended: !v.SuspendedAt.IsZero(), SuspendedAt: v.SuspendedAt})
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

// Set stores or replaces the caller's own value for a declared key. Schema
// rules only warn the owner (never refuse or publish it), and it is never
// scanned.
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
		if err := refusePersonalForbidden(ctx, r, p, key, scope.Env); err != nil {
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
		if limit := s.maxPerProject(); !replacing && count >= limit {
			return PersonalEntry{}, fmt.Errorf("%w: a project holds at most %d personal values", domain.ErrLimitExceeded, limit)
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
		// Warnings are computed after the audit event, so the event is the
		// same whether or not the value breaks a rule.
		warnings, err := personalWarnings(key, value)
		if err != nil {
			return PersonalEntry{}, err
		}
		return PersonalEntry{KeyID: key.ID, Name: key.Name, Set: true, UpdatedAt: at, Warnings: warnings}, nil
	})
}

// refusePersonalForbidden refuses a personal value for a key forbidden_in
// the environment: a forbidden key holds no value, personal or shared.
func refusePersonalForbidden(ctx context.Context, r store.Repos, p authz.Proof, key store.CatalogueKey, env domain.EnvID) error {
	presence, err := r.Catalogue().ListPresence(ctx, p)
	if err != nil {
		return err
	}
	if presenceOf(key.ID, key.RequiredMode, key.ForbiddenMode, presence).Forbidden.Covers(string(env)) {
		return invalidDetail("key %q is forbidden in this environment", key.Name)
	}
	return nil
}

// personalWarnings names the schema rules value does not satisfy. It is the
// owner's advice only: no schema rule ever refuses a personal value, the
// warnings never reach an audit record, and they name rules, never the value.
func personalWarnings(key store.CatalogueKey, value string) ([]string, error) {
	decl, err := schema.ParseDeclaration([]byte(key.Declaration))
	if err != nil {
		return nil, fmt.Errorf("service: key %s: stored declaration unreadable: %w", key.ID, err)
	}
	compiled, err := schema.CompileClassified(schema.Classification(key.Classification), decl)
	if err != nil {
		return nil, fmt.Errorf("service: key %s: stored declaration does not compile: %w", key.ID, err)
	}
	var out []string
	for _, f := range compiled.Validate(value).Errors {
		if !slices.Contains(out, f.Keyword) {
			out = append(out, f.Keyword)
		}
	}
	return out, nil
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
		warnings, err := personalWarnings(key, string(plain))
		if err != nil {
			return PersonalEntry{}, err
		}
		return PersonalEntry{KeyID: key.ID, Name: key.Name, Set: true, UpdatedAt: row.UpdatedAt, Value: string(plain), Warnings: warnings}, nil
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

// cascadePersonalForbidden deletes the key's personal values in every
// environment its forbidden_in rule now covers, leaving each owner a notice
// with cause `forbidden`. A forbidden key can hold no value, personal or not.
func cascadePersonalForbidden(ctx context.Context, r store.Repos, p authz.Proof, actor domain.PrincipalID, key store.CatalogueKey, forbidden schema.Presence) error {
	if forbidden.Mode == schema.PresenceNone {
		return nil
	}
	cells, err := r.PersonalValues().CellsForKey(ctx, p, key.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var envs []string
	for _, c := range cells {
		if !forbidden.Covers(c.EnvironmentID) {
			continue
		}
		if !slices.Contains(envs, c.EnvironmentID) {
			envs = append(envs, c.EnvironmentID)
		}
		if err := r.PersonalValues().RecordNotice(ctx, p, c.EnvironmentID, key.Name, c.Owner, PersonalCauseForbidden, now); err != nil {
			return err
		}
		ev, err := personalEvent(ctx, audit.EventPersonalValueDeleted, actor, key, c.Owner, audit.Payload{"cause": PersonalCauseForbidden})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, p, ev); err != nil {
			return err
		}
	}
	for _, env := range envs {
		if _, err := r.PersonalValues().DeleteForKeyInEnvironment(ctx, p, key.ID, env); err != nil {
			return err
		}
	}
	return nil
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

// PersonalRetentionWindow is how long a personal value survives its owner
// losing the access predicate before the retention sweep deletes it.
const PersonalRetentionWindow = 14 * 24 * time.Hour

const personalRetentionBatch = 100

// SweepRetention is the scheduler's retention job (personal-values ADR,
// Retention). It checks each value's owner predicate live: read and reveal on
// the environment, and qualification. It records suspended_at when it first
// sees the predicate missing, clears it when the predicate returns, and
// deletes the value once it has been suspended for the retention window,
// leaving the owner a `retention` notice. Delivery does not wait for it: the
// developer fetch evaluates the same predicate on every request.
func (s *PersonalValues) SweepRetention(ctx context.Context) error {
	cursor := ""
	for {
		next, n, err := s.sweepRetentionBatch(ctx, cursor)
		if err != nil || n < personalRetentionBatch {
			return err
		}
		cursor = next
	}
}

func (s *PersonalValues) sweepRetentionBatch(ctx context.Context, cursor string) (string, int, error) {
	now := s.now()
	cutoff := now.Add(-PersonalRetentionWindow)
	var next string
	var n int
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
		if err != nil {
			return err
		}
		rows, err := r.PersonalValues().RetentionPage(ctx, p, cursor, personalRetentionBatch)
		if err != nil {
			return err
		}
		n = len(rows)
		holds := map[string]bool{}
		for _, row := range rows {
			next = row.ID
			scope := domain.Scope{Org: domain.OrgID(row.OrgID), Project: domain.ProjectID(row.ProjectID), Env: domain.EnvID(row.EnvironmentID)}
			memo := string(row.Owner) + "\x00" + row.EnvironmentID
			ok, seen := holds[memo]
			if !seen {
				if ok, err = personalOwnerPredicate(ctx, az, row.Owner, scope); err != nil {
					return err
				}
				holds[memo] = ok
			}
			scoped, err := az.ScopedSystemAuthority(ctx, authz.SiteScheduler, scope)
			if err != nil {
				return err
			}
			switch {
			case ok && !row.SuspendedAt.IsZero():
				_, err = r.PersonalValues().MarkSuspended(ctx, scoped, row.ID, time.Time{})
			case !ok && row.SuspendedAt.IsZero():
				_, err = r.PersonalValues().MarkSuspended(ctx, scoped, row.ID, now)
			case !ok && !row.SuspendedAt.After(cutoff):
				err = retainPersonalExpired(ctx, r, scoped, row, cutoff, now)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	return next, n, err
}

// personalOwnerPredicate is the retention predicate: read and reveal on the
// environment, and a standing possession factor. An owner with no account is
// not qualified.
func personalOwnerPredicate(ctx context.Context, az *authz.TxAuthorizer, owner domain.PrincipalID, scope domain.Scope) (bool, error) {
	ok, err := az.PersonalOwnerHasAccess(ctx, owner, scope)
	if err != nil || !ok {
		return false, err
	}
	ok, err = personalQualified(ctx, az, owner)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	return ok, err
}

// retainPersonalExpired deletes one value suspended past the window, leaving
// the owner a `retention` notice and a system-actor audit event.
func retainPersonalExpired(ctx context.Context, r store.Repos, p authz.Proof, row store.PersonalRetentionRow, cutoff, now time.Time) error {
	deleted, err := r.PersonalValues().DeleteExpired(ctx, p, row.ID, cutoff)
	if err != nil || !deleted {
		return err
	}
	if err := r.PersonalValues().RecordNotice(ctx, p, row.EnvironmentID, row.KeyName, row.Owner, PersonalCauseRetention, now); err != nil {
		return err
	}
	ev, err := newAuditEvent(ctx, audit.EventPersonalValueDeleted, "", audit.Object{Type: "key", ID: row.KeyID}, audit.OutcomeSuccess, "",
		audit.Payload{"key_id": row.KeyID, "name": audit.SanitizeFreeText(row.KeyName), "owner_principal_id": string(row.Owner), "cause": PersonalCauseRetention})
	if err != nil {
		return err
	}
	ev.Actor.Class = audit.ActorSystem
	ev.OccurredAt = now
	return r.Audit().InsertTenant(ctx, p, ev)
}

// personalCountForEnvironment counts one environment's personal values for a
// definitions deletion plan. Metadata only: it keeps the plan off the
// personal repo, whose callers are pinned.
func personalCountForEnvironment(ctx context.Context, r store.Repos, p authz.Proof, environmentID string) (int64, error) {
	return r.PersonalValues().CountForEnvironment(ctx, p, environmentID)
}
