// Package jsonwire holds the encoding/json/v2 options for output whose bytes
// are observable: signed or hashed payloads, persisted documents, golden
// fixtures and the HTTP, CLI and MCP contracts.
package jsonwire

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
)

// Stable makes json.Marshal produce the bytes encoding/json (v1) produced:
// sorted map keys, null for nil slices and maps, HTML and JavaScript
// escaping, and string escapes inside embedded raw values kept as written.
// Struct tags still decide omitempty, durations and byte arrays; see the v1
// differences in the encoding/json package documentation.
// Unlike v1, invalid UTF-8 is an error instead of being replaced.
var Stable = json.JoinOptions(
	json.Deterministic(true),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
	jsontext.EscapeForHTML(true),
	jsontext.EscapeForJS(true),
	jsontext.PreserveRawStrings(true),
)

// Encode writes v as one document, marshaled with Stable and then opts, plus a
// trailing newline, in a single write: exactly the bytes encoding/json (v1)
// Encoder.Encode wrote. Nothing is written when v cannot be encoded.
func Encode(w io.Writer, v any, opts ...json.Options) error {
	b, err := json.Marshal(v, append([]json.Options{Stable}, opts...)...)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// ErrTrailing reports a second value after the document Decode expected to be
// alone.
var ErrTrailing = errors.New("trailing content after the JSON document")

// Decode decodes the one JSON document in r into out. Unlike json.UnmarshalRead
// it keeps a malformed document (the decoder's error) apart from a value after
// it (ErrTrailing), for callers that word the two refusals differently. Decode
// reads r only as far as the document and one token after it, so do not use it
// on a stream that stays open.
func Decode(r io.Reader, out any, opts ...json.Options) error {
	dec := jsontext.NewDecoder(r)
	if err := json.UnmarshalDecode(dec, out, opts...); err != nil {
		return err
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		return ErrTrailing
	}
	return nil
}
