// Package vaultkv is the one-way Vault/OpenBao KV v2 synchronization adapter.
// Its provider interface deliberately cannot express a value read: metadata
// reads establish version and ownership, and nothing linked here can fetch
// destination plaintext (docs/adr/vault-kv-adapter.md).
package vaultkv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/netpolicy"
)

const (
	responseCap = 1 << 20
	// maxRenewals bounds token renewal to the module lease: a login token is
	// renewed at most this many times per outbox attempt and never by a
	// background daemon.
	maxRenewals = 2
	// renewMargin renews a token that would expire within this window, so a
	// provider write cannot start on a token about to lapse mid-request.
	renewMargin = 45 * time.Second
	// defaultRateBackoff applies when a 429 carries no usable Retry-After.
	defaultRateBackoff = 30 * time.Second
)

// Health is the value-free sys/health view.
type Health struct {
	Initialized bool
	Sealed      bool
	Version     string
}

// Mount is the value-free identity of a KV mount.
type Mount struct {
	Type     string
	Version  string
	UUID     string
	Accessor string
}

// TokenInfo is the value-free part of token lookup-self.
type TokenInfo struct {
	ExpireTime time.Time
}

// VersionMetadata is one version's lifecycle metadata. It never carries data.
type VersionMetadata struct {
	Deleted   bool
	Destroyed bool
}

// Metadata is the KV v2 metadata view of one path. It is decoded from
// GET /v1/{mount}/metadata/{path}, which returns versions and custom metadata
// but never secret data.
type Metadata struct {
	CurrentVersion int64
	CustomMetadata map[string]string
	Versions       map[int64]VersionMetadata
}

// API is the closed provider surface Sync can link. There is no data GET, no
// LIST, no version destroy, no metadata delete, and no sys/raw; the structural
// test pins this method set and the operation registry.
type API interface {
	Health(context.Context) (Health, error)
	MountInfo(ctx context.Context, mount string) (Mount, error)
	LookupSelf(context.Context) (TokenInfo, error)
	ReadMetadata(ctx context.Context, mount, path string) (Metadata, error)
	PatchCustomMetadata(ctx context.Context, mount, path string, custom map[string]*string) error
	WriteCAS(ctx context.Context, mount, path, value string, cas int64) (int64, error)
	DeleteVersion(ctx context.Context, mount, path string, version int64) error
}

type operation struct {
	Method        string
	Path          string
	Authenticated bool
	// RootOnly operations are served only by the root namespace (sys/health
	// refuses "operation unavailable in namespaces"), so they carry no
	// namespace header.
	RootOnly bool
}

// operationRegistry is the closed linked provider surface. Every request the
// client makes resolves through it; the structural test refuses any GET or
// LIST on a /data/ path and any destroy or metadata delete.
var operationRegistry = map[string]operation{
	"health":           {Method: http.MethodGet, Path: "/v1/sys/health?standbyok=true&perfstandbyok=true&sealedcode=200&uninitcode=200&drsecondarycode=200&performancestandbycode=200", RootOnly: true},
	"mount-info":       {Method: http.MethodGet, Path: "/v1/sys/internal/ui/mounts/{mount}", Authenticated: true},
	"approle-login":    {Method: http.MethodPost, Path: "/v1/auth/{auth_mount}/login"},
	"token-lookup":     {Method: http.MethodGet, Path: "/v1/auth/token/lookup-self", Authenticated: true},
	"token-renew":      {Method: http.MethodPost, Path: "/v1/auth/token/renew-self", Authenticated: true},
	"token-revoke":     {Method: http.MethodPost, Path: "/v1/auth/token/revoke-self", Authenticated: true},
	"read-metadata":    {Method: http.MethodGet, Path: "/v1/{mount}/metadata/{path}", Authenticated: true},
	"patch-metadata":   {Method: http.MethodPatch, Path: "/v1/{mount}/metadata/{path}", Authenticated: true},
	"write-cas":        {Method: http.MethodPost, Path: "/v1/{mount}/data/{path}", Authenticated: true},
	"soft-delete-data": {Method: http.MethodPost, Path: "/v1/{mount}/delete/{path}", Authenticated: true},
}

type ClientConfig struct {
	Origin       string
	Credential   string
	AllowedCIDRs []netip.Prefix
	Deadline     time.Duration
}

// Client is the hand-rolled Vault/OpenBao HTTP client. One client serves one
// outbox attempt; Forget revokes any login token and drops credentials.
type Client struct {
	base      string
	namespace string
	http      *http.Client
	deadline  time.Duration
	now       func() time.Time

	mu         sync.Mutex
	credential Credential
	token      string
	expires    time.Time
	loggedIn   bool
	renewals   int
	renewAt    time.Time
	renewable  bool
}

var _ API = (*Client)(nil)

// NewClient prepares a client without contacting the server. Deadline must be
// positive and shorter than adapter.LeaseTime. Invalid origins, credentials,
// trust material, deadlines, or egress policies return errors. Call Forget
// when the attempt ends to release credentials and any AppRole login token.
func NewClient(cfg ClientConfig) (*Client, error) {
	return newClient(cfg, net.DefaultResolver, &net.Dialer{Timeout: cfg.Deadline})
}

// newClient constructs a client using the supplied DNS resolver and dialer
// for egress enforcement, returning the same configuration errors as NewClient.
func newClient(cfg ClientConfig, resolver netpolicy.Resolver, dialer netpolicy.Dialer) (*Client, error) {
	origin, err := ParseOrigin(cfg.Origin)
	if err != nil {
		return nil, err
	}
	credential, err := ParseCredential(cfg.Credential)
	if err != nil {
		return nil, err
	}
	if cfg.Deadline <= 0 {
		return nil, errors.New("vault-kv: a request deadline is required")
	}
	if cfg.Deadline >= adapter.LeaseTime {
		return nil, errors.New("vault-kv: request deadline must be shorter than the provider-write lease")
	}
	publicDialer, err := netpolicy.NewPublicDialer(cfg.AllowedCIDRs, resolver, dialer)
	if err != nil {
		return nil, fmt.Errorf("vault-kv: egress policy: %w", err)
	}
	transport := &http.Transport{
		Proxy:           nil,
		TLSClientConfig: credential.tlsConfig(),
		DialContext:     publicDialer.DialContext,
	}
	client := &Client{
		base:       origin.Base,
		namespace:  origin.Namespace,
		deadline:   cfg.Deadline,
		now:        func() time.Time { return time.Now().UTC() },
		credential: credential,
		http: &http.Client{
			Transport: transport,
			Timeout:   cfg.Deadline,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("vault-kv: redirects are refused")
			},
		},
	}
	if credential.Method == TokenAuth {
		client.token = credential.Token
	}
	return client, nil
}

// Forget ends the attempt: a token this client minted by login is revoked
// (best effort, bounded by one request deadline), and every secret-bearing
// field is dropped. An operator-supplied static token is never revoked.
func (c *Client) Forget() {
	c.mu.Lock()
	minted := c.loggedIn && c.token != ""
	token := c.token
	c.mu.Unlock()
	if minted {
		ctx, cancel := context.WithTimeout(context.Background(), c.deadline)
		_ = c.send(ctx, operationRegistry["token-revoke"], nil, token, nil, nil)
		cancel()
	}
	c.mu.Lock()
	c.token = ""
	c.loggedIn = false
	c.credential.forget()
	c.mu.Unlock()
	c.http.CloseIdleConnections()
}

// Origin is the parsed adapter origin: the HTTPS base address plus an
// optional namespace carried as the URL path. Namespaces travel in the
// X-Vault-Namespace header, which both Vault Enterprise and OpenBao honor.
type Origin struct {
	Base      string
	Namespace string
}

var namespaceSegment = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ParseOrigin accepts https://host[:port] optionally followed by /ns[/child].
func ParseOrigin(raw string) (Origin, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Origin{}, errors.New("vault-kv: origin is not a URL")
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
		return Origin{}, errors.New("vault-kv: origin must be https://host[:port] optionally followed by /<namespace>")
	}
	namespace := strings.Trim(u.Path, "/")
	if namespace != "" {
		if strings.HasSuffix(u.Path, "/") {
			return Origin{}, errors.New("vault-kv: origin namespace must not end with a slash")
		}
		for _, segment := range strings.Split(namespace, "/") {
			if !namespaceSegment.MatchString(segment) {
				return Origin{}, errors.New("vault-kv: origin namespace segments must be letters, digits, '-' or '_'")
			}
		}
		if namespace == "root" {
			return Origin{}, errors.New("vault-kv: omit the namespace for the root namespace")
		}
	} else if u.Path != "" && u.Path != "/" {
		return Origin{}, errors.New("vault-kv: origin path must name a namespace")
	}
	return Origin{Base: "https://" + u.Host, Namespace: namespace}, nil
}

// CanonicalOrigin is the persisted spelling of an origin.
func CanonicalOrigin(raw string) (string, error) {
	origin, err := ParseOrigin(raw)
	if err != nil {
		return "", err
	}
	if origin.Namespace == "" {
		return origin.Base, nil
	}
	return origin.Base + "/" + origin.Namespace, nil
}

// ResponseError is a non-2xx provider answer. Provider bodies are never
// surfaced: a broken provider can echo request material, which may be
// plaintext. CASMismatch is derived internally from the fixed KV v2 phrase.
type ResponseError struct {
	Status      int
	CASMismatch bool
	Sealed      bool
}

func (e *ResponseError) Error() string {
	switch {
	case e.CASMismatch:
		return "vault-kv: check-and-set version did not match; the destination moved"
	case e.Sealed:
		return "vault-kv: provider is sealed or unavailable (status " + strconv.Itoa(e.Status) + ")"
	}
	return "vault-kv: provider refused request with status " + strconv.Itoa(e.Status)
}

type rateLimitError struct{ at time.Time }

func (e *rateLimitError) Error() string      { return adapter.ErrRateLimited.Error() }
func (e *rateLimitError) Unwrap() error      { return adapter.ErrRateLimited }
func (e *rateLimitError) RetryAt() time.Time { return e.at }

// IsNotFound reports a 404.
func IsNotFound(err error) bool {
	var response *ResponseError
	return errors.As(err, &response) && response.Status == http.StatusNotFound
}

// IsCASMismatch reports the KV v2 check-and-set refusal.
func IsCASMismatch(err error) bool {
	var response *ResponseError
	return errors.As(err, &response) && response.CASMismatch
}

// definitive reports a provider answer that proves the write did not apply.
func definitive(err error) bool {
	var response *ResponseError
	if !errors.As(err, &response) {
		return false
	}
	switch response.Status {
	case http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusPreconditionFailed:
		// 412 is Vault's "standby has not caught up": retry, never proof.
		return false
	}
	return response.Status >= 400 && response.Status < 500
}

// authorize returns the static token or logs in with AppRole on first use.
// A login token nearing expiry is renewed at most twice per client. Released
// credentials, missing login tokens, and insufficient remaining token lifetime
// return adapter.ErrProviderAuth. Valid leases remain usable after the renewal
// budget is exhausted; request failures pass through loginError.
func (c *Client) authorize(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.credential.Method == AppRoleAuth && !c.loggedIn {
		if c.credential.RoleID == "" {
			return "", fmt.Errorf("%w: the adapter credential was already released", adapter.ErrProviderAuth)
		}
		var out struct {
			Auth *struct {
				ClientToken   string `json:"client_token"`
				LeaseDuration int64  `json:"lease_duration"`
				Renewable     bool   `json:"renewable"`
			} `json:"auth"`
		}
		body := map[string]string{"role_id": c.credential.RoleID, "secret_id": c.credential.SecretID}
		params := map[string]string{"auth_mount": c.credential.Mount}
		if err := c.send(ctx, operationRegistry["approle-login"], params, "", body, &out); err != nil {
			return "", loginError(err)
		}
		if out.Auth == nil || out.Auth.ClientToken == "" {
			return "", fmt.Errorf("%w: AppRole login returned no token", adapter.ErrProviderAuth)
		}
		c.token = out.Auth.ClientToken
		c.loggedIn = true
		if out.Auth.LeaseDuration > 0 {
			c.setLease(out.Auth.LeaseDuration)
		}
		c.renewable = out.Auth.Renewable
	}
	if c.token == "" {
		return "", fmt.Errorf("%w: the adapter credential was already released", adapter.ErrProviderAuth)
	}
	if c.loggedIn && c.renewable && !c.expires.IsZero() && (!c.now().Before(c.renewAt) || !c.now().Add(c.deadline).Before(c.expires)) && c.renewals < maxRenewals {
		c.renewals++
		var out struct {
			Auth *struct {
				LeaseDuration int64 `json:"lease_duration"`
			} `json:"auth"`
		}
		if err := c.send(ctx, operationRegistry["token-renew"], nil, c.token, map[string]string{}, &out); err != nil {
			return "", loginError(err)
		}
		if out.Auth == nil || out.Auth.LeaseDuration <= 0 {
			return "", fmt.Errorf("%w: login token could not be renewed", adapter.ErrProviderAuth)
		}
		c.setLease(out.Auth.LeaseDuration)
	}
	if c.loggedIn && !c.expires.IsZero() && !c.now().Add(c.deadline).Before(c.expires) {
		return "", fmt.Errorf("%w: login token cannot outlive the request deadline; renewal budget exhausted or lease not renewable", adapter.ErrProviderAuth)
	}
	return c.token, nil
}

func (c *Client) setLease(seconds int64) {
	ttl := time.Duration(seconds) * time.Second
	now := c.now()
	c.expires = now.Add(ttl)
	// A short lease must not trigger another immediate renewal.
	c.renewAt = c.expires.Add(-min(renewMargin, ttl/2))
}

// loginError adds adapter.ErrProviderAuth to provider 4xx login or renewal
// refusals other than rate limits, preserving the original error.
func loginError(err error) error {
	var response *ResponseError
	if errors.As(err, &response) && response.Status >= 400 && response.Status < 500 && response.Status != http.StatusTooManyRequests {
		return errors.Join(adapter.ErrProviderAuth, err)
	}
	return err
}

// do authorizes and sends a registered operation, rejecting unknown keys.
// Authorization and send errors propagate to the caller.
func (c *Client) do(ctx context.Context, key string, params map[string]string, body, out any) error {
	op, ok := operationRegistry[key]
	if !ok {
		return fmt.Errorf("vault-kv: operation %q is not linked", key)
	}
	token := ""
	if op.Authenticated {
		var err error
		if token, err = c.authorize(ctx); err != nil {
			return err
		}
	}
	return c.send(ctx, op, params, token, body, out)
}

// send sends a JSON request and decodes a nonempty successful response into
// out when supplied. Responses over 1 MiB, unreadable bodies, and decoding
// failures return errors. A 429 returns adapter.ErrRateLimited with a retry deadline;
// other non-2xx responses return ResponseError, joined with
// adapter.ErrProviderAuth for 401 or 403. Encoding, request construction, and
// transport errors propagate without including provider response bodies.
func (c *Client) send(ctx context.Context, op operation, params map[string]string, token string, body, out any) error {
	path, query, _ := strings.Cut(op.Path, "?")
	for name, value := range params {
		placeholder := "{" + name + "}"
		if value == "" || !strings.Contains(path, placeholder) {
			return errors.New("vault-kv: invalid operation path parameter")
		}
		for _, segment := range strings.Split(value, "/") {
			if segment == "" || segment == "." || segment == ".." {
				return errors.New("vault-kv: invalid operation path segment")
			}
		}
		path = strings.ReplaceAll(path, placeholder, escapePath(value))
	}
	if strings.ContainsAny(path, "{}") {
		return errors.New("vault-kv: missing operation path parameter")
	}

	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, op.Method, c.base, input)
	if err != nil {
		return err
	}
	// Request parameters may only affect the escaped path. Keep the validated
	// origin and the registry-owned query separate from user-controlled names.
	req.URL.Path, err = url.PathUnescape(path)
	if err != nil {
		return errors.New("vault-kv: invalid operation path encoding")
	}
	req.URL.RawPath = path
	req.URL.RawQuery = query
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if c.namespace != "" && !op.RootOnly {
		req.Header.Set("X-Vault-Namespace", c.namespace)
	}
	if body != nil {
		contentType := "application/json"
		if op.Method == http.MethodPatch {
			contentType = "application/merge-patch+json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("vault-kv: provider request failed: %w", redactURLError(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseCap+1))
	if err != nil {
		return errors.New("vault-kv: provider response could not be read")
	}
	if len(raw) > responseCap {
		return errors.New("vault-kv: provider response exceeded 1 MiB")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return &rateLimitError{at: c.retryAt(resp.Header)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseErr := &ResponseError{Status: resp.StatusCode}
		switch resp.StatusCode {
		case http.StatusBadRequest:
			responseErr.CASMismatch = bytes.Contains(raw, []byte("check-and-set parameter did not match"))
		case http.StatusServiceUnavailable:
			responseErr.Sealed = true
		case http.StatusForbidden, http.StatusUnauthorized:
			return errors.Join(adapter.ErrProviderAuth, responseErr)
		}
		return responseErr
	}
	if out != nil && len(raw) != 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return errors.New("vault-kv: provider response did not match the expected shape")
		}
	}
	return nil
}

// retryAt interprets Retry-After as nonnegative seconds or an HTTP date,
// falling back to 30 seconds from now when the header is absent or invalid.
func (c *Client) retryAt(header http.Header) time.Time {
	now := c.now()
	if raw := header.Get("Retry-After"); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
			return now.Add(time.Duration(seconds) * time.Second)
		}
		if at, err := http.ParseTime(raw); err == nil {
			return at.UTC()
		}
	}
	return now.Add(defaultRateBackoff)
}

// redactURLError drops the request URL from transport errors. Paths are
// names, not values, but keeping transport errors URL-free keeps them uniform.
func redactURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// escapePath URL-escapes each path segment while preserving slash separators.
func escapePath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

// Health reads server initialization, seal state, and version without a token
// or namespace header. Sealed and uninitialized states are returned as data;
// request and response-decoding failures are returned as errors.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var out struct {
		Initialized bool   `json:"initialized"`
		Sealed      bool   `json:"sealed"`
		Version     string `json:"version"`
	}
	// Status codes are pinned to 200 so a sealed or uninitialized server is
	// reported by body rather than conflated with a transport failure.
	if err := c.do(ctx, "health", nil, nil, &out); err != nil {
		return Health{}, err
	}
	return Health{Initialized: out.Initialized, Sealed: out.Sealed, Version: out.Version}, nil
}

// MountInfo returns the engine type, version, and identity for a mount path.
// Authentication, request, and response-decoding errors propagate to the caller.
func (c *Client) MountInfo(ctx context.Context, mount string) (Mount, error) {
	var out struct {
		Data struct {
			Type     string `json:"type"`
			UUID     string `json:"uuid"`
			Accessor string `json:"accessor"`
			Options  struct {
				Version string `json:"version"`
			} `json:"options"`
		} `json:"data"`
	}
	if err := c.do(ctx, "mount-info", map[string]string{"mount": mount}, nil, &out); err != nil {
		return Mount{}, err
	}
	return Mount{Type: out.Data.Type, Version: out.Data.Options.Version, UUID: out.Data.UUID, Accessor: out.Data.Accessor}, nil
}

// LookupSelf reports the expiry of an operator-supplied token. An AppRole
// login token lives only for one attempt, so its expiry says nothing about the
// credential and no request is made. Missing or malformed expiry timestamps
// produce a zero ExpireTime; request errors still propagate.
func (c *Client) LookupSelf(ctx context.Context) (TokenInfo, error) {
	c.mu.Lock()
	approle := c.credential.Method == AppRoleAuth
	c.mu.Unlock()
	if approle {
		return TokenInfo{}, nil
	}
	var out struct {
		Data struct {
			ExpireTime *string `json:"expire_time"`
		} `json:"data"`
	}
	if err := c.do(ctx, "token-lookup", nil, nil, &out); err != nil {
		return TokenInfo{}, err
	}
	info := TokenInfo{}
	if out.Data.ExpireTime != nil && *out.Data.ExpireTime != "" {
		if at, err := time.Parse(time.RFC3339Nano, *out.Data.ExpireTime); err == nil {
			info.ExpireTime = at.UTC()
		}
	}
	return info, nil
}

// ReadMetadata returns version and ownership metadata without reading values.
// A deletion time at or before now marks a version deleted; a future time does
// not. Invalid version numbers or deletion timestamps return errors, as do
// request failures, including a 404 for missing metadata.
func (c *Client) ReadMetadata(ctx context.Context, mount, path string) (Metadata, error) {
	var out struct {
		Data struct {
			CurrentVersion int64             `json:"current_version"`
			CustomMetadata map[string]string `json:"custom_metadata"`
			Versions       map[string]struct {
				DeletionTime string `json:"deletion_time"`
				Destroyed    bool   `json:"destroyed"`
			} `json:"versions"`
		} `json:"data"`
	}
	if err := c.do(ctx, "read-metadata", map[string]string{"mount": mount, "path": path}, nil, &out); err != nil {
		return Metadata{}, err
	}
	meta := Metadata{CurrentVersion: out.Data.CurrentVersion, CustomMetadata: out.Data.CustomMetadata, Versions: map[int64]VersionMetadata{}}
	now := c.now()
	for raw, version := range out.Data.Versions {
		number, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || number <= 0 {
			return Metadata{}, errors.New("vault-kv: metadata names a malformed version")
		}
		// A mount or path with delete_version_after stamps every write with a
		// future deletion_time; that version stays readable until then.
		deleted := false
		if version.DeletionTime != "" {
			at, err := time.Parse(time.RFC3339Nano, version.DeletionTime)
			if err != nil {
				return Metadata{}, errors.New("vault-kv: metadata names a malformed deletion time")
			}
			deleted = !at.After(now)
		}
		meta.Versions[number] = VersionMetadata{Deleted: deleted, Destroyed: version.Destroyed}
	}
	if meta.CustomMetadata == nil {
		meta.CustomMetadata = map[string]string{}
	}
	return meta, nil
}

// PatchCustomMetadata merge-patches the named custom metadata fields; a nil
// value removes its field. Authentication and request errors propagate.
func (c *Client) PatchCustomMetadata(ctx context.Context, mount, path string, custom map[string]*string) error {
	return c.do(ctx, "patch-metadata", map[string]string{"mount": mount, "path": path}, map[string]any{"custom_metadata": custom}, nil)
}

// WriteCAS writes a single "value" field, using cas as the expected current
// version (zero requests creation). It returns the new version, or
// adapter.ErrIndeterminate if the reported version is not cas+1. Request
// errors propagate, including refusals recognized by IsCASMismatch.
func (c *Client) WriteCAS(ctx context.Context, mount, path, value string, cas int64) (int64, error) {
	var out struct {
		Data struct {
			Version int64 `json:"version"`
		} `json:"data"`
	}
	body := map[string]any{"data": map[string]string{"value": value}, "options": map[string]int64{"cas": cas}}
	if err := c.do(ctx, "write-cas", map[string]string{"mount": mount, "path": path}, body, &out); err != nil {
		return 0, err
	}
	if out.Data.Version != cas+1 {
		return 0, fmt.Errorf("%w: write reported version %d after check-and-set %d", adapter.ErrIndeterminate, out.Data.Version, cas)
	}
	return out.Data.Version, nil
}

// DeleteVersion soft-deletes only the specified version, preserving metadata
// and other versions. Authentication and request errors, including 404s,
// propagate to the caller.
func (c *Client) DeleteVersion(ctx context.Context, mount, path string, version int64) error {
	return c.do(ctx, "soft-delete-data", map[string]string{"mount": mount, "path": path}, map[string][]int64{"versions": {version}}, nil)
}
