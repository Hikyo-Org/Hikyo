package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/transit"
)

// Transit is the transit surface (#156, transit ADR): environment-scoped named
// keys whose material the caller never holds. Key management rides
// crypto-manage; the data plane rides crypto-use and is then narrowed by the
// key's own policy (state, version window, compromise, allowed operations and
// per-key caller entries), read inside the operation's own transaction.
//
// Nothing about a key is cached between requests: every operation re-reads
// the key row (under a shared row lock on postgres), so a disable, compromise
// or deletion on any node binds the next request on every node.
type Transit struct {
	DB      *store.DB
	Keyring *crypto.Keyring
	Custody *transit.Registry
	Budget  *Budget
	// Shared, when set (multi-node HA), charges the data-plane rate against
	// the installation-wide admission counters instead of this node's budget.
	Shared TransitWindowCounter
	Now    func() time.Time
}

// TransitWindowCounter is the slice of the #146 shared admission store the
// transit rate limit needs. *store.Coordination satisfies it.
type TransitWindowCounter interface {
	BumpWindow(ctx context.Context, bucket, subject string, window time.Time) (int64, error)
}

// now returns the configured clock in UTC, or the current UTC time.
func (s *Transit) now() time.Time { return nowOr(s.Now) }

// Transit ADR D2 / D9 vocabularies and bounds.
const (
	TransitOpEncrypt          = "encrypt"
	TransitOpDecrypt          = "decrypt"
	TransitOpRewrap           = "rewrap"
	TransitOpDataKey          = "datakey"
	TransitOpDataKeyPlaintext = "datakey-plaintext"
	TransitOpSign             = "sign"
	TransitOpVerify           = "verify"
	TransitOpHMAC             = "hmac"
	TransitOpHMACVerify       = "hmac-verify"

	TransitStateActive          = "active"
	TransitStateRetired         = "retired"
	TransitStateDisabled        = "disabled"
	TransitStatePendingDeletion = "pending-deletion"
	TransitStateDestroyed       = "destroyed"

	MaxTransitKeysPerEnvironment = 256
	MaxTransitVersionsPerKey     = 1024
	MaxTransitCallersPerKey      = 64
	MinTransitRotationPeriod     = time.Hour
	MaxTransitRotationPeriod     = 10 * 365 * 24 * time.Hour
	MinTransitDeletionDelay      = 24 * time.Hour
	MaxTransitDeletionDelay      = 90 * 24 * time.Hour
	DefaultTransitDeletionDelay  = 7 * 24 * time.Hour
	DefaultTransitDataKeyBits    = 256
	transitSweepBatch            = 100
)

// transitAlgorithmOps is D2's table: the operations each algorithm can ever
// perform. A key's allowed set is a subset fixed at creation.
var transitAlgorithmOps = map[crypto.TransitAlgorithm][]string{
	crypto.TransitXChaCha20Poly1305: {TransitOpEncrypt, TransitOpDecrypt, TransitOpRewrap, TransitOpDataKey, TransitOpDataKeyPlaintext},
	crypto.TransitEd25519:           {TransitOpSign, TransitOpVerify},
	crypto.TransitHMACSHA256:        {TransitOpHMAC, TransitOpHMACVerify},
}

// transitProducing are the operations that create new ciphertext, signatures,
// MACs or data keys: active state only, version inside
// [min_encrypt_version, latest] and above the compromise mark.
var transitProducing = map[string]bool{
	TransitOpEncrypt: true, TransitOpDataKey: true, TransitOpDataKeyPlaintext: true,
	TransitOpSign: true, TransitOpHMAC: true, TransitOpRewrap: true,
}

var transitKeyName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

var (
	// ErrTransitForbidden is a post-authorization refusal by the key's own
	// policy: the key does not allow the operation, or its caller entries
	// exclude the caller. It renders 403 and names nothing.
	ErrTransitForbidden = fmt.Errorf("%w: the key's policy does not permit this caller or operation", domain.ErrUnauthorized)
	// ErrTransitCustodyUnavailable is the fail-closed answer when the key's
	// custody provider cannot serve. It never falls back to another provider.
	ErrTransitCustodyUnavailable = fmt.Errorf("transit: %w", transit.ErrUnavailable)
)

// transitRefusal is a 409 whose detail names the closed refusal cause. It is
// decided after authorization and names only the key's own policy.
type transitRefusal struct{ cause string }

func (e *transitRefusal) Error() string {
	return "service: transit key refused the operation: " + e.cause
}
func (e *transitRefusal) Unwrap() error      { return domain.ErrConflict }
func (e *transitRefusal) SafeDetail() string { return e.cause }

// transitInvalid is a 400 whose detail names which input failed. It never
// echoes the input.
type transitInvalid struct{ detail string }

func (e *transitInvalid) Error() string      { return "service: transit input refused: " + e.detail }
func (e *transitInvalid) Unwrap() error      { return domain.ErrInvalid }
func (e *transitInvalid) SafeDetail() string { return e.detail }

// invalidTransit formats a validation error whose detail may be returned to
// callers; format and args must not contain secret input.
func invalidTransit(format string, args ...any) error {
	return &transitInvalid{detail: fmt.Sprintf(format, args...)}
}

// ---- Views ---------------------------------------------------------------

// TransitCallerEntry is one per-key caller entry on the request and view side.
type TransitCallerEntry struct {
	PrincipalID string
	Operations  []string
}

// TransitKeyView is a key's metadata: never material, never a data-plane
// output.
type TransitKeyView struct {
	store.TransitKeyRecord
	Versions    []store.TransitVersionRecord
	Callers     []store.TransitCaller
	RotationDue bool
}

// CreateTransitKeyRequest creates a named key (ADR D1-D3, D7).
type CreateTransitKeyRequest struct {
	Name                  string
	Algorithm             string
	Custody               string
	AllowedOperations     []string
	RotationPeriodSeconds int64
	Exportable            bool
	Callers               []TransitCallerEntry
}

// ConfigureTransitKeyRequest changes mutable policy; nil fields are unchanged.
type ConfigureTransitKeyRequest struct {
	MinEncryptVersion     *uint32
	MinDecryptVersion     *uint32
	RotationPeriodSeconds *int64
	Callers               *[]TransitCallerEntry
}

// TransitOutput is a data-plane result. Value is the caller-facing ciphertext,
// signature or MAC; Plaintext is set only by decrypt and datakey-plaintext and
// is display-once. Valid is set by the two verify operations.
type TransitOutput struct {
	KeyVersion uint32
	Value      string
	Plaintext  []byte
	Valid      bool
}

// ---- Validation ----------------------------------------------------------

// errTransitScope is the refusal for a call outside one environment.
const errTransitScope = "transit keys are addressed within one environment"

// requireKeyAddress is the entry check of every method that addresses one key:
// an environment scope and a grammatical name. It runs before the name can
// reach a query or an audit object.
func requireKeyAddress(scope domain.Scope, name string) error {
	if err := requireEnvScope(scope, errTransitScope); err != nil {
		return err
	}
	return checkTransitName(name)
}

// checkTransitName returns an invalid-input error unless name is 1 to 63
// lowercase ASCII letters, digits, dots, underscores, or hyphens, starting with
// a letter or digit.
func checkTransitName(name string) error {
	if !transitKeyName.MatchString(name) {
		return invalidTransit("name must match %s", transitKeyName.String())
	}
	return nil
}

// checkTransitOps rejects duplicate operations and operations unsupported by
// alg. An empty set is accepted; what labels the set in validation errors.
func checkTransitOps(alg crypto.TransitAlgorithm, ops []string, what string) error {
	allowed := transitAlgorithmOps[alg]
	seen := map[string]bool{}
	for _, op := range ops {
		if !slices.Contains(allowed, op) {
			return invalidTransit("%s names %q, which a %s key cannot perform", what, op, alg)
		}
		if seen[op] {
			return invalidTransit("%s names %q twice", what, op)
		}
		seen[op] = true
	}
	return nil
}

// checkRotationPeriod accepts zero (disabled) or a period from one hour
// through ten 365-day years, in seconds; other values return an invalid-input error.
func checkRotationPeriod(seconds int64) error {
	if seconds == 0 {
		return nil
	}
	// Compare in seconds: converting first would overflow time.Duration for
	// huge inputs and could wrap into the permitted range.
	if seconds < int64(MinTransitRotationPeriod/time.Second) || seconds > int64(MaxTransitRotationPeriod/time.Second) {
		return invalidTransit("rotation_period_seconds must be 0 or between %d and %d", int64(MinTransitRotationPeriod/time.Second), int64(MaxTransitRotationPeriod/time.Second))
	}
	return nil
}

// transitPrincipalIDGrammar is the API's ID grammar (api/openapi.yaml ID).
// A caller entry naming anything else could never match a principal.
var transitPrincipalIDGrammar = regexp.MustCompile(`^[a-z]{2,8}_[0-9a-fA-F-]{36}$`)

// checkTransitCallers validates the entry limit, principal ID grammar and
// uniqueness, and nonempty operation subsets, then copies entries for storage.
// It does not check principal existence. An empty list imposes no per-key
// caller restriction.
func checkTransitCallers(callers []TransitCallerEntry, allowed []string) ([]store.TransitCaller, error) {
	if len(callers) > MaxTransitCallersPerKey {
		return nil, invalidTransit("at most %d caller entries per key", MaxTransitCallersPerKey)
	}
	seen := map[string]bool{}
	out := make([]store.TransitCaller, 0, len(callers))
	for _, c := range callers {
		if !transitPrincipalIDGrammar.MatchString(c.PrincipalID) {
			return nil, invalidTransit("a caller entry needs a principal id (a prefixed UUIDv7)")
		}
		if seen[c.PrincipalID] {
			return nil, invalidTransit("principal %s has two caller entries", c.PrincipalID)
		}
		seen[c.PrincipalID] = true
		if len(c.Operations) == 0 {
			return nil, invalidTransit("caller entry for %s names no operation", c.PrincipalID)
		}
		for _, op := range c.Operations {
			if !slices.Contains(allowed, op) {
				return nil, invalidTransit("caller entry for %s names %q, which the key does not allow", c.PrincipalID, op)
			}
		}
		out = append(out, store.TransitCaller{PrincipalID: c.PrincipalID, Operations: slices.Clone(c.Operations)})
	}
	return out, nil
}

// transitRotationDue reports whether an active key with automatic rotation
// has reached its deadline. A missing latest-version timestamp is not due.
func transitRotationDue(k store.TransitKeyRecord, latestCreated time.Time, now time.Time) bool {
	return k.State == TransitStateActive && k.RotationPeriodSeconds > 0 && !latestCreated.IsZero() &&
		!latestCreated.Add(time.Duration(k.RotationPeriodSeconds)*time.Second).After(now)
}

// transitTarget binds a key version and storage row to scope for custody
// operations. An unrecognized stored algorithm returns an error.
func transitTarget(scope domain.Scope, k store.TransitKeyRecord, version uint32, versionRowID string) (transit.Target, error) {
	alg, err := crypto.ParseTransitAlgorithm(k.Algorithm)
	if err != nil {
		return transit.Target{}, err
	}
	return transit.Target{
		Binding: crypto.TransitBinding{
			Algorithm: alg, OrgID: string(scope.Org), ProjectID: string(scope.Project),
			EnvID: string(scope.Env), KeyID: k.ID, Version: version,
		},
		VersionRowID: versionRowID,
	}, nil
}

// transitVersion exposes stored material references and public metadata to
// custody; it does not decrypt sealed material.
func transitVersion(m store.TransitVersionMaterial) transit.Version {
	return transit.Version{Sealed: m.Sealed, ExternalRef: m.ExternalRef, PublicKey: m.PublicKey}
}

// preflight authorizes op for scope in a bare read transaction before any key
// material is created outside a transaction: a caller the formula refuses
// never makes the keyring mint a project DEK or a provider mint material. The
// real transaction re-authorizes; nothing but the refusal crosses over.
func (s *Transit) preflight(ctx context.Context, actor Actor, op authz.Operation, scope domain.Scope) error {
	return tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		_, _, err := authorize(ctx, az, actor, op, scope, s.now())
		return err
	})
}

// createVersion makes material for one new version at the key's custody
// provider. The returned cleanup destroys external material when the
// transaction that should record it does not commit. Cleanup is best effort
// and ignores destroy errors. Resolution and provider-unavailable errors become
// ErrTransitCustodyUnavailable; other creation errors propagate.
func (s *Transit) createVersion(ctx context.Context, custodyKind string, t transit.Target) (transit.Version, func(), error) {
	kind, err := transit.ParseCustodyKind(custodyKind)
	if err != nil {
		return transit.Version{}, nil, err
	}
	p, err := s.Custody.Resolve(kind)
	if err != nil {
		return transit.Version{}, nil, ErrTransitCustodyUnavailable
	}
	v, err := p.Create(ctx, t)
	if errors.Is(err, transit.ErrUnavailable) {
		return transit.Version{}, nil, ErrTransitCustodyUnavailable
	}
	if err != nil {
		return transit.Version{}, nil, err
	}
	cleanup := func() {
		if v.ExternalRef != "" {
			// Best effort: the reference was never recorded, so nothing can use
			// it; a failed destroy leaves unreachable material at the provider.
			_ = p.Destroy(context.WithoutCancel(ctx), t, v)
		}
	}
	return v, cleanup, nil
}

// fenceSealed checks that the DEK used to seal material is still active,
// returning a conflict if it is stale. Versions without sealed material need
// no DEK fence; other store errors propagate.
func (s *Transit) fenceSealed(ctx context.Context, r store.Repos, p authz.Proof, scope domain.Scope, v transit.Version) error {
	if len(v.Sealed) == 0 {
		return nil
	}
	return fenceProjectVersion(ctx, r, p, domain.Scope{Org: scope.Org, Project: scope.Project}, v.SealedDEKVersion)
}

// ---- Management ----------------------------------------------------------

// CreateKey creates a named key with its first version (ADR D1-D3). Empty
// custody defaults to software; an empty allowed-operation list enables the
// algorithm's operations except datakey-plaintext. Invalid configuration,
// duplicate names, and the environment key limit return errors. Authorization,
// custody, and transaction failures propagate; uncommitted external material
// is destroyed on a best-effort basis.
func (s *Transit) CreateKey(ctx context.Context, actor Actor, scope domain.Scope, req CreateTransitKeyRequest) (TransitKeyView, error) {
	if err := requireEnvScope(scope, errTransitScope); err != nil {
		return TransitKeyView{}, err
	}
	if err := checkTransitName(req.Name); err != nil {
		return TransitKeyView{}, err
	}
	alg, err := crypto.ParseTransitAlgorithm(req.Algorithm)
	if err != nil {
		return TransitKeyView{}, invalidTransit("algorithm must be one of %v", audit.TransitAlgorithmValues)
	}
	if req.Custody == "" {
		req.Custody = string(transit.CustodySoftware)
	}
	custody, err := transit.ParseCustodyKind(req.Custody)
	if err != nil {
		return TransitKeyView{}, invalidTransit("custody must be software or external")
	}
	if !s.Custody.Registered(custody) {
		return TransitKeyView{}, invalidTransit("custody %s is not available on this instance", custody)
	}
	if req.Exportable {
		return TransitKeyView{}, invalidTransit("exportable keys are not supported; key material never leaves custody")
	}
	allowed := req.AllowedOperations
	if len(allowed) == 0 {
		for _, op := range transitAlgorithmOps[alg] {
			if op != TransitOpDataKeyPlaintext {
				allowed = append(allowed, op)
			}
		}
	}
	if err := checkTransitOps(alg, allowed, "allowed_operations"); err != nil {
		return TransitKeyView{}, err
	}
	if err := checkRotationPeriod(req.RotationPeriodSeconds); err != nil {
		return TransitKeyView{}, err
	}
	callers, err := checkTransitCallers(req.Callers, allowed)
	if err != nil {
		return TransitKeyView{}, err
	}
	if err := s.preflight(ctx, actor, authz.OpTransitKeyCreate, scope); err != nil {
		return TransitKeyView{}, err
	}
	keyID, err := newID("tk")
	if err != nil {
		return TransitKeyView{}, err
	}
	versionID, err := newID("tkv")
	if err != nil {
		return TransitKeyView{}, err
	}
	t, err := transitTarget(scope, store.TransitKeyRecord{ID: keyID, Algorithm: string(alg)}, 1, versionID)
	if err != nil {
		return TransitKeyView{}, err
	}
	v, cleanup, err := s.createVersion(ctx, string(custody), t)
	if err != nil {
		return TransitKeyView{}, err
	}
	committed := false
	defer func() {
		if !committed {
			cleanup()
		}
	}()
	now := store.CanonTime(s.now())
	var out TransitKeyView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpTransitKeyCreate, scope, now)
		if err != nil {
			return err
		}
		n, err := r.Transit().CountKeys(ctx, proof)
		if err != nil {
			return err
		}
		if n >= MaxTransitKeysPerEnvironment {
			return fmt.Errorf("%w: at most %d transit keys per environment", domain.ErrLimitExceeded, MaxTransitKeysPerEnvironment)
		}
		if err := s.fenceSealed(ctx, r, proof, scope, v); err != nil {
			return err
		}
		record, err := r.Transit().CreateKey(ctx, proof, store.TransitKeyCreate{
			ID: keyID, Name: req.Name, Algorithm: string(alg), Custody: string(custody),
			AllowedOperations: allowed, RotationPeriodSeconds: req.RotationPeriodSeconds,
			CreatedBy: string(caller.Principal), Callers: callers,
			First: store.TransitVersionCreate{ID: versionID, Version: 1, Sealed: v.Sealed, ExternalRef: v.ExternalRef, PublicKey: v.PublicKey, At: now},
			At:    now,
		})
		if errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("%w: a transit key named %q already exists in this environment", domain.ErrConflict, req.Name)
		}
		if err != nil {
			return err
		}
		sortedOps := slices.Clone(record.AllowedOperations)
		ev, err := domainEvent(ctx, audit.EventTransitKeyCreated, caller.Principal,
			audit.Object{Type: "transit-key", ID: keyID}, audit.Payload{
				"algorithm": string(alg), "custody": string(custody),
				"allowed_operations": sortedOps, "rotation_period_seconds": req.RotationPeriodSeconds,
				"caller_count": int64(len(callers)),
			})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, proof, ev); err != nil {
			return err
		}
		out = TransitKeyView{TransitKeyRecord: record, Callers: callers, Versions: []store.TransitVersionRecord{{
			ID: versionID, Version: 1, PublicKey: v.PublicKey, HasMaterial: len(v.Sealed) > 0,
			ExternalHeld: v.ExternalRef != "", CreatedAt: now.Format(time.RFC3339Nano),
		}}}
		return nil
	})
	if err == nil {
		committed = true
	}
	return out, err
}

// ListKeys lists an environment's non-destroyed keys by name (metadata only),
// including versions, callers, and rotation status. Scope, authorization, and
// store errors propagate.
func (s *Transit) ListKeys(ctx context.Context, actor Actor, scope domain.Scope) ([]TransitKeyView, error) {
	if err := requireEnvScope(scope, errTransitScope); err != nil {
		return nil, err
	}
	var out []TransitKeyView
	now := s.now()
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpTransitKeyInspect, scope, now)
		if err != nil {
			return err
		}
		keys, err := r.Transit().ListKeys(ctx, proof)
		if err != nil {
			return err
		}
		out = make([]TransitKeyView, 0, len(keys))
		for _, k := range keys {
			view, err := s.keyView(ctx, r.Transit(), proof, k, now)
			if err != nil {
				return err
			}
			out = append(out, view)
		}
		return nil
	})
	return out, err
}

// GetKey returns one key's metadata, versions and caller entries. Missing or
// destroyed keys return a not-found error; address, authorization, and store
// errors propagate.
func (s *Transit) GetKey(ctx context.Context, actor Actor, scope domain.Scope, name string) (TransitKeyView, error) {
	if err := requireKeyAddress(scope, name); err != nil {
		return TransitKeyView{}, err
	}
	var out TransitKeyView
	now := s.now()
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpTransitKeyInspect, scope, now)
		if err != nil {
			return err
		}
		k, err := r.Transit().GetKey(ctx, proof, name)
		if err != nil {
			return err
		}
		out, err = s.keyView(ctx, r.Transit(), proof, k, now)
		return err
	})
	return out, err
}

// keyView loads versions and caller entries and computes rotation status.
// Store errors propagate; an unparseable latest-version timestamp leaves
// RotationDue false.
func (s *Transit) keyView(ctx context.Context, r store.TransitReader, proof authz.Proof, k store.TransitKeyRecord, now time.Time) (TransitKeyView, error) {
	versions, err := r.ListVersions(ctx, proof, k.ID)
	if err != nil {
		return TransitKeyView{}, err
	}
	callers, err := r.ListCallers(ctx, proof, k.ID)
	if err != nil {
		return TransitKeyView{}, err
	}
	view := TransitKeyView{TransitKeyRecord: k, Versions: versions, Callers: callers}
	for _, v := range versions {
		if v.Version == k.LatestVersion {
			if created, err := time.Parse(time.RFC3339Nano, v.CreatedAt); err == nil {
				view.RotationDue = transitRotationDue(k, created, now)
			}
		}
	}
	return view, nil
}

// viewUnder reads a key's full view under a management operation's own
// authority, so a manager who holds crypto-manage but not read still sees the
// result of what they changed.
func (s *Transit) viewUnder(ctx context.Context, actor Actor, op authz.Operation, scope domain.Scope, name string) (TransitKeyView, error) {
	var out TransitKeyView
	now := s.now()
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, op, scope, now)
		if err != nil {
			return err
		}
		k, err := r.Transit().GetKey(ctx, proof, name)
		if err != nil {
			return err
		}
		out, err = s.keyView(ctx, r.Transit(), proof, k, now)
		return err
	})
	return out, err
}

// readForManage reads a key under a management operation's authority in its
// own read transaction (the metadata a pre-transaction step needs).
func (s *Transit) readForManage(ctx context.Context, actor Actor, op authz.Operation, scope domain.Scope, name string) (store.TransitKeyRecord, error) {
	var out store.TransitKeyRecord
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, op, scope, s.now())
		if err != nil {
			return err
		}
		out, err = r.Transit().GetKey(ctx, proof, name)
		return err
	})
	return out, err
}

// RotateKey appends a new version (ADR D5), including for retired or disabled
// keys. Pending deletion is refused; concurrent rotation and the retained-version
// limit return conflict and limit errors. Authorization, custody, and store
// errors propagate. Rotation remains committed if reading the updated view fails.
func (s *Transit) RotateKey(ctx context.Context, actor Actor, scope domain.Scope, name string) (TransitKeyView, error) {
	if err := requireKeyAddress(scope, name); err != nil {
		return TransitKeyView{}, err
	}
	k, err := s.readForManage(ctx, actor, authz.OpTransitKeyRotate, scope, name)
	if err != nil {
		return TransitKeyView{}, err
	}
	_, err = s.appendVersion(ctx, scope, k, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, now time.Time) (authz.Proof, domain.PrincipalID, error) {
		caller, proof, err := authorize(ctx, az, actor, authz.OpTransitKeyRotate, scope, now)
		return proof, caller.Principal, err
	}, "operator")
	if err != nil {
		return TransitKeyView{}, err
	}
	return s.viewUnder(ctx, actor, authz.OpTransitKeyRotate, scope, name)
}

// appendVersion is the shared tail of operator and scheduled rotation: create
// material for latest+1 outside the transaction, then append under the
// compare-and-swap with the writer fence and the rotation event inside it.
func (s *Transit) appendVersion(ctx context.Context, scope domain.Scope, k store.TransitKeyRecord,
	auth func(context.Context, store.Repos, *authz.TxAuthorizer, time.Time) (authz.Proof, domain.PrincipalID, error), trigger string,
) (store.TransitKeyRecord, error) {
	if k.State == TransitStatePendingDeletion || k.State == TransitStateDestroyed {
		return store.TransitKeyRecord{}, &transitRefusal{cause: "state"}
	}
	next := k.LatestVersion + 1
	versionID, err := newID("tkv")
	if err != nil {
		return store.TransitKeyRecord{}, err
	}
	t, err := transitTarget(scope, k, next, versionID)
	if err != nil {
		return store.TransitKeyRecord{}, err
	}
	v, cleanup, err := s.createVersion(ctx, k.Custody, t)
	if err != nil {
		return store.TransitKeyRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			cleanup()
		}
	}()
	now := store.CanonTime(s.now())
	var out store.TransitKeyRecord
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		proof, principal, err := auth(ctx, r, az, now)
		if err != nil {
			return err
		}
		if err := s.fenceSealed(ctx, r, proof, scope, v); err != nil {
			return err
		}
		err = r.Transit().AppendVersion(ctx, proof, k.ID, k.LatestVersion, MaxTransitVersionsPerKey, store.TransitVersionCreate{
			ID: versionID, Version: next, Sealed: v.Sealed, ExternalRef: v.ExternalRef, PublicKey: v.PublicKey, At: now,
		})
		if errors.Is(err, store.ErrTransitVersionLimit) {
			return fmt.Errorf("%w: at most %d versions per key; raise min_decrypt_version and trim first", domain.ErrLimitExceeded, MaxTransitVersionsPerKey)
		}
		if errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("%w: the key was rotated or changed concurrently; retry", domain.ErrConflict)
		}
		if err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventTransitKeyRotated, principal,
			audit.Object{Type: "transit-key", ID: k.ID}, audit.Payload{"version": int64(next), "trigger": trigger})
		if err != nil {
			return err
		}
		if trigger == "schedule" {
			ev.Actor.Class = audit.ActorSystem
		}
		if err := r.Audit().InsertTenant(ctx, proof, ev); err != nil {
			return err
		}
		out = k
		out.LatestVersion = next
		return nil
	})
	if err == nil {
		committed = true
	}
	return out, err
}

// ConfigureKey changes a key's version window, rotation period and caller
// entries (ADR D5, D7). Nil fields are unchanged; a present empty caller list
// clears restrictions, and a zero rotation period disables automatic rotation.
// Invalid version bounds or periods are refused. Authorization and store errors
// propagate; the update remains committed if reading the resulting view fails.
func (s *Transit) ConfigureKey(ctx context.Context, actor Actor, scope domain.Scope, name string, req ConfigureTransitKeyRequest) (TransitKeyView, error) {
	if err := requireKeyAddress(scope, name); err != nil {
		return TransitKeyView{}, err
	}
	now := store.CanonTime(s.now())
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpTransitKeyConfigure, scope, now)
		if err != nil {
			return err
		}
		k, err := r.Transit().GetKey(ctx, proof, name)
		if err != nil {
			return err
		}
		m := store.TransitKeyConfig{
			KeyID: k.ID, MinEncryptVersion: k.MinEncryptVersion, MinDecryptVersion: k.MinDecryptVersion,
			RotationPeriodSeconds: k.RotationPeriodSeconds, At: now,
		}
		if req.MinEncryptVersion != nil {
			m.MinEncryptVersion = *req.MinEncryptVersion
		}
		if req.MinDecryptVersion != nil {
			m.MinDecryptVersion = *req.MinDecryptVersion
		}
		if req.RotationPeriodSeconds != nil {
			if err := checkRotationPeriod(*req.RotationPeriodSeconds); err != nil {
				return err
			}
			m.RotationPeriodSeconds = *req.RotationPeriodSeconds
		}
		if m.MinDecryptVersion < 1 || m.MinDecryptVersion > m.MinEncryptVersion || m.MinEncryptVersion > k.LatestVersion {
			return invalidTransit("versions must satisfy 1 <= min_decrypt_version <= min_encrypt_version <= latest_version (%d)", k.LatestVersion)
		}
		if m.MinDecryptVersion < k.MinAvailableVersion {
			return invalidTransit("versions below %d are trimmed; min_decrypt_version cannot go below it", k.MinAvailableVersion)
		}
		var callers []store.TransitCaller
		if req.Callers != nil {
			callers, err = checkTransitCallers(*req.Callers, k.AllowedOperations)
			if err != nil {
				return err
			}
			m.Callers = &callers
		}
		if err := r.Transit().Configure(ctx, proof, m); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return &transitRefusal{cause: "state"}
			}
			return err
		}
		var callerCount int64
		if req.Callers != nil {
			callerCount = int64(len(callers))
		} else {
			existing, err := r.Transit().ListCallers(ctx, proof, k.ID)
			if err != nil {
				return err
			}
			callerCount = int64(len(existing))
		}
		ev, err := domainEvent(ctx, audit.EventTransitKeyConfigured, caller.Principal,
			audit.Object{Type: "transit-key", ID: k.ID}, audit.Payload{
				"min_encrypt_version": int64(m.MinEncryptVersion), "min_decrypt_version": int64(m.MinDecryptVersion),
				"rotation_period_seconds": m.RotationPeriodSeconds, "caller_count": callerCount,
				"callers_changed": req.Callers != nil,
			})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, proof, ev)
	})
	if err != nil {
		return TransitKeyView{}, err
	}
	return s.viewUnder(ctx, actor, authz.OpTransitKeyConfigure, scope, name)
}

// transitTransitions is D6's lifecycle table: action -> permitted source
// states and target state. compromise is not a state change and is handled
// separately.
var transitTransitions = map[string]struct {
	from []string
	to   string
}{
	"disable":           {[]string{TransitStateActive, TransitStateRetired}, TransitStateDisabled},
	"enable":            {[]string{TransitStateDisabled, TransitStateRetired}, TransitStateActive},
	"retire":            {[]string{TransitStateActive, TransitStateDisabled}, TransitStateRetired},
	"schedule-deletion": {[]string{TransitStateActive, TransitStateRetired, TransitStateDisabled}, TransitStatePendingDeletion},
	"cancel-deletion":   {[]string{TransitStatePendingDeletion}, TransitStateDisabled},
}

// ChangeKeyState applies one lifecycle action (ADR D6). delay applies to
// schedule-deletion only; zero means seven days, and the accepted range is
// 24 hours through 90 days. Canceling deletion leaves the key disabled and is
// refused once the deadline has elapsed or purging has started. Compromise marks
// all current versions without changing state. Validation, authorization, and
// store errors propagate; a later view-read failure does not undo the change.
func (s *Transit) ChangeKeyState(ctx context.Context, actor Actor, scope domain.Scope, name, action string, delay time.Duration) (TransitKeyView, error) {
	if err := requireKeyAddress(scope, name); err != nil {
		return TransitKeyView{}, err
	}
	transition, isTransition := transitTransitions[action]
	if !isTransition && action != "compromise" {
		return TransitKeyView{}, invalidTransit("action must be one of %v", audit.TransitLifecycleActions)
	}
	if action != "schedule-deletion" && delay != 0 {
		return TransitKeyView{}, invalidTransit("delay applies to schedule-deletion only")
	}
	if action == "schedule-deletion" {
		if delay == 0 {
			delay = DefaultTransitDeletionDelay
		}
		if delay < MinTransitDeletionDelay || delay > MaxTransitDeletionDelay {
			return TransitKeyView{}, invalidTransit("deletion delay must be between %s and %s", MinTransitDeletionDelay, MaxTransitDeletionDelay)
		}
	}
	now := store.CanonTime(s.now())
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpTransitKeyLifecycle, scope, now)
		if err != nil {
			return err
		}
		k, err := r.Transit().GetKey(ctx, proof, name)
		if err != nil {
			return err
		}
		payload := audit.Payload{"action": action, "from_state": k.State}
		if action == "compromise" {
			through, err := r.Transit().Compromise(ctx, proof, k.ID, now)
			if err != nil {
				return err
			}
			k.CompromisedThroughVersion = through
			payload["to_state"] = k.State
			payload["compromised_through_version"] = int64(through)
		} else {
			change := store.TransitStateChange{KeyID: k.ID, From: transition.from, To: transition.to, At: now}
			if transition.to == TransitStatePendingDeletion {
				change.DeletionAfter = now.Add(delay)
				payload["deletion_after"] = audit.FormatTime(change.DeletionAfter)
			}
			if err := r.Transit().ChangeState(ctx, proof, change); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return &transitRefusal{cause: "state"}
				}
				return err
			}
			payload["to_state"] = transition.to
		}
		ev, err := domainEvent(ctx, audit.EventTransitKeyStateChanged, caller.Principal,
			audit.Object{Type: "transit-key", ID: k.ID}, payload)
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, proof, ev)
	})
	if err != nil {
		return TransitKeyView{}, err
	}
	return s.viewUnder(ctx, actor, authz.OpTransitKeyLifecycle, scope, name)
}

// TrimKey permanently deletes versions below min_decrypt_version (ADR D5).
// It first fences the trim: the key's trim floor (min_available_version) is
// raised to min_decrypt_version, after which ConfigureKey can no longer lower
// min_decrypt_version past it. External custody then destroys the material
// below the floor at the provider; an unavailable provider refuses the trim
// rather than leaving material orphaned. Retries can finish external destruction
// before the version rows below the floor are deleted. It returns
// the updated view and deleted row count. The floor and any external destruction
// persist on later failure; deletion stays committed if reading the view fails.
// Authorization, custody, and store errors propagate.
func (s *Transit) TrimKey(ctx context.Context, actor Actor, scope domain.Scope, name string) (TransitKeyView, int64, error) {
	if err := requireKeyAddress(scope, name); err != nil {
		return TransitKeyView{}, 0, err
	}
	var external []store.TransitVersionMaterial
	var key store.TransitKeyRecord
	var floor uint32
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpTransitKeyTrim, scope, s.now())
		if err != nil {
			return err
		}
		key, err = r.Transit().GetKey(ctx, proof, name)
		if err != nil {
			return err
		}
		floor, err = r.Transit().FenceTrim(ctx, proof, key.ID, store.CanonTime(s.now()))
		if errors.Is(err, store.ErrConflict) {
			return &transitRefusal{cause: "state"}
		}
		if err != nil {
			return err
		}
		external = external[:0]
		if key.Custody != string(transit.CustodyExternal) {
			return nil
		}
		versions, err := r.Transit().ListVersions(ctx, proof, key.ID)
		if err != nil {
			return err
		}
		for _, version := range versions {
			if version.Version >= floor {
				continue
			}
			m, err := r.Transit().VersionMaterial(ctx, proof, key.ID, version.Version)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if m.ExternalRef != "" {
				external = append(external, m)
			}
		}
		return nil
	})
	if err != nil {
		return TransitKeyView{}, 0, err
	}
	if len(external) > 0 {
		p, err := s.Custody.Resolve(transit.CustodyExternal)
		if err != nil {
			return TransitKeyView{}, 0, ErrTransitCustodyUnavailable
		}
		for _, m := range external {
			t, err := transitTarget(scope, key, m.Version, m.ID)
			if err != nil {
				return TransitKeyView{}, 0, err
			}
			if err := p.Destroy(ctx, t, transitVersion(m)); err != nil {
				if errors.Is(err, transit.ErrUnavailable) {
					return TransitKeyView{}, 0, ErrTransitCustodyUnavailable
				}
				return TransitKeyView{}, 0, err
			}
		}
	}
	now := store.CanonTime(s.now())
	var deleted int64
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpTransitKeyTrim, scope, now)
		if err != nil {
			return err
		}
		k, err := r.Transit().GetKey(ctx, proof, name)
		if err != nil {
			return err
		}
		deleted, err = r.Transit().Trim(ctx, proof, k.ID, floor)
		if errors.Is(err, store.ErrConflict) {
			return &transitRefusal{cause: "state"}
		}
		if err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventTransitKeyTrimmed, caller.Principal,
			audit.Object{Type: "transit-key", ID: k.ID}, audit.Payload{
				"min_decrypt_version": int64(floor), "versions_deleted": deleted,
			})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, proof, ev)
	})
	if err != nil {
		return TransitKeyView{}, 0, err
	}
	view, err := s.viewUnder(ctx, actor, authz.OpTransitKeyTrim, scope, name)
	return view, deleted, err
}
