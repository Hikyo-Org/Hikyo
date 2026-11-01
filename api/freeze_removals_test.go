package api_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
)

// Exercise the public gate, including resolved and recursive schemas. A sunset
// is lifecycle metadata, never permission to remove part of the frozen API.
func TestFreezeRejectsPropertyRemovalsRegardlessOfSunset(t *testing.T) {
	for _, context := range []string{"request", "response", "parameter", "parameter-content", "response-header"} {
		for _, metadata := range []struct{ name, json string }{
			{"ordinary", ""},
			{"deprecated", `,"deprecated":true`},
			{"past-sunset", `,"deprecated":true,"x-sunset":"2000-01-01"`},
			{"future-sunset", `,"deprecated":true,"x-sunset":"2999-01-01"`},
			{"invalid-sunset", `,"deprecated":true,"x-sunset":"invalid"`},
		} {
			for _, shape := range []string{"inline", "nested", "array", "allOf", "anyOf", "oneOf", "reference", "recursive-reference"} {
				t.Run(context+"/"+metadata.name+"/"+shape, func(t *testing.T) {
					required := ""
					if context == "response" {
						required = `,"required":["kept","removed"]`
					}
					base := fmt.Sprintf(`{"type":"object"%s,"properties":{"kept":{"type":"string"},"removed":{"type":"string"%s}}}`, required, metadata.json)
					revised := `{"type":"object","properties":{"kept":{"type":"string"}}}`
					if context == "response" {
						revised = `{"type":"object","required":["kept"],"properties":{"kept":{"type":"string"}}}`
					}
					violations, err := api.CheckFreeze(freezeSchemaDocument(context, shape, base), freezeSchemaDocument(context, shape, revised))
					if err != nil {
						t.Fatal(err)
					}
					if !slices.ContainsFunc(violations, func(v api.Violation) bool {
						return v.ID == "hikyo-property-removed" && v.Path == "/api/v1/thing" && v.Operation == "POST" &&
							v.Level == "ERR" && strings.Contains(v.Text, "removed")
					}) {
						t.Fatalf("missing structural property-removal violation: %v", violations)
					}
				})
			}
		}
	}
}

func freezeSchemaDocument(context, shape, schema string) []byte {
	components := ""
	switch shape {
	case "nested":
		schema = fmt.Sprintf(`{"type":"object","properties":{"nested":%s}}`, schema)
	case "array":
		schema = fmt.Sprintf(`{"type":"array","items":%s}`, schema)
	case "allOf", "anyOf", "oneOf":
		schema = fmt.Sprintf(`{"%s":[%s]}`, shape, schema)
	case "reference", "recursive-reference":
		if shape == "recursive-reference" {
			schema = fmt.Sprintf(`{"type":"object","properties":{"next":{"$ref":"#/components/schemas/Thing"},"value":%s}}`, schema)
		}
		components = fmt.Sprintf(`,"components":{"schemas":{"Thing":%s}}`, schema)
		schema = `{"$ref":"#/components/schemas/Thing"}`
	}
	operation := fmt.Sprintf(`"requestBody":{"content":{"application/json":{"schema":%s}}},"responses":{"200":{"description":"ok"}}`, schema)
	if context == "response" {
		operation = fmt.Sprintf(`"responses":{"200":{"description":"ok","content":{"application/json":{"schema":%s}}}}`, schema)
	}
	if context == "parameter" || context == "parameter-content" {
		parameterSchema := fmt.Sprintf(`"schema":%s`, schema)
		if context == "parameter-content" {
			parameterSchema = fmt.Sprintf(`"content":{"application/json":{"schema":%s}}`, schema)
		}
		operation = fmt.Sprintf(`"parameters":[{"name":"filter","in":"query",%s}],"responses":{"200":{"description":"ok"}}`, parameterSchema)
	}
	if context == "response-header" {
		operation = fmt.Sprintf(`"responses":{"200":{"description":"ok","headers":{"X-Fixture":{"schema":%s}}}}`, schema)
	}
	return []byte(fmt.Sprintf(`{"openapi":"3.1.0","jsonSchemaDialect":"https://spec.openapis.org/oas/3.1/dialect/base","info":{"title":"fixture","version":"1"},"paths":{"/api/v1/thing":{"post":{%s}}}%s}`, operation, components))
}

func TestFreezeRejectsPastSunsetEndpointAndParameterRemovals(t *testing.T) {
	const get = `"get":{"deprecated":true,"x-sunset":"2000-01-01","responses":{"200":{"description":"ok"}}}`
	const post = `"post":{"responses":{"200":{"description":"ok"}}}`
	const parameter = `{"in":"query","name":"legacy","deprecated":true,"x-sunset":"2000-01-01","schema":{"type":"string"}}`
	const parameterGet = `"get":{"deprecated":true,"x-sunset":"2000-01-01","parameters":[` + parameter + `],"responses":{"200":{"description":"ok"}}}`
	for _, tc := range []struct{ name, base, revised string }{
		{"path", `{"/api/v1/thing":{` + get + `}}`, `{}`},
		{"operation", `{"/api/v1/thing":{` + get + `,` + post + `}}`, `{"/api/v1/thing":{` + post + `}}`},
		{"operation-parameter", `{"/api/v1/thing":{` + parameterGet + `}}`, `{"/api/v1/thing":{` + get + `}}`},
		{"inherited-parameter", `{"/api/v1/thing":{"parameters":[` + parameter + `],` + get + `}}`, `{"/api/v1/thing":{` + get + `}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document := func(paths string) []byte {
				return []byte(fmt.Sprintf(`{"openapi":"3.1.0","jsonSchemaDialect":"https://spec.openapis.org/oas/3.1/dialect/base","info":{"title":"fixture","version":"1"},"paths":%s}`, paths))
			}
			violations, err := api.CheckFreeze(document(tc.base), document(tc.revised))
			if err != nil {
				t.Fatal(err)
			}
			wantID := "hikyo-endpoint-removed"
			if strings.Contains(tc.name, "parameter") {
				wantID = "hikyo-parameter-removed"
			}
			if !slices.ContainsFunc(violations, func(v api.Violation) bool {
				return v.ID == wantID && v.Path == "/api/v1/thing" && v.Level == "ERR"
			}) {
				t.Fatalf("missing structural removal violation %q: %v", wantID, violations)
			}
		})
	}
}

func TestFreezeRemovalDiagnosticsAreStable(t *testing.T) {
	base := freezeSchemaDocument("request", "inline", `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}}}`)
	revised := freezeSchemaDocument("request", "inline", `{"type":"object"}`)
	want, err := api.CheckFreeze(base, revised)
	if err != nil || len(want) == 0 {
		t.Fatalf("initial removal check: %v, %v", want, err)
	}
	for range 10 {
		got, err := api.CheckFreeze(base, revised)
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("unstable diagnostics: got %v, %v; want %v", got, err, want)
		}
	}
}

func TestFreezeStillPermitsPropertyAdditions(t *testing.T) {
	for _, context := range []string{"request", "response"} {
		for _, shape := range []string{"inline", "nested", "array", "allOf", "anyOf", "oneOf", "reference", "recursive-reference"} {
			t.Run(context+"/"+shape, func(t *testing.T) {
				base := freezeSchemaDocument(context, shape, `{"type":"object","properties":{"kept":{"type":"string"}}}`)
				revised := freezeSchemaDocument(context, shape, `{"type":"object","properties":{"kept":{"type":"string"},"added":{"type":"string"}}}`)
				violations, err := api.CheckFreeze(base, revised)
				if err != nil || len(violations) != 0 {
					t.Fatalf("a permitted property addition was refused: %v, %v", violations, err)
				}
				violations, err = api.CheckFreeze(revised, revised)
				if err != nil || len(violations) != 0 {
					t.Fatalf("an identical document was refused: %v, %v", violations, err)
				}
			})
		}
	}
}

func TestFreezeRejectsPastSunsetCallbackPropertyRemoval(t *testing.T) {
	document := func(properties string) []byte {
		return []byte(fmt.Sprintf(`{
			"openapi":"3.1.0",
			"jsonSchemaDialect":"https://spec.openapis.org/oas/3.1/dialect/base",
			"info":{"title":"fixture","version":"1"},
			"paths":{"/api/v1/thing":{"post":{
				"responses":{"200":{"description":"ok"}},
				"callbacks":{"notice":{"{$request.query.url}":{"post":{
					"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{%s}}}}},
					"responses":{"200":{"description":"ok"}}
				}}}}
			}}}
		}`, properties))
	}
	base := document(`"kept":{"type":"string"},"removed":{"type":"string","deprecated":true,"x-sunset":"2000-01-01"}`)
	revised := document(`"kept":{"type":"string"}`)
	violations, err := api.CheckFreeze(base, revised)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(violations, func(v api.Violation) bool {
		return v.ID == "hikyo-property-removed" && v.Path == "{$request.query.url}"
	}) {
		t.Fatalf("missing callback property-removal violation: %v", violations)
	}
}

func TestFreezeComparesEffectiveParameters(t *testing.T) {
	for _, callback := range []bool{false, true} {
		for _, tc := range []struct {
			name                    string
			basePath, baseOperation bool
			newPath, newOperation   bool
			extraOperation          bool
			wantPass                bool
		}{
			{"move-to-operation", true, false, false, true, false, true},
			{"move-to-path", false, true, true, false, false, true},
			{"remove-shadowed-declaration", true, true, false, true, false, true},
			{"remove-inherited-from-sibling", true, true, false, true, true, false},
		} {
			t.Run(fmt.Sprintf("%s/callback=%t", tc.name, callback), func(t *testing.T) {
				base := freezeParameterDocument(tc.basePath, tc.baseOperation, tc.extraOperation, callback)
				revised := freezeParameterDocument(tc.newPath, tc.newOperation, tc.extraOperation, callback)
				violations, err := api.CheckFreeze(base, revised)
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantPass {
					if len(violations) != 0 {
						t.Fatalf("unchanged effective parameters were refused: %v", violations)
					}
				} else if !slices.ContainsFunc(violations, func(v api.Violation) bool {
					return v.ID == "hikyo-parameter-removed" && v.Operation == "POST"
				}) {
					t.Fatalf("missing sibling's inherited parameter-removal violation: %v", violations)
				}
			})
		}
	}
}

func freezeParameterDocument(pathParameter, operationParameter, extraOperation, callback bool) []byte {
	const parameters = `"parameters":[{"name":"filter","in":"query","deprecated":true,"x-sunset":"2000-01-01","schema":{"type":"string"}}],`
	const responses = `"responses":{"200":{"description":"ok"}}`
	pathFields, operationFields := "", ""
	if pathParameter {
		pathFields = parameters
	}
	if operationParameter {
		operationFields = parameters
	}
	path := fmt.Sprintf(`{%s"get":{%s%s}}`, pathFields, operationFields, responses)
	if extraOperation {
		path = fmt.Sprintf(`{%s"get":{%s%s},"post":{%s}}`, pathFields, operationFields, responses, responses)
	}
	if callback {
		path = fmt.Sprintf(`{"post":{%s,"callbacks":{"notice":{"{$request.query.url}":%s}}}}`, responses, path)
	}
	return []byte(fmt.Sprintf(`{"openapi":"3.1.0","jsonSchemaDialect":"https://spec.openapis.org/oas/3.1/dialect/base","info":{"title":"fixture","version":"1"},"paths":{"/api/v1/thing":%s}}`, path))
}
