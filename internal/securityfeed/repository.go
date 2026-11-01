// Package securityfeed authenticates a common public security catalog using
// the maintained TUF client. Feed authorization never authorizes an executable;
// release signatures and exact compatibility routes are independently required.
package securityfeed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"github.com/gofrs/flock"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	tufconfig "github.com/theupdateframework/go-tuf/v2/metadata/config"
	"github.com/theupdateframework/go-tuf/v2/metadata/trustedmetadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

const (
	CatalogPath       = "security/catalog.json"
	MaxDocumentBytes  = 4 << 20
	maxAggregateBytes = 16 << 20
	maxRequests       = 40
	refreshTimeout    = 90 * time.Second
)

// Config is installation-owned. TrustedRoot is an explicitly provisioned feed
// root, separate from Hikyo's independently provisioned release trust. Mirrors
// may change URLs while retaining the same state directory and bootstrap root.
type Config struct {
	MetadataURL      string
	TargetsURL       string
	StateDirectory   string
	TrustedRoot      []byte
	Client           *http.Client
	OfflineDirectory string
}

// Document contains only freshly authenticated bytes. Version is the TUF
// targets-metadata version; the catalog's own schema and sequence are separate.
type Document struct {
	Data    []byte
	Version uint64
	Expires time.Time
}

type Repository struct{ config Config }

// New validates configuration without contacting the network or resetting any
// cached trust. Refresh serializes all mutation of the local trust cache.
func New(c Config) (*Repository, error) {
	if !filepath.IsAbs(c.StateDirectory) || filepath.Clean(c.StateDirectory) != c.StateDirectory || c.StateDirectory == string(filepath.Separator) {
		return nil, errors.New("security feed requires an absolute private state directory")
	}
	if len(c.TrustedRoot) == 0 || len(c.TrustedRoot) > MaxDocumentBytes {
		return nil, errors.New("security feed requires a bounded explicitly trusted root")
	}
	trusted, err := trustedmetadata.New(c.TrustedRoot)
	if err != nil {
		return nil, fmt.Errorf("security feed bootstrap root: %w", err)
	}
	if err := validateRoot(trusted.Root); err != nil {
		return nil, err
	}
	if c.OfflineDirectory != "" {
		if !filepath.IsAbs(c.OfflineDirectory) || filepath.Clean(c.OfflineDirectory) != c.OfflineDirectory {
			return nil, errors.New("offline security feed requires an absolute repository directory")
		}
		c.MetadataURL = "https://offline.invalid/metadata/"
		c.TargetsURL = "https://offline.invalid/targets/"
	}
	metadataURL, err := parseBaseURL(c.MetadataURL)
	if err != nil {
		return nil, fmt.Errorf("security feed metadata URL: %w", err)
	}
	targetsURL, err := parseBaseURL(c.TargetsURL)
	if err != nil {
		return nil, fmt.Errorf("security feed targets URL: %w", err)
	}
	if metadataURL.Scheme != targetsURL.Scheme || metadataURL.Host != targetsURL.Host || metadataURL.Path == targetsURL.Path {
		return nil, errors.New("security feed metadata and targets require distinct paths on one HTTPS origin")
	}
	c.MetadataURL, c.TargetsURL = metadataURL.String(), targetsURL.String()
	c.TrustedRoot = slices.Clone(c.TrustedRoot)
	client := &http.Client{Timeout: 15 * time.Second, Transport: http.DefaultTransport}
	if c.Client != nil {
		*client = *c.Client
	}
	if client.Timeout <= 0 || client.Timeout > 15*time.Second {
		client.Timeout = 15 * time.Second
	}
	client.Jar = nil
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("security feed redirects are not permitted; configure the final mirror URL")
	}
	c.Client = client
	return &Repository{config: c}, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || strings.Contains(u.Path, "\\") {
		return nil, errors.New("expected HTTPS without credentials, query, fragment or encoded path")
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/"
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return nil, errors.New("security feed base path contains traversal")
		}
	}
	return u, nil
}

type roleFloor struct {
	Version int64  `json:"version"`
	SHA256  string `json:"sha256"`
}

type trustFloor struct {
	Format          string               `json:"format"`
	BootstrapSHA256 string               `json:"bootstrap_sha256"`
	CheckedAt       time.Time            `json:"checked_at"`
	Roles           map[string]roleFloor `json:"roles"`
}

// Refresh always runs the normal TUF update workflow, including for an offline
// import. A feed failure returns no document and never resets a cached root,
// rollback floor, expiration or an independently admitted serving application.
func (r *Repository) Refresh(ctx context.Context) (document Document, returnErr error) {
	defer func() {
		if returnErr != nil {
			document = Document{}
		}
	}()
	if r == nil {
		return Document{}, errors.New("security feed is not configured")
	}
	c := r.config
	if err := privateDirectory(c.StateDirectory); err != nil {
		return Document{}, err
	}
	lockPath := filepath.Join(c.StateDirectory, "refresh.lock")
	if err := checkOptionalFile(lockPath, 65536, false); err != nil {
		return Document{}, err
	}
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return Document{}, errors.Join(err, errors.New("another security feed refresh owns this state"))
	}
	defer func() { returnErr = errors.Join(returnErr, lock.Unlock()) }()
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	metadataDir := filepath.Join(c.StateDirectory, "metadata")
	targetsDir := filepath.Join(c.StateDirectory, "targets")
	for _, directory := range []string{metadataDir, targetsDir} {
		if err := privateDirectory(directory); err != nil {
			return Document{}, err
		}
	}
	if err := validateCache(metadataDir); err != nil {
		return Document{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, syncMetadata(metadataDir)) }()
	floorPath := filepath.Join(c.StateDirectory, "trust-floor.json")
	if _, err := os.Lstat(floorPath); errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(metadataDir)
		if err != nil || len(entries) != 0 {
			return Document{}, errors.New("security feed trust state is missing beside an existing metadata cache")
		}
	}
	floor, err := loadFloor(floorPath, c.TrustedRoot)
	if err != nil {
		return Document{}, err
	}
	now := time.Now().UTC()
	if now.Before(floor.CheckedAt) {
		return Document{}, errors.New("security feed clock precedes the durable trusted-time floor")
	}
	floor.CheckedAt = now
	if err := saveFloor(floorPath, floor); err != nil {
		return Document{}, err
	}
	rootBytes := c.TrustedRoot
	if raw, err := readRegular(filepath.Join(metadataDir, "root.json"), MaxDocumentBytes, false); err == nil {
		rootBytes = raw
	} else if !errors.Is(err, os.ErrNotExist) {
		return Document{}, err
	} else if len(floor.Roles) != 0 {
		return Document{}, errors.New("security feed cache is missing its previously trusted root; refusing bootstrap reset")
	}
	cfg, err := tufconfig.New(c.MetadataURL, rootBytes)
	if err != nil {
		return Document{}, err
	}
	cfg.LocalMetadataDir, cfg.LocalTargetsDir = metadataDir, targetsDir
	cfg.RemoteTargetsURL = c.TargetsURL
	cfg.MaxRootRotations, cfg.MaxDelegations = 32, 1
	cfg.RootMaxLength, cfg.TimestampMaxLength = 512<<10, 64<<10
	cfg.SnapshotMaxLength, cfg.TargetsMaxLength = MaxDocumentBytes, MaxDocumentBytes
	fetch := &boundedFetcher{ctx: ctx, client: c.Client, metadataURL: c.MetadataURL, targetsURL: c.TargetsURL, floors: floor.Roles}
	if c.OfflineDirectory != "" {
		info, err := os.Lstat(c.OfflineDirectory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Document{}, errors.New("offline security feed repository must be a real directory")
		}
		fetch.local, err = os.OpenRoot(c.OfflineDirectory)
		if err != nil {
			return Document{}, err
		}
		defer func() { returnErr = errors.Join(returnErr, fetch.local.Close()) }()
	}
	cfg.Fetcher = fetch
	client, err := updater.New(cfg)
	if err != nil {
		return Document{}, fmt.Errorf("security feed cached trust: %w", err)
	}
	if err := client.Refresh(); err != nil {
		return Document{}, fmt.Errorf("security feed refresh: %w", err)
	}
	set := client.GetTrustedMetadataSet()
	if err := validateRoot(set.Root); err != nil {
		return Document{}, err
	}
	top, ok := set.Targets[metadata.TARGETS]
	if !ok || top.Signed.Delegations != nil || len(top.Signed.Targets) != 1 {
		return Document{}, errors.New("security feed requires exactly one top-level catalog target and no delegations")
	}
	info, ok := top.Signed.Targets[CatalogPath]
	if !ok || info.Length <= 0 || info.Length > MaxDocumentBytes || len(info.Hashes) != 1 || len(info.Hashes["sha256"]) != sha256.Size {
		return Document{}, errors.New("security catalog requires a bounded length and one SHA-256 binding")
	}
	roles := map[string]roleFloor{}
	for role, signed := range map[string]any{metadata.ROOT: set.Root.Signed, metadata.TIMESTAMP: set.Timestamp.Signed, metadata.SNAPSHOT: set.Snapshot.Signed, metadata.TARGETS: top.Signed} {
		value, err := signedRoleClaim(signed)
		if err != nil {
			return Document{}, err
		}
		previous := floor.Roles[role]
		if value.Version < previous.Version || value.Version == previous.Version && value.SHA256 != previous.SHA256 {
			return Document{}, errors.New("security feed metadata rollback or equivocation refused")
		}
		roles[role] = value
	}
	floor.Roles = roles
	if err := saveFloor(floorPath, floor); err != nil {
		return Document{}, err
	}
	target, err := client.GetTargetInfo(CatalogPath)
	if err != nil {
		return Document{}, err
	}
	// Do not reuse cached target bytes as freshness evidence. Only freshly
	// refreshed metadata and a verified download can produce a document.
	if err := checkOptionalFile(filepath.Join(targetsDir, "catalog.json"), MaxDocumentBytes, false); err != nil {
		return Document{}, err
	}
	_, raw, err := client.DownloadTarget(target, filepath.Join(targetsDir, "catalog.json"), "")
	if err != nil {
		return Document{}, fmt.Errorf("security catalog download: %w", err)
	}
	if err := syncFile(filepath.Join(targetsDir, "catalog.json")); err != nil {
		return Document{}, err
	}
	if err := filedurability.SyncDirectory(targetsDir); err != nil {
		return Document{}, err
	}
	expires := set.Root.Signed.Expires
	for _, next := range []time.Time{set.Timestamp.Signed.Expires, set.Snapshot.Signed.Expires, top.Signed.Expires} {
		if next.Before(expires) {
			expires = next
		}
	}
	if !time.Now().Before(expires) {
		return Document{}, errors.New("security feed metadata expired during refresh")
	}
	return Document{Data: slices.Clone(raw), Version: uint64(top.Signed.Version), Expires: expires}, nil
}

func signedRoleClaim(signed any) (roleFloor, error) {
	raw, err := json.Marshal(signed)
	if err != nil {
		return roleFloor{}, err
	}
	var header struct {
		Version int64 `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil || header.Version <= 0 {
		return roleFloor{}, errors.New("security feed metadata version is invalid")
	}
	return roleFloor{Version: header.Version, SHA256: digest(raw)}, nil
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func loadFloor(path string, bootstrap []byte) (trustFloor, error) {
	initial := trustFloor{Format: "hikyo.security-feed-trust/v1", BootstrapSHA256: digest(bootstrap), Roles: map[string]roleFloor{}}
	raw, err := readRegular(path, 65536, true)
	if errors.Is(err, os.ErrNotExist) {
		return initial, nil
	}
	if err != nil {
		return initial, err
	}
	var value trustFloor
	if err := definitions.DecodeStrict(raw, &value); err != nil || value.Format != initial.Format || value.BootstrapSHA256 != initial.BootstrapSHA256 || value.CheckedAt.IsZero() || value.Roles == nil || len(value.Roles) > 4 {
		return initial, errors.New("invalid security feed trust state or changed bootstrap root")
	}
	for role, floor := range value.Roles {
		if !slices.Contains(metadata.TOP_LEVEL_ROLE_NAMES[:], role) || floor.Version < 1 || len(floor.SHA256) != 64 {
			return initial, errors.New("invalid security feed role floor")
		}
	}
	return value, nil
}

func saveFloor(path string, floor trustFloor) error {
	raw, err := json.Marshal(floor)
	if err != nil {
		return err
	}
	return securefile.WriteAtomic(path, append(raw, '\n'), 0600)
}
