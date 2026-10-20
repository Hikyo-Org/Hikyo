package remotefetch

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/pairing"
)

const pairedCredentialPrefix = "hikyo-paired-v1:"

type pairedCredential struct {
	Version    int    `json:"version"`
	Token      string `json:"token"`
	PrivateKey []byte `json:"private_key"`
}

// EncodePairedCredential creates a sealed-storage payload, never a wire bearer.
// Its reserved prefix makes malformed paired state an error instead of fallback.
func EncodePairedCredential(token string, key ed25519.PrivateKey) (string, error) {
	if token == "" || len(key) != ed25519.PrivateKeySize {
		return "", pairing.ErrInvalidProof
	}
	raw, err := json.Marshal(pairedCredential{Version: 1, Token: token, PrivateKey: key})
	if err != nil {
		return "", err
	}
	defer crypto.Zero(raw)
	return pairedCredentialPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}
func applyDirectoryCredential(req *http.Request, credential string) error {
	if !strings.HasPrefix(credential, "hikyo-paired-") {
		req.Header.Set("Authorization", "Bearer "+credential)
		return nil
	}
	if !strings.HasPrefix(credential, pairedCredentialPrefix) || len(credential) > 8192 {
		return pairing.ErrInvalidProof
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(credential, pairedCredentialPrefix))
	if err != nil {
		return pairing.ErrInvalidProof
	}
	defer crypto.Zero(raw)
	var stored pairedCredential
	defer func() { crypto.Zero(stored.PrivateKey) }()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&stored) != nil || decoder.Decode(new(any)) != io.EOF || stored.Version != 1 || stored.Token == "" || len(stored.PrivateKey) != ed25519.PrivateKeySize {
		return pairing.ErrInvalidProof
	}
	proof, err := pairing.GenerateDPoP(ed25519.PrivateKey(stored.PrivateKey), pairing.DPoPClaims{Method: http.MethodGet, URL: req.URL.String(), AccessToken: stored.Token, IssuedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "DPoP "+stored.Token)
	req.Header.Set("DPoP", proof)
	return nil
}

// PairingStart and PairingFinish are fixed authentication-protocol paths. They
// use exactly the directory client's pinned TLS, redirect and response bounds.
func (c *Client) PairingStart(ctx context.Context, origin, pin, capability, publicJWK string, out any) error {
	return c.pairingExchange(ctx, origin, pin, "/api/v1/auth/pairing/start", struct {
		InviteCapability string `json:"invite_capability"`
		PublicJWK        string `json:"public_jwk"`
	}{capability, publicJWK}, out)
}
func (c *Client) PairingFinish(ctx context.Context, origin, pin, exchange, message, proof string, out any) error {
	return c.pairingExchange(ctx, origin, pin, "/api/v1/auth/pairing/finish", struct {
		Exchange      string `json:"exchange"`
		ClientMessage string `json:"client_message"`
		Proof         string `json:"proof"`
	}{exchange, message, proof}, out)
}
func (c *Client) pairingExchange(ctx context.Context, origin, pin, path string, input, out any) error {
	canonical, err := CanonicalRemoteURL(origin)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if len(raw) > 32768 {
		return fmt.Errorf("remotefetch: pairing message exceeds request cap")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, canonical+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient(pin).Do(req)
	if err != nil {
		return fmt.Errorf("remotefetch: pairing transport failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("remotefetch: pairing refused with status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32769))
	if err != nil {
		return err
	}
	if len(body) > 32768 {
		return fmt.Errorf("remotefetch: pairing response exceeds cap")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("remotefetch: invalid pairing response")
	}
	if dec.Decode(new(any)) != io.EOF {
		return fmt.Errorf("remotefetch: trailing pairing response")
	}
	return nil
}
