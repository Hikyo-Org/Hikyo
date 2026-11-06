package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
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

const retainedBackupFile = "retained-backup.json"

type retainedBackup struct {
	Format            string `json:"format"`
	EvidenceDirectory string `json:"evidence_directory"`
	CiphertextPath    string `json:"ciphertext_path"`
}

// retainLastBackup records the newest proven backup outside the journal. The journal
// of a preparing run names none, and an aborted run's journal is deleted, so the
// sweeps would otherwise leave the volume with no backup while a replacement is made.
func retainLastBackup(directory string, runtime hostupgrade.RuntimeEvidence) error {
	raw, err := json.Marshal(retainedBackup{Format: "hikyo.unattended-retained-backup/v1", EvidenceDirectory: runtime.EvidenceDirectory, CiphertextPath: runtime.CiphertextPath})
	if err != nil {
		return err
	}
	return writeAutomaticFile(filepath.Join(directory, retainedBackupFile), raw, 0600)
}

func readRetainedBackup(directory string) (retainedBackup, error) {
	var retained retainedBackup
	raw, err := readUnattendedPrivate(filepath.Join(directory, retainedBackupFile))
	if errors.Is(err, os.ErrNotExist) {
		return retained, nil
	}
	if err != nil || definitions.DecodeStrict(raw, &retained) != nil || retained.Format != "hikyo.unattended-retained-backup/v1" {
		return retainedBackup{}, errors.Join(err, errors.New("invalid retained backup record"))
	}
	return retained, nil
}

// sweepUnattendedAttempts removes the backup, evidence and restore scratch of every
// attempt the journal and the retained backup record do not reference. Each failed
// attempt otherwise leaves a full copy of the database on the state volume, and a
// full volume stops the source from starting. Only the coordinator lock holder runs
// it. The completed upgrade's backup stays until its replacement is journaled.
func sweepUnattendedAttempts(directory string, keep *automaticJournal) error {
	retained := map[string]bool{}
	last, err := readRetainedBackup(directory)
	if err != nil {
		return err
	}
	paths := []string{last.EvidenceDirectory, last.CiphertextPath}
	if keep != nil {
		paths = append(paths, keep.Runtime.EvidenceDirectory, keep.Runtime.CiphertextPath)
	}
	for _, path := range paths {
		if rel, err := filepath.Rel(directory, path); path != "" && err == nil && filepath.IsLocal(rel) {
			retained[strings.SplitN(rel, string(filepath.Separator), 2)[0]] = true
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var failed error
	for _, entry := range entries {
		name := entry.Name()
		generated := strings.HasPrefix(name, "backup-") || strings.HasPrefix(name, "evidence-") || strings.HasPrefix(name, "scratch-") || strings.HasPrefix(name, ".hikyo-upgrade-evidence-")
		if generated && !retained[name] && entry.IsDir() {
			failed = errors.Join(failed, os.RemoveAll(filepath.Join(directory, name)))
		}
	}
	return failed
}
