package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func scanAdapterRecord(row interface{ Scan(...any) error }) (AdapterRecord, error) {
	var out AdapterRecord
	var credential int
	var credentialSetAt, credentialExpiresAt, createdAt adapterStoredTime
	err := row.Scan(&out.ID, &out.Provider, &out.Origin, &credential, &credentialSetAt, &credentialExpiresAt, &out.AuthorityPrincipalID, &out.State, &createdAt)
	if isNoRows(err) {
		return AdapterRecord{}, ErrNotFound
	}
	if err != nil {
		return AdapterRecord{}, err
	}
	out.CredentialPresent = credential == 1
	out.CredentialSetAt = credentialSetAt.value
	out.CredentialExpiresAt = credentialExpiresAt.value
	out.CreatedAt = createdAt.value
	return out, nil
}

const adapterRecordColumns = `id,provider,origin,CASE WHEN credential_ciphertext IS NULL THEN 0 ELSE 1 END,credential_set_at,credential_expires_at,authority_principal_id,state,created_at`

type adapterStoredTime struct{ value string }

// Time returns the stored instant, or nil for an absent one.
func (t adapterStoredTime) Time() (*time.Time, error) {
	if t.value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(timeFormat, t.value)
	if err != nil {
		return nil, fmt.Errorf("store: malformed adapter timestamp %q: %w", t.value, err)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func (t *adapterStoredTime) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		t.value = ""
		return nil
	case time.Time:
		t.value = CanonTime(value).Format(timeFormat)
		return nil
	case string:
		if value == "" {
			t.value = ""
			return nil
		}
		parsed, err := time.Parse(timeFormat, value)
		if err != nil {
			return fmt.Errorf("store: malformed adapter timestamp %q: %w", value, err)
		}
		t.value = CanonTime(parsed).Format(timeFormat)
		return nil
	case []byte:
		return t.Scan(string(value))
	default:
		return fmt.Errorf("store: unsupported adapter timestamp type %T", src)
	}
}

func (r adapterQueries) Get(ctx context.Context, p authz.Proof, adapterID string) (AdapterRecord, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersGet, r.tok)
	if err != nil {
		return AdapterRecord{}, err
	}
	return scanAdapterRecord(r.db.QueryRow(ctx, r.db.SQLPerEngine(
		`SELECT `+adapterRecordColumns+` FROM adapters WHERE id=? AND org_id=? AND project_id=? AND state<>'tombstoned'`,
		`SELECT `+adapterRecordColumns+` FROM adapters WHERE id=$1 AND org_id=$2 AND project_id=$3 AND state<>'tombstoned'`), adapterID, chain.Org, chain.Project))
}

func (r adapterQueries) Configuration(ctx context.Context, p authz.Proof, adapterID string) (AdapterRecord, []byte, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersConfiguration, r.tok)
	if err != nil {
		return AdapterRecord{}, nil, err
	}
	var record AdapterRecord
	var credential []byte
	var present int
	var credentialSetAt, credentialExpiresAt, createdAt adapterStoredTime
	err = r.db.QueryRow(ctx, r.db.SQLPerEngine(
		`SELECT `+adapterRecordColumns+`,credential_ciphertext FROM adapters WHERE id=? AND org_id=? AND project_id=? AND state<>'tombstoned'`,
		`SELECT `+adapterRecordColumns+`,credential_ciphertext FROM adapters WHERE id=$1 AND org_id=$2 AND project_id=$3 AND state<>'tombstoned'`), adapterID, chain.Org, chain.Project).Scan(&record.ID, &record.Provider, &record.Origin, &present, &credentialSetAt, &credentialExpiresAt, &record.AuthorityPrincipalID, &record.State, &createdAt, &credential)
	if isNoRows(err) {
		return AdapterRecord{}, nil, ErrNotFound
	}
	if err != nil {
		return AdapterRecord{}, nil, err
	}
	record.CredentialPresent = present == 1
	record.CredentialSetAt = credentialSetAt.value
	record.CredentialExpiresAt = credentialExpiresAt.value
	record.CreatedAt = createdAt.value
	return record, credential, nil
}

type adapterRecordRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func collectAdapterRecords(rows adapterRecordRows) ([]AdapterRecord, error) {
	var out []AdapterRecord
	for rows.Next() {
		record, err := scanAdapterRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r adapterQueries) List(ctx context.Context, p authz.Proof) ([]AdapterRecord, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersList, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, r.db.SQLPerEngine(
		`SELECT `+adapterRecordColumns+` FROM adapters WHERE org_id=? AND project_id=? AND state<>'tombstoned' ORDER BY id`,
		`SELECT `+adapterRecordColumns+` FROM adapters WHERE org_id=$1 AND project_id=$2 AND state<>'tombstoned' ORDER BY id`), chain.Org, chain.Project)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	return collectAdapterRecords(rows)
}

type adapterTargetRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func collectAdapterTargets(rows adapterTargetRows) ([]AdapterTarget, error) {
	var out []AdapterTarget
	for rows.Next() {
		target, err := scanAdapterTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, target)
	}
	return out, rows.Err()
}

const adapterTargetColumns = `t.id,t.adapter_id,t.environment_id,a.provider,a.origin,t.destination_kind,t.destination_owner,t.destination_name,t.destination_environment,t.destination_id,t.repository_id,t.visibility,t.selected_repository_ids,t.name_prefix,t.generation,t.state,t.sync_status,t.converged_revision,t.failure_names,t.warnings,a.authority_principal_id,t.paused_at,t.last_attempted_revision,t.last_attempted_at,t.last_error_class,t.drift_attention,COALESCE(j.state,''),j.next_attempt_at,COALESCE(j.attempt_count,0)`

// adapterTargetFrom is the FROM clause every adapterTargetColumns read uses:
// the owning adapter for provider/origin/authority, and the target's active
// outbox job for the pending-versus-running signal and the retry time. The
// job join is outer: a target between jobs has none. Postgres callers that
// lock must name the target (`FOR UPDATE OF t`); an outer-joined row cannot
// be locked.
const adapterTargetFrom = ` FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id LEFT JOIN adapter_outbox j ON j.id=t.active_job_id AND j.org_id=t.org_id AND j.project_id=t.project_id AND j.environment_id=t.environment_id`

func (r adapterQueries) ListTargets(ctx context.Context, p authz.Proof, adapterID string) ([]AdapterTarget, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersListTargets, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, r.db.SQLPerEngine(
		`SELECT `+adapterTargetColumns+adapterTargetFrom+` WHERE t.adapter_id=? AND t.org_id=? AND t.project_id=? AND t.state='active' ORDER BY t.id`,
		`SELECT `+adapterTargetColumns+adapterTargetFrom+` WHERE t.adapter_id=$1 AND t.org_id=$2 AND t.project_id=$3 AND t.state='active' ORDER BY t.id`), adapterID, chain.Org, chain.Project)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	targets, err := collectAdapterTargets(rows)
	if err != nil {
		return nil, err
	}
	if err := closeAdapterRows(rows); err != nil {
		return nil, err
	}
	for i := range targets {
		targets[i].Findings, err = r.targetFindings(ctx, chain, targets[i])
		if err != nil {
			return nil, err
		}
	}
	return targets, nil
}

func collectStrings(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]string, error) {
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (r adapterQueries) TargetKeyIDs(ctx context.Context, p authz.Proof, targetID string) ([]string, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersTargetKeyIDs, r.tok)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, r.db.SQL(
		`SELECT key_id FROM adapter_target_keys WHERE target_id=? AND org_id=? AND project_id=? ORDER BY key_id`,
	), targetID, chain.Org, chain.Project)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	return collectStrings(rows)
}

// TargetKeys is the target's explicit subset by name and classification. It
// reads the catalogue, not the published snapshot, so a member key that has
// no published value yet is still echoed as a member.
func (r adapterQueries) TargetKeys(ctx context.Context, p authz.Proof, targetID string) ([]AdapterTargetKey, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersTargetKeys, r.tok)
	if err != nil {
		return nil, err
	}
	query := r.db.SQL(
		`SELECT k.id,k.name,k.classification FROM adapter_target_keys tk JOIN keys k ON k.id=tk.key_id AND k.org_id=tk.org_id AND k.project_id=tk.project_id WHERE tk.target_id=? AND tk.org_id=? AND tk.project_id=? ORDER BY k.name`,
	)
	rows, err := r.db.Query(ctx, query, targetID, chain.Org, chain.Project)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	out := []AdapterTargetKey{}
	for rows.Next() {
		var key AdapterTargetKey
		if err := rows.Scan(&key.ID, &key.Name, &key.Classification); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func validateTargetMutation(m AdapterTargetMutation) error {
	if m.ID == "" || m.AdapterID == "" || m.EnvironmentID == "" || m.DestinationOwner == "" || m.DestinationID <= 0 || len(m.KeyIDs) == 0 {
		return fmt.Errorf("%w: adapter target requires ids, environment, destination, and an explicit non-empty key subset", domain.ErrInvalid)
	}
	switch m.DestinationKind {
	case string(adapter.Repository):
		if m.DestinationName == "" || m.DestinationEnvironment != "" || m.RepositoryID != 0 || m.Visibility != "" || len(m.SelectedRepositoryIDs) != 0 {
			return fmt.Errorf("%w: repository target requires repository name", domain.ErrInvalid)
		}
	case string(adapter.Organization):
		if m.DestinationName != "" || m.DestinationEnvironment != "" || m.RepositoryID != 0 {
			return fmt.Errorf("%w: organization target does not take repository routing fields", domain.ErrInvalid)
		}
		switch m.Visibility {
		case "", "all", "private":
			if len(m.SelectedRepositoryIDs) != 0 {
				return fmt.Errorf("%w: selected repository ids require selected visibility", domain.ErrInvalid)
			}
		case "selected":
			if len(m.SelectedRepositoryIDs) == 0 {
				return fmt.Errorf("%w: selected visibility requires repository ids", domain.ErrInvalid)
			}
		default:
			return fmt.Errorf("%w: organization visibility must be all, private, or selected", domain.ErrInvalid)
		}
	case string(adapter.Environment):
		if m.DestinationName == "" || m.DestinationEnvironment == "" || m.RepositoryID <= 0 || m.Visibility != "" || len(m.SelectedRepositoryIDs) != 0 {
			return fmt.Errorf("%w: environment target requires repository and environment identities", domain.ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unsupported adapter destination kind", domain.ErrInvalid)
	}
	seenRepositories := map[int64]bool{}
	for _, id := range m.SelectedRepositoryIDs {
		if id <= 0 || seenRepositories[id] {
			return fmt.Errorf("%w: selected repository ids must be positive and unique", domain.ErrInvalid)
		}
		seenRepositories[id] = true
	}
	seen := map[string]bool{}
	for _, id := range m.KeyIDs {
		if id == "" || seen[id] {
			return fmt.Errorf("%w: adapter target key ids must be non-empty and unique", domain.ErrInvalid)
		}
		seen[id] = true
	}
	return nil
}

func targetManifest(ctx context.Context, db adapterDB, chain domain.Scope, m AdapterTargetMutation) ([]adapter.ManifestEntry, error) {
	providerQuery := db.SQL(
		`SELECT provider FROM adapters WHERE id=? AND org_id=? AND project_id=?`,
	)
	providerRows, err := db.Query(ctx, providerQuery, m.AdapterID, chain.Org, chain.Project)
	if err != nil {
		return nil, err
	}
	var provider string
	if !providerRows.Next() {
		if err := providerRows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	if err := providerRows.Scan(&provider); err != nil {
		return nil, err
	}
	if providerRows.Next() {
		return nil, fmt.Errorf("store: adapter provider lookup was not unique")
	}
	if err := providerRows.Err(); err != nil {
		return nil, err
	}
	args := []any{chain.Org, chain.Project}
	for _, id := range m.KeyIDs {
		args = append(args, id)
	}
	q := db.SQLPerEngine(
		`SELECT id,name,classification FROM keys WHERE org_id=? AND project_id=? AND id IN (`+db.Placeholders(len(m.KeyIDs), 3)+`) ORDER BY id`,
		`SELECT id,name,classification FROM keys WHERE org_id=$1 AND project_id=$2 AND id IN (`+db.Placeholders(len(m.KeyIDs), 3)+`) ORDER BY id`)
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var manifest []adapter.ManifestEntry
	for rows.Next() {
		var row adapter.ManifestEntry
		var classification string
		if err := rows.Scan(&row.KeyID, &row.CanonicalName, &classification); err != nil {
			return nil, err
		}
		row.Classification = adapter.Classification(classification)
		manifest = append(manifest, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(manifest) != len(m.KeyIDs) {
		return nil, ErrNotFound
	}
	if err := adapter.ValidateProviderManifest(provider, m.NamePrefix, manifest, false); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	return manifest, nil
}

func refuseDestinationNameCollision(ctx context.Context, db adapterDB, chain domain.Scope, m AdapterTargetMutation, manifest []adapter.ManifestEntry, excludeTargetID string) error {
	desired := map[string]struct{}{m.NamePrefix + adapter.SentinelName: {}}
	for _, entry := range manifest {
		desired[m.NamePrefix+entry.CanonicalName] = struct{}{}
	}
	q := db.SQLPerEngine(`SELECT t.id,t.name_prefix,COALESCE(k.name,'')
		FROM adapter_targets t
		JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id
		JOIN adapters candidate ON candidate.id=? AND candidate.org_id=t.org_id AND candidate.project_id=t.project_id
		LEFT JOIN adapter_target_keys tk ON tk.target_id=t.id AND tk.org_id=t.org_id AND tk.project_id=t.project_id AND tk.environment_id=t.environment_id
		LEFT JOIN keys k ON k.id=tk.key_id AND k.org_id=tk.org_id AND k.project_id=tk.project_id
		WHERE t.org_id=? AND t.project_id=? AND t.state='active' AND a.state='active'
		AND a.origin=candidate.origin AND t.destination_kind=? AND t.destination_id=? AND t.id<>?
		ORDER BY t.id,k.name`,
		`SELECT t.id,t.name_prefix,COALESCE(k.name,'')
			FROM adapter_targets t
			JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id
			JOIN adapters candidate ON candidate.id=$1 AND candidate.org_id=t.org_id AND candidate.project_id=t.project_id
			LEFT JOIN adapter_target_keys tk ON tk.target_id=t.id AND tk.org_id=t.org_id AND tk.project_id=t.project_id AND tk.environment_id=t.environment_id
			LEFT JOIN keys k ON k.id=tk.key_id AND k.org_id=tk.org_id AND k.project_id=tk.project_id
			WHERE t.org_id=$2 AND t.project_id=$3 AND t.state='active' AND a.state='active'
			AND a.origin=candidate.origin AND t.destination_kind=$4 AND t.destination_id=$5 AND t.id<>$6
			ORDER BY t.id,k.name`)
	rows, err := db.Query(ctx, q, m.AdapterID, chain.Org, chain.Project, m.DestinationKind, m.DestinationID, excludeTargetID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var targetID, prefix, canonicalName string
		if err := rows.Scan(&targetID, &prefix, &canonicalName); err != nil {
			return err
		}
		if _, found := desired[prefix+adapter.SentinelName]; found {
			return fmt.Errorf("%w: effective name %q is already configured by target %q on this destination", domain.ErrConflict, prefix+adapter.SentinelName, targetID)
		}
		if canonicalName != "" {
			if _, found := desired[prefix+canonicalName]; found {
				return fmt.Errorf("%w: effective name %q is already configured by target %q on this destination", domain.ErrConflict, prefix+canonicalName, targetID)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	pendingQuery := db.SQL(
		`SELECT c.target_id,c.effective_name FROM adapter_route_move_claims c JOIN adapters candidate ON candidate.id=? AND candidate.org_id=c.org_id AND candidate.project_id=c.project_id WHERE c.org_id=? AND c.project_id=? AND c.provider_origin=candidate.origin AND c.destination_kind=? AND c.destination_owner=? AND c.destination_name=? AND c.destination_environment=? AND c.target_id<>? ORDER BY c.target_id,c.effective_name`,
	)
	pendingRows, err := db.Query(ctx, pendingQuery, m.AdapterID, chain.Org, chain.Project, m.DestinationKind, m.DestinationOwner, m.DestinationName, m.DestinationEnvironment, excludeTargetID)
	if err != nil {
		return err
	}
	for pendingRows.Next() {
		var targetID, effectiveName string
		if err := pendingRows.Scan(&targetID, &effectiveName); err != nil {
			return err
		}
		if _, found := desired[effectiveName]; found {
			return fmt.Errorf("%w: effective name %q is reserved by pending target %q on this destination", domain.ErrConflict, effectiveName, targetID)
		}
	}
	return pendingRows.Err()
}

func insertTargetConfig(ctx context.Context, db adapterDB, chain domain.Scope, m AdapterTargetMutation, at time.Time) error {
	manifest, err := targetManifest(ctx, db, chain, m)
	if err != nil {
		return err
	}
	if err := refuseDestinationNameCollision(ctx, db, chain, m, manifest, ""); err != nil {
		return err
	}
	selected, err := json.Marshal(m.SelectedRepositoryIDs)
	if err != nil {
		return err
	}
	q := db.SQL(
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_environment,destination_id,repository_id,visibility,selected_repository_ids,name_prefix,generation,state,sync_status,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,'active','never',?)`,
	)
	if _, err := db.Exec(ctx, q, m.ID, chain.Org, chain.Project, m.EnvironmentID, m.AdapterID, m.DestinationKind, m.DestinationOwner, m.DestinationName, m.DestinationEnvironment, m.DestinationID, m.RepositoryID, m.Visibility, selected, m.NamePrefix, db.Stamp(at)); err != nil {
		return constraint(err)
	}
	for _, keyID := range m.KeyIDs {
		q = db.SQL(
			`INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES (?,?,?,?,?,?)`,
		)
		if _, err := db.Exec(ctx, q, chain.Org, chain.Project, m.EnvironmentID, m.ID, m.AdapterID, keyID); err != nil {
			return constraint(err)
		}
	}
	return nil
}

func (r adapterQueries) Create(ctx context.Context, p authz.Proof, m AdapterCreate) (AdapterRecord, AdapterTarget, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersCreate, r.tok)
	if err != nil {
		return AdapterRecord{}, AdapterTarget{}, err
	}
	if m.ID == "" || (m.Provider != "forgejo" && m.Provider != "github-actions") || m.Origin == "" || len(m.CredentialCiphertext) == 0 || m.AuthorityPrincipalID == "" || m.Target.AdapterID != m.ID {
		return AdapterRecord{}, AdapterTarget{}, fmt.Errorf("%w: incomplete atomic adapter bootstrap", domain.ErrInvalid)
	}
	if err := validateTargetMutation(m.Target); err != nil {
		return AdapterRecord{}, AdapterTarget{}, err
	}
	at := CanonTime(m.At)
	stamp := r.db.Stamp(at)
	var expires any
	if !m.CredentialExpiresAt.IsZero() {
		expires = r.db.Stamp(m.CredentialExpiresAt)
	}
	if _, err := r.db.Exec(ctx, r.db.SQL(
		`INSERT INTO adapters (id,org_id,project_id,provider,origin,credential_ciphertext,credential_set_at,credential_expires_at,authority_principal_id,state,created_at) VALUES (?,?,?,?,?,?,?,?,?,'active',?)`,
	),
		m.ID, chain.Org, chain.Project, m.Provider, m.Origin, m.CredentialCiphertext, stamp, expires, m.AuthorityPrincipalID, stamp); err != nil {
		return AdapterRecord{}, AdapterTarget{}, constraint(err)
	}
	if err := insertTargetConfig(ctx, r.db, chain, m.Target, at); err != nil {
		return AdapterRecord{}, AdapterTarget{}, err
	}
	atString := at.Format(timeFormat)
	record := AdapterRecord{ID: m.ID, Provider: m.Provider, Origin: m.Origin, CredentialPresent: true, CredentialSetAt: atString, AuthorityPrincipalID: m.AuthorityPrincipalID, State: "active", CreatedAt: atString}
	if !m.CredentialExpiresAt.IsZero() {
		record.CredentialExpiresAt = CanonTime(m.CredentialExpiresAt).Format(timeFormat)
	}
	return record, mutationTarget(record, m.Target, 1), nil
}

func mutationTarget(record AdapterRecord, m AdapterTargetMutation, generation int64) AdapterTarget {
	return AdapterTarget{ID: m.ID, AdapterID: m.AdapterID, Provider: record.Provider, EnvironmentID: m.EnvironmentID, Origin: record.Origin, DestinationKind: m.DestinationKind, DestinationOwner: m.DestinationOwner, DestinationName: m.DestinationName, DestinationEnvironment: m.DestinationEnvironment, DestinationID: m.DestinationID, RepositoryID: m.RepositoryID, Visibility: m.Visibility, SelectedRepositoryIDs: append([]int64(nil), m.SelectedRepositoryIDs...), NamePrefix: m.NamePrefix, Generation: generation, State: "active", SyncStatus: "never", AuthorityPrincipalID: record.AuthorityPrincipalID}
}

func (r adapterQueries) AddTarget(ctx context.Context, p authz.Proof, m AdapterTargetUpdate) (AdapterTargetAddResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersAddTarget, r.tok)
	if err != nil {
		return AdapterTargetAddResult{}, err
	}
	if err := validateTargetMutation(m.Target); err != nil {
		return AdapterTargetAddResult{}, err
	}
	record, err := scanAdapterRecord(r.db.QueryRow(ctx, r.db.SQLPerEngine(
		`SELECT `+adapterRecordColumns+` FROM adapters WHERE id=? AND org_id=? AND project_id=? AND state='active'`,
		`SELECT `+adapterRecordColumns+` FROM adapters WHERE id=$1 AND org_id=$2 AND project_id=$3 AND state='active' FOR UPDATE`), m.Target.AdapterID, chain.Org, chain.Project))
	if err != nil {
		return AdapterTargetAddResult{}, err
	}
	if !record.CredentialPresent {
		return AdapterTargetAddResult{}, adapter.ErrProviderAuth
	}
	previousAuthority := record.AuthorityPrincipalID
	at := CanonTime(m.At)
	if err := insertTargetConfig(ctx, r.db, chain, m.Target, at); err != nil {
		return AdapterTargetAddResult{}, err
	}
	var expires any
	if !m.CredentialExpiresAt.IsZero() {
		expires = r.db.Stamp(m.CredentialExpiresAt)
	}
	if rows, err := r.db.Exec(ctx, r.db.SQL(
		`UPDATE adapters SET authority_principal_id=?,credential_expires_at=COALESCE(?,credential_expires_at) WHERE id=? AND org_id=? AND project_id=? AND state='active'`,
	),
		m.AuthorityPrincipalID, expires, m.Target.AdapterID, chain.Org, chain.Project); err != nil || rows != 1 {
		return AdapterTargetAddResult{}, errors.Join(err, ErrNotFound)
	}
	record.AuthorityPrincipalID = m.AuthorityPrincipalID
	if !m.CredentialExpiresAt.IsZero() {
		record.CredentialExpiresAt = CanonTime(m.CredentialExpiresAt).Format(timeFormat)
	}
	return AdapterTargetAddResult{Target: mutationTarget(record, m.Target, 1), PreviousAuthorityPrincipalID: previousAuthority, AuthorityPrincipalID: m.AuthorityPrincipalID}, nil
}

func (r adapterQueries) RecordCredentialExpiry(ctx context.Context, p authz.Proof, adapterID string, expiresAt time.Time) error {
	chain, err := authz.Verify(p, authz.StoreAdaptersRecordCredentialExpiry, r.tok)
	if err != nil {
		return err
	}
	if adapterID == "" || expiresAt.IsZero() {
		return fmt.Errorf("%w: credential expiry requires adapter and timestamp", domain.ErrInvalid)
	}
	rows, err := r.db.Exec(ctx, r.db.SQL(
		`UPDATE adapters SET credential_expires_at=? WHERE id=? AND org_id=? AND project_id=? AND state='active'`,
	),
		r.db.Stamp(expiresAt), adapterID, chain.Org, chain.Project)
	if err != nil || rows != 1 {
		return errors.Join(err, ErrNotFound)
	}
	return nil
}

func validateAdapterConfigureFence(fence AdapterConfigureFence) error {
	if fence.TargetID == "" || fence.EnvironmentID == "" || fence.DestinationKind != string(adapter.Environment) || fence.DestinationOwner == "" || fence.DestinationName == "" || fence.DestinationEnvironment == "" || fence.Generation != 1 || fence.EffectID == "" || fence.LeaseExpiresAt.IsZero() || fence.At.IsZero() || !fence.LeaseExpiresAt.After(fence.At) {
		return fmt.Errorf("%w: incomplete adapter configure fence", ErrConflict)
	}
	return nil
}

func (r adapterQueries) BeginConfigureEffect(ctx context.Context, p authz.Proof, fence AdapterConfigureFence) error {
	chain, err := authz.Verify(p, authz.StoreAdaptersBeginConfigureEffect, r.tok)
	if err != nil {
		return err
	}
	if err := validateAdapterConfigureFence(fence); err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, r.db.SQL(
		`INSERT INTO adapter_configure_fences (target_id,org_id,project_id,environment_id,destination_kind,destination_owner,destination_name,destination_environment,generation,effect_id,lease_expires_at,state,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,'leased',?)`,
	),
		fence.TargetID, chain.Org, chain.Project, fence.EnvironmentID, fence.DestinationKind, fence.DestinationOwner, fence.DestinationName, fence.DestinationEnvironment, fence.Generation, fence.EffectID, r.db.Stamp(fence.LeaseExpiresAt), r.db.Stamp(fence.At))
	return constraint(err)
}

func validateAdapterConfigureOutcome(targetID, effectID, outcome string, at time.Time) error {
	if targetID == "" || effectID == "" || (outcome != "succeeded" && outcome != "failed") || at.IsZero() {
		return fmt.Errorf("%w: incomplete adapter configure outcome", ErrConflict)
	}
	return nil
}

func (r adapterQueries) FinishConfigureEffect(ctx context.Context, p authz.Proof, targetID, effectID, outcome string, at time.Time) error {
	chain, err := authz.Verify(p, authz.StoreAdaptersFinishConfigureEffect, r.tok)
	if err != nil {
		return err
	}
	if err := validateAdapterConfigureOutcome(targetID, effectID, outcome, at); err != nil {
		return err
	}
	rows, err := r.db.Exec(ctx, r.db.SQL(
		`UPDATE adapter_configure_fences SET state=?,completed_at=? WHERE target_id=? AND effect_id=? AND org_id=? AND project_id=? AND state='leased'`,
	),
		outcome, r.db.Stamp(at), targetID, effectID, chain.Org, chain.Project)
	if err != nil {
		return constraint(err)
	}
	if rows != 1 {
		return ErrConflict
	}
	return nil
}

func (r adapterQueries) UpdateTarget(ctx context.Context, p authz.Proof, m AdapterTargetUpdate) (AdapterTargetUpdateResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersUpdateTarget, r.tok)
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	return updateTargetConfig(ctx, r.db, chain, m)
}

func updateTargetConfig(ctx context.Context, db adapterDB, chain domain.Scope, m AdapterTargetUpdate) (AdapterTargetUpdateResult, error) {
	if err := validateTargetMutation(m.Target); err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	if m.ExpectedGeneration <= 0 || m.AuthorityPrincipalID == "" {
		return AdapterTargetUpdateResult{}, fmt.Errorf("%w: target update requires generation and authority", domain.ErrInvalid)
	}
	manifest, err := targetManifest(ctx, db, chain, m.Target)
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	if err := refuseDestinationNameCollision(ctx, db, chain, m.Target, manifest, m.Target.ID); err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	lookup := db.SQLPerEngine(
		`SELECT `+adapterTargetColumns+adapterTargetFrom+` WHERE t.id=? AND t.org_id=? AND t.project_id=? AND t.state='active'`,
		`SELECT `+adapterTargetColumns+adapterTargetFrom+` WHERE t.id=$1 AND t.org_id=$2 AND t.project_id=$3 AND t.state='active' FOR UPDATE OF t`)
	rows, err := db.Query(ctx, lookup, m.Target.ID, chain.Org, chain.Project)
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	if !rows.Next() {
		return AdapterTargetUpdateResult{}, ErrNotFound
	}
	current, err := scanAdapterTarget(rows)
	for rows.Next() {
	}
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	if current.Generation != m.ExpectedGeneration {
		return AdapterTargetUpdateResult{}, adapter.ErrSuperseded
	}
	previousAuthority := current.AuthorityPrincipalID
	if current.AdapterID != m.Target.AdapterID || current.EnvironmentID != m.Target.EnvironmentID || current.DestinationKind != m.Target.DestinationKind || current.DestinationOwner != m.Target.DestinationOwner || current.DestinationName != m.Target.DestinationName || current.DestinationEnvironment != m.Target.DestinationEnvironment || current.DestinationID != m.Target.DestinationID || current.RepositoryID != m.Target.RepositoryID {
		return AdapterTargetUpdateResult{}, fmt.Errorf("%w: moving an adapter target requires the scrub transition", domain.ErrConflict)
	}
	var activeJob string
	activeQuery := db.SQL(
		`SELECT COALESCE(active_job_id,'') FROM adapter_targets WHERE id=? AND org_id=? AND project_id=? AND environment_id=?`,
	)
	if err := db.QueryRow(ctx, activeQuery, m.Target.ID, chain.Org, chain.Project, m.Target.EnvironmentID).Scan(&activeJob); err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	q := db.SQL(
		`DELETE FROM adapter_target_keys WHERE target_id=? AND org_id=? AND project_id=? AND environment_id=?`,
	)
	if _, err := db.Exec(ctx, q, m.Target.ID, chain.Org, chain.Project, m.Target.EnvironmentID); err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	for _, keyID := range m.Target.KeyIDs {
		q = db.SQL(
			`INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES (?,?,?,?,?,?)`,
		)
		if _, err := db.Exec(ctx, q, chain.Org, chain.Project, m.Target.EnvironmentID, m.Target.ID, m.Target.AdapterID, keyID); err != nil {
			return AdapterTargetUpdateResult{}, constraint(err)
		}
	}
	selectedJSON, err := json.Marshal(m.Target.SelectedRepositoryIDs)
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	q = db.SQL(
		`UPDATE adapter_targets SET visibility=?,selected_repository_ids=?,name_prefix=? WHERE id=? AND org_id=? AND project_id=? AND generation=? AND state='active' AND provider_lease_job_id IS NULL`,
	)
	n, err := db.Exec(ctx, q, m.Target.Visibility, selectedJSON, m.Target.NamePrefix, m.Target.ID, chain.Org, chain.Project, m.ExpectedGeneration)
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	if n != 1 {
		return AdapterTargetUpdateResult{}, adapter.ErrProviderBusy
	}
	enqueued, err := enqueuePublishedTargets(ctx, db, chain, []publishedAdapterTarget{{
		id: m.Target.ID, environmentID: m.Target.EnvironmentID, generation: current.Generation,
		activeJob: activeJob, authority: m.AuthorityPrincipalID,
	}}, CanonTime(m.At))
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	q = db.SQL(
		`UPDATE adapters SET authority_principal_id=? WHERE id=? AND org_id=? AND project_id=? AND state='active'`,
	)
	if n, err = db.Exec(ctx, q, m.AuthorityPrincipalID, m.Target.AdapterID, chain.Org, chain.Project); err != nil || n != 1 {
		return AdapterTargetUpdateResult{}, errors.Join(err, ErrNotFound)
	}
	current.NamePrefix = m.Target.NamePrefix
	current.Visibility = m.Target.Visibility
	current.SelectedRepositoryIDs = append([]int64(nil), m.Target.SelectedRepositoryIDs...)
	current.Generation = enqueued[0].Generation
	current.SyncStatus = "converging"
	current.FailureNames = nil
	current.AuthorityPrincipalID = m.AuthorityPrincipalID
	slices.Sort(m.Target.KeyIDs)
	return AdapterTargetUpdateResult{Target: current, Enqueue: enqueued[0], PreviousAuthorityPrincipalID: previousAuthority, AuthorityPrincipalID: m.AuthorityPrincipalID}, nil
}

// targetFindings projects the latest completed effect per name in the current
// generation. A later successful effect clears its finding; older generations
// cannot describe a reconfigured destination. Values and audit payloads are never read.
func (r adapterQueries) targetFindings(ctx context.Context, chain domain.Scope, target AdapterTarget) ([]AdapterFinding, error) {
	rows, err := r.db.Query(ctx, r.db.SQL(`SELECT surface,effective_name,finding FROM (
 SELECT e.surface,e.effective_name,e.finding,ROW_NUMBER() OVER (PARTITION BY e.surface,UPPER(e.effective_name) ORDER BY e.finished_at DESC,e.id DESC) AS ordinal
 FROM adapter_effects e JOIN adapter_outbox o ON o.id=e.job_id AND o.org_id=e.org_id AND o.project_id=e.project_id AND o.environment_id=e.environment_id
 WHERE e.target_id=? AND e.org_id=? AND e.project_id=? AND e.environment_id=? AND o.generation=? AND e.outcome IS NOT NULL
 ) ranked WHERE ordinal=1 AND finding<>'' ORDER BY surface,effective_name`), target.ID, chain.Org, chain.Project, target.EnvironmentID, target.Generation)
	if err != nil {
		return nil, err
	}
	defer closeAdapterRows(rows)
	out := []AdapterFinding{}
	for rows.Next() {
		var finding AdapterFinding
		if err := rows.Scan(&finding.Surface, &finding.EffectiveName, &finding.Finding); err != nil {
			return nil, err
		}
		out = append(out, finding)
	}
	return out, rows.Err()
}
