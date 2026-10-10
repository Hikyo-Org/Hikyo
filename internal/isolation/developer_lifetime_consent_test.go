package isolation

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestDeveloperLifetimeCeilingReadRequiresOnlyEnvironmentRead(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "developer-ceiling-reader")
		principal := string(fixture.admin.boot.PrincipalID)
		// Ordinary self-delegation must not depend on instance configuration rights.
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE principal_id='`+principal+`' AND capability NOT IN ('read','reveal'))`)
		execRaw(t, db, `DELETE FROM grants WHERE principal_id='`+principal+`' AND capability NOT IN ('read','reveal')`)
		actor := service.Bearer(fixture.admin.token)
		if _, err := (&service.DeveloperCredentials{DB: db, Auth: fixture.admin.auth}).Policy(t.Context(), actor); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("instance policy should require configuration permission: %v", err)
		}
		reveal := &service.Reveal{DB: db, Auth: fixture.admin.auth}
		for _, ceiling := range []time.Duration{8 * time.Hour, time.Hour} {
			execRaw(t, db, `UPDATE developer_credential_policy SET max_lifetime_seconds=`+strconv.FormatInt(int64(ceiling/time.Second), 10))
			window, err := reveal.Window(t.Context(), actor, envScope(envA1))
			if err != nil {
				t.Fatal(err)
			}
			if window.DeveloperCredentialMaxLifetimeSeconds != int64(ceiling/time.Second) {
				t.Fatalf("ceiling=%d, want %d", window.DeveloperCredentialMaxLifetimeSeconds, ceiling/time.Second)
			}
		}
		if _, err := reveal.Window(t.Context(), actor, envScope(envB1)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unreadable environment exposed policy: %v", err)
		}
	})
}
