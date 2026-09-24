package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/deliverytarget"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Delivery-target condition reporting (#788, k8s-condition-reporting ADR).
// Every method verifies the proof at the boundary and binds every chain
// parameter from the verified proof's resolved chain. The two scheduler
// methods run under system authority and are the only cross-tenant doors.

// DeliveryTargetKey is the row key under one environment: the reporting
// principal and the target's three UIDs (ADR D3).
type DeliveryTargetKey struct {
	PrincipalID string
	ClusterID   string
	InstanceUID string
	TargetUID   string
}

// DeliveryTargetReport is one latest-state row.
type DeliveryTargetReport struct {
	ID                    string
	EnvironmentID         string
	PrincipalID           string
	Target                deliverytarget.Target
	Vocabulary            int
	Generation            int64
	ObservedGeneration    int64
	ReportedAt            time.Time
	ReceivedAt            time.Time
	ReportIntervalSeconds int64
	Lifecycle             string
	Conditions            []deliverytarget.Condition
	Reporter              string
	ReporterVersion       string
	// RefusalCause and RefusedAt are set only by a vocabulary refusal after
	// the last accepted report; both are empty otherwise.
	RefusalCause string
	RefusedAt    time.Time
	CreatedAt    time.Time
}

// ExpiredDeliveryTargetReport is one purge candidate, with the chain the
// scheduler needs to write its tenant-trail purge event.
type ExpiredDeliveryTargetReport struct {
	ID            string
	OrgID         string
	ProjectID     string
	EnvironmentID string
	PrincipalID   string
	ReceivedAt    time.Time
}

// DeliveryTargetReader is the read side the human list operation uses.
type DeliveryTargetReader interface {
	List(ctx context.Context, p authz.Proof) ([]DeliveryTargetReport, error)
	// QuotaNotices returns the project's quota-refused notices, keyed by
	// principal, with each one's last refusal time.
	QuotaNotices(ctx context.Context, p authz.Proof) (map[string]time.Time, error)
	// LastFetchAt returns the principal's last identity.delivery_fetched time
	// in the proof's environment, if any (ADR D2, observed by the server).
	LastFetchAt(ctx context.Context, p authz.Proof, principalID string) (time.Time, bool, error)
}

// DeliveryTargetRepo is the write bundle's delivery-target surface.
type DeliveryTargetRepo interface {
	DeliveryTargetReader
	Get(ctx context.Context, p authz.Proof, key DeliveryTargetKey) (DeliveryTargetReport, error)
	CountForPrincipal(ctx context.Context, p authz.Proof, principalID string) (int64, error)
	Insert(ctx context.Context, p authz.Proof, row DeliveryTargetReport) error
	// Update replaces the accepted state of the row with row.ID and clears
	// any recorded refusal.
	Update(ctx context.Context, p authz.Proof, row DeliveryTargetReport) (bool, error)
	RecordRefusal(ctx context.Context, p authz.Proof, id, cause string, at time.Time) (bool, error)
	Delete(ctx context.Context, p authz.Proof, id string) (bool, error)
	RecordQuotaRefusal(ctx context.Context, p authz.Proof, principalID string, at time.Time) error
	// SelectExpired returns at most limit rows with no accepted report since
	// cutoff.
	SelectExpired(ctx context.Context, p authz.Proof, cutoff time.Time, limit int) ([]ExpiredDeliveryTargetReport, error)
	Purge(ctx context.Context, p authz.Proof, id string, cutoff time.Time) (bool, error)
}

// deliveryFetchedEvent is audit.EventDeliveryFetched, the only event the
// server-observed contact layer reads.
const deliveryFetchedEvent = "identity.delivery_fetched"

func (s sqliteReadRepos) DeliveryTargets() DeliveryTargetReader { return s.r.DeliveryTargets() }
func (p pgReadRepos) DeliveryTargets() DeliveryTargetReader     { return p.r.DeliveryTargets() }

func (r sqliteRepos) DeliveryTargets() DeliveryTargetRepo {
	return sqliteDeliveryTargets{q: sqlitegen.New(r.db), tok: r.tok}
}

func (r pgRepos) DeliveryTargets() DeliveryTargetRepo {
	return pgDeliveryTargets{q: pggen.New(r.db), tok: r.tok}
}

type conditionJSON struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	ObservedGeneration int64  `json:"observed_generation"`
}

// encodeConditions is the canonical stored form: sorted by type, so two
// reports asserting the same set store the same bytes.
func encodeConditions(conditions []deliverytarget.Condition) (string, error) {
	out := make([]conditionJSON, 0, len(conditions))
	for _, c := range conditions {
		out = append(out, conditionJSON{Type: c.Type, Status: c.Status, Reason: c.Reason, ObservedGeneration: c.ObservedGeneration})
	}
	slices.SortFunc(out, func(a, b conditionJSON) int { return strings.Compare(a.Type, b.Type) })
	raw, err := json.Marshal(out)
	return string(raw), err
}

func decodeConditions(id, raw string) ([]deliverytarget.Condition, error) {
	var in []conditionJSON
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, fmt.Errorf("store: delivery target %s conditions: %w", id, err)
	}
	out := make([]deliverytarget.Condition, 0, len(in))
	for _, c := range in {
		out = append(out, deliverytarget.Condition{Type: c.Type, Status: c.Status, Reason: c.Reason, ObservedGeneration: c.ObservedGeneration})
	}
	return out, nil
}

// --- sqlite ---

type sqliteDeliveryTargets struct {
	q   *sqlitegen.Queries
	tok *authz.TxToken
}

func (r sqliteDeliveryTargets) Get(ctx context.Context, p authz.Proof, key DeliveryTargetKey) (DeliveryTargetReport, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsGet, r.tok)
	if err != nil {
		return DeliveryTargetReport{}, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsGet)
	if err != nil {
		return DeliveryTargetReport{}, err
	}
	row, err := r.q.GetDeliveryTargetReport(ctx, sqlitegen.GetDeliveryTargetReportParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env,
		PrincipalID: key.PrincipalID, ClusterID: key.ClusterID, InstanceUid: key.InstanceUID, TargetUid: key.TargetUID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryTargetReport{}, ErrNotFound
	}
	if err != nil {
		return DeliveryTargetReport{}, err
	}
	return deliveryTargetFromSQLite(row)
}

func (r sqliteDeliveryTargets) List(ctx context.Context, p authz.Proof) ([]DeliveryTargetReport, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsList, r.tok)
	if err != nil {
		return nil, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsList)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListDeliveryTargetReports(ctx, sqlitegen.ListDeliveryTargetReportsParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env,
	})
	if err != nil {
		return nil, err
	}
	out := make([]DeliveryTargetReport, 0, len(rows))
	for _, row := range rows {
		report, err := deliveryTargetFromSQLite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, report)
	}
	return out, nil
}

func (r sqliteDeliveryTargets) QuotaNotices(ctx context.Context, p authz.Proof) (map[string]time.Time, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsQuotaNotices, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListDeliveryTargetQuotaNotices(ctx, sqlitegen.ListDeliveryTargetQuotaNoticesParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project),
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]time.Time, len(rows))
	for _, row := range rows {
		if out[row.PrincipalID], err = parseTime("delivery target quota notice", row.PrincipalID, row.RefusedAt); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r sqliteDeliveryTargets) LastFetchAt(ctx context.Context, p authz.Proof, principalID string) (time.Time, bool, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsLastFetch, r.tok)
	if err != nil {
		return time.Time{}, false, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsLastFetch)
	if err != nil {
		return time.Time{}, false, err
	}
	row, err := r.q.LastDeliveryFetchAt(ctx, sqlitegen.LastDeliveryFetchAtParams{
		OrgID: string(chain.Org), ProjectID: nullString(string(chain.Project)), EnvID: nullString(env),
		ActorID: nullString(principalID), Type: deliveryFetchedEvent,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	at, err := parseTime("delivery fetch", principalID, row.OccurredAt)
	return at, err == nil, err
}

func (r sqliteDeliveryTargets) CountForPrincipal(ctx context.Context, p authz.Proof, principalID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsCountPrincipal, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.CountDeliveryTargetReportsForPrincipal(ctx, sqlitegen.CountDeliveryTargetReportsForPrincipalParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), PrincipalID: principalID,
	})
}

func (r sqliteDeliveryTargets) Insert(ctx context.Context, p authz.Proof, row DeliveryTargetReport) error {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsInsert, r.tok)
	if err != nil {
		return err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsInsert)
	if err != nil {
		return err
	}
	conditions, err := encodeConditions(row.Conditions)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertDeliveryTargetReport(ctx, sqlitegen.InsertDeliveryTargetReportParams{
		ID: row.ID, OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env,
		PrincipalID: row.PrincipalID, ClusterID: row.Target.ClusterID, InstanceUid: row.Target.InstanceUID,
		TargetUid: row.Target.UID, Namespace: row.Target.Namespace, Name: row.Target.Name,
		Vocabulary: int64(row.Vocabulary), Generation: row.Generation, ObservedGeneration: row.ObservedGeneration,
		ReportedAt: fixedStamp(row.ReportedAt), ReceivedAt: fixedStamp(row.ReceivedAt),
		ReportIntervalSeconds: row.ReportIntervalSeconds, Lifecycle: row.Lifecycle, Conditions: conditions,
		Reporter: row.Reporter, ReporterVersion: row.ReporterVersion, CreatedAt: fixedStamp(row.CreatedAt),
	}))
}

func (r sqliteDeliveryTargets) Update(ctx context.Context, p authz.Proof, row DeliveryTargetReport) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsUpdate, r.tok)
	if err != nil {
		return false, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsUpdate)
	if err != nil {
		return false, err
	}
	conditions, err := encodeConditions(row.Conditions)
	if err != nil {
		return false, err
	}
	n, err := r.q.UpdateDeliveryTargetReport(ctx, sqlitegen.UpdateDeliveryTargetReportParams{
		Namespace: row.Target.Namespace, Name: row.Target.Name, Vocabulary: int64(row.Vocabulary),
		Generation: row.Generation, ObservedGeneration: row.ObservedGeneration,
		ReportedAt: fixedStamp(row.ReportedAt), ReceivedAt: fixedStamp(row.ReceivedAt),
		ReportIntervalSeconds: row.ReportIntervalSeconds, Lifecycle: row.Lifecycle, Conditions: conditions,
		Reporter: row.Reporter, ReporterVersion: row.ReporterVersion,
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env, ID: row.ID,
	})
	return n > 0, constraint(err)
}

func (r sqliteDeliveryTargets) RecordRefusal(ctx context.Context, p authz.Proof, id, cause string, at time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsRecordRefusal, r.tok)
	if err != nil {
		return false, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsRecordRefusal)
	if err != nil {
		return false, err
	}
	n, err := r.q.RecordDeliveryTargetRefusal(ctx, sqlitegen.RecordDeliveryTargetRefusalParams{
		RefusalCause: nullString(cause), RefusedAt: nullTimeString(at),
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env, ID: id,
	})
	return n > 0, constraint(err)
}

func (r sqliteDeliveryTargets) Delete(ctx context.Context, p authz.Proof, id string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsDelete, r.tok)
	if err != nil {
		return false, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsDelete)
	if err != nil {
		return false, err
	}
	n, err := r.q.DeleteDeliveryTargetReport(ctx, sqlitegen.DeleteDeliveryTargetReportParams{
		OrgID: string(chain.Org), ProjectID: string(chain.Project), EnvironmentID: env, ID: id,
	})
	return n > 0, err
}

func (r sqliteDeliveryTargets) RecordQuotaRefusal(ctx context.Context, p authz.Proof, principalID string, at time.Time) error {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsRecordQuota, r.tok)
	if err != nil {
		return err
	}
	n, err := r.q.UpdateDeliveryTargetQuotaNotice(ctx, sqlitegen.UpdateDeliveryTargetQuotaNoticeParams{
		RefusedAt: fixedStamp(at), OrgID: string(chain.Org), ProjectID: string(chain.Project), PrincipalID: principalID,
	})
	if err != nil || n > 0 {
		return constraint(err)
	}
	return constraint(r.q.InsertDeliveryTargetQuotaNotice(ctx, sqlitegen.InsertDeliveryTargetQuotaNoticeParams{
		PrincipalID: principalID, OrgID: string(chain.Org), ProjectID: string(chain.Project), RefusedAt: fixedStamp(at),
	}))
}

func (r sqliteDeliveryTargets) SelectExpired(ctx context.Context, p authz.Proof, cutoff time.Time, limit int) ([]ExpiredDeliveryTargetReport, error) {
	if _, err := authz.Verify(p, authz.StoreDeliveryTargetsSelectExpired, r.tok); err != nil {
		return nil, err
	}
	rows, err := r.q.SelectExpiredDeliveryTargetReports(ctx, sqlitegen.SelectExpiredDeliveryTargetReportsParams{
		ReceivedAt: fixedStamp(cutoff), BatchLimit: int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]ExpiredDeliveryTargetReport, 0, len(rows))
	for _, row := range rows {
		received, err := parseTime("delivery target", row.ID, row.ReceivedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, ExpiredDeliveryTargetReport{
			ID: row.ID, OrgID: row.OrgID, ProjectID: row.ProjectID, EnvironmentID: row.EnvironmentID,
			PrincipalID: row.PrincipalID, ReceivedAt: received,
		})
	}
	return out, nil
}

func (r sqliteDeliveryTargets) Purge(ctx context.Context, p authz.Proof, id string, cutoff time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StoreDeliveryTargetsPurge, r.tok); err != nil {
		return false, err
	}
	n, err := r.q.PurgeDeliveryTargetReport(ctx, sqlitegen.PurgeDeliveryTargetReportParams{ID: id, ReceivedAt: fixedStamp(cutoff)})
	return n > 0, err
}

func deliveryTargetFromSQLite(row sqlitegen.DeliveryTargetReport) (DeliveryTargetReport, error) {
	reported, err := parseTime("delivery target", row.ID, row.ReportedAt)
	if err != nil {
		return DeliveryTargetReport{}, err
	}
	received, err := parseTime("delivery target", row.ID, row.ReceivedAt)
	if err != nil {
		return DeliveryTargetReport{}, err
	}
	created, err := parseTime("delivery target", row.ID, row.CreatedAt)
	if err != nil {
		return DeliveryTargetReport{}, err
	}
	var refused time.Time
	if row.RefusedAt.Valid {
		if refused, err = parseTime("delivery target", row.ID, row.RefusedAt.String); err != nil {
			return DeliveryTargetReport{}, err
		}
	}
	conditions, err := decodeConditions(row.ID, row.Conditions)
	if err != nil {
		return DeliveryTargetReport{}, err
	}
	return DeliveryTargetReport{
		ID: row.ID, EnvironmentID: row.EnvironmentID, PrincipalID: row.PrincipalID,
		Target: deliverytarget.Target{
			ClusterID: row.ClusterID, InstanceUID: row.InstanceUid, Namespace: row.Namespace,
			Name: row.Name, UID: row.TargetUid,
		},
		Vocabulary: int(row.Vocabulary), Generation: row.Generation, ObservedGeneration: row.ObservedGeneration,
		ReportedAt: reported, ReceivedAt: received, ReportIntervalSeconds: row.ReportIntervalSeconds,
		Lifecycle: row.Lifecycle, Conditions: conditions, Reporter: row.Reporter, ReporterVersion: row.ReporterVersion,
		RefusalCause: row.RefusalCause.String, RefusedAt: refused, CreatedAt: created,
	}, nil
}

// --- postgres ---

type pgDeliveryTargets struct {
	q   *pggen.Queries
	tok *authz.TxToken
}

func pgRequiredTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: CanonTime(t), Valid: true}
}

func (r pgDeliveryTargets) Get(ctx context.Context, p authz.Proof, key DeliveryTargetKey) (DeliveryTargetReport, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsGet, r.tok)
	if err != nil {
		return DeliveryTargetReport{}, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsGet)
	if err != nil {
		return DeliveryTargetReport{}, err
	}
	row, err := r.q.GetDeliveryTargetReport(ctx, pggen.GetDeliveryTargetReportParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		PrincipalID: key.PrincipalID, ClusterID: key.ClusterID, InstanceUid: key.InstanceUID, TargetUid: key.TargetUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return DeliveryTargetReport{}, ErrNotFound
	}
	if err != nil {
		return DeliveryTargetReport{}, err
	}
	return deliveryTargetFromPG(row)
}

func (r pgDeliveryTargets) List(ctx context.Context, p authz.Proof) ([]DeliveryTargetReport, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsList, r.tok)
	if err != nil {
		return nil, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsList)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListDeliveryTargetReports(ctx, pggen.ListDeliveryTargetReportsParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
	})
	if err != nil {
		return nil, err
	}
	out := make([]DeliveryTargetReport, 0, len(rows))
	for _, row := range rows {
		report, err := deliveryTargetFromPG(row)
		if err != nil {
			return nil, err
		}
		out = append(out, report)
	}
	return out, nil
}

func (r pgDeliveryTargets) QuotaNotices(ctx context.Context, p authz.Proof) (map[string]time.Time, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsQuotaNotices, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListDeliveryTargetQuotaNotices(ctx, pggen.ListDeliveryTargetQuotaNoticesParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]time.Time, len(rows))
	for _, row := range rows {
		out[row.PrincipalID] = row.RefusedAt.Time.UTC()
	}
	return out, nil
}

func (r pgDeliveryTargets) LastFetchAt(ctx context.Context, p authz.Proof, principalID string) (time.Time, bool, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsLastFetch, r.tok)
	if err != nil {
		return time.Time{}, false, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsLastFetch)
	if err != nil {
		return time.Time{}, false, err
	}
	row, err := r.q.LastDeliveryFetchAt(ctx, pggen.LastDeliveryFetchAtParams{
		ChainOrgID: string(chain.Org), ChainProjectID: pgText(string(chain.Project)), ChainEnvID: pgText(env),
		ActorID: pgText(principalID), Type: deliveryFetchedEvent,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return row.OccurredAt.Time.UTC(), true, nil
}

func (r pgDeliveryTargets) CountForPrincipal(ctx context.Context, p authz.Proof, principalID string) (int64, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsCountPrincipal, r.tok)
	if err != nil {
		return 0, err
	}
	return r.q.CountDeliveryTargetReportsForPrincipal(ctx, pggen.CountDeliveryTargetReportsForPrincipalParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), PrincipalID: principalID,
	})
}

func (r pgDeliveryTargets) Insert(ctx context.Context, p authz.Proof, row DeliveryTargetReport) error {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsInsert, r.tok)
	if err != nil {
		return err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsInsert)
	if err != nil {
		return err
	}
	conditions, err := encodeConditions(row.Conditions)
	if err != nil {
		return err
	}
	return constraint(r.q.InsertDeliveryTargetReport(ctx, pggen.InsertDeliveryTargetReportParams{
		ID: row.ID, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env,
		PrincipalID: row.PrincipalID, ClusterID: row.Target.ClusterID, InstanceUid: row.Target.InstanceUID,
		TargetUid: row.Target.UID, Namespace: row.Target.Namespace, Name: row.Target.Name,
		Vocabulary: int32(row.Vocabulary), Generation: row.Generation, ObservedGeneration: row.ObservedGeneration,
		ReportedAt: pgRequiredTime(row.ReportedAt), ReceivedAt: pgRequiredTime(row.ReceivedAt),
		ReportIntervalSeconds: int32(row.ReportIntervalSeconds), Lifecycle: row.Lifecycle, Conditions: conditions,
		Reporter: row.Reporter, ReporterVersion: row.ReporterVersion, CreatedAt: pgRequiredTime(row.CreatedAt),
	}))
}

func (r pgDeliveryTargets) Update(ctx context.Context, p authz.Proof, row DeliveryTargetReport) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsUpdate, r.tok)
	if err != nil {
		return false, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsUpdate)
	if err != nil {
		return false, err
	}
	conditions, err := encodeConditions(row.Conditions)
	if err != nil {
		return false, err
	}
	n, err := r.q.UpdateDeliveryTargetReport(ctx, pggen.UpdateDeliveryTargetReportParams{
		Namespace: row.Target.Namespace, Name: row.Target.Name, Vocabulary: int32(row.Vocabulary),
		Generation: row.Generation, ObservedGeneration: row.ObservedGeneration,
		ReportedAt: pgRequiredTime(row.ReportedAt), ReceivedAt: pgRequiredTime(row.ReceivedAt),
		ReportIntervalSeconds: int32(row.ReportIntervalSeconds), Lifecycle: row.Lifecycle, Conditions: conditions,
		Reporter: row.Reporter, ReporterVersion: row.ReporterVersion,
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, ID: row.ID,
	})
	return n > 0, constraint(err)
}

func (r pgDeliveryTargets) RecordRefusal(ctx context.Context, p authz.Proof, id, cause string, at time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsRecordRefusal, r.tok)
	if err != nil {
		return false, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsRecordRefusal)
	if err != nil {
		return false, err
	}
	n, err := r.q.RecordDeliveryTargetRefusal(ctx, pggen.RecordDeliveryTargetRefusalParams{
		RefusalCause: pgText(cause), RefusedAt: pgRequiredTime(at),
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, ID: id,
	})
	return n > 0, constraint(err)
}

func (r pgDeliveryTargets) Delete(ctx context.Context, p authz.Proof, id string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsDelete, r.tok)
	if err != nil {
		return false, err
	}
	env, err := envOf(chain, authz.StoreDeliveryTargetsDelete)
	if err != nil {
		return false, err
	}
	n, err := r.q.DeleteDeliveryTargetReport(ctx, pggen.DeleteDeliveryTargetReportParams{
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, ID: id,
	})
	return n > 0, err
}

func (r pgDeliveryTargets) RecordQuotaRefusal(ctx context.Context, p authz.Proof, principalID string, at time.Time) error {
	chain, err := authz.Verify(p, authz.StoreDeliveryTargetsRecordQuota, r.tok)
	if err != nil {
		return err
	}
	n, err := r.q.UpdateDeliveryTargetQuotaNotice(ctx, pggen.UpdateDeliveryTargetQuotaNoticeParams{
		RefusedAt: pgRequiredTime(at), ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
		PrincipalID: principalID,
	})
	if err != nil || n > 0 {
		return constraint(err)
	}
	return constraint(r.q.InsertDeliveryTargetQuotaNotice(ctx, pggen.InsertDeliveryTargetQuotaNoticeParams{
		PrincipalID: principalID, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project),
		RefusedAt: pgRequiredTime(at),
	}))
}

func (r pgDeliveryTargets) SelectExpired(ctx context.Context, p authz.Proof, cutoff time.Time, limit int) ([]ExpiredDeliveryTargetReport, error) {
	if _, err := authz.Verify(p, authz.StoreDeliveryTargetsSelectExpired, r.tok); err != nil {
		return nil, err
	}
	rows, err := r.q.SelectExpiredDeliveryTargetReports(ctx, pggen.SelectExpiredDeliveryTargetReportsParams{
		ReceivedAt: pgRequiredTime(cutoff), BatchLimit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]ExpiredDeliveryTargetReport, 0, len(rows))
	for _, row := range rows {
		out = append(out, ExpiredDeliveryTargetReport{
			ID: row.ID, OrgID: row.OrgID, ProjectID: row.ProjectID, EnvironmentID: row.EnvironmentID,
			PrincipalID: row.PrincipalID, ReceivedAt: row.ReceivedAt.Time.UTC(),
		})
	}
	return out, nil
}

func (r pgDeliveryTargets) Purge(ctx context.Context, p authz.Proof, id string, cutoff time.Time) (bool, error) {
	if _, err := authz.Verify(p, authz.StoreDeliveryTargetsPurge, r.tok); err != nil {
		return false, err
	}
	n, err := r.q.PurgeDeliveryTargetReport(ctx, pggen.PurgeDeliveryTargetReportParams{ID: id, ReceivedAt: pgRequiredTime(cutoff)})
	return n > 0, err
}

func deliveryTargetFromPG(row pggen.DeliveryTargetReport) (DeliveryTargetReport, error) {
	conditions, err := decodeConditions(row.ID, row.Conditions)
	if err != nil {
		return DeliveryTargetReport{}, err
	}
	var refused time.Time
	if row.RefusedAt.Valid {
		refused = row.RefusedAt.Time.UTC()
	}
	return DeliveryTargetReport{
		ID: row.ID, EnvironmentID: row.EnvironmentID, PrincipalID: row.PrincipalID,
		Target: deliverytarget.Target{
			ClusterID: row.ClusterID, InstanceUID: row.InstanceUid, Namespace: row.Namespace,
			Name: row.Name, UID: row.TargetUid,
		},
		Vocabulary: int(row.Vocabulary), Generation: row.Generation, ObservedGeneration: row.ObservedGeneration,
		ReportedAt: row.ReportedAt.Time.UTC(), ReceivedAt: row.ReceivedAt.Time.UTC(),
		ReportIntervalSeconds: int64(row.ReportIntervalSeconds), Lifecycle: row.Lifecycle, Conditions: conditions,
		Reporter: row.Reporter, ReporterVersion: row.ReporterVersion,
		RefusalCause: row.RefusalCause.String, RefusedAt: refused, CreatedAt: row.CreatedAt.Time.UTC(),
	}, nil
}
