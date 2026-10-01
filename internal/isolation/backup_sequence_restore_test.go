package isolation

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestSupportedPostgresRestoreRejectsExhaustedSequences(t *testing.T) {
	c := newCustody(t)
	target := postgresTarget(t, t.TempDir(), c.recipient(t))
	target.configureCustody(t, c)
	db := target.open(t)
	for _, statement := range fixtureSQL {
		execRaw(t, db, statement)
	}
	seedOrigins(t, db)
	original := exportArchive(t, target)
	input, err := os.Open(original)
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	err = backup.ExtractTo(&plain, input, backup.Unlock{Identity: c.read(t, c.backupStore, "identity")})
	_ = input.Close()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := store.ReadManifest(bytes.NewReader(plain.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var sequences []string
	for name := range manifest.Sequences {
		sequences = append(sequences, name)
	}
	slices.Sort(sequences)
	if len(sequences) == 0 {
		t.Fatal("real archive has no sequences")
	}
	_ = db.Close()
	target.destroy(t)
	conn, err := pgx.Connect(t.Context(), target.cfg.Store.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(t.Context())
	type position struct {
		Value  int64
		Called bool
	}
	positions := func() map[string]position {
		out := map[string]position{}
		for _, name := range sequences {
			var p position
			if err := conn.QueryRow(t.Context(), "SELECT last_value,is_called FROM "+pq(name)).Scan(&p.Value, &p.Called); err != nil {
				t.Fatal(err)
			}
			out[name] = p
		}
		return out
	}
	for _, value := range []int64{math.MaxInt64, math.MaxInt64 - 1} {
		target.destroy(t)
		forged := rewriteSequenceArchive(t, plain.Bytes(), c, sequences[0], value)
		err := app.RunRestore(t.Context(), target.cfg, drillLogger(), []string{"run", "--from", forged, "--identity-file", c.identityFile()}, io.Discard, nil, nil)
		if !errors.Is(err, store.ErrArchiveFormat) || !strings.Contains(err.Error(), "safe restore range") {
			t.Fatalf("exhausted sequence %d was not refused: %v", value, err)
		}
		for name, got := range positions() {
			// The schema admission legitimately consumes Goose's bookkeeping
			// sequence while applying migrations, before archive import begins.
			if name == "goose_db_version_id_seq" {
				var migrationID int64
				if err := conn.QueryRow(t.Context(), "SELECT MAX(id) FROM goose_db_version").Scan(&migrationID); err != nil {
					t.Fatal(err)
				}
				if got.Value != migrationID || !got.Called {
					t.Fatalf("Goose sequence differs from legitimate migration bookkeeping: %+v, max migration ID %d", got, migrationID)
				}
				continue
			}
			if got.Value != 1 || got.Called {
				t.Fatalf("refused archive changed newly migrated target sequence %s: %+v", name, got)
			}
		}
		var count int
		if err := conn.QueryRow(t.Context(), "SELECT COUNT(*) FROM principals").Scan(&count); err != nil || count != 0 {
			t.Fatalf("refused archive imported principals: count %d error %v", count, err)
		}
	}
	// Same genuine payload and empty destination, without sequence pollution,
	// must restore normally and leave room for the next canonical sequence use.
	target.destroy(t)
	if err := app.RunRestore(t.Context(), target.cfg, drillLogger(), []string{"run", "--from", original, "--identity-file", c.identityFile()}, io.Discard, nil, nil); err != nil {
		t.Fatalf("normal archive restore: %v", err)
	}
	var next int64
	if err := conn.QueryRow(t.Context(), "SELECT nextval($1::regclass)", sequences[0]).Scan(&next); err != nil || next <= manifest.Sequences[sequences[0]] {
		t.Fatalf("restored sequence cannot advance: next %d, error %v", next, err)
	}
}

func rewriteSequenceArchive(t *testing.T, plain []byte, c custody, name string, value int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sequence-forgery.age")
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	encrypted, err := backup.Encrypt(out, backup.Options{Recipients: []string{c.recipient(t)}})
	if err != nil {
		t.Fatal(err)
	}
	w := tar.NewWriter(encrypted)
	r := tar.NewReader(bytes.NewReader(plain))
	for {
		header, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == "manifest.json" {
			var m store.Manifest
			if err := json.Unmarshal(payload, &m); err != nil {
				t.Fatal(err)
			}
			m.Sequences[name] = value
			payload, err = json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
		}
		header.Size = int64(len(payload))
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := encrypted.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
