package api_test

import (
	"math"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/getkin/kin-openapi/openapi3"
)

func TestDynamicTTLRange(t *testing.T) {
	doc, err := api.Doc()
	if err != nil {
		t.Fatal(err)
	}
	const maximum = math.MaxInt64 / int64(time.Second)
	for _, name := range []string{"MintLeaseRequest", "RenewLeaseRequest"} {
		schema := doc.Components.Schemas[name].Value.Properties["max_ttl_seconds"].Value
		for _, seconds := range []int64{1, 3600, maximum, maximum + 1, math.MaxInt64, 0, -1} {
			err := schema.VisitJSON(float64(seconds), openapi3.EnableJSONSchema2020())
			valid := seconds >= 1 && seconds <= maximum
			if (err == nil) != valid {
				t.Errorf("%s TTL %d: valid=%v err=%v", name, seconds, valid, err)
			}
		}
	}
	for _, body := range []map[string]any{{}, {"max_ttl_seconds": nil}} {
		if err := doc.Components.Schemas["RenewLeaseRequest"].Value.VisitJSON(body, openapi3.EnableJSONSchema2020()); err != nil {
			t.Fatalf("omitted/null renewal rejected: %v", err)
		}
	}
}
