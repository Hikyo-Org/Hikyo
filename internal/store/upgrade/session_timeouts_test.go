package upgrade

import (
	"os"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func TestCheckPostgresVersion(t *testing.T) {
	for _, ok := range []string{"180000", "180001", "190000"} {
		if err := CheckPostgresVersion(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"170009", "160000", "90600", "", "18", "18.1"} {
		err := CheckPostgresVersion(bad)
		if err == nil {
			t.Errorf("%q not refused", bad)
			continue
		}
		if !strings.Contains(err.Error(), "PostgreSQL 18 or later") {
			t.Errorf("%q: refusal does not name the requirement: %v", bad, err)
		}
	}
	if err := CheckPostgresVersion("90600"); err == nil || !strings.Contains(err.Error(), "server version 9.6.0 (") {
		t.Errorf("pre-10 version formatted wrongly: %v", err)
	}
	err := CheckPostgresVersion("170009")
	if want := "unsupported PostgreSQL server version 17.9 (server_version_num 170009); Hikyo requires PostgreSQL 18 or later (server_version_num 180000)"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("message = %v, want it to contain %q", err, want)
	}
}

// HIKYO_TEST_POSTGRES_OLD_DSN points at a PostgreSQL older than 18 (a disposable
// postgres:17-alpine). CI starts one; elsewhere the tests skip.
func oldPostgresDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_OLD_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires the PostgreSQL 17 refusal fixture")
		}
		t.Skip("HIKYO_TEST_POSTGRES_OLD_DSN not set")
	}
	return dsn
}

// upgrade.open serves every operator session, restore included.
func TestLockRefusesOldPostgres(t *testing.T) {
	ran := false
	err := WithLock(t.Context(), Config{Engine: releaseidentity.Postgres, DSN: oldPostgresDSN(t)}, func(*Session) error {
		ran = true
		return nil
	})
	if ran || err == nil || !strings.Contains(err.Error(), "requires PostgreSQL 18 or later") {
		t.Fatalf("operator session not refused (ran=%v): %v", ran, err)
	}
}

// The lock session runs a target's migrations, so a target that dies mid-migration leaves it
// behind; the server must drop it soon after, or the rollback refuses beside it.
func TestLockSessionDropsDeadClient(t *testing.T) {
	err := WithLock(t.Context(), testConfig(t, releaseidentity.Postgres), func(s *Session) error {
		var version string
		if err := s.conn.QueryRowContext(t.Context(), "SELECT version()").Scan(&version); err != nil {
			return err
		}
		want := map[string]string{"tcp_keepalives_idle": "60", "tcp_keepalives_interval": "10", "tcp_keepalives_count": "3"}
		// A server reports 0 where its OS lacks TCP_USER_TIMEOUT, as macOS does; Linux has it.
		if strings.Contains(version, "linux") {
			want["tcp_user_timeout"] = "30000"
		}
		for name, value := range want {
			var got string
			if err := s.conn.QueryRowContext(t.Context(), "SELECT current_setting($1)", name).Scan(&got); err != nil {
				return err
			}
			if got != value {
				t.Errorf("%s = %s, want %s", name, got, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
