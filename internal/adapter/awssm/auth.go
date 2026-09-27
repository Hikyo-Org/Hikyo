package awssm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// AuthMode is the closed set of ways the adapter obtains AWS credentials.
type AuthMode string

const (
	// AuthAmbient uses the server's default AWS credential chain: environment,
	// shared config, web identity (IRSA), ECS task role, or EC2 instance role.
	AuthAmbient AuthMode = "ambient"
	// AuthAssumeRole assumes a role with ambient credentials as the source.
	AuthAssumeRole AuthMode = "assume-role"
	// AuthWebIdentity assumes a role with the server's projected web identity
	// token (AWS_WEB_IDENTITY_TOKEN_FILE). The descriptor cannot name the file.
	AuthWebIdentity AuthMode = "web-identity"
	// AuthStatic uses an explicitly supplied access key. It is the only mode
	// whose descriptor carries secret material.
	AuthStatic AuthMode = "static"
)

const (
	defaultSessionSeconds = 900
	minSessionSeconds     = 900
	maxSessionSeconds     = 3600
	roleSessionName       = "hikyo-adapter"
	descriptorLimit       = 4096
)

var (
	// ErrWorkloadIdentityDisabled refuses every mode that borrows the server's
	// own AWS identity until the instance operator opts in. Without the opt-in a
	// project administrator could write wherever the server's role can.
	ErrWorkloadIdentityDisabled = errors.New("aws-secrets-manager: server workload identity is disabled; the instance operator must set HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY=allow, or use static credentials")

	awsRegion  = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`)
	awsRoleARN = regexp.MustCompile(`^arn:aws(-cn|-us-gov)?:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$`)
	awsKeyID   = regexp.MustCompile(`^[A-Z0-9]{16,128}$`)
	externalID = regexp.MustCompile(`^[A-Za-z0-9+=,.@:/-]+$`)
	endpoint   = regexp.MustCompile(`^secretsmanager(-fips)?\.([a-z0-9-]+)\.amazonaws\.com(\.cn)?$`)
)

// ConfigError is a caller-correctable refusal of an origin, access
// descriptor, or destination. Its text is built only from fixed wording and
// the caller's own non-secret inputs, so the transport may return it as the
// refusal's safe detail.
type ConfigError struct{ err error }

func (e *ConfigError) Error() string      { return e.err.Error() }
func (e *ConfigError) SafeDetail() string { return e.err.Error() }
func (e *ConfigError) Unwrap() error      { return e.err }

func configError(err error) error {
	if err == nil {
		return nil
	}
	return &ConfigError{err: err}
}

// Descriptor is the adapter's write-only access descriptor. It is sealed as
// the adapter credential, so none of it (the static secret key included) is
// ever returned by an ordinary read, logged, or placed in an audit payload.
type Descriptor struct {
	Mode           AuthMode `json:"mode"`
	Region         string   `json:"region,omitempty"`
	RoleARN        string   `json:"role_arn,omitempty"`
	ExternalID     string   `json:"external_id,omitempty"`
	SessionSeconds int      `json:"session_seconds,omitempty"`
	AccessKeyID    string   `json:"access_key_id,omitempty"`
	SecretKey      string   `json:"secret_access_key,omitempty"`
	SessionToken   string   `json:"session_token,omitempty"`
	// STSOrigin overrides the STS endpoint for a non-AWS origin, such as a
	// VPC endpoint pair or a local emulator. AWS origins derive it.
	STSOrigin string `json:"sts_origin,omitempty"`
}

// ParseDescriptor strictly decodes and validates one descriptor.
func ParseDescriptor(raw string) (Descriptor, error) {
	if len(raw) == 0 || len(raw) > descriptorLimit {
		return Descriptor{}, errors.New("aws-secrets-manager: credential must be a JSON access descriptor of at most 4096 bytes")
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	var d Descriptor
	if err := decoder.Decode(&d); err != nil {
		// The decoder error can quote descriptor bytes; never surface it.
		return Descriptor{}, errors.New("aws-secrets-manager: credential is not a valid JSON access descriptor")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Descriptor{}, errors.New("aws-secrets-manager: credential holds trailing data after the access descriptor")
	}
	return d, d.validate()
}

func (d Descriptor) validate() error {
	if d.Region != "" && !awsRegion.MatchString(d.Region) {
		return fmt.Errorf("aws-secrets-manager: region %q is not an AWS region name", d.Region)
	}
	if d.STSOrigin != "" {
		if _, err := canonicalOrigin(d.STSOrigin); err != nil {
			return fmt.Errorf("aws-secrets-manager: sts_origin: %w", err)
		}
	}
	role := d.RoleARN != "" || d.ExternalID != "" || d.SessionSeconds != 0
	static := d.AccessKeyID != "" || d.SecretKey != "" || d.SessionToken != ""
	switch d.Mode {
	case AuthAmbient:
		if role || static {
			return errors.New("aws-secrets-manager: ambient mode takes no role or static key fields")
		}
	case AuthAssumeRole, AuthWebIdentity:
		if static {
			return fmt.Errorf("aws-secrets-manager: %s mode takes no static key fields", d.Mode)
		}
		if len(d.RoleARN) > 2048 || !awsRoleARN.MatchString(d.RoleARN) {
			return fmt.Errorf("aws-secrets-manager: %s mode requires an IAM role ARN", d.Mode)
		}
		if d.Mode == AuthWebIdentity && d.ExternalID != "" {
			return errors.New("aws-secrets-manager: web-identity mode does not take an external id")
		}
		if d.ExternalID != "" && (len(d.ExternalID) < 2 || len(d.ExternalID) > 1224 || !externalID.MatchString(d.ExternalID)) {
			return errors.New("aws-secrets-manager: external id must be 2-1224 characters of A-Z a-z 0-9 + = , . @ : / -")
		}
		if d.SessionSeconds != 0 && (d.SessionSeconds < minSessionSeconds || d.SessionSeconds > maxSessionSeconds) {
			return fmt.Errorf("aws-secrets-manager: session_seconds must be between %d and %d", minSessionSeconds, maxSessionSeconds)
		}
	case AuthStatic:
		if role {
			return errors.New("aws-secrets-manager: static mode takes no role fields")
		}
		if !awsKeyID.MatchString(d.AccessKeyID) || d.SecretKey == "" {
			return errors.New("aws-secrets-manager: static mode requires access_key_id and secret_access_key")
		}
	default:
		return errors.New("aws-secrets-manager: mode must be ambient, assume-role, web-identity, or static")
	}
	return nil
}

func (d Descriptor) usesWorkloadIdentity() bool {
	return d.Mode == AuthAmbient || d.Mode == AuthAssumeRole || d.Mode == AuthWebIdentity
}

func (d Descriptor) sessionDuration() time.Duration {
	if d.SessionSeconds == 0 {
		return defaultSessionSeconds * time.Second
	}
	return time.Duration(d.SessionSeconds) * time.Second
}

// route is the resolved Secrets Manager endpoint, signing region, and STS
// endpoint for one adapter origin.
type route struct {
	origin string
	region string
	sts    string
	aws    bool
}

// canonicalOrigin accepts only a bare https origin.
func canonicalOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("aws-secrets-manager: origin is not a URL")
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("aws-secrets-manager: origin must be a bare https origin")
	}
	return "https://" + strings.ToLower(u.Host), nil
}

// ValidateOrigin checks an adapter origin without a descriptor. AWS regional
// endpoints carry their region; any other origin must name a region in the
// descriptor, which resolveRoute enforces once the credential is available.
func ValidateOrigin(raw string) error {
	_, err := canonicalOrigin(raw)
	return err
}

func resolveRoute(rawOrigin string, d Descriptor) (route, error) {
	origin, err := canonicalOrigin(rawOrigin)
	if err != nil {
		return route{}, err
	}
	host := strings.TrimPrefix(origin, "https://")
	if match := endpoint.FindStringSubmatch(host); match != nil {
		region := match[2]
		if !awsRegion.MatchString(region) {
			return route{}, fmt.Errorf("aws-secrets-manager: origin region %q is not an AWS region name", region)
		}
		if d.Region != "" && d.Region != region {
			return route{}, fmt.Errorf("aws-secrets-manager: descriptor region %q does not match origin region %q", d.Region, region)
		}
		if d.STSOrigin != "" {
			return route{}, errors.New("aws-secrets-manager: sts_origin applies only to a non-AWS origin")
		}
		stsHost := "sts"
		if match[1] != "" {
			stsHost = "sts-fips"
		}
		return route{origin: origin, region: region, sts: "https://" + stsHost + "." + region + ".amazonaws.com" + match[3], aws: true}, nil
	}
	if d.Region == "" {
		return route{}, errors.New("aws-secrets-manager: a non-AWS origin (VPC endpoint or emulator) requires region in the access descriptor")
	}
	if d.usesWorkloadIdentity() {
		// STS receives the node's own identity material: the projected web
		// identity token, or a signed AssumeRole a receiver could replay for
		// credentials. A tenant-configured endpoint never gets it; these
		// modes always use AWS's regional STS.
		if d.STSOrigin != "" {
			return route{}, errors.New("aws-secrets-manager: sts_origin applies only to static credentials; workload identity always uses AWS STS")
		}
		return route{origin: origin, region: d.Region, sts: regionalSTS(d.Region)}, nil
	}
	sts := origin
	if d.STSOrigin != "" {
		sts, _ = canonicalOrigin(d.STSOrigin)
	}
	return route{origin: origin, region: d.Region, sts: sts}, nil
}

func regionalSTS(region string) string {
	if strings.HasPrefix(region, "cn-") {
		return "https://sts." + region + ".amazonaws.com.cn"
	}
	return "https://sts." + region + ".amazonaws.com"
}

// credentialProvider builds the cached credential source for one descriptor.
// Ambient discovery (IMDS, ECS, IRSA files) must reach link-local and private
// addresses, so it keeps the SDK transport; every STS call Hikyo originates
// goes through the vetted public-egress client.
func credentialProvider(d Descriptor, r route, vetted *http.Client, workloadIdentity bool) (aws.CredentialsProvider, error) {
	if d.usesWorkloadIdentity() && !workloadIdentity {
		return nil, ErrWorkloadIdentityDisabled
	}
	switch d.Mode {
	case AuthStatic:
		return aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(d.AccessKeyID, d.SecretKey, d.SessionToken)), nil
	case AuthAmbient:
		return ambientProvider(r.region)
	case AuthAssumeRole:
		source, err := ambientProvider(r.region)
		if err != nil {
			return nil, err
		}
		client := newSTS(r, source, vetted)
		return aws.NewCredentialsCache(stscreds.NewAssumeRoleProvider(client, d.RoleARN, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = roleSessionName
			o.Duration = d.sessionDuration()
			if d.ExternalID != "" {
				o.ExternalID = aws.String(d.ExternalID)
			}
		})), nil
	case AuthWebIdentity:
		tokenFile := os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE")
		if tokenFile == "" {
			return nil, errors.New("aws-secrets-manager: web-identity mode requires the server to run with AWS_WEB_IDENTITY_TOKEN_FILE")
		}
		client := newSTS(r, aws.AnonymousCredentials{}, vetted)
		return aws.NewCredentialsCache(stscreds.NewWebIdentityRoleProvider(client, d.RoleARN, stscreds.IdentityTokenFile(tokenFile), func(o *stscreds.WebIdentityRoleOptions) {
			o.RoleSessionName = roleSessionName
			o.Duration = d.sessionDuration()
		})), nil
	default:
		return nil, errors.New("aws-secrets-manager: unsupported authentication mode")
	}
}

func ambientProvider(region string) (aws.CredentialsProvider, error) {
	// LoadDefaultConfig resolves sources lazily; no credential I/O happens here.
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		return nil, errors.New("aws-secrets-manager: server AWS configuration could not be loaded")
	}
	if cfg.Credentials == nil {
		return nil, errors.New("aws-secrets-manager: server has no ambient AWS credential source")
	}
	return cfg.Credentials, nil
}

func newSTS(r route, creds aws.CredentialsProvider, vetted *http.Client) *sts.Client {
	return sts.New(sts.Options{
		Region:       r.region,
		Credentials:  creds,
		HTTPClient:   vetted,
		BaseEndpoint: aws.String(r.sts),
	})
}
