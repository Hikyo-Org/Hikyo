// Package releaseidentity defines release-wide claims. Valid structure is not
// authentication: only releasetrust can establish a verified release.
package releaseidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
)

type Profile string

const (
	StableV1  Profile = "stable/v1"
	NightlyV1 Profile = "nightly/v1"
)

func (p Profile) Validate() error {
	if p != StableV1 && p != NightlyV1 {
		return errors.New("unknown release trust profile")
	}
	return nil
}

// Digest is the lowercase hexadecimal SHA-256 of exact bytes.
type Digest string

func Hash(raw []byte) Digest {
	sum := sha256.Sum256(raw)
	return Digest(hex.EncodeToString(sum[:]))
}

func (d Digest) Validate() error {
	if len(d) != 64 {
		return errors.New("SHA-256 must contain 64 lowercase hexadecimal characters")
	}
	for _, c := range d {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return errors.New("SHA-256 must contain 64 lowercase hexadecimal characters")
		}
	}
	return nil
}

// Identity deliberately excludes platform and installation identity. Every
// architecture of one release shares this identity; artifact bytes are bound
// separately by its verified release manifest.
type Identity struct {
	Profile             Profile `json:"profile"`
	Version             string  `json:"version"`
	Sequence            uint64  `json:"sequence"`
	Commit              string  `json:"commit"`
	CompatibilitySHA256 Digest  `json:"compatibility_sha256"`
	ManifestSHA256      Digest  `json:"manifest_sha256"`
}

// Source names either an exact release or an explicitly declared fresh/legacy
// genesis. It is a claim: accepting a genesis still requires F2's independent
// complete schema/migration inspection. Engine migration and schema digests
// travel alongside this value, never as an invented release identity.
type Source struct {
	Genesis string   `json:"genesis,omitempty"`
	Release Identity `json:"release,omitzero"`
}

const (
	FreshGenesisV1  = "fresh/v1"
	LegacyGenesisV1 = "legacy/v1"
)

func (s Source) Validate() error {
	if s.Genesis != "" {
		if (s.Genesis != LegacyGenesisV1 && s.Genesis != FreshGenesisV1) || s.Release != (Identity{}) {
			return errors.New("source must name an exact fresh/v1 or legacy/v1 genesis without a release")
		}
		return nil
	}
	return s.Release.Validate()
}

func (s Source) IsRelease() bool { return s.Genesis == "" && s.Release.Validate() == nil }

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (i Identity) Validate() error {
	if err := ValidateTarget(i.Profile, i.Version, i.Sequence, i.Commit); err != nil {
		return err
	}
	if err := i.CompatibilitySHA256.Validate(); err != nil {
		return fmt.Errorf("compatibility digest: %w", err)
	}
	if err := i.ManifestSHA256.Validate(); err != nil {
		return fmt.Errorf("manifest digest: %w", err)
	}
	return nil
}

// ValidateTarget checks the source-owned fields that can be embedded before
// the containing artifact and release manifest are built.
func ValidateTarget(profile Profile, version string, sequence uint64, commit string) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	parsed, err := semver.StrictNewVersion(version)
	if err != nil || len(version) > 256 || parsed.String() != version {
		return errors.New("release version must be canonical semantic version without v")
	}
	if sequence == 0 || sequence > math.MaxInt64 || !commitPattern.MatchString(commit) {
		return errors.New("release sequence or source commit is invalid")
	}
	if profile == StableV1 && (parsed.Prerelease() == "nightly" || strings.HasPrefix(parsed.Prerelease(), "nightly.")) {
		return errors.New("stable identity cannot name a nightly")
	}
	if profile == NightlyV1 && !strings.HasPrefix(parsed.Prerelease(), "nightly.") {
		return errors.New("nightly identity must name an explicit nightly prerelease")
	}
	return nil
}

type Engine string

const (
	SQLite            Engine = "sqlite"
	Postgres          Engine = "postgres"
	MaxMigrations            = 4096
	MaxMigrationBytes        = 4 << 20
)

func (e Engine) Validate() error {
	if e != SQLite && e != Postgres {
		return errors.New("unknown migration engine")
	}
	return nil
}

type Migration struct {
	Version uint64 `json:"version"`
	SHA256  Digest `json:"sha256"`
}

// MigrationManifest is ordered by strictly increasing goose version. Its
// digest covers the engine and each exact migration byte digest in that order.
type MigrationManifest struct {
	Engine  Engine      `json:"engine"`
	Entries []Migration `json:"entries"`
}

func (m MigrationManifest) Validate() error {
	if err := m.Engine.Validate(); err != nil {
		return err
	}
	if m.Entries == nil || len(m.Entries) > MaxMigrations {
		return errors.New("migration entries must be a bounded non-null array")
	}
	var previous uint64
	for _, entry := range m.Entries {
		if entry.Version <= previous || entry.Version > math.MaxInt64 {
			return errors.New("migration versions must be positive, ordered and unique")
		}
		if err := entry.SHA256.Validate(); err != nil {
			return fmt.Errorf("migration %d: %w", entry.Version, err)
		}
		previous = entry.Version
	}
	return nil
}

func (m MigrationManifest) Clone() MigrationManifest {
	m.Entries = slices.Clone(m.Entries)
	return m
}

func (m MigrationManifest) Digest() (Digest, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return Hash(raw), nil
}

// BuildMigrationManifest reads the actual source-owned SQL bytes. Callers pass
// the embedded migration filesystem or an explicit build-time filesystem;
// this leaf package never imports datastore adapters.
func BuildMigrationManifest(source fs.FS, directory string, engine Engine) (MigrationManifest, error) {
	manifest := MigrationManifest{Engine: engine, Entries: []Migration{}}
	if err := engine.Validate(); err != nil {
		return MigrationManifest{}, err
	}
	entries, err := fs.ReadDir(source, directory)
	if err != nil {
		return MigrationManifest{}, err
	}
	if len(entries) > MaxMigrations {
		return MigrationManifest{}, errors.New("migration directory exceeds entry bound")
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			return MigrationManifest{}, fmt.Errorf("unexpected migration entry %q", entry.Name())
		}
		prefix, suffix, found := strings.Cut(entry.Name(), "_")
		version, err := strconv.ParseUint(prefix, 10, 63)
		if !found || err != nil || version == 0 || suffix == ".sql" {
			return MigrationManifest{}, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > MaxMigrationBytes {
			return MigrationManifest{}, fmt.Errorf("migration %q is not a bounded regular file", entry.Name())
		}
		file, err := source.Open(path.Join(directory, entry.Name()))
		if err != nil {
			return MigrationManifest{}, err
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, MaxMigrationBytes+1))
		if err := errors.Join(readErr, file.Close()); err != nil {
			return MigrationManifest{}, err
		}
		if len(raw) == 0 || len(raw) > MaxMigrationBytes {
			return MigrationManifest{}, fmt.Errorf("migration %q size is invalid", entry.Name())
		}
		manifest.Entries = append(manifest.Entries, Migration{Version: version, SHA256: Hash(raw)})
	}
	slices.SortFunc(manifest.Entries, func(a, b Migration) int {
		if a.Version < b.Version {
			return -1
		}
		if a.Version > b.Version {
			return 1
		}
		return 0
	})
	if err := manifest.Validate(); err != nil {
		return MigrationManifest{}, err
	}
	return manifest, nil
}
