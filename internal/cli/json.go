package cli

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// errTrailingJSON reports a second value after the document a strict reader
// expected to be alone.
var errTrailingJSON = errors.New("trailing content after the JSON document")

// writeJSONLine writes v as one stable JSON document followed by the newline
// encoding/json's Encoder used to write: machine output is parsed by scripts,
// so its bytes stay what they were.
func writeJSONLine(w io.Writer, v any, opts ...jsontext.Options) error {
	return json.MarshalEncode(jsontext.NewEncoder(w, opts...), v, jsonwire.Stable)
}

// decodeDocument decodes the one JSON document in r into out. A value after
// the document yields errTrailingJSON, so a second document never passes as
// the first one's intent.
func decodeDocument(r io.Reader, out any, opts ...json.Options) error {
	dec := jsontext.NewDecoder(r)
	if err := json.UnmarshalDecode(dec, out, opts...); err != nil {
		return err
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		return errTrailingJSON
	}
	return nil
}

// decodeStrictDocument is decodeDocument that also refuses unknown members
// (duplicate names are always refused), so a typo in a security-relevant
// document fails where it was written.
func decodeStrictDocument(r io.Reader, out any) error {
	return decodeDocument(r, out, json.RejectUnknownMembers(true))
}
