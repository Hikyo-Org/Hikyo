package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// The production gate, `hikyo migrate` and the preflight must each carry the real
// revocation checker. A hook that quietly returns "nothing revoked" would let a revoked
// bridge admit; the real checker refuses a missing state directory or a bad mode, which
// a no-op never does.
func TestProductionGateRequestsCarryTheRealRevocationChecker(t *testing.T) {
	for name, setting := range map[string]struct{ mode, want string }{
		"no state directory": {mode: "", want: "HIKYO_UPGRADE_STATE_DIR"},
		"unknown mode":       {mode: "sometimes", want: "must be online or off"},
	} {
		cfg := &config.Config{Store: config.Datastore{Engine: config.EngineSQLite, Path: filepath.Join(t.TempDir(), "hikyo.db")}}
		cfg.Upgrade.RevocationCheck = setting.mode
		// upgradeRequest returns the request it built even when later setup refuses it.
		gate, cleanup, _ := upgradeRequest(t.Context(), cfg, nil, upgradegate.Migrate)
		cleanup()
		for hookName, hook := range map[string]func(context.Context, releasetrust.Snapshot) ([]releaseidentity.Digest, error){
			"gate and migrate": gate.BridgeRevocation,
			"preflight":        upgradeSourceRequest(cfg, releasetrust.PinnedTrust{}, nil).BridgeRevocation,
		} {
			if hook == nil {
				t.Fatalf("%s %s: no revocation hook, so a bridge route would be refused or unguarded", name, hookName)
			}
			if _, err := hook(t.Context(), releasetrust.Snapshot{}); err == nil || !strings.Contains(err.Error(), setting.want) {
				t.Fatalf("%s %s: want the real checker's refusal %q, got %v", name, hookName, setting.want, err)
			}
		}
	}
}

// A discovery reloads its growing bundle several times; the list is fetched once, and a
// refusal is asked again rather than remembered.
func TestOnceRevocationFetchesOncePerSnapshotAndNeverRemembersARefusal(t *testing.T) {
	snapshot := trustfixture.New(t).Snapshot(t)
	calls, fail := 0, true
	lookup := onceRevocation(func(context.Context, releasetrust.Snapshot) ([]releaseidentity.Digest, error) {
		calls++
		if fail {
			return nil, errors.New("endpoint refused")
		}
		return []releaseidentity.Digest{releaseidentity.Hash([]byte("bridge"))}, nil
	})
	if _, err := lookup(t.Context(), snapshot); err == nil {
		t.Fatal("the refusal is returned")
	}
	fail = false
	for range 3 {
		if revoked, err := lookup(t.Context(), snapshot); err != nil || len(revoked) != 1 {
			t.Fatalf("answer: %v %v", revoked, err)
		}
	}
	if calls != 2 {
		t.Fatalf("one refused ask and one answered fetch, got %d calls", calls)
	}
}
