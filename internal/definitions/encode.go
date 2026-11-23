package definitions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// Encode renders a validated canonical bundle: sorted object keys at every
// depth (including any JSON Schema document embedded in a declaration),
// two-space indentation, HTML escaping off, and exactly one trailing newline.
// Byte-stability is what makes the digest a meaningful apply pin and a PR diff
// legible.
//
// Parse and Canonicalize are the only producers of an encodable bundle, so
// every successful result parses back to the same canonical model.
func Encode(b CanonicalBundle) ([]byte, error) {
	if !b.valid {
		return nil, invalidDetail("canonical bundle was not produced by Parse or Canonicalize")
	}
	return bytes.Clone(b.encoded), nil
}

// canonicalize serializes any value with sorted keys and stable integers. The
// round-trip through a generic tree sorts every object member (the encoder
// sorts map keys) while keeping each number as its literal digits, which a
// float64 round-trip would not preserve for int64.
func canonicalize(v any) ([]byte, error) {
	raw, err := marshalNoEscape(v)
	if err != nil {
		return nil, err
	}
	generic, err := DecodeLiteralTree(jsontext.NewDecoder(bytes.NewReader(raw)))
	if err != nil {
		return nil, fmt.Errorf("definitions: canonicalize decode: %w", err)
	}
	indented, err := json.Marshal(generic, jsonwire.Stable, jsontext.EscapeForHTML(false), jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("definitions: canonicalize indent: %w", err)
	}
	return append(indented, '\n'), nil
}

// marshalNoEscape marshals without HTML escaping and without a trailing
// newline, so `<`, `>` and `&` in a pattern or description survive verbatim.
func marshalNoEscape(v any) ([]byte, error) {
	raw, err := json.Marshal(v, jsonwire.Stable, jsontext.EscapeForHTML(false))
	if err != nil {
		return nil, fmt.Errorf("definitions: marshal: %w", err)
	}
	return raw, nil
}

// Digest is the SHA-256 of the canonical encoding, hex-encoded and unprefixed.
// It is computed over canonical bytes, never the raw file bytes, so a
// whitespace-only edit does not move the apply pin while any content change
// does. (This deliberately differs from internal/importer.Digest, whose
// "sha256:"-prefixed form is a template-reference spelling.)
func Digest(b CanonicalBundle) (string, error) {
	canonical, err := Encode(b)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
