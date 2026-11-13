package configrollout

import (
	"encoding/json/v2"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	corev1 "k8s.io/api/core/v1"
)

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v, jsonwire.Stable)
	if err != nil {
		panic(err)
	}
	return raw
}

// A value v2 cannot encode must never digest equal to anything, itself included.
func TestDigestOfUnencodableValuesNeverMatches(t *testing.T) {
	bad := map[string]string{"HIKYO_X": "\xff"}
	if digest(bad) == digest(bad) {
		t.Fatal("unencodable values digest equal; a digest check would pass")
	}
	if digest(map[string]string{"A": "b"}) != digest(map[string]string{"A": "b"}) {
		t.Fatal("encodable values must digest deterministically")
	}
	if err := putRecord(&corev1.Secret{}, "k", bad); err == nil {
		t.Fatal("putRecord must refuse an unencodable record")
	}
}
