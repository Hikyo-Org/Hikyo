package app

import (
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/adapter/sealedwebhook"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
)

// activateSealedWebhookEndpoints validates the operator registry before the
// server serves. Any endpoint whose confirmed fingerprint does not match its
// origin, keys, and generation refuses the whole boot: a changed host or key
// is a new trust boundary that an instance admin must re-confirm.
func activateSealedWebhookEndpoints(cfg *config.SealedWebhookConfig) (sealedWebhookEndpoints, error) {
	if cfg == nil {
		return nil, nil
	}
	signer, err := sealedhook.ParseSigningKeyPEM(cfg.SigningKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("sealed webhook: %w", err)
	}
	out := make(sealedWebhookEndpoints, len(cfg.Targets))
	for _, target := range cfg.Targets {
		endpoint, err := sealedwebhook.NewEndpoint(sealedwebhook.EndpointConfig{
			ID: target.ID, InstanceID: cfg.InstanceID, Origin: target.Origin, RecipientKey: target.RecipientKey,
			AckKey: target.AckKey, Generation: target.Generation, Timeout: target.Timeout,
			MaxResponseBytes: target.MaxResponseBytes, ConfirmedFingerprint: target.ConfirmedFingerprint, Signer: signer,
		})
		if err != nil {
			return nil, fmt.Errorf("sealed webhook: %w", err)
		}
		out[endpoint.Origin()] = endpoint
	}
	return out, nil
}
