package shell

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
)

// decodeStrict parses one JSON value into v. It refuses unknown members,
// duplicate member names (the default of encoding/json/v2), invalid UTF-8 and
// trailing data, so a request the shell parses cannot mean something else to
// the instance.
func decodeStrict(data []byte, v any) error {
	return json.Unmarshal(data, v, json.RejectUnknownMembers(true))
}

// readLimited reads at most limit bytes and errors when the body is larger.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

var errTooLarge = errors.New("body too large")

// object parses a JSON object into its members without interpreting them.
// Duplicate names are an error.
func object(data []byte) (map[string]jsontext.Value, error) {
	var m map[string]jsontext.Value
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("not an object")
	}
	return m, nil
}

// stringMember returns a member that must be a JSON string, "" when absent.
func stringMember(m map[string]jsontext.Value, key string) (string, bool) {
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func isNull(raw jsontext.Value) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }
