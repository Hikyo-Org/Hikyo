package authz

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pairing"
)

// ReplayKeyLimit bounds one enrolled key's share of the durable replay store.
// Keep the installation-wide ceiling as a separate fail-closed storage bound.
const ReplayKeyLimit = 256

// AuthenticatePairedConnection is a separate authentication door. Ordinary
// bearer authentication rejects every connection with a persisted key binding.
// Replay consumption and all live credential checks share the serve transaction.
func (a *TxAuthorizer) AuthenticatePairedConnection(ctx context.Context, token, proof string, now time.Time) (Identity, error) {
	if crypto.ParseArtifact(token, crypto.ArtifactInstanceConn) != nil {
		return Identity{}, domain.ErrUnauthenticated
	}
	c, err := a.r.InstanceConnectionByVerifier(ctx, crypto.ArtifactVerifier(token))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return Identity{}, domain.ErrUnauthenticated
		}
		return Identity{}, err
	}
	epoch, err := a.r.CredentialEpoch(ctx)
	if err != nil {
		return Identity{}, err
	}
	if !c.Live(now, epoch) || !c.ProofBound {
		return Identity{}, domain.ErrUnauthenticated
	}
	binding, err := a.r.RemotePairedConnectionByID(ctx, c.ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return Identity{}, domain.ErrUnauthenticated
		}
		return Identity{}, err
	}
	verified, err := pairing.VerifyDPoP(proof, pairing.DPoPExpectation{Method: "GET", URL: binding.Origin + "/api/v1/instance/directory", AccessToken: token, Thumbprint: binding.Thumbprint, Now: now})
	if err != nil {
		return Identity{}, domain.ErrUnauthenticated
	}
	// Serialize the global replay census across nodes before pruning or claiming.
	// This uses the same identity-row-first order as invitation admission.
	if err := a.r.LockInstanceIdentityRow(ctx); err != nil {
		return Identity{}, err
	}
	if err := a.r.PruneRemotePairingState(ctx, now); err != nil {
		return Identity{}, err
	}
	keyCount, err := a.r.CountRemotePairingReplaysForKey(ctx, verified.Thumbprint)
	if err != nil {
		return Identity{}, err
	}
	if keyCount >= ReplayKeyLimit {
		return Identity{}, domain.ErrLimitExceeded
	}
	n, err := a.r.CountRemotePairingReplays(ctx)
	if err != nil {
		return Identity{}, err
	}
	if n >= 10000 {
		return Identity{}, domain.ErrLimitExceeded
	}
	accepted, err := a.r.ClaimRemotePairingReplay(ctx, verified.Thumbprint, crypto.ArtifactVerifier(verified.JTI), verified.IssuedAt.Add(pairing.MaxProofAge+pairing.MaxFutureSkew))
	if err != nil {
		return Identity{}, err
	}
	if !accepted {
		return Identity{}, domain.ErrUnauthenticated
	}
	return Identity{Principal: c.PrincipalID, Artifact: string(crypto.ArtifactInstanceConn), Class: domain.ClassInstanceConn, CredentialID: c.ID, CreatedAt: c.CreatedAt, LastSeenAt: c.LastUsedAt}, nil
}
