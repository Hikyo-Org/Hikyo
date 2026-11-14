package cli

import (
	"encoding/json/v2"
	"io"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// decodeStrictDocument decodes the one JSON document in r into out and refuses
// unknown members (duplicate names are always refused), so a typo in a
// security-relevant document fails where it was written. A value after the
// document yields jsonwire.ErrTrailing.
func decodeStrictDocument(r io.Reader, out any) error {
	return jsonwire.Decode(r, out, json.RejectUnknownMembers(true))
}
