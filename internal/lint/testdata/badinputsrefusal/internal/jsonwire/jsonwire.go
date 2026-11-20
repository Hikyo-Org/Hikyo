package jsonwire

import "encoding/json/v2"

var Stable = json.JoinOptions(json.Deterministic(true), json.FormatNilSliceAsNull(true))
