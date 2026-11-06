package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// The personal-values matrix counts and offboarding report (#1235,
// personal-values ADR). Every read returns counts, key names, environment ids
// and timestamps: never ciphertext, never an audit payload.

// PersonalCounts is one environment's personal state, counted.
type PersonalCounts struct {
	Holders  map[string]int  // by key id
	Notices  map[string]int  // by key name
	Required map[string]bool // by key id
}

// PersonalReceipt is the latest recorded disclosure of one shared key, in one
// environment, to a member.
type PersonalReceipt struct {
	EnvironmentID string
	KeyName       string
	At            time.Time
}

// PersonalKeyChange is when a key name was last published in an environment.
type PersonalKeyChange struct {
	EnvironmentID string
	KeyName       string
	At            time.Time
}

// PersonalEvidence bounds the report's evidence: the oldest event the
// project's trail holds (Any is false when it holds none), and the instance's
// access-class retention in days (0 when unconfigured).
type PersonalEvidence struct {
	Oldest     time.Time
	Any        bool
	AccessDays int
}

// PersonalReportRepo is the proof-bound report surface.
type PersonalReportRepo interface {
	// Counts reads the proof environment's holder, notice and requirement counts.
	Counts(ctx context.Context, p authz.Proof) (PersonalCounts, error)
	// Receipts reads the project's disclosures to member, as actor or as the
	// recorded human authority.
	Receipts(ctx context.Context, p authz.Proof, member string) ([]PersonalReceipt, error)
	KeyChanges(ctx context.Context, p authz.Proof) ([]PersonalKeyChange, error)
	// Evidence reads how far back the project's trail can answer.
	Evidence(ctx context.Context, p authz.Proof) (PersonalEvidence, error)
	// MemberSeen reports whether any project event names member.
	MemberSeen(ctx context.Context, p authz.Proof, member string) (bool, error)
}

// --- sqlite ---

type sqlitePersonalReport struct {
	q   *sqlitegen.Queries
	tok *authz.TxToken
}

func (r sqliteRepos) PersonalReport() PersonalReportRepo {
	return sqlitePersonalReport{q: sqlitegen.New(r.db), tok: r.tok}
}

func (r sqlitePersonalReport) Counts(ctx context.Context, p authz.Proof) (PersonalCounts, error) {
	chain, env, err := envChain(p, authz.StorePersonalReportCounts, r.tok)
	if err != nil {
		return PersonalCounts{}, err
	}
	org, project := string(chain.Org), string(chain.Project)
	out := PersonalCounts{Holders: map[string]int{}, Notices: map[string]int{}, Required: map[string]bool{}}
	holders, err := r.q.PersonalValueHolderCounts(ctx, sqlitegen.PersonalValueHolderCountsParams{ChainOrgID: org, ChainProjectID: project, ChainEnvID: env})
	if err != nil {
		return PersonalCounts{}, err
	}
	for _, h := range holders {
		out.Holders[h.KeyID] = int(h.Holders)
	}
	notices, err := r.q.PersonalValueNoticeCounts(ctx, sqlitegen.PersonalValueNoticeCountsParams{ChainOrgID: org, ChainProjectID: project, ChainEnvID: env})
	if err != nil {
		return PersonalCounts{}, err
	}
	for _, n := range notices {
		out.Notices[n.KeyName] = int(n.Notices)
	}
	required, err := r.q.PersonalRequiredKeysForEnvironment(ctx, sqlitegen.PersonalRequiredKeysForEnvironmentParams{ChainOrgID: org, ChainProjectID: project, ChainEnvID: env})
	if err != nil {
		return PersonalCounts{}, err
	}
	for _, k := range required {
		out.Required[k] = true
	}
	return out, nil
}

func (r sqlitePersonalReport) Receipts(ctx context.Context, p authz.Proof, member string) ([]PersonalReceipt, error) {
	chain, err := authz.Verify(p, authz.StorePersonalReportReceipts, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.PersonalReportReceipts(ctx, sqlitegen.PersonalReportReceiptsParams{
		ChainOrgID: string(chain.Org), ChainProjectID: nullString(string(chain.Project)), Member: nullString(member),
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalReceipt, 0, len(rows))
	for _, row := range rows {
		at, err := parseTime("audit receipt", row.EnvID.String, row.LastReceivedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, PersonalReceipt{EnvironmentID: row.EnvID.String, KeyName: row.KeyName, At: at})
	}
	return out, nil
}

func (r sqlitePersonalReport) KeyChanges(ctx context.Context, p authz.Proof) ([]PersonalKeyChange, error) {
	chain, err := authz.Verify(p, authz.StorePersonalReportKeyChanges, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.PersonalReportKeyChanges(ctx, sqlitegen.PersonalReportKeyChangesParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalKeyChange, 0, len(rows))
	for _, row := range rows {
		at, err := parseTime("key change", row.EnvironmentID, row.LastChangedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, PersonalKeyChange{EnvironmentID: row.EnvironmentID, KeyName: row.KeyName, At: at})
	}
	return out, nil
}

func (r sqlitePersonalReport) Evidence(ctx context.Context, p authz.Proof) (PersonalEvidence, error) {
	chain, err := authz.Verify(p, authz.StorePersonalReportEvidence, r.tok)
	if err != nil {
		return PersonalEvidence{}, err
	}
	var out PersonalEvidence
	policy, err := r.q.GetAuditRetentionPolicy(ctx)
	switch {
	case err == nil:
		out.AccessDays = int(policy.AccessDays)
	case !errors.Is(err, sql.ErrNoRows):
		return PersonalEvidence{}, err
	}
	row, err := r.q.PersonalReportEvidenceFloor(ctx, sqlitegen.PersonalReportEvidenceFloorParams{ChainOrgID: string(chain.Org), ChainProjectID: nullString(string(chain.Project))})
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return PersonalEvidence{}, err
	}
	if out.Oldest, err = parseTime("audit event", row.ID, row.RecordedAt); err != nil {
		return PersonalEvidence{}, err
	}
	out.Any = true
	return out, nil
}

func (r sqlitePersonalReport) MemberSeen(ctx context.Context, p authz.Proof, member string) (bool, error) {
	chain, err := authz.Verify(p, authz.StorePersonalReportMemberSeen, r.tok)
	if err != nil {
		return false, err
	}
	_, err = r.q.PersonalReportMemberSeen(ctx, sqlitegen.PersonalReportMemberSeenParams{
		ChainOrgID: string(chain.Org), ChainProjectID: nullString(string(chain.Project)), Member: nullString(member),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// --- postgres ---

type pgPersonalReport struct {
	q   *pggen.Queries
	tok *authz.TxToken
}

func (r pgRepos) PersonalReport() PersonalReportRepo {
	return pgPersonalReport{q: pggen.New(r.db), tok: r.tok}
}

func (r pgPersonalReport) Counts(ctx context.Context, p authz.Proof) (PersonalCounts, error) {
	chain, env, err := envChain(p, authz.StorePersonalReportCounts, r.tok)
	if err != nil {
		return PersonalCounts{}, err
	}
	org, project := string(chain.Org), string(chain.Project)
	out := PersonalCounts{Holders: map[string]int{}, Notices: map[string]int{}, Required: map[string]bool{}}
	holders, err := r.q.PersonalValueHolderCounts(ctx, pggen.PersonalValueHolderCountsParams{ChainOrgID: org, ChainProjectID: project, ChainEnvID: env})
	if err != nil {
		return PersonalCounts{}, err
	}
	for _, h := range holders {
		out.Holders[h.KeyID] = int(h.Holders)
	}
	notices, err := r.q.PersonalValueNoticeCounts(ctx, pggen.PersonalValueNoticeCountsParams{ChainOrgID: org, ChainProjectID: project, ChainEnvID: env})
	if err != nil {
		return PersonalCounts{}, err
	}
	for _, n := range notices {
		out.Notices[n.KeyName] = int(n.Notices)
	}
	required, err := r.q.PersonalRequiredKeysForEnvironment(ctx, pggen.PersonalRequiredKeysForEnvironmentParams{ChainOrgID: org, ChainProjectID: project, ChainEnvID: env})
	if err != nil {
		return PersonalCounts{}, err
	}
	for _, k := range required {
		out.Required[k] = true
	}
	return out, nil
}

func (r pgPersonalReport) Receipts(ctx context.Context, p authz.Proof, member string) ([]PersonalReceipt, error) {
	chain, err := authz.Verify(p, authz.StorePersonalReportReceipts, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.PersonalReportReceipts(ctx, pggen.PersonalReportReceiptsParams{
		ChainOrgID: string(chain.Org), ChainProjectID: pgText(string(chain.Project)), Member: pgText(member),
	})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalReceipt, 0, len(rows))
	for _, row := range rows {
		out = append(out, PersonalReceipt{EnvironmentID: row.EnvID.String, KeyName: row.KeyName, At: pgTimeToGo(row.LastReceivedAt)})
	}
	return out, nil
}

func (r pgPersonalReport) KeyChanges(ctx context.Context, p authz.Proof) ([]PersonalKeyChange, error) {
	chain, err := authz.Verify(p, authz.StorePersonalReportKeyChanges, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.PersonalReportKeyChanges(ctx, pggen.PersonalReportKeyChangesParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project)})
	if err != nil {
		return nil, err
	}
	out := make([]PersonalKeyChange, 0, len(rows))
	for _, row := range rows {
		out = append(out, PersonalKeyChange{EnvironmentID: row.EnvironmentID, KeyName: row.KeyName, At: pgTimeToGo(row.LastChangedAt)})
	}
	return out, nil
}

func (r pgPersonalReport) Evidence(ctx context.Context, p authz.Proof) (PersonalEvidence, error) {
	chain, err := authz.Verify(p, authz.StorePersonalReportEvidence, r.tok)
	if err != nil {
		return PersonalEvidence{}, err
	}
	var out PersonalEvidence
	policy, err := r.q.GetAuditRetentionPolicy(ctx)
	switch {
	case err == nil:
		out.AccessDays = int(policy.AccessDays)
	case !errors.Is(err, pgx.ErrNoRows):
		return PersonalEvidence{}, err
	}
	row, err := r.q.PersonalReportEvidenceFloor(ctx, pggen.PersonalReportEvidenceFloorParams{ChainOrgID: string(chain.Org), ChainProjectID: pgText(string(chain.Project))})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return PersonalEvidence{}, err
	}
	out.Oldest, out.Any = pgTimeToGo(row.RecordedAt), true
	return out, nil
}

func (r pgPersonalReport) MemberSeen(ctx context.Context, p authz.Proof, member string) (bool, error) {
	chain, err := authz.Verify(p, authz.StorePersonalReportMemberSeen, r.tok)
	if err != nil {
		return false, err
	}
	_, err = r.q.PersonalReportMemberSeen(ctx, pggen.PersonalReportMemberSeenParams{
		ChainOrgID: string(chain.Org), ChainProjectID: pgText(string(chain.Project)), Member: pgText(member),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
