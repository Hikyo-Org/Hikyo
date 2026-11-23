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
	first, second := digest(bad), digest(bad)
	if first == second {
		t.Fatal("unencodable values digest equal; a digest check would pass")
	}
	good := map[string]string{"A": "b"}
	first, second = digest(good), digest(good)
	if first != second {
		t.Fatal("encodable values must digest deterministically")
	}
	if err := putRecord(&corev1.Secret{}, "k", bad); err == nil {
		t.Fatal("putRecord must refuse an unencodable record")
	}
}
