package config

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"time"
)

// Config and SealedWebhookTarget carry time.Duration fields. encoding/json (v1)
// encoded a Duration as its nanosecond count; v2 has no default and fails at
// runtime. These methods keep the v1 integers without changing the field types
// or their order, and keep the encoder's own options, so a caller using
// jsonwire.Stable still gets the v1 bytes for everything else.
var (
	nanosecondMarshalers = json.WithMarshalers(json.MarshalToFunc(func(enc *jsontext.Encoder, d time.Duration) error {
		return enc.WriteToken(jsontext.Int(int64(d)))
	}))
	nanosecondUnmarshalers = json.WithUnmarshalers(json.UnmarshalFromFunc(func(dec *jsontext.Decoder, d *time.Duration) error {
		var nanoseconds int64
		if err := json.UnmarshalDecode(dec, &nanoseconds); err != nil {
			return err
		}
		*d = time.Duration(nanoseconds)
		return nil
	}))
)

// MarshalJSONTo implements json.MarshalerTo.
func (c Config) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain Config
	return json.MarshalEncode(enc, plain(c), nanosecondMarshalers)
}

// UnmarshalJSONFrom implements json.UnmarshalerFrom.
func (c *Config) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type plain Config
	return json.UnmarshalDecode(dec, (*plain)(c), nanosecondUnmarshalers)
}

// MarshalJSONTo implements json.MarshalerTo.
func (t SealedWebhookTarget) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain SealedWebhookTarget
	return json.MarshalEncode(enc, plain(t), nanosecondMarshalers)
}

// UnmarshalJSONFrom implements json.UnmarshalerFrom.
func (t *SealedWebhookTarget) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type plain SealedWebhookTarget
	return json.UnmarshalDecode(dec, (*plain)(t), nanosecondUnmarshalers)
}
