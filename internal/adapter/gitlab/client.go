// Package gitlab implements the GitLab CI/CD variable deployment adapter.
//
// GitLab's variable GET and list endpoints return stored values, so this
// provider boundary deliberately cannot express any variable read: existence
// and ownership come from Hikyo's ledger alone, and a POST refused because the
// key is already taken is the only way an unowned variable is discovered.
package gitlab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/netpolicy"
)

const (
	responseCap        = 1 << 20
	caBundleCap        = 64 << 10
	defaultRateBackoff = time.Minute
	apiPrefix          = "/api/v4"
)

// DestinationIdentity is the immutable numeric id and the current full path
// GitLab reports for a project or group.
type DestinationIdentity struct {
	ID   int64
	Path string
}

// TokenInfo is value-free metadata about the presented access token.
type TokenInfo struct {
	Scopes    []string
	Active    bool
	Revoked   bool
	ExpiresAt time.Time
	// Bot is true for project and group access tokens, whose user is a
	// GitLab-managed bot. A personal access token belongs to a human user.
	Bot bool
}

// Variable is one write request. Masked and Hidden are only ever set for
// secret-classified keys; Hidden is honoured on creation only.
type Variable struct {
	Key       string
	Value     string
	Scope     string
	Protected bool
	Masked    bool
	Hidden    bool
	Raw       bool
}

type WriteResult struct{ Status int }

// API is the closed GitLab surface. There is no variable read or list method:
// both would disclose stored values. The closure test pins this method set.
type API interface {
	Version(context.Context) (string, error)
	Token(context.Context) (TokenInfo, error)
	ResolveDestination(context.Context, adapter.Destination) (DestinationIdentity, error)
	CreateVariable(context.Context, adapter.Destination, Variable) (WriteResult, error)
	UpdateVariable(context.Context, adapter.Destination, Variable) (WriteResult, error)
	DeleteVariable(context.Context, adapter.Destination, string, string) error
}

type operation struct {
	Method   string
	Path     string
	Mutation bool
}

// operationRegistry is the closed linked provider surface. No GET may address
// a variables collection or member; the route-scan test enforces it.
var operationRegistry = map[string]operation{
	"version":            {Method: http.MethodGet, Path: "/version"},
	"token-self":         {Method: http.MethodGet, Path: "/personal_access_tokens/self"},
	"current-user":       {Method: http.MethodGet, Path: "/user"},
	"resolve-project":    {Method: http.MethodGet, Path: "/projects/{id}"},
	"resolve-group":      {Method: http.MethodGet, Path: "/groups/{id}"},
	"create-variable":    {Method: http.MethodPost, Path: "/{destination}/variables", Mutation: true},
	"update-variable":    {Method: http.MethodPut, Path: "/{destination}/variables/{key}", Mutation: true},
	"delete-variable":    {Method: http.MethodDelete, Path: "/{destination}/variables/{key}", Mutation: true},
	"resolve-project-by": {Method: http.MethodGet, Path: "/projects/{path}"},
	"resolve-group-by":   {Method: http.MethodGet, Path: "/groups/{path}"},
}

type ClientConfig struct {
	Origin       string
	Credential   string
	AllowedCIDRs []netip.Prefix
	Deadline     time.Duration
	SPKIPin      string
	CABundlePEM  string
}

type Client struct {
	origin     string
	token      string
	http       *http.Client
	now        func() time.Time
	forgetOnce sync.Once
	rateMu     sync.Mutex
	rateTries  int
}

// Forget releases this module lease's private transport and bearer.
func (c *Client) Forget() {
	c.forgetOnce.Do(func() {
		c.token = ""
		c.http.CloseIdleConnections()
	})
}

func NewClient(cfg ClientConfig) (*Client, error) {
	return newClient(cfg, net.DefaultResolver, &net.Dialer{Timeout: cfg.Deadline})
}

func newClient(cfg ClientConfig, resolver netpolicy.Resolver, dialer netpolicy.Dialer) (*Client, error) {
	origin, err := CanonicalOrigin(cfg.Origin)
	if err != nil {
		return nil, err
	}
	if cfg.Credential == "" {
		return nil, errors.New("gitlab: a project or group access token is required")
	}
	if cfg.Deadline <= 0 || cfg.Deadline >= adapter.LeaseTime {
		return nil, errors.New("gitlab: request deadline must be positive and shorter than the provider-write lease")
	}
	tlsConfig, err := TLSConfig(cfg.SPKIPin, cfg.CABundlePEM)
	if err != nil {
		return nil, err
	}
	publicDialer, err := netpolicy.NewPublicDialer(cfg.AllowedCIDRs, resolver, dialer)
	if err != nil {
		return nil, fmt.Errorf("gitlab: egress policy: %w", err)
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: tlsConfig, DialContext: publicDialer.DialContext}
	return &Client{
		origin: origin, token: cfg.Credential, now: func() time.Time { return time.Now().UTC() },
		http: &http.Client{
			Transport: transport, Timeout: cfg.Deadline,
			CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("gitlab: redirects are refused") },
		},
	}, nil
}

// CanonicalOrigin accepts an HTTPS GitLab base URL, optionally under a
// relative URL root, and returns it without a trailing slash or /api/v4.
func CanonicalOrigin(raw string) (string, error) {
	return adapter.CanonicalOrigin(adapter.GitLabProvider, raw)
}

// ValidatePin checks an SPKI pin's shape: base64(sha256(SubjectPublicKeyInfo)).
func ValidatePin(pin string) error {
	if pin == "" {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(pin)
	if err != nil || len(raw) != sha256.Size {
		return errors.New("gitlab: spki_pin must be base64(sha256(SubjectPublicKeyInfo))")
	}
	return nil
}

// TLSConfig builds the adapter egress TLS policy: TLS >= 1.2, normal chain
// verification against the system roots plus an optional CA bundle, and, when
// pinned, a SubjectPublicKeyInfo match against the verified chain.
func TLSConfig(pin, caBundlePEM string) (*tls.Config, error) {
	if err := ValidatePin(pin); err != nil {
		return nil, err
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if caBundlePEM != "" {
		if len(caBundlePEM) > caBundleCap {
			return nil, errors.New("gitlab: ca_bundle exceeds 64 KiB")
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(caBundlePEM)) {
			return nil, errors.New("gitlab: ca_bundle contains no PEM certificates")
		}
		config.RootCAs = pool
	}
	if pin != "" {
		want, _ := base64.StdEncoding.DecodeString(pin)
		config.VerifyConnection = func(state tls.ConnectionState) error {
			for _, chain := range state.VerifiedChains {
				for _, cert := range chain {
					got := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
					if subtle.ConstantTimeCompare(got[:], want) == 1 {
						return nil
					}
				}
			}
			return errors.New("gitlab: server certificate does not match the configured SPKI pin")
		}
	}
	return config, nil
}

// ResponseError carries the status and whether GitLab reported the key as
// already taken. It never carries the response body.
type ResponseError struct {
	Status int
	Taken  bool
}

func (e *ResponseError) Error() string {
	if e.Taken {
		return "gitlab: variable key has already been taken"
	}
	return "gitlab: provider refused request with status " + strconv.Itoa(e.Status)
}

type rateLimitError struct{ at time.Time }

func (e *rateLimitError) Error() string      { return adapter.ErrRateLimited.Error() }
func (e *rateLimitError) Unwrap() error      { return adapter.ErrRateLimited }
func (e *rateLimitError) RetryAt() time.Time { return e.at }

func IsStatus(err error, status int) bool {
	var response *ResponseError
	return errors.As(err, &response) && response.Status == status
}

// IsTaken reports GitLab's "(key) has already been taken" create refusal.
func IsTaken(err error) bool {
	var response *ResponseError
	return errors.As(err, &response) && response.Taken
}

func (c *Client) rateDeadline(status int, header http.Header, now time.Time) (time.Time, bool) {
	if status != http.StatusTooManyRequests {
		return time.Time{}, false
	}
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	if raw := header.Get("Retry-After"); raw != "" {
		c.rateTries = 0
		if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
			return now.Add(time.Duration(min(seconds, int(adapter.RetryCap/time.Second))) * time.Second), true
		}
		if at, err := http.ParseTime(raw); err == nil {
			return at.UTC(), true
		}
	}
	if seconds, err := strconv.ParseInt(header.Get("RateLimit-Reset"), 10, 64); err == nil && seconds > 0 {
		c.rateTries = 0
		return time.Unix(seconds, 0).UTC(), true
	}
	c.rateTries++
	delay := defaultRateBackoff
	for i := 1; i < c.rateTries && delay < adapter.RetryCap; i++ {
		delay = min(delay*2, adapter.RetryCap)
	}
	return now.Add(delay), true
}

// takenBody reports whether a 400 body is GitLab's key-uniqueness refusal.
// Only the key field's messages are inspected; nothing is retained.
func takenBody(raw []byte) bool {
	var body struct {
		Message json.RawMessage `json:"message"`
	}
	if json.Unmarshal(raw, &body) != nil || len(body.Message) == 0 {
		return false
	}
	var fields map[string][]string
	if json.Unmarshal(body.Message, &fields) != nil {
		return false
	}
	for _, message := range fields["key"] {
		if strings.Contains(message, "has already been taken") {
			return true
		}
	}
	return false
}

func (c *Client) do(ctx context.Context, op operation, path string, body any, out any) (int, error) {
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, op.Method, c.origin+apiPrefix+path, input)
	if err != nil {
		return 0, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("gitlab: provider request: %w", adapter.SafeTransportError(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseCap+1))
	if err != nil {
		return resp.StatusCode, errors.New("gitlab: provider response could not be read")
	}
	if len(raw) > responseCap {
		return resp.StatusCode, errors.New("gitlab: provider response exceeded 1 MiB")
	}
	if at, ok := c.rateDeadline(resp.StatusCode, resp.Header, c.now()); ok {
		return resp.StatusCode, &rateLimitError{at: at}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseErr := &ResponseError{Status: resp.StatusCode}
		if resp.StatusCode == http.StatusBadRequest && op.Mutation {
			responseErr.Taken = takenBody(raw)
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return resp.StatusCode, errors.Join(adapter.ErrProviderAuth, responseErr)
		}
		return resp.StatusCode, responseErr
	}
	c.rateMu.Lock()
	c.rateTries = 0
	c.rateMu.Unlock()
	if out != nil && len(raw) != 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, errors.New("gitlab: provider response did not match expected shape")
		}
	}
	return resp.StatusCode, nil
}

func (c *Client) Version(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if _, err := c.do(ctx, operationRegistry["version"], "/version", nil, &out); err != nil {
		return "", err
	}
	if out.Version == "" {
		return "", errors.New("gitlab: provider version did not match expected shape")
	}
	return out.Version, nil
}

func (c *Client) Token(ctx context.Context) (TokenInfo, error) {
	var token struct {
		Scopes    []string `json:"scopes"`
		Active    bool     `json:"active"`
		Revoked   bool     `json:"revoked"`
		ExpiresAt *string  `json:"expires_at"`
	}
	if _, err := c.do(ctx, operationRegistry["token-self"], "/personal_access_tokens/self", nil, &token); err != nil {
		return TokenInfo{}, err
	}
	var user struct {
		Bot bool `json:"bot"`
	}
	if _, err := c.do(ctx, operationRegistry["current-user"], "/user", nil, &user); err != nil {
		return TokenInfo{}, err
	}
	info := TokenInfo{Scopes: token.Scopes, Active: token.Active, Revoked: token.Revoked, Bot: user.Bot}
	if token.ExpiresAt != nil && *token.ExpiresAt != "" {
		expires, err := time.Parse(time.DateOnly, *token.ExpiresAt)
		if err != nil {
			return TokenInfo{}, errors.New("gitlab: token expiry did not match expected shape")
		}
		// GitLab tokens expire at the start of expires_at, UTC.
		info.ExpiresAt = expires.UTC()
	}
	return info, nil
}

// destinationPath addresses a project or group by immutable numeric id.
func destinationPath(d adapter.Destination) (string, error) {
	if d.NumericID <= 0 {
		return "", errors.New("gitlab: destination numeric id is not resolved")
	}
	switch d.Kind {
	case adapter.Repository:
		return "/projects/" + strconv.FormatInt(d.NumericID, 10), nil
	case adapter.Organization:
		return "/groups/" + strconv.FormatInt(d.NumericID, 10), nil
	default:
		return "", errors.New("gitlab: destination must be a project (repository) or group (organization)")
	}
}

// FullPath is the configured GitLab path a destination names.
func FullPath(d adapter.Destination) string {
	if d.Kind == adapter.Repository {
		return d.Owner + "/" + d.Name
	}
	return d.Owner
}

func (c *Client) ResolveDestination(ctx context.Context, d adapter.Destination) (DestinationIdentity, error) {
	var key, path string
	switch d.Kind {
	case adapter.Repository:
		key, path = "resolve-project", "/projects/"
	case adapter.Organization:
		key, path = "resolve-group", "/groups/"
	default:
		return DestinationIdentity{}, errors.New("gitlab: destination must be a project (repository) or group (organization)")
	}
	if d.NumericID > 0 {
		path += strconv.FormatInt(d.NumericID, 10)
	} else {
		key += "-by"
		path += url.PathEscape(FullPath(d))
	}
	var out struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		FullPath          string `json:"full_path"`
	}
	if _, err := c.do(ctx, operationRegistry[key], path, nil, &out); err != nil {
		return DestinationIdentity{}, err
	}
	identity := DestinationIdentity{ID: out.ID, Path: out.FullPath}
	if d.Kind == adapter.Repository {
		identity.Path = out.PathWithNamespace
	}
	if identity.ID <= 0 || identity.Path == "" {
		return DestinationIdentity{}, errors.New("gitlab: destination did not match expected shape")
	}
	return identity, nil
}

func scopeFilter(scope string) string {
	if scope == "" {
		scope = "*"
	}
	return "?filter%5Benvironment_scope%5D=" + url.QueryEscape(scope)
}

func variableBody(v Variable, create bool) map[string]any {
	scope := v.Scope
	if scope == "" {
		scope = "*"
	}
	body := map[string]any{
		"value": v.Value, "variable_type": "env_var", "environment_scope": scope,
		"protected": v.Protected, "masked": v.Masked, "raw": v.Raw,
	}
	if create {
		body["key"] = v.Key
		body["description"] = "Managed by Hikyo"
		if v.Hidden {
			body["masked_and_hidden"] = true
		}
	}
	return body
}

func (c *Client) CreateVariable(ctx context.Context, d adapter.Destination, v Variable) (WriteResult, error) {
	path, err := destinationPath(d)
	if err != nil {
		return WriteResult{}, err
	}
	status, err := c.do(ctx, operationRegistry["create-variable"], path+"/variables", variableBody(v, true), nil)
	return WriteResult{Status: status}, err
}

func (c *Client) UpdateVariable(ctx context.Context, d adapter.Destination, v Variable) (WriteResult, error) {
	path, err := destinationPath(d)
	if err != nil {
		return WriteResult{}, err
	}
	status, err := c.do(ctx, operationRegistry["update-variable"], path+"/variables/"+url.PathEscape(v.Key)+scopeFilter(v.Scope), variableBody(v, false), nil)
	return WriteResult{Status: status}, err
}

func (c *Client) DeleteVariable(ctx context.Context, d adapter.Destination, key, scope string) error {
	path, err := destinationPath(d)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, operationRegistry["delete-variable"], path+"/variables/"+url.PathEscape(key)+scopeFilter(scope), nil, nil)
	return err
}
