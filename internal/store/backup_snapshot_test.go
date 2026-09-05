package store

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExportSQLiteManifestUsesArchivedSchemaDuringMigration(t *testing.T) {
	db, err := admittedStoreFixture(t, Config{Engine: EngineSQLite, Path: filepath.Join(t.TempDir(), "source.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	held, err := db.sqWrite.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	before := db.sqWrite.Stats().WaitCount
	var archive bytes.Buffer
	result := make(chan error, 1)
	work := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	go func() { _, err := Export(ctx, db, &archive, work); result <- err }()
	// The only writer connection is held. Once VACUUM waits for it, any old
	// live preflight has completed, but the actual snapshot has not started.
	for db.sqWrite.Stats().WaitCount == before {
		select {
		case err := <-result:
			t.Fatalf("export returned before snapshot barrier: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := held.ExecContext(ctx, "INSERT INTO goose_db_version(version_id,is_applied) VALUES(45,true)"); err != nil {
		t.Fatal(err)
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	manifest, err := ReadManifest(bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "restored.db")
	if _, err := RestoreSQLite(ctx, bytes.NewReader(archive.Bytes()), restored, nil); err != nil {
		t.Fatal(err)
	}
	copy, err := sql.Open("sqlite", "file:"+restored+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	var version int64
	if err := copy.QueryRowContext(ctx, "SELECT max(version_id) FROM goose_db_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 45 || manifest.SchemaVersion != version {
		t.Fatalf("manifest schema %d, archived schema %d; want both45", manifest.SchemaVersion, version)
	}
}

type schemaChangeTracer struct {
	once   sync.Once
	change func()
}

func (s *schemaChangeTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, snapshotQueryKey{}, data.SQL == "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version")
}

type snapshotQueryKey struct{}

func (s *schemaChangeTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if match, _ := ctx.Value(snapshotQueryKey{}).(bool); match {
		s.once.Do(s.change)
	}
}

func TestExportPostgresManifestUsesCopySnapshotDuringMigration(t *testing.T) {
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires HIKYO_TEST_POSTGRES_DSN")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	name := fmt.Sprintf("hikyo_snapshot_%d", time.Now().UnixNano())
	ident := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+ident+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	}()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	writer, err := pgx.ConnectConfig(ctx, cfg.ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(context.Background())
	targetURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	targetURL.Path = "/" + name
	targetConfig := Config{Engine: EnginePostgres, DSN: targetURL.String()}
	admitted, err := admittedStoreFixture(t, targetConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer admitted.Close()
	var changeErr error
	cfg.ConnConfig.Tracer = &schemaChangeTracer{change: func() {
		_, changeErr = writer.Exec(ctx, "INSERT INTO goose_db_version(version_id,is_applied) VALUES(45,true)")
	}}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db := &DB{engine: EnginePostgres, pool: pool, admission: admitted.admission}
	var archive bytes.Buffer
	manifest, err := Export(ctx, db, &archive, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if changeErr != nil {
		t.Fatal(changeErr)
	}
	tr := tar.NewReader(bytes.NewReader(archive.Bytes()))
	var payload string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == pgMemberPrefix+"goose_db_version" {
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			payload = string(b)
		}
	}
	// The schema change commits immediately after the version read. Both the
	// version and COPY must remain on that transaction's original snapshot.
	if manifest.SchemaVersion != 44 || !strings.Contains(payload, "\t44\tt\t") || strings.Contains(payload, "\t45\tt\t") {
		t.Fatalf("manifest=%d COPY=%q; want consistent original schema44", manifest.SchemaVersion, payload)
	}
	var live int64
	if err := writer.QueryRow(ctx, "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version").Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 45 {
		t.Fatalf("concurrent schema change did not commit: %d", live)
	}
}
