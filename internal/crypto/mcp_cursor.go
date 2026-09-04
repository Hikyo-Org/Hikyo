package crypto

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// MCPCursorSealer encrypts and authenticates opaque MCP pagination cursors
// (#629) with XChaCha20-Poly1305 under an instance-wide derived key. Encryption
// is required because the keyset position can be a tenant-controlled name,
// which must not appear in the cursor. The crypto chokepoint owns the primitive;
// the transport holds only the narrow Seal/Open verbs.
type MCPCursorSealer struct {
	derive func(context.Context) ([]byte, error)
}

// ErrCursorInvalid is the generic, non-distinguishing failure the sealer returns
// for a tampered, truncated, or forged token.
var ErrCursorInvalid = errors.New("crypto: invalid cursor")

var mcpCursorEncoding = base64.RawURLEncoding

// Seal authenticates and encodes one cursor payload.
func (s *MCPCursorSealer) Seal(ctx context.Context, payload []byte) (string, error) {
	key, err := s.cursorKey(ctx)
	if err != nil {
		return "", err
	}
	defer Zero(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", fmt.Errorf("crypto: cursor cipher: %w", err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypto: cursor nonce: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, payload, nil)
	return mcpCursorEncoding.EncodeToString(sealed), nil
}

// Open verifies one cursor token and returns its payload. Every malformed or
// unauthenticated token returns ErrCursorInvalid without distinguishing which.
func (s *MCPCursorSealer) Open(ctx context.Context, token string) ([]byte, error) {
	sealed, err := mcpCursorEncoding.DecodeString(token)
	if err != nil || len(sealed) < chacha20poly1305.NonceSizeX+chacha20poly1305.Overhead {
		return nil, ErrCursorInvalid
	}
	key, err := s.cursorKey(ctx)
	if err != nil {
		return nil, ErrCursorInvalid
	}
	defer Zero(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, ErrCursorInvalid
	}
	nonce := sealed[:chacha20poly1305.NonceSizeX]
	payload, err := aead.Open(nil, nonce, sealed[chacha20poly1305.NonceSizeX:], nil)
	if err != nil {
		return nil, ErrCursorInvalid
	}
	return payload, nil
}

func (s *MCPCursorSealer) cursorKey(ctx context.Context) ([]byte, error) {
	if s == nil || s.derive == nil {
		return nil, errors.New("crypto: cursor sealer unavailable")
	}
	key, err := s.derive(ctx)
	if err != nil || len(key) != chacha20poly1305.KeySize {
		if key != nil {
			Zero(key)
		}
		return nil, errors.New("crypto: cursor key unavailable")
	}
	return key, nil
}
