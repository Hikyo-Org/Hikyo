package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/jackc/pgx/v5"
)

// BeginPostgresSerialized takes the existing named session lock before runtime
// admission. Settlement releases that connection before denial auditing can
// ask the pool for another connection. Ambiguous unlock discards the owner.
func (d *DB) BeginPostgresSerialized(ctx context.Context, namespace, key int32) (*PostgresTransaction, error) {
	if d == nil || d.engine != EnginePostgres || !d.admission.Valid() {
		return nil, upgrade.ErrConflict
	}
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	discard := func() {
		raw := conn.Hijack()
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = raw.Close(cleanup)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1,$2)", namespace, key); err != nil {
		discard()
		return nil, err
	}
	var once sync.Once
	var cleanupErr error
	release := func() error {
		once.Do(func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			var unlocked bool
			err := conn.QueryRow(cleanup, "SELECT pg_advisory_unlock($1,$2)", namespace, key).Scan(&unlocked)
			if err != nil || !unlocked {
				discard()
				cleanupErr = errors.Join(err, errors.New("store: serialized owner unlock was not confirmed"))
				return
			}
			conn.Release()
		})
		return cleanupErr
	}
	tx, err := d.beginPostgresOn(ctx, conn, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errors.Join(err, release())
	}
	tx.release = release
	return tx, nil
}
