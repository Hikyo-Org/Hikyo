package adapter

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

var awsCustodyEndpoint = regexp.MustCompile(`^secretsmanager(-fips)?\.([a-z0-9-]+)\.amazonaws\.com(\.cn)?$`)
var awsCustodyVPCEndpoint = regexp.MustCompile(`^vpce-[a-z0-9-]+(?:\.|-)secretsmanager(-fips)?\.([a-z0-9-]+)\.vpce\.amazonaws\.com(\.cn)?$`)
var awsCustodyRegion = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`)

// AWSOriginRegion identifies the admitted AWS-owned endpoint namespace without
// rewriting transport or disabling FIPS. Account identity stays in the verified
// destination. Custom endpoints do not establish AWS-wide account authority;
// their custody namespace remains the exact canonical endpoint.
func AWSOriginRegion(canonical string) (string, bool) {
	u, err := url.Parse(canonical)
	if err != nil || u.Scheme != "https" || u.Port() != "" || u.Path != "" {
		return "", false
	}
	match := awsCustodyEndpoint.FindStringSubmatch(u.Hostname())
	if match == nil {
		match = awsCustodyVPCEndpoint.FindStringSubmatch(u.Hostname())
	}
	if match == nil || !awsCustodyRegion.MatchString(match[2]) || strings.HasPrefix(match[2], "cn-") != (match[3] == ".cn") {
		return "", false
	}
	return match[2] + match[3], true
}

// CanonicalOrigin is the sole URL-spelling owner for configured provider
// endpoints and custody comparisons. It never changes the selected service,
// meaningful relative root, Vault namespace, or non-default port.
func CanonicalOrigin(provider Provider, raw string) (string, error) {
	if _, err := ParseProvider(string(provider)); err != nil {
		return "", err
	}
	if raw == "" {
		switch provider {
		case GitHubActionsProvider:
			raw = "https://api.github.com"
		case GitLabProvider:
			raw = "https://gitlab.com"
		case CloudflareProvider:
			raw = "https://api.cloudflare.com"
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s: parse origin: %w", provider, err)
	}
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", fmt.Errorf("%s: origin must be HTTPS without credentials, query, or fragment", provider)
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return "", fmt.Errorf("%s: scoped IP origins are not supported", provider)
		}
		host = ip.Unmap().String()
	} else {
		host, err = idna.Lookup.ToASCII(host)
		if err != nil || host == "" || strings.ContainsAny(host, ":%/\\") {
			return "", fmt.Errorf("%s: origin must name a valid host", provider)
		}
		host = strings.ToLower(host)
	}
	port := u.Port()
	if strings.HasSuffix(u.Host, ":") {
		return "", fmt.Errorf("%s: origin has an empty port", provider)
	}
	if port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return "", fmt.Errorf("%s: origin port must be between 1 and 65535", provider)
		}
		port = strconv.FormatUint(n, 10)
	}
	if port != "" && port != "443" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	path := ""
	switch provider {
	case ForgejoProvider, SealedWebhookProvider, AWSSecretsManagerProvider, CloudflareProvider:
		if u.Path != "" && u.Path != "/" {
			return "", fmt.Errorf("%s: origin must be a bare https origin", provider)
		}
	case GitHubActionsProvider:
		path = strings.TrimSuffix(u.EscapedPath(), "/")
		if path != "" && path != "/api/v3" {
			return "", fmt.Errorf("%s: origin must be https://api.github.com or an HTTPS GHES /api/v3 base URL", provider)
		}
	case GitLabProvider:
		// Encoded separators and dot segments have proxy-dependent routing
		// semantics. They cannot establish a unique custody namespace.
		if strings.Contains(strings.ToLower(u.RawPath), "%2f") || strings.Contains(strings.ToLower(u.RawPath), "%5c") || strings.Contains(u.Path, "\\") {
			return "", fmt.Errorf("%s: origin root contains an ambiguous separator", provider)
		}
		path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/api/v4")
		for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
			if segment == "." || segment == ".." || (segment == "" && path != "") {
				return "", fmt.Errorf("%s: origin root contains an ambiguous segment", provider)
			}
		}
		path = (&url.URL{Path: path}).EscapedPath()
	case VaultKVProvider:
		if u.RawPath != "" {
			return "", fmt.Errorf("%s: origin namespace must not be escaped", provider)
		}
		namespace := strings.Trim(u.Path, "/")
		if namespace != "" {
			if strings.HasSuffix(u.Path, "/") || namespace == "root" {
				return "", fmt.Errorf("%s: omit root namespace and do not end namespace with a slash", provider)
			}
			for _, segment := range strings.Split(namespace, "/") {
				if segment == "" {
					return "", fmt.Errorf("%s: invalid namespace segment", provider)
				}
				for _, ch := range segment {
					if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
						return "", fmt.Errorf("%s: invalid namespace segment", provider)
					}
				}
			}
			path = "/" + namespace
		} else if u.Path != "" && u.Path != "/" {
			return "", fmt.Errorf("%s: origin path must name a namespace", provider)
		}
	}
	canonical := "https://" + host + path
	if provider == CloudflareProvider && canonical != "https://api.cloudflare.com" {
		return "", fmt.Errorf("%s: origin must be https://api.cloudflare.com", provider)
	}
	return canonical, nil
}
