package vaultkv

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// AuthMethod is the closed set of Vault/OpenBao login methods the adapter
// accepts. JWT/OIDC login is deliberately absent in v1: Hikyo has no issuer
// for its own workload identity, and a caller-supplied JWT is a static bearer
// with extra steps (docs/adr/vault-kv-adapter.md, "Authentication").
type AuthMethod string

const (
	TokenAuth   AuthMethod = "token"
	AppRoleAuth AuthMethod = "approle"
)

// Credential is the parsed, sealed adapter credential. It is protected input:
// it arrives only through no-echo TTY, --stdin, or --value-file intake, is
// sealed under PurposeAdapter, and never appears in argv, logs, audit payloads
// or any read. Trust material rides inside it because the adapter record has
// no provider-config column; rotating trust is a credential replacement.
type Credential struct {
	Method     AuthMethod
	Token      string
	RoleID     string
	SecretID   string
	Mount      string
	CAPEM      string
	SPKISHA256 string
	rootsPool  *x509.CertPool
	pinnedSPKI []byte
}

type credentialJSON struct {
	Method     string `json:"method"`
	Token      string `json:"token"`
	RoleID     string `json:"role_id"`
	SecretID   string `json:"secret_id"`
	Mount      string `json:"mount"`
	CAPEM      string `json:"ca_pem"`
	SPKISHA256 string `json:"spki_sha256"`
}

var authMountSyntax = regexp.MustCompile(`^[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*$`)

// ParseCredential accepts either a bare Vault/OpenBao token or a JSON object:
//
//	{"method":"token","token":"...","ca_pem":"...","spki_sha256":"..."}
//	{"method":"approle","role_id":"...","secret_id":"...","mount":"approle"}
//
// Unknown fields are refused so a typo cannot silently drop a trust pin.
// Errors never echo the input.
func ParseCredential(raw string) (Credential, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Credential{}, errors.New("vault-kv: a token or an AppRole credential is required")
	}
	if !strings.HasPrefix(trimmed, "{") {
		if strings.ContainsAny(trimmed, " \t\r\n") {
			return Credential{}, errors.New("vault-kv: a bare token must not contain whitespace")
		}
		return Credential{Method: TokenAuth, Token: trimmed}, nil
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var encoded credentialJSON
	if err := decoder.Decode(&encoded); err != nil {
		return Credential{}, errors.New("vault-kv: credential JSON is malformed or names an unknown field")
	}
	if decoder.More() {
		return Credential{}, errors.New("vault-kv: credential JSON has trailing content")
	}
	out := Credential{Method: AuthMethod(encoded.Method), CAPEM: encoded.CAPEM, SPKISHA256: encoded.SPKISHA256}
	switch out.Method {
	case TokenAuth:
		if encoded.Token == "" || encoded.RoleID != "" || encoded.SecretID != "" || encoded.Mount != "" {
			return Credential{}, errors.New("vault-kv: token credential takes only token and trust fields")
		}
		if strings.ContainsAny(encoded.Token, " \t\r\n") {
			return Credential{}, errors.New("vault-kv: token must not contain whitespace")
		}
		out.Token = encoded.Token
	case AppRoleAuth:
		if encoded.Token != "" || encoded.RoleID == "" || encoded.SecretID == "" {
			return Credential{}, errors.New("vault-kv: approle credential requires role_id and secret_id and takes no token")
		}
		out.RoleID, out.SecretID = encoded.RoleID, encoded.SecretID
		out.Mount = encoded.Mount
		if out.Mount == "" {
			out.Mount = "approle"
		}
		if !authMountSyntax.MatchString(out.Mount) {
			return Credential{}, errors.New("vault-kv: approle mount must be a plain auth mount path")
		}
	default:
		return Credential{}, errors.New("vault-kv: credential method must be token or approle")
	}
	if out.CAPEM != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(out.CAPEM)) {
			return Credential{}, errors.New("vault-kv: ca_pem holds no PEM certificate")
		}
		out.rootsPool = pool
	}
	if out.SPKISHA256 != "" {
		pin, err := base64.StdEncoding.DecodeString(out.SPKISHA256)
		if err != nil || len(pin) != sha256.Size {
			return Credential{}, errors.New("vault-kv: spki_sha256 must be base64(sha256(SubjectPublicKeyInfo))")
		}
		out.pinnedSPKI = pin
	}
	return out, nil
}

// tlsConfig returns the connection trust. With no trust material the system
// roots verify the chain and hostname. A CA bundle replaces the system roots
// with the operator's CA, still verifying hostname. A SPKI pin additionally
// requires the leaf's public key to match; like the CLI trust store it is
// checked against the LEAF only so a chain cannot smuggle the pinned key in as
// an intermediate.
func (c Credential) tlsConfig() *tls.Config {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.rootsPool != nil {
		config.RootCAs = c.rootsPool
	}
	if len(c.pinnedSPKI) != 0 {
		pin := append([]byte(nil), c.pinnedSPKI...)
		config.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("vault-kv: server presented no certificate")
			}
			sum := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
			if string(sum[:]) != string(pin) {
				return fmt.Errorf("vault-kv: server certificate does not match the pinned SPKI")
			}
			return nil
		}
	}
	return config
}

// forget drops every secret-bearing field. Parsed trust stays usable because
// it is not secret.
func (c *Credential) forget() {
	c.Token, c.RoleID, c.SecretID = "", "", ""
}
