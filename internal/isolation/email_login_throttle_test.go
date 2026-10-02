package isolation

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Even unknown addresses share the canonical failure bucket. Otherwise domain
// casing buys additional password guesses and distinguishes missing accounts.
func TestEmailLoginCanonicalThrottle(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth := authService(t, db)
		now := time.Now()
		limiter, err := admission.New(admission.Config{ArgonMemoryKiB: crypto.PasswordFloor.MemoryKiB, Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		auth.Admission = limiter
		for _, email := range []string{"Unknown@EXAMPLE.COM", "Unknown@example.COM", "Unknown@Example.com", "Unknown@eXample.com", "Unknown@example.com", "Unknown@examplE.com"} {
			if _, err := auth.LocalLogin(t.Context(), email, "incorrect password", service.ArtifactCLI); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("failed login for %q = %v, want unauthenticated", email, err)
			}
		}
		if _, err := auth.LocalLogin(t.Context(), "Unknown@exAMPle.com", "incorrect password", service.ArtifactCLI); !errors.Is(err, admission.ErrOverloaded) {
			t.Fatalf("domain case bypass = %v, want overload", err)
		}
		if auth.Admission.AccountDelay("Unknown@example.com") <= 0 {
			t.Fatal("canonical email failure bucket has no backoff")
		}
		if auth.Admission.AccountDelay("unknown@example.com") != 0 {
			t.Fatal("local-part case was incorrectly folded")
		}
	})
}
