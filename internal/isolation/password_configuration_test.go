package isolation

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestPasswordConfigurationRejectsLockout(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth := authService(t, db)
		if err := auth.CheckPasswordConfiguration(t.Context()); err != nil {
			t.Fatal(err)
		}
		admin := bootstrapAdmin(t, db, adminOpts{auth: auth, username: "kdf-admin", password: "correct horse battery staple KDF guard"})
		original := auth.KDF
		for _, candidate := range []crypto.PasswordParams{
			{MemoryKiB: original.MemoryKiB + 1024, Time: original.Time, Parallelism: original.Parallelism},
			{MemoryKiB: original.MemoryKiB, Time: original.Time + 1, Parallelism: original.Parallelism},
			{MemoryKiB: original.MemoryKiB, Time: original.Time, Parallelism: original.Parallelism + 1},
		} {
			auth.KDF = candidate
			if err := auth.CheckPasswordConfiguration(t.Context()); err == nil || !strings.Contains(err.Error(), "restore the previous Argon2 configuration") {
				t.Fatalf("incompatible cost accepted: %v", err)
			}
		}
		auth.KDF = original
		if err := auth.CheckPasswordConfiguration(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := auth.LocalLogin(t.Context(), "kdf-admin", admin.password, service.ArtifactCLI); err != nil {
			t.Fatalf("original configuration no longer authenticates: %v", err)
		}
		// Superseded credentials are already inert under the restore contract.
		// They must not prevent booting the recovery configuration.
		execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch=credential_epoch+1 WHERE id=1")
		auth.KDF.Time++
		if err := auth.CheckPasswordConfiguration(t.Context()); err != nil {
			t.Fatalf("recovery configuration rejected: %v", err)
		}
	})
}
