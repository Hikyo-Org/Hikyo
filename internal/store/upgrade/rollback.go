package upgrade

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/gofrs/flock"
)

// Rollback is confined to WithRollbackLock's callback. It restores the snapshot
// the fence took, so it is the only holder of the migration lock that may replace
// the live database. SQLite holds just the lock file with no connection open,
// because WithLock would open (and could create) the very file being replaced.
type Rollback struct {
	cfg     Config
	session *Session
	active  bool
	// needed is the basis of the last Decide that required the snapshot; Admit accepts only it.
	needed *RollbackBasis
}

// WithRollbackLock takes the same exclusion as WithLock, without opening a SQLite connection.
func WithRollbackLock(ctx context.Context, cfg Config, fn func(*Rollback) error) (err error) {
	if err := cfg.Engine.Validate(); err != nil {
		return err
	}
	if fn == nil {
		return errors.New("upgrade: missing locked operation")
	}
	if cfg.Engine == releaseidentity.Postgres {
		return WithLock(ctx, cfg, func(s *Session) error {
			r := &Rollback{cfg: cfg, session: s, active: true}
			defer func() { r.active = false }()
			return fn(r)
		})
	}
	cfg.Path, err = canonicalSQLite(cfg.Path)
	if err != nil {
		return err
	}
	fileLock := flock.New(cfg.Path + ".lock")
	locked, lockErr := fileLock.TryLockContext(ctx, 25*time.Millisecond)
	if lockErr != nil {
		return lockErr
	}
	if !locked {
		return errors.New("upgrade: migration lock held")
	}
	defer func() { err = errors.Join(err, fileLock.Unlock()) }()
	r := &Rollback{cfg: cfg, active: true}
	defer func() { r.active = false }()
	return fn(r)
}

// Identity names the database the rollback is about to replace, so a crash that
// leaves it absent can be told apart from a different database being pointed at.
func (r *Rollback) Identity(ctx context.Context) (string, error) {
	if !r.active {
		return "", errors.New("upgrade: inactive rollback lock")
	}
	if r.cfg.Engine == releaseidentity.SQLite {
		return r.cfg.Path, nil
	}
	// The database name alone repeats on any cluster: the system identifier tells clusters apart.
	var name string
	err := r.session.conn.QueryRowContext(ctx, "SELECT current_database() || '/' || (SELECT system_identifier FROM pg_control_system())").Scan(&name)
	return name, err
}

// RollbackBasis is what the authenticated journal and receipt claim. The live row
// must agree with it before anything is replaced.
type RollbackBasis struct {
	Instance            string
	Source              Source
	Target              releaseidentity.Identity
	Route               releaseidentity.Digest
	BackupID            string
	SourceGeneration    int64
	RestoreEpoch        int64
	RecoveryIncarnation string
	// Rolling is set only when the journal already says "rolling-back"; Database is
	// the identity it recorded then.
	Rolling  bool
	Database string
	// HostFenced is the native deployment: no backup fence exists, so the archive's row is
	// the source `healthy` without maintenance and the host fence is what held the window.
	HostFenced bool
}

// Decide reads the live control row under the lock. It returns whether the row
// needs the snapshot back. A read error is permission only when it is "absent",
// the journal is already rolling back and the database is the one recorded.
func (r *Rollback) Decide(ctx context.Context, b RollbackBasis) (needed bool, err error) {
	if !r.active {
		return false, errors.New("upgrade: inactive rollback lock")
	}
	r.needed = nil
	defer func() {
		if needed && err == nil {
			r.needed = &b
		}
	}()
	state, err := InspectControl(ctx, r.cfg)
	if errors.Is(err, ErrAbsent) {
		identity, identityErr := r.Identity(ctx)
		if identityErr != nil || !b.Rolling || b.Database == "" || identity != b.Database {
			return false, errors.Join(ErrConflict, identityErr)
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	incarnation, err := state.RecoveryIncarnation.MarshalText()
	if err != nil || state.Pending == nil || state.InstanceID != b.Instance || state.RestoreEpoch != b.RestoreEpoch || string(incarnation) != b.RecoveryIncarnation {
		return false, ErrConflict
	}
	pending := state.Pending
	untouched := !pending.Invalidated && state.Generation == b.SourceGeneration && state.Applied == b.Source && ((pending.Phase == Healthy && !state.Maintenance) ||
		(pending.Phase == BackupPreparing && !b.HostFenced && pending.Preparation != nil && pending.Preparation.Target == b.Target && pending.Preparation.RouteDigest == b.Route))
	if untouched {
		return false, nil
	}
	if state.Maintenance && pending.BackupID == b.BackupID && b.BackupID != "" && state.Generation > b.SourceGeneration {
		return true, nil
	}
	return false, ErrConflict
}

// RollbackAdmission is the only authority to replace the live database with the
// fenced snapshot. It exists only inside the lock and carries its own target.
type RollbackAdmission struct {
	rollback *Rollback
	basis    RollbackBasis
}

// Admit mints the capability only after Decide reported, for this exact basis, that the row needs the snapshot.
func (r *Rollback) Admit(b RollbackBasis) (RollbackAdmission, error) {
	if !r.active || r.needed == nil || *r.needed != b || b.BackupID == "" || b.Instance == "" || b.SourceGeneration < 1 {
		return RollbackAdmission{}, ErrConflict
	}
	return RollbackAdmission{rollback: r, basis: b}, nil
}

// Path is the one SQLite file this admission may replace.
func (a RollbackAdmission) Path() (string, error) {
	if a.rollback == nil || !a.rollback.active || a.rollback.cfg.Engine != releaseidentity.SQLite {
		return "", ErrConflict
	}
	return a.rollback.cfg.Path, nil
}

// CheckRestored is rule 3, run inside the staged database before it is published:
// the restored row is this journal's source at the receipt's generation, in the
// instance the snapshot belongs to: fenced for this target and route on container
// owners, healthy without maintenance on native.
func (a RollbackAdmission) CheckRestored(state State) error {
	b := a.basis
	incarnation, err := state.RecoveryIncarnation.MarshalText()
	if a.rollback == nil || !a.rollback.active || err != nil || state.Pending == nil || state.InstanceID != b.Instance || state.RestoreEpoch != b.RestoreEpoch || string(incarnation) != b.RecoveryIncarnation ||
		state.Generation != b.SourceGeneration || state.Applied != b.Source || state.Pending.Invalidated {
		return ErrConflict
	}
	if b.HostFenced {
		// The archive was taken from the healthy source under the host fence.
		if state.Maintenance || state.Pending.Phase != Healthy {
			return ErrConflict
		}
		return nil
	}
	if !state.Maintenance || state.Pending.Phase != BackupPreparing || state.Pending.Preparation == nil || state.Pending.Preparation.Target != b.Target || state.Pending.Preparation.RouteDigest != b.Route {
		return ErrConflict
	}
	return nil
}

// RemoveLive removes the live SQLite file, then its -wal and -shm, in that order,
// so a stale WAL can never meet the restored file. Missing files are fine.
func (a RollbackAdmission) RemoveLive(remove func(string) error) error {
	path, err := a.Path()
	if err != nil {
		return err
	}
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if err := remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// publicObjects lists what a rollback removes from "public" and what the preflight checks the
// role owns, from one query, so the two cannot disagree. Objects an extension owns, and the
// indexes, row types and array types that go with their tables, are not listed: they go with
// their owner. Operator families and classes need a superuser to exist and are not listed; one
// that does exist fails the emptiness check after the reset.
const publicObjects = `
 SELECT CASE c.relkind WHEN 'v' THEN 'VIEW' WHEN 'm' THEN 'MATERIALIZED VIEW' WHEN 'S' THEN 'SEQUENCE' WHEN 'f' THEN 'FOREIGN TABLE' ELSE 'TABLE' END AS kind, c.oid::regclass::text AS name, c.relowner AS owner
 FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r','p','v','m','S','f') AND NOT EXISTS (SELECT 1 FROM pg_inherits i WHERE i.inhrelid = c.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.classid = 'pg_class'::regclass AND d.deptype IN ('e','a','i'))
 UNION ALL SELECT CASE p.prokind WHEN 'p' THEN 'PROCEDURE' WHEN 'a' THEN 'AGGREGATE' ELSE 'FUNCTION' END, p.oid::regprocedure::text, p.proowner FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace
 AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = p.oid AND d.classid = 'pg_proc'::regclass AND d.deptype = 'e')
 UNION ALL SELECT 'TYPE', t.oid::regtype::text, t.typowner FROM pg_type t WHERE t.typnamespace = 'public'::regnamespace
 AND (t.typtype IN ('e','d','r','m') OR (t.typtype = 'c' AND EXISTS (SELECT 1 FROM pg_class k WHERE k.oid = t.typrelid AND k.relkind = 'c')))
 AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = t.oid AND d.classid = 'pg_type'::regclass AND d.deptype = 'e')
 UNION ALL SELECT 'OPERATOR', o.oid::regoperator::text, o.oprowner FROM pg_operator o WHERE o.oprnamespace = 'public'::regnamespace
 AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = o.oid AND d.classid = 'pg_operator'::regclass AND d.deptype = 'e')
 UNION ALL SELECT 'COLLATION', format('public.%I', c.collname), c.collowner FROM pg_collation c WHERE c.collnamespace = 'public'::regnamespace
 AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.classid = 'pg_collation'::regclass AND d.deptype = 'e')
 UNION ALL SELECT 'CONVERSION', format('public.%I', c.conname), c.conowner FROM pg_conversion c WHERE c.connamespace = 'public'::regnamespace
 UNION ALL SELECT 'TEXT SEARCH CONFIGURATION', format('public.%I', c.cfgname), c.cfgowner FROM pg_ts_config c WHERE c.cfgnamespace = 'public'::regnamespace
 UNION ALL SELECT 'TEXT SEARCH DICTIONARY', format('public.%I', c.dictname), c.dictowner FROM pg_ts_dict c WHERE c.dictnamespace = 'public'::regnamespace`

// publicLeftovers counts what still hangs off "public" after a reset, of any kind: an object
// that is not an index, row type, array type or extension member and has no owner above it.
const publicLeftovers = `SELECT count(*) FROM pg_depend d WHERE d.refclassid = 'pg_namespace'::regclass AND d.refobjid = 'public'::regnamespace AND d.deptype = 'n'
 AND NOT EXISTS (SELECT 1 FROM pg_depend x WHERE x.classid = d.classid AND x.objid = d.objid AND x.objsubid = d.objsubid AND x.deptype IN ('i','a','e'))`

// ResetPostgres empties the public schema so the source migrations and the
// archive import can run again. It refuses while any other session is connected
// to the database, bounds every lock wait, and leaves the schema and its grants.
// It is not part of one transaction with the import: "rolling-back" repeats it.
func (a RollbackAdmission) ResetPostgres(ctx context.Context) error {
	if a.rollback == nil || !a.rollback.active || a.rollback.cfg.Engine != releaseidentity.Postgres {
		return ErrConflict
	}
	conn := a.rollback.session.conn
	// A role that cannot see another role's sessions gets NULL for backend_type, so a NULL counts
	// as a client. A backend that just closed (our own control read) lingers briefly: wait for it.
	for attempt := 0; ; attempt++ {
		var others int
		if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid() AND coalesce(backend_type, 'client backend') = 'client backend'`).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			break
		}
		if attempt == 40 {
			return errors.New("upgrade: another session is connected to the database")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Local to this transaction: the session goes on to import the archive afterwards.
	if _, err := tx.ExecContext(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		return err
	}
	// Extension members go with their extension, which the source schema never has.
	rows, err := tx.QueryContext(ctx, `SELECT format('DROP %s IF EXISTS %s CASCADE', kind, name) FROM (`+publicObjects+`) objects`)
	if err != nil {
		return err
	}
	var statements []string
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			rows.Close()
			return err
		}
		statements = append(statements, statement)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	var left int
	if err := tx.QueryRowContext(ctx, publicLeftovers).Scan(&left); err != nil {
		return err
	}
	if left != 0 {
		return fmt.Errorf("upgrade: %d objects remain in the public schema after the reset", left)
	}
	return tx.Commit()
}

// ApplyRestoreSchema and ValidateRestoreDestination are the existing restore
// steps, reachable here only through the rollback admission.
func (a RollbackAdmission) ApplyRestoreSchema(ctx context.Context, plan upgradecompat.Plan, source embed.FS, directory string) error {
	if a.rollback == nil || !a.rollback.active || a.rollback.session == nil {
		return ErrConflict
	}
	return a.rollback.session.ApplyRestoreSchema(ctx, plan, source, directory)
}

func (a RollbackAdmission) ValidateRestoreDestination(ctx context.Context, archive *backupreceipt.AuthenticatedArchive, plan upgradecompat.Plan) (RestoreDestinationAdmission, error) {
	if a.rollback == nil || !a.rollback.active || a.rollback.session == nil {
		return RestoreDestinationAdmission{}, ErrConflict
	}
	return a.rollback.session.ValidateRestoreDestination(ctx, archive, plan)
}

// PreflightPostgresRollback runs before the backup fence, so a refusal is harmless.
// A rollback empties and rebuilds the "public" schema as the connected role, which
// needs that to be the current schema and every object in it to be owned by that role.
func PreflightPostgresRollback(ctx context.Context, cfg Config) error {
	if cfg.Engine != releaseidentity.Postgres {
		return nil
	}
	return WithLock(ctx, cfg, func(s *Session) error {
		var schema string
		var foreign int
		// Identity needs the cluster system identifier; a server that withholds it from this role refuses here, before the fence.
		var identifier int64
		if err := s.conn.QueryRowContext(ctx, "SELECT system_identifier FROM pg_control_system()").Scan(&identifier); err != nil {
			return fmt.Errorf("upgrade: automatic rollback needs the database role to read pg_control_system(): %w", err)
		}
		if err := s.conn.QueryRowContext(ctx, `SELECT current_schema(), (SELECT count(*) FROM (`+publicObjects+`) o WHERE o.owner <> (SELECT oid FROM pg_roles WHERE rolname = current_user))`).Scan(&schema, &foreign); err != nil {
			return err
		}
		if schema != "public" || foreign != 0 {
			return errors.New("upgrade: automatic rollback needs the public schema and every object in it owned by the database role")
		}
		return nil
	})
}
