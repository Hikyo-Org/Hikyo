//go:build floorbench

package isolation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Fixtures use the existing signed development admission and tenant harness.
// Bulk preparation is outside the timer; the measured publish and rewrap use
// the actual service transaction, authorization, encryption and durability.
func TestFloorBenchPublish(t *testing.T) {
	db := seededDB(t, openSQLite)
	floorSeedCells(t, db, 10, 10000)
	minLength := 1
	started := time.Now()
	_, err := keySvc(t, db).UpdateDeclaration(t.Context(), service.LocalPrincipal(custodian),
		scopeProject(orgA, prjA1), keyA1, service.KeyDeclarationUpdate{
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString, MinLength: &minLength}},
			Presence:    schema.DefaultPresenceRules(),
		}, nil)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("100000-cell schema publish after %s: %v", elapsed, err)
	}
	// Export every committed snapshot through its real authorized service.
	// Counting catalogue rows alone would accept an empty or partial publish.
	read := 0
	for _, env := range floorEnvironments(10) {
		values, revision, err := revisionSvc(t, db).Export(t.Context(), service.LocalPrincipal(custodian),
			domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID(env)}, 0, false)
		if err != nil || revision < 1 || len(values) != 10000 {
			t.Fatalf("committed environment %s: revision=%d cells=%d error=%v", env, revision, len(values), err)
		}
		for _, value := range values {
			if !value.Revealed || value.Value != "floor-value" {
				t.Fatal("committed cell did not preserve the fixture value")
			}
		}
		read += len(values)
	}
	floorWrite(t, "publish.json", map[string]any{"elapsed_ms": float64(elapsed) / float64(time.Millisecond), "cells": read, "environments": 10, "keys": 10000, "operation": "Keys.UpdateDeclaration schema fan-out", "readback_verified": true})
}

func floorEnvironments(count int) []string {
	envs := []string{"env_a1", "env_prod"}
	for i := 2; i < count; i++ {
		envs = append(envs, fmt.Sprintf("floor_env_%02d", i))
	}
	return envs[:count]
}

func floorSeedCells(t *testing.T, db *store.DB, environments, keys int) {
	t.Helper()
	for _, env := range floorEnvironments(environments)[2:] {
		execRaw(t, db, fmt.Sprintf(`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('%s','org_a','prj_a1','%s','','2026-01-01T00:00:00Z',2)`, env, env))
	}
	execRaw(t, db, fmt.Sprintf(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < %d)
	 INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,group_id,created_at)
	 SELECT printf('floor_key_%%05d',i),'org_a','prj_a1',printf('FLOOR_%%05d',i),'','config','',FALSE,'','{"rule":{"type":"string"}}','none','none',NULL,'2026-01-01T00:00:00Z' FROM n`, keys-1))
	sealer, err := probeKeyring(t, db).ForProject(t.Context(), string(orgA), string(prjA1))
	if err != nil {
		t.Fatal(err)
	}
	// One prepared transaction makes the large valid fixture cheap to prepare.
	// Native handles remain confined to this tagged fixture, never app code.
	tx, err := db.SQLiteWrite().BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(t.Context(), `INSERT INTO value_entries (id,org_id,project_id,environment_id,key_id,ciphertext,updated_at,updated_by) VALUES (?,'org_a','prj_a1',?,?,?,'2026-01-01T00:00:00Z','usr_custodian')`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	for _, env := range floorEnvironments(environments) {
		for i := 0; i < keys; i++ {
			key := fmt.Sprintf("floor_key_%05d", i)
			if i == 0 {
				key = keyA1
			}
			row := env + "_" + key
			aad := crypto.ValueAAD{OrgID: string(orgA), ProjectID: string(prjA1), EnvID: env, KeyID: key, RowID: row, FieldTag: "value"}
			ciphertext, err := sealer.SealValue(aad, []byte("floor-value"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := stmt.ExecContext(t.Context(), row, env, key, ciphertext); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestFloorBenchReencrypt(t *testing.T) {
	db := seededDB(t, openSQLite)
	floorSeedCells(t, db, 2, 125)
	kr := probeKeyring(t, db)
	rotation := &service.Rotation{DB: db, Keyring: kr, RootKey: probeRootSource{db: db}}
	if _, err := rotation.RotateDEK(t.Context(), service.LocalPrincipal(root), service.DEKScope{OrgID: string(orgA), ProjectID: string(prjA1)}); err != nil {
		t.Fatal(err)
	}
	var times []time.Time
	var committed []int
	// An independent read connection sees the previous committed chunk. This
	// proves the actual batch sizes without changing the walk or its pause.
	re := &service.Reencrypt{DB: db, Keyring: kr, AfterChunk: func(ctx context.Context, table string) error {
		if table != "value" {
			return nil
		}
		times = append(times, time.Now())
		rows, err := db.SQLiteRead().QueryContext(ctx, `SELECT ciphertext FROM value_entries WHERE project_id='prj_a1'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var blob []byte
			if err := rows.Scan(&blob); err != nil {
				return err
			}
			version, err := crypto.RecordKeyVersion(blob)
			if err != nil {
				return err
			}
			if version == 2 {
				count++
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		committed = append(committed, count)
		return nil
	}}
	started := time.Now()
	result, err := re.ReencryptProject(t.Context(), service.LocalPrincipal(root), string(orgA), string(prjA1))
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if len(committed) != 3 || committed[0] != 0 || committed[1] != 100 || committed[2] != 200 {
		t.Fatalf("committed chunk progression: %v", committed)
	}
	if service.ReencryptChunkSize != 100 || service.ReencryptChunkPause != 100*time.Millisecond {
		t.Fatal("production chunk defaults differ from declared floor")
	}
	minimumPause := times[1].Sub(times[0])
	for i := 2; i < len(times); i++ {
		minimumPause = min(minimumPause, times[i].Sub(times[i-1]))
	}
	if minimumPause < 100*time.Millisecond {
		t.Fatalf("inter-chunk interval %s", minimumPause)
	}
	sealer, err := kr.ForProject(t.Context(), string(orgA), string(prjA1))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.SQLiteRead().QueryContext(t.Context(), `SELECT id,environment_id,key_id,ciphertext FROM value_entries WHERE project_id='prj_a1'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	read := 0
	for rows.Next() {
		var id, env, key string
		var blob []byte
		if err := rows.Scan(&id, &env, &key, &blob); err != nil {
			t.Fatal(err)
		}
		version, err := crypto.RecordKeyVersion(blob)
		if err != nil || version != 2 {
			t.Fatal("row remains on old key")
		}
		plain, err := sealer.OpenValue(crypto.ValueAAD{OrgID: string(orgA), ProjectID: string(prjA1), EnvID: env, KeyID: key, RowID: id, FieldTag: "value"}, blob)
		if err != nil || string(plain) != "floor-value" {
			t.Fatal("rewrapped row unreadable")
		}
		read++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if read != 250 || result.RowsMoved < 250 {
		t.Fatal("rewrap did not finish every value row")
	}
	floorWrite(t, "reencrypt.json", map[string]any{"elapsed_ms": float64(elapsed) / float64(time.Millisecond), "value_rows": read, "rows_moved": result.RowsMoved, "chunk_rows": 100, "committed_progression": committed, "minimum_interchunk_interval_ms": float64(minimumPause) / float64(time.Millisecond), "configured_pause_ms": 100, "readback_verified": true})
}

func floorWrite(t *testing.T, name string, value any) {
	t.Helper()
	directory := os.Getenv("HIKYO_FLOOR_EVIDENCE")
	if directory == "" {
		t.Fatal("HIKYO_FLOOR_EVIDENCE is required")
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
