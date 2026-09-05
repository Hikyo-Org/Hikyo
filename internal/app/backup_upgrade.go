package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// TrustContext is supplied by the shared build/installation gate. No CLI flag
// can substitute a root, target identity, floor or current operator pin.
type TrustContext struct {
	BundleDirectory string
	Pinned          releasetrust.PinnedTrust
	Target          releaseidentity.Identity
	Floor           releaseidentity.SnapshotFloor
	OperatorPin     backupreceipt.PinnedOperator
}

func RunUpgradeBackup(ctx context.Context, cfg *config.Config, args []string, out io.Writer, trust TrustContext) error {
	if len(args) == 0 || cfg == nil || trust.Target.Validate() != nil || trust.Floor.Validate() != nil || !trust.OperatorPin.Valid() {
		return errors.New("upgrade backup requires shared installation trust context")
	}
	switch args[0] {
	case "upgrade-export":
		return runUpgradeExport(ctx, cfg, args[1:], out, trust)
	case "upgrade-drill":
		return runUpgradeDrill(ctx, args[1:], out, trust)
	default:
		return errors.New("usage: hikyo backup upgrade-export | upgrade-drill")
	}
}

func runUpgradeExport(ctx context.Context, cfg *config.Config, args []string, out io.Writer, trust TrustContext) error {
	fs := flag.NewFlagSet("backup upgrade-export", flag.ContinueOnError)
	fs.SetOutput(out)
	bundleDir := fs.String("bundle", trust.BundleDirectory, "verified offline release artifact directory")
	dir := fs.String("out", cfg.BackupDir, "public ciphertext and receipt destination")
	var recipients recipientList
	fs.Var(&recipients, "recipient", "public age X25519 recipient; repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *bundleDir == "" || *dir == "" {
		return errors.New("upgrade-export requires --bundle DIR and --out DIR")
	}
	options, err := exportOptions(cfg, recipients, "")
	if err != nil {
		return err
	}
	if _, err := options.UpgradeRecipientFingerprints(); err != nil {
		return err
	}
	bundle, err := upgradebundle.Load(ctx, *bundleDir, trust.Pinned, trust.Floor)
	if err != nil {
		return err
	}
	database := storeConfig(cfg)
	sourceConfig := upgrade.Config{Engine: releaseidentity.Engine(database.Engine), Path: database.Path, DSN: database.DSN}
	return upgrade.WithLock(ctx, sourceConfig, func(session *upgrade.Session) error {
		plan, err := inspectBackupPlan(ctx, sourceConfig, bundle, trust.Target, trust.OperatorPin.InstanceID())
		if err != nil {
			return err
		}
		authority, err := session.PrepareExport(ctx, plan)
		if err != nil {
			return err
		}
		db, err := store.OpenPreparation(ctx, database, authority)
		if err != nil {
			return err
		}
		defer db.Close()
		var proposal *backupreceipt.LegacyProposal
		if plan.Source().Genesis == releaseidentity.LegacyGenesisV1 {
			value, err := backupreceipt.NewLegacyProposal()
			if err != nil {
				return err
			}
			proposal = &value
		}
		exported, err := service.ExportPreparedUpgrade(ctx, db, options, *dir, plan, proposal)
		if err != nil {
			return err
		}
		if exported.Receipt == nil || exported.Receipt.Snapshot.InstanceID != trust.OperatorPin.InstanceID() {
			return errors.New("exported source differs from current installation operator pin")
		}
		_, err = fmt.Fprintf(out, "ciphertext: %s\nreceipt: %s\n", exported.Path, exported.ReceiptPath)
		return err
	})
}

func inspectBackupPlan(ctx context.Context, cfg upgrade.Config, bundle upgradebundle.Bundle, target releaseidentity.Identity, instance string) (upgradecompat.Plan, error) {
	for _, candidate := range bundle.Sources(cfg.Engine) {
		inspected, err := upgrade.InspectInstalled(ctx, cfg, candidate.Migrations)
		if err != nil {
			continue
		}
		if inspected.Source != candidate.Identity || inspected.SchemaDigest != candidate.SchemaSHA256 || inspected.InstanceID != instance {
			continue
		}
		return bundle.Plan(upgradecompat.InstalledSource{Identity: inspected.Source, Migrations: candidate.Migrations, SchemaSHA256: inspected.SchemaDigest}, target)
	}
	return upgradecompat.Plan{}, errors.New("actual source is absent from authenticated migration and schema candidates")
}

func runUpgradeDrill(ctx context.Context, args []string, out io.Writer, trust TrustContext) error {
	fs := flag.NewFlagSet("backup upgrade-drill", flag.ContinueOnError)
	fs.SetOutput(out)
	bundleDir := fs.String("bundle", trust.BundleDirectory, "verified offline release artifact directory")
	source := fs.String("from", "", "exact encrypted upgrade archive")
	receiptFile := fs.String("receipt", "", "public receipt for that archive")
	identityFile := fs.String("identity-file", "", "operator-held age X25519 identity")
	rootFile := fs.String("root-key-file", "", "separately escrowed source root key")
	sqlitePath := fs.String("target-sqlite", "", "empty scratch SQLite path")
	postgresFile := fs.String("target-postgres-dsn-file", "", "file selecting an empty scratch PostgreSQL database")
	principal := fs.String("principal", "", "one scratch reconciliation principal, required when principals exist")
	project := fs.String("project", "", "scratch reconciliation ORG/PROJECT")
	dir := fs.String("out", "", "public signed attestation destination")
	signer := fs.String("cosign", "cosign", "local maintained cosign executable")
	signingKey := fs.String("signing-key", "", "operator-held cosign private key file")
	lifetime := fs.Duration("valid-for", 24*time.Hour, "attestation validity, at most 24h")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *bundleDir == "" || *source == "" || *receiptFile == "" || *identityFile == "" || *rootFile == "" || *dir == "" || *signingKey == "" || (*sqlitePath == "") == (*postgresFile == "") {
		return errors.New("upgrade-drill requires bundle, archive, receipt, separate custody files, signer, output and exactly one empty scratch target")
	}
	bundle, err := upgradebundle.Load(ctx, *bundleDir, trust.Pinned, trust.Floor)
	if err != nil {
		return err
	}
	rawReceipt, err := readUpgradePublicFile(*receiptFile, backupreceipt.MaxArtifactBytes)
	if err != nil {
		return err
	}
	receipt, err := backupreceipt.ParseReceipt(rawReceipt)
	if err != nil {
		return err
	}
	var plan upgradecompat.Plan
	for _, candidate := range bundle.Sources(receipt.Snapshot.Engine) {
		digest, err := candidate.Migrations.Digest()
		if err != nil {
			return err
		}
		if candidate.Identity == receipt.Snapshot.SourceIdentity && candidate.SchemaSHA256 == receipt.Snapshot.SourceSchemaSHA256 && digest == receipt.Snapshot.MigrationSHA256 {
			plan, err = bundle.Plan(candidate, trust.Target)
			if err != nil {
				return err
			}
			break
		}
	}
	if !plan.Valid() {
		return errors.New("receipt source absent from authenticated bundle")
	}
	pinned, err := backupreceipt.PinCiphertext(ctx, *source, "")
	if err != nil {
		return err
	}
	defer pinned.Close()
	unlock, err := restoreUnlock(*identityFile, "")
	if err != nil {
		return err
	}
	root, err := crypto.ReadRootKey(*rootFile, "")
	if err != nil {
		return err
	}
	defer crypto.Zero(root)
	scratch, err := drillTargetConfig(*sqlitePath, *postgresFile)
	if err != nil {
		return err
	}
	var scope domain.Scope
	if *project != "" {
		scope, err = parseDrillProject(*project)
		if err != nil {
			return err
		}
	}
	result, err := DrillUpgrade(ctx, UpgradeDrillRequest{Scratch: scratch, Ciphertext: pinned, Receipt: rawReceipt, Plan: plan, Operator: trust.OperatorPin, Unlock: unlock, RootKey: root, Principal: domain.PrincipalID(*principal), Scope: scope, Now: time.Now().UTC(), Lifetime: *lifetime})
	if err != nil {
		return err
	}
	signature, err := signUpgradeStatement(ctx, *signer, *signingKey, result.Attestation, trust.OperatorPin)
	if err != nil {
		return err
	}
	statementPath, signaturePath, err := publishUpgradeAttestation(*dir, result.Attestation, signature)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "hierarchy: verified existing wrappers\nsecret: %s\ncredential: %s\nattestation: %s\nsignature: %s\n", result.SecretProof, result.CredentialProof, statementPath, signaturePath)
	return err
}

func parseDrillProject(value string) (domain.Scope, error) {
	org, project, ok := strings.Cut(value, "/")
	if !ok || org == "" || project == "" || strings.Contains(project, "/") {
		return domain.Scope{}, errors.New("project must be ORG/PROJECT")
	}
	return domain.Scope{Org: domain.OrgID(org), Project: domain.ProjectID(project)}, nil
}

func readUpgradePublicFile(path string, maximum int64) ([]byte, error) {
	return backupreceipt.ReadPublicArtifact(path, maximum)
}

func signUpgradeStatement(ctx context.Context, executable, keyFile string, statement []byte, pin backupreceipt.PinnedOperator) ([]byte, error) {
	a, err := backupreceipt.ParseAttestation(statement)
	if err != nil || a.OperatorKeyID != pin.KeyID() || a.InstanceID != pin.InstanceID() {
		return nil, errors.New("unsigned statement differs from current installation pin")
	}
	info, err := os.Stat(keyFile)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > backupreceipt.MaxSignatureBytes || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("operator signing key file unavailable")
	}
	work, err := os.MkdirTemp("", "hikyo-upgrade-sign-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	payload := filepath.Join(work, "attestation.json")
	bundle := filepath.Join(work, "attestation.sigstore.json")
	if err := os.WriteFile(payload, statement, 0600); err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, executable, "sign-blob", "--yes", "--new-bundle-format=false", "--tlog-upload=false", "--use-signing-config=false", "--key", keyFile, "--bundle", bundle, payload)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, errors.New("local operator signing failed")
	}
	signature, err := readUpgradePublicFile(bundle, backupreceipt.MaxSignatureBytes)
	if err != nil {
		return nil, err
	}
	if err := backupreceipt.CheckOperatorSignature(pin, signature, statement); err != nil {
		return nil, err
	}
	if !time.Now().Before(a.ExpiresAt) || a.IssuedAt.After(time.Now()) {
		return nil, errors.New("operator signature completed outside attestation validity")
	}
	return signature, nil
}

func publishUpgradeAttestation(directory string, statement, signature []byte) (string, string, error) {
	a, err := backupreceipt.ParseAttestation(statement)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", "", err
	}
	ancestry, err := filedurability.DirectoryAncestry(directory)
	if err != nil {
		return "", "", err
	}
	name := "upgrade-attestation-" + string(a.Nonce)
	paths := []string{filepath.Join(directory, name+".json"), filepath.Join(directory, name+".sigstore.json")}
	for i, raw := range [][]byte{statement, signature} {
		file, err := os.CreateTemp(directory, ".upgrade-attestation-*.partial")
		if err != nil {
			return "", "", err
		}
		partial := file.Name()
		writeErr := func() error {
			defer file.Close()
			if err := file.Chmod(0600); err != nil {
				return err
			}
			if _, err := file.Write(raw); err != nil {
				return err
			}
			if err := file.Sync(); err != nil {
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
			return os.Link(partial, paths[i])
		}()
		cleanupErr := os.Remove(partial)
		if err := errors.Join(writeErr, cleanupErr); err != nil {
			return "", "", err
		}
		for _, path := range ancestry {
			if err := filedurability.SyncDirectory(path); err != nil {
				return "", "", errors.New("public attestation publication durability unconfirmed")
			}
		}
	}
	return paths[0], paths[1], nil
}
