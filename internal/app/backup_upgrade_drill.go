package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// UpgradeDrillResult contains public proof outcomes and an unsigned statement.
// Signing occurs only after this function has performed the actual recovery.
type UpgradeDrillResult struct {
	Attestation       []byte
	HierarchyReadable bool
	SecretProof       string
	CredentialProof   string
}

type UpgradeDrillRequest struct {
	Scratch    store.Config
	Ciphertext *backupreceipt.Ciphertext
	Receipt    []byte
	Plan       upgradecompat.Plan
	Operator   backupreceipt.PinnedOperator
	Unlock     backup.Unlock
	RootKey    []byte
	Principal  domain.PrincipalID
	Scope      domain.Scope
	Now        time.Time
	Lifetime   time.Duration
	// AutoCredentialProof selects one existing authorized principal only in
	// this isolated scratch restore. Ordinary manual drills remain explicit.
	AutoCredentialProof bool
}

// DrillUpgrade is a local operator operation. It consumes separately supplied
// root escrow, decrypts a pinned copy in full, restores an empty scratch target,
// proves the existing hierarchy and never writes private custody to a server.
func DrillUpgrade(ctx context.Context, request UpgradeDrillRequest) (UpgradeDrillResult, error) {
	defer crypto.Zero(request.RootKey)
	if request.Ciphertext == nil || !request.Operator.Valid() || request.Now.IsZero() || request.Lifetime <= 0 || request.Lifetime > backupreceipt.MaxAttestationLifetime {
		return UpgradeDrillResult{}, errors.New("upgrade drill requires complete operator custody and bounded validity")
	}
	if request.AutoCredentialProof && (request.Principal != "" || request.Scope != (domain.Scope{})) {
		return UpgradeDrillResult{}, errors.New("automatic scratch credential selection cannot be combined with an explicit principal or project")
	}
	receipt, err := backupreceipt.ParseReceipt(request.Receipt)
	if err != nil {
		return UpgradeDrillResult{}, err
	}
	if err := backupreceipt.CheckReceiptPlan(receipt, request.Plan); err != nil {
		return UpgradeDrillResult{}, err
	}
	if receipt.Snapshot.InstanceID != request.Operator.InstanceID() || receipt.Snapshot.Engine != releaseidentity.Engine(request.Scratch.Engine) {
		return UpgradeDrillResult{}, errors.New("upgrade drill installation or scratch engine mismatch")
	}
	if err := store.VerifyEmbeddedUpgradeSource(request.Plan, request.Scratch.Engine); err != nil {
		return UpgradeDrillResult{}, err
	}
	authenticated, err := backupreceipt.AuthenticateArchive(ctx, request.Ciphertext, request.Receipt, request.Plan, request.Unlock, "")
	if err != nil {
		return UpgradeDrillResult{}, err
	}
	defer authenticated.Close()
	plain, err := authenticated.Open()
	if err != nil {
		return UpgradeDrillResult{}, err
	}
	manifest, _, err := store.ReadManifestEvidence(plain)
	if err != nil {
		return UpgradeDrillResult{}, err
	}
	if err := checkRestorable(ctx, request.Scratch, manifest); err != nil {
		return UpgradeDrillResult{}, err
	}
	switch request.Scratch.Engine {
	case store.EngineSQLite:
		if _, err := tx.RestoreUpgradeSQLite(ctx, plain, request.Scratch.Path, request.Plan, service.CompleteRestore(request.Now, manifest)); err != nil {
			return UpgradeDrillResult{}, err
		}
	case store.EnginePostgres:

	default:
		return UpgradeDrillResult{}, store.ErrEngineMismatch
	}
	source := receipt.Snapshot
	var result UpgradeDrillResult
	err = upgrade.WithLock(ctx, upgrade.Config{Engine: source.Engine, Path: request.Scratch.Path, DSN: request.Scratch.DSN}, func(session *upgrade.Session) error {
		if request.Scratch.Engine == store.EnginePostgres {
			if err := session.ApplyRestoreSchema(ctx, request.Plan, store.MigrationsFS, "migrations/postgres"); err != nil {
				return fmt.Errorf("initialize scratch schema: %w", err)
			}
			authority, err := session.ValidateRestoreDestination(ctx, authenticated, request.Plan)
			if err != nil {
				return fmt.Errorf("validate scratch destination: %w", err)
			}
			destination, err := store.OpenRestoreDestination(ctx, request.Scratch, authority, authenticated, request.Plan)
			if err != nil {
				return fmt.Errorf("open scratch destination: %w", err)
			}
			_, restoreErr := tx.RestoreUpgradeDestinationPostgres(ctx, destination, service.CompleteRestore(request.Now, manifest))
			if err := errors.Join(restoreErr, destination.Close()); err != nil {
				return fmt.Errorf("import scratch destination: %w", err)
			}
		}
		authority, err := session.ScratchAdmission(ctx, authenticated, request.Plan)
		if err != nil {
			return fmt.Errorf("admit restored scratch: %w", err)
		}
		scratch, err := store.OpenRecovery(ctx, request.Scratch, authority)
		if err != nil {
			return fmt.Errorf("open restored scratch: %w", err)
		}
		defer scratch.Close()
		recovery := &service.Recovery{DB: scratch}
		existing := &keyring.RecoveryStore{DB: scratch}
		if err := crypto.VerifyExistingHierarchy(ctx, existing, slices.Clone(request.RootKey)); err != nil {
			return fmt.Errorf("verify restored hierarchy: %w", err)
		}
		kr, err := crypto.LoadKeyring(ctx, existing, slices.Clone(request.RootKey))
		if err != nil {
			return fmt.Errorf("open restored keyring: %w", err)
		}
		result = UpgradeDrillResult{HierarchyReadable: true}
		readable, err := recovery.ProveValuesReadable(ctx, kr)
		switch {
		case err == nil && readable:
			result.SecretProof = "existing-secret-readable"
		case errors.Is(err, service.ErrNoSecretToProve):
			result.SecretProof = "authoritatively-no-secret"
		default:
			return errors.New("stored secret readability proof failed")
		}
		status, err := recovery.Status(ctx)
		if err != nil {
			return fmt.Errorf("read restored status: %w", err)
		}
		if len(status.Pending) == 0 {
			result.CredentialProof = "authoritatively-no-unreconciled-principal"
		} else if request.AutoCredentialProof {
			if err := recovery.AutoCredentialProof(ctx, kr); err != nil {
				return fmt.Errorf("automatic scratch credential proof: %w", err)
			}
			result.CredentialProof = "reconciled-minted-revoked"
		} else {
			if request.Principal == "" {
				return errors.New("populated identity inventory requires an explicit scratch reconciliation principal and project")
			}
			if _, err := recovery.Reconcile(ctx, request.Principal); err != nil {
				return fmt.Errorf("reconcile scratch principal: %w", err)
			}
			err := recovery.MintAndRevoke(ctx, kr, request.Principal, request.Scope)
			if err != nil {
				return errors.Join(errors.New("scratch credential mint and revoke proof failed"), err)
			}
			result.CredentialProof = "reconciled-minted-revoked"
		}
		return nil
	})
	if err != nil {
		return UpgradeDrillResult{}, err
	}
	now := request.Now.UTC().Truncate(time.Second)
	if now.Before(source.CreatedAt) {
		return UpgradeDrillResult{}, errors.New("attestation clock predates backup")
	}
	nonce, err := backupreceipt.NewNonce()
	if err != nil {
		return UpgradeDrillResult{}, err
	}
	bridges := request.Plan.BridgeDigests()
	slices.Sort(bridges)
	attestation := backupreceipt.Attestation{Format: backupreceipt.AttestationFormat, Authority: source.Authority, ReceiptSHA256: releaseidentity.Hash(request.Receipt), RouteSHA256: request.Plan.Digest(), BridgeSHA256: bridges, TargetIdentity: request.Plan.Target(), InstanceID: source.InstanceID, RestoreEpoch: source.RestoreEpoch, RecoveryIncarnation: source.RecoveryIncarnation, SourceGeneration: source.SourceGeneration, RouteGeneration: source.RouteGeneration, OperatorKeyID: request.Operator.KeyID(), IssuedAt: now, ExpiresAt: now.Add(request.Lifetime), Nonce: nonce}
	if err := attestation.Validate(); err != nil {
		return UpgradeDrillResult{}, err
	}
	result.Attestation, err = json.MarshalIndent(attestation, "", "  ")
	return result, err
}
