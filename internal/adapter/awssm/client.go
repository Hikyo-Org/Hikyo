// Package awssm is the AWS Secrets Manager deployment adapter (#158). Its
// provider interface deliberately cannot express a value read: there is no
// GetSecretValue, BatchGetSecretValue, or GetRandomPassword anywhere in it.
package awssm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/netpolicy"
)

const (
	responseCap        = 1 << 20
	listPageSize       = 100
	secretNameLimit    = 10_000
	recoveryWindowDays = 30
	serviceName        = "secretsmanager"
	// CurrentStage is the staging label Hikyo moves with every write. A value
	// written by anyone else moves AWSCURRENT without it, which is how an
	// external update is detected without reading the value.
	CurrentStage = "HIKYO_CURRENT"
	// VersionTag names the version Hikyo last wrote. An operator accepts an
	// overwrite of an external edit by setting it to that edit's version id.
	VersionTag = "HIKYO_VERSION"
	awsCurrent = "AWSCURRENT"
	// defaultThrottleWait is used when AWS throttles without Retry-After.
	defaultThrottleWait = 5 * time.Second
)

var ErrSecretListLimit = errors.New("aws-secrets-manager: secret name listing reached the 10000-name safety limit before exhaustion")

// Identity is the caller identity STS reports for the adapter credential.
type Identity struct {
	Account string
	ARN     string
}

// SecretMetadata is everything DescribeSecret reports that Hikyo uses. It is
// metadata only; Secrets Manager never returns a value from DescribeSecret.
type SecretMetadata struct {
	ARN      string
	Name     string
	KMSKeyID string
	Deleted  bool
	Tags     map[string]string
	// Stages maps each version id to its staging labels.
	Stages map[string][]string
}

// CreateSecretInput creates a secret with no version. The first value is
// written by PutSecretValue so every Hikyo version carries CurrentStage.
type CreateSecretInput struct {
	Name     string
	KMSKeyID string
	Tags     map[string]string
}

// API is the complete provider surface. The closure test pins this exact
// method set; adding a value-returning operation fails the build's tests.
type API interface {
	ResolveIdentity(context.Context) (Identity, error)
	DescribeSecret(context.Context, string) (SecretMetadata, error)
	ListSecretNames(ctx context.Context, prefix string, limit int) ([]string, error)
	CreateSecret(context.Context, CreateSecretInput) error
	PutSecretValue(ctx context.Context, name, token, value string) error
	TagSecret(ctx context.Context, name string, tags map[string]string) error
	RestoreSecret(context.Context, string) error
	DeleteSecret(context.Context, string) error
}

// operationRegistry is the closed Secrets Manager surface this client can
// sign. Anything not listed here cannot be sent.
var operationRegistry = map[string]string{
	"describe-secret":  "DescribeSecret",
	"list-secrets":     "ListSecrets",
	"create-secret":    "CreateSecret",
	"put-secret-value": "PutSecretValue",
	"tag-resource":     "TagResource",
	"restore-secret":   "RestoreSecret",
	"delete-secret":    "DeleteSecret",
}

type ClientConfig struct {
	Origin       string
	Credential   string
	AllowedCIDRs []netip.Prefix
	Deadline     time.Duration
	// WorkloadIdentity is the instance operator's opt-in for descriptors that
	// borrow the server's own AWS identity.
	WorkloadIdentity bool
	// RootCAs trusts a private endpoint or emulator certificate. Nil uses the
	// system roots, which is the only production configuration.
	RootCAs *x509.CertPool
}

type Client struct {
	route       route
	http        *http.Client
	credentials aws.CredentialsProvider
	sts         *sts.Client
	signer      *v4.Signer
	now         func() time.Time
}

var _ API = (*Client)(nil)

func NewClient(cfg ClientConfig) (*Client, error) {
	return newClient(cfg, net.DefaultResolver, &net.Dialer{Timeout: cfg.Deadline})
}

func newClient(cfg ClientConfig, resolver netpolicy.Resolver, dialer netpolicy.Dialer) (*Client, error) {
	if cfg.Deadline <= 0 {
		return nil, errors.New("aws-secrets-manager: a request deadline is required")
	}
	if cfg.Deadline >= adapter.LeaseTime {
		return nil, errors.New("aws-secrets-manager: request deadline must be shorter than the provider-write lease")
	}
	descriptor, err := ParseDescriptor(cfg.Credential)
	if err != nil {
		return nil, configError(err)
	}
	r, err := resolveRoute(cfg.Origin, descriptor)
	if err != nil {
		return nil, configError(err)
	}
	publicDialer, err := netpolicy.NewPublicDialer(cfg.AllowedCIDRs, resolver, dialer)
	if err != nil {
		return nil, fmt.Errorf("aws-secrets-manager: egress policy: %w", err)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: cfg.RootCAs}
	vetted := &http.Client{
		Transport: &http.Transport{
			Proxy:           nil,
			TLSClientConfig: tlsConfig,
			DialContext:     publicDialer.DialContext,
		},
		Timeout: cfg.Deadline,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("aws-secrets-manager: redirects are refused")
		},
	}
	creds, err := credentialProvider(descriptor, r, vetted, cfg.WorkloadIdentity)
	if err != nil {
		return nil, configError(err)
	}
	return &Client{
		route: r, http: vetted, credentials: creds,
		sts:    newSTS(r, creds, vetted),
		signer: v4.NewSigner(),
		now:    time.Now,
	}, nil
}

// Region is the signing region the client resolved from origin and descriptor.
func (c *Client) Region() string { return c.route.region }

// Forget releases the private transport and credential cache when one outbox
// attempt ends. No other module lease shares them.
func (c *Client) Forget() {
	c.credentials = nil
	c.sts = nil
	c.http.CloseIdleConnections()
}

// ResponseError is a refused Secrets Manager request. Code is AWS's closed
// error type name; the message is dropped because a provider can echo request
// material, and request material may be plaintext.
type ResponseError struct {
	Status int
	Code   string
}

func (e *ResponseError) Error() string {
	if e.Code == "" {
		return "aws-secrets-manager: provider refused request with status " + strconv.Itoa(e.Status)
	}
	return "aws-secrets-manager: provider refused request with " + e.Code + " (status " + strconv.Itoa(e.Status) + ")"
}

// Definite reports that AWS answered and refused, so nothing was applied.
func (e *ResponseError) Definite() bool { return e.Status >= 400 && e.Status < 500 }

// throttled carries an authoritative retry deadline through the outbox.
type throttled struct {
	*ResponseError
	at time.Time
}

func (e *throttled) RetryAt() time.Time { return e.at }
func (e *throttled) Unwrap() []error    { return []error{adapter.ErrRateLimited, e.ResponseError} }

var authCodes = map[string]bool{
	"AccessDeniedException": true, "UnrecognizedClientException": true, "InvalidSignatureException": true,
	"SignatureDoesNotMatch": true, "IncompleteSignature": true, "MissingAuthenticationToken": true,
	"InvalidClientTokenId": true, "ExpiredToken": true, "ExpiredTokenException": true, "AccessDenied": true,
	// KMS refusals surface through Secrets Manager as these two types; the
	// operator fixes them in IAM or the key policy, so they are auth, not retry.
	"EncryptionFailure": true, "DecryptionFailure": true,
}

var throttleCodes = map[string]bool{
	"ThrottlingException": true, "Throttling": true, "TooManyRequestsException": true,
	"RequestLimitExceeded": true, "LimitExceededException": true,
}

func codeOf(err error) string {
	var response *ResponseError
	if errors.As(err, &response) {
		return response.Code
	}
	return ""
}

func IsNotFound(err error) bool { return codeOf(err) == "ResourceNotFoundException" }
func IsExists(err error) bool   { return codeOf(err) == "ResourceExistsException" }

// IsDefinite reports that AWS answered and refused the request.
func IsDefinite(err error) bool {
	var response *ResponseError
	return errors.As(err, &response) && response.Definite()
}

func (c *Client) do(ctx context.Context, operation string, in, out any) error {
	target, ok := operationRegistry[operation]
	if !ok {
		return fmt.Errorf("aws-secrets-manager: operation %q is outside the closed registry", operation)
	}
	if c.credentials == nil {
		return errors.New("aws-secrets-manager: client was released")
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	creds, err := c.credentials.Retrieve(ctx)
	if err != nil {
		return credentialError(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.route.origin+"/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager."+target)
	sum := sha256.Sum256(body)
	if err := c.signer.SignHTTP(ctx, creds, req, hex.EncodeToString(sum[:]), serviceName, c.route.region, c.now().UTC()); err != nil {
		return errors.New("aws-secrets-manager: request could not be signed")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("aws-secrets-manager: provider request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseCap+1))
	if err != nil {
		return errors.New("aws-secrets-manager: provider response could not be read")
	}
	if len(raw) > responseCap {
		return errors.New("aws-secrets-manager: provider response exceeded 1 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.responseError(resp, raw)
	}
	if out != nil && len(raw) != 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return errors.New("aws-secrets-manager: provider response did not match the expected shape")
		}
	}
	return nil
}

func (c *Client) responseError(resp *http.Response, raw []byte) error {
	code := errorCode(resp.Header.Get("X-Amzn-ErrorType"))
	if code == "" {
		var envelope struct {
			Type string `json:"__type"`
		}
		if json.Unmarshal(raw, &envelope) == nil {
			code = errorCode(envelope.Type)
		}
	}
	response := &ResponseError{Status: resp.StatusCode, Code: code}
	switch {
	case authCodes[code], code == "" && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden):
		return errors.Join(adapter.ErrProviderAuth, response)
	case throttleCodes[code], resp.StatusCode == http.StatusTooManyRequests:
		return &throttled{ResponseError: response, at: retryAfter(resp.Header.Get("Retry-After"), c.now())}
	}
	return response
}

// errorCode normalizes "prefix#Code" and "Code:detail" forms to the bare code,
// keeping only a bounded identifier.
func errorCode(raw string) string {
	if i := strings.LastIndexByte(raw, '#'); i >= 0 {
		raw = raw[i+1:]
	}
	if i := strings.IndexByte(raw, ':'); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)
	if len(raw) > 64 {
		return ""
	}
	for _, r := range raw {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	return raw
}

func retryAfter(header string, now time.Time) time.Time {
	if header != "" {
		if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds >= 0 {
			seconds = min(seconds, int(adapter.RetryCap/time.Second))
			return now.Add(time.Duration(seconds) * time.Second)
		}
		if at, err := http.ParseTime(header); err == nil {
			return at
		}
	}
	return now.Add(defaultThrottleWait)
}

// credentialError keeps credential-source refusals (an STS or IMDS answer)
// fatal while leaving transport failures retryable.
func credentialError(err error) error {
	var api smithy.APIError
	if errors.As(err, &api) {
		code := errorCode(api.ErrorCode())
		if throttleCodes[code] {
			return errors.Join(adapter.ErrRateLimited, fmt.Errorf("aws-secrets-manager: credential source throttled (%s)", code))
		}
		return errors.Join(adapter.ErrProviderAuth, fmt.Errorf("aws-secrets-manager: credential source refused (%s)", code))
	}
	if errors.Is(err, ErrWorkloadIdentityDisabled) {
		return errors.Join(adapter.ErrProviderAuth, ErrWorkloadIdentityDisabled)
	}
	return errors.New("aws-secrets-manager: credentials could not be obtained")
}

func (c *Client) ResolveIdentity(ctx context.Context) (Identity, error) {
	if c.sts == nil {
		return Identity{}, errors.New("aws-secrets-manager: client was released")
	}
	out, err := c.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		var api smithy.APIError
		if errors.As(err, &api) {
			return Identity{}, credentialError(err)
		}
		return Identity{}, errors.New("aws-secrets-manager: STS caller identity could not be resolved")
	}
	return Identity{Account: aws.ToString(out.Account), ARN: aws.ToString(out.Arn)}, nil
}

type tag struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

func tagList(tags map[string]string) []tag {
	out := make([]tag, 0, len(tags))
	for key, value := range tags {
		out = append(out, tag{Key: key, Value: value})
	}
	return out
}

func (c *Client) DescribeSecret(ctx context.Context, name string) (SecretMetadata, error) {
	var out struct {
		ARN                string              `json:"ARN"`
		Name               string              `json:"Name"`
		KmsKeyID           string              `json:"KmsKeyId"`
		DeletedDate        *json.Number        `json:"DeletedDate"`
		Tags               []tag               `json:"Tags"`
		VersionIdsToStages map[string][]string `json:"VersionIdsToStages"`
	}
	if err := c.do(ctx, "describe-secret", map[string]string{"SecretId": name}, &out); err != nil {
		return SecretMetadata{}, err
	}
	meta := SecretMetadata{ARN: out.ARN, Name: out.Name, KMSKeyID: out.KmsKeyID, Deleted: out.DeletedDate != nil, Tags: map[string]string{}, Stages: out.VersionIdsToStages}
	for _, t := range out.Tags {
		meta.Tags[t.Key] = t.Value
	}
	if meta.Stages == nil {
		meta.Stages = map[string][]string{}
	}
	return meta, nil
}

// ListSecretNames pages ListSecrets. AWS's name filter is a case-insensitive
// prefix match, so the exact prefix is re-checked here. limit 0 means all
// names up to the safety limit.
func (c *Client) ListSecretNames(ctx context.Context, prefix string, limit int) ([]string, error) {
	type filter struct {
		Key    string   `json:"Key"`
		Values []string `json:"Values"`
	}
	request := struct {
		Filters                []filter `json:"Filters,omitempty"`
		MaxResults             int      `json:"MaxResults"`
		NextToken              string   `json:"NextToken,omitempty"`
		IncludePlannedDeletion bool     `json:"IncludePlannedDeletion"`
	}{MaxResults: listPageSize, IncludePlannedDeletion: true}
	if prefix != "" {
		request.Filters = []filter{{Key: "name", Values: []string{prefix}}}
	}
	if limit > 0 && limit < listPageSize {
		request.MaxResults = limit
	}
	var names []string
	for {
		var page struct {
			SecretList []struct {
				Name string `json:"Name"`
			} `json:"SecretList"`
			NextToken string `json:"NextToken"`
		}
		if err := c.do(ctx, "list-secrets", request, &page); err != nil {
			return nil, err
		}
		for _, entry := range page.SecretList {
			if strings.HasPrefix(entry.Name, prefix) {
				names = append(names, entry.Name)
			}
		}
		if limit > 0 && len(names) >= limit {
			return names[:limit], nil
		}
		if len(names) > secretNameLimit {
			return nil, ErrSecretListLimit
		}
		if page.NextToken == "" {
			return names, nil
		}
		request.NextToken = page.NextToken
	}
}

func (c *Client) CreateSecret(ctx context.Context, input CreateSecretInput) error {
	request := struct {
		Name        string `json:"Name"`
		Description string `json:"Description"`
		KmsKeyID    string `json:"KmsKeyId,omitempty"`
		Tags        []tag  `json:"Tags"`
	}{Name: input.Name, Description: "Managed by Hikyo. Values are overwritten on every sync.", KmsKeyID: input.KMSKeyID, Tags: tagList(input.Tags)}
	return c.do(ctx, "create-secret", request, nil)
}

// PutSecretValue writes one version under an idempotency token and moves both
// AWSCURRENT and CurrentStage to it atomically.
func (c *Client) PutSecretValue(ctx context.Context, name, token, value string) error {
	request := struct {
		SecretID           string   `json:"SecretId"`
		ClientRequestToken string   `json:"ClientRequestToken"`
		SecretString       string   `json:"SecretString"`
		VersionStages      []string `json:"VersionStages"`
	}{SecretID: name, ClientRequestToken: token, SecretString: value, VersionStages: []string{awsCurrent, CurrentStage}}
	return c.do(ctx, "put-secret-value", request, nil)
}

func (c *Client) TagSecret(ctx context.Context, name string, tags map[string]string) error {
	request := struct {
		SecretID string `json:"SecretId"`
		Tags     []tag  `json:"Tags"`
	}{SecretID: name, Tags: tagList(tags)}
	return c.do(ctx, "tag-resource", request, nil)
}

func (c *Client) RestoreSecret(ctx context.Context, name string) error {
	return c.do(ctx, "restore-secret", map[string]string{"SecretId": name}, nil)
}

// DeleteSecret always schedules deletion with the 30-day recovery window.
// ForceDeleteWithoutRecovery is not expressible.
func (c *Client) DeleteSecret(ctx context.Context, name string) error {
	request := struct {
		SecretID             string `json:"SecretId"`
		RecoveryWindowInDays int    `json:"RecoveryWindowInDays"`
	}{SecretID: name, RecoveryWindowInDays: recoveryWindowDays}
	return c.do(ctx, "delete-secret", request, nil)
}
