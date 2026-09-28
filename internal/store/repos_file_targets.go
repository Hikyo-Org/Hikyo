package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Generic file synchronization (#164). A file target binds one environment,
// one key selection and one workload service account; it carries no host
// path. Every method verifies the proof at the boundary and binds the tenant
// chain from the verified proof.

// FileTarget is one file-target row.
type FileTarget struct {
	ID                   string
	EnvironmentID        string
	Name                 string
	ServiceAccountID     string
	PrincipalID          string
	Generation           int64
	AuthorityPrincipalID string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	// Report is the client's last value-free assertion, nil until the first.
	Report *FileTargetReport
	// Keys is attached by the service from Keys; the row read leaves it nil.
	Keys []FileTargetKey
}

// FileTargetReport is the client's last accepted report.
type FileTargetReport struct {
	State      string
	Revision   int64
	Stamp      string
	Generation int64
	ReportedAt time.Time
	ReceivedAt time.Time
}

// FileTargetKey is one selected key: its immutable id, current name and
// classification.
type FileTargetKey struct {
	KeyID          string
	Name           string
	Classification string
}

// FileTargetReader is the read side.
type FileTargetReader interface {
	List(ctx context.Context, p authz.Proof) ([]FileTarget, error)
	Get(ctx context.Context, p authz.Proof, id string) (FileTarget, error)
	Keys(ctx context.Context, p authz.Proof, id string) ([]FileTargetKey, error)
	KeysForTargets(ctx context.Context, p authz.Proof, ids []string) (map[string][]FileTargetKey, error)
	// ForPrincipal returns the target a workload principal is bound to in the
	// proof's project, or ErrNotFound when it is bound to none.
	ForPrincipal(ctx context.Context, p authz.Proof, principalID string) (FileTarget, error)
}

// FileTargetRepo is the write bundle's file-target surface.
type FileTargetRepo interface {
	FileTargetReader
	Create(ctx context.Context, p authz.Proof, target FileTarget, keyIDs []string) error
	// ReplaceKeys swaps the key selection under a generation compare-and-swap
	// and returns false when the expected generation is stale.
	ReplaceKeys(ctx context.Context, p authz.Proof, target FileTarget, expectedGeneration int64, keyIDs []string, authority string, at time.Time) (bool, error)
	Delete(ctx context.Context, p authz.Proof, id string) (bool, error)
	// RecordReport stores the report iff principalID is the target's bound
	// principal and the target lives in the proof's environment.
	RecordReport(ctx context.Context, p authz.Proof, id, principalID string, report FileTargetReport) (bool, error)
}

func (s sqliteReadRepos) FileTargets() FileTargetReader { return s.r.FileTargets() }
func (p pgReadRepos) FileTargets() FileTargetReader     { return p.r.FileTargets() }

func (r sqliteRepos) FileTargets() FileTargetRepo {
	return sqliteFileTargets{q: sqlitegen.New(r.db), tok: r.tok}
}

func (r pgRepos) FileTargets() FileTargetRepo {
	return pgFileTargets{q: pggen.New(r.db), tok: r.tok}
}

// --- sqlite ---

type sqliteFileTargets struct {
	q   *sqlitegen.Queries
	tok *authz.TxToken
}

func fileTargetFromSQLite(row sqlitegen.GetFileTargetRow) (FileTarget, error) {
	created, err := parseTime("file target", row.ID, row.CreatedAt)
	if err != nil {
		return FileTarget{}, err
	}
	updated, err := parseTime("file target", row.ID, row.UpdatedAt)
	if err != nil {
		return FileTarget{}, err
	}
	out := FileTarget{
		ID: row.ID, EnvironmentID: row.EnvironmentID, Name: row.Name,
		ServiceAccountID: row.ServiceAccountID, PrincipalID: row.PrincipalID,
		Generation: row.Generation, AuthorityPrincipalID: row.AuthorityPrincipalID,
		CreatedAt: created, UpdatedAt: updated,
	}
	if row.ReportState.Valid {
		received, err := parseTime("file target report", row.ID, row.ReceivedAt.String)
		if err != nil {
			return FileTarget{}, err
		}
		reported, err := parseTime("file target report", row.ID, row.ReportedAt.String)
		if err != nil {
			return FileTarget{}, err
		}
		out.Report = &FileTargetReport{
			State: row.ReportState.String, Revision: row.ReportRevision.Int64, Stamp: row.ReportStamp.String,
			Generation: row.ReportGeneration.Int64, ReportedAt: reported, ReceivedAt: received,
		}
	}
	return out, nil
}

func (r sqliteFileTargets) List(ctx context.Context, p authz.Proof) ([]FileTarget, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListFileTargets(ctx, sqlitegen.ListFileTargetsParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	out := make([]FileTarget, 0, len(rows))
	for _, row := range rows {
		t, err := fileTargetFromSQLite(sqlitegen.GetFileTargetRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (r sqliteFileTargets) Get(ctx context.Context, p authz.Proof, id string) (FileTarget, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsGet, r.tok)
	if err != nil {
		return FileTarget{}, err
	}
	row, err := r.q.GetFileTarget(ctx, sqlitegen.GetFileTargetParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return FileTarget{}, ErrNotFound
	}
	if err != nil {
		return FileTarget{}, err
	}
	return fileTargetFromSQLite(row)
}

func (r sqliteFileTargets) ForPrincipal(ctx context.Context, p authz.Proof, principalID string) (FileTarget, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsForPrincipal, r.tok)
	if err != nil {
		return FileTarget{}, err
	}
	row, err := r.q.GetFileTargetForPrincipal(ctx, sqlitegen.GetFileTargetForPrincipalParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), PrincipalID: principalID})
	if errors.Is(err, sql.ErrNoRows) {
		return FileTarget{}, ErrNotFound
	}
	if err != nil {
		return FileTarget{}, err
	}
	return fileTargetFromSQLite(sqlitegen.GetFileTargetRow(row))
}

func (r sqliteFileTargets) Keys(ctx context.Context, p authz.Proof, id string) ([]FileTargetKey, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsKeys, r.tok)
	if err != nil {
		return nil, err
	}
	selections, err := r.keysForTargets(ctx, chain, []string{id})
	return selections[id], err
}

func (r sqliteFileTargets) KeysForTargets(ctx context.Context, p authz.Proof, ids []string) (map[string][]FileTargetKey, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsKeysForTargets, r.tok)
	if err != nil {
		return nil, err
	}
	return r.keysForTargets(ctx, chain, ids)
}

// keysForTargets accepts only the chain verified by the public read methods.
func (r sqliteFileTargets) keysForTargets(ctx context.Context, chain domain.Scope, ids []string) (map[string][]FileTargetKey, error) {
	rows, err := r.q.ListFileTargetCatalogue(ctx, sqlitegen.ListFileTargetCatalogueParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	catalogue := make(map[string]FileTargetKey, len(rows))
	for _, row := range rows {
		catalogue[row.ID] = FileTargetKey{KeyID: row.ID, Name: row.Name, Classification: row.Classification}
	}
	out := make(map[string][]FileTargetKey, len(ids))
	for _, id := range ids {
		selected, err := r.q.ListFileTargetKeyIDs(ctx, sqlitegen.ListFileTargetKeyIDsParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), TargetID: id})
		if err != nil {
			return nil, err
		}
		keys := make([]FileTargetKey, 0, len(selected))
		for _, keyID := range selected {
			if key, ok := catalogue[keyID]; ok {
				keys = append(keys, key)
			}
		}
		slices.SortFunc(keys, func(a, b FileTargetKey) int {
			if c := strings.Compare(a.Name, b.Name); c != 0 {
				return c
			}
			return strings.Compare(a.KeyID, b.KeyID)
		})
		out[id] = keys
	}
	return out, nil
}

func (r sqliteFileTargets) Create(ctx context.Context, p authz.Proof, t FileTarget, keyIDs []string) error {
	chain, err := authz.Verify(p, authz.StoreFileTargetsCreate, r.tok)
	if err != nil {
		return err
	}
	if err := constraint(r.q.InsertFileTarget(ctx, sqlitegen.InsertFileTargetParams{
		ID: t.ID, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), EnvironmentID: t.EnvironmentID,
		Name: t.Name, ServiceAccountID: t.ServiceAccountID, PrincipalID: t.PrincipalID,
		AuthorityPrincipalID: t.AuthorityPrincipalID, CreatedAt: fixedStamp(t.CreatedAt),
	})); err != nil {
		return err
	}
	for _, keyID := range keyIDs {
		if err := constraint(r.q.InsertFileTargetKey(ctx, sqlitegen.InsertFileTargetKeyParams{
			ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), EnvironmentID: t.EnvironmentID, TargetID: t.ID, KeyID: keyID,
		})); err != nil {
			return err
		}
	}
	return nil
}

func (r sqliteFileTargets) ReplaceKeys(ctx context.Context, p authz.Proof, t FileTarget, expected int64, keyIDs []string, authority string, at time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsReplaceKeys, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.BumpFileTargetGeneration(ctx, sqlitegen.BumpFileTargetGenerationParams{
		UpdatedAt: fixedStamp(at), AuthorityPrincipalID: authority,
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ID: t.ID, ExpectedGeneration: expected,
	})
	if err != nil || n == 0 {
		return false, err
	}
	if err := r.q.DeleteFileTargetKeys(ctx, sqlitegen.DeleteFileTargetKeysParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), TargetID: t.ID}); err != nil {
		return false, err
	}
	for _, keyID := range keyIDs {
		if err := constraint(r.q.InsertFileTargetKey(ctx, sqlitegen.InsertFileTargetKeyParams{
			ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), EnvironmentID: t.EnvironmentID, TargetID: t.ID, KeyID: keyID,
		})); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (r sqliteFileTargets) Delete(ctx context.Context, p authz.Proof, id string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsDelete, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.DeleteFileTarget(ctx, sqlitegen.DeleteFileTargetParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ID: id})
	return n > 0, err
}

func (r sqliteFileTargets) RecordReport(ctx context.Context, p authz.Proof, id, principalID string, rep FileTargetReport) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsRecordReport, r.tok)
	if err != nil {
		return false, err
	}
	env, err := envOf(chain, authz.StoreFileTargetsRecordReport)
	if err != nil {
		return false, err
	}
	n, err := r.q.RecordFileTargetReport(ctx, sqlitegen.RecordFileTargetReportParams{
		ReportState: nullString(rep.State), ReportRevision: sql.NullInt64{Int64: rep.Revision, Valid: true},
		ReportStamp: nullString(rep.Stamp), ReportGeneration: sql.NullInt64{Int64: rep.Generation, Valid: true},
		ReportedAt: nullTimeString(rep.ReportedAt), ReceivedAt: nullTimeString(rep.ReceivedAt),
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, ID: id, PrincipalID: principalID,
	})
	return n > 0, constraint(err)
}

// --- postgres ---

type pgFileTargets struct {
	q   *pggen.Queries
	tok *authz.TxToken
}

func fileTargetFromPG(row pggen.GetFileTargetRow) FileTarget {
	out := FileTarget{
		ID: row.ID, EnvironmentID: row.EnvironmentID, Name: row.Name,
		ServiceAccountID: row.ServiceAccountID, PrincipalID: row.PrincipalID,
		Generation: row.Generation, AuthorityPrincipalID: row.AuthorityPrincipalID,
		CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
	}
	if row.ReportState.Valid {
		out.Report = &FileTargetReport{
			State: row.ReportState.String, Revision: row.ReportRevision.Int64, Stamp: row.ReportStamp.String,
			Generation: row.ReportGeneration.Int64, ReportedAt: row.ReportedAt.Time.UTC(), ReceivedAt: row.ReceivedAt.Time.UTC(),
		}
	}
	return out
}

func (r pgFileTargets) List(ctx context.Context, p authz.Proof) ([]FileTarget, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListFileTargets(ctx, pggen.ListFileTargetsParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	out := make([]FileTarget, 0, len(rows))
	for _, row := range rows {
		out = append(out, fileTargetFromPG(pggen.GetFileTargetRow(row)))
	}
	return out, nil
}

func (r pgFileTargets) Get(ctx context.Context, p authz.Proof, id string) (FileTarget, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsGet, r.tok)
	if err != nil {
		return FileTarget{}, err
	}
	row, err := r.q.GetFileTarget(ctx, pggen.GetFileTargetParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return FileTarget{}, ErrNotFound
	}
	if err != nil {
		return FileTarget{}, err
	}
	return fileTargetFromPG(row), nil
}

func (r pgFileTargets) ForPrincipal(ctx context.Context, p authz.Proof, principalID string) (FileTarget, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsForPrincipal, r.tok)
	if err != nil {
		return FileTarget{}, err
	}
	row, err := r.q.GetFileTargetForPrincipal(ctx, pggen.GetFileTargetForPrincipalParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), PrincipalID: principalID})
	if errors.Is(err, pgx.ErrNoRows) {
		return FileTarget{}, ErrNotFound
	}
	if err != nil {
		return FileTarget{}, err
	}
	return fileTargetFromPG(pggen.GetFileTargetRow(row)), nil
}

func (r pgFileTargets) Keys(ctx context.Context, p authz.Proof, id string) ([]FileTargetKey, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsKeys, r.tok)
	if err != nil {
		return nil, err
	}
	selections, err := r.keysForTargets(ctx, chain, []string{id})
	return selections[id], err
}

func (r pgFileTargets) KeysForTargets(ctx context.Context, p authz.Proof, ids []string) (map[string][]FileTargetKey, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsKeysForTargets, r.tok)
	if err != nil {
		return nil, err
	}
	return r.keysForTargets(ctx, chain, ids)
}

// keysForTargets accepts only the chain verified by the public read methods.
func (r pgFileTargets) keysForTargets(ctx context.Context, chain domain.Scope, ids []string) (map[string][]FileTargetKey, error) {
	rows, err := r.q.ListFileTargetCatalogue(ctx, pggen.ListFileTargetCatalogueParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	catalogue := make(map[string]FileTargetKey, len(rows))
	for _, row := range rows {
		catalogue[row.ID] = FileTargetKey{KeyID: row.ID, Name: row.Name, Classification: row.Classification}
	}
	out := make(map[string][]FileTargetKey, len(ids))
	for _, id := range ids {
		selected, err := r.q.ListFileTargetKeyIDs(ctx, pggen.ListFileTargetKeyIDsParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), TargetID: id})
		if err != nil {
			return nil, err
		}
		keys := make([]FileTargetKey, 0, len(selected))
		for _, keyID := range selected {
			if key, ok := catalogue[keyID]; ok {
				keys = append(keys, key)
			}
		}
		slices.SortFunc(keys, func(a, b FileTargetKey) int {
			if c := strings.Compare(a.Name, b.Name); c != 0 {
				return c
			}
			return strings.Compare(a.KeyID, b.KeyID)
		})
		out[id] = keys
	}
	return out, nil
}

func (r pgFileTargets) Create(ctx context.Context, p authz.Proof, t FileTarget, keyIDs []string) error {
	chain, err := authz.Verify(p, authz.StoreFileTargetsCreate, r.tok)
	if err != nil {
		return err
	}
	if err := constraint(r.q.InsertFileTarget(ctx, pggen.InsertFileTargetParams{
		ID: t.ID, ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), EnvironmentID: t.EnvironmentID,
		Name: t.Name, ServiceAccountID: t.ServiceAccountID, PrincipalID: t.PrincipalID,
		AuthorityPrincipalID: t.AuthorityPrincipalID, CreatedAt: pgRequiredTime(t.CreatedAt),
	})); err != nil {
		return err
	}
	for _, keyID := range keyIDs {
		if err := constraint(r.q.InsertFileTargetKey(ctx, pggen.InsertFileTargetKeyParams{
			ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), EnvironmentID: t.EnvironmentID, TargetID: t.ID, KeyID: keyID,
		})); err != nil {
			return err
		}
	}
	return nil
}

func (r pgFileTargets) ReplaceKeys(ctx context.Context, p authz.Proof, t FileTarget, expected int64, keyIDs []string, authority string, at time.Time) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsReplaceKeys, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.BumpFileTargetGeneration(ctx, pggen.BumpFileTargetGenerationParams{
		UpdatedAt: pgRequiredTime(at), AuthorityPrincipalID: authority,
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ID: t.ID, ExpectedGeneration: expected,
	})
	if err != nil || n == 0 {
		return false, err
	}
	if err := r.q.DeleteFileTargetKeys(ctx, pggen.DeleteFileTargetKeysParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), TargetID: t.ID}); err != nil {
		return false, err
	}
	for _, keyID := range keyIDs {
		if err := constraint(r.q.InsertFileTargetKey(ctx, pggen.InsertFileTargetKeyParams{
			ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), EnvironmentID: t.EnvironmentID, TargetID: t.ID, KeyID: keyID,
		})); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (r pgFileTargets) Delete(ctx context.Context, p authz.Proof, id string) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsDelete, r.tok)
	if err != nil {
		return false, err
	}
	n, err := r.q.DeleteFileTarget(ctx, pggen.DeleteFileTargetParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ID: id})
	return n > 0, err
}

func (r pgFileTargets) RecordReport(ctx context.Context, p authz.Proof, id, principalID string, rep FileTargetReport) (bool, error) {
	chain, err := authz.Verify(p, authz.StoreFileTargetsRecordReport, r.tok)
	if err != nil {
		return false, err
	}
	env, err := envOf(chain, authz.StoreFileTargetsRecordReport)
	if err != nil {
		return false, err
	}
	n, err := r.q.RecordFileTargetReport(ctx, pggen.RecordFileTargetReportParams{
		ReportState: pgText(rep.State), ReportRevision: pgtype.Int8{Int64: rep.Revision, Valid: true},
		ReportStamp: pgText(rep.Stamp), ReportGeneration: pgtype.Int8{Int64: rep.Generation, Valid: true},
		ReportedAt: pgRequiredTime(rep.ReportedAt), ReceivedAt: pgRequiredTime(rep.ReceivedAt),
		ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, ID: id, PrincipalID: principalID,
	})
	return n > 0, constraint(err)
}
