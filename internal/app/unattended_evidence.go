package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
)

func prepareUnattendedEvidence(ctx context.Context, cfg *config.Config, options UnattendedUpgradeOptions, directory string, route automaticRoute, vault *upgradecustody.Vault, journal *automaticJournal, scratch store.Config) error {
	if options.Progress != nil {
		options.Progress("backup")
	}
	output, err := os.MkdirTemp(directory, "backup-")
	if err != nil {
		return err
	}
	database := upgrade.Config{Engine: releaseidentity.Engine(cfg.Store.Engine), Path: cfg.Store.Path, DSN: cfg.Store.DSN}
	var exported service.ExportResult
	err = upgrade.WithLock(ctx, database, func(session *upgrade.Session) error {
		authority, err := session.PrepareExport(ctx, route.Plan)
		if err != nil {
			return err
		}
		prepared, err := store.OpenPreparation(ctx, storeConfig(cfg), authority)
		if err != nil {
			return err
		}
		defer prepared.Close()
		exported, err = service.ExportPreparedUpgrade(ctx, prepared, backup.Options{Recipients: []string{vault.Recipient()}}, output, route.Plan, nil)
		return err
	})
	if err != nil {
		return err
	}
	receipt, err := backupreceipt.ReadPublicArtifact(exported.ReceiptPath, backupreceipt.MaxArtifactBytes)
	if err != nil {
		return err
	}
	ciphertext, err := backupreceipt.PinCiphertext(ctx, exported.Path, directory)
	if err != nil {
		return err
	}
	defer ciphertext.Close()
	if options.Progress != nil {
		options.Progress("restore-check")
	}
	result, err := DrillUpgrade(ctx, UpgradeDrillRequest{Scratch: scratch, Ciphertext: ciphertext, Receipt: receipt, Plan: route.Plan, Operator: vault.Pin(), Unlock: vault.BackupUnlock(), RootKey: vault.RootKey(), AutoCredentialProof: true, Now: time.Now(), Lifetime: backupreceipt.MaxAttestationLifetime})
	if err != nil {
		return err
	}
	signature, err := vault.SignAttestation(result.Attestation, time.Now())
	if err != nil {
		return err
	}
	evidence, err := os.MkdirTemp(directory, "evidence-")
	if err != nil {
		return err
	}
	for name, raw := range map[string][]byte{"receipt.json": receipt, "attestation.json": result.Attestation, "attestation.sigstore.json": signature} {
		if err := writeAutomaticFile(filepath.Join(evidence, name), raw, 0644); err != nil {
			return err
		}
	}
	public, err := backupreceipt.ReadPublicArtifact(cfg.Upgrade.OperatorPublicKeyFile, 1<<20)
	if err != nil {
		return err
	}
	if !bytes.Equal(vault.PublicKey(), public) {
		return errors.New("enrolled operator changed during recovery proof")
	}
	journal.Runtime.EvidenceDirectory, journal.Runtime.CiphertextPath = evidence, exported.Path
	return nil
}
