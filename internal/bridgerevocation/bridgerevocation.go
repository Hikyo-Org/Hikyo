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
	"log/slog"
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
// GitHub's release asset hosts. It honors the proxy environment (HTTPS_PROXY): the
// request carries no credentials, a proxy-only host would otherwise see the endpoint as
// unreachable, and the signed content, not the path, is the authority.
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
	if errors.Is(err, errNotFound) {
		// Nothing published yet. Every bridge now carries a signed not_after, so expiry
		// still bounds a route; this is the same fallback as an unreachable endpoint.
		return nil, nil, fmt.Errorf("%w: the revocation list is not published: %w", ErrUnreachable, err)
	}
	if err != nil {
		return nil, nil, err
	}
	// A list without its signature is an answer that cannot be trusted, not an absence.
	signature, err := f.get(ctx, f.BaseURL+".sigstore.json")
	if err != nil {
		return nil, nil, err
	}
	return list, signature, nil
}

// errNotFound is a 404 for one asset; Fetch decides what it means.
var errNotFound = errors.New("revocation endpoint has no such asset")

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
	case http.StatusNotFound:
		return nil, fmt.Errorf("%w: status 404", errNotFound)
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
	// storeFile holds the accepted list and its signature as one document, replaced by
	// a single rename, so a crash never leaves a list beside another list's signature.
	storeFile = "bridge-revocations-store.json"
	// listFile and signatureFile are the earlier two-file form, still read (and removed
	// on the next store) so an installation's floor survives the upgrade.
	listFile      = "bridge-revocations.json"
	signatureFile = "bridge-revocations.sigstore.json"
	checkFile     = "bridge-revocations-check.json"
)

type storedList struct {
	List      []byte `json:"list"`
	Signature []byte `json:"signature"`
}

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
	unusable := errors.Is(err, ErrUnusableState)
	if err != nil && (!unusable || c.Mode == Off) {
		return Result{}, err
	}
	result := Result{Outcome: Disabled}
	if haveKnown {
		result.Sequence, result.Revoked = known.Sequence(), known.Revoked()
		if err := c.ensureFloor(known); err != nil {
			return Result{}, err
		}
	}
	if c.Mode == Off {
		return result, c.record(result)
	}
	list, signature, err := c.Fetcher.Fetch(ctx)
	if unusable && err != nil {
		// Only a fresh, verified online answer may replace unusable state.
		if errors.Is(err, ErrUnreachable) {
			return Result{}, fmt.Errorf("%w: the endpoint is unreachable, so nothing can replace it: %w", ErrUnusableState, err)
		}
		return Result{}, err
	}
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
	if unusable {
		if age > MaxListAge {
			return Result{}, fmt.Errorf("%w: the fetched list is stale and cannot replace it", ErrUnusableState)
		}
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

// ErrUnusableState marks stored revocation state that exists but does not verify. It is
// never a quiet "nothing stored": losing known revocations must not widen admission, so
// bridge routes refuse until a fresh verified online list replaces the state or the
// operator removes it explicitly.
var ErrUnusableState = errors.New("stored recovery bridge revocation list is unusable")

func (c Checker) unusable(cause error) error {
	return fmt.Errorf("%w (%w); bridge routes are refused. Restore access to the revocation endpoint so a valid list replaces it, or, as an explicit operator action, stop the service and remove %s (and any older %s and %s), accepting that revocations known only to that list are lost",
		ErrUnusableState, cause, filepath.Join(c.Directory, storeFile), listFile, signatureFile)
}

// loadKnown returns the stored list when it verifies against this snapshot. Nothing
// stored, or a list correctly signed but bound to another (rotated) root, is "none". Stored
// state that does not verify is ErrUnusableState.
func (c Checker) loadKnown(snapshot releasetrust.Snapshot) (releasetrust.VerifiedRevocations, bool, error) {
	list, signature, found, err := c.readStored()
	if err != nil || !found {
		return releasetrust.VerifiedRevocations{}, false, err
	}
	verified, err := snapshot.VerifyRevocations(list, signature)
	if errors.Is(err, releasetrust.ErrRevocationRoot) {
		slog.Warn("stored recovery bridge revocation list is bound to another trust root and is not applied", "error", err)
		return releasetrust.VerifiedRevocations{}, false, nil
	}
	if err != nil {
		return releasetrust.VerifiedRevocations{}, false, c.unusable(err)
	}
	return verified, true, nil
}

func parseStored(raw []byte) (list, signature []byte) {
	var stored storedList
	if definitions.DecodeStrict(raw, &stored) != nil {
		return nil, nil
	}
	return stored.List, stored.Signature
}

// readStored reads the combined store, else the earlier two files. Content that cannot
// be parsed comes back as found with empty bytes, which then fails verification.
func (c Checker) readStored() (list, signature []byte, found bool, err error) {
	raw, err := os.ReadFile(filepath.Join(c.Directory, storeFile))
	if err == nil {
		list, signature = parseStored(raw)
		return list, signature, true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, false, err
	}
	list, err = os.ReadFile(filepath.Join(c.Directory, listFile))
	if errors.Is(err, os.ErrNotExist) {
		// A store written and the pair removed between the two reads: look again, so a
		// concurrent migration never reads as "nothing stored".
		raw, err = os.ReadFile(filepath.Join(c.Directory, storeFile))
		if err == nil {
			list, signature = parseStored(raw)
			return list, signature, true, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, false, nil
		}
		return nil, nil, false, err
	}
	if err != nil {
		return nil, nil, false, err
	}
	signature, err = os.ReadFile(filepath.Join(c.Directory, signatureFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, false, err
	}
	return list, signature, true, nil
}

// persist replaces the stored list under an exclusive lock and only ever raises the
// stored sequence: Check compared against the list it read before the lock, so a
// concurrent run may have stored a higher one since. The list and its signature are
// one file, written by a single rename.
func (c Checker) persist(snapshot releasetrust.Snapshot, fresh releasetrust.VerifiedRevocations, list, signature []byte) error {
	release, err := c.lock()
	if err != nil {
		return err
	}
	defer release()
	known, haveKnown, err := c.loadKnown(snapshot)
	unusable := errors.Is(err, ErrUnusableState)
	if err != nil && !unusable {
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
	if unusable {
		// The stored list is gone, so the high-water floor is the only memory left. A
		// missing floor is a first run; an unreadable one is as unusable as the list.
		floor, found, floorErr := c.readFloor()
		if floorErr != nil {
			return fmt.Errorf("%w: the sequence floor record is unreadable: %w", ErrUnusableState, floorErr)
		}
		if found && fresh.Sequence() < floor.Sequence {
			return errors.New("revocation list rollback refused")
		}
		if found && fresh.Sequence() == floor.Sequence && fresh.Digest() != floor.Digest {
			return errors.New("revocation list equivocation refused")
		}
	}
	// The floor lands first: a crash between the two leaves a floor at or above the store.
	if err := c.raiseFloor(fresh.Sequence(), fresh.Digest()); err != nil {
		return err
	}
	raw, err := json.Marshal(storedList{List: list, Signature: signature}, jsonwire.Stable)
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(c.Directory, storeFile), raw); err != nil {
		return err
	}
	// The earlier two-file form is superseded; a leftover pair would only confuse a reader.
	_ = os.Remove(filepath.Join(c.Directory, listFile))
	_ = os.Remove(filepath.Join(c.Directory, signatureFile))
	return nil
}

const floorFile = "bridge-revocations-floor.json"

// floorRecord is the highest list this installation accepted. Only persist and ensureFloor
// write it, under the persistence lock, and never lower it; the outcome log is separate.
type floorRecord struct {
	Sequence int64                  `json:"sequence"`
	Digest   releaseidentity.Digest `json:"digest"`
}

func (c Checker) readFloor() (floorRecord, bool, error) {
	raw, err := os.ReadFile(filepath.Join(c.Directory, floorFile))
	if errors.Is(err, os.ErrNotExist) {
		return floorRecord{}, false, nil
	}
	if err != nil {
		return floorRecord{}, false, err
	}
	var floor floorRecord
	if err := definitions.DecodeStrict(raw, &floor); err != nil || floor.Sequence < 1 || floor.Digest.Validate() != nil {
		return floorRecord{}, false, errors.New("sequence floor record is malformed")
	}
	return floor, true, nil
}

// raiseFloor stores max(existing, new). The caller holds the lock. An unreadable existing
// record is replaced: the list being stored is verified and the old record is already lost.
func (c Checker) raiseFloor(sequence int64, digest releaseidentity.Digest) error {
	if existing, found, err := c.readFloor(); err == nil && found && existing.Sequence >= sequence {
		return nil
	}
	raw, err := json.Marshal(floorRecord{Sequence: sequence, Digest: digest}, jsonwire.Stable)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(c.Directory, floorFile), raw)
}

// ensureFloor gives an installation that stored its list before the floor existed a floor
// at that list's sequence, so losing the list later cannot lower it.
func (c Checker) ensureFloor(known releasetrust.VerifiedRevocations) error {
	if existing, found, err := c.readFloor(); err == nil && found && existing.Sequence >= known.Sequence() {
		return nil
	}
	release, err := c.lock()
	if err != nil {
		return err
	}
	defer release()
	return c.raiseFloor(known.Sequence(), known.Digest())
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
	if err := temporary.Sync(); err != nil {
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
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	// The rename is durable before the caller removes anything it supersedes.
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
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
