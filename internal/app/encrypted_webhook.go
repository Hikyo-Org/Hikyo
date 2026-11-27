package app

import (
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/adapter/encryptedwebhook"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/encryptedhook"
)

// activateEncryptedWebhookEndpoints validates the operator registry before the
// server serves. Any endpoint whose confirmed fingerprint does not match its
// origin, keys, and generation refuses the whole boot: a changed host or key
// is a new trust boundary that an instance admin must re-confirm.
func activateEncryptedWebhookEndpoints(cfg *config.EncryptedWebhookConfig) (encryptedWebhookEndpoints, error) {
	if cfg == nil {
		return nil, nil
	}
	signer, err := encryptedhook.ParseSigningKeyPEM(cfg.SigningKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("encrypted webhook: %w", err)
	}
	var signerPQ *encryptedhook.MLDSASigner
	if len(cfg.SigningKeyMLDSA65PEM) > 0 {
		if signerPQ, err = encryptedhook.ParseMLDSASigningKeyPEM(cfg.SigningKeyMLDSA65PEM); err != nil {
			return nil, fmt.Errorf("encrypted webhook: %w", err)
		}
	}
	out := make(encryptedWebhookEndpoints, len(cfg.Targets))
	for _, target := range cfg.Targets {
		endpointConfig := encryptedwebhook.EndpointConfig{
			ID: target.ID, InstanceID: cfg.InstanceID, Origin: target.Origin, RecipientKey: target.RecipientKey,
			AckKey: target.AckKey, Generation: target.Generation, Timeout: target.Timeout,
			MaxResponseBytes: target.MaxResponseBytes, ConfirmedFingerprint: target.ConfirmedFingerprint, Signer: signer,
			Protocol: target.Protocol, AckKeyMLDSA65: target.AckKeyMLDSA65,
		}
		if target.Protocol == 2 {
			endpointConfig.SignerPQ = signerPQ
		}
		endpoint, err := encryptedwebhook.NewEndpoint(endpointConfig)
		if err != nil {
			return nil, fmt.Errorf("encrypted webhook: %w", err)
		}
		out[endpoint.Origin()] = endpoint
	}
	return out, nil
}
