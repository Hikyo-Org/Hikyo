// Package bridgerevocation checks, at apply time, whether the recovery bridge a
// route uses has been revoked. The list is signed by the pinned recovery key and
// bound to the pinned root, so the endpoint is only a transport: it can withhold
// an answer but cannot forge, roll back or redirect one. An unreachable endpoint
// falls back to the bridge's own signed expiry; a reachable endpoint that answers
// wrongly fails closed.
package bridgerevocation

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"time"

	"encoding/json/v2"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// DefaultURL is the pinned list location: the two assets (".json" and
// ".sigstore.json" are appended) of the fixed "trust" release in the Hikyo
// repository. The owner publishes them as part of the signing step; agents never
// do. The signed content, not the location, carries the authority.
const DefaultURL = "https://github.com/Hikyo-Org/Hikyo/releases/download/trust/recovery-revocations"

// releaseHosts are the only hosts a redirect may lead to: GitHub serves release
// assets by redirecting to its asset hosts. The signature, not the host, is the
// authority, so this only keeps the fetcher from being steered to arbitrary hosts.
var releaseHosts = map[string]bool{"github.com": true, "objects.githubusercontent.com": true, "release-assets.githubusercontent.com": true}

// followRelease follows at most two redirects, each to https on a release host.
func followRelease(request *http.Request, via []*http.Request) error {
	if len(via) > 2 || request.URL.Scheme != "https" || !releaseHosts[request.URL.Hostname()] {
		return http.ErrUseLastResponse
	}
	return nil
}

// MaxListAge is how old a list's signed issued_at may be and still count as a
// current answer. A replayed older list cannot be told apart from the current
// one by sequence on an installation that has stored nothing; its age can.
const MaxListAge = 7 * 24 * time.Hour

// maxFutureSkew bounds how far ahead of the host clock issued_at may be.
const maxFutureSkew = 24 * time.Hour

const (
	maxBody        = 64 << 10
	connectTimeout = 5 * time.Second
	requestTimeout = 10 * time.Second
)

// Mode selects the online check or the operator's explicit opt-out.
type Mode string

const (
	Online Mode = "online"
	Off    Mode = "off"
)

// ParseMode accepts the empty string as the default.
func ParseMode(value string) (Mode, error) {
	switch value {
	case "", "online":
		return Online, nil
	case "off":
		return Off, nil
	}
	return "", fmt.Errorf("revocation check must be online or off, not %q", value)
}

// Outcome is recorded locally for every check.
type Outcome string

const (
	Checked     Outcome = "checked"
	Unreachable Outcome = "unreachable-fell-back-to-expiry"
	Disabled    Outcome = "disabled-by-operator"
	// Stale is an authentic list issued longer ago than MaxListAge. Its entries
	// still apply, but it proves nothing about newer revocations, so the route
	// is bounded by the bridge's expiry exactly as when unreachable.
	Stale Outcome = "stale-fell-back-to-expiry"
)

// ErrUnreachable marks a transport failure: there was no authoritative answer.
var ErrUnreachable = errors.New("revocation endpoint unreachable")

// Fetcher returns the signed list and its detached signature.
type Fetcher interface {
	Fetch(ctx context.Context) (list, signature []byte, err error)
}

// HTTPFetcher reads <BaseURL>.json and <BaseURL>.sigstore.json over https.
type HTTPFetcher struct {
	BaseURL string
	Client  *http.Client
}

// NewHTTPFetcher refuses anything but an https URL and follows redirects only to
// GitHub's release asset hosts.
func NewHTTPFetcher(base string) (HTTPFetcher, error) {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return HTTPFetcher{}, errors.New("revocation endpoint must be an https URL")
	}
	dialer := &net.Dialer{Timeout: connectTimeout}
	client := &http.Client{
		Timeout:       requestTimeout,
		Transport:     &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: connectTimeout, Proxy: http.ProxyFromEnvironment},
		CheckRedirect: followRelease,
	}
	return HTTPFetcher{BaseURL: base, Client: client}, nil
}

func (f HTTPFetcher) Fetch(ctx context.Context) ([]byte, []byte, error) {
	list, err := f.get(ctx, f.BaseURL+".json")
	if err != nil {
		return nil, nil, err
	}
	signature, err := f.get(ctx, f.BaseURL+".sigstore.json")
	if err != nil {
		return nil, nil, err
	}
	return list, signature, nil
}

func (f HTTPFetcher) get(ctx context.Context, target string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	response, err := f.Client.Do(request)
	if err != nil {
		if transportFailure(err) {
			return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
		}
		return nil, fmt.Errorf("revocation endpoint answered unusably: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return nil, fmt.Errorf("%w: status %d", ErrUnreachable, response.StatusCode)
	default:
		return nil, fmt.Errorf("revocation endpoint answered status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		// A response began and then broke: a timeout is no answer, anything else
		// (a truncated body) is an answer that cannot be trusted.
		var network net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
			return nil, fmt.Errorf("%w: reading body: %w", ErrUnreachable, err)
		}
		return nil, fmt.Errorf("revocation response body unusable: %w", err)
	}
	if len(body) > maxBody {
		return nil, errors.New("revocation response exceeds its byte bound")
	}
	return body, nil
}

// transportFailure is true for errors before any HTTP response: name
// resolution, dialing, resets, EOF and timeouts. They mean "no authoritative
// answer". A certificate or TLS protocol failure means a server did answer,
// wrongly, and is not a fallback case.
func transportFailure(err error) bool {
	var unknownAuthority x509.UnknownAuthorityError
	var invalidCertificate x509.CertificateInvalidError
	var hostname x509.HostnameError
	var verification *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var alert tls.AlertError
	if errors.As(err, &unknownAuthority) || errors.As(err, &invalidCertificate) || errors.As(err, &hostname) || errors.As(err, &verification) || errors.As(err, &record) || errors.As(err, &alert) {
		return false
	}
	return true
}

// Result is what a check learned. Revoked is the verified list's contents from
// this fetch or, offline, from the last list this installation verified.
type Result struct {
	Outcome  Outcome
	Sequence int64
	Revoked  []releaseidentity.Digest
}

// Checker holds the fetcher and the local state paths.
type Checker struct {
	Mode      Mode
	Fetcher   Fetcher
	Directory string
	Now       func() time.Time
}

const (
	listFile      = "bridge-revocations.json"
	signatureFile = "bridge-revocations.sigstore.json"
	checkFile     = "bridge-revocations-check.json"
)

type checkRecord struct {
	Outcome  Outcome `json:"outcome"`
	Sequence int64   `json:"sequence"`
	At       string  `json:"at"`
}

// Check verifies the last list this installation accepted, then (unless the
// operator turned it off) asks the endpoint for a newer one. Any error means the
// bridge must be refused; only an unreachable endpoint or an explicit opt-out
// returns a result without a fresh list.
func (c Checker) Check(ctx context.Context, snapshot releasetrust.Snapshot) (Result, error) {
	known, haveKnown, err := c.loadKnown(snapshot)
	if err != nil {
		return Result{}, err
	}
	result := Result{Outcome: Disabled}
	if haveKnown {
		result.Sequence, result.Revoked = known.Sequence(), known.Revoked()
	}
	if c.Mode == Off {
		return result, c.record(result)
	}
	list, signature, err := c.Fetcher.Fetch(ctx)
	if errors.Is(err, ErrUnreachable) {
		result.Outcome = Unreachable
		return result, c.record(result)
	}
	if err != nil {
		return Result{}, err
	}
	fresh, err := snapshot.VerifyRevocations(list, signature)
	if err != nil {
		return Result{}, err
	}
	now := c.clock()
	age := now.Sub(fresh.IssuedAt())
	if age < -maxFutureSkew {
		return Result{}, errors.New("revocation list is issued in the future")
	}
	if haveKnown {
		if fresh.Sequence() < known.Sequence() {
			return Result{}, errors.New("revocation list rollback refused")
		}
		if fresh.Sequence() == known.Sequence() && fresh.Digest() != known.Digest() {
			return Result{}, errors.New("revocation list equivocation refused")
		}
	}
	if !haveKnown || fresh.Sequence() > known.Sequence() {
		if err := c.persist(snapshot, fresh, list, signature); err != nil {
			return Result{}, err
		}
	}
	outcome := Checked
	if age > MaxListAge {
		outcome = Stale
	}
	return Result{Outcome: outcome, Sequence: fresh.Sequence(), Revoked: fresh.Revoked()}, c.record(Result{Outcome: outcome, Sequence: fresh.Sequence()})
}

// Revoked adapts Check to the gate's hook: the revoked digests, or the error
// that refuses the bridge.
func (c Checker) Revoked(ctx context.Context, snapshot releasetrust.Snapshot) ([]releaseidentity.Digest, error) {
	result, err := c.Check(ctx, snapshot)
	return result.Revoked, err
}

func (c Checker) loadKnown(snapshot releasetrust.Snapshot) (releasetrust.VerifiedRevocations, bool, error) {
	list, err := os.ReadFile(filepath.Join(c.Directory, listFile))
	if errors.Is(err, os.ErrNotExist) {
		return releasetrust.VerifiedRevocations{}, false, nil
	}
	if err != nil {
		return releasetrust.VerifiedRevocations{}, false, err
	}
	signature, err := os.ReadFile(filepath.Join(c.Directory, signatureFile))
	if err != nil {
		return releasetrust.VerifiedRevocations{}, false, fmt.Errorf("stored revocation list lacks its signature: %w", err)
	}
	verified, err := snapshot.VerifyRevocations(list, signature)
	if err != nil {
		return releasetrust.VerifiedRevocations{}, false, fmt.Errorf("stored revocation list no longer verifies: %w", err)
	}
	return verified, true, nil
}

// persist replaces the stored list under an exclusive lock and only ever raises the
// stored sequence: Check compared against the list it read before the lock, so a
// concurrent run may have stored a higher one since. The signature lands first so a
// crash never leaves a list without the signature that authenticates it.
func (c Checker) persist(snapshot releasetrust.Snapshot, fresh releasetrust.VerifiedRevocations, list, signature []byte) error {
	release, err := c.lock()
	if err != nil {
		return err
	}
	defer release()
	known, haveKnown, err := c.loadKnown(snapshot)
	if err != nil {
		return err
	}
	if haveKnown {
		if known.Sequence() == fresh.Sequence() && known.Digest() != fresh.Digest() {
			return errors.New("revocation list equivocation refused")
		}
		if known.Sequence() >= fresh.Sequence() {
			return nil
		}
	}
	if err := writeAtomic(filepath.Join(c.Directory, signatureFile), signature); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(c.Directory, listFile), list)
}

const (
	lockFile     = ".bridge-revocations.lock"
	lockStale    = 30 * time.Second
	lockWait     = 10 * time.Second
	lockInterval = 5 * time.Millisecond
)

// lock takes the state directory's persistence lock by exclusive create, which is
// atomic on every supported platform. A lock older than lockStale belongs to a run
// that died and is taken over.
func (c Checker) lock() (func(), error) {
	path := filepath.Join(c.Directory, lockFile)
	deadline := time.Now().Add(lockWait)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStale {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("revocation list state is locked by another run")
		}
		time.Sleep(lockInterval)
	}
}

func (c Checker) clock() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c Checker) record(result Result) error {
	raw, err := json.Marshal(checkRecord{Outcome: result.Outcome, Sequence: result.Sequence, At: c.clock().UTC().Format(time.RFC3339)}, jsonwire.Stable)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(c.Directory, checkFile), raw)
}

func writeAtomic(path string, raw []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".revocation-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		_ = os.Remove(name)
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// LastCheck returns the recorded outcome of the previous check, for logs.
func LastCheck(directory string) (Outcome, int64, string, error) {
	raw, err := os.ReadFile(filepath.Join(directory, checkFile))
	if err != nil {
		return "", 0, "", err
	}
	var record checkRecord
	if err := definitions.DecodeStrict(raw, &record); err != nil {
		return "", 0, "", err
	}
	return record.Outcome, record.Sequence, record.At, nil
}

// Contains reports whether any used bridge digest is revoked.
func Contains(revoked, used []releaseidentity.Digest) bool {
	return slices.ContainsFunc(used, func(d releaseidentity.Digest) bool { return slices.Contains(revoked, d) })
}
