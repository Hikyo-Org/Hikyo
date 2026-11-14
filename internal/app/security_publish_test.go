package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
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
	raw, err := json.Marshal(value, jsonwire.Stable)
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
		{"refresh", "--root", "/one", "--root", "/two"},
		{"refresh", "--targets-key", "/key"},
		{"advisory", "--id", "A", "--features", "--id", "B"},
		{"advisory", "positional"},
	} {
		if err := RunSecurityFeed(context.Background(), args, io.Discard); err == nil {
			t.Fatalf("ambiguous flags accepted: %q", args)
		}
	}
	if err := uniqueSecurityFeedFlags([]string{"--targets-key", "/one", "--targets-key=/two"}, map[string]bool{"targets-key": true}); err != nil {
		t.Fatal("distinct threshold signer flags were rejected")
	}
	for _, command := range [][]string{{"--help"}, {"init-root", "--help"}, {"publish", "--help"}, {"refresh", "--help"}, {"advisory", "--help"}} {
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

// A binary target advertises platforms the client selects by signed archive name,
// so each one must be present in the independently signed inventory.
func TestSecurityPublicationTargetsCheckEveryBinaryPlatform(t *testing.T) {
	catalog := securityPublicationCatalog()
	release := feedFixtureRelease("1.0.1", 2)
	catalog.Targets[0].Identity = release.Identity
	catalog.Targets[0].Image = ""
	catalog.Targets[0].Platforms = []string{"linux/amd64", "darwin/arm64"}
	prepare := func(context.Context, releaseidentity.Identity, string) (publicationVerifiedRelease, error) {
		return release, nil
	}
	if err := verifySecurityPublicationTargets(context.Background(), catalog, "/unused", prepare); err != nil {
		t.Fatal(err)
	}
	catalog.Targets[0].Platforms = append(catalog.Targets[0].Platforms, "darwin/amd64")
	if err := verifySecurityPublicationTargets(context.Background(), catalog, "/unused", prepare); err == nil || !strings.Contains(err.Error(), "darwin/amd64") {
		t.Fatalf("binary target platform without a signed binary accepted: %v", err)
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

type feedRepositoryFixture struct {
	directory, root, output, cache string
	keys                           map[string]string
	targetsExpires                 string
}

func newFeedRepositoryFixture(t *testing.T) feedRepositoryFixture {
	t.Helper()
	directory, config, keys := securityPublishFixture(t)
	configPath, rootPath := filepath.Join(directory, "root-config.json"), filepath.Join(directory, "root.json")
	writeSecurityPublishJSON(t, configPath, config)
	if err := RunSecurityFeed(context.Background(), []string{"init-root", "--config", configPath, "--output", rootPath}, io.Discard); err != nil {
		t.Fatal(err)
	}
	return feedRepositoryFixture{directory: directory, root: rootPath, output: filepath.Join(directory, "published"), cache: filepath.Join(directory, "cache"), keys: keys, targetsExpires: time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)}
}

func feedFixtureRelease(version string, sequence uint64) publicationVerifiedRelease {
	return publicationVerifiedRelease{
		Identity: releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: version, Sequence: sequence, Commit: fmt.Sprintf("%040x", sequence), CompatibilitySHA256: releaseidentity.Digest(fmt.Sprintf("%064x", sequence+100)), ManifestSHA256: releaseidentity.Digest(fmt.Sprintf("%064x", sequence))},
		// Real stable manifests name binary platforms only through signed names.
		Artifacts: []releasetrust.Artifact{
			{Kind: "binary", Name: "hikyo_" + version + "_Linux_x86_64.tar.gz"},
			{Kind: "binary", Name: "hikyo_" + version + "_Linux_arm64.tar.gz"},
			{Kind: "binary", Name: "hikyo_" + version + "_Darwin_arm64.tar.gz"},
			{Kind: "image", Image: "ghcr.io/hikyo-org/hikyo", Digest: fmt.Sprintf("sha256:%064x", sequence+200)},
		},
	}
}

func feedFixtureResolver(_ context.Context, version, _ string) (publicationVerifiedRelease, error) {
	switch version {
	case "1.0.1":
		return feedFixtureRelease(version, 2), nil
	case "1.0.2":
		return feedFixtureRelease(version, 3), nil
	}
	return publicationVerifiedRelease{}, errors.New("synthetic release is unavailable")
}

func (f feedRepositoryFixture) advisory(t *testing.T, name string, args ...string) string {
	t.Helper()
	output := filepath.Join(f.directory, name)
	if err := runSecurityFeedAdvisory(context.Background(), append([]string{"--output", output, "--cache", f.cache}, args...), io.Discard, feedFixtureResolver); err != nil {
		t.Fatal(err)
	}
	return output
}

func (f feedRepositoryFixture) publish(catalog string, version int, verify publicationCatalogVerifier) error {
	now := time.Now().UTC()
	return runSecurityFeed(context.Background(), []string{"publish", "--catalog", catalog, "--root", f.root, "--output", f.output, "--cache", f.cache, "--version", fmt.Sprint(version), "--targets-expires", f.targetsExpires, "--snapshot-expires", now.Add(24 * time.Hour).Format(time.RFC3339), "--timestamp-expires", now.Add(2 * time.Hour).Format(time.RFC3339), "--targets-key", f.keys[metadata.TARGETS], "--snapshot-key", f.keys[metadata.SNAPSHOT], "--timestamp-key", f.keys[metadata.TIMESTAMP]}, io.Discard, verify)
}

func (f feedRepositoryFixture) refresh(snapshotExpires, timestampExpires time.Time) error {
	return RunSecurityFeed(context.Background(), []string{"refresh", "--root", f.root, "--output", f.output, "--snapshot-expires", snapshotExpires.UTC().Format(time.RFC3339), "--timestamp-expires", timestampExpires.UTC().Format(time.RFC3339), "--snapshot-key", f.keys[metadata.SNAPSHOT], "--timestamp-key", f.keys[metadata.TIMESTAMP]}, io.Discard)
}

func acceptFeedTargets(context.Context, securitymaintenance.Catalog, string) error { return nil }

var criticalAdvisoryArgs = []string{"--id", "CVE-2099-0001", "--summary", "Critical synthetic flaw", "--severity", "critical", "--component", "hikyo", "--platform", "linux/amd64", "--platform", "linux/arm64"}

func readFeedCatalog(t *testing.T, path string) securitymaintenance.Catalog {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := securitymaintenance.DecodeCatalog(raw, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestSecurityFeedCriticalFlawRoundTripThroughRealClient(t *testing.T) {
	f := newFeedRepositoryFixture(t)
	firstCatalog := f.advisory(t, "catalog-1.json", append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.1")...)
	if err := f.publish(firstCatalog, 1, acceptFeedTargets); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.FileServer(http.Dir(f.output)))
	t.Cleanup(server.Close)
	rootRaw, err := os.ReadFile(f.root)
	if err != nil {
		t.Fatal(err)
	}
	state, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client, err := securityfeed.New(securityfeed.Config{MetadataURL: server.URL + "/metadata/", TargetsURL: server.URL + "/targets/", StateDirectory: filepath.Join(state, "client"), TrustedRoot: rootRaw, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	seen := func(want uint64) securityfeed.Document {
		t.Helper()
		document, err := client.Refresh(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := securitymaintenance.DecodeCatalog(document.Data, time.Now().UTC())
		if err != nil || catalog.Sequence != want {
			t.Fatalf("client saw sequence %d, want %d: %v", catalog.Sequence, want, err)
		}
		return document
	}
	first := seen(1)
	if first.Version != 1 {
		t.Fatalf("first targets version %d", first.Version)
	}

	// Liveness moves only timestamp expiry; catalog and targets stay untouched.
	now := time.Now()
	if err := f.refresh(now.Add(24*time.Hour), now.Add(12*time.Hour)); err != nil {
		t.Fatal(err)
	}
	refreshed := seen(1)
	if refreshed.Version != 1 || !slices.Equal(refreshed.Data, first.Data) || !refreshed.Expires.After(first.Expires) {
		t.Fatalf("refresh changed targets or did not extend freshness: %+v", refreshed.Version)
	}
	if _, err := os.Lstat(filepath.Join(f.output, "metadata/2.targets.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refresh wrote targets metadata")
	}
	for _, version := range []string{"1.snapshot.json", "2.snapshot.json", "1.targets.json"} {
		if _, err := os.Stat(filepath.Join(f.output, "metadata", version)); err != nil {
			t.Fatalf("versioned file lost: %v", err)
		}
	}

	// A real lapse: the only timestamp a client can fetch has expired.
	timestampPath := filepath.Join(f.output, "metadata/timestamp.json")
	raw, err := os.ReadFile(timestampPath)
	if err != nil {
		t.Fatal(err)
	}
	lapsed, err := metadata.Timestamp().FromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := securityfeed.LoadSigner(f.keys[metadata.TIMESTAMP])
	if err != nil {
		t.Fatal(err)
	}
	lapsed.Signed.Version++
	lapsed.Signed.Expires = now.Add(-time.Hour).UTC()
	lapsed.ClearSignatures()
	if _, err := lapsed.Sign(signer); err != nil {
		t.Fatal(err)
	}
	lapsedRaw, err := lapsed.ToBytes(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(timestampPath, lapsedRaw, 0644); err != nil {
		t.Fatal(err)
	}
	if document, err := client.Refresh(context.Background()); err == nil || document.Data != nil {
		t.Fatalf("expired feed produced a document: %v", err)
	}
	if err := f.refresh(now.Add(24*time.Hour), now.Add(12*time.Hour)); err != nil {
		t.Fatalf("online keys could not recover a lapsed feed: %v", err)
	}
	seen(1)

	// The bad release is pulled while its advisory moves past it. Only the new
	// release is verified, and the withdrawn one cannot block publication.
	secondCatalog := f.advisory(t, "catalog-2.json", append(slices.Clone(criticalAdvisoryArgs), "--previous-catalog", firstCatalog, "--fixed-version", "1.0.2", "--withdraw-target", "1.0.1", "--rollout-minutes", "30")...)
	prepared := []releaseidentity.Identity{}
	if err := f.publish(secondCatalog, 5, func(ctx context.Context, catalog securitymaintenance.Catalog, cache string) error {
		return verifySecurityPublicationTargets(ctx, catalog, cache, func(_ context.Context, identity releaseidentity.Identity, _ string) (publicationVerifiedRelease, error) {
			prepared = append(prepared, identity)
			return feedFixtureRelease(identity.Version, identity.Sequence), nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	if len(prepared) != 1 || prepared[0].Version != "1.0.2" {
		t.Fatalf("verification was not limited to the new release: %+v", prepared)
	}
	if document := seen(2); document.Version != 5 {
		t.Fatalf("new targets version %d", document.Version)
	}
	catalog := readFeedCatalog(t, secondCatalog)
	for _, target := range catalog.Targets {
		if target.Identity.Version == "1.0.1" && !target.Withdrawn {
			t.Fatal("bad release remained eligible")
		}
		if target.Identity.Version == "1.0.2" && (target.Withdrawn || target.RolloutMinutes != 30 || target.AvailableAt.IsZero()) {
			t.Fatalf("new target lost rollout fields: %+v", target)
		}
	}
	if err := f.refresh(now.Add(24*time.Hour), now.Add(12*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if document := seen(2); document.Version != 5 {
		t.Fatalf("refresh after publication changed targets version to %d", document.Version)
	}
}

func TestSecurityFeedPublishRefusesNonAdvancingCatalogAndAllowsResigning(t *testing.T) {
	f := newFeedRepositoryFixture(t)
	first := f.advisory(t, "catalog-1.json", append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.1")...)
	if err := f.publish(first, 1, acceptFeedTargets); err != nil {
		t.Fatal(err)
	}
	second := f.advisory(t, "catalog-2.json", "--previous-catalog", first)
	if err := f.publish(second, 2, acceptFeedTargets); err != nil {
		t.Fatal(err)
	}
	timestampPath := filepath.Join(f.output, "metadata/timestamp.json")
	before, err := os.ReadFile(timestampPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := readFeedCatalog(t, second)
	changed.Advisories[0].Summary = "Different content at the same sequence"
	equalSequence := filepath.Join(f.directory, "equal-sequence.json")
	writeSecurityPublishJSON(t, equalSequence, changed)
	for _, catalog := range []string{equalSequence, first} {
		err := f.publish(catalog, 3, func(context.Context, securitymaintenance.Catalog, string) error {
			t.Fatal("refused catalog reached release verification")
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "does not advance past published sequence 2") {
			t.Fatalf("non-advancing catalog not refused clearly: %v", err)
		}
	}
	if after, err := os.ReadFile(timestampPath); err != nil || !slices.Equal(before, after) {
		t.Fatal("refused catalog changed the repository")
	}
	if err := f.publish(second, 3, func(_ context.Context, catalog securitymaintenance.Catalog, _ string) error {
		if len(catalog.Targets) != 0 {
			t.Fatalf("unchanged targets were verified again: %+v", catalog.Targets)
		}
		return nil
	}); err != nil {
		t.Fatalf("re-signing identical catalog bytes refused: %v", err)
	}
}

func TestSecurityFeedWithdrawalIsNotBlockedByUnavailableRelease(t *testing.T) {
	f := newFeedRepositoryFixture(t)
	first := f.advisory(t, "catalog-1.json", append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.1")...)
	if err := f.publish(first, 1, acceptFeedTargets); err != nil {
		t.Fatal(err)
	}
	withdrawal := filepath.Join(f.directory, "withdrawal.json")
	// No --cache: a withdrawal authors without any release access.
	if err := runSecurityFeedAdvisory(context.Background(), []string{"--output", withdrawal, "--previous-catalog", first, "--withdraw-target", "1.0.1"}, io.Discard, func(context.Context, string, string) (publicationVerifiedRelease, error) {
		t.Fatal("withdrawal resolved a release")
		return publicationVerifiedRelease{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.publish(withdrawal, 2, func(ctx context.Context, catalog securitymaintenance.Catalog, cache string) error {
		return verifySecurityPublicationTargets(ctx, catalog, cache, func(context.Context, releaseidentity.Identity, string) (publicationVerifiedRelease, error) {
			return publicationVerifiedRelease{}, errors.New("synthetic GitHub outage")
		})
	}); err != nil {
		t.Fatalf("withdrawal blocked by release availability: %v", err)
	}
}

func TestSecurityFeedRefreshBoundsAndRefusals(t *testing.T) {
	f := newFeedRepositoryFixture(t)
	now := time.Now()
	if err := f.refresh(now.Add(time.Hour), now.Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "run feed publish first") {
		t.Fatalf("refresh without a repository: %v", err)
	}
	if _, err := os.Lstat(f.output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused refresh created a repository")
	}
	first := f.advisory(t, "catalog-1.json", append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.1")...)
	if err := f.publish(first, 1, acceptFeedTargets); err != nil {
		t.Fatal(err)
	}
	timestampPath := filepath.Join(f.output, "metadata/timestamp.json")
	before, err := os.ReadFile(timestampPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, expiry := range map[string][2]time.Time{
		"snapshot beyond targets": {now.Add(49 * time.Hour), now.Add(time.Hour)},
		"timestamp past snapshot": {now.Add(time.Hour), now.Add(2 * time.Hour)},
		"already expired":         {now.Add(time.Hour), now.Add(-time.Hour)},
	} {
		if err := f.refresh(expiry[0], expiry[1]); err == nil || !strings.Contains(err.Error(), "feed refresh expiry") {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if after, err := os.ReadFile(timestampPath); err != nil || !slices.Equal(before, after) {
		t.Fatal("refused refresh changed the timestamp")
	}

	// A tampered chain is a broken repository, never a first publication.
	targetsPath := filepath.Join(f.output, "metadata/1.targets.json")
	saved, err := os.ReadFile(targetsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(targetsPath); err != nil {
		t.Fatal(err)
	}
	if err := f.refresh(now.Add(time.Hour), now.Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "read published feed targets") {
		t.Fatalf("missing targets metadata accepted: %v", err)
	}
	if err := f.publish(first, 2, acceptFeedTargets); err == nil || !strings.Contains(err.Error(), "read published feed targets") {
		t.Fatalf("publish ignored a broken previous repository: %v", err)
	}
	if err := os.WriteFile(targetsPath, saved, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSecurityFeedOutputIsCheckedWithRealClient(t *testing.T) {
	f := newFeedRepositoryFixture(t)
	first := f.advisory(t, "catalog-1.json", append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.1")...)
	if err := f.publish(first, 1, acceptFeedTargets); err != nil {
		t.Fatal(err)
	}
	root, err := os.ReadFile(f.root)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyPublicationWithClient(context.Background(), f.output, root, catalog); err != nil {
		t.Fatal(err)
	}
	if err := verifyPublicationWithClient(context.Background(), f.output, root, []byte("other")); err == nil {
		t.Fatal("different catalog accepted")
	}
	stored := filepath.Join(f.output, "targets/security", string(releaseidentity.Hash(catalog))+".catalog.json")
	if err := os.WriteFile(stored, append(slices.Clone(catalog), ' '), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyPublicationWithClient(context.Background(), f.output, root, catalog); err == nil {
		t.Fatal("client verification accepted tampered target bytes")
	}
}

func TestSecurityFeedAdvisoryDerivesTargetsAndComputesFixes(t *testing.T) {
	f := newFeedRepositoryFixture(t)
	first := f.advisory(t, "catalog-1.json", append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.1", "--introduced", "1.0.0")...)
	previous := readFeedCatalog(t, first)
	if previous.Sequence != 1 || previous.ExpiresAt.Sub(previous.IssuedAt) != securitymaintenance.MaxCatalogLifetime || len(previous.Targets) != 2 {
		t.Fatalf("first catalog: %+v", previous)
	}
	release := feedFixtureRelease("1.0.1", 2)
	binary, image := previous.Targets[0], previous.Targets[1]
	if binary.Identity != release.Identity || image.Identity != release.Identity || binary.Image != "" || image.Image != "ghcr.io/hikyo-org/hikyo@"+release.Artifacts[3].Digest || !binary.Impact.Restart || !image.Impact.Restart || binary.RolloutMinutes != 0 || !binary.AvailableAt.IsZero() {
		t.Fatalf("records not derived from the signed release: %+v", previous.Targets)
	}
	// Three more advisories exercise each fix rule: withdrawn, unfixed by this
	// target, and fixed by it without anybody naming it.
	future := previous.Advisories[0]
	future.ID, future.Affected = "CVE-2099-FUTURE", []securitymaintenance.Affected{{Profile: releaseidentity.StableV1, Fixed: "2.0.0", Platforms: []string{"linux/amd64"}}}
	withdrawn := previous.Advisories[0]
	withdrawn.ID, withdrawn.Withdrawn = "CVE-2099-WITHDRAWN", true
	previous.Advisories = append(previous.Advisories, future, withdrawn)
	prior := filepath.Join(f.directory, "prior.json")
	writeSecurityPublishJSON(t, prior, previous)
	next := f.advisory(t, "catalog-2.json", "--previous-catalog", prior, "--id", "CVE-2099-0002", "--summary", "Second flaw", "--severity", "high", "--component", "hikyo", "--platform", "linux/amd64", "--platform", "darwin/arm64", "--fixed-version", "1.0.2", "--schema", "--maintenance", "--rollout-minutes", "60")
	catalog := readFeedCatalog(t, next)
	if catalog.Sequence != 2 || len(catalog.Advisories) != 4 || len(catalog.Targets) != 4 {
		t.Fatalf("carry over: %+v", catalog)
	}
	added, addedImage := catalog.Targets[2], catalog.Targets[3]
	if !slices.Equal(added.Fixes, []string{"CVE-2099-0001", "CVE-2099-0002"}) || !slices.Equal(addedImage.Fixes, added.Fixes) {
		t.Fatalf("fixes not computed: %v %v", added.Fixes, addedImage.Fixes)
	}
	if !slices.Equal(added.Platforms, []string{"linux/amd64", "darwin/arm64"}) || !slices.Equal(addedImage.Platforms, []string{"linux/amd64"}) || addedImage.Image == "" {
		t.Fatalf("delivery platforms: %+v %+v", added, addedImage)
	}
	if !added.Impact.Restart || !added.Impact.Schema || !added.Impact.Maintenance || added.Impact.Features || added.RolloutMinutes != 60 || added.AvailableAt.IsZero() {
		t.Fatalf("impact or rollout: %+v", added)
	}
	// A later advisory fixed by the same release extends its records in place.
	third := f.advisory(t, "catalog-3.json", "--previous-catalog", next, "--id", "CVE-2099-0003", "--summary", "Third flaw", "--severity", "low", "--component", "hikyo", "--platform", "linux/amd64", "--fixed-version", "1.0.2", "--features")
	updated := readFeedCatalog(t, third)
	if len(updated.Targets) != 4 || !slices.Contains(updated.Targets[2].Fixes, "CVE-2099-0003") || !updated.Targets[2].Impact.Features || !updated.Targets[2].Impact.Schema || updated.Targets[2].RolloutMinutes != 60 {
		t.Fatalf("existing release records not extended: %+v", updated.Targets)
	}
	// With no changes the command renews the catalog, even after it expired.
	renewal := readFeedCatalog(t, f.advisory(t, "renewal.json", "--previous-catalog", third))
	if renewal.Sequence != 4 || len(renewal.Targets) != 4 || !renewal.ExpiresAt.After(time.Now()) {
		t.Fatalf("renewal: %+v", renewal)
	}
	empty := readFeedCatalog(t, f.advisory(t, "empty.json"))
	if empty.Sequence != 1 || empty.Advisories == nil || empty.Targets == nil {
		t.Fatalf("empty first catalog: %+v", empty)
	}
}

// An advisory fixed by an older release is also fixed by the newer targets already listed.
func TestSecurityFeedAdvisoryAddsNewFixesToExistingTargets(t *testing.T) {
	f := newFeedRepositoryFixture(t)
	newer := f.advisory(t, "newer.json", "--id", "CVE-2099-0002", "--summary", "Later flaw", "--severity", "high", "--component", "hikyo", "--platform", "linux/amd64", "--fixed-version", "1.0.2")
	catalog := readFeedCatalog(t, f.advisory(t, "older.json", append([]string{"--previous-catalog", newer}, append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.1")...)...))
	for _, target := range catalog.Targets {
		want := []string{"CVE-2099-0001"}
		if target.Identity.Version == "1.0.2" {
			want = []string{"CVE-2099-0002", "CVE-2099-0001"}
		}
		if !slices.Equal(target.Fixes, want) {
			t.Fatalf("target %s image=%t fixes %v, want %v", target.Identity.Version, target.Image != "", target.Fixes, want)
		}
	}
}

func TestSecurityFeedAdvisoryRerunKeepsOtherProfiles(t *testing.T) {
	f := newFeedRepositoryFixture(t)
	first := f.advisory(t, "catalog-1.json", append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.1", "--introduced", "1.0.0")...)
	previous := readFeedCatalog(t, first)
	nightly := securitymaintenance.Affected{Profile: releaseidentity.NightlyV1, Fixed: "1.1.0-nightly.5", Platforms: []string{"linux/amd64"}}
	previous.Advisories[0].Affected = append(previous.Advisories[0].Affected, nightly)
	prior := filepath.Join(f.directory, "prior.json")
	writeSecurityPublishJSON(t, prior, previous)
	next := f.advisory(t, "catalog-2.json", append([]string{"--previous-catalog", prior}, append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.1", "--introduced", "1.0.0")...)...)
	affected := readFeedCatalog(t, next).Advisories[0].Affected
	if len(affected) != 2 || !slices.ContainsFunc(affected, func(a securitymaintenance.Affected) bool {
		return a.Profile == releaseidentity.NightlyV1 && a.Fixed == nightly.Fixed
	}) {
		t.Fatalf("rerun erased another profile's range: %+v", affected)
	}
}

func TestSecurityFeedAdvisoryErrorsAreSpecific(t *testing.T) {
	f := newFeedRepositoryFixture(t)
	first := f.advisory(t, "catalog-1.json", append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.1")...)
	withdrawn := f.advisory(t, "withdrawn.json", "--previous-catalog", first, "--withdraw-target", "1.0.1")
	retracted := readFeedCatalog(t, f.advisory(t, "retracted.json", "--previous-catalog", first, "--withdraw-advisory", "CVE-2099-0001"))
	if len(retracted.Advisories) != 1 || retracted.Advisories[0].ID != "CVE-2099-0001" || !retracted.Advisories[0].Withdrawn || retracted.Sequence <= readFeedCatalog(t, first).Sequence {
		t.Fatalf("existing advisory was not withdrawn: %+v", retracted.Advisories)
	}
	base := []string{"--id", "A", "--summary", "S", "--severity", "high", "--component", "hikyo", "--platform", "linux/amd64", "--fixed-version", "1.0.2", "--cache", f.cache}
	without := func(name string) []string {
		index := slices.Index(base, name)
		return slices.Delete(slices.Clone(base), index, index+2)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{without("--summary"), "requires --summary"},
		{without("--severity"), "requires --severity"},
		{without("--component"), "requires --component"},
		{without("--platform"), "requires --platform"},
		{without("--fixed-version"), "requires --fixed-version"},
		{without("--cache"), "requires --cache"},
		{append(slices.Clone(base[:4]), "--severity", "urgent", "--component", "hikyo", "--platform", "linux/amd64", "--fixed-version", "1.0.2", "--cache", f.cache), "must be low, moderate, high or critical"},
		{[]string{"--summary", "orphan"}, "require --id"},
		{[]string{"--features"}, "require --id"},
		{[]string{"--withdraw-target", "1.0.1"}, "requires --previous-catalog"},
		{[]string{"--previous-catalog", first, "--withdraw-target", "9.9.9"}, "--withdraw-target 9.9.9 is not in"},
		{[]string{"--previous-catalog", first, "--withdraw-advisory", "absent"}, "--withdraw-advisory absent is not in"},
		{append(slices.Clone(base), "--previous-catalog", first, "--withdraw-target", "1.0.2"), "cannot both withdraw and target"},
		{append(slices.Clone(base), "--rollout-minutes", "10081"), "rollout_minutes must be 0..10080"},
		{append(without("--fixed-version"), "--fixed-version", "9.9.9"), "synthetic release is unavailable"},
		{append(without("--platform"), "--platform", "windows/arm64"), "no signed binary for windows/arm64"},
		{append(without("--fixed-version"), "--fixed-version", "1.0.1", "--previous-catalog", withdrawn), "withdrawn and cannot be a fix"},
		{append(without("--fixed-version"), "--fixed-version", "1.0.2", "--introduced", "not-a-version"), "next security catalog"},
		{append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.2", "--cache", f.cache, "--previous-catalog", first, "--withdraw-advisory", "CVE-2099-0001"), "CVE-2099-0001 is withdrawn and cannot be extended"},
		{append(slices.Clone(criticalAdvisoryArgs), "--fixed-version", "1.0.2", "--cache", f.cache, "--previous-catalog", f.advisory(t, "retracted-again.json", "--previous-catalog", first, "--withdraw-advisory", "CVE-2099-0001")), "CVE-2099-0001 is withdrawn and cannot be extended"},
	} {
		output := filepath.Join(f.directory, "refused.json")
		err := runSecurityFeedAdvisory(context.Background(), append([]string{"--output", output}, tc.args...), io.Discard, feedFixtureResolver)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%q: got %v, want %q", tc.args, err, tc.want)
		}
		if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%q wrote output", tc.args)
		}
	}
	// A moved fix must not leave a still-active target claiming the old one.
	moved := filepath.Join(f.directory, "moved.json")
	err := runSecurityFeedAdvisory(context.Background(), append([]string{"--output", moved, "--cache", f.cache, "--previous-catalog", first, "--fixed-version", "1.0.2"}, criticalAdvisoryArgs...), io.Discard, feedFixtureResolver)
	if err == nil || !strings.Contains(err.Error(), "1.0.1 claims a fix for CVE-2099-0001") {
		t.Fatalf("active bad target accepted: %v", err)
	}
	mismatch := func(context.Context, string, string) (publicationVerifiedRelease, error) {
		return feedFixtureRelease("1.0.1", 2), nil
	}
	if err := runSecurityFeedAdvisory(context.Background(), append([]string{"--output", moved}, base...), io.Discard, mismatch); err == nil || !strings.Contains(err.Error(), "differs from --fixed-version") {
		t.Fatalf("verified release mismatch accepted: %v", err)
	}
	if err := runSecurityFeedAdvisory(context.Background(), []string{"--output", first}, io.Discard, feedFixtureResolver); err == nil {
		t.Fatal("advisory replaced an existing file")
	}
	// The exported path derives targets only from production release trust.
	if err := RunSecurityFeed(context.Background(), append([]string{"advisory", "--output", moved}, base...), io.Discard); err == nil || !strings.Contains(err.Error(), "independent security release verification") {
		t.Fatalf("unstamped source build derived a target: %v", err)
	}
	if _, err := os.Lstat(moved); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused advisory wrote output")
	}
}
