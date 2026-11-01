package securityfeed

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

type fixture struct {
	root    []byte
	roles   map[string]RoleKeys
	signers map[string][]signature.Signer
}

func feedFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{roles: map[string]RoleKeys{}, signers: map[string][]signature.Signer{}}
	for _, role := range metadata.TOP_LEVEL_ROLE_NAMES {
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		signer, err := signature.LoadSigner(private, crypto.Hash(0))
		if err != nil {
			t.Fatal(err)
		}
		f.roles[role] = RoleKeys{PublicKeys: []crypto.PublicKey{public}, Threshold: 1}
		f.signers[role] = []signature.Signer{signer}
	}
	var err error
	f.root, err = CreateRoot(RootConfig{Version: 1, Expires: time.Now().Add(365 * 24 * time.Hour), Roles: f.roles, Signers: f.signers[metadata.ROOT]})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fixture) publication(t *testing.T, version uint64, data string, previous *Publication) *Publication {
	t.Helper()
	var previousTimestamp []byte
	if previous != nil {
		previousTimestamp = previous.files["metadata/timestamp.json"]
	}
	now := time.Now()
	p, err := BuildPublication(PublishConfig{Root: f.root, Catalog: []byte(data), PreviousTimestamp: previousTimestamp, Version: version, Signers: f.signers, Expires: Expirations{Targets: now.Add(24 * time.Hour), Snapshot: now.Add(2 * time.Hour), Timestamp: now.Add(time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func privateTemp(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func offlineRepository(t *testing.T, f fixture, p *Publication) (*Repository, Config, string) {
	t.Helper()
	directory := filepath.Join(privateTemp(t), "published")
	if err := p.Write(directory); err != nil {
		t.Fatal(err)
	}
	c := Config{StateDirectory: filepath.Join(privateTemp(t), "state"), TrustedRoot: f.root, OfflineDirectory: directory}
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	return r, c, directory
}

func TestOfflineRefreshVerifiesCatalogAndPersistsRollbackProtection(t *testing.T) {
	f := feedFixture(t)
	first := f.publication(t, 1, `{"sequence":1}`, nil)
	r, c, directory := offlineRepository(t, f, first)
	document, err := r.Refresh(context.Background())
	if err != nil || document.Version != 1 || string(document.Data) != `{"sequence":1}` || !document.Expires.After(time.Now()) {
		t.Fatalf("fresh catalog not verified: %+v %v", document, err)
	}
	second := f.publication(t, 2, `{"sequence":2}`, first)
	if err := second.Write(directory); err != nil {
		t.Fatal(err)
	}
	r, err = New(c)
	if err != nil {
		t.Fatal(err)
	}
	document, err = r.Refresh(context.Background())
	if err != nil || document.Version != 2 || string(document.Data) != `{"sequence":2}` {
		t.Fatalf("reopened trust did not advance: %+v %v", document, err)
	}
	if err := os.WriteFile(filepath.Join(directory, "metadata/timestamp.json"), first.files["metadata/timestamp.json"], 0644); err != nil {
		t.Fatal(err)
	}
	r, err = New(c)
	if err != nil {
		t.Fatal(err)
	}
	document, err = r.Refresh(context.Background())
	if err == nil || len(document.Data) != 0 {
		t.Fatalf("replayed timestamp produced a document: %+v %v", document, err)
	}
	third := f.publication(t, 3, `{"sequence":3}`, second)
	if err := third.Write(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("valid later publication did not recover after replay refusal: %v", err)
	}
}

func TestRefreshRejectsTamperedCatalogAndMetadata(t *testing.T) {
	for _, corruption := range []string{"target", "metadata"} {
		t.Run(corruption, func(t *testing.T) {
			f := feedFixture(t)
			p := f.publication(t, 1, `{"sequence":1}`, nil)
			r, _, directory := offlineRepository(t, f, p)
			name := "metadata/1.targets.json"
			if corruption == "target" {
				name = "targets/security/" + digest([]byte(`{"sequence":1}`)) + ".catalog.json"
			}
			if err := os.WriteFile(filepath.Join(directory, name), []byte("tampered"), 0644); err != nil {
				t.Fatal(err)
			}
			document, err := r.Refresh(context.Background())
			if err == nil || len(document.Data) != 0 {
				t.Fatalf("tampered %s accepted: %+v %v", corruption, document, err)
			}
		})
	}
}

func TestOfflineRefreshRejectsExpiredSignedTimestamp(t *testing.T) {
	f := feedFixture(t)
	p := f.publication(t, 1, "{}", nil)
	timestamp, err := metadata.Timestamp().FromBytes(p.files["metadata/timestamp.json"])
	if err != nil {
		t.Fatal(err)
	}
	timestamp.Signed.Expires = time.Now().Add(-time.Hour)
	timestamp.ClearSignatures()
	if err := sign(timestamp, f.signers[metadata.TIMESTAMP]); err != nil {
		t.Fatal(err)
	}
	p.files["metadata/timestamp.json"], err = timestamp.ToBytes(false)
	if err != nil {
		t.Fatal(err)
	}
	r, _, _ := offlineRepository(t, f, p)
	if document, err := r.Refresh(context.Background()); err == nil || len(document.Data) != 0 {
		t.Fatalf("expired offline metadata accepted: %+v %v", document, err)
	}
}

func TestRefreshRetainsRotatedRootAcrossReopen(t *testing.T) {
	f := feedFixture(t)
	first := f.publication(t, 1, "one", nil)
	r, c, directory := offlineRepository(t, f, first)
	if _, err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := feedFixture(t)
	rootSigners := append(slices.Clone(f.signers[metadata.ROOT]), next.signers[metadata.ROOT]...)
	rotated, err := CreateRoot(RootConfig{Version: 2, Expires: time.Now().Add(365 * 24 * time.Hour), Roles: next.roles, Signers: rootSigners, PreviousRoot: f.root})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	second, err := BuildPublication(PublishConfig{Root: rotated, PreviousRoot: f.root, Catalog: []byte("two"), PreviousTimestamp: first.files["metadata/timestamp.json"], Version: 2, Signers: next.signers, Expires: Expirations{Targets: now.Add(24 * time.Hour), Snapshot: now.Add(2 * time.Hour), Timestamp: now.Add(time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Write(directory); err != nil {
		t.Fatal(err)
	}
	if document, err := r.Refresh(context.Background()); err != nil || string(document.Data) != "two" {
		t.Fatalf("consecutive root/key rotation failed: %+v %v", document, err)
	}
	r, err = New(c)
	if err != nil {
		t.Fatal(err)
	}
	if document, err := r.Refresh(context.Background()); err != nil || string(document.Data) != "two" {
		t.Fatalf("bootstrap root replaced durable rotated root: %+v %v", document, err)
	}
	c.TrustedRoot = rotated
	r, err = New(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Refresh(context.Background()); err == nil {
		t.Fatal("changed configured bootstrap silently reset an existing installation")
	}
}

func TestHTTPSRefreshUsesStaticAnonymousRequestsAndHonorsCancellation(t *testing.T) {
	f := feedFixture(t)
	p := f.publication(t, 1, "catalog", nil)
	directory := filepath.Join(privateTemp(t), "published")
	if err := p.Write(directory); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	requests := 0
	files := http.FileServer(http.Dir(directory))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		if req.Method != http.MethodGet || req.URL.RawQuery != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.Header.Get("User-Agent") != "Hikyo-Security-Maintenance/1" || req.Header.Get("Cache-Control") != "no-cache" {
			t.Error("feed request disclosed or accepted installation-specific request metadata")
		}
		files.ServeHTTP(w, req)
	}))
	defer server.Close()
	c := Config{MetadataURL: server.URL + "/metadata", TargetsURL: server.URL + "/targets", StateDirectory: filepath.Join(privateTemp(t), "state"), TrustedRoot: f.root, Client: server.Client()}
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	if document, err := r.Refresh(context.Background()); err != nil || string(document.Data) != "catalog" {
		t.Fatalf("HTTPS catalog failed: %+v %v", document, err)
	}
	mu.Lock()
	count := requests
	mu.Unlock()
	if count != 5 {
		t.Fatalf("unexpected download count: %d", count)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled refresh ignored: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != count {
		t.Fatal("cancelled refresh contacted the publisher")
	}
}

func TestFeedRejectsRedirectsAndUnboundedDownloads(t *testing.T) {
	f := feedFixture(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "https://unexpected.invalid/root.json", http.StatusFound)
	}))
	defer server.Close()
	r, err := New(Config{MetadataURL: server.URL + "/metadata", TargetsURL: server.URL + "/targets", StateDirectory: filepath.Join(privateTemp(t), "state"), TrustedRoot: f.root, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("redirect not refused: %v", err)
	}
	fetcher := &boundedFetcher{ctx: context.Background(), metadataURL: server.URL + "/metadata/", targetsURL: server.URL + "/targets/", client: server.Client()}
	if _, err := fetcher.DownloadFile(server.URL+"/metadata/root.json", MaxDocumentBytes+1, 0); err == nil {
		t.Fatal("oversize authorized length accepted")
	}
	if _, err := fetcher.DownloadFile("https://unexpected.invalid/metadata/root.json", 1024, 0); err == nil {
		t.Fatal("unconfigured origin accepted")
	}
}

func TestRefreshRejectsSameVersionTimestampEquivocation(t *testing.T) {
	f := feedFixture(t)
	p := f.publication(t, 1, "catalog", nil)
	r, _, directory := offlineRepository(t, f, p)
	if _, err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	timestamp, err := metadata.Timestamp().FromBytes(p.files["metadata/timestamp.json"])
	if err != nil {
		t.Fatal(err)
	}
	timestamp.Signed.Expires = timestamp.Signed.Expires.Add(time.Minute)
	timestamp.ClearSignatures()
	if err := sign(timestamp, f.signers[metadata.TIMESTAMP]); err != nil {
		t.Fatal(err)
	}
	raw, err := timestamp.ToBytes(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "metadata/timestamp.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	if document, err := r.Refresh(context.Background()); err == nil || len(document.Data) != 0 || !strings.Contains(err.Error(), "equivocation") {
		t.Fatalf("same-version changed signed timestamp accepted: %+v %v", document, err)
	}
}

func TestHTTPSRefreshCancelsAnInFlightRequest(t *testing.T) {
	f := feedFixture(t)
	started := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		close(started)
		<-req.Context().Done()
	}))
	defer server.Close()
	r, err := New(Config{MetadataURL: server.URL + "/metadata", TargetsURL: server.URL + "/targets", StateDirectory: filepath.Join(privateTemp(t), "state"), TrustedRoot: f.root, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.Refresh(ctx)
		done <- err
	}()
	select {
	case <-started:
		cancel()
	case err := <-done:
		t.Fatalf("request did not reach the publisher: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("publisher did not receive request")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("in-flight cancellation lost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled request did not return")
	}
}

func TestStateDirectoryRejectsSymlinkAncestors(t *testing.T) {
	base := privateTemp(t)
	actual := filepath.Join(base, "actual")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	if err := privateDirectory(filepath.Join(link, "state")); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
	if _, err := os.Lstat(filepath.Join(actual, "state")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("state was created through a rejected symlink")
	}
}

func TestTrustStateRefusesMissingFloorSymlinksAndClockRollback(t *testing.T) {
	for _, mutation := range []string{"missing-floor", "root-symlink", "target-symlink", "clock-rollback"} {
		t.Run(mutation, func(t *testing.T) {
			f := feedFixture(t)
			p := f.publication(t, 1, "catalog", nil)
			r, c, _ := offlineRepository(t, f, p)
			if _, err := r.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "missing-floor":
				if err := os.Remove(filepath.Join(c.StateDirectory, "trust-floor.json")); err != nil {
					t.Fatal(err)
				}
			case "clock-rollback":
				path := filepath.Join(c.StateDirectory, "trust-floor.json")
				floor, err := loadFloor(path, c.TrustedRoot)
				if err != nil {
					t.Fatal(err)
				}
				floor.CheckedAt = time.Now().Add(time.Hour)
				if err := saveFloor(path, floor); err != nil {
					t.Fatal(err)
				}
			default:
				path := filepath.Join(c.StateDirectory, "metadata/root.json")
				if mutation == "target-symlink" {
					path = filepath.Join(c.StateDirectory, "targets/catalog.json")
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(privateTemp(t), "other"), path); err != nil {
					t.Fatal(err)
				}
			}
			if document, err := r.Refresh(context.Background()); err == nil || len(document.Data) != 0 {
				t.Fatalf("unsafe state %s accepted: %+v %v", mutation, document, err)
			}
		})
	}
}

func TestPublisherThresholdsAndImmutableVersions(t *testing.T) {
	f := feedFixture(t)
	roles := map[string]RoleKeys{}
	for role, keys := range f.roles {
		roles[role] = keys
	}
	roles[metadata.TIMESTAMP] = roles[metadata.ROOT]
	if _, err := CreateRoot(RootConfig{Version: 1, Expires: time.Now().Add(time.Hour), Roles: roles, Signers: f.signers[metadata.ROOT]}); err == nil {
		t.Fatal("shared root/online key accepted")
	}
	rootKeys := f.roles[metadata.ROOT]
	additional := feedFixture(t)
	rootKeys.PublicKeys = append(slices.Clone(rootKeys.PublicKeys), additional.roles[metadata.ROOT].PublicKeys...)
	rootKeys.Threshold = 2
	roles[metadata.TIMESTAMP] = f.roles[metadata.TIMESTAMP]
	roles[metadata.ROOT] = rootKeys
	if _, err := CreateRoot(RootConfig{Version: 1, Expires: time.Now().Add(time.Hour), Roles: roles, Signers: f.signers[metadata.ROOT]}); err == nil {
		t.Fatal("unsatisfied root threshold accepted")
	}
	first := f.publication(t, 1, "one", nil)
	directory := filepath.Join(privateTemp(t), "published")
	if err := first.Write(directory); err != nil {
		t.Fatal(err)
	}
	second := f.publication(t, 2, "two", first)
	for name, raw := range second.files {
		if strings.HasSuffix(name, ".targets.json") {
			if err := os.WriteFile(filepath.Join(directory, name), []byte("different prior bytes"), 0644); err != nil {
				t.Fatal(err)
			}
			_ = raw
		}
	}
	if err := second.Write(directory); err == nil {
		t.Fatal("immutable versioned metadata replaced")
	}
	raw, err := os.ReadFile(filepath.Join(directory, "metadata/timestamp.json"))
	if err != nil || !slices.Equal(raw, first.files["metadata/timestamp.json"]) {
		t.Fatal("failed publication changed live timestamp")
	}
	files := first.Files()
	files["metadata/timestamp.json"][0] ^= 1
	if slices.Equal(files["metadata/timestamp.json"], first.files["metadata/timestamp.json"]) {
		t.Fatal("publication Files leaked mutable internal bytes")
	}
}

func TestLoadSignerRequiresProtectedRegularLocalKey(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(privateTemp(t), "role.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSigner(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSigner(path); err == nil {
		t.Fatal("readable private signer accepted")
	}
}

func TestLoadRSASignerUsesTUFPSSScheme(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(privateTemp(t), "rsa-role.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), 0600); err != nil {
		t.Fatal(err)
	}
	signer, err := LoadSigner(path)
	if err != nil {
		t.Fatal(err)
	}
	public, err := metadata.KeyFromPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	root := metadata.Root(time.Now().Add(time.Hour))
	if err := root.Signed.AddKey(public, metadata.TARGETS); err != nil {
		t.Fatal(err)
	}
	targets := metadata.Targets(time.Now().Add(time.Hour))
	if _, err := targets.Sign(signer); err != nil {
		t.Fatal(err)
	}
	if err := root.VerifyDelegate(metadata.TARGETS, targets); err != nil {
		t.Fatalf("PEM RSA signer did not use TUF's required PSS scheme: %v", err)
	}
}

func TestReadPublicInputRequiresOwnedRegularAuthority(t *testing.T) {
	directory := privateTemp(t)
	path := filepath.Join(directory, "public.json")
	if err := os.WriteFile(path, []byte("public bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if raw, err := ReadPublicFile(path, 1024); err != nil || string(raw) != "public bytes" {
		t.Fatalf("ordinary protected public file rejected: %v", err)
	}
	link := filepath.Join(directory, "public-link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPublicFile(link, 1024); err == nil {
		t.Fatal("public-input symlink accepted")
	}
	parentLink := filepath.Join(directory, "parent-link")
	if err := os.Symlink(directory, parentLink); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPublicFile(filepath.Join(parentLink, "public.json"), 1024); err == nil {
		t.Fatal("public-input symlink ancestor accepted")
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPublicFile(path, 1024); err == nil {
		t.Fatal("writable public authority accepted")
	}
}

func TestConfigAndTrustFloorAreStrict(t *testing.T) {
	f := feedFixture(t)
	base := Config{MetadataURL: "https://updates.example/metadata/", TargetsURL: "https://updates.example/targets/", StateDirectory: filepath.Join(privateTemp(t), "state"), TrustedRoot: f.root}
	for _, raw := range []string{"http://updates.example/metadata", "https://user:password@updates.example/metadata", "https://updates.example/metadata?version=1.2", "https://updates.example/a/../metadata", "https://updates.example/%2fmetadata"} {
		c := base
		c.MetadataURL = raw
		if _, err := New(c); err == nil {
			t.Fatalf("unsafe publisher URL accepted: %q", raw)
		}
	}
	if err := privateDirectory(base.StateDirectory); err != nil {
		t.Fatal(err)
	}
	floor := trustFloor{Format: "hikyo.security-feed-trust/v1", BootstrapSHA256: digest(f.root), CheckedAt: time.Now(), Roles: map[string]roleFloor{}}
	raw, err := json.Marshal(floor)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw[:len(raw)-1], []byte(`,"unexpected":true}`)...)
	path := filepath.Join(base.StateDirectory, "trust-floor.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFloor(path, f.root); err == nil {
		t.Fatal("unknown trust state accepted")
	}
}
