// Package cloudflare implements one-way synchronization into Cloudflare
// Workers secrets and Cloudflare Pages encrypted environment variables.
//
// Every write uses the provider's secret_text type. The provider boundary
// cannot express a plaintext write, and the only read that can carry a
// plaintext Pages variable value decodes names and types alone.
package cloudflare

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
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
	// DefaultOrigin is the only Cloudflare API origin the adapter accepts.
	DefaultOrigin = "https://api.cloudflare.com"
	apiBase       = "/client/v4"
	// Script listings carry metadata for every Worker in the account, so the
	// cap is larger than the other adapters' 1 MiB.
	responseCap = 4 << 20
	// Cloudflare's global limit is 1200 requests per five minutes per user.
	// Without an authoritative Retry-After the adapter waits out one window.
	defaultRateBackoff = 5 * time.Minute
	secretNameLimit    = 10_000
	// SecretType is the only variable type the adapter ever writes.
	SecretType = "secret_text"
)

// ErrAmbiguousResponse marks a 2xx response whose envelope did not confirm
// success. The write may or may not have landed.
var ErrAmbiguousResponse = errors.New("cloudflare: provider response did not confirm success")

// TokenStatus is value-free token metadata from the verify endpoint.
type TokenStatus struct {
	Status    string
	ExpiresAt time.Time
}

// ProjectShape is a Pages project's identity plus the names and types of the
// selected environment's variables. It structurally cannot hold a value.
type ProjectShape struct {
	ID    string
	Names map[string]string
}

// API is the closed provider surface Sync can link. There is no operation that
// returns a secret value, and no operation decodes a plain_text value: the
// Pages project read and the Workers settings read decode names only.
type API interface {
	VerifyToken(ctx context.Context, account string) (TokenStatus, error)
	ListAccountIDs(ctx context.Context) ([]string, error)
	ResolveScript(ctx context.Context, d adapter.Destination) (string, error)
	ResolveProject(ctx context.Context, d adapter.Destination) (ProjectShape, error)
	ListBindingNames(ctx context.Context, d adapter.Destination) ([]string, error)
	PutSecret(ctx context.Context, d adapter.Destination, name, value string) error
	DeleteSecret(ctx context.Context, d adapter.Destination, name string) error
	PatchPagesSecret(ctx context.Context, d adapter.Destination, name string, value *string) error
}

type operation struct {
	Method   string
	Path     string
	Mutation bool
}

// operationRegistry is the closed linked provider surface. The structural
// test pins it: no operation reads a secret value, the Pages project read is
// decoded through ProjectShape only, and the Workers settings read decodes
// binding names only.
var operationRegistry = map[string]operation{
	"verify-account-token": {Method: http.MethodGet, Path: "/accounts/{account}/tokens/verify"},
	"verify-user-token":    {Method: http.MethodGet, Path: "/user/tokens/verify"},
	"list-accounts":        {Method: http.MethodGet, Path: "/accounts"},
	"list-scripts":         {Method: http.MethodGet, Path: "/accounts/{account}/workers/scripts"},
	"script-binding-names": {Method: http.MethodGet, Path: "/accounts/{account}/workers/scripts/{script}/settings"},
	"put-script-secret":    {Method: http.MethodPut, Path: "/accounts/{account}/workers/scripts/{script}/secrets", Mutation: true},
	"delete-script-secret": {Method: http.MethodDelete, Path: "/accounts/{account}/workers/scripts/{script}/secrets/{name}", Mutation: true},
	"project-shape":        {Method: http.MethodGet, Path: "/accounts/{account}/pages/projects/{project}"},
	"patch-project-secret": {Method: http.MethodPatch, Path: "/accounts/{account}/pages/projects/{project}", Mutation: true},
}

type ClientConfig struct {
	Origin       string
	Credential   string
	AllowedCIDRs []netip.Prefix
	Deadline     time.Duration
}

type Client struct {
	origin     string
	token      string
	http       *http.Client
	now        func() time.Time
	forgetOnce sync.Once
	expiryMu   sync.RWMutex
	expiresAt  time.Time
}

// CredentialExpiresAt returns the expiry the verify endpoint reported, if any.
func (c *Client) CredentialExpiresAt() time.Time {
	c.expiryMu.RLock()
	defer c.expiryMu.RUnlock()
	return c.expiresAt
}

// Forget releases the private transport and retained bearer when one outbox
// attempt ends.
func (c *Client) Forget() {
	c.forgetOnce.Do(func() {
		c.token = ""
		c.http.CloseIdleConnections()
	})
}

var (
	globalAPIKey = regexp.MustCompile(`^[0-9a-f]{37}$`)
	apiToken     = regexp.MustCompile(`^[A-Za-z0-9_-]{40,200}$`)
	accountID    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	scriptName   = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]{0,61}[a-z0-9])?$`)
	projectName  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,56}[a-z0-9])?$`)
)

// validateCredential accepts only scoped API tokens sent as Authorization:
// Bearer. A Global API Key grants the whole user and is refused by shape.
func validateCredential(token string) error {
	switch {
	case token == "":
		return errors.New("cloudflare: a scoped API token is required")
	case globalAPIKey.MatchString(token):
		return errors.New("cloudflare: Global API Key refused; create a scoped API token for one account")
	case strings.Contains(token, ":") || strings.Contains(token, "@"):
		return errors.New("cloudflare: email/key credential pairs are refused; use a scoped API token")
	case !apiToken.MatchString(token):
		return errors.New("cloudflare: credential is not a Cloudflare API token")
	}
	return nil
}

func NewClient(cfg ClientConfig) (*Client, error) {
	return newClient(cfg, net.DefaultResolver, &net.Dialer{Timeout: cfg.Deadline})
}

func newClient(cfg ClientConfig, resolver netpolicy.Resolver, dialer netpolicy.Dialer) (*Client, error) {
	origin, err := canonicalOrigin(cfg.Origin)
	if err != nil {
		return nil, err
	}
	if err := validateCredential(cfg.Credential); err != nil {
		return nil, err
	}
	if cfg.Deadline <= 0 || cfg.Deadline >= adapter.LeaseTime {
		return nil, errors.New("cloudflare: request deadline must be positive and shorter than the provider-write lease")
	}
	publicDialer, err := netpolicy.NewPublicDialer(cfg.AllowedCIDRs, resolver, dialer)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: egress policy: %w", err)
	}
	transport := &http.Transport{
		Proxy:           nil,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext:     publicDialer.DialContext,
	}
	return &Client{
		origin: origin, token: cfg.Credential, now: func() time.Time { return time.Now().UTC() },
		http: &http.Client{
			Transport: transport, Timeout: cfg.Deadline,
			CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("cloudflare: redirects are refused") },
		},
	}, nil
}

func canonicalOrigin(raw string) (string, error) {
	if raw == "" {
		return DefaultOrigin, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("cloudflare: parse origin: %w", err)
	}
	if u.Scheme != "https" || !strings.EqualFold(u.Host, "api.cloudflare.com") || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("cloudflare: origin must be https://api.cloudflare.com")
	}
	return DefaultOrigin, nil
}

// ResponseError keeps only the status. Provider bodies are never surfaced: a
// broken provider can echo request material, which may be plaintext.
type ResponseError struct{ Status int }

func (e *ResponseError) Error() string {
	return "cloudflare: provider refused request with status " + strconv.Itoa(e.Status)
}

type rateLimitError struct{ at time.Time }

func (e *rateLimitError) Error() string      { return adapter.ErrRateLimited.Error() }
func (e *rateLimitError) Unwrap() error      { return adapter.ErrRateLimited }
func (e *rateLimitError) RetryAt() time.Time { return e.at }

func retryDeadline(header http.Header, now time.Time) time.Time {
	if raw := header.Get("Retry-After"); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
			// Clamp before converting: a huge value would overflow Duration
			// and land in the past. The outbox caps the deadline again.
			return now.Add(time.Duration(min(seconds, int(adapter.RetryCap/time.Second))) * time.Second)
		}
		if at, err := http.ParseTime(raw); err == nil {
			return at.UTC()
		}
	}
	return now.Add(defaultRateBackoff)
}

type envelope struct {
	Success    *bool           `json:"success"`
	Result     json.RawMessage `json:"result"`
	ResultInfo *struct {
		TotalCount int `json:"total_count"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

// do sends one registry operation. A 2xx whose envelope does not say
// success=true is ambiguous, not a success.
func (c *Client) do(ctx context.Context, op operation, path string, body any) (envelope, error) {
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return envelope{}, err
		}
		input = bytes.NewReader(raw)
		defer clear(raw)
	}
	req, err := http.NewRequestWithContext(ctx, op.Method, c.origin+apiBase+path, input)
	if err != nil {
		return envelope{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return envelope{}, fmt.Errorf("cloudflare: provider request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseCap+1))
	if err != nil {
		return envelope{}, errors.New("cloudflare: provider response could not be read")
	}
	if len(raw) > responseCap {
		clear(raw)
		return envelope{}, errors.New("cloudflare: provider response exceeded 4 MiB")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		clear(raw)
		return envelope{}, &rateLimitError{at: retryDeadline(resp.Header, c.now())}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		clear(raw)
		responseErr := &ResponseError{Status: resp.StatusCode}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return envelope{}, errors.Join(adapter.ErrProviderAuth, responseErr)
		}
		return envelope{}, responseErr
	}
	var out envelope
	decodeErr := json.Unmarshal(raw, &out)
	// The Pages project read can carry plain_text values; they are never
	// decoded, and the transport buffer is scrubbed before returning.
	clear(raw)
	if decodeErr != nil || out.Success == nil || !*out.Success {
		return envelope{}, fmt.Errorf("%w (status %d)", ErrAmbiguousResponse, resp.StatusCode)
	}
	return out, nil
}

func accountPath(d adapter.Destination) (string, error) {
	if !accountID.MatchString(d.Owner) {
		return "", errors.New("cloudflare: destination requires a 32-character hexadecimal account id")
	}
	return "/accounts/" + d.Owner, nil
}

func scriptPath(d adapter.Destination) (string, error) {
	if d.Kind != adapter.WorkersScript {
		return "", errors.New("cloudflare: destination is not a Workers script")
	}
	base, err := accountPath(d)
	if err != nil {
		return "", err
	}
	if !scriptName.MatchString(d.Name) || d.Environment != "" {
		return "", errors.New("cloudflare: Workers destination requires a lowercase script name and no environment")
	}
	return base + "/workers/scripts/" + url.PathEscape(d.Name), nil
}

func projectPath(d adapter.Destination) (string, error) {
	if d.Kind != adapter.PagesProject {
		return "", errors.New("cloudflare: destination is not a Pages project")
	}
	base, err := accountPath(d)
	if err != nil {
		return "", err
	}
	if !projectName.MatchString(d.Name) {
		return "", errors.New("cloudflare: Pages destination requires a lowercase project name")
	}
	if d.Environment != "preview" && d.Environment != "production" {
		return "", errors.New("cloudflare: Pages destination environment must be preview or production")
	}
	return base + "/pages/projects/" + url.PathEscape(d.Name), nil
}

func (c *Client) VerifyToken(ctx context.Context, account string) (TokenStatus, error) {
	if !accountID.MatchString(account) {
		return TokenStatus{}, errors.New("cloudflare: token verification requires a 32-character hexadecimal account id")
	}
	env, err := c.do(ctx, operationRegistry["verify-account-token"], "/accounts/"+account+"/tokens/verify", nil)
	var response *ResponseError
	if err != nil && errors.As(err, &response) && response.Status >= 400 && response.Status < 500 {
		// A user-owned token is verified on the user endpoint instead.
		env, err = c.do(ctx, operationRegistry["verify-user-token"], "/user/tokens/verify", nil)
	}
	if err != nil {
		return TokenStatus{}, err
	}
	var out struct {
		Status    string `json:"status"`
		ExpiresOn string `json:"expires_on"`
	}
	if err := json.Unmarshal(env.Result, &out); err != nil {
		return TokenStatus{}, errors.New("cloudflare: token verification did not match the expected shape")
	}
	status := TokenStatus{Status: out.Status}
	if out.ExpiresOn != "" {
		expires, err := time.Parse(time.RFC3339, out.ExpiresOn)
		if err != nil {
			return TokenStatus{}, errors.New("cloudflare: token expiry did not match the expected shape")
		}
		status.ExpiresAt = expires.UTC()
		c.expiryMu.Lock()
		c.expiresAt = status.ExpiresAt
		c.expiryMu.Unlock()
	}
	return status, nil
}

func (c *Client) ListAccountIDs(ctx context.Context) ([]string, error) {
	var ids []string
	for page := 1; page <= 20; page++ {
		env, err := c.do(ctx, operationRegistry["list-accounts"], "/accounts?per_page=50&page="+strconv.Itoa(page), nil)
		if err != nil {
			return nil, err
		}
		var rows []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(env.Result, &rows); err != nil {
			return nil, errors.New("cloudflare: account listing did not match the expected shape")
		}
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		if env.ResultInfo == nil || page >= env.ResultInfo.TotalPages || len(rows) == 0 {
			return ids, nil
		}
	}
	return ids, nil
}

func (c *Client) ResolveScript(ctx context.Context, d adapter.Destination) (string, error) {
	if _, err := scriptPath(d); err != nil {
		return "", err
	}
	env, err := c.do(ctx, operationRegistry["list-scripts"], "/accounts/"+d.Owner+"/workers/scripts", nil)
	if err != nil {
		return "", err
	}
	var rows []struct {
		ID  string `json:"id"`
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(env.Result, &rows); err != nil {
		return "", errors.New("cloudflare: script listing did not match the expected shape")
	}
	for _, row := range rows {
		if row.ID == d.Name {
			if row.Tag == "" {
				return "", errors.New("cloudflare: script has no immutable tag")
			}
			return row.Tag, nil
		}
	}
	return "", &ResponseError{Status: http.StatusNotFound}
}

// ResolveProject reads the Pages project and decodes only its id and the
// names and types of the selected environment's variables. plain_text values
// Cloudflare returns in the same body are never decoded.
func (c *Client) ResolveProject(ctx context.Context, d adapter.Destination) (ProjectShape, error) {
	path, err := projectPath(d)
	if err != nil {
		return ProjectShape{}, err
	}
	env, err := c.do(ctx, operationRegistry["project-shape"], path, nil)
	if err != nil {
		return ProjectShape{}, err
	}
	var out struct {
		ID                string `json:"id"`
		Name              string `json:"name"`
		DeploymentConfigs map[string]struct {
			EnvVars map[string]*struct {
				Type string `json:"type"`
			} `json:"env_vars"`
		} `json:"deployment_configs"`
	}
	decodeErr := json.Unmarshal(env.Result, &out)
	clear(env.Result)
	if decodeErr != nil || out.ID == "" || out.Name != d.Name {
		return ProjectShape{}, errors.New("cloudflare: Pages project did not match the expected shape")
	}
	shape := ProjectShape{ID: out.ID, Names: map[string]string{}}
	for name, variable := range out.DeploymentConfigs[d.Environment].EnvVars {
		if variable == nil {
			continue
		}
		shape.Names[name] = variable.Type
	}
	return shape, nil
}

// ListBindingNames reads the script's settings and decodes only the names of
// its bindings, every type included. plain_text binding values Cloudflare
// returns in the same body are never decoded, and the buffer is cleared.
func (c *Client) ListBindingNames(ctx context.Context, d adapter.Destination) ([]string, error) {
	path, err := scriptPath(d)
	if err != nil {
		return nil, err
	}
	env, err := c.do(ctx, operationRegistry["script-binding-names"], path+"/settings", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Bindings *[]struct {
			Name string `json:"name"`
		} `json:"bindings"`
	}
	decodeErr := json.Unmarshal(env.Result, &out)
	clear(env.Result)
	if decodeErr != nil || out.Bindings == nil {
		return nil, errors.New("cloudflare: script settings did not match the expected shape")
	}
	if len(*out.Bindings) > secretNameLimit {
		return nil, errors.New("cloudflare: binding name listing exceeded the 10000-name safety limit")
	}
	names := make([]string, 0, len(*out.Bindings))
	for _, binding := range *out.Bindings {
		if binding.Name == "" {
			return nil, errors.New("cloudflare: script settings listed a binding without a name")
		}
		names = append(names, binding.Name)
	}
	return names, nil
}

// PutSecret creates or replaces one Workers secret. Cloudflare deploys a new
// version of the script for every secret write.
func (c *Client) PutSecret(ctx context.Context, d adapter.Destination, name, value string) error {
	path, err := scriptPath(d)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, operationRegistry["put-script-secret"], path+"/secrets", map[string]string{"name": name, "text": value, "type": SecretType})
	return err
}

func (c *Client) DeleteSecret(ctx context.Context, d adapter.Destination, name string) error {
	path, err := scriptPath(d)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, operationRegistry["delete-script-secret"], path+"/secrets/"+url.PathEscape(name), nil)
	return err
}

type pagesVariable struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// PatchPagesSecret sets one encrypted Pages variable, or deletes it when value
// is nil. Cloudflare merges env_vars per key, so no other variable is touched.
func (c *Client) PatchPagesSecret(ctx context.Context, d adapter.Destination, name string, value *string) error {
	path, err := projectPath(d)
	if err != nil {
		return err
	}
	var variable *pagesVariable
	if value != nil {
		variable = &pagesVariable{Type: SecretType, Value: *value}
	}
	body := map[string]any{"deployment_configs": map[string]any{d.Environment: map[string]any{"env_vars": map[string]*pagesVariable{name: variable}}}}
	_, err = c.do(ctx, operationRegistry["patch-project-secret"], path, body)
	return err
}

// Fingerprint maps a provider's immutable identity to the positive int64 the
// ledger keys ownership by. Pages preview and production of one project are
// distinct destinations and so carry distinct fingerprints.
func Fingerprint(d adapter.Destination, immutableID string) int64 {
	sum := sha256.Sum256([]byte("hikyo-cloudflare\x00" + string(d.Kind) + "\x00" + d.Owner + "\x00" + immutableID + "\x00" + d.Environment))
	id := int64(binary.BigEndian.Uint64(sum[:8]) & (1<<63 - 1))
	if id == 0 {
		return 1
	}
	return id
}

func IsStatus(err error, status int) bool {
	var response *ResponseError
	return errors.As(err, &response) && response.Status == status
}
