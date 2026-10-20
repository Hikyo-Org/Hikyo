package pairing

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

const (
	MaxProofAge   = time.Minute
	MaxFutureSkew = 5 * time.Second
	MaxProofBytes = 8192
)

var ErrInvalidProof = errors.New("pairing: invalid proof of possession")

type DPoPClaims struct {
	Method, URL, AccessToken, Nonce, JTI string
	IssuedAt                             time.Time
}

type DPoPExpectation struct {
	Method, URL, AccessToken, Nonce, Thumbprint string
	Now                                         time.Time
}

type VerifiedDPoP struct {
	Thumbprint, JTI string
	IssuedAt        time.Time
}

type wireProofClaims struct {
	JTI             string `json:"jti"`
	Method          string `json:"htm"`
	URL             string `json:"htu"`
	IssuedAt        int64  `json:"iat"`
	AccessTokenHash string `json:"ath,omitempty"`
	Nonce           string `json:"nonce,omitempty"`
}

func PublicKeyThumbprint(key jose.JSONWebKey) (string, error) {
	public, ok := key.Key.(ed25519.PublicKey)
	if !ok || len(public) != ed25519.PublicKeySize {
		return "", ErrInvalidProof
	}
	thumbprint, err := key.Thumbprint(crypto.SHA256)
	if err != nil {
		return "", ErrInvalidProof
	}
	return base64.RawURLEncoding.EncodeToString(thumbprint), nil
}

func GenerateDPoP(private ed25519.PrivateKey, claims DPoPClaims) (string, error) {
	public, err := PublicJWK(private)
	if err != nil {
		return "", err
	}
	target, err := proofTarget(claims.URL)
	if err != nil || claims.Method == "" || claims.IssuedAt.IsZero() {
		return "", ErrInvalidProof
	}
	if claims.JTI == "" {
		value := make([]byte, 24)
		if _, err := rand.Read(value); err != nil {
			return "", err
		}
		claims.JTI = base64.RawURLEncoding.EncodeToString(value)
	}
	if len(claims.JTI) > 128 || len(claims.Nonce) > 256 {
		return "", ErrInvalidProof
	}
	wire := wireProofClaims{JTI: claims.JTI, Method: claims.Method, URL: target,
		IssuedAt: claims.IssuedAt.Unix(), Nonce: claims.Nonce}
	if claims.AccessToken != "" {
		wire.AccessTokenHash = accessTokenHash(claims.AccessToken)
	}
	payload, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	opts := (&jose.SignerOptions{}).WithType("dpop+jwt").WithHeader("jwk", public)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: private}, opts)
	if err != nil {
		return "", err
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return signed.CompactSerialize()
}

// VerifyDPoP uses go-jose for JWS verification and RFC 7638 thumbprints. The
// application checks here implement RFC 9449 section 4.3. The caller must reject
// duplicate HTTP DPoP headers and atomically consume (Thumbprint,JTI) before
// granting authority; this stateless verifier alone does not prevent replay.
// https://www.rfc-editor.org/rfc/rfc9449.html#section-4.3
func VerifyDPoP(raw string, expected DPoPExpectation) (VerifiedDPoP, error) {
	fail := func() (VerifiedDPoP, error) { return VerifiedDPoP{}, ErrInvalidProof }
	if len(raw) == 0 || len(raw) > MaxProofBytes || strings.ContainsAny(raw, " \t\r\n") || expected.Now.IsZero() || expected.Method == "" {
		return fail()
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return fail()
	}
	headerBytes, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil || len(headerBytes) > 2048 {
		return fail()
	}
	header, err := strictObject(headerBytes, "typ", "alg", "jwk")
	if err != nil || !jsonStringEquals(header["typ"], "dpop+jwt") || !jsonStringEquals(header["alg"], "EdDSA") {
		return fail()
	}
	key, err := ParsePublicJWK(header["jwk"])
	if err != nil {
		return fail()
	}
	thumbprint, err := PublicKeyThumbprint(key)
	if err != nil || expected.Thumbprint != "" && !equal(thumbprint, expected.Thumbprint) {
		return fail()
	}
	signed, err := jose.ParseSignedCompact(raw, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil || len(signed.Signatures) != 1 {
		return fail()
	}
	payload, err := signed.Verify(key.Key)
	if err != nil {
		return fail()
	}
	fields, err := strictObject(payload, "jti", "htm", "htu", "iat", "ath", "nonce")
	if err != nil || fields["iat"] == nil || bytes.Equal(fields["iat"], []byte("null")) {
		return fail()
	}
	for _, name := range []string{"jti", "htm", "htu", "ath", "nonce"} {
		if raw, present := fields[name]; present {
			var value string
			if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil {
				return fail()
			}
		}
	}
	var claims wireProofClaims
	if json.Unmarshal(payload, &claims) != nil || claims.JTI == "" || len(claims.JTI) > 128 || len(claims.Nonce) > 256 {
		return fail()
	}
	target, err := proofTarget(expected.URL)
	if err != nil || claims.URL != target || claims.Method != expected.Method || !equal(claims.Nonce, expected.Nonce) {
		return fail()
	}
	issuedAt := time.Unix(claims.IssuedAt, 0)
	if issuedAt.After(expected.Now.Add(MaxFutureSkew)) || issuedAt.Before(expected.Now.Add(-MaxProofAge)) {
		return fail()
	}
	if expected.AccessToken != "" {
		if !equal(claims.AccessTokenHash, accessTokenHash(expected.AccessToken)) {
			return fail()
		}
	} else if claims.AccessTokenHash != "" {
		return fail()
	}
	return VerifiedDPoP{Thumbprint: thumbprint, JTI: claims.JTI, IssuedAt: issuedAt}, nil
}

func ParsePublicJWK(raw []byte) (jose.JSONWebKey, error) {
	if len(raw) == 0 || len(raw) > 2048 {
		return jose.JSONWebKey{}, ErrInvalidProof
	}
	fields, err := strictObject(raw, "kty", "crv", "x", "alg", "use")
	if err != nil || !jsonStringEquals(fields["kty"], "OKP") || !jsonStringEquals(fields["crv"], "Ed25519") {
		return jose.JSONWebKey{}, ErrInvalidProof
	}
	if fields["alg"] != nil && !jsonStringEquals(fields["alg"], "EdDSA") || fields["use"] != nil && !jsonStringEquals(fields["use"], "sig") {
		return jose.JSONWebKey{}, ErrInvalidProof
	}
	var x string
	if json.Unmarshal(fields["x"], &x) != nil {
		return jose.JSONWebKey{}, ErrInvalidProof
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(x)
	if err != nil || len(decoded) != ed25519.PublicKeySize || base64.RawURLEncoding.EncodeToString(decoded) != x {
		return jose.JSONWebKey{}, ErrInvalidProof
	}
	var key jose.JSONWebKey
	if json.Unmarshal(raw, &key) != nil || !key.IsPublic() || !key.Valid() {
		return jose.JSONWebKey{}, ErrInvalidProof
	}
	if _, err := PublicKeyThumbprint(key); err != nil {
		return jose.JSONWebKey{}, err
	}
	return key, nil
}

func accessTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func proofTarget(raw string) (string, error) {
	if len(raw) > 2048 {
		return "", ErrInvalidProof
	}
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "https" || target.Host == "" || target.User != nil || target.Opaque != "" {
		return "", ErrInvalidProof
	}
	target.RawQuery, target.Fragment, target.RawFragment = "", "", ""
	return target.String(), nil
}

func jsonStringEquals(raw json.RawMessage, expected string) bool {
	var value string
	return json.Unmarshal(raw, &value) == nil && value == expected
}

// strictObject refuses duplicate and unknown members before either JOSE or the
// typed decoder interprets the object, preventing parser disagreement.
func strictObject(raw []byte, allowed ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	open, err := decoder.Token()
	if err != nil || open != json.Delim('{') {
		return nil, ErrInvalidProof
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalidProof
		}
		name, ok := nameToken.(string)
		if !ok {
			return nil, ErrInvalidProof
		}
		found := false
		for _, candidate := range allowed {
			found = found || name == candidate
		}
		if _, duplicate := fields[name]; !found || duplicate {
			return nil, ErrInvalidProof
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, ErrInvalidProof
		}
		fields[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, ErrInvalidProof
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidProof
	}
	return fields, nil
}
