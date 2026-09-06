package store

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPostgresSourceProofBindsAliasAndExpires(t *testing.T) {
	db := poolReplacementFixture(t, 4, "")
	original := db.PG().Config().ConnString()
	alias, err := url.Parse(original)
	if err != nil {
		t.Fatal(err)
	}
	query := alias.Query()
	query.Set("application_name", "hikyo-proof-alias")
	alias.RawQuery = query.Encode()
	candidate := alias.String()
	proof, err := db.VerifyPostgresSource(t.Context(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.Digest()) != 64 {
		t.Fatal("missing opaque evidence digest")
	}
	if err := proof.ValidateFor(t.Context(), db, candidate, time.Now()); err != nil {
		t.Fatal(err)
	}
	if proof.ValidateFor(t.Context(), db, original, time.Now()) == nil {
		t.Fatal("proof accepted another descriptor")
	}
	if proof.ValidateFor(t.Context(), db, candidate, proof.issued.Add(-time.Nanosecond)) == nil {
		t.Fatal("proof accepted a time before issue")
	}
	if proof.ValidateFor(t.Context(), db, candidate, proof.expires) == nil {
		t.Fatal("expired proof accepted")
	}
	if proof.ValidateFor(t.Context(), &DB{}, candidate, time.Now()) == nil {
		t.Fatal("proof accepted another DB owner")
	}
	if (&VerifiedPostgresSource{}).ValidateFor(t.Context(), db, candidate, time.Now()) == nil {
		t.Fatal("zero proof accepted")
	}
	second, err := db.VerifyPostgresSource(t.Context(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Digest() == second.Digest() {
		t.Fatal("fresh proof reused its challenge evidence")
	}
	assertNoPostgresProofLocks(t, db)
	before := db.PG()
	replacement := preparePoolReplacement(t, db, 3)
	if err := replacement.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if db.PG() == before {
		t.Fatal("test did not replace pool")
	}
	if err := proof.ValidateFor(t.Context(), db, candidate, time.Now()); err != nil {
		t.Fatalf("stable DB pool change invalidated same-source proof: %v", err)
	}
}

func TestPostgresSourceProofSanitizesFailuresAndTimeout(t *testing.T) {
	db := poolReplacementFixture(t, 4, "")
	dsn := db.PG().Config().ConnString()
	candidate, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	candidate.User = url.UserPassword("nonexistent_proof_principal", "private-credential-sentinel")
	for _, raw := range []string{candidate.String(), "postgres://private-credential-sentinel@%not-a-host"} {
		proof, err := db.VerifyPostgresSource(t.Context(), raw)
		if err == nil || proof != nil {
			t.Fatal("invalid credentials/source accepted")
		}
		if strings.Contains(err.Error(), "private-credential-sentinel") || strings.Contains(err.Error(), candidate.Host) || strings.Contains(err.Error(), "nonexistent_proof_principal") {
			t.Fatal("source refusal disclosed locator or credential")
		}
	}
	blocker, err := db.PG().Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(t.Context())
	if _, err := blocker.Exec(t.Context(), "SELECT singleton FROM upgrade_control FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if proof, err := db.VerifyPostgresSource(ctx, dsn); err == nil || proof != nil {
		t.Fatal("timed out source proof succeeded")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("proof ignored caller deadline")
	}
	if err := blocker.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertNoPostgresProofLocks(t, db)
	if err := db.CheckAdmission(t.Context()); err != nil {
		t.Fatalf("failed proof changed source admission: %v", err)
	}
}

func TestPostgresSourceProofRefusesRecoveryChange(t *testing.T) {
	db := poolReplacementFixture(t, 4, "")
	dsn := db.PG().Config().ConnString()
	proof, err := db.VerifyPostgresSource(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Recovery replaces the admitted incarnation. A static comparison against
	// DB.RecoveryIdentity alone would keep accepting the stale proof here.
	if _, err := db.PG().Exec(t.Context(), "UPDATE upgrade_control SET incarnation=repeat('0',64)"); err != nil {
		t.Fatal(err)
	}
	if proof.ValidateFor(t.Context(), db, dsn, time.Now()) == nil {
		t.Fatal("pre-restore proof remained valid")
	}
}

func TestPostgresSourceProofRejectsClonedInstallation(t *testing.T) {
	cfg := ownedAdmissionConfig(t, EnginePostgres)
	db, err := admittedStoreFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	admission := db.admission
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	sourceName := strings.TrimPrefix(parsed.Path, "/")
	adminURL := *parsed
	adminURL.Path = "/postgres"
	admin, err := pgx.Connect(t.Context(), adminURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	cloneName := fmt.Sprintf("hikyo_source_clone_%d", time.Now().UnixNano())
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{cloneName}.Sanitize()+" TEMPLATE "+pgx.Identifier{sourceName}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{cloneName}.Sanitize()+" WITH (FORCE)")
	})
	db, err = Open(t.Context(), cfg, admission)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cloneURL := *parsed
	cloneURL.Path = "/" + cloneName
	clone, err := pgx.Connect(t.Context(), cloneURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close(t.Context())
	// A real byte-equivalent TEMPLATE clone passes owner/schema/incarnation
	// admission. Demonstrate why those checks alone cannot authorize a rollout.
	checked, err := db.beginPostgresOn(t.Context(), clone, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("fixture clone did not preserve exact admitted identity: %v", err)
	}
	defer checked.Rollback(t.Context())
	source, err := db.BeginPostgres(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Rollback(t.Context())
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	for index := range 2 {
		first := int32(binary.BigEndian.Uint32(nonce[index*8 : index*8+4]))
		second := int32(binary.BigEndian.Uint32(nonce[index*8+4 : index*8+8]))
		var acquired bool
		if err := source.QueryRow(t.Context(), "SELECT pg_catalog.pg_try_advisory_xact_lock($1,$2)", first, second).Scan(&acquired); err != nil || !acquired {
			t.Fatalf("source challenge: %v", err)
		}
		if err := checked.QueryRow(t.Context(), "SELECT pg_catalog.pg_try_advisory_xact_lock($1,$2)", first, second).Scan(&acquired); err != nil || !acquired {
			t.Fatalf("clone should have independent locks: %v", err)
		}
	}
	if err := checked.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := source.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if proof, err := db.VerifyPostgresSource(t.Context(), cloneURL.String()); err == nil || proof != nil {
		t.Fatal("cloned identity authorized as current live database")
	}
	assertNoPostgresProofLocks(t, db)
	var locks int
	if err := clone.QueryRow(t.Context(), "SELECT count(*) FROM pg_catalog.pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=pg_catalog.current_database())").Scan(&locks); err != nil || locks != 0 {
		t.Fatalf("failed clone challenge leaked locks: count=%d err=%v", locks, err)
	}
}

func assertNoPostgresProofLocks(t *testing.T, db *DB) {
	t.Helper()
	var locks int
	if err := db.PG().QueryRow(t.Context(), "SELECT count(*) FROM pg_catalog.pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=pg_catalog.current_database())").Scan(&locks); err != nil {
		t.Fatal(err)
	}
	if locks != 0 {
		t.Fatalf("source proof left %d advisory locks", locks)
	}
}

func TestPostgresSourceProofRequiresRuntimePrivileges(t *testing.T) {
	db := poolReplacementFixture(t, 4, "")
	ctx := t.Context()
	role := fmt.Sprintf("hikyo_source_role_%d", time.Now().UnixNano())
	quotedRole := pgx.Identifier{role}.Sanitize()
	var passwordBytes [32]byte
	if _, err := rand.Read(passwordBytes[:]); err != nil {
		t.Fatal(err)
	}
	password := fmt.Sprintf("%x", passwordBytes)
	exec := func(sql string) {
		t.Helper()
		if _, err := db.PG().Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	// A private random hex credential works with both local trust fixtures and
	// CI's SCRAM authentication without widening this role's database grants.
	exec("CREATE ROLE " + quotedRole + " LOGIN PASSWORD '" + password + "'")
	t.Cleanup(func() {
		_, _ = db.PG().Exec(context.Background(), "DROP OWNED BY "+quotedRole)
		_, _ = db.PG().Exec(context.Background(), "DROP ROLE "+quotedRole)
	})
	exec("REVOKE ALL ON SCHEMA public FROM PUBLIC")
	exec("GRANT USAGE ON SCHEMA public TO " + quotedRole)
	exec("GRANT SELECT ON ALL TABLES IN SCHEMA public TO " + quotedRole)
	exec("GRANT UPDATE ON public.upgrade_control TO " + quotedRole)
	exec("GRANT INSERT,UPDATE,DELETE ON public.self_config_nodes TO " + quotedRole)
	candidate, err := url.Parse(db.PG().Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	candidate.User = url.UserPassword(role, password)
	raw := candidate.String()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	// This principal passes the existing admission gate and can acknowledge a
	// node, but still cannot perform ordinary domain or audit writes.
	admitted, err := db.beginPostgresOn(ctx, conn, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("restricted principal could not pass admission: %v", err)
	}
	if err := admitted.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "UPDATE public.self_config_nodes SET node_id=node_id WHERE false"); err != nil {
		t.Fatal(err)
	}
	if proof, err := db.VerifyPostgresSource(ctx, raw); err == nil || proof != nil {
		t.Fatal("node-only writer authorized as runtime source")
	}
	// Ordinary CRUD is sufficient. No ownership, DDL, TRUNCATE, or sequence
	// UPDATE is granted, and bookkeeping stays read-only except admission lock.
	exec("GRANT INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO " + quotedRole)
	exec("REVOKE INSERT,UPDATE,DELETE ON public.goose_db_version,public.upgrade_pending,public.upgrade_nonces FROM " + quotedRole)
	exec("REVOKE INSERT,DELETE ON public.upgrade_control FROM " + quotedRole)
	exec("GRANT SELECT,USAGE ON ALL SEQUENCES IN SCHEMA public TO " + quotedRole)
	exec("REVOKE USAGE ON SEQUENCE public.goose_db_version_id_seq FROM " + quotedRole)
	var canCreate bool
	if err := conn.QueryRow(ctx, "SELECT pg_catalog.has_schema_privilege('public','CREATE')").Scan(&canCreate); err != nil || canCreate {
		t.Fatalf("fixture unexpectedly grants DDL: create=%v err=%v", canCreate, err)
	}
	proof, err := db.VerifyPostgresSource(ctx, raw)
	if err != nil {
		t.Fatalf("non-owner runtime grants rejected: %v", err)
	}
	for _, tc := range []struct{ object, privilege string }{
		{"TABLE public.orgs", "SELECT"},
		{"TABLE public.orgs", "INSERT"},
		{"TABLE public.orgs", "UPDATE"},
		{"TABLE public.orgs", "DELETE"},
		{"TABLE public.audit_tenant_events", "INSERT"},
		{"TABLE public.audit_instance_events", "UPDATE"},
		{"SEQUENCE public.audit_tenant_commit_seq", "USAGE"},
		{"SEQUENCE public.audit_tenant_commit_seq", "SELECT"},
		{"SCHEMA public", "USAGE"},
	} {
		t.Run(tc.object+"/"+tc.privilege, func(t *testing.T) {
			exec("REVOKE " + tc.privilege + " ON " + tc.object + " FROM " + quotedRole)
			defer exec("GRANT " + tc.privilege + " ON " + tc.object + " TO " + quotedRole)
			if got, err := db.VerifyPostgresSource(ctx, raw); err == nil || got != nil {
				t.Fatal("missing independent runtime privilege accepted")
			}
			if err := proof.ValidateFor(ctx, db, raw, time.Now()); err == nil {
				t.Fatal("earlier proof survived runtime privilege revocation")
			}
		})
	}
	assertNoPostgresProofLocks(t, db)
}
