package authn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// The incident ledger (incident-containment ADR, O5 option B). Mutable state
// kept apart from the append-only audit tables. Every method here runs after
// the owning operation authorized credential-reset at the incident's scope; the
// ledger itself checks nothing about who is asking.

// MaxIncidentItemPage bounds every ledger read.
const MaxIncidentItemPage = 500

// ErrIncidentItemState reports an outcome and reason pair outside the closed
// vocabulary's pairing rules.
var ErrIncidentItemState = errors.New("authn: incident item outcome and reason do not pair")

// Incident is one ledger row.
type Incident struct {
	ID         string
	ScopeKind  domain.IncidentScopeKind
	ScopeID    string
	OrgID      domain.OrgID
	Status     domain.IncidentStatus
	OpenedBy   domain.PrincipalID
	OpenedAt   time.Time
	ClosedBy   domain.PrincipalID
	ClosedAt   time.Time
	RowVersion int64
}

// NewIncident is the insert carrier. OrgID is the owning organisation for an
// org, project or adapter incident and empty for an account incident.
type NewIncident struct {
	ID        string
	ScopeKind domain.IncidentScopeKind
	ScopeID   string
	OrgID     domain.OrgID
	OpenedBy  domain.PrincipalID
	OpenedAt  time.Time
}

// IncidentItem is one affected object. It carries ids and closed vocabulary
// only: never a name, an address, an issuer or a secret.
type IncidentItem struct {
	ID            string
	IncidentID    string
	OwnerFunction string
	ObjectKind    string
	ObjectID      string
	Scope         string
	Outcome       domain.ItemOutcome
	Reason        domain.ItemReason
	AttestedBy    domain.PrincipalID
	AttestedAt    time.Time
	RowVersion    int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewIncidentItem is the enumeration insert carrier. The outcome is pending, or
// out_of_scope with its reason, or blocked for an object class the opener may
// not read.
type NewIncidentItem struct {
	ID            string
	IncidentID    string
	OwnerFunction string
	ObjectKind    string
	ObjectID      string
	Scope         string
	Outcome       domain.ItemOutcome
	Reason        domain.ItemReason
	At            time.Time
}

// ItemTransition compare-and-sets one open item.
type ItemTransition struct {
	ID              string
	IncidentID      string
	ExpectedVersion int64
	Outcome         domain.ItemOutcome
	Reason          domain.ItemReason
	// AttestedBy and At record who stated an attested outcome and when. Both are
	// empty for any other outcome.
	AttestedBy domain.PrincipalID
	At         time.Time
}

// ValidItemState reports whether an outcome and reason pair is allowed. The
// database CHECK enforces the vocabulary; this enforces the pairing so a blocked
// item always says who must act and an unknown item always says why.
func ValidItemState(o domain.ItemOutcome, r domain.ItemReason) bool {
	if !o.Valid() || !r.Valid() {
		return false
	}
	switch o {
	case domain.ItemPending:
		return r == domain.ReasonNone || r == domain.ReasonOwnerFailed
	case domain.ItemDone:
		return r == domain.ReasonNone
	case domain.ItemUnknown:
		return r == domain.ReasonOwnerUnconfirmed || r == domain.ReasonEnumerationTruncated || r == domain.ReasonUpstreamNotObservable
	case domain.ItemAttested:
		return r == domain.ReasonNone || r == domain.ReasonUpstreamNotObservable
	case domain.ItemBlocked:
		return r == domain.ReasonNeedsAnotherOperator
	case domain.ItemOutOfScope:
		return r.OutOfScopeReason()
	}
	return false
}

// OpenIncident inserts the open incident for a scope. When the scope already
// has one open, it inserts nothing and returns the existing row with created
// false: opening is idempotent.
func (r *Resolver) OpenIncident(ctx context.Context, n NewIncident) (Incident, bool, error) {
	if !n.ScopeKind.Valid() || n.ID == "" || n.ScopeID == "" || n.OpenedBy == "" {
		return Incident{}, false, fmt.Errorf("authn: incident needs a scope kind, scope id and opener: %w", domain.ErrInvalid)
	}
	var rows int64
	var err error
	if r.sq != nil {
		rows, err = r.sq.InsertIncident(ctx, sqlitegen.InsertIncidentParams{
			ID: n.ID, ScopeKind: string(n.ScopeKind), ScopeID: n.ScopeID, OrgID: string(n.OrgID),
			OpenedBy: string(n.OpenedBy), OpenedAt: encodeTime(n.OpenedAt),
		})
	} else {
		rows, err = r.pg.InsertIncident(ctx, pggen.InsertIncidentParams{
			ID: n.ID, ScopeKind: string(n.ScopeKind), ScopeID: n.ScopeID, OrgID: string(n.OrgID),
			OpenedBy: string(n.OpenedBy), OpenedAt: pgTimestamp(n.OpenedAt),
		})
	}
	if err != nil {
		return Incident{}, false, err
	}
	if rows == 1 {
		got, err := r.IncidentByID(ctx, n.ID)
		return got, true, err
	}
	var existing Incident
	if r.sq != nil {
		row, gerr := r.sq.GetOpenIncidentForScope(ctx, sqlitegen.GetOpenIncidentForScopeParams{ScopeKind: string(n.ScopeKind), ScopeID: n.ScopeID})
		if gerr != nil {
			return Incident{}, false, notFoundOr(gerr)
		}
		existing, err = sqliteIncident(row)
	} else {
		row, gerr := r.pg.GetOpenIncidentForScope(ctx, pggen.GetOpenIncidentForScopeParams{ScopeKind: string(n.ScopeKind), ScopeID: n.ScopeID})
		if gerr != nil {
			return Incident{}, false, notFoundOr(gerr)
		}
		existing = pgIncident(row)
	}
	return existing, false, err
}

// IncidentByID reads one incident. domain.ErrNotFound when there is none.
func (r *Resolver) IncidentByID(ctx context.Context, id string) (Incident, error) {
	if r.sq != nil {
		row, err := r.sq.GetIncident(ctx, id)
		if err != nil {
			return Incident{}, notFoundOr(err)
		}
		return sqliteIncident(row)
	}
	row, err := r.pg.GetIncident(ctx, id)
	if err != nil {
		return Incident{}, notFoundOr(err)
	}
	return pgIncident(row), nil
}

// CloseIncident compare-and-sets an open incident to a closed status. It reports
// false when the version moved or the incident is no longer open.
func (r *Resolver) CloseIncident(ctx context.Context, id string, expectedVersion int64, status domain.IncidentStatus, by domain.PrincipalID, at time.Time) (bool, error) {
	if (status != domain.IncidentClosed && status != domain.IncidentClosedWithResiduals) || by == "" {
		return false, fmt.Errorf("authn: closing an incident needs a closed status and an actor: %w", domain.ErrInvalid)
	}
	var rows int64
	var err error
	if r.sq != nil {
		rows, err = r.sq.CloseIncident(ctx, sqlitegen.CloseIncidentParams{
			Status: string(status), ClosedBy: nullString(string(by)), ClosedAt: sql.NullString{String: encodeTime(at), Valid: true},
			ID: id, ExpectedVersion: expectedVersion,
		})
	} else {
		rows, err = r.pg.CloseIncident(ctx, pggen.CloseIncidentParams{
			Status: string(status), ClosedBy: pgText(string(by)), ClosedAt: pgTimestamp(at),
			ID: id, ExpectedVersion: expectedVersion,
		})
	}
	return rows == 1, err
}

// AddIncidentItem records one affected object. It reports false, and changes
// nothing, when the ledger already holds the object (the unique key) or the
// incident is not open, so a repeated enumeration never resets an outcome.
func (r *Resolver) AddIncidentItem(ctx context.Context, n NewIncidentItem) (bool, error) {
	if n.ID == "" || n.IncidentID == "" || n.OwnerFunction == "" || n.ObjectKind == "" || n.ObjectID == "" {
		return false, fmt.Errorf("authn: incident item needs an id, incident, owner function, kind and object: %w", domain.ErrInvalid)
	}
	if !ValidItemState(n.Outcome, n.Reason) || n.Outcome == domain.ItemDone || n.Outcome == domain.ItemAttested {
		return false, ErrIncidentItemState
	}
	var rows int64
	var err error
	if r.sq != nil {
		rows, err = r.sq.InsertIncidentItem(ctx, sqlitegen.InsertIncidentItemParams{
			ID: n.ID, OwnerFunction: n.OwnerFunction, ObjectKind: n.ObjectKind, ObjectID: n.ObjectID, Scope: n.Scope,
			Outcome: string(n.Outcome), Reason: string(n.Reason), At: encodeTime(n.At), IncidentID: n.IncidentID,
		})
	} else {
		rows, err = r.pg.InsertIncidentItem(ctx, pggen.InsertIncidentItemParams{
			ID: n.ID, OwnerFunction: n.OwnerFunction, ObjectKind: n.ObjectKind, ObjectID: n.ObjectID, Scope: n.Scope,
			Outcome: string(n.Outcome), Reason: string(n.Reason), At: pgTimestamp(n.At), IncidentID: n.IncidentID,
		})
	}
	return rows == 1, err
}

// IncidentItems pages one incident's items in id order after the cursor, at most
// limit rows (capped at MaxIncidentItemPage). openOnly keeps pending, unknown
// and blocked items, which is what resume re-reads.
func (r *Resolver) IncidentItems(ctx context.Context, incidentID, afterID string, limit int, openOnly bool) ([]IncidentItem, error) {
	if limit <= 0 || limit > MaxIncidentItemPage {
		limit = MaxIncidentItemPage
	}
	var out []IncidentItem
	if r.sq != nil {
		var rows []sqlitegen.IncidentItem
		var err error
		if openOnly {
			rows, err = r.sq.ListOpenIncidentItems(ctx, sqlitegen.ListOpenIncidentItemsParams{IncidentID: incidentID, AfterID: afterID, RowLimit: int64(limit)})
		} else {
			rows, err = r.sq.ListIncidentItems(ctx, sqlitegen.ListIncidentItemsParams{IncidentID: incidentID, AfterID: afterID, RowLimit: int64(limit)})
		}
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			item, err := sqliteIncidentItem(row)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	}
	var rows []pggen.IncidentItem
	var err error
	if openOnly {
		rows, err = r.pg.ListOpenIncidentItems(ctx, pggen.ListOpenIncidentItemsParams{IncidentID: incidentID, AfterID: afterID, RowLimit: int32(limit)})
	} else {
		rows, err = r.pg.ListIncidentItems(ctx, pggen.ListIncidentItemsParams{IncidentID: incidentID, AfterID: afterID, RowLimit: int32(limit)})
	}
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out = append(out, pgIncidentItem(row))
	}
	return out, nil
}

// IncidentItemCounts counts one incident's items by outcome.
func (r *Resolver) IncidentItemCounts(ctx context.Context, incidentID string) (map[domain.ItemOutcome]int64, error) {
	out := map[domain.ItemOutcome]int64{}
	if r.sq != nil {
		rows, err := r.sq.CountIncidentItemsByOutcome(ctx, incidentID)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			out[domain.ItemOutcome(row.Outcome)] = row.ItemCount
		}
		return out, nil
	}
	rows, err := r.pg.CountIncidentItemsByOutcome(ctx, incidentID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[domain.ItemOutcome(row.Outcome)] = row.ItemCount
	}
	return out, nil
}

// TransitionIncidentItem compare-and-sets one open item. It reports false, and
// changes nothing, when the version moved, the item is already terminal or the
// incident is closed: a stale writer or a repeated result never overwrites.
func (r *Resolver) TransitionIncidentItem(ctx context.Context, t ItemTransition) (bool, error) {
	if !ValidItemState(t.Outcome, t.Reason) || t.Outcome == domain.ItemPending {
		return false, ErrIncidentItemState
	}
	if (t.Outcome == domain.ItemAttested) != (t.AttestedBy != "") {
		return false, ErrIncidentItemState
	}
	var rows int64
	var err error
	if r.sq != nil {
		attestedAt := sql.NullString{}
		if t.AttestedBy != "" {
			attestedAt = sql.NullString{String: encodeTime(t.At), Valid: true}
		}
		rows, err = r.sq.TransitionIncidentItem(ctx, sqlitegen.TransitionIncidentItemParams{
			Outcome: string(t.Outcome), Reason: string(t.Reason), AttestedBy: nullString(string(t.AttestedBy)), AttestedAt: attestedAt,
			At: encodeTime(t.At), ID: t.ID, IncidentID: t.IncidentID, ExpectedVersion: t.ExpectedVersion,
		})
	} else {
		attestedAt := pgtype.Timestamptz{}
		if t.AttestedBy != "" {
			attestedAt = pgTimestamp(t.At)
		}
		rows, err = r.pg.TransitionIncidentItem(ctx, pggen.TransitionIncidentItemParams{
			Outcome: string(t.Outcome), Reason: string(t.Reason), AttestedBy: pgText(string(t.AttestedBy)), AttestedAt: attestedAt,
			At: pgTimestamp(t.At), ID: t.ID, IncidentID: t.IncidentID, ExpectedVersion: t.ExpectedVersion,
		})
	}
	return rows == 1, err
}

func sqliteIncident(row sqlitegen.Incident) (Incident, error) {
	opened, err := decodeTime(row.OpenedAt)
	if err != nil {
		return Incident{}, err
	}
	out := Incident{
		ID: row.ID, ScopeKind: domain.IncidentScopeKind(row.ScopeKind), ScopeID: row.ScopeID, OrgID: domain.OrgID(row.OrgID),
		Status: domain.IncidentStatus(row.Status), OpenedBy: domain.PrincipalID(row.OpenedBy), OpenedAt: opened,
		ClosedBy: domain.PrincipalID(row.ClosedBy.String), RowVersion: row.RowVersion,
	}
	if row.ClosedAt.Valid {
		if out.ClosedAt, err = decodeTime(row.ClosedAt.String); err != nil {
			return Incident{}, err
		}
	}
	return out, nil
}

func pgIncident(row pggen.Incident) Incident {
	out := Incident{
		ID: row.ID, ScopeKind: domain.IncidentScopeKind(row.ScopeKind), ScopeID: row.ScopeID, OrgID: domain.OrgID(row.OrgID),
		Status: domain.IncidentStatus(row.Status), OpenedBy: domain.PrincipalID(row.OpenedBy), OpenedAt: canon(row.OpenedAt.Time),
		ClosedBy: domain.PrincipalID(row.ClosedBy.String), RowVersion: row.RowVersion,
	}
	if row.ClosedAt.Valid {
		out.ClosedAt = canon(row.ClosedAt.Time)
	}
	return out
}

func sqliteIncidentItem(row sqlitegen.IncidentItem) (IncidentItem, error) {
	created, err := decodeTime(row.CreatedAt)
	if err != nil {
		return IncidentItem{}, err
	}
	updated, err := decodeTime(row.UpdatedAt)
	if err != nil {
		return IncidentItem{}, err
	}
	out := IncidentItem{
		ID: row.ID, IncidentID: row.IncidentID, OwnerFunction: row.OwnerFunction, ObjectKind: row.ObjectKind, ObjectID: row.ObjectID,
		Scope: row.Scope, Outcome: domain.ItemOutcome(row.Outcome), Reason: domain.ItemReason(row.Reason),
		AttestedBy: domain.PrincipalID(row.AttestedBy.String), RowVersion: row.RowVersion, CreatedAt: created, UpdatedAt: updated,
	}
	if row.AttestedAt.Valid {
		if out.AttestedAt, err = decodeTime(row.AttestedAt.String); err != nil {
			return IncidentItem{}, err
		}
	}
	return out, nil
}

func pgIncidentItem(row pggen.IncidentItem) IncidentItem {
	out := IncidentItem{
		ID: row.ID, IncidentID: row.IncidentID, OwnerFunction: row.OwnerFunction, ObjectKind: row.ObjectKind, ObjectID: row.ObjectID,
		Scope: row.Scope, Outcome: domain.ItemOutcome(row.Outcome), Reason: domain.ItemReason(row.Reason),
		AttestedBy: domain.PrincipalID(row.AttestedBy.String), RowVersion: row.RowVersion,
		CreatedAt: canon(row.CreatedAt.Time), UpdatedAt: canon(row.UpdatedAt.Time),
	}
	if row.AttestedAt.Valid {
		out.AttestedAt = canon(row.AttestedAt.Time)
	}
	return out
}

// IncidentItemByID re-reads one item inside its incident. domain.ErrNotFound
// when the incident holds no such item.
func (r *Resolver) IncidentItemByID(ctx context.Context, incidentID, id string) (IncidentItem, error) {
	if r.sq != nil {
		row, err := r.sq.GetIncidentItem(ctx, sqlitegen.GetIncidentItemParams{IncidentID: incidentID, ID: id})
		if err != nil {
			return IncidentItem{}, notFoundOr(err)
		}
		return sqliteIncidentItem(row)
	}
	row, err := r.pg.GetIncidentItem(ctx, pggen.GetIncidentItemParams{IncidentID: incidentID, ID: id})
	if err != nil {
		return IncidentItem{}, notFoundOr(err)
	}
	return pgIncidentItem(row), nil
}
