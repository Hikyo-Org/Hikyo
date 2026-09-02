package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Reencrypt walks a scope's retained ciphertext onto the active DEK version and
// (once every table in the scope is on it) retires the superseded versions —
// the completion half of a DEK rotation (encryption-model ADR § Rotation, #75;
// ops-spec §9 bounds, #187).
//
// The walk is chunked (default 100 rows) with a fixed inter-chunk pause (default
// 100 ms) so a large scope does not monopolize a connection, and it is resumable
// by construction: a row already on the active version is skipped, so a re-run
// (or a crash mid-walk) continues from where it left off. Each row is moved
// under a compare-and-swap on its old ciphertext — a concurrent write that
// replaced the row leaves it on a fresh version and the CAS matches nothing, so
// reencrypt never resurrects a superseded value.
type Reencrypt struct {
	DB      *store.DB
	Keyring *crypto.Keyring
	// Budget applies the §179 fail-closed default (60/min·principal, 8/org) to
	// the reencrypt trigger — a row-proportional crypto walk with no named
	// category. Nil disables it.
	Budget *Budget
	Now    func() time.Time
	// ChunkSize / ChunkPause override the #187 defaults; zero uses them. Tests
	// set a tiny chunk and no pause.
	ChunkSize  int
	ChunkPause time.Duration
	// BeforeRetire, when set, runs after the walk and before the retire's dryness
	// gate. Test-only (nil in production): it lets a test interleave a rotate-dek
	// or seed a straggler into the window the gate must refuse.
	BeforeRetire func(context.Context) error
	// AfterChunk, when set, runs inside each chunk transaction after its rows are
	// processed and before commit, receiving the table being walked. Test-only
	// (nil in production): returning a retryable error (store.ErrRetrySerialization)
	// exercises the tx.Write replay path, proving a chunk's cursor and moved-count
	// outputs are attempt-local and never published on a rolled-back attempt.
	AfterChunk func(ctx context.Context, table string) error
}

// ReencryptChunkSize and ReencryptChunkPause are the ops-spec §9 (§167) bounds
// on the background rewrap walk: at most 100 rows per chunk transaction, a fixed
// 100 ms pause between chunks. They are the enforced defaults (a caller may set
// a smaller chunk or no pause for tests, never a larger chunk); the conformance
// bound-registry pins these values against drift.
const (
	ReencryptChunkSize  = 100
	ReencryptChunkPause = 100 * time.Millisecond
)

func (s *Reencrypt) chunkSize() int {
	if s.ChunkSize > 0 {
		return s.ChunkSize
	}
	return ReencryptChunkSize
}

func (s *Reencrypt) chunkPause() time.Duration {
	if s.ChunkPause != 0 {
		return s.ChunkPause
	}
	return ReencryptChunkPause
}

func (s *Reencrypt) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

// ReencryptResult reports one reencrypt run: the scope and how many rows moved.
type ReencryptResult struct {
	Scope     string
	OrgID     string
	ProjectID string
	RowsMoved int
}

// RetiringScope names a DEK scope that still carries a superseded (retiring)
// version — one reencrypt has not yet cleared. OpenableVersions > 1 is the
// signal: a fully-reencrypted scope has exactly one openable version (active).
type RetiringScope struct {
	Purpose          string
	OrgID            string
	ProjectID        string
	OpenableVersions int
}

// SweepRetiring reports every scope whose DEK still has a retiring version. It is
// read-only and takes no operator proof: it runs the same boot-surface
// AllOpenableTier3 read the keyring loads from (SiteBoot authority, no writes)
// and groups the openable versions by scope. The scheduler logs a warning per
// result so an operator runs `reencrypt` — reencrypt stays an operator act, and
// the sweep grants the scheduler no write access to any ciphertext table
// (#75/#187, scheduler option A).
func (s *Reencrypt) SweepRetiring(ctx context.Context) ([]RetiringScope, error) {
	keys, err := (&keyring.Store{DB: s.DB}).AllOpenableTier3(ctx)
	if err != nil {
		return nil, err
	}
	type scopeKey struct{ purpose, org, project string }
	counts := map[scopeKey]int{}
	for _, k := range keys {
		counts[scopeKey{string(k.Purpose), k.OrgID, k.ProjectID}]++
	}
	var out []RetiringScope
	for sk, n := range counts {
		if n > 1 {
			out = append(out, RetiringScope{Purpose: sk.purpose, OrgID: sk.org, ProjectID: sk.project, OpenableVersions: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Purpose != out[j].Purpose {
			return out[i].Purpose < out[j].Purpose
		}
		if out[i].OrgID != out[j].OrgID {
			return out[i].OrgID < out[j].OrgID
		}
		return out[i].ProjectID < out[j].ProjectID
	})
	return out, nil
}

// ReencryptProject walks a project's value ciphertext onto the active DEK
// version. (Adapters, pending drafts and snapshot payloads join this walk with
// their store methods; the DEK-version retire lands once every project table is
// covered, since a version is retired only when zero ciphertexts reference it.)
func (s *Reencrypt) ReencryptProject(ctx context.Context, actor Actor, orgID, projectID string) (ReencryptResult, error) {
	if s.Keyring == nil {
		return ReencryptResult{}, errors.New("service: reencrypt requires a keyring")
	}
	if orgID == "" || projectID == "" {
		return ReencryptResult{}, fmt.Errorf("%w: reencrypt --project requires org and project ids", domain.ErrInvalid)
	}
	// One sealer for the whole walk: it holds every still-openable version, so it
	// opens rows under their retiring version and seals under the active one.
	sealer, err := s.Keyring.ForProject(ctx, orgID, projectID)
	if err != nil {
		return ReencryptResult{}, err
	}
	active := sealer.ActiveVersion()
	scope := domain.Scope{Org: domain.OrgID(orgID), Project: domain.ProjectID(projectID)}

	// §179 fail-closed default: a crypto walk proportional to every stored row,
	// with no named category. Acquired once at entry and held for the whole
	// multi-transaction walk.
	release, err := chargeDefaultAtEntry(ctx, s.DB, s.Budget, actor, authz.OpReencryptProject, authz.OpReencryptProject, scope, s.now)
	if err != nil {
		return ReencryptResult{}, err
	}
	defer release()

	// Adapter credential ciphertext (live row + any pending route-move credential)
	// binds the ADAPTER's id (row.owner) in its AAD, not the row id.
	adapterAAD := func(row projectFieldRow) crypto.AAD {
		return adapter.CredentialAAD(orgID, projectID, row.owner)
	}
	// The five project ciphertext tables, defined once and shared by the walk and
	// the retire's dryness gate — a DEK version is retired only when zero
	// ciphertexts across ALL of them reference it, so both must cover the same set.
	tables := []projectTable{
		{"value",
			func(ctx context.Context, r store.Repos, p authz.Proof, cursor string) ([]projectFieldRow, error) {
				rows, err := r.Values().ListForReencrypt(ctx, p, cursor, s.chunkSize())
				return valueRows(rows), err
			},
			func(row projectFieldRow) crypto.AAD {
				return crypto.ValueAAD{OrgID: orgID, ProjectID: projectID, EnvID: row.env, KeyID: row.key, RowID: row.id, FieldTag: valueFieldTag}
			},
			func(ctx context.Context, r store.Repos, p authz.Proof, id string, newCt, oldCt []byte) (bool, error) {
				return r.Values().Reencrypt(ctx, p, id, newCt, oldCt)
			}},
		{"snapshot",
			func(ctx context.Context, r store.Repos, p authz.Proof, cursor string) ([]projectFieldRow, error) {
				rows, err := r.Snapshots().ListForReencrypt(ctx, p, cursor, s.chunkSize())
				return fieldRows(rows), err
			},
			func(row projectFieldRow) crypto.AAD {
				return snapshotAAD(orgID, projectID, row.env, row.key, row.snapshot, row.id)
			},
			func(ctx context.Context, r store.Repos, p authz.Proof, id string, newCt, oldCt []byte) (bool, error) {
				return r.Snapshots().Reencrypt(ctx, p, id, newCt, oldCt)
			}},
		{"pending",
			func(ctx context.Context, r store.Repos, p authz.Proof, cursor string) ([]projectFieldRow, error) {
				rows, err := r.Pending().ListForReencrypt(ctx, p, cursor, s.chunkSize())
				return fieldRows(rows), err
			},
			func(row projectFieldRow) crypto.AAD {
				return pendingAAD(orgID, projectID, row.env, row.key, row.id)
			},
			func(ctx context.Context, r store.Repos, p authz.Proof, id string, newCt, oldCt []byte) (bool, error) {
				return r.Pending().Reencrypt(ctx, p, id, newCt, oldCt)
			}},
		{"adapter",
			func(ctx context.Context, r store.Repos, p authz.Proof, cursor string) ([]projectFieldRow, error) {
				rows, err := r.Adapters().ListAdaptersForReencrypt(ctx, p, cursor, s.chunkSize())
				return fieldRows(rows), err
			}, adapterAAD,
			func(ctx context.Context, r store.Repos, p authz.Proof, id string, newCt, oldCt []byte) (bool, error) {
				return r.Adapters().ReencryptAdapter(ctx, p, id, newCt, oldCt)
			}},
		{"adapter_route_move",
			func(ctx context.Context, r store.Repos, p authz.Proof, cursor string) ([]projectFieldRow, error) {
				rows, err := r.Adapters().ListRouteMovesForReencrypt(ctx, p, cursor, s.chunkSize())
				return fieldRows(rows), err
			}, adapterAAD,
			func(ctx context.Context, r store.Repos, p authz.Proof, id string, newCt, oldCt []byte) (bool, error) {
				return r.Adapters().ReencryptRouteMove(ctx, p, id, newCt, oldCt)
			}},
		{"dynamic_provider",
			func(ctx context.Context, r store.Repos, p authz.Proof, cursor string) ([]projectFieldRow, error) {
				rows, err := r.Dynamic().ListProvidersForReencrypt(ctx, p, cursor, s.chunkSize())
				return fieldRows(rows), err
			},
			func(row projectFieldRow) crypto.AAD {
				return providerCredentialAAD(orgID, projectID, row.owner)
			},
			func(ctx context.Context, r store.Repos, p authz.Proof, id string, newCt, oldCt []byte) (bool, error) {
				return r.Dynamic().ReencryptProvider(ctx, p, id, newCt, oldCt)
			}},
	}

	moved := 0
	for _, t := range tables {
		if err := s.walkTable(ctx, actor, scope, t.name, &moved, t.list, t.aad, t.cas, sealer, active); err != nil {
			return ReencryptResult{}, err
		}
	}

	if s.BeforeRetire != nil {
		if err := s.BeforeRetire(ctx); err != nil {
			return ReencryptResult{}, err
		}
	}
	// Retire the scope's retiring DEK versions — but only after re-asserting the
	// version we sealed onto is still active AND proving every project table is
	// dry (zero ciphertexts off the active version), both inside the scope fence.
	// A version is retired only when zero ciphertexts reference it, verified by
	// query, never assumed (ADR § Rotation, invariant 7).
	if err := s.retireProject(ctx, actor, scope, moved, active, tables); err != nil {
		return ReencryptResult{}, err
	}
	s.Keyring.EvictProjectDEK(orgID, projectID)
	return ReencryptResult{Scope: "project", OrgID: orgID, ProjectID: projectID, RowsMoved: moved}, nil
}

// projectTable is one project ciphertext table's reencrypt binding — its paged
// lister, its per-row AAD, and its compare-and-swap reseal — shared by the walk
// and the retire dryness gate so the two never diverge on coverage.
type projectTable struct {
	name string
	list func(context.Context, store.Repos, authz.Proof, string) ([]projectFieldRow, error)
	aad  func(projectFieldRow) crypto.AAD
	cas  func(context.Context, store.Repos, authz.Proof, string, []byte, []byte) (bool, error)
}

// instanceTable is one instance credential table's complete reencrypt binding:
// its paged lister, version source, per-row AAD, and CAS reseal. One registry
// feeds both the walk and retire dryness gate so table coverage cannot diverge.
type instanceTable struct {
	table     string
	list      func(store.ReencryptRepo, context.Context, authz.Proof, string, int) ([]store.ReencryptInstanceRow, error)
	versionOf func(store.ReencryptInstanceRow) (uint32, error)
	aad       func(string) crypto.InstanceFieldAAD
	reseal    func(store.ReencryptRepo, context.Context, authz.Proof, store.ReencryptInstanceRow, []byte, uint32) (bool, error)
}

func instanceColumnVersion(row store.ReencryptInstanceRow) (uint32, error) {
	return row.DEKVersion, nil
}

func versionedInstanceReseal(
	reseal func(store.ReencryptRepo, context.Context, authz.Proof, string, []byte, uint32, uint32) (bool, error),
) func(store.ReencryptRepo, context.Context, authz.Proof, store.ReencryptInstanceRow, []byte, uint32) (bool, error) {
	return func(r store.ReencryptRepo, ctx context.Context, p authz.Proof, row store.ReencryptInstanceRow, ciphertext []byte, active uint32) (bool, error) {
		return reseal(r, ctx, p, row.ID, ciphertext, active, row.RowVersion)
	}
}

// ReencryptInstance walks the six instance-DEK credential tables onto the active
// instance DEK version, then retires the superseded versions. Instance-scoped:
// no tenant chain, one InstanceSealer for the whole walk.
func (s *Reencrypt) ReencryptInstance(ctx context.Context, actor Actor) (ReencryptResult, error) {
	if s.Keyring == nil {
		return ReencryptResult{}, errors.New("service: reencrypt requires a keyring")
	}
	// §179 fail-closed default: an instance-wide crypto walk with no named
	// category (empty scope → the org bound keys on the instance bucket). Held
	// for the whole multi-transaction walk.
	release, err := chargeDefaultAtEntry(ctx, s.DB, s.Budget, actor, authz.OpReencryptInstance, authz.OpReencryptInstance, domain.Scope{}, s.now)
	if err != nil {
		return ReencryptResult{}, err
	}
	defer release()
	sealer := s.Keyring.ForInstance()
	active := sealer.Version()
	moved := 0
	tables := []instanceTable{
		{table: "password_credentials",
			list:      store.ReencryptRepo.ListPasswordCredsForReencrypt,
			versionOf: instanceColumnVersion,
			aad: func(id string) crypto.InstanceFieldAAD {
				return crypto.InstanceFieldAAD{OwnerTable: "password_credentials", OwnerRowID: id, FieldTag: "verifier"}
			},
			reseal: versionedInstanceReseal(store.ReencryptRepo.ReencryptPasswordCred)},
		{table: "totp_credentials",
			list:      store.ReencryptRepo.ListTotpCredsForReencrypt,
			versionOf: instanceColumnVersion,
			aad: func(id string) crypto.InstanceFieldAAD {
				return crypto.InstanceFieldAAD{OwnerTable: "totp_credentials", OwnerRowID: id, FieldTag: "seed"}
			},
			reseal: versionedInstanceReseal(store.ReencryptRepo.ReencryptTotpCred)},
		{table: "recovery_codes",
			list:      store.ReencryptRepo.ListRecoveryCodesForReencrypt,
			versionOf: instanceColumnVersion,
			aad: func(id string) crypto.InstanceFieldAAD {
				return crypto.InstanceFieldAAD{OwnerTable: "recovery_codes", OwnerRowID: id, FieldTag: "batch"}
			},
			reseal: versionedInstanceReseal(store.ReencryptRepo.ReencryptRecoveryCodes)},
		{table: "oidc_providers",
			list:      store.ReencryptRepo.ListOidcProvidersForReencrypt,
			versionOf: instanceColumnVersion,
			aad: func(id string) crypto.InstanceFieldAAD {
				return crypto.InstanceFieldAAD{OwnerTable: "oidc_providers", OwnerRowID: id, FieldTag: "client_secret"}
			},
			reseal: versionedInstanceReseal(store.ReencryptRepo.ReencryptOidcProvider)},
		{table: "saml_sp_keys",
			list:      store.ReencryptRepo.ListSamlKeysForReencrypt,
			versionOf: instanceColumnVersion,
			aad: func(id string) crypto.InstanceFieldAAD {
				return crypto.InstanceFieldAAD{OwnerTable: "saml_sp_keys", OwnerRowID: id, FieldTag: "private_key"}
			},
			reseal: versionedInstanceReseal(store.ReencryptRepo.ReencryptSamlKey)},
		{table: "remotes",
			list: store.ReencryptRepo.ListRemotesForReencrypt,
			versionOf: func(row store.ReencryptInstanceRow) (uint32, error) {
				return crypto.RecordKeyVersion(row.Ciphertext)
			},
			aad: func(id string) crypto.InstanceFieldAAD {
				return crypto.InstanceFieldAAD{OwnerTable: "remotes", OwnerRowID: id, FieldTag: "credential"}
			},
			reseal: func(r store.ReencryptRepo, ctx context.Context, p authz.Proof, row store.ReencryptInstanceRow, ciphertext []byte, _ uint32) (bool, error) {
				return r.ReencryptRemote(ctx, p, row.ID, ciphertext, row.Ciphertext)
			}},
	}
	for _, table := range tables {
		if err := s.walkInstance(ctx, actor, table, &moved, sealer, active); err != nil {
			return ReencryptResult{}, err
		}
	}

	if s.BeforeRetire != nil {
		if err := s.BeforeRetire(ctx); err != nil {
			return ReencryptResult{}, err
		}
	}
	if err := s.retireInstance(ctx, actor, moved, active, tables); err != nil {
		return ReencryptResult{}, err
	}
	if err := s.Keyring.ReloadInstanceDEK(ctx); err != nil {
		return ReencryptResult{}, err
	}
	return ReencryptResult{Scope: "instance", RowsMoved: moved}, nil
}

// walkChunked pages one table to completion in chunk-sized write transactions
// with the inter-chunk pause. step processes one chunk starting at cursor and
// returns the next cursor, how many rows it saw, and how many it moved.
//
// tx.Write replays step on a serialization retry (postgres 40001/40P01, sqlite
// BUSY/LOCKED), so step MUST be a pure function of its cursor argument — it may
// publish nothing outside its return values. walkChunked advances the outer
// cursor and *moved only after the transaction commits, so a rolled-back attempt
// changes neither: it neither skips the rolled-back page nor double-counts it
// (#187 reopen).
func (s *Reencrypt) walkChunked(
	ctx context.Context, table string, moved *int,
	step func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, cursor string) (next string, seen, delta int, err error),
) error {
	cursor := ""
	for {
		var next string
		var seen, delta int
		err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			var e error
			// A retry re-runs step with the SAME (not-yet-published) cursor and
			// overwrites next/seen/delta, so only the committed attempt's outputs
			// survive to be published below.
			next, seen, delta, e = step(ctx, r, az, cursor)
			if e != nil {
				return e
			}
			if s.AfterChunk != nil {
				return s.AfterChunk(ctx, table)
			}
			return nil
		})
		if err != nil {
			return err
		}
		cursor = next
		*moved += delta
		if seen < s.chunkSize() {
			return nil
		}
		if err := s.pause(ctx); err != nil {
			return err
		}
	}
}

func (s *Reencrypt) walkInstance(ctx context.Context, actor Actor, table instanceTable, moved *int, sealer *crypto.InstanceSealer, active uint32) error {
	return s.walkChunked(ctx, table.table, moved, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, cursor string) (string, int, int, error) {
		caller, err := actor.resolve(ctx, az, s.now())
		if err != nil {
			return "", 0, 0, err
		}
		p, err := az.Authorize(ctx, caller, authz.OpReencryptInstance, domain.Scope{})
		if err != nil {
			return "", 0, 0, err
		}
		// Per-chunk fence: abort if a rotate-dek --instance demoted our target.
		if err := fenceInstanceVersion(ctx, r, p, active); err != nil {
			return "", 0, 0, err
		}
		rows, err := table.list(r.Reencrypt(), ctx, p, cursor, s.chunkSize())
		if err != nil {
			return "", 0, 0, err
		}
		next, delta := cursor, 0
		for _, row := range rows {
			next = row.ID // advance over skipped rows too, else a full-skip chunk re-lists forever
			version, err := table.versionOf(row)
			if err != nil {
				return "", 0, 0, fmt.Errorf("reencrypt: inspect %s %s version: %w", table.table, row.ID, err)
			}
			if version == active {
				continue
			}
			aad := table.aad(row.ID)
			plain, err := sealer.OpenField(aad, row.Ciphertext)
			if err != nil {
				return "", 0, 0, fmt.Errorf("reencrypt: open %s %s: %w", table.table, row.ID, err)
			}
			resealed, err := sealer.SealField(aad, plain)
			crypto.Zero(plain)
			if err != nil {
				return "", 0, 0, fmt.Errorf("reencrypt: reseal %s %s: %w", table.table, row.ID, err)
			}
			did, err := table.reseal(r.Reencrypt(), ctx, p, row, resealed, active)
			if err != nil {
				return "", 0, 0, err
			}
			if did {
				delta++
			}
		}
		return next, len(rows), delta, nil
	})
}

func (s *Reencrypt) pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.chunkPause()):
		return nil
	}
}

func (s *Reencrypt) retireInstance(ctx context.Context, actor Actor, moved int, active uint32, tables []instanceTable) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, s.now())
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpReencryptInstance, domain.Scope{})
		if err != nil {
			return err
		}
		// Re-assert the instance DEK version we sealed onto is still active (FOR
		// SHARE blocks a concurrent rotate-dek --instance demoting it until commit).
		if err := fenceInstanceVersion(ctx, r, p, active); err != nil {
			return err
		}
		// Dryness gate across the same complete instance credential registry the
		// walk used (encryption-model ADR, invariant 7).
		for _, table := range tables {
			straggler, err := s.instanceStraggler(ctx, r, p, table, active)
			if err != nil {
				return err
			}
			if straggler != "" {
				return fmt.Errorf("%w: reencrypt cannot retire: %s references a superseded DEK version",
					domain.ErrConflict, straggler)
			}
		}
		if _, err := r.Keys().RetireRetiringTier3(ctx, p, crypto.PurposeInstance, "", ""); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventReencryptCompleted, caller.Principal,
			audit.Object{Type: "instance", ID: "instance"},
			audit.Payload{"scope": "instance", "rows_moved": int64(moved)})
		if err != nil {
			return err
		}
		return r.Audit().InsertInstance(ctx, p, ev)
	})
}

// instanceStraggler pages one credential table and returns "table:id" for the
// first row whose registry-defined version is off active, or "".
func (s *Reencrypt) instanceStraggler(ctx context.Context, r store.Repos, p authz.Proof, table instanceTable, active uint32) (string, error) {
	cursor := ""
	for {
		rows, err := table.list(r.Reencrypt(), ctx, p, cursor, s.chunkSize())
		if err != nil {
			return "", err
		}
		for _, row := range rows {
			cursor = row.ID
			version, err := table.versionOf(row)
			if err != nil {
				return "", fmt.Errorf("reencrypt: dryness %s %s: %w", table.table, row.ID, err)
			}
			if version != active {
				return table.table + ":" + row.ID, nil
			}
		}
		if len(rows) < s.chunkSize() {
			return "", nil
		}
	}
}

// projectFieldRow is the reencrypt walk's uniform view of a project ciphertext
// row across the value and project_field tables. owner is the AAD owner_row_id
// where it differs from id (an adapter route move keys by its own id but seals
// under the adapter's); empty means "same as id".
type projectFieldRow struct {
	id, env, key, snapshot, owner string
	ciphertext                    []byte
}

func valueRows(rows []store.ReencryptValueRow) []projectFieldRow {
	out := make([]projectFieldRow, len(rows))
	for i, r := range rows {
		out[i] = projectFieldRow{id: r.ID, env: r.EnvironmentID, key: r.KeyID, ciphertext: r.Ciphertext}
	}
	return out
}

func fieldRows(rows []store.ReencryptFieldRow) []projectFieldRow {
	out := make([]projectFieldRow, len(rows))
	for i, r := range rows {
		out[i] = projectFieldRow{id: r.ID, env: r.EnvironmentID, key: r.KeyID, snapshot: r.SnapshotID, owner: r.Owner, ciphertext: r.Ciphertext}
	}
	return out
}

// walkTable pages one project ciphertext table to completion in chunk-sized
// transactions with the inter-chunk pause, re-sealing every row not already on
// the active version under a per-row compare-and-swap.
func (s *Reencrypt) walkTable(
	ctx context.Context, actor Actor, scope domain.Scope, table string, moved *int,
	list func(context.Context, store.Repos, authz.Proof, string) ([]projectFieldRow, error),
	aadOf func(projectFieldRow) crypto.AAD,
	cas func(context.Context, store.Repos, authz.Proof, string, []byte, []byte) (bool, error),
	sealer *crypto.ProjectSealer, active uint32,
) error {
	return s.walkChunked(ctx, table, moved, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, cursor string) (string, int, int, error) {
		caller, err := actor.resolve(ctx, az, s.now())
		if err != nil {
			return "", 0, 0, err
		}
		p, err := az.Authorize(ctx, caller, authz.OpReencryptProject, scope)
		if err != nil {
			return "", 0, 0, err
		}
		// Per-chunk fence: abort the walk if a concurrent rotate-dek demoted the
		// version we are sealing onto. Without this the walk would keep sealing
		// rows onto a now-retiring version, which the retire's dryness gate would
		// then (correctly) refuse — but aborting here kills that at the source.
		if err := fenceProject(ctx, r, p, sealer, scope); err != nil {
			return "", 0, 0, err
		}
		rows, err := list(ctx, r, p, cursor)
		if err != nil {
			return "", 0, 0, err
		}
		next, delta := cursor, 0
		for _, row := range rows {
			next = row.id // advance over skipped rows too, else a full-skip chunk re-lists forever
			if len(row.ciphertext) == 0 {
				continue // an `unset` pending draft holds no ciphertext to move
			}
			ver, err := crypto.RecordKeyVersion(row.ciphertext)
			if err != nil {
				return "", 0, 0, fmt.Errorf("reencrypt: %s %s: %w", table, row.id, err)
			}
			if ver == active {
				continue
			}
			aad := aadOf(row)
			plain, err := sealer.Open(aad, row.ciphertext)
			if err != nil {
				return "", 0, 0, fmt.Errorf("reencrypt: open %s %s: %w", table, row.id, err)
			}
			resealed, err := sealer.Seal(aad, plain)
			crypto.Zero(plain)
			if err != nil {
				return "", 0, 0, fmt.Errorf("reencrypt: reseal %s %s: %w", table, row.id, err)
			}
			did, err := cas(ctx, r, p, row.id, resealed, row.ciphertext)
			if err != nil {
				return "", 0, 0, err
			}
			if did {
				delta++
			}
		}
		return next, len(rows), delta, nil
	})
}

// retireAndRecord retires the scope's now-unreferenced retiring DEK versions and
// writes the completion event, in one transaction on the trail matching the
// scope.
func (s *Reencrypt) retireProject(ctx context.Context, actor Actor, scope domain.Scope, moved int, active uint32, tables []projectTable) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, s.now())
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, authz.OpReencryptProject, scope)
		if err != nil {
			return err
		}
		// Re-assert the version we sealed onto is still active. Its FOR SHARE lock
		// (postgres) also blocks a concurrent rotate-dek from demoting it before this
		// transaction commits, so the dryness scan below cannot be invalidated under
		// us. A demotion since the walk means we sealed onto a now-retiring version:
		// refuse rather than retire it out from under the rows we just moved.
		if err := fenceProjectVersion(ctx, r, p, scope, active); err != nil {
			return err
		}
		// Dryness gate: page every project table and refuse if any ciphertext is
		// still off the active version. Never assume the walk was total.
		for _, t := range tables {
			straggler, err := s.projectTableStraggler(ctx, r, p, t, active)
			if err != nil {
				return err
			}
			if straggler != "" {
				return fmt.Errorf("%w: reencrypt cannot retire: %s references a superseded DEK version",
					domain.ErrConflict, straggler)
			}
		}
		if _, err := r.Keys().RetireRetiringTier3(ctx, p, crypto.PurposeProject, string(scope.Org), string(scope.Project)); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventReencryptCompleted, caller.Principal,
			audit.Object{Type: "project", ID: string(scope.Project)},
			audit.Payload{"scope": "project", "rows_moved": int64(moved)})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
}

// projectTableStraggler pages one table and returns "table:id" for the first row
// whose ciphertext is not on `active`, or "" if the table is dry. It applies the
// same skips the walk does (an empty ciphertext holds no reference).
func (s *Reencrypt) projectTableStraggler(ctx context.Context, r store.Repos, p authz.Proof, t projectTable, active uint32) (string, error) {
	cursor := ""
	for {
		rows, err := t.list(ctx, r, p, cursor)
		if err != nil {
			return "", err
		}
		for _, row := range rows {
			cursor = row.id
			if len(row.ciphertext) == 0 {
				continue
			}
			ver, err := crypto.RecordKeyVersion(row.ciphertext)
			if err != nil {
				return "", fmt.Errorf("reencrypt: dryness %s %s: %w", t.name, row.id, err)
			}
			if ver != active {
				return t.name + ":" + row.id, nil
			}
		}
		if len(rows) < s.chunkSize() {
			return "", nil
		}
	}
}
