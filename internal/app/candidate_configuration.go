package app

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/scanning"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// checkCandidateConfiguration validates local constructor inputs before the
// gate can mark a migrated candidate healthy. No runtime DB, listener, worker,
// secret-provider call or new key hierarchy is available here.
func checkCandidateConfiguration(ctx context.Context, cfg *config.Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, _, err := AuthComponents(cfg); err != nil {
		return fmt.Errorf("authentication configuration: %w", err)
	}
	if _, err := scanning.Load(); err != nil {
		return fmt.Errorf("secret-scanning ruleset: %w", err)
	}
	if _, err := parseCIDRs(cfg.TrustedProxyCIDRs); err != nil {
		return err
	}
	auth := service.Auth{ExternalOrigin: cfg.ExternalOrigin}
	if err := auth.ConfigureWebAuthnRP(); err != nil {
		return fmt.Errorf("WebAuthn relying-party configuration: %w", err)
	}
	if cfg.TLSCertFile != "" {
		if _, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil {
			return fmt.Errorf("TLS certificate configuration: %w", err)
		}
	}
	return ctx.Err()
}
