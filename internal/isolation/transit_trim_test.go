package isolation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/transit"
)

type interleavedTrimCustody struct {
	transit.Custody
	beforeDestroy func()
	failVersion   uint32
}

func (p *interleavedTrimCustody) Destroy(ctx context.Context, target transit.Target, version transit.Version) error {
	if f := p.beforeDestroy; f != nil {
		p.beforeDestroy = nil
		f()
	}
	if target.Binding.Version == p.failVersion {
		return transit.ErrUnavailable
	}
	return p.Custody.Destroy(ctx, target, version)
}

// Force two trim attempts to interleave at the external provider boundary.
// The second attempt fences a higher floor but cannot destroy all material.
// The first must not erase references to material only the second saw.
func TestTransitTrimFencesConfigurationAndPreservesUnprocessedMaterial(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc, ext := transitEnv(t, db, "trim")
		key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "trim-race", Algorithm: "xchacha20-poly1305", Custody: "external", AllowedOperations: []string{"encrypt", "decrypt"}})
		ctx, actor := tctx(t), service.LocalPrincipal(alice)
		for i := 0; i < 2; i++ {
			if _, err := svc.RotateKey(ctx, actor, transitScope, key.Name); err != nil {
				t.Fatal(err)
			}
		}
		one, two, three := uint32(1), uint32(2), uint32(3)
		if _, err := svc.ConfigureKey(ctx, actor, transitScope, key.Name, service.ConfigureTransitKeyRequest{MinEncryptVersion: &two, MinDecryptVersion: &two}); err != nil {
			t.Fatal(err)
		}
		provider := &interleavedTrimCustody{Custody: ext}
		svc.Custody = transit.NewRegistry(&transit.Software{Keyring: svc.Keyring}, provider)
		provider.beforeDestroy = func() {
			if _, err := svc.ConfigureKey(ctx, actor, transitScope, key.Name, service.ConfigureTransitKeyRequest{MinDecryptVersion: &one}); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("lowering fenced floor: %v", err)
			}
			if _, err := svc.ConfigureKey(ctx, actor, transitScope, key.Name, service.ConfigureTransitKeyRequest{MinEncryptVersion: &three, MinDecryptVersion: &three}); err != nil {
				t.Fatal(err)
			}
			provider.failVersion = 2
			if _, _, err := svc.TrimKey(ctx, actor, transitScope, key.Name); err == nil {
				t.Fatal("provider failure accepted")
			}
			provider.failVersion = 0
		}
		view, n, err := svc.TrimKey(ctx, actor, transitScope, key.Name)
		if err != nil || n != 1 {
			t.Fatalf("first trim: deleted=%d err=%v", n, err)
		}
		if len(view.Versions) != 2 || view.Versions[0].Version != 2 || !view.Versions[0].ExternalHeld {
			t.Fatalf("unprocessed version lost: %+v", view.Versions)
		}
		view, n, err = svc.TrimKey(ctx, actor, transitScope, key.Name)
		if err != nil || n != 1 || len(view.Versions) != 1 || view.Versions[0].Version != 3 {
			t.Fatalf("retry trim: deleted=%d versions=%+v err=%v", n, view.Versions, err)
		}
	})
}

func TestTransitRotationCountsRetainedVersions(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc, _ := transitEnv(t, db, "limit")
		k := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "version-limit", Algorithm: "xchacha20-poly1305", AllowedOperations: []string{"encrypt"}})
		// Fill the retained-version quota without thousands of cryptographic calls.
		execRaw(t, db, fmt.Sprintf(`WITH RECURSIVE n(v) AS (SELECT 2 UNION ALL SELECT v+1 FROM n WHERE v<%d) INSERT INTO transit_key_versions (id,org_id,project_id,environment_id,key_id,version,material_ciphertext,external_ref,public_key,created_at) SELECT 'tkv_limit_' || n.v,k.org_id,k.project_id,k.environment_id,k.key_id,n.v,k.material_ciphertext,NULL,k.public_key,k.created_at FROM n CROSS JOIN transit_key_versions k WHERE k.key_id='%s' AND k.version=1`, service.MaxTransitVersionsPerKey, k.ID))
		execRaw(t, db, fmt.Sprintf(`UPDATE transit_keys SET latest_version=%d,min_encrypt_version=%d,min_decrypt_version=%d WHERE id='%s'`, service.MaxTransitVersionsPerKey, service.MaxTransitVersionsPerKey, service.MaxTransitVersionsPerKey, k.ID))
		ctx, actor := tctx(t), service.LocalPrincipal(alice)
		if _, err := svc.RotateKey(ctx, actor, transitScope, k.Name); !errors.Is(err, domain.ErrLimitExceeded) {
			t.Fatalf("untrimmed rotation: %v", err)
		}
		if _, n, err := svc.TrimKey(ctx, actor, transitScope, k.Name); err != nil || n != service.MaxTransitVersionsPerKey-1 {
			t.Fatalf("trim quota: %d %v", n, err)
		}
		if _, err := svc.RotateKey(ctx, actor, transitScope, k.Name); err != nil {
			t.Fatalf("rotation after trim: %v", err)
		}
	})
}

func TestTransitConcurrentCreateHonorsEnvironmentLimit(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc, _ := transitEnv(t, db, "keys")
		k := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "key-limit", Algorithm: "xchacha20-poly1305"})
		execRaw(t, db, fmt.Sprintf(`WITH RECURSIVE n(v) AS (SELECT 2 UNION ALL SELECT v+1 FROM n WHERE v<%d) INSERT INTO transit_keys (id,org_id,project_id,environment_id,name,algorithm,custody,allowed_operations,state,latest_version,min_encrypt_version,min_decrypt_version,created_by,created_at,updated_at) SELECT 'tk_limit_' || n.v,k.org_id,k.project_id,k.environment_id,'key-limit-' || n.v,k.algorithm,k.custody,k.allowed_operations,k.state,k.latest_version,k.min_encrypt_version,k.min_decrypt_version,k.created_by,k.created_at,k.updated_at FROM n CROSS JOIN transit_keys k WHERE k.id='%s'`, service.MaxTransitKeysPerEnvironment-1, k.ID))
		const contenders = 8
		start := make(chan struct{})
		results := make(chan error, contenders)
		var wg sync.WaitGroup
		for i := 0; i < contenders; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := svc.CreateKey(tctx(t), service.LocalPrincipal(alice), transitScope, service.CreateTransitKeyRequest{Name: fmt.Sprintf("contender-%d", i), Algorithm: "xchacha20-poly1305"})
				results <- err
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		accepted := 0
		for err := range results {
			if err == nil {
				accepted++
			} else if !errors.Is(err, domain.ErrLimitExceeded) {
				t.Fatalf("create refused unexpectedly: %v", err)
			}
		}
		if accepted != 1 {
			t.Fatalf("accepted %d keys into one remaining slot", accepted)
		}
	})
}

func TestTransitPurgeFencesDelayedCancellation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc, ext := transitEnv(t, db, "purge")
		start := time.Now().UTC()
		svc.Now = func() time.Time { return start }
		k := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "purge-race", Algorithm: "xchacha20-poly1305", Custody: "external"})
		actor, ctx := service.LocalPrincipal(alice), tctx(t)
		if _, err := svc.ChangeKeyState(ctx, actor, transitScope, k.Name, "schedule-deletion", 24*time.Hour); err != nil {
			t.Fatal(err)
		}
		svc.Now = func() time.Time { return start.Add(25 * time.Hour) }
		provider := &interleavedTrimCustody{Custody: ext}
		svc.Custody = transit.NewRegistry(&transit.Software{Keyring: svc.Keyring}, provider)
		provider.beforeDestroy = func() {
			// Models cancellation timestamped before the deadline but delayed until
			// the scheduler has crossed the destructive provider boundary.
			svc.Now = func() time.Time { return start.Add(time.Hour) }
			_, err := svc.ChangeKeyState(ctx, actor, transitScope, k.Name, "cancel-deletion", 0)
			wantConflictCause(t, err, "state")
		}
		if n, err := svc.PurgeDue(ctx); err != nil || n != 1 {
			t.Fatalf("fenced purge: %d %v", n, err)
		}
		if _, err := svc.GetKey(ctx, actor, transitScope, k.Name); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("purged key accessible: %v", err)
		}
	})
}
