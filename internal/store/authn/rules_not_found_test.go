package authn

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/jackc/pgx/v5"
)

func TestRulePendingNotFoundWrappedDrivers(t *testing.T) {
	for _, missing := range []error{sql.ErrNoRows, pgx.ErrNoRows} {
		for _, err := range []error{missing, fmt.Errorf("pending metadata: %w", missing)} {
			if got := rulePendingNotFound(err); !errors.Is(got, domain.ErrNotFound) {
				t.Fatalf("wrapped no-row error = %v; want domain not found", got)
			}
		}
	}
	failure := errors.New("database unavailable")
	if got := rulePendingNotFound(failure); got != failure {
		t.Fatalf("database failure changed to %v", got)
	}
	if got := rulePendingNotFound(nil); got != nil {
		t.Fatalf("nil changed to %v", got)
	}
}
