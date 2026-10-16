package samlsp

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func TestEncodeNameIDIsInjectiveAcrossFieldsAndPresence(t *testing.T) {
	t.Parallel()

	empty := ""
	format := "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
	tests := []struct {
		name string
		id   NameID
	}{
		{name: "baseline", id: NameID{Value: []byte("alice")}},
		{name: "different bytes", id: NameID{Value: []byte("Alice")}},
		{name: "format", id: NameID{Value: []byte("alice"), Format: &format}},
		{name: "present empty format", id: NameID{Value: []byte("alice"), Format: &empty}},
		{name: "present empty name qualifier", id: NameID{Value: []byte("alice"), NameQualifier: &empty}},
		{name: "present empty sp name qualifier", id: NameID{Value: []byte("alice"), SPNameQualifier: &empty}},
	}

	encoded := make(map[string]string, len(tests))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeNameID(tt.id)
			if err != nil {
				t.Fatalf("EncodeNameID() error = %v", err)
			}
			key := string(got)
			if previous, exists := encoded[key]; exists {
				t.Fatalf("encoding collides with %q: %x", previous, got)
			}
			encoded[key] = tt.name
		})
	}
}

func TestEncodeNameIDUsesSpecifiedWireEncoding(t *testing.T) {
	t.Parallel()

	empty := ""
	got, err := EncodeNameID(NameID{Value: []byte("A"), Format: &empty})
	if err != nil {
		t.Fatalf("EncodeNameID() error = %v", err)
	}
	want, err := hex.DecodeString("010000000141010000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeNameID() = %x, want %x", got, want)
	}
}

func TestEncodeNameIDRefusesEmptyValue(t *testing.T) {
	t.Parallel()

	_, err := EncodeNameID(NameID{})
	if !errors.Is(err, ErrEmptyNameID) {
		t.Fatalf("EncodeNameID() error = %v, want ErrEmptyNameID", err)
	}
}

func TestDecodeNameIDPreservesEncodedIdentity(t *testing.T) {
	t.Parallel()
	empty := ""
	email := "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"
	qualifier := "Exact Qualifier"
	for _, want := range []NameID{
		{Value: []byte("CaseSensitive")},
		{Value: []byte{0, 0xff, 1}, Format: &empty, NameQualifier: &qualifier, SPNameQualifier: &empty},
		{Value: []byte("Email@Example.test"), Format: &email},
	} {
		encoded, err := EncodeNameID(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeNameID(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("decoded identity=%#v want=%#v", got, want)
		}
		roundtrip, err := EncodeNameID(got)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(roundtrip, encoded) {
			t.Fatal("identity encoding changed after roundtrip")
		}
	}
}

func TestDecodeNameIDRejectsMalformedEncoding(t *testing.T) {
	t.Parallel()
	valid, err := EncodeNameID(NameID{Value: []byte("alice")})
	if err != nil {
		t.Fatal(err)
	}
	malformed := map[string][]byte{
		"empty":                            {},
		"absent mandatory value":           append([]byte{0, 0, 0, 0, 0}, valid[10:]...),
		"empty mandatory value":            append([]byte{1, 0, 0, 0, 0}, valid[10:]...),
		"invalid presence":                 append([]byte{2}, valid[1:]...),
		"overflowed length":                append([]byte{1, 0xff, 0xff, 0xff, 0xff}, valid[5:]...),
		"absent optional value with bytes": append(append([]byte{}, valid[:10]...), append([]byte{0, 0, 0, 0, 1, 'x'}, valid[15:]...)...),
		"trailing bytes":                   append(append([]byte{}, valid...), 0),
	}
	for length := 1; length < len(valid); length++ {
		if _, err := DecodeNameID(valid[:length]); !errors.Is(err, ErrInvalidNameIDEncoding) {
			t.Errorf("truncation %d accepted: %v", length, err)
		}
	}
	for name, encoded := range malformed {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeNameID(encoded); !errors.Is(err, ErrInvalidNameIDEncoding) {
				t.Fatalf("malformed identity accepted: %v", err)
			}
		})
	}
}
