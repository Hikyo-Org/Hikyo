package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

func configuredUpgradeSource(cfg *config.Config) configrollout.UpgradeCustodySource {
	u := cfg.Upgrade
	return configrollout.UpgradeCustodySource{BundleDirectory: u.BundleDirectory, StateDirectory: u.StateDirectory, EvidenceDirectory: u.EvidenceDirectory, CiphertextPath: u.CiphertextPath, OperatorPublicKeyFile: u.OperatorPublicKeyFile, TargetManifestSHA256: u.TargetManifestSHA256, LegacyWritersStopped: u.LegacyWritersStopped}
}

func selectedUpgradeConfiguration(cfg *config.Config, source configrollout.UpgradeCustodySource) *config.Config {
	next := *cfg
	next.Upgrade = config.UpgradeConfiguration{BundleDirectory: source.BundleDirectory, StateDirectory: source.StateDirectory, EvidenceDirectory: source.EvidenceDirectory, CiphertextPath: source.CiphertextPath, OperatorPublicKeyFile: source.OperatorPublicKeyFile, TargetManifestSHA256: source.TargetManifestSHA256, LegacyWritersStopped: source.LegacyWritersStopped, OperatorInstanceID: cfg.Upgrade.OperatorInstanceID}
	return &next
}

// resolveSelectedUpgrade runs before gate construction. Enrollment and the
// downward projection resolve bootstrap inputs without opening the managed
// project or constructing a provider, signer, network client or runtime DB.
func resolveSelectedUpgrade(ctx context.Context, cfg *config.Config, selectionDirectory string) (*config.Config, error) {
	if cfg.ConfigRolloutEnrollment == "" {
		return cfg, nil
	}
	sources, err := loadEnrolledRootSources(cfg, deploymentSourcesDirectory)
	if err != nil {
		return nil, err
	}
	if len(sources.enrollment.Target.UpgradeSources) == 0 {
		return cfg, nil
	}
	if cfg.Dev {
		return nil, errors.New("enrolled upgrade custody requires the production gate; development material is separately owned")
	}
	raw, err := readDeploymentFile(filepath.Join(selectionDirectory, "upgrade-alias"), false)
	if err != nil {
		return nil, errors.New("upgrade custody selection unavailable")
	}
	alias := strings.TrimSpace(string(raw))
	source, ok := sources.enrollment.Target.UpgradeSources[alias]
	if !ok || !source.Valid() {
		return nil, errors.New("upgrade custody selection is not enrolled")
	}
	if configuredUpgradeSource(cfg) != source {
		return nil, errors.New("upgrade startup inputs differ from enrolled selection; install a matching upgradeSources profile and initialUpgradeSource, or restore the exact rollout tuple")
	}
	next := selectedUpgradeConfiguration(cfg, source)
	next.UpgradeSource = alias
	raw, err = readDeploymentFile(filepath.Join(selectionDirectory, "upgrade-proof"), false)
	if err != nil {
		return nil, errors.New("upgrade material proof unavailable")
	}
	proof := strings.TrimSpace(string(raw))
	if proof == "" {
		if alias != sources.enrollment.Target.InitialUpgradeSource {
			return nil, errors.New("an applied upgrade selection requires its authorized material proof")
		}
		return next, nil
	} // Initial tuple: the actual gate owns first admission.
	if releaseidentity.Digest(proof).Validate() != nil {
		return nil, errors.New("invalid upgrade material proof")
	}
	installed, err := upgradegate.InspectCustodyDirectory(source.StateDirectory)
	if err != nil {
		return nil, err
	}
	checked, err := inspectUpgradeSource(ctx, next, installed, false)
	if err != nil {
		return nil, err
	}
	if checked.MaterialDigest != proof {
		return nil, errors.New("upgrade artifacts changed after the authorized rollout decision")
	}
	next.UpgradeMaterialDigest = proof
	return next, nil
}

// inspectUpgradeSource deliberately does not call upgradeRequest: that path
// creates development state and ciphertext copies as part of actual execution.
func inspectUpgradeSource(ctx context.Context, cfg *config.Config, installed os.FileInfo, preparing bool) (upgradegate.ConfigurationProof, error) {
	if cfg.Dev {
		return upgradegate.ConfigurationProof{}, configrollout.ErrUnsupported
	}
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		return upgradegate.ConfigurationProof{}, err
	}
	public, err := backupreceipt.ReadPublicArtifact(cfg.Upgrade.OperatorPublicKeyFile, 1<<20)
	if err != nil {
		return upgradegate.ConfigurationProof{}, err
	}
	request := upgradegate.Request{Store: upgrade.Config{Engine: releaseidentity.Engine(cfg.Store.Engine), Path: cfg.Store.Path, DSN: cfg.Store.DSN}, Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(cfg.Store.Engine), BundleDirectory: cfg.Upgrade.BundleDirectory, StateDirectory: cfg.Upgrade.StateDirectory, InitialOperatorPublicKey: public, Pinned: pinned, LegacyWritersStopped: cfg.Upgrade.LegacyWritersStopped}
	if cfg.Upgrade.EvidenceDirectory != "" {
		for _, item := range []struct {
			name  string
			value *[]byte
		}{{"receipt.json", &request.Evidence.Receipt}, {"attestation.json", &request.Evidence.Attestation}, {"attestation.sigstore.json", &request.Evidence.Signature}} {
			*item.value, err = backupreceipt.ReadPublicArtifact(filepath.Join(cfg.Upgrade.EvidenceDirectory, item.name), 1<<20)
			if err != nil {
				return upgradegate.ConfigurationProof{}, err
			}
		}
	}
	input := upgradegate.ConfigurationPreflight{Request: request, InstalledCustody: installed, TargetManifest: cfg.Upgrade.TargetManifestSHA256, CiphertextPath: cfg.Upgrade.CiphertextPath}
	var proof upgradegate.ConfigurationProof
	if preparing {
		proof, err = upgradegate.PreflightConfiguration(ctx, input)
	} else {
		proof, err = upgradegate.CheckStartupConfiguration(ctx, input)
	}
	if err != nil {
		return upgradegate.ConfigurationProof{}, err
	}
	proof.MaterialDigest = string(releaseidentity.Hash([]byte("hikyo.upgrade-selection.v1\x00" + configrollout.UpgradeSourceDigest(configuredUpgradeSource(cfg)) + "\x00" + proof.MaterialDigest)))
	return proof, nil
}
