package securityfeed

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"github.com/gofrs/flock"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/trustedmetadata"
)

// RoleKeys contains public authorization only. Private keys remain with their
// role owners and are supplied separately when a publication is signed.
type RoleKeys struct {
	PublicKeys []crypto.PublicKey
	Threshold  int
}

type RootConfig struct {
	Version      uint64
	Expires      time.Time
	Roles        map[string]RoleKeys
	Signers      []signature.Signer
	PreviousRoot []byte
}

// CreateRoot creates a self-authenticated TUF root, or a consecutive rotation
// authenticated by both the old and new root thresholds. It never generates
// keys or changes Hikyo's release-signing trust.
func CreateRoot(c RootConfig) ([]byte, error) {
	if c.Version == 0 || c.Version >= math.MaxInt64 || !c.Expires.After(time.Now()) || len(c.Roles) != 4 || len(c.PreviousRoot) > MaxDocumentBytes || c.Version > 1 && len(c.PreviousRoot) == 0 {
		return nil, errors.New("feed root requires a valid version, future expiry, four explicit roles and rotation history")
	}
	root := metadata.Root(c.Expires.UTC())
	root.Signed.Version = int64(c.Version)
	root.Signed.ConsistentSnapshot = true
	for _, role := range metadata.TOP_LEVEL_ROLE_NAMES {
		keys, ok := c.Roles[role]
		if !ok || keys.Threshold < 1 || keys.Threshold > len(keys.PublicKeys) || len(keys.PublicKeys) > 16 {
			return nil, errors.New("feed root role requires a bounded satisfiable threshold")
		}
		for _, public := range keys.PublicKeys {
			key, err := metadata.KeyFromPublicKey(public)
			if err != nil {
				return nil, err
			}
			if err := root.Signed.AddKey(key, role); err != nil {
				return nil, err
			}
		}
		root.Signed.Roles[role].Threshold = keys.Threshold
	}
	if err := validateRoot(root); err != nil {
		return nil, err
	}
	if err := sign(root, c.Signers); err != nil {
		return nil, err
	}
	if err := root.VerifyDelegate(metadata.ROOT, root); err != nil {
		return nil, fmt.Errorf("feed root threshold: %w", err)
	}
	raw, err := root.ToBytes(false)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxDocumentBytes {
		return nil, errors.New("feed root exceeds its byte bound")
	}
	if len(c.PreviousRoot) > 0 {
		previous, err := trustedmetadata.New(c.PreviousRoot)
		if err != nil {
			return nil, err
		}
		if _, err := previous.UpdateRoot(raw); err != nil {
			return nil, fmt.Errorf("feed root rotation: %w", err)
		}
	}
	return raw, nil
}

// Expirations are explicit operator decisions. The earliest role expiration
// bounds a client's catalog eligibility, even when another role lives longer.
type Expirations struct {
	Targets   time.Time
	Snapshot  time.Time
	Timestamp time.Time
}

type PublishConfig struct {
	Root              []byte
	PreviousRoot      []byte
	Catalog           []byte
	PreviousTimestamp []byte
	Version           uint64
	Expires           Expirations
	Signers           map[string][]signature.Signer
}

// Publication is constructed only by verifying all role signatures and exact
// metadata hash bindings. Files returns copies; no private key is retained.
type Publication struct {
	files   map[string][]byte
	version uint64
	// baseline is the timestamp this publication was built from; nil for the first.
	baseline []byte
}

func BuildPublication(c PublishConfig) (*Publication, error) {
	if c.Version == 0 || c.Version >= math.MaxInt64 || len(c.Root) == 0 || len(c.Root) > MaxDocumentBytes || len(c.PreviousRoot) > MaxDocumentBytes || len(c.PreviousTimestamp) > MaxDocumentBytes || len(c.Catalog) == 0 || len(c.Catalog) > MaxDocumentBytes {
		return nil, errors.New("feed publication requires bounded root/catalog bytes and a positive version")
	}
	c.Root, c.Catalog = slices.Clone(c.Root), slices.Clone(c.Catalog)
	trusted, err := trustedmetadata.New(c.Root)
	if err != nil {
		return nil, err
	}
	if err := validateRoot(trusted.Root); err != nil {
		return nil, err
	}
	now := time.Now()
	if trusted.Root.Signed.IsExpired(now) || !c.Expires.Targets.After(now) || !c.Expires.Snapshot.After(now) || !c.Expires.Timestamp.After(now) || c.Expires.Timestamp.After(c.Expires.Snapshot) || c.Expires.Snapshot.After(c.Expires.Targets) || c.Expires.Targets.After(trusted.Root.Signed.Expires) {
		return nil, errors.New("feed role expiry must be future and ordered timestamp <= snapshot <= targets <= root")
	}
	if len(c.PreviousTimestamp) == 0 {
		if c.Version != 1 {
			return nil, errors.New("publishing a later feed version requires the previous authenticated timestamp")
		}
	} else {
		previous, err := metadata.Timestamp().FromBytes(c.PreviousTimestamp)
		if err != nil || previous.Signed.Version <= 0 || uint64(previous.Signed.Version) >= c.Version {
			return nil, errors.New("previous feed timestamp is unauthenticated or version does not increase")
		}
		previousAuthority := trusted.Root
		if len(c.PreviousRoot) != 0 {
			old, err := trustedmetadata.New(c.PreviousRoot)
			if err != nil {
				return nil, err
			}
			previousAuthority = old.Root
			if _, err := old.UpdateRoot(c.Root); err != nil {
				return nil, fmt.Errorf("publisher root rotation history: %w", err)
			}
		}
		if previousAuthority.VerifyDelegate(metadata.TIMESTAMP, previous) != nil {
			return nil, errors.New("previous feed timestamp is unauthenticated")
		}
	}
	target, err := metadata.TargetFile().FromBytes(CatalogPath, c.Catalog, "sha256")
	if err != nil {
		return nil, err
	}
	targets := metadata.Targets(c.Expires.Targets.UTC())
	targets.Signed.Version = int64(c.Version)
	targets.Signed.Targets[CatalogPath] = target
	if err := sign(targets, c.Signers[metadata.TARGETS]); err != nil {
		return nil, err
	}
	if err := trusted.Root.VerifyDelegate(metadata.TARGETS, targets); err != nil {
		return nil, fmt.Errorf("feed targets threshold: %w", err)
	}
	targetsRaw, err := targets.ToBytes(false)
	if err != nil {
		return nil, err
	}
	snapshotRaw, timestampRaw, err := signLiveness(trusted, targetsRaw, int64(c.Version), c.Version, c.Expires.Snapshot, c.Expires.Timestamp, c.Signers)
	if err != nil {
		return nil, err
	}
	version := strconv.FormatUint(c.Version, 10)
	rootVersion := strconv.FormatInt(trusted.Root.Signed.Version, 10)
	return &Publication{version: c.Version, baseline: slices.Clone(c.PreviousTimestamp), files: map[string][]byte{
		"metadata/" + rootVersion + ".root.json":                  c.Root,
		"metadata/" + version + ".targets.json":                   targetsRaw,
		"metadata/" + version + ".snapshot.json":                  snapshotRaw,
		"metadata/timestamp.json":                                 timestampRaw,
		"targets/security/" + digest(c.Catalog) + ".catalog.json": c.Catalog,
	}}, nil
}

// signLiveness signs the online roles that bind existing targets metadata.
func signLiveness(trusted *trustedmetadata.TrustedMetadata, targetsRaw []byte, targetsVersion int64, version uint64, snapshotExpires, timestampExpires time.Time, signers map[string][]signature.Signer) (snapshotRaw, timestampRaw []byte, err error) {
	snapshot := metadata.Snapshot(snapshotExpires.UTC())
	snapshot.Signed.Version = int64(version)
	snapshot.Signed.Meta["targets.json"], err = metadataBinding(targetsRaw, targetsVersion)
	if err != nil {
		return nil, nil, err
	}
	if err := sign(snapshot, signers[metadata.SNAPSHOT]); err != nil {
		return nil, nil, err
	}
	if err := trusted.Root.VerifyDelegate(metadata.SNAPSHOT, snapshot); err != nil {
		return nil, nil, fmt.Errorf("feed snapshot threshold: %w", err)
	}
	snapshotRaw, err = snapshot.ToBytes(false)
	if err != nil {
		return nil, nil, err
	}
	timestamp := metadata.Timestamp(timestampExpires.UTC())
	timestamp.Signed.Version = int64(version)
	timestamp.Signed.Meta["snapshot.json"], err = metadataBinding(snapshotRaw, int64(version))
	if err != nil {
		return nil, nil, err
	}
	if err := sign(timestamp, signers[metadata.TIMESTAMP]); err != nil {
		return nil, nil, err
	}
	if err := trusted.Root.VerifyDelegate(metadata.TIMESTAMP, timestamp); err != nil {
		return nil, nil, fmt.Errorf("feed timestamp threshold: %w", err)
	}
	timestampRaw, err = timestamp.ToBytes(false)
	return snapshotRaw, timestampRaw, err
}

// Published is the current content of a prepared repository, authenticated
// under the supplied root. Expiry is deliberately not checked here, because a
// lapsed feed must remain recoverable by its online roles.
type Published struct {
	Catalog        []byte
	TargetsExpires time.Time
	Version        uint64

	targetsRaw     []byte
	targetsVersion int64
	timestampRaw   []byte
}

// ReadPublished authenticates the existing timestamp, snapshot, targets and
// catalog chain. It reports false only when the repository has no timestamp.
func ReadPublished(directory string, root, previousRoot []byte) (Published, bool, error) {
	var published Published
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return published, false, errors.New("feed repository requires an absolute canonical directory")
	}
	if err := checkAncestors(directory, ""); err != nil {
		return published, false, err
	}
	timestampRaw, err := readRegular(filepath.Join(directory, "metadata/timestamp.json"), MaxDocumentBytes, false)
	if errors.Is(err, os.ErrNotExist) {
		return published, false, nil
	}
	if err != nil {
		return published, false, fmt.Errorf("read published feed timestamp: %w", err)
	}
	authorityRaw := root
	if len(previousRoot) != 0 {
		authorityRaw = previousRoot
	}
	trusted, err := trustedmetadata.New(authorityRaw)
	if err != nil {
		return published, false, err
	}
	authority := trusted.Root
	if len(previousRoot) != 0 {
		if _, err := trusted.UpdateRoot(root); err != nil {
			return published, false, fmt.Errorf("publisher root rotation history: %w", err)
		}
	}
	timestamp, err := metadata.Timestamp().FromBytes(timestampRaw)
	if err != nil || timestamp.Signed.Version <= 0 || authority.VerifyDelegate(metadata.TIMESTAMP, timestamp) != nil {
		return published, false, errors.New("published feed timestamp is unauthenticated")
	}
	snapshotMeta := timestamp.Signed.Meta["snapshot.json"]
	if snapshotMeta == nil {
		return published, false, errors.New("published feed timestamp has no snapshot binding")
	}
	snapshotRaw, err := readBoundMetadata(directory, snapshotMeta, "snapshot")
	if err != nil {
		return published, false, err
	}
	snapshot, err := metadata.Snapshot().FromBytes(snapshotRaw)
	if err != nil || authority.VerifyDelegate(metadata.SNAPSHOT, snapshot) != nil {
		return published, false, errors.New("published feed snapshot is unauthenticated")
	}
	targetsMeta := snapshot.Signed.Meta["targets.json"]
	if targetsMeta == nil {
		return published, false, errors.New("published feed snapshot has no targets binding")
	}
	targetsRaw, err := readBoundMetadata(directory, targetsMeta, "targets")
	if err != nil {
		return published, false, err
	}
	targets, err := metadata.Targets().FromBytes(targetsRaw)
	if err != nil || authority.VerifyDelegate(metadata.TARGETS, targets) != nil {
		return published, false, errors.New("published feed targets metadata is unauthenticated")
	}
	target := targets.Signed.Targets[CatalogPath]
	if target == nil || len(target.Hashes["sha256"]) == 0 {
		return published, false, errors.New("published feed targets metadata has no catalog binding")
	}
	catalogRaw, err := readRegular(filepath.Join(directory, "targets/security", target.Hashes["sha256"].String()+".catalog.json"), MaxDocumentBytes, false)
	if err != nil {
		return published, false, fmt.Errorf("read published feed catalog: %s", err)
	}
	if err := target.VerifyLengthHashes(catalogRaw); err != nil {
		return published, false, fmt.Errorf("published feed catalog binding: %w", err)
	}
	return Published{Catalog: catalogRaw, TargetsExpires: targets.Signed.Expires, Version: uint64(timestamp.Signed.Version), targetsRaw: targetsRaw, targetsVersion: targets.Signed.Version, timestampRaw: timestampRaw}, true, nil
}

// readBoundMetadata never wraps os.ErrNotExist: a timestamp whose binding is
// missing is a broken repository, not a first publication.
func readBoundMetadata(directory string, meta *metadata.MetaFiles, role string) ([]byte, error) {
	raw, err := readRegular(filepath.Join(directory, "metadata", strconv.FormatInt(meta.Version, 10)+"."+role+".json"), MaxDocumentBytes, false)
	if err != nil {
		return nil, fmt.Errorf("read published feed %s: %s", role, err)
	}
	if err := meta.VerifyLengthHashes(raw); err != nil {
		return nil, fmt.Errorf("published feed %s binding: %w", role, err)
	}
	return raw, nil
}

type RefreshConfig struct {
	Root             []byte
	Published        Published
	SnapshotExpires  time.Time
	TimestampExpires time.Time
	Signers          map[string][]signature.Signer
}

// BuildRefresh re-signs only snapshot and timestamp at a higher version around
// the existing targets metadata. Liveness never needs the targets key.
func BuildRefresh(c RefreshConfig) (*Publication, error) {
	if len(c.Root) == 0 || len(c.Root) > MaxDocumentBytes || len(c.Published.targetsRaw) == 0 || len(c.Published.timestampRaw) == 0 || c.Published.Version == 0 || c.Published.Version >= math.MaxInt64-1 {
		return nil, errors.New("feed refresh requires a bounded root and an authenticated published repository")
	}
	trusted, err := trustedmetadata.New(c.Root)
	if err != nil {
		return nil, err
	}
	if err := validateRoot(trusted.Root); err != nil {
		return nil, err
	}
	now := time.Now()
	if trusted.Root.Signed.IsExpired(now) {
		return nil, errors.New("feed root has expired; a root rotation is required before refresh")
	}
	targets, err := metadata.Targets().FromBytes(c.Published.targetsRaw)
	if err != nil {
		return nil, err
	}
	if err := trusted.Root.VerifyDelegate(metadata.TARGETS, targets); err != nil {
		return nil, fmt.Errorf("published targets are not authorized by this root: %w", err)
	}
	if !targets.Signed.Expires.After(now) {
		return nil, fmt.Errorf("feed targets expired at %s; publish a renewed catalog with the targets key", targets.Signed.Expires.Format(time.RFC3339))
	}
	if !c.TimestampExpires.After(now) || c.TimestampExpires.After(c.SnapshotExpires) || c.SnapshotExpires.After(targets.Signed.Expires) || c.SnapshotExpires.After(trusted.Root.Signed.Expires) {
		return nil, fmt.Errorf("feed refresh expiry must be future and ordered timestamp <= snapshot <= targets (%s) <= root", targets.Signed.Expires.Format(time.RFC3339))
	}
	version := c.Published.Version + 1
	snapshotRaw, timestampRaw, err := signLiveness(trusted, c.Published.targetsRaw, c.Published.targetsVersion, version, c.SnapshotExpires, c.TimestampExpires, c.Signers)
	if err != nil {
		return nil, err
	}
	return &Publication{version: version, baseline: slices.Clone(c.Published.timestampRaw), files: map[string][]byte{
		"metadata/" + strconv.FormatInt(trusted.Root.Signed.Version, 10) + ".root.json": slices.Clone(c.Root),
		"metadata/" + strconv.FormatUint(version, 10) + ".snapshot.json":                snapshotRaw,
		"metadata/timestamp.json": timestampRaw,
	}}, nil
}

func metadataBinding(raw []byte, version int64) (*metadata.MetaFiles, error) {
	info, err := metadata.TargetFile().FromBytes("metadata.json", raw, "sha256")
	if err != nil {
		return nil, err
	}
	return &metadata.MetaFiles{Version: version, Length: info.Length, Hashes: info.Hashes}, nil
}

func sign[T metadata.Roles](meta *metadata.Metadata[T], signers []signature.Signer) error {
	if len(signers) == 0 || len(signers) > 32 {
		return errors.New("feed role requires explicitly supplied bounded signing keys")
	}
	for _, signer := range signers {
		if signer == nil {
			return errors.New("nil feed signer")
		}
		if _, err := meta.Sign(signer); err != nil {
			return err
		}
	}
	return nil
}

func validateRoot(root *metadata.Metadata[metadata.RootType]) error {
	if root == nil || root.Signed.Version <= 0 || !root.Signed.ConsistentSnapshot || len(root.Signed.Roles) != 4 || len(root.Signed.Keys) > 64 {
		return errors.New("security feed requires a bounded consistent-snapshot root with four roles")
	}
	owners := map[string]string{}
	for _, role := range metadata.TOP_LEVEL_ROLE_NAMES {
		value := root.Signed.Roles[role]
		if value == nil || value.Threshold < 1 || value.Threshold > len(value.KeyIDs) || len(value.KeyIDs) > 16 {
			return errors.New("security feed role has an invalid threshold")
		}
		for _, keyID := range value.KeyIDs {
			key, ok := root.Signed.Keys[keyID]
			if !ok || key == nil {
				return errors.New("security feed role references an absent key")
			}
			id, err := key.ID()
			if err != nil || id != keyID {
				return errors.New("security feed key identity is inconsistent")
			}
			if _, err := key.ToPublicKey(); err != nil {
				return errors.New("security feed contains an invalid public key")
			}
			if previous, ok := owners[keyID]; ok {
				return fmt.Errorf("security feed keys must be distinct across roles and unique within %s/%s", previous, role)
			}
			owners[keyID] = role
		}
	}
	return nil
}

func (p *Publication) Files() map[string][]byte {
	if p == nil {
		return nil
	}
	files := make(map[string][]byte, len(p.files))
	for name, raw := range p.files {
		files[name] = slices.Clone(raw)
	}
	return files
}

// Write stages immutable targets and versioned metadata before atomically
// publishing timestamp.json last. Existing immutable bytes cannot be replaced.
// Public hosting and production root enrollment remain separate operator work.
// publish.lock is local coordination in the repository root; only metadata/ and
// targets/ are the public tree.
func (p *Publication) Write(directory string) (returnErr error) {
	if p == nil || p.files["metadata/timestamp.json"] == nil || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return errors.New("feed publication requires a constructed publication and absolute directory")
	}
	if err := privateDirectory(directory, ""); err != nil {
		return err
	}
	lockPath := filepath.Join(directory, "publish.lock")
	if err := checkOptionalFile(lockPath, 65536, false); err != nil {
		return err
	}
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return errors.Join(err, errors.New("another feed publication owns this repository"))
	}
	defer func() { returnErr = errors.Join(returnErr, lock.Unlock()) }()
	for _, name := range []string{"metadata", "targets", "targets/security"} {
		if err := privateDirectory(filepath.Join(directory, name), ""); err != nil {
			return err
		}
	}
	timestampPath := filepath.Join(directory, "metadata/timestamp.json")
	previous, err := readRegular(timestampPath, MaxDocumentBytes, false)
	if (err == nil || errors.Is(err, os.ErrNotExist)) && !bytes.Equal(previous, p.baseline) {
		return errors.New("feed repository timestamp changed since this publication read it; rebuild from the current repository")
	}
	if err == nil {
		value, err := metadata.Timestamp().FromBytes(previous)
		if err != nil || value.Signed.Version <= 0 || uint64(value.Signed.Version) >= p.version {
			return errors.New("published feed timestamp would not increase")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if p.version != 1 {
		return errors.New("later feed publication requires an existing repository timestamp")
	}
	for name, raw := range p.files {
		if name == "metadata/timestamp.json" {
			continue
		}
		path := filepath.Join(directory, filepath.FromSlash(name))
		previous, err := readRegular(path, MaxDocumentBytes, false)
		if err == nil {
			if !slices.Equal(previous, raw) {
				return errors.New("feed publication would replace immutable versioned bytes")
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := securefile.WriteAtomic(path, raw, 0644); err != nil {
			return err
		}
	}
	if err := securefile.WriteAtomic(timestampPath, p.files["metadata/timestamp.json"], 0644); err != nil {
		return err
	}
	return filedurability.SyncDirectory(directory)
}

// LoadSigner loads a protected local PEM without logging or returning private
// key bytes. Encrypted keys are refused; hardware/out-of-band signers can be
// supplied directly to CreateRoot and BuildPublication instead.
func LoadSigner(path string) (signature.Signer, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("feed signer requires an absolute canonical path")
	}
	if err := checkAncestors(filepath.Dir(path), ""); err != nil {
		return nil, err
	}
	raw, err := readRegular(path, 65536, true)
	if err != nil {
		return nil, err
	}
	private, err := cryptoutils.UnmarshalPEMToPrivateKey(raw, cryptoutils.SkipPassword)
	if err != nil {
		return nil, errors.New("could not load protected unencrypted feed signing key")
	}
	if key, ok := private.(*rsa.PrivateKey); ok {
		return signature.LoadRSAPSSSigner(key, crypto.SHA256, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash, Hash: crypto.SHA256})
	}
	return signature.LoadSigner(private, crypto.SHA256)
}

// ReadPublicFile reads operator-owned public policy/evidence without following
// symlink authority or accepting group/world writable files or ancestors.
func ReadPublicFile(path string, max int64) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || max <= 0 || max > MaxDocumentBytes {
		return nil, errors.New("feed public input requires an absolute canonical path and bounded length")
	}
	if err := checkAncestors(filepath.Dir(path), ""); err != nil {
		return nil, err
	}
	return readRegular(path, max, false)
}

// LoadPublicKey parses a protected local PEM through the maintained Sigstore
// parser. Public keys are never selected from downloaded catalog evidence.
func LoadPublicKey(path string) (crypto.PublicKey, error) {
	raw, err := ReadPublicFile(path, 65536)
	if err != nil {
		return nil, err
	}
	public, err := cryptoutils.UnmarshalPEMToPublicKey(raw)
	if err != nil {
		return nil, errors.New("invalid protected feed public key PEM")
	}
	return public, nil
}
