package cli_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// writeJSONLine writes v the way the Hikyo server does: stable bytes plus the
// trailing newline encoding/json's Encoder wrote, so fake servers stay
// byte-identical to the real one.
func writeJSONLine(w io.Writer, v any) error {
	return json.MarshalEncode(jsontext.NewEncoder(w), v, jsonwire.Stable)
}
