package badjsonv1

import (
	"encoding/json" // want "jsonv1: bad.go imports encoding/json; use encoding/json/v2"
)

var _ = json.Marshal
