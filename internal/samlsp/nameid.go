// Package samlsp owns Hikyo's relying-party policy around SAML parsing and
// identity material. Cryptographic verification remains the responsibility of
// the pinned SAML/XML-DSIG libraries; this package makes their policy inputs
// unambiguous and fail-closed.
package samlsp

import (
	"encoding/binary"
	"errors"
	"math"
)

var (
	ErrEmptyNameID           = errors.New("samlsp: NameID value is empty")
	ErrNameIDTooLarge        = errors.New("samlsp: NameID value is too large")
	ErrInvalidNameIDEncoding = errors.New("samlsp: invalid encoded NameID")
)

// NameID is the byte-exact identity material extracted from a verified SAML
// Assertion. Pointers preserve the difference between an absent XML attribute
// and one explicitly present with an empty value.
type NameID struct {
	Value           []byte
	Format          *string
	NameQualifier   *string
	SPNameQualifier *string
}

// EncodeNameID returns the injective subject encoding fixed by the SAML ADR.
// Each field is presence-byte || uint32-big-endian-length || bytes. Value is
// mandatory and therefore always present; optional attributes retain presence.
func EncodeNameID(id NameID) ([]byte, error) {
	if len(id.Value) == 0 {
		return nil, ErrEmptyNameID
	}
	if len(id.Value) > math.MaxInt-20 {
		return nil, ErrNameIDTooLarge
	}

	capHint := len(id.Value) + 20
	encoded := make([]byte, 0, capHint)
	encoded = appendPresentField(encoded, id.Value)
	encoded = appendOptionalField(encoded, id.Format)
	encoded = appendOptionalField(encoded, id.NameQualifier)
	encoded = appendOptionalField(encoded, id.SPNameQualifier)
	return encoded, nil
}

func appendOptionalField(dst []byte, value *string) []byte {
	if value == nil {
		return appendField(dst, false, nil)
	}
	return appendField(dst, true, []byte(*value))
}

func appendPresentField(dst, value []byte) []byte {
	return appendField(dst, true, value)
}

func appendField(dst []byte, present bool, value []byte) []byte {
	if uint64(len(value)) > math.MaxUint32 {
		panic("samlsp: NameID field exceeds uint32")
	}
	if present {
		dst = append(dst, 1)
	} else {
		dst = append(dst, 0)
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	dst = append(dst, length[:]...)
	return append(dst, value...)
}

// DecodeNameID reverses EncodeNameID without normalizing any field. Invalid
// presence bits, absent fields carrying bytes, truncation and trailing data
// fail closed, so only the existing injective representation is accepted.
func DecodeNameID(encoded []byte) (NameID, error) {
	field := func() ([]byte, bool, error) {
		if len(encoded) < 5 {
			return nil, false, ErrInvalidNameIDEncoding
		}
		present := encoded[0]
		length := uint64(binary.BigEndian.Uint32(encoded[1:5]))
		encoded = encoded[5:]
		if present > 1 || (present == 0 && length != 0) || length > uint64(len(encoded)) {
			return nil, false, ErrInvalidNameIDEncoding
		}
		value := encoded[:int(length)]
		encoded = encoded[int(length):]
		return value, present == 1, nil
	}
	value, present, err := field()
	if err != nil || !present || len(value) == 0 {
		return NameID{}, ErrInvalidNameIDEncoding
	}
	var optional [3]*string
	for index := range optional {
		bytes, present, err := field()
		if err != nil {
			return NameID{}, err
		}
		if present {
			text := string(bytes)
			optional[index] = &text
		}
	}
	if len(encoded) != 0 {
		return NameID{}, ErrInvalidNameIDEncoding
	}
	return NameID{Value: append([]byte(nil), value...), Format: optional[0], NameQualifier: optional[1], SPNameQualifier: optional[2]}, nil
}
