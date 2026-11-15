package main

import (
	"bytes"
	"os"
	"testing"
)

// The CI "generated code is fresh" step runs only oapi-codegen, so a stale
// overlay would silently leave new optional fields on omitempty.
func TestCommittedOverlayMatchesSpec(t *testing.T) {
	spec, err := os.ReadFile("../openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want, err := generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../omitzero-overlay.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("api/omitzero-overlay.yaml is stale; regenerate with `go run ./api/omitzero`, " +
			"then `go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml`")
	}
}

func TestGenerateSelectsOptionalPropertiesOnly(t *testing.T) {
	spec := `
components:
  schemas:
    A:
      type: object
      required: [req, ro]
      properties:
        req: {type: string}
        opt: {type: string}
        ro: {type: string, readOnly: true}
        it's: {type: string}
        refd: {$ref: '#/components/schemas/B'}
        properties:
          type: object
          properties: {inner: {type: string}}
    B: {type: string, writeOnly: true}
  parameters:
    q: {name: q, in: query, schema: {type: string}}
    p: {name: p, in: path, required: true, schema: {type: string}}
    r: {name: r, in: query, required: true, schema: {type: string}}
`
	out, err := generate([]byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"$['components']['schemas']['A']['properties']['opt','ro','it\\'s','refd','properties']",
		"$['components']['schemas']['A']['properties']['properties']['properties']['inner']",
		"$['components']['parameters']['q']",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("overlay lacks target %s:\n%s", want, out)
		}
	}
	for _, bad := range []string{"'req'", "['p']", "['r']"} {
		if bytes.Contains(out, []byte(bad)) {
			t.Errorf("overlay must not select %s:\n%s", bad, out)
		}
	}
}
