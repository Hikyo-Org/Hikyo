package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securityfeed"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

type nativeSecurityAuthorization struct {
	Route        releaseidentity.Digest
	Until        time.Time
	PolicyFile   string
	PolicySHA256 releaseidentity.Digest
	Source       *nativeSecuritySourceGuard
	// Rollback asks the target's executable to put the fenced snapshot back, not to update.
	Rollback bool
}

type nativeSecuritySourceGuard struct {
	Source       securitymaintenance.Observation `json:"source"`
	HostSHA256   releaseidentity.Digest          `json:"host_sha256"`
	BinarySHA256 releaseidentity.Digest          `json:"binary_sha256"`
}

func nativeSecurityHostFingerprint(configuration string, c hostupgrade.Config) (releaseidentity.Digest, error) {
	// Hash file bytes: source and target builds re-marshal parsed structs
	// differently. Retain only the hash. The environment can contain operator
	// secrets and must not reach command arguments or maintenance status.
	files := make([][]byte, 2)
	for i, path := range []string{configuration, c.EnvironmentFile} {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		files[i] = raw
	}
	raw, err := json.Marshal(files)
	return releaseidentity.Hash(raw), err
}

func parseAutomaticSecuritySource(raw string, supplied bool, expected *releaseidentity.Identity) (*nativeSecuritySourceGuard, error) {
	if !supplied {
		return nil, nil
	}
	var guard nativeSecuritySourceGuard
	if expected == nil || len(raw) == 0 || len(raw) > 16384 || definitions.DecodeStrict([]byte(raw), &guard) != nil || !guard.Source.Healthy() || guard.Source.Engine != releaseidentity.SQLite || guard.HostSHA256.Validate() != nil || guard.BinarySHA256.Validate() != nil {
		return nil, errors.New("--maintenance-source requires an exact release and complete original native source guard")
	}
	return &guard, nil
}

func appendNativeSecuritySource(args []string, guard *nativeSecuritySourceGuard, expected *releaseidentity.Identity) ([]string, error) {
	if guard == nil {
		return args, nil
	}
	raw, err := json.Marshal(guard)
	if err != nil {
		return nil, err
	}
	if _, err := parseAutomaticSecuritySource(string(raw), true, expected); err != nil {
		return nil, err
	}
	return append(args, "--maintenance-source", string(raw)), nil
}

func checkNativeSecuritySourceGuard(guard nativeSecuritySourceGuard, configuration string, c hostupgrade.Config, current securitymaintenance.Observation) error {
	digest, err := nativeSecurityHostFingerprint(configuration, c)
	if err != nil || digest != guard.HostSHA256 || !sameNativeSecurityObservation(guard.Source, current) {
		return errors.Join(securitymaintenance.ErrNotStarted, err, errors.New("native deployment authority or enrolled source changed during final preparation"))
	}
	return nil
}

func verifyNativeSecurityMutation(ctx context.Context, path string, host *hostupgrade.Host, database upgrade.Config, guard nativeSecuritySourceGuard) error {
	c, err := hostupgrade.LoadConfig(path)
	if err != nil || c != host.Config() {
		return errors.Join(securitymaintenance.ErrNotStarted, err, errors.New("native host configuration changed before maintenance"))
	}
	if err := host.Preflight(ctx); err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	state, err := upgrade.InspectControl(ctx, database)
	if err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	current, err := securityControlObservation(state)
	if err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	current.Engine = database.Engine
	if err := checkNativeSecuritySourceGuard(guard, path, c, current); err != nil {
		return err
	}
	if err := host.CheckRunning(ctx, string(guard.BinarySHA256)); err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	return nil
}

func verifySecurityPolicyFingerprint(path string, expected releaseidentity.Digest) error {
	// Equal bytes are what the source build already validated. A target build
	// must not reinterpret them with its own configuration schema.
	raw, err := securityfeed.ReadPublicFile(path, 65536)
	if err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	if releaseidentity.Hash(raw) != expected {
		return errors.Join(securitymaintenance.ErrNotStarted, errors.New("local maintenance policy or deployment authority changed during staging; recheck before application"))
	}
	return nil
}

func parseAutomaticSecurityPolicy(path, digest string, supplied bool, expected *releaseidentity.Identity) (string, releaseidentity.Digest, error) {
	if !supplied {
		return "", "", nil
	}
	if expected == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || releaseidentity.Digest(digest).Validate() != nil {
		return "", "", errors.New("maintenance policy guard requires --release-identity and a complete local policy path/hash pair")
	}
	return path, releaseidentity.Digest(digest), nil
}
