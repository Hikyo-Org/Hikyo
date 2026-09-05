package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// ExportUpgrade publishes exact ciphertext first and its public receipt last.
// It accepts only public X25519 recipients and never loads private custody.
func (s *Backup) ExportUpgrade(ctx context.Context, dir string, plan upgradecompat.Plan, proposal *backupreceipt.LegacyProposal) (ExportResult, error) {
	if s.DB == nil || !plan.Valid() {
		return ExportResult{}, errors.New("upgrade export requires database and authenticated route")
	}
	request, err := s.upgradeRequest(plan, proposal)
	if err != nil {
		return ExportResult{}, err
	}
	return s.export(ctx, dir, true, func(ctx context.Context, writer io.Writer, work string) (store.Manifest, error) {
		return store.ExportUpgrade(ctx, s.DB, writer, work, request)
	})
}

// ExportPreparedUpgrade accepts only the narrow, live-session preparation
// capability. It cannot construct a runtime DB or publish audit/domain writes.
func ExportPreparedUpgrade(ctx context.Context, db *store.PreparationDB, options backup.Options, dir string, plan upgradecompat.Plan, proposal *backupreceipt.LegacyProposal) (ExportResult, error) {
	if db == nil || !plan.Valid() {
		return ExportResult{}, errors.New("upgrade export requires owned preparation and authenticated route")
	}
	publisher := &Backup{Options: options}
	request, err := publisher.upgradeRequest(plan, proposal)
	if err != nil {
		return ExportResult{}, err
	}
	return publisher.export(ctx, dir, true, func(ctx context.Context, writer io.Writer, work string) (store.Manifest, error) {
		return db.ExportUpgrade(ctx, writer, work, request)
	})
}

func (s *Backup) upgradeRequest(plan upgradecompat.Plan, proposal *backupreceipt.LegacyProposal) (store.UpgradeExportRequest, error) {
	recipients, err := s.Options.UpgradeRecipientFingerprints()
	if err != nil {
		return store.UpgradeExportRequest{}, err
	}
	id, err := backupreceipt.NewNonce()
	if err != nil {
		return store.UpgradeExportRequest{}, err
	}
	return store.UpgradeExportRequest{Plan: plan, Recipients: recipients, LegacyProposal: proposal, BackupID: id, CreatedAt: s.now().UTC().Truncate(time.Second)}, nil
}

func (s *Backup) publishUpgradeReceipt(ciphertext string, receipt backupreceipt.Receipt, syncPaths []string) (string, error) {
	if err := receipt.Validate(); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp(filepath.Dir(ciphertext), ".hikyo-receipt-*.partial")
	if err != nil {
		return "", err
	}
	partial := file.Name()
	defer os.Remove(partial)
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return "", err
	}
	if _, err := file.Write(raw); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	final := ciphertext + ".receipt.json"
	if err := os.Link(partial, final); err != nil {
		return "", fmt.Errorf("publish upgrade receipt without overwrite: %w", err)
	}
	syncDirectory := s.syncDirectory
	if syncDirectory == nil {
		syncDirectory = filedurability.SyncDirectory
	}
	for _, path := range syncPaths {
		if err := syncDirectory(path); err != nil {
			return "", fmt.Errorf("%w: upgrade receipt %s: %w", ErrBackupDurabilityUnconfirmed, final, err)
		}
	}
	return final, nil
}
