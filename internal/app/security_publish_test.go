package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/securityfeed"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

func securityPublishFixture(t *testing.T) (string, publicationRootConfig, map[string]string) {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := publicationRootConfig{Version: 1, Expires: time.Now().Add(72 * time.Hour), Roles: map[string]publicationRootRole{}}
	privatePaths := map[string]string{}
	for _, role := range metadata.TOP_LEVEL_ROLE_NAMES {
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		publicDER, err := x509.MarshalPKIXPublicKey(public)
		if err != nil {
			t.Fatal(err)
		}
		privateDER, err := x509.MarshalPKCS8PrivateKey(private)
		if err != nil {
			t.Fatal(err)
		}
		publicPath, privatePath := filepath.Join(directory, role+".pub"), filepath.Join(directory, role+".pem")
		if err := os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0600); err != nil {
			t.Fatal(err)
		}
		config.Roles[role] = publicationRootRole{PublicKeyFiles: []string{publicPath}, Threshold: 1}
		privatePaths[role] = privatePath
	}
	config.SignerFiles = []string{privatePaths[metadata.ROOT]}
	return directory, config, privatePaths
}

func writeSecurityPublishJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSecurityFeedInitRootIsExplicitAndDoesNotReplaceRoot(t *testing.T) {
	directory, config, _ := securityPublishFixture(t)
	configPath, output := filepath.Join(directory, "root-config.json"), filepath.Join(directory, "root.json")
	writeSecurityPublishJSON(t, configPath, config)
	var stdout bytes.Buffer
	if err := RunSecurityFeed(context.Background(), []string{"init-root", "--config", configPath, "--output", output}, &stdout); err != nil {
		t.Fatal(err)
	}
	raw, err := securityfeed.ReadPublicFile(output, securityfeed.MaxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	root, err := metadata.Root().FromBytes(raw)
	if err != nil || root.Signed.Version != 1 || root.VerifyDelegate(metadata.ROOT, root) != nil {
		t.Fatal("init-root did not write an authenticated local root")
	}
	if !strings.Contains(stdout.String(), "Production trust is unchanged") || strings.Contains(stdout.String(), "PRIVATE KEY") || strings.Contains(stdout.String(), config.SignerFiles[0]) {
		t.Fatal("root receipt omitted its activation boundary or disclosed signer inputs")
	}
	if err := RunSecurityFeed(context.Background(), []string{"init-root", "--config", configPath, "--output", output}, io.Discard); err == nil {
		t.Fatal("init-root replaced an existing trust anchor")
	}
	after, err := os.ReadFile(output)
	if err != nil || !slices.Equal(raw, after) {
		t.Fatal("failed root replacement changed original bytes")
	}
}

func TestSecurityFeedRootConfigRejectsDuplicateUnknownAndInvalidThreshold(t *testing.T) {
	directory, config, _ := securityPublishFixture(t)
	configPath := filepath.Join(directory, "root-config.json")
	for _, raw := range []string{`{"version":1,"version":2}`, `{"version":1,"unrecognized":true}`, `{} {}`} {
		if err := os.WriteFile(configPath, []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
		if err := RunSecurityFeed(context.Background(), []string{"init-root", "--config", configPath, "--output", filepath.Join(directory, "root.json")}, io.Discard); err == nil {
			t.Fatalf("invalid root config accepted: %s", raw)
		}
	}
	role := config.Roles[metadata.ROOT]
	role.Threshold = 2
	config.Roles[metadata.ROOT] = role
	writeSecurityPublishJSON(t, configPath, config)
	if err := RunSecurityFeed(context.Background(), []string{"init-root", "--config", configPath, "--output", filepath.Join(directory, "root.json")}, io.Discard); err == nil {
		t.Fatal("unsatisfiable root role threshold accepted")
	}
}

func TestSecurityFeedFlagsRejectAmbiguousAuthorityAndPositionals(t *testing.T) {
	for _, args := range [][]string{
		{"init-root", "--config", "/first", "--config=/second", "--output", "/root.json"},
		{"init-root", "--config", "/config", "--output", "/one", "--output", "/two"},
		{"init-root", "--config", "relative", "--output", "/root.json"},
		{"init-root", "--unknown", "value"},
		{"publish", "--version", "1", "--version=2"},
		{"publish", "--targets-key", "/key", "--targets-key", "/key"},
		{"publish", "positional"},
	} {
		if err := RunSecurityFeed(context.Background(), args, io.Discard); err == nil {
			t.Fatalf("ambiguous flags accepted: %q", args)
		}
	}
	if err := uniqueSecurityFeedFlags([]string{"--targets-key", "/one", "--targets-key=/two"}, map[string]bool{"targets-key": true}); err != nil {
		t.Fatal("distinct threshold signer flags were rejected")
	}
	for _, command := range [][]string{{"--help"}, {"init-root", "--help"}, {"publish", "--help"}} {
		if err := RunSecurityFeed(context.Background(), command, io.Discard); err != nil {
			t.Fatalf("command help failed: %v", err)
		}
	}
}

func securityPublicationCatalog() securitymaintenance.Catalog {
	now := time.Now().UTC()
	identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.0.1", Sequence: 2, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Digest(strings.Repeat("b", 64)), ManifestSHA256: releaseidentity.Digest(strings.Repeat("c", 64))}
	return securitymaintenance.Catalog{Format: securitymaintenance.CatalogFormat, Sequence: 1, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(24 * time.Hour), Advisories: []securitymaintenance.Advisory{{ID: "CVE-2099-0001", Summary: "Synthetic fixture only", Severity: securitymaintenance.High, Components: []string{"hikyo"}, Affected: []securitymaintenance.Affected{{Profile: releaseidentity.StableV1, Introduced: "1.0.0", Fixed: "1.0.1", Platforms: []string{"linux/amd64", "linux/arm64"}}}}}, Targets: []securitymaintenance.Target{{Identity: identity, Platforms: []string{"linux/amd64"}, Image: "registry.example/hikyo@sha256:" + strings.Repeat("d", 64), Fixes: []string{"CVE-2099-0001"}, Impact: securitymaintenance.Impact{Restart: true}}}}
}

func TestSecurityFeedPublishRequiresIndependentVerificationBeforeOutput(t *testing.T) {
	directory, config, keys := securityPublishFixture(t)
	configPath, rootPath := filepath.Join(directory, "root-config.json"), filepath.Join(directory, "root.json")
	writeSecurityPublishJSON(t, configPath, config)
	if err := RunSecurityFeed(context.Background(), []string{"init-root", "--config", configPath, "--output", rootPath}, io.Discard); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(directory, "catalog.json")
	writeSecurityPublishJSON(t, catalogPath, securityPublicationCatalog())
	output, cache := filepath.Join(directory, "published"), filepath.Join(directory, "release-cache")
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	args := []string{"publish", "--catalog", catalogPath, "--root", rootPath, "--output", output, "--cache", cache, "--version", "1", "--targets-expires", expires, "--snapshot-expires", expires, "--timestamp-expires", expires, "--targets-key", keys[metadata.TARGETS], "--snapshot-key", keys[metadata.SNAPSHOT], "--timestamp-key", keys[metadata.TIMESTAMP]}
	refused := errors.New("synthetic independent-release refusal")
	if err := runSecurityFeed(context.Background(), args, io.Discard, func(_ context.Context, catalog securitymaintenance.Catalog, selectedCache string) error {
		if catalog.Sequence != 1 || selectedCache != cache {
			t.Fatal("wrong structurally validated catalog or local cache passed to verification")
		}
		return refused
	}); !errors.Is(err, refused) {
		t.Fatalf("independent verifier refusal lost: %v", err)
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("publisher wrote output before independent target verification")
	}
	calls := 0
	if err := runSecurityFeed(context.Background(), args, io.Discard, func(context.Context, securitymaintenance.Catalog, string) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("publication skipped verification")
	}
	if _, err := os.Stat(filepath.Join(output, "metadata/timestamp.json")); err != nil {
		t.Fatal("verified publication was not written")
	}
	// The exported production path cannot inject a fixture verifier or obtain
	// release trust from the feed root. Unstamped source builds refuse it.
	otherOutput := filepath.Join(directory, "production-refusal")
	for i := range args {
		if args[i] == output {
			args[i] = otherOutput
		}
	}
	if err := RunSecurityFeed(context.Background(), args, io.Discard); err == nil {
		t.Fatal("unstamped source build published executable eligibility")
	}
	if _, err := os.Lstat(otherOutput); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing independent production trust left a published timestamp")
	}
}

func TestSecurityPublicationTargetsDedupeReleaseVerificationButCheckEachImage(t *testing.T) {
	catalog := securityPublicationCatalog()
	identity, image := catalog.Targets[0].Identity, catalog.Targets[0].Image
	second := catalog.Targets[0]
	second.Platforms = []string{"linux/arm64"}
	catalog.Targets = append(catalog.Targets, second)
	withdrawn := second
	withdrawn.Identity.Sequence = 3
	withdrawn.Withdrawn = true
	catalog.Targets = append(catalog.Targets, withdrawn)
	calls := 0
	prepare := func(_ context.Context, selected releaseidentity.Identity, _ string) (publicationVerifiedRelease, error) {
		calls++
		if selected != identity {
			t.Fatal("withdrawn target was prepared")
		}
		return publicationVerifiedRelease{Identity: identity, Artifacts: []releasetrust.Artifact{{Kind: "image", Image: "registry.example/hikyo", Digest: "sha256:" + strings.Repeat("d", 64)}}}, nil
	}
	if err := verifySecurityPublicationTargets(context.Background(), catalog, "/unused", prepare); err != nil || calls != 1 {
		t.Fatalf("release verification was not deduplicated: calls=%d err=%v", calls, err)
	}
	catalog.Targets[1].Image = image[:len(image)-1] + "e"
	if err := verifySecurityPublicationTargets(context.Background(), catalog, "/unused", prepare); err == nil {
		t.Fatal("second platform's unauthenticated image accepted through identity cache")
	}
	catalog.Targets = catalog.Targets[:1]
	if err := verifySecurityPublicationTargets(context.Background(), catalog, "/unused", func(context.Context, releaseidentity.Identity, string) (publicationVerifiedRelease, error) {
		wrong := identity
		wrong.Commit = strings.Repeat("f", 40)
		return publicationVerifiedRelease{Identity: wrong}, nil
	}); err == nil {
		t.Fatal("independent identity mismatch accepted")
	}
}

func TestSecurityFeedInvalidCatalogNeverCallsReleaseVerifier(t *testing.T) {
	directory, _, keys := securityPublishFixture(t)
	catalog := filepath.Join(directory, "invalid-catalog.json")
	if err := os.WriteFile(catalog, []byte(`{"format":"unknown"}`), 0644); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	args := []string{"publish", "--catalog", catalog, "--root", filepath.Join(directory, "absent-root"), "--output", filepath.Join(directory, "published"), "--cache", filepath.Join(directory, "cache"), "--version", "1", "--targets-expires", expires, "--snapshot-expires", expires, "--timestamp-expires", expires, "--targets-key", keys[metadata.TARGETS], "--snapshot-key", keys[metadata.SNAPSHOT], "--timestamp-key", keys[metadata.TIMESTAMP]}
	if err := runSecurityFeed(context.Background(), args, io.Discard, func(context.Context, securitymaintenance.Catalog, string) error {
		t.Fatal("invalid catalog reached independent verification")
		return nil
	}); err == nil {
		t.Fatal("invalid domain catalog accepted")
	}
}
