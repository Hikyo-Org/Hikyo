package service

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestCLIReauthHandoffSchemaAdmitsEveryServicePair(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]func(*testing.T) store.Config{
		"sqlite": func(t *testing.T) store.Config {
			return store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "handoff.db")}
		},
		"postgres": func(t *testing.T) store.Config { return selfConfigPostgres(t) },
	} {
		t.Run(name, func(t *testing.T) {
			s, local := selfConfigFixtureConfig(t, cfg(t), nil)
			_, sessionID := selfConfigSession(t, s, local)
			// The pairs come from the service's own table, so a purpose or operation
			// added there without a migration fails here instead of at a user's CLI.
			admitted := 0
			for i, descriptor := range reauthIntentDescriptors {
				if descriptor.variant == intentUnbound || !cliReauthPurposeOperation(descriptor.purpose, descriptor.operation) {
					continue
				}
				admitted++
				keySet := "key_set"
				if descriptor.purpose == PurposeAdapter || descriptor.purpose == PurposeMint {
					keySet = ""
				}
				now := time.Now()
				err := tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
					return az.CreateCLIReauthHandoff(ctx, authz.NewCLIReauthHandoff{
						ID: fmt.Sprintf("clh_pair_%d", i), StateVerifier: []byte(fmt.Sprintf("verifier-%032d", i)), SessionID: sessionID,
						PrincipalID: local.principal, Purpose: string(descriptor.purpose), Operation: string(descriptor.operation),
						EnvironmentSet: "env_a", KeySet: keySet, PKCEChallenge: "challenge", RedirectURI: "http://127.0.0.1:1/cb",
						CreatedAt: now, ExpiresAt: now.Add(time.Minute),
					})
				})
				if err != nil {
					t.Errorf("handoff (%s, %s) is admitted by the service but refused by the schema: %v", descriptor.purpose, descriptor.operation, err)
				}
			}
			if admitted == 0 {
				t.Fatal("no admitted pairs derived")
			}
		})
	}
}
