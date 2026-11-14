// Package jsonwire holds the encoding/json/v2 options for output whose bytes
// are observable: signed or hashed payloads, persisted documents, golden
// fixtures and the HTTP, CLI and MCP contracts.
package jsonwire

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
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
