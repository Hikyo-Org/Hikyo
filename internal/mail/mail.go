// Package mail prepares immutable SMTP configuration and sends text-only mail.
// Preparation never resolves DNS, opens connections, or reads operator files.
package mail

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	stdmail "net/mail"
	"net/netip"
	"strconv"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/schema"
)

// Config contains effective mail values. Password is used byte-for-byte.
type Config struct {
	Addr, TLS, User, Password, From, EHLO, AllowedCIDRs, CAPEM string
}

// Client is immutable after construction. Each send owns its connection.
type Client struct {
	configured   bool
	cfg          Config
	host         string
	port         int
	tlsConfig    *tls.Config
	allowedCIDRs []netip.Prefix
}

func New(cfg Config) (*Client, error) {
	if cfg == (Config{}) {
		return &Client{}, nil
	}
	for _, field := range []struct{ key, value string }{
		{"ADDR", cfg.Addr}, {"TLS", cfg.TLS}, {"USER", cfg.User}, {"PASSWORD", cfg.Password},
		{"FROM", cfg.From}, {"EHLO", cfg.EHLO}, {"ALLOWED_CIDRS", cfg.AllowedCIDRs}, {"CA_PEM", cfg.CAPEM},
	} {
		if len(field.value) > schema.MaxValueBytes {
			return nil, invalid(field.key, "exceeds the value size limit")
		}
	}
	host, portText, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return nil, invalid("ADDR", "must be host:port")
	}
	if _, err := netip.ParseAddr(host); err != nil && !hostname(host) {
		return nil, invalid("ADDR", "must contain a valid hostname or IP address")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, invalid("ADDR", "must contain a port from 1 through 65535")
	}
	if cfg.TLS != "implicit" && cfg.TLS != "starttls" {
		return nil, invalid("TLS", "must be implicit or starttls")
	}
	if _, err := stdmail.ParseAddress(cfg.From); err != nil || strings.ContainsAny(cfg.From, "\r\n") {
		return nil, invalid("FROM", "must be one RFC 5322 address")
	}
	if cfg.User != "" && cfg.Password == "" {
		return nil, invalid("PASSWORD", "is required when HIKYO_MAIL_USER is set")
	}
	if cfg.Password != "" && cfg.User == "" {
		return nil, invalid("USER", "is required when HIKYO_MAIL_PASSWORD is set")
	}
	if cfg.EHLO != "" && !hostname(cfg.EHLO) {
		return nil, invalid("EHLO", "must be a hostname")
	}
	var allowed []netip.Prefix
	if cfg.AllowedCIDRs != "" {
		for part := range strings.SplitSeq(cfg.AllowedCIDRs, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
			if err != nil {
				return nil, invalid("ALLOWED_CIDRS", "must be a comma-separated list of CIDRs")
			}
			allowed = append(allowed, prefix.Masked())
		}
	}
	var roots *x509.CertPool
	if cfg.CAPEM != "" {
		roots = x509.NewCertPool()
		remaining := bytes.TrimSpace([]byte(cfg.CAPEM))
		if len(remaining) == 0 {
			return nil, invalid("CA_PEM", "must contain certificate PEM blocks")
		}
		for len(remaining) > 0 {
			if !bytes.HasPrefix(remaining, []byte("-----BEGIN CERTIFICATE-----")) {
				return nil, invalid("CA_PEM", "must contain only certificate PEM blocks")
			}
			block, rest := pem.Decode(remaining)
			if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
				return nil, invalid("CA_PEM", "must contain only certificate PEM blocks")
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, invalid("CA_PEM", "contains an invalid certificate")
			}
			roots.AddCert(cert)
			remaining = bytes.TrimSpace(rest)
		}
	}
	return &Client{configured: true, cfg: cfg, host: host, port: port, allowedCIDRs: allowed,
		tlsConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host, RootCAs: roots}}, nil
}

func (c *Client) Configured() bool { return c != nil && c.configured }

func invalid(key, reason string) error { return fmt.Errorf("HIKYO_MAIL_%s %s", key, reason) }

func hostname(value string) bool {
	value = strings.TrimSuffix(value, ".")
	if len(value) == 0 || len(value) > 253 {
		return false
	}
	for label := range strings.SplitSeq(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-') {
				return false
			}
		}
	}
	return true
}
