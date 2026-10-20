package app

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"net/url"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/remotefetch"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

const pairingOriginConfigurationError = service.PairingOriginConfigurationMessage

// Capture one immutable generation's serving identity. In particular, neither
// construction nor the configuration status performs DNS or network discovery.
func newPairingIdentity(cfg *config.Config, certificate *managedCertificate, fetcher *remotefetch.Client) (string, func(context.Context) (string, error)) {
	origin, explicit, override := cfg.ExternalOrigin, cfg.ExternalOriginExplicit, cfg.PairingSPKIPin
	canonical, err := remotefetch.CanonicalRemoteURL(origin)
	configurationError := ""
	if !explicit || err != nil || origin != canonical {
		configurationError = pairingOriginConfigurationError
	}
	var nativeLeaf *x509.Certificate
	if certificate != nil && certificate.pair != nil {
		nativeLeaf = certificate.pair.Leaf
	}
	return configurationError, func(ctx context.Context) (string, error) {
		if configurationError != "" {
			return "", service.ErrPairingOriginConfiguration
		}
		if err := ctx.Err(); err != nil {
			return "", service.ErrPairingTLSConfiguration
		}
		if override != "" {
			decoded, err := base64.StdEncoding.Strict().DecodeString(override)
			if err != nil || len(decoded) != 32 || base64.StdEncoding.EncodeToString(decoded) != override {
				return "", service.ErrPairingTLSConfiguration
			}
			return override, nil
		}
		var trustedNativeLeaf *x509.Certificate
		if nativeLeaf != nil {
			u, _ := url.Parse(origin) // canonical HTTPS origin checked above
			now := time.Now()
			if !now.Before(nativeLeaf.NotBefore) && now.Before(nativeLeaf.NotAfter) && nativeLeaf.VerifyHostname(u.Hostname()) == nil {
				trustedNativeLeaf = nativeLeaf
			}
		}
		if fetcher != nil {
			if pin, err := fetcher.PairingSPKIPin(ctx, origin, trustedNativeLeaf); err == nil {
				return pin, nil
			}
		}
		// Never disclose addresses, certificates, DNS results or proxy credentials.
		return "", service.ErrPairingTLSConfiguration
	}
}
