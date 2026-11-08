package upgrade

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

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
