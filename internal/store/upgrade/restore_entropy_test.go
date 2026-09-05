package upgrade

import (
	"bytes"
	"reflect"
	"testing"
)

func TestRestoreEntropyFailureLeavesTransactionUnchanged(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		old := bootstrap(t, cfg)
		query(t, cfg, `CREATE TABLE auth_instance_state(id INTEGER PRIMARY KEY,credential_epoch BIGINT,restore_epoch BIGINT)`)
		query(t, cfg, `INSERT INTO auth_instance_state VALUES(1,99,99)`)
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			return s.transaction(t.Context(), func() error {
				_, err := reconcileRestore(t.Context(), func(q string, a ...any) scanner { return s.conn.QueryRowContext(t.Context(), q, a...) }, func(q string, a ...any) (int64, error) {
					result, err := s.conn.ExecContext(t.Context(), q, a...)
					if err != nil {
						return 0, err
					}
					return result.RowsAffected()
				}, bytes.NewReader(nil))
				if err == nil {
					t.Fatal("entropy failure accepted")
				}
				read, readErr := s.Read(t.Context())
				if readErr != nil {
					return readErr
				}
				if !reflect.DeepEqual(old, read) {
					t.Fatal("entropy failure changed state")
				}
				return nil
			})
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
