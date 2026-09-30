package store

import (
	"fmt"
	"testing"
)

func TestPostgresSerializedAdmissionHoldsLockUntilSettlement(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprintf("commit=%t", commit), func(t *testing.T) {
			db, err := admittedStoreFixture(t, ownedAdmissionConfig(t, EnginePostgres))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			const namespace, key int32 = 1464159830, 619
			tx, err := db.BeginPostgresSerialized(t.Context(), namespace, key)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(t.Context())
			observer, err := db.pool.Acquire(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Release()
			var held bool
			if err := observer.QueryRow(t.Context(), "SELECT pg_try_advisory_lock($1,$2)", namespace, key).Scan(&held); err != nil {
				t.Fatal(err)
			}
			if held {
				_, _ = observer.Exec(t.Context(), "SELECT pg_advisory_unlock($1,$2)", namespace, key)
				t.Fatal("serialized admission released its session lock before settlement")
			}
			if commit {
				err = tx.Commit(t.Context())
			} else {
				err = tx.Rollback(t.Context())
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := observer.QueryRow(t.Context(), "SELECT pg_try_advisory_lock($1,$2)", namespace, key).Scan(&held); err != nil {
				t.Fatal(err)
			}
			if !held {
				t.Fatal("serialized admission kept its session lock after settlement")
			}
			if _, err := observer.Exec(t.Context(), "SELECT pg_advisory_unlock($1,$2)", namespace, key); err != nil {
				t.Fatal(err)
			}
		})
	}
}
