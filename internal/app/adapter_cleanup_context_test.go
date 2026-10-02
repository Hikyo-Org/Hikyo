package app

import (
	"context"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestAdapterLoaderCleanupCapturesAttemptContextAndAlwaysZerosCredential(t *testing.T) {
	for _, kind := range []adapter.JobKind{adapter.Converge, adapter.Activate} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), adapter.AttemptTimeout)
			defer cancel()
			deadline, _ := ctx.Deadline()
			credential := []byte("cleanup-provider-secret")
			calls := 0
			loader := &adapterLoader{
				moduleFactory: func(adapter.Provider, adapter.Config, string) (*adapter.ModuleLease, error) {
					return adapter.NewModuleLeaseWithContext(stubProviderModule{}, func(got context.Context) {
						calls++
						gotDeadline, ok := got.Deadline()
						if got != ctx || !ok || !gotDeadline.Equal(deadline) || got.Err() != context.Canceled {
							t.Fatal("cleanup escaped originating attempt context")
						}
						for _, value := range credential {
							if value != 0 {
								t.Fatal("provider cleanup started before plaintext was zeroed")
							}
						}
					})
				},
				openField: func(crypto.ProjectFieldAAD, []byte) ([]byte, error) { return credential, nil },
				loadExecution: func(context.Context, adapter.Job) (store.AdapterExecution, error) {
					return store.AdapterExecution{Provider: "vault-kv", Origin: "https://vault.example", CredentialOwnerID: "adapter", CredentialCiphertext: []byte{1}, Revision: 7}, nil
				},
				loadActivation: func(context.Context, adapter.Job) (store.AdapterActivation, error) {
					return store.AdapterActivation{Provider: "vault-kv", Origin: "https://vault.example", CredentialOwnerID: "adapter", CredentialCiphertext: []byte{1}}, nil
				},
			}
			var release func()
			journal := &orderedLoaderJournal{}
			if kind == adapter.Activate {
				loaded, err := loader.LoadActivation(ctx, adapter.Job{Kind: kind}, journal)
				if err != nil {
					t.Fatal(err)
				}
				release = loaded.Release
			} else {
				loaded, err := loader.Load(ctx, adapter.Job{Kind: kind}, journal)
				if err != nil {
					t.Fatal(err)
				}
				release = loaded.Release
			}
			if remaining := time.Until(deadline); remaining > 2*time.Minute || remaining < time.Minute {
				t.Fatalf("attempt deadline = %v", remaining)
			}
			cancel()
			release()
			release()
			if calls != 1 {
				t.Fatalf("cleanup calls = %d; want 1", calls)
			}
		})
	}
}
