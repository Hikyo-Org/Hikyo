package isolation

import (
	"encoding/json/v2"
	"io"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// encodeLine writes v and a trailing newline, byte for byte what
// encodeLine(w, v) wrote under encoding/json (v1). Test peers use
// it where another package parses their output.
func encodeLine(w io.Writer, v any) error {
	raw, err := json.Marshal(v, jsonwire.Stable)
	if err != nil {
		return err
	}
	_, err = w.Write(append(raw, '\n'))
	return err
}
