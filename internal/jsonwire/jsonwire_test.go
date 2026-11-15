package jsonwire

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strings"
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

func TestEncodeMatchesV1Encoder(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, map[string]any{"b": []string(nil), "a": "<&>"}); err != nil {
		t.Fatal(err)
	}
	// The bytes encoding/json (v1) Encoder.Encode wrote.
	if want := "{\"a\":\"\\u003c\\u0026\\u003e\",\"b\":null}\n"; buf.String() != want {
		t.Fatalf("Encode:\n got %q\nwant %q", buf.String(), want)
	}
	buf.Reset()
	if err := Encode(&buf, []int{1}, jsontext.WithIndent("  ")); err != nil {
		t.Fatal(err)
	}
	if want := "[\n  1\n]\n"; buf.String() != want {
		t.Fatalf("indented Encode: got %q want %q", buf.String(), want)
	}
	buf.Reset()
	if err := Encode(&buf, "\xff"); err == nil || buf.Len() != 0 {
		t.Fatalf("invalid UTF-8 must fail without writing: err=%v wrote %q", err, buf.String())
	}
}

func TestDecodeKeepsMalformedApartFromTrailing(t *testing.T) {
	var v struct{ A int }
	if err := Decode(strings.NewReader(` {"A":1} `), &v); err != nil || v.A != 1 {
		t.Fatalf("one document: err=%v v=%+v", err, v)
	}
	if err := Decode(strings.NewReader(`{"A":1}{"A":2}`), &v); !errors.Is(err, ErrTrailing) {
		t.Fatalf("second document: got %v, want ErrTrailing", err)
	}
	if err := Decode(strings.NewReader(`{"A":`), &v); err == nil || errors.Is(err, ErrTrailing) {
		t.Fatalf("truncated document: got %v, want a decoder error", err)
	}
	if err := Decode(strings.NewReader(`{"B":1}`), &v, json.RejectUnknownMembers(true)); err == nil {
		t.Fatal("options must reach the decoder")
	}
}
