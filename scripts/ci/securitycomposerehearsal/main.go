// securitycomposerehearsal drives the real securitydeployment.Executor against a
// real Compose stack for scripts/ci/security-compose-rehearsal.sh. It also serves
// the fixture's signed releases at GitHub's hostnames, because the in-image stage
// command has no offline path. Every byte is still verified under fixture trust.
//
// For scripts/ci/security-feed-rehearsal.sh the same server also answers as ghcr.io
// (an in-memory registry holding the fixture's image layouts) and as hikyo.app (the
// feed directory a publisher wrote, at /security-feed/).
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: securitycomposerehearsal github|ca|observe|prepare|apply|reconcile ...")
	}
	var err error
	switch os.Args[1] {
	case "ca":
		err = writeAuthority(os.Args[2])
	case "totp":
		err = printTOTP(os.Args[2])
	case "github":
		err = serveGitHub(os.Args[2], os.Args[3], os.Args[4], os.Args[5:])
	case "observe", "prepare", "apply", "reconcile":
		err = deployment(os.Args[1], os.Args[2:])
	default:
		err = errors.New("unknown command")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

// deployment args: CONFIG [FIXTURE_LABEL IMAGE [fail-stop|kill-after-stop]]
func deployment(action string, args []string) error {
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var file struct {
		Deployment securitydeployment.Config `json:"deployment"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return err
	}
	// New runs the production authority checks before any fault is injected.
	executor, err := securitydeployment.New(file.Deployment)
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch action {
	case "observe":
		return report(executor.Observe(ctx))
	case "reconcile":
		observed, err := executor.Observe(ctx)
		if err != nil {
			return err
		}
		return executor.Reconcile(ctx, observed)
	}
	target, err := fixtureTarget(args[1], args[2])
	if err != nil {
		return err
	}
	if action == "prepare" {
		return report(executor.Prepare(ctx, target))
	}
	target.EligibilityUntil = time.Now().Add(time.Hour)
	if len(args) > 3 {
		runner := faultRunner{securitydeployment.CommandRunner{}, args[3]}
		executor, err = securitydeployment.NewWithDependencies(file.Deployment, runner, nil)
		if err != nil {
			return err
		}
	}
	return executor.Apply(ctx, target)
}

func report(observed securitydeployment.Observation, err error) error {
	if err != nil {
		return err
	}
	return json.MarshalEncode(jsontext.NewEncoder(os.Stdout, jsonwire.Stable), observed)
}

// fixtureTarget is the bypass: the coordinator's driver would derive this from a
// feed and release download, and this reads the fixture's exact identity instead.
func fixtureTarget(descriptor, image string) (securitydeployment.Target, error) {
	raw, err := os.ReadFile(descriptor)
	if err != nil {
		return securitydeployment.Target{}, err
	}
	var prepared struct{ Identity releaseidentity.Identity }
	if err := json.Unmarshal(raw, &prepared); err != nil {
		return securitydeployment.Target{}, err
	}
	return securitydeployment.Target{Identity: prepared.Identity, Image: image}, nil
}

type faultRunner struct {
	real  securitydeployment.Runner
	fault string
}

func (r faultRunner) Run(ctx context.Context, command securitydeployment.Command) ([]byte, error) {
	if !slices.Contains(command.Args, "stop") {
		return r.real.Run(ctx, command)
	}
	if r.fault == "fail-stop" {
		return nil, errors.New("rehearsal: injected stop failure")
	}
	output, err := r.real.Run(ctx, command)
	if r.fault == "kill-after-stop" {
		fmt.Fprintln(os.Stderr, "rehearsal: coordinator dying after stop, before up")
		os.Exit(137)
	}
	return output, err
}

func writeAuthority(directory string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "hikyo-1068-rehearsal CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	encoded, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "ca.key"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0644)
}

func serverCertificate(directory string) (tls.Certificate, error) {
	authorityPEM, err := os.ReadFile(filepath.Join(directory, "ca.crt"))
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM, err := os.ReadFile(filepath.Join(directory, "ca.key"))
	if err != nil {
		return tls.Certificate{}, err
	}
	block, _ := pem.Decode(authorityPEM)
	authority, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return tls.Certificate{}, err
	}
	block, _ = pem.Decode(keyPEM)
	authorityKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return tls.Certificate{}, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "github.com"}, DNSNames: []string{"api.github.com", "github.com", "raw.githubusercontent.com", "ghcr.io", "hikyo.app"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	raw, err := x509.CreateCertificate(rand.Reader, template, authority, &key.PublicKey, authorityKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{raw}, PrivateKey: key}, nil
}

// serveGitHub answers only the exact URLs the release client requests. Trust
// documents come from bundle-c, like a main branch that has moved past A and B.
// Optional extra: FEED_DIRECTORY IMAGE_REPOSITORY, which also serve hikyo.app and ghcr.io.
func serveGitHub(fixture, authority, listen string, extra []string) error {
	certificate, err := serverCertificate(authority)
	if err != nil {
		return err
	}
	public := filepath.Join(fixture, "public")
	directories := map[string]string{}
	for _, label := range []string{"a", "b", "c"} {
		raw, err := os.ReadFile(filepath.Join(fixture, "descriptor-"+label+".json"))
		if err != nil {
			return err
		}
		var prepared struct{ Identity releaseidentity.Identity }
		if err := json.Unmarshal(raw, &prepared); err != nil {
			return err
		}
		directories["v"+prepared.Identity.Version] = filepath.Join(public, "release-"+label)
	}
	trust := map[string]string{
		"nightly/policy.json": filepath.Join(public, "release-c", "nightly-policy.json"),
		"primary.pub":         filepath.Join(public, "bundle-c", "keys", "test-primary.pub"),
	}
	for _, name := range []string{"metadata.json", "metadata.sigstore.json", "catalog.json", "catalog.sigstore.json"} {
		trust[name] = filepath.Join(public, "bundle-c", name)
	}
	var feed http.Handler
	var images http.Handler
	if len(extra) == 2 {
		feed = http.StripPrefix("/security-feed/", http.FileServer(http.Dir(extra[0])))
		// A native installation has no images, so its repository is empty.
		if extra[1] != "" {
			// Blobs on disk: the images are tens of megabytes, and the engine's memory is small.
			blobs, err := os.MkdirTemp("", "hikyo-1068-registry-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(blobs)
			images = registry.New(registry.Logger(log.New(io.Discard, "", 0)), registry.WithBlobHandler(registry.NewDiskBlobHandler(blobs)))
			if err := pushImages(fixture, extra[1], images); err != nil {
				return err
			}
		}
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, found := strings.Cut(r.Host, ":")
		if !found {
			host = r.Host
		}
		log.Printf("%s %s%s", r.Method, host, r.URL.Path)
		switch {
		case host == "ghcr.io" && images != nil:
			images.ServeHTTP(w, r)
			return
		case host == "hikyo.app" && feed != nil && strings.HasPrefix(r.URL.Path, "/security-feed/"):
			w.Header().Set("Cache-Control", "no-store")
			feed.ServeHTTP(w, r)
			return
		}
		if host == "api.github.com" {
			w.Header().Set("Content-Type", "application/json")
		}
		switch {
		case host == "api.github.com" && strings.HasPrefix(r.URL.Path, "/repos/Hikyo-Org/Hikyo/releases/tags/"):
			tag := path.Base(r.URL.Path)
			if directories[tag] == "" {
				http.NotFound(w, r)
				return
			}
			release, err := githubRelease(tag, directories[tag])
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.MarshalEncode(jsontext.NewEncoder(w, jsonwire.Stable), release)
		case host == "api.github.com" && r.URL.Path == "/repos/Hikyo-Org/Hikyo/releases":
			var releases []any
			for tag, directory := range directories {
				release, err := githubRelease(tag, directory)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				releases = append(releases, release)
			}
			_ = json.MarshalEncode(jsontext.NewEncoder(w, jsonwire.Stable), releases)
		case host == "github.com" && strings.HasPrefix(r.URL.Path, "/Hikyo-Org/Hikyo/releases/download/"):
			parts := strings.Split(r.URL.Path, "/")
			file, ok := releaseAsset(directories[parts[len(parts)-2]], parts[len(parts)-1])
			if !ok {
				http.NotFound(w, r)
				return
			}
			http.ServeFile(w, r, file)
		case host == "raw.githubusercontent.com" && strings.HasPrefix(r.URL.Path, "/Hikyo-Org/Hikyo/refs/heads/main/release/trust/"):
			file := trust[strings.TrimPrefix(r.URL.Path, "/Hikyo-Org/Hikyo/refs/heads/main/release/trust/")]
			if file == "" {
				http.NotFound(w, r)
				return
			}
			http.ServeFile(w, r, file)
		default:
			http.NotFound(w, r)
		}
	})
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, TLSConfig: &tls.Config{Certificates: []tls.Certificate{certificate}}, ReadHeaderTimeout: 10 * time.Second}
	return server.ServeTLS(listener, "", "")
}

// digests keeps each asset's SHA-256, computed by streaming once, because the
// release list is requested often and the archives are tens of megabytes.
var digests sync.Map

func fileDigest(path string, info os.FileInfo) (string, error) {
	key := fmt.Sprintf("%s/%d/%d", path, info.Size(), info.ModTime().UnixNano())
	if cached, ok := digests.Load(key); ok {
		return cached.(string), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	digests.Store(key, digest)
	return digest, nil
}

func githubRelease(tag, directory string) (map[string]any, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	assets := []map[string]any{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		digest, err := fileDigest(filepath.Join(directory, entry.Name()), info)
		if err != nil {
			return nil, err
		}
		assets = append(assets, map[string]any{"name": entry.Name(), "size": info.Size(), "digest": "sha256:" + digest,
			"browser_download_url": "https://github.com/Hikyo-Org/Hikyo/releases/download/" + tag + "/" + entry.Name()})
	}
	return map[string]any{"tag_name": tag, "prerelease": true, "draft": false, "immutable": true, "published_at": "2026-11-01T00:00:00Z", "assets": assets}, nil
}

type handlerTransport struct{ http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	if r.Body == nil { // A server always has a body, an empty one at least.
		r.Body = http.NoBody
	}
	t.ServeHTTP(recorder, r)
	response := recorder.Result()
	response.Request = r
	return response, nil
}

// pushImages loads each OCI layout the fixture wrote under images/ into the
// registry, tagged with the release version, without changing a byte.
func pushImages(fixture, repository string, images http.Handler) error {
	entries, err := os.ReadDir(filepath.Join(fixture, "images"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		index, err := layout.ImageIndexFromPath(filepath.Join(fixture, "images", entry.Name()))
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(filepath.Join(fixture, "descriptor-"+entry.Name()+".json"))
		if err != nil {
			return err
		}
		var prepared struct{ Identity releaseidentity.Identity }
		if err := json.Unmarshal(raw, &prepared); err != nil {
			return err
		}
		reference, err := name.ParseReference(repository + ":" + prepared.Identity.Version)
		if err != nil {
			return err
		}
		if err := remote.WriteIndex(reference, index, remote.WithTransport(handlerTransport{images})); err != nil {
			return err
		}
	}
	return nil
}

// printTOTP prints the code the authenticator behind an otpauth URI shows now, so
// the rehearsal can enrol a factor and sign in with it like a person would.
func printTOTP(uri string) error {
	key, err := otp.NewKeyFromURL(uri)
	if err != nil {
		return err
	}
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		return err
	}
	fmt.Println(code)
	return nil
}

// releaseAsset confines a download to one plain file name inside its release directory.
func releaseAsset(directory, name string) (string, bool) {
	if directory == "" || name == "." || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return "", false
	}
	return filepath.Join(directory, name), true
}
