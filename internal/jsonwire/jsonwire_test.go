package jsonwire

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"
)

func TestStableMatchesV1Bytes(t *testing.T) {
	type doc struct {
		Nil   []string       `json:"nil"`
		Empty []string       `json:"empty"`
		Map   map[string]any `json:"map"`
		None  map[string]any `json:"none"`
	}
	in := doc{Empty: []string{}, Map: map[string]any{"z": "<&>\u2028\u2029", "a": 1, "é": []int(nil)}}
	// The bytes encoding/json (v1) produced for in.
	want := `{"nil":null,"empty":[],"map":{"a":1,"z":"\u003c\u0026\u003e\u2028\u2029","é":null},"none":null}`
	got, err := json.Marshal(in, Stable)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("Marshal(Stable):\n got %s\nwant %s", got, want)
	}
	indented, err := json.Marshal([]any{map[string]any{}}, Stable, jsontext.WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if want := "[\n  {}\n]"; string(indented) != want {
		t.Fatalf("indented:\n got %q\nwant %q", indented, want)
	}
	raw, err := json.Marshal(map[string]jsontext.Value{"r": jsontext.Value(`{"k":"\u0041\/"}`)}, Stable)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"r":{"k":"\u0041\/"}}`; string(raw) != want {
		t.Fatalf("raw value:\n got %s\nwant %s", raw, want)
	}
	if _, err := json.Marshal("\xff", Stable); err == nil {
		t.Fatal("invalid UTF-8 must fail instead of being replaced")
	}
}
