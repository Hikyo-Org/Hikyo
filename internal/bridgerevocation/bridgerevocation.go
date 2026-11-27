// Package bridgerevocation checks, at apply time, whether the recovery bridge a
// route uses has been revoked. The list is signed by the pinned recovery key and
// bound to the pinned root, so the endpoint is only a transport: it can withhold
// an answer but cannot forge or redirect one, nor roll back below the highest list
// this installation stored under that root (each root keeps its own floor; the
// first run under a root has none). An unreachable endpoint
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
	"strings"
	"time"

	"encoding/json/v2"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/gofrs/flock"
	"golang.org/x/net/http/httpproxy"
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
	return NewHTTPFetcherWithProxy(base, http.ProxyFromEnvironment)
}

// ProxyFrom resolves a proxy the way net/http does for the process environment, from the
// three values of any environment. A service that does not run with the operator's
// environment passes the operator's own file here.
func ProxyFrom(httpProxy, httpsProxy, noProxy string) func(*http.Request) (*url.URL, error) {
	resolve := (&httpproxy.Config{HTTPProxy: httpProxy, HTTPSProxy: httpsProxy, NoProxy: noProxy}).ProxyFunc()
	return func(request *http.Request) (*url.URL, error) { return resolve(request.URL) }
}

// NewHTTPFetcherWithProxy is NewHTTPFetcher with an explicit proxy resolver.
func NewHTTPFetcherWithProxy(base string, proxy func(*http.Request) (*url.URL, error)) (HTTPFetcher, error) {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return HTTPFetcher{}, errors.New("revocation endpoint must be an https URL")
	}
	dialer := &net.Dialer{Timeout: connectTimeout}
	client := &http.Client{
		Timeout:       requestTimeout,
		Transport:     &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: connectTimeout, Proxy: proxy},
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
	// lockWait overrides lockWait for tests.
	lockWait time.Duration
}

const (
	// storeFile holds the accepted list and its signature as one document, replaced by
	// a single rename, so a crash never leaves a list beside another list's signature.
	storeFile = "bridge-revocations-store.json"
	checkFile = "bridge-revocations-check.json"
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
// returns a result without a fresh list, and neither does while stored state is
// unusable.
func (c Checker) Check(ctx context.Context, snapshot releasetrust.Snapshot) (Result, error) {
	// This read is unlocked, so it can land between a concurrent persist's floor and store
	// writes and see the store behind the floor. It only seeds the result: whether the
	// state is unusable is decided by the locked re-read in newest or persist.
	st, err := c.load(snapshot)
	if err != nil {
		return Result{}, err
	}
	usable := st.have && st.unusable == nil
	result := Result{Outcome: Disabled}
	if usable {
		result.Sequence, result.Revoked = st.known.Sequence(), st.known.Revoked()
		if err := c.ensureFloor(snapshot, st.known); err != nil {
			return Result{}, err
		}
	}
	if c.Mode == Off {
		return c.newest(snapshot, result, nil)
	}
	list, signature, err := c.Fetcher.Fetch(ctx)
	if errors.Is(err, ErrUnreachable) {
		result.Outcome = Unreachable
		return c.newest(snapshot, result, err)
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
	if usable {
		if fresh.Sequence() < st.known.Sequence() {
			return Result{}, errors.New("revocation list rollback refused")
		}
		if fresh.Sequence() == st.known.Sequence() && fresh.Digest() != st.known.Digest() {
			return Result{}, errors.New("revocation list equivocation refused")
		}
	}
	// stored is the list the directory holds afterwards, read under the lock: a concurrent run
	// may have stored a newer one since this run read the directory, and only that list's
	// revocations may be reported, never an older list's. persist stores nothing when the
	// directory already holds this list or a newer one.
	stored, err := c.persist(snapshot, fresh, list, signature)
	if err != nil {
		return Result{}, err
	}
	outcome := Checked
	if now.Sub(stored.IssuedAt()) > MaxListAge {
		outcome = Stale
	}
	return Result{Outcome: outcome, Sequence: stored.Sequence(), Revoked: stored.Revoked()}, c.record(Result{Outcome: outcome, Sequence: stored.Sequence()})
}

// newest finishes a result that carries no fresh list (opt-out, or unreachable, the fetch
// error): the store is read again under the lock, which alone decides whether it is unusable,
// and a newer list a concurrent run stored since this run read the directory is reported.
func (c Checker) newest(snapshot releasetrust.Snapshot, result Result, unreachable error) (Result, error) {
	release, err := c.lock()
	if err != nil {
		return Result{}, err
	}
	st, err := c.load(snapshot)
	release()
	if err != nil {
		return Result{}, err
	}
	if st.unusable != nil {
		if unreachable != nil {
			// Only a verified list fetched online may replace unusable state.
			return Result{}, c.unusable(fmt.Errorf("%w; the endpoint is unreachable, so nothing can replace it: %w", st.unusable, unreachable))
		}
		return Result{}, c.unusable(st.unusable)
	}
	if st.have && st.known.Sequence() > result.Sequence {
		result.Sequence, result.Revoked = st.known.Sequence(), st.known.Revoked()
	}
	return result, c.record(result)
}

// Revoked adapts Check to the gate's hook: the revoked digests, or the error
// that refuses the bridge.
func (c Checker) Revoked(ctx context.Context, snapshot releasetrust.Snapshot) ([]releaseidentity.Digest, error) {
	result, err := c.Check(ctx, snapshot)
	return result.Revoked, err
}

// ErrUnusableState marks stored revocation state that cannot be relied on: it exists but
// does not verify, it is bound to another trust root, it is behind the sequence floor, or the
// floor is unreadable. It is never a quiet "nothing stored": losing known revocations must
// not widen admission, so bridge routes refuse until a verified online list, whatever its age,
// at or above a readable floor replaces the state, or the operator removes it explicitly.
var ErrUnusableState = errors.New("stored recovery bridge revocation state is unusable")

// errFloorUnreadable marks a floor record that exists but cannot be read. No list can replace
// that state, because storing one first needs the floor.
var errFloorUnreadable = errors.New("the sequence floor record is unreadable")

func (c Checker) unusable(cause error) error {
	store, floor := filepath.Join(c.Directory, storeFile), filepath.Join(c.Directory, floorFile)
	if errors.Is(cause, errFloorUnreadable) {
		return fmt.Errorf("%w (%w); bridge routes are refused, and no fetched list can replace this while the floor record cannot be read. As an explicit operator action, stop the service and repair or remove %s (the next verified list recreates it), accepting that the recorded sequence floor of every trust root is lost, so the next check accepts any validly signed list the endpoint serves, even an older one",
			ErrUnusableState, cause, floor)
	}
	way := "Restore access to the revocation endpoint so a verified list at or above this trust root's sequence floor replaces it, whatever its age"
	if c.Mode == Off {
		way = "The check is off, so nothing is fetched: set HIKYO_UPGRADE_REVOCATION_CHECK to online so a verified list at or above this trust root's sequence floor replaces it, whatever its age"
	}
	return fmt.Errorf("%w (%w); bridge routes are refused. %s, or, as an explicit operator action, stop the service and remove %s and %s, accepting that revocations known only to that state and the sequence floor of every trust root are lost",
		ErrUnusableState, cause, way, store, floor)
}

// state is what the directory holds: the stored list and this snapshot's root's floor
// (other roots' floors never apply). unusable is non-nil when the stored list cannot be
// relied on.
type state struct {
	known     releasetrust.VerifiedRevocations
	have      bool
	floor     floorRecord
	haveFloor bool
	unusable  error
}

// load reads the stored list and this root's floor and decides whether they agree. A floor
// with no matching stored list (removed store, or a run under another root stored its own
// list), a list behind the floor
// (a crash between the two writes) and a list that differs from the floor at its sequence
// are all unusable: the floor proves a higher list was accepted, so its revocations can no
// longer be honoured. Only a first run under this root (no list stored for it and no floor
// for it) is "nothing stored".
func (c Checker) load(snapshot releasetrust.Snapshot) (state, error) {
	var st state
	list, signature, found, err := c.readStored()
	if err != nil {
		return st, err
	}
	if found {
		verified, verr := snapshot.VerifyRevocations(list, signature)
		if verr != nil {
			// A list correctly signed but bound to another root (ErrRevocationRoot) was never
			// issued for this root: neither its revocations nor their absence can be applied.
			st.unusable = verr
		} else {
			st.known, st.have = verified, true
		}
	}
	floor, haveFloor, floorErr := c.readFloor(snapshot.RevocationRoot())
	switch {
	case floorErr != nil:
		// Keep the floor error beside a store error: the floor is what the operator must repair.
		if st.unusable == nil {
			st.unusable = fmt.Errorf("%w: %w", errFloorUnreadable, floorErr)
		} else {
			st.unusable = fmt.Errorf("%w; %w: %w", st.unusable, errFloorUnreadable, floorErr)
		}
	case haveFloor:
		st.floor, st.haveFloor = floor, true
	}
	if st.unusable != nil || !st.haveFloor {
		return st, nil
	}
	switch {
	case !st.have:
		st.unusable = fmt.Errorf("no stored list for this trust root, but the floor records sequence %d", st.floor.Sequence)
	case st.have && st.known.Sequence() < st.floor.Sequence:
		st.unusable = fmt.Errorf("the stored list (sequence %d) is behind the floor (sequence %d)", st.known.Sequence(), st.floor.Sequence)
	case st.have && st.known.Sequence() == st.floor.Sequence && st.known.Digest() != st.floor.Digest:
		st.unusable = fmt.Errorf("the stored list differs from the one the floor recorded at sequence %d", st.floor.Sequence)
	}
	return st, nil
}

func parseStored(raw []byte) (list, signature []byte) {
	var stored storedList
	if definitions.DecodeStrict(raw, &stored) != nil {
		return nil, nil
	}
	return stored.List, stored.Signature
}

// readStored reads the store. Content that cannot be parsed comes back as found with
// empty bytes, which then fails verification.
func (c Checker) readStored() (list, signature []byte, found bool, err error) {
	raw, err := os.ReadFile(filepath.Join(c.Directory, storeFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	list, signature = parseStored(raw)
	return list, signature, true, nil
}

// persist replaces the stored list under an exclusive lock and only ever raises the
// stored sequence, and returns the list the directory holds afterwards (a newer one stored by
// a concurrent run is returned as it is): Check compared against the list it read before the lock, so a
// concurrent run may have stored a higher or a different one since. The list and its
// signature are one file, written by a single rename. Whether a list may replace unusable
// state is decided here, from the read under the lock: any verified list, whatever its age
// (lists are signed only when something is revoked), at or above a readable floor and, at the
// floor's sequence, with the floor's digest.
func (c Checker) persist(snapshot releasetrust.Snapshot, fresh releasetrust.VerifiedRevocations, list, signature []byte) (releasetrust.VerifiedRevocations, error) {
	release, err := c.lock()
	if err != nil {
		return releasetrust.VerifiedRevocations{}, err
	}
	defer release()
	st, err := c.load(snapshot)
	if err != nil {
		return releasetrust.VerifiedRevocations{}, err
	}
	if st.unusable == nil && st.have {
		if st.known.Sequence() == fresh.Sequence() && st.known.Digest() != fresh.Digest() {
			return releasetrust.VerifiedRevocations{}, errors.New("revocation list equivocation refused")
		}
		if st.known.Sequence() >= fresh.Sequence() {
			return st.known, nil
		}
	}
	if st.unusable != nil {
		// The stored list cannot be relied on, so the floor is the memory left. A missing floor
		// is a first run; an unreadable one is as unusable as the list.
		floor, found := st.floor, st.haveFloor
		if !found {
			if _, _, floorErr := c.readFloor(snapshot.RevocationRoot()); floorErr != nil {
				return releasetrust.VerifiedRevocations{}, c.unusable(fmt.Errorf("%w; %w: %w", st.unusable, errFloorUnreadable, floorErr))
			}
		}
		if found && fresh.Sequence() < floor.Sequence {
			return releasetrust.VerifiedRevocations{}, fmt.Errorf("revocation list rollback refused: sequence %d is below this trust root's floor (sequence %d)", fresh.Sequence(), floor.Sequence)
		}
		if found && fresh.Sequence() == floor.Sequence && fresh.Digest() != floor.Digest {
			return releasetrust.VerifiedRevocations{}, errors.New("revocation list equivocation refused")
		}
	}
	// The floor lands first: a crash between the two leaves a floor at or above the store.
	if err := c.raiseFloor(snapshot, fresh.Sequence(), fresh.Digest()); err != nil {
		return releasetrust.VerifiedRevocations{}, err
	}
	raw, err := json.Marshal(storedList{List: list, Signature: signature}, jsonwire.Stable)
	if err != nil {
		return releasetrust.VerifiedRevocations{}, err
	}
	if err := writeAtomic(filepath.Join(c.Directory, storeFile), raw); err != nil {
		return releasetrust.VerifiedRevocations{}, err
	}
	return fresh, nil
}

const floorFile = "bridge-revocations-floor.json"

// floorRecord is the highest list this installation accepted under one trust root. Only
// persist and ensureFloor write it, under the persistence lock, and never lower it; the
// outcome log is separate.
type floorRecord struct {
	Sequence int64                  `json:"sequence"`
	Digest   releaseidentity.Digest `json:"digest"`
	Root     releaseidentity.Digest `json:"root"`
}

// floorSet is the floor file: one record per trust root this installation has stored a list
// under, so a run under one root never lowers or rebinds another root's floor.
type floorSet struct {
	Floors []floorRecord `json:"floors"`
}

// readFloors reads every root's floor. A file holding one bare record is the earlier
// single-slot form, read as the floor of the root it records.
func (c Checker) readFloors() ([]floorRecord, error) {
	raw, err := os.ReadFile(filepath.Join(c.Directory, floorFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var set floorSet
	if definitions.DecodeStrict(raw, &set) != nil {
		var single floorRecord
		if definitions.DecodeStrict(raw, &single) != nil {
			return nil, errors.New("sequence floor record is malformed")
		}
		set.Floors = []floorRecord{single}
	}
	roots := map[releaseidentity.Digest]bool{}
	for _, floor := range set.Floors {
		if floor.Sequence < 1 || floor.Digest.Validate() != nil || floor.Root.Validate() != nil || roots[floor.Root] {
			return nil, errors.New("sequence floor record is malformed")
		}
		roots[floor.Root] = true
	}
	if len(set.Floors) == 0 {
		return nil, errors.New("sequence floor record is malformed")
	}
	return set.Floors, nil
}

// readFloor is root's floor; another root's record never applies to it.
func (c Checker) readFloor(root releaseidentity.Digest) (floorRecord, bool, error) {
	floors, err := c.readFloors()
	if err != nil {
		return floorRecord{}, false, err
	}
	if i := slices.IndexFunc(floors, func(floor floorRecord) bool { return floor.Root == root }); i >= 0 {
		return floors[i], true, nil
	}
	return floorRecord{}, false, nil
}

// raiseFloor stores max(existing, new) for the snapshot's root and keeps every other root's
// record as it is. The caller holds the lock. An unreadable or malformed file is an error:
// load and persist refuse before they get here, so overwriting it would only hide a lost floor.
func (c Checker) raiseFloor(snapshot releasetrust.Snapshot, sequence int64, digest releaseidentity.Digest) error {
	root := snapshot.RevocationRoot()
	floors, err := c.readFloors()
	if err != nil {
		return err
	}
	raised := floorRecord{Sequence: sequence, Digest: digest, Root: root}
	if i := slices.IndexFunc(floors, func(floor floorRecord) bool { return floor.Root == root }); i < 0 {
		floors = append(floors, raised)
	} else if floors[i].Sequence >= sequence {
		return nil
	} else {
		floors[i] = raised
	}
	slices.SortFunc(floors, func(a, b floorRecord) int { return strings.Compare(string(a.Root), string(b.Root)) })
	raw, err := json.Marshal(floorSet{Floors: floors}, jsonwire.Stable)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(c.Directory, floorFile), raw)
}

// ensureFloor restores a missing floor for this root (none recorded yet, or the file is
// absent) from the stored list, so losing the list later cannot lower it.
func (c Checker) ensureFloor(snapshot releasetrust.Snapshot, known releasetrust.VerifiedRevocations) error {
	if existing, found, err := c.readFloor(snapshot.RevocationRoot()); err == nil && found && existing.Sequence >= known.Sequence() {
		return nil
	}
	release, err := c.lock()
	if err != nil {
		return err
	}
	defer release()
	return c.raiseFloor(snapshot, known.Sequence(), known.Digest())
}

const (
	lockFile     = ".bridge-revocations.lock"
	lockWait     = 10 * time.Second
	lockInterval = 5 * time.Millisecond
)

// lock takes the state directory's persistence lock with an operating system file lock.
// The system releases it when its holder exits or dies, so there is no stale lock to take
// over, and no path on which two runs both believe they hold it.
func (c Checker) lock() (func(), error) {
	file := flock.New(filepath.Join(c.Directory, lockFile))
	wait := lockWait
	if c.lockWait > 0 {
		wait = c.lockWait
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	locked, err := file.TryLockContext(ctx, lockInterval)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	if !locked {
		return nil, errors.New("revocation list state is locked by another run")
	}
	return func() { _ = file.Unlock() }, nil
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
