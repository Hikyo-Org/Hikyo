package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/auditrow"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Audit trail repositories (#45, audit-model ADR § Storage and export). The
// application layer holds INSERT and SELECT only on both audit tables — the
// append-only invariant scans the query files; these are the only proof-
// carrying doors to them (the denial writer is the authorization package's
// own enumerated path and does not pass through here).
//
// Chain binding follows the store's universal rule: a tenant event's chain
// is the proof's resolved chain — the caller's Event.Scope is deliberately
// ignored and overwritten, so caller arguments structurally cannot reach a
// chain column. Page reads bind their chain predicates the same way.

// AuditFilter is the normalized filter structure for trail reads. Zero From
// means the epoch; zero To means unbounded (bound to MaxTime at the query).
// AfterSeq is the public allocation-order lower bound; Limit is the page size
// and must be positive (the caller's bound — ops spec owns defaults). Export
// mode adds an internal commit-order cursor without changing AfterSeq.
//
// The time range, AfterSeq lower bound and chain scope are enforced by the
// store's SQL (chain-confined, analyzer-2-provable). The equality fields below
// are applied by Matches AFTER the authorized page read — a projection on rows
// the proof already admitted, never a SQL predicate: analyzer 2 (tenant
// isolation, invariant 8) accepts only `col OP param` conjuncts, so an optional
// equality predicate has no provable SQL shape. An empty field is unset.
type AuditFilter struct {
	RetentionSnapshot time.Time // internal export guard; not a client filter
	From              time.Time
	To                time.Time
	AfterSeq          int64
	ToSeq             int64 // session ceiling: interactive pages never return seq above this
	Limit             int
	Actor             string         // actor_id (the acting principal)
	Type              string         // event type (the operation)
	Outcome           string         // outcome
	ObjectType        string         // object type (a supported resource identifier)
	ObjectID          string         // object id (a supported resource identifier)
	CorrelationID     string         // links INTENT and OUTCOME of one act
	Order             AuditPageOrder // service-controlled page mode; excluded from Normalized
	AfterCommitSeq    AuditCommitSeq // service-controlled export cursor; excluded from Normalized
}

// Matches reports whether a scanned row satisfies the filter's equality fields.
// It is pure and engine-independent by construction, so browser and CLI queries
// over the same filter select the same events regardless of storage engine. The
// time range, cursor and scope are already applied by the SQL that produced e.
func (f AuditFilter) Matches(e AuditEvent) bool {
	switch {
	case f.Actor != "" && e.Actor.ID != f.Actor:
		return false
	case f.Type != "" && string(e.Type) != f.Type:
		return false
	case f.Outcome != "" && string(e.Outcome) != f.Outcome:
		return false
	case f.ObjectType != "" && e.Object.Type != f.ObjectType:
		return false
	case f.ObjectID != "" && e.Object.ID != f.ObjectID:
		return false
	case f.CorrelationID != "" && e.CorrelationID != f.CorrelationID:
		return false
	default:
		return true
	}
}

// AuditPageOrder names the storage order for an audit page.
type AuditPageOrder uint8

const (
	AuditPageBySeq AuditPageOrder = iota
	AuditPageByCommit
)

// AuditCommitSeq is postgres's database-owned export position.
type AuditCommitSeq int64

// auditMaxTime bounds an open-ended To (year 9999 is inside both engines'
// ranges and lexicographically last in the fixed-width text form).
var auditMaxTime = time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)

// AuditMaxPageSize is the page-size ceiling (ops-spec § 10 response caps: list
// endpoints paged, page ≤ 1 000 items; audit pages § 2). A larger requested
// Limit is CLAMPED here, not refused — a page cap is a response-shape bound
// like SCIM's count clamp, applied at the single store chokepoint every audit
// page read routes through.
const AuditMaxPageSize = 1000

// bounds normalizes and validates the filter. It takes a pointer so the page
// clamp it applies is seen by the SQL binding that follows in every caller.
func (f *AuditFilter) bounds() (from, to time.Time, err error) {
	if f.Limit <= 0 {
		return time.Time{}, time.Time{}, errors.New("store: audit page limit must be positive")
	}
	if f.Limit > AuditMaxPageSize {
		f.Limit = AuditMaxPageSize
	}
	if f.Order != AuditPageBySeq && f.Order != AuditPageByCommit {
		return time.Time{}, time.Time{}, fmt.Errorf("store: unknown audit page order %d", f.Order)
	}
	from = f.From.UTC()
	to = f.To.UTC()
	if f.To.IsZero() {
		to = auditMaxTime
	}
	return from, to, nil
}

// Normalized renders the filter as the audit.query event's payload fields —
// the parsed, normalized filter structure, never a raw query string.
func (f AuditFilter) Normalized() audit.Payload {
	p := audit.Payload{"filter_limit": f.Limit}
	if !f.From.IsZero() {
		p["filter_from"] = audit.FormatTime(f.From)
	}
	if !f.To.IsZero() {
		p["filter_to"] = audit.FormatTime(f.To)
	}
	if f.AfterSeq > 0 {
		p["filter_after_seq"] = f.AfterSeq
	}
	if f.ToSeq > 0 {
		p["filter_to_seq"] = f.ToSeq
	}
	if f.Actor != "" {
		p["filter_actor"] = f.Actor
	}
	if f.Type != "" {
		p["filter_type"] = f.Type
	}
	if f.Outcome != "" {
		p["filter_outcome"] = f.Outcome
	}
	if f.ObjectType != "" {
		p["filter_object_type"] = f.ObjectType
	}
	if f.ObjectID != "" {
		p["filter_object_id"] = f.ObjectID
	}
	if f.CorrelationID != "" {
		p["filter_correlation_id"] = f.CorrelationID
	}
	return p
}

// AuditEvent is one stored trail row: the envelope plus its storage-assigned
// seq and recorded_at, and the chain columns as plain strings (read-side
// output — the analyzer's tenant-typed-parameter rule concerns inputs).
type AuditEvent struct {
	audit.Event
	Seq        int64
	CommitSeq  AuditCommitSeq // postgres export cursor; equals Seq on sqlite
	RecordedAt time.Time
	ScopeClass string
	OrgID      string
	ProjectID  string
	EnvID      string
	RawPayload string // schema-versioned JSON as stored; export emits it verbatim
}

// AuditReader is the read side of the trails.
type AuditReader interface {
	// PageTenant returns one bounded page of the tenant trail addressed by
	// the proof's resolved chain (org proofs read the whole org, deeper
	// proofs read their refinement), in the filter's validated order.
	PageTenant(ctx context.Context, p authz.Proof, f AuditFilter) ([]AuditEvent, error)
	// PageInstance returns one bounded page of the instance trail.
	PageInstance(ctx context.Context, p authz.Proof, f AuditFilter) ([]AuditEvent, error)
	// MaxTenantSeq returns the highest seq in the tenant trail addressed by the
	// proof's resolved chain, or 0 when it is empty. It is the interactive
	// query's session ceiling: pinned once and carried by the caller so paging
	// terminates and never chases the audit.query events its own reads append.
	MaxTenantSeq(ctx context.Context, p authz.Proof) (int64, error)
	// MaxInstanceSeq is MaxTenantSeq for the instance trail.
	MaxInstanceSeq(ctx context.Context, p authz.Proof) (int64, error)
}

// AuditRepo adds the two insert doors. Insertion validates against the
// closed registry and fails the surrounding operation on refusal
// (fail-closed — an operation without its durable audit record does not
// complete).
type AuditRepo interface {
	AuditReader
	// InsertTenant writes one tenant-trail event. The event's chain is the
	// proof's resolved chain; the engine assigns recorded_at at its durable
	// insert boundary.
	InsertTenant(ctx context.Context, p authz.Proof, e audit.Event) error
	// InsertInstance writes one instance-trail event.
	InsertInstance(ctx context.Context, p authz.Proof, e audit.Event) error
	// ClaimOfflineRecord atomically claims the principal-scoped client record id.
	// False means a prior reconciliation already inserted it.
	ClaimOfflineRecord(ctx context.Context, p authz.Proof, principalID, recordID string, at time.Time) (bool, error)
}

// --- sqlite ---

type sqliteAudit struct {
	q   *sqlitegen.Queries
	tok *authz.TxToken
}

func (r sqliteRepos) Audit() AuditRepo { return sqliteAudit{q: sqlitegen.New(r.db), tok: r.tok} }

func (a sqliteAudit) InsertTenant(ctx context.Context, p authz.Proof, e audit.Event) error {
	chain, err := authz.VerifyEvent(p, authz.StoreAuditTenantInsert, a.tok, e.Type)
	if err != nil {
		return err
	}
	e.Actor, err = auditrow.ResolveActorClass(ctx, a.q.GetPrincipalKind, e.Actor, authz.IsSystemProof(p))
	if err != nil {
		return err
	}
	row, err := audit.BuildRow(e, audit.TrailTenant, chain, time.Now())
	if err != nil {
		return err
	}
	return a.q.InsertTenantAuditEvent(ctx, auditrow.SQLiteTenant(row))
}

func (a sqliteAudit) InsertInstance(ctx context.Context, p authz.Proof, e audit.Event) error {
	if _, err := authz.VerifyEvent(p, authz.StoreAuditInstanceInsert, a.tok, e.Type); err != nil {
		return err
	}
	var err error
	e.Actor, err = auditrow.ResolveActorClass(ctx, a.q.GetPrincipalKind, e.Actor, authz.IsSystemProof(p))
	if err != nil {
		return err
	}
	row, err := audit.BuildRow(e, audit.TrailInstance, domain.Scope{}, time.Now())
	if err != nil {
		return err
	}
	err = a.q.InsertInstanceAuditEvent(ctx, auditrow.SQLiteInstance(row))
	if sqliteUniqueViolation(err) {
		return fmt.Errorf("%w: duplicate instance audit event", ErrConflict)
	}
	return err
}

func (a sqliteAudit) ClaimOfflineRecord(ctx context.Context, p authz.Proof, principalID, recordID string, at time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StoreAuditClaimOfflineRecord, a.tok); err != nil {
		return false, err
	}
	n, err := a.q.ClaimOfflineRecord(ctx, sqlitegen.ClaimOfflineRecordParams{
		PrincipalID: principalID, RecordID: recordID, CreatedAt: CanonTime(at).Format(timeFormat),
	})
	return n == 1, err
}

func (a sqliteAudit) PageTenant(ctx context.Context, p authz.Proof, f AuditFilter) ([]AuditEvent, error) {
	chain, err := authz.Verify(p, authz.StoreAuditTenantPage, a.tok)
	if err != nil {
		return nil, err
	}
	from, to, err := f.bounds()
	if err != nil {
		return nil, err
	}
	level, err := chain.Level()
	if err != nil {
		return nil, err
	}
	if !f.RetentionSnapshot.IsZero() {
		if err := a.guardRetentionSnapshot(ctx, f.RetentionSnapshot); err != nil {
			return nil, err
		}
	}
	if f.Order == AuditPageByCommit {
		return a.pageTenantExport(ctx, chain, level, f, from, to)
	}
	var rows []sqlitegen.AuditTenantEvent
	switch level {
	case domain.LevelOrg:
		rows, err = a.q.PageTenantAuditOrg(ctx, sqlitegen.PageTenantAuditOrgParams{
			OrgID: string(chain.Org), Seq: f.AfterSeq,
			RecordedAt: audit.FormatTime(from), RecordedAt_2: audit.FormatTime(to),
			Limit: int64(f.Limit),
		})
	case domain.LevelProject:
		rows, err = a.q.PageTenantAuditProject(ctx, sqlitegen.PageTenantAuditProjectParams{
			OrgID: string(chain.Org), ProjectID: sql.NullString{String: string(chain.Project), Valid: true},
			Seq:        f.AfterSeq,
			RecordedAt: audit.FormatTime(from), RecordedAt_2: audit.FormatTime(to),
			Limit: int64(f.Limit),
		})
	case domain.LevelEnv:
		rows, err = a.q.PageTenantAuditEnv(ctx, sqlitegen.PageTenantAuditEnvParams{
			OrgID: string(chain.Org), ProjectID: sql.NullString{String: string(chain.Project), Valid: true},
			EnvID: sql.NullString{String: string(chain.Env), Valid: true}, Seq: f.AfterSeq,
			RecordedAt: audit.FormatTime(from), RecordedAt_2: audit.FormatTime(to),
			Limit: int64(f.Limit),
		})
	default:
		return nil, errors.New("store: tenant audit page with an empty chain")
	}
	if err != nil {
		return nil, err
	}
	out := make([]AuditEvent, 0, len(rows))
	for _, r := range rows {
		ev, err := auditEventFromSQLiteTenant(r)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

func (a sqliteAudit) PageInstance(ctx context.Context, p authz.Proof, f AuditFilter) ([]AuditEvent, error) {
	if _, err := authz.Verify(p, authz.StoreAuditInstancePage, a.tok); err != nil {
		return nil, err
	}
	from, to, err := f.bounds()
	if err != nil {
		return nil, err
	}
	if !f.RetentionSnapshot.IsZero() {
		if err := a.guardRetentionSnapshot(ctx, f.RetentionSnapshot); err != nil {
			return nil, err
		}
	}
	if f.Order == AuditPageByCommit {
		return a.pageInstanceExport(ctx, f, from, to)
	}
	rows, err := a.q.PageInstanceAudit(ctx, sqlitegen.PageInstanceAuditParams{
		Seq:        f.AfterSeq,
		RecordedAt: audit.FormatTime(from), RecordedAt_2: audit.FormatTime(to),
		Limit: int64(f.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]AuditEvent, 0, len(rows))
	for _, r := range rows {
		ev, err := auditEventFromSQLiteInstance(r)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

func (a sqliteAudit) pageTenantExport(ctx context.Context, chain domain.Scope, level domain.Level, f AuditFilter, from, to time.Time) ([]AuditEvent, error) {
	var err error
	var rows []sqlitegen.AuditTenantEvent
	switch level {
	case domain.LevelOrg:
		rows, err = a.q.PageTenantAuditExportOrg(ctx, sqlitegen.PageTenantAuditExportOrgParams{
			ChainOrgID: string(chain.Org), AfterSeq: f.AfterSeq, AfterCommitSeq: int64(f.AfterCommitSeq),
			FromTime: audit.FormatTime(from), ToTime: audit.FormatTime(to), PageLimit: int64(f.Limit),
		})
	case domain.LevelProject:
		rows, err = a.q.PageTenantAuditExportProject(ctx, sqlitegen.PageTenantAuditExportProjectParams{
			ChainOrgID: string(chain.Org), ChainProjectID: sql.NullString{String: string(chain.Project), Valid: true},
			AfterSeq: f.AfterSeq, AfterCommitSeq: int64(f.AfterCommitSeq),
			FromTime: audit.FormatTime(from), ToTime: audit.FormatTime(to), PageLimit: int64(f.Limit),
		})
	case domain.LevelEnv:
		rows, err = a.q.PageTenantAuditExportEnv(ctx, sqlitegen.PageTenantAuditExportEnvParams{
			ChainOrgID: string(chain.Org), ChainProjectID: sql.NullString{String: string(chain.Project), Valid: true},
			ChainEnvID: sql.NullString{String: string(chain.Env), Valid: true},
			AfterSeq:   f.AfterSeq, AfterCommitSeq: int64(f.AfterCommitSeq),
			FromTime: audit.FormatTime(from), ToTime: audit.FormatTime(to), PageLimit: int64(f.Limit),
		})
	default:
		return nil, errors.New("store: tenant audit export page with an empty chain")
	}
	if err != nil {
		return nil, err
	}
	out := make([]AuditEvent, 0, len(rows))
	for _, row := range rows {
		event, err := auditEventFromSQLiteTenant(row)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

func (a sqliteAudit) MaxTenantSeq(ctx context.Context, p authz.Proof) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreAuditTenantMaxSeq, a.tok)
	if err != nil {
		return 0, err
	}
	level, err := chain.Level()
	if err != nil {
		return 0, err
	}
	switch level {
	case domain.LevelOrg:
		return a.q.MaxTenantAuditOrg(ctx, string(chain.Org))
	case domain.LevelProject:
		return a.q.MaxTenantAuditProject(ctx, sqlitegen.MaxTenantAuditProjectParams{
			OrgID: string(chain.Org), ProjectID: sql.NullString{String: string(chain.Project), Valid: true},
		})
	case domain.LevelEnv:
		return a.q.MaxTenantAuditEnv(ctx, sqlitegen.MaxTenantAuditEnvParams{
			OrgID: string(chain.Org), ProjectID: sql.NullString{String: string(chain.Project), Valid: true},
			EnvID: sql.NullString{String: string(chain.Env), Valid: true},
		})
	default:
		return 0, errors.New("store: tenant audit ceiling with an empty chain")
	}
}

func (a sqliteAudit) MaxInstanceSeq(ctx context.Context, p authz.Proof) (int64, error) {
	if _, err := authz.Verify(p, authz.StoreAuditInstanceMaxSeq, a.tok); err != nil {
		return 0, err
	}
	return a.q.MaxInstanceAudit(ctx)
}

func (a sqliteAudit) pageInstanceExport(ctx context.Context, f AuditFilter, from, to time.Time) ([]AuditEvent, error) {
	rows, err := a.q.PageInstanceAuditExport(ctx, sqlitegen.PageInstanceAuditExportParams{
		AfterSeq:       f.AfterSeq,
		AfterCommitSeq: int64(f.AfterCommitSeq),
		FromTime:       audit.FormatTime(from),
		ToTime:         audit.FormatTime(to),
		PageLimit:      int64(f.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]AuditEvent, 0, len(rows))
	for _, row := range rows {
		event, err := auditEventFromSQLiteInstance(row)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

// --- postgres ---

type pgAudit struct {
	q   *pggen.Queries
	tok *authz.TxToken
}

func (r pgRepos) Audit() AuditRepo { return pgAudit{q: pggen.New(r.db), tok: r.tok} }

func (a pgAudit) InsertTenant(ctx context.Context, p authz.Proof, e audit.Event) error {
	chain, err := authz.VerifyEvent(p, authz.StoreAuditTenantInsert, a.tok, e.Type)
	if err != nil {
		return err
	}
	e.Actor, err = auditrow.ResolveActorClass(ctx, a.q.GetPrincipalKind, e.Actor, authz.IsSystemProof(p))
	if err != nil {
		return err
	}
	row, err := audit.BuildRow(e, audit.TrailTenant, chain, time.Time{})
	if err != nil {
		return err
	}
	return a.q.InsertTenantAuditEvent(ctx, auditrow.PGTenant(row))
}

func (a pgAudit) InsertInstance(ctx context.Context, p authz.Proof, e audit.Event) error {
	if _, err := authz.VerifyEvent(p, authz.StoreAuditInstanceInsert, a.tok, e.Type); err != nil {
		return err
	}
	var err error
	e.Actor, err = auditrow.ResolveActorClass(ctx, a.q.GetPrincipalKind, e.Actor, authz.IsSystemProof(p))
	if err != nil {
		return err
	}
	row, err := audit.BuildRow(e, audit.TrailInstance, domain.Scope{}, time.Time{})
	if err != nil {
		return err
	}
	err = a.q.InsertInstanceAuditEvent(ctx, auditrow.PGInstance(row))
	if pgUniqueViolation(err) {
		return fmt.Errorf("%w: duplicate instance audit event", ErrConflict)
	}
	return err
}

func (a pgAudit) ClaimOfflineRecord(ctx context.Context, p authz.Proof, principalID, recordID string, at time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StoreAuditClaimOfflineRecord, a.tok); err != nil {
		return false, err
	}
	n, err := a.q.ClaimOfflineRecord(ctx, pggen.ClaimOfflineRecordParams{
		PrincipalID: principalID, RecordID: recordID,
		CreatedAt: pgtype.Timestamptz{Time: CanonTime(at), Valid: true},
	})
	return n == 1, err
}

func (a pgAudit) PageTenant(ctx context.Context, p authz.Proof, f AuditFilter) ([]AuditEvent, error) {
	chain, err := authz.Verify(p, authz.StoreAuditTenantPage, a.tok)
	if err != nil {
		return nil, err
	}
	from, to, err := f.bounds()
	if err != nil {
		return nil, err
	}
	level, err := chain.Level()
	if err != nil {
		return nil, err
	}
	if !f.RetentionSnapshot.IsZero() {
		if err := a.guardRetentionSnapshot(ctx, f.RetentionSnapshot); err != nil {
			return nil, err
		}
	}
	if f.Order == AuditPageByCommit {
		return a.pageTenantExport(ctx, chain, level, f, from, to)
	}
	fromTz := pgtype.Timestamptz{Time: from, Valid: true}
	toTz := pgtype.Timestamptz{Time: to, Valid: true}
	var rows []pggen.AuditTenantEvent
	switch level {
	case domain.LevelOrg:
		rows, err = a.q.PageTenantAuditOrg(ctx, pggen.PageTenantAuditOrgParams{
			ChainOrgID: string(chain.Org), AfterSeq: f.AfterSeq,
			FromTime: fromTz, ToTime: toTz, PageLimit: int32(f.Limit),
		})
	case domain.LevelProject:
		rows, err = a.q.PageTenantAuditProject(ctx, pggen.PageTenantAuditProjectParams{
			ChainOrgID: string(chain.Org), ChainProjectID: pgtype.Text{String: string(chain.Project), Valid: true},
			AfterSeq: f.AfterSeq,
			FromTime: fromTz, ToTime: toTz, PageLimit: int32(f.Limit),
		})
	case domain.LevelEnv:
		rows, err = a.q.PageTenantAuditEnv(ctx, pggen.PageTenantAuditEnvParams{
			ChainOrgID: string(chain.Org), ChainProjectID: pgtype.Text{String: string(chain.Project), Valid: true},
			ChainEnvID: pgtype.Text{String: string(chain.Env), Valid: true},
			AfterSeq:   f.AfterSeq,
			FromTime:   fromTz, ToTime: toTz, PageLimit: int32(f.Limit),
		})
	default:
		return nil, errors.New("store: tenant audit page with an empty chain")
	}
	if err != nil {
		return nil, err
	}
	out := make([]AuditEvent, 0, len(rows))
	for _, r := range rows {
		ev, err := auditEventFromPGTenant(r)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

func (a pgAudit) PageInstance(ctx context.Context, p authz.Proof, f AuditFilter) ([]AuditEvent, error) {
	if _, err := authz.Verify(p, authz.StoreAuditInstancePage, a.tok); err != nil {
		return nil, err
	}
	from, to, err := f.bounds()
	if err != nil {
		return nil, err
	}
	if !f.RetentionSnapshot.IsZero() {
		if err := a.guardRetentionSnapshot(ctx, f.RetentionSnapshot); err != nil {
			return nil, err
		}
	}
	if f.Order == AuditPageByCommit {
		return a.pageInstanceExport(ctx, f, from, to)
	}
	rows, err := a.q.PageInstanceAudit(ctx, pggen.PageInstanceAuditParams{
		AfterSeq:  f.AfterSeq,
		FromTime:  pgtype.Timestamptz{Time: from, Valid: true},
		ToTime:    pgtype.Timestamptz{Time: to, Valid: true},
		PageLimit: int32(f.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]AuditEvent, 0, len(rows))
	for _, r := range rows {
		ev, err := auditEventFromPGInstance(r)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

func (a pgAudit) pageTenantExport(ctx context.Context, chain domain.Scope, level domain.Level, f AuditFilter, from, to time.Time) ([]AuditEvent, error) {
	var err error
	fromTz := pgtype.Timestamptz{Time: from, Valid: true}
	toTz := pgtype.Timestamptz{Time: to, Valid: true}
	commitCursor := pgtype.Int8{Int64: int64(f.AfterCommitSeq), Valid: true}
	var rows []pggen.AuditTenantEvent
	switch level {
	case domain.LevelOrg:
		rows, err = a.q.PageTenantAuditExportOrg(ctx, pggen.PageTenantAuditExportOrgParams{
			ChainOrgID: string(chain.Org), AfterSeq: f.AfterSeq, AfterCommitSeq: commitCursor,
			FromTime: fromTz, ToTime: toTz, PageLimit: int32(f.Limit),
		})
	case domain.LevelProject:
		rows, err = a.q.PageTenantAuditExportProject(ctx, pggen.PageTenantAuditExportProjectParams{
			ChainOrgID: string(chain.Org), ChainProjectID: pgtype.Text{String: string(chain.Project), Valid: true},
			AfterSeq: f.AfterSeq, AfterCommitSeq: commitCursor,
			FromTime: fromTz, ToTime: toTz, PageLimit: int32(f.Limit),
		})
	case domain.LevelEnv:
		rows, err = a.q.PageTenantAuditExportEnv(ctx, pggen.PageTenantAuditExportEnvParams{
			ChainOrgID: string(chain.Org), ChainProjectID: pgtype.Text{String: string(chain.Project), Valid: true},
			ChainEnvID: pgtype.Text{String: string(chain.Env), Valid: true},
			AfterSeq:   f.AfterSeq, AfterCommitSeq: commitCursor,
			FromTime: fromTz, ToTime: toTz, PageLimit: int32(f.Limit),
		})
	default:
		return nil, errors.New("store: tenant audit export page with an empty chain")
	}
	if err != nil {
		return nil, err
	}
	out := make([]AuditEvent, 0, len(rows))
	for _, row := range rows {
		event, err := auditEventFromPGTenant(row)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

func (a pgAudit) MaxTenantSeq(ctx context.Context, p authz.Proof) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreAuditTenantMaxSeq, a.tok)
	if err != nil {
		return 0, err
	}
	level, err := chain.Level()
	if err != nil {
		return 0, err
	}
	switch level {
	case domain.LevelOrg:
		return a.q.MaxTenantAuditOrg(ctx, string(chain.Org))
	case domain.LevelProject:
		return a.q.MaxTenantAuditProject(ctx, pggen.MaxTenantAuditProjectParams{
			ChainOrgID: string(chain.Org), ChainProjectID: pgtype.Text{String: string(chain.Project), Valid: true},
		})
	case domain.LevelEnv:
		return a.q.MaxTenantAuditEnv(ctx, pggen.MaxTenantAuditEnvParams{
			ChainOrgID: string(chain.Org), ChainProjectID: pgtype.Text{String: string(chain.Project), Valid: true},
			ChainEnvID: pgtype.Text{String: string(chain.Env), Valid: true},
		})
	default:
		return 0, errors.New("store: tenant audit ceiling with an empty chain")
	}
}

func (a pgAudit) MaxInstanceSeq(ctx context.Context, p authz.Proof) (int64, error) {
	if _, err := authz.Verify(p, authz.StoreAuditInstanceMaxSeq, a.tok); err != nil {
		return 0, err
	}
	return a.q.MaxInstanceAudit(ctx)
}

func (a pgAudit) pageInstanceExport(ctx context.Context, f AuditFilter, from, to time.Time) ([]AuditEvent, error) {
	rows, err := a.q.PageInstanceAuditExport(ctx, pggen.PageInstanceAuditExportParams{
		AfterSeq:       f.AfterSeq,
		AfterCommitSeq: pgtype.Int8{Int64: int64(f.AfterCommitSeq), Valid: true},
		FromTime:       pgtype.Timestamptz{Time: from, Valid: true},
		ToTime:         pgtype.Timestamptz{Time: to, Valid: true},
		PageLimit:      int32(f.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]AuditEvent, 0, len(rows))
	for _, row := range rows {
		event, err := auditEventFromPGInstance(row)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

func auditEventFromSQLiteTenant(r sqlitegen.AuditTenantEvent) (AuditEvent, error) {
	occurred, err := audit.ParseTime(r.OccurredAt)
	if err != nil {
		return AuditEvent{}, err
	}
	recorded, err := audit.ParseTime(r.RecordedAt)
	if err != nil {
		return AuditEvent{}, err
	}
	if r.OccurredAsserted != 0 && r.OccurredAsserted != 1 {
		return AuditEvent{}, fmt.Errorf("store: audit event %s: occurred_asserted = %d, not a boolean", r.ID, r.OccurredAsserted)
	}
	return AuditEvent{
		Event: audit.Event{
			ID: r.ID, Type: audit.EventType(r.Type), SchemaVersion: int(r.SchemaVersion),
			OccurredAt: occurred, OccurredAsserted: r.OccurredAsserted == 1,
			Actor: audit.Actor{
				ID: r.ActorID.String, Class: audit.ActorClass(r.ActorClass),
				CredentialID: r.ActorCredentialID.String,
			},
			AuthorityID:   r.AuthorityID.String,
			Object:        audit.Object{Type: r.ObjectType.String, ID: r.ObjectID.String},
			Outcome:       audit.Outcome(r.Outcome),
			CorrelationID: r.CorrelationID.String,
			SourceIP:      r.SourceIp.String, UserAgent: r.UserAgent.String,
			Origin: audit.Origin(r.Origin),
		},
		Seq: r.Seq, CommitSeq: AuditCommitSeq(r.Seq), RecordedAt: recorded, ScopeClass: r.ScopeClass,
		OrgID: r.OrgID, ProjectID: r.ProjectID.String, EnvID: r.EnvID.String,
		RawPayload: r.Payload,
	}, nil
}

func auditEventFromSQLiteInstance(r sqlitegen.AuditInstanceEvent) (AuditEvent, error) {
	occurred, err := audit.ParseTime(r.OccurredAt)
	if err != nil {
		return AuditEvent{}, err
	}
	recorded, err := audit.ParseTime(r.RecordedAt)
	if err != nil {
		return AuditEvent{}, err
	}
	if r.OccurredAsserted != 0 && r.OccurredAsserted != 1 {
		return AuditEvent{}, fmt.Errorf("store: audit event %s: occurred_asserted = %d, not a boolean", r.ID, r.OccurredAsserted)
	}
	return AuditEvent{
		Event: audit.Event{
			ID: r.ID, Type: audit.EventType(r.Type), SchemaVersion: int(r.SchemaVersion),
			OccurredAt: occurred, OccurredAsserted: r.OccurredAsserted == 1,
			Actor: audit.Actor{
				ID: r.ActorID.String, Class: audit.ActorClass(r.ActorClass),
				CredentialID: r.ActorCredentialID.String,
			},
			AuthorityID:   r.AuthorityID.String,
			Object:        audit.Object{Type: r.ObjectType.String, ID: r.ObjectID.String},
			Outcome:       audit.Outcome(r.Outcome),
			CorrelationID: r.CorrelationID.String,
			SourceIP:      r.SourceIp.String, UserAgent: r.UserAgent.String,
			Origin: audit.Origin(r.Origin),
		},
		Seq: r.Seq, CommitSeq: AuditCommitSeq(r.Seq), RecordedAt: recorded, ScopeClass: "instance", RawPayload: r.Payload,
	}, nil
}

func auditEventFromPGTenant(r pggen.AuditTenantEvent) (AuditEvent, error) {
	if !r.OccurredAt.Valid || !r.RecordedAt.Valid || !r.CommitSeq.Valid {
		return AuditEvent{}, fmt.Errorf("store: audit event %s: null timestamp or commit order", r.ID)
	}
	return AuditEvent{
		Event: audit.Event{
			ID: r.ID, Type: audit.EventType(r.Type), SchemaVersion: int(r.SchemaVersion),
			OccurredAt: r.OccurredAt.Time.UTC(), OccurredAsserted: r.OccurredAsserted,
			Actor: audit.Actor{
				ID: r.ActorID.String, Class: audit.ActorClass(r.ActorClass),
				CredentialID: r.ActorCredentialID.String,
			},
			AuthorityID:   r.AuthorityID.String,
			Object:        audit.Object{Type: r.ObjectType.String, ID: r.ObjectID.String},
			Outcome:       audit.Outcome(r.Outcome),
			CorrelationID: r.CorrelationID.String,
			SourceIP:      r.SourceIp.String, UserAgent: r.UserAgent.String,
			Origin: audit.Origin(r.Origin),
		},
		Seq: r.Seq, CommitSeq: AuditCommitSeq(r.CommitSeq.Int64), RecordedAt: r.RecordedAt.Time.UTC(), ScopeClass: r.ScopeClass,
		OrgID: r.OrgID, ProjectID: r.ProjectID.String, EnvID: r.EnvID.String,
		RawPayload: r.Payload,
	}, nil
}

func auditEventFromPGInstance(r pggen.AuditInstanceEvent) (AuditEvent, error) {
	if !r.OccurredAt.Valid || !r.RecordedAt.Valid || !r.CommitSeq.Valid {
		return AuditEvent{}, fmt.Errorf("store: audit event %s: null timestamp or commit order", r.ID)
	}
	return AuditEvent{
		Event: audit.Event{
			ID: r.ID, Type: audit.EventType(r.Type), SchemaVersion: int(r.SchemaVersion),
			OccurredAt: r.OccurredAt.Time.UTC(), OccurredAsserted: r.OccurredAsserted,
			Actor: audit.Actor{
				ID: r.ActorID.String, Class: audit.ActorClass(r.ActorClass),
				CredentialID: r.ActorCredentialID.String,
			},
			AuthorityID:   r.AuthorityID.String,
			Object:        audit.Object{Type: r.ObjectType.String, ID: r.ObjectID.String},
			Outcome:       audit.Outcome(r.Outcome),
			CorrelationID: r.CorrelationID.String,
			SourceIP:      r.SourceIp.String, UserAgent: r.UserAgent.String,
			Origin: audit.Origin(r.Origin),
		},
		Seq: r.Seq, CommitSeq: AuditCommitSeq(r.CommitSeq.Int64), RecordedAt: r.RecordedAt.Time.UTC(), ScopeClass: "instance", RawPayload: r.Payload,
	}, nil
}
