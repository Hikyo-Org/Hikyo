package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

func TestBridgeRevocationRefusesMisconfiguration(t *testing.T) {
	for name, tc := range map[string]struct {
		check, state, want string
	}{
		"no state directory": {check: "off", state: "", want: "HIKYO_UPGRADE_STATE_DIR"},
		"unknown mode":       {check: "sometimes", state: t.TempDir(), want: "online or off"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Upgrade.RevocationCheck, cfg.Upgrade.StateDirectory = tc.check, tc.state
			_, err := bridgeRevocation(cfg)(t.Context(), releasetrust.Snapshot{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

type refusingFetcher struct{}

func (refusingFetcher) Fetch(context.Context) ([]byte, []byte, error) {
	return nil, nil, errors.New("offline")
}

// With no HIKYO_UPGRADE_REVOCATION_URL the check reads the trust release asset.
func TestBridgeRevocationDefaultsToTheTrustReleaseAsset(t *testing.T) {
	var address string
	previous := newRevocationFetcher
	newRevocationFetcher = func(a string, _ func(*http.Request) (*url.URL, error)) (bridgerevocation.Fetcher, error) {
		address = a
		return refusingFetcher{}, nil
	}
	t.Cleanup(func() { newRevocationFetcher = previous })
	cfg := &config.Config{}
	cfg.Upgrade.StateDirectory = t.TempDir()
	_, _ = bridgeRevocation(cfg)(t.Context(), releasetrust.Snapshot{})
	if want := "https://github.com/Hikyo-Org/Hikyo/releases/download/trust/recovery-revocations"; address != want || bridgerevocation.DefaultURL != want {
		t.Fatalf("default address %q, DefaultURL %q, want %q", address, bridgerevocation.DefaultURL, want)
	}
}
