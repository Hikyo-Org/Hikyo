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
	for _, context := range []string{"request", "response", "parameter", "parameter-content", "response-header", "response-header-content"} {
		for _, metadata := range []struct{ name, json string }{
			{"ordinary", ""},
			{"deprecated", `,"deprecated":true`},
			{"past-sunset", `,"deprecated":true,"x-sunset":"2000-01-01"`},
			{"future-sunset", `,"deprecated":true,"x-sunset":"2999-01-01"`},
			{"invalid-sunset", `,"deprecated":true,"x-sunset":"invalid"`},
		} {
			for _, shape := range []string{"inline", "nested", "array", "allOf", "anyOf", "oneOf", "reference", "recursive-reference"} {
				if freezeUnresolvableFixture(context, shape) {
					continue
				}
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
							v.Level == "ERR" && strings.Contains(v.Text, `removed"`) && strings.Contains(v.Text, "removed from the frozen API")
					}) {
						t.Fatalf("missing structural property-removal violation: %v", violations)
					}
				})
			}
		}
	}
}

// The loader leaves references inside header content unresolved, and the gate
// then fails closed with a diffing error rather than returning a verdict.
func freezeUnresolvableFixture(context, shape string) bool {
	return context == "response-header-content" && strings.HasSuffix(shape, "reference")
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
	if context == "response-header-content" {
		operation = fmt.Sprintf(`"responses":{"200":{"description":"ok","headers":{"X-Fixture":{"content":{"application/json":{"schema":%s}}}}}}`, schema)
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
				return v.ID == wantID && v.Path == "/api/v1/thing" && v.Operation == "GET" && v.Level == "ERR"
			}) {
				t.Fatalf("missing structural removal violation %q: %v", wantID, violations)
			}
		})
	}
}

func TestFreezeRemovalDiagnosticsAreStable(t *testing.T) {
	document := func(properties string) []byte {
		schema := fmt.Sprintf(`{"type":"object","properties":{%s}}`, properties)
		operation := fmt.Sprintf(`{"requestBody":{"content":{"application/json":{"schema":%s}}},"responses":{"200":{"description":"ok"}}}`, schema)
		return []byte(fmt.Sprintf(`{"openapi":"3.1.0","jsonSchemaDialect":"https://spec.openapis.org/oas/3.1/dialect/base","info":{"title":"fixture","version":"1"},"paths":{"/a":{"post":%[1]s,"put":%[1]s},"/b":{"post":%[1]s}}}`, operation))
	}
	base, revised := document(`"x":{"type":"string"},"y":{"type":"string"}`), document(``)
	want := []string{`/a POST "x"`, `/a POST "y"`, `/a PUT "x"`, `/a PUT "y"`, `/b POST "x"`, `/b POST "y"`}
	// The structural check ranges over maps, so one run can be ordered by luck.
	for range 50 {
		violations, err := api.CheckFreeze(base, revised)
		if err != nil {
			t.Fatal(err)
		}
		var order []string
		for _, v := range violations {
			if v.ID == "hikyo-property-removed" {
				order = append(order, fmt.Sprintf("%s %s %s", v.Path, v.Operation, strings.Fields(v.Text)[4]))
			}
		}
		if !slices.Equal(order, want) {
			t.Fatalf("diagnostic order: got %q, want %q", order, want)
		}
	}
}

func TestFreezeStillPermitsPropertyAdditions(t *testing.T) {
	for _, context := range []string{"request", "response", "parameter", "parameter-content", "response-header", "response-header-content"} {
		for _, shape := range []string{"inline", "nested", "array", "allOf", "anyOf", "oneOf", "reference", "recursive-reference"} {
			if freezeUnresolvableFixture(context, shape) {
				continue
			}
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

func TestFreezeRefusesCallbackRemovalsAndChanges(t *testing.T) {
	const responses = `"responses":{"200":{"description":"ok"}}`
	const endpoint = `{"post":{` + responses + `}}`
	const expression = `"{$request.query.url}"`
	callback := func(operation string) string {
		return `,"callbacks":{"notice":{` + expression + `:{"post":{` + operation + `}}}}`
	}
	body := func(schema string) string {
		return `"requestBody":{"content":{"application/json":{"schema":` + schema + `}}},` + responses
	}
	for _, tc := range []struct{ name, base, revised, wantID string }{
		{"whole-callback", `,"callbacks":{"notice":{` + expression + `:` + endpoint + `}}`, ``, "hikyo-callback-removed"},
		{"empty-callback", `,"callbacks":{"notice":{}}`, ``, "hikyo-callback-removed"},
		{
			"expression",
			`,"callbacks":{"notice":{"{$request.query.url}/kept":` + endpoint + `,"{$request.query.url}/removed":` + endpoint + `}}`,
			`,"callbacks":{"notice":{"{$request.query.url}/kept":` + endpoint + `}}`,
			"hikyo-callback-changed",
		},
		{
			"past-sunset-property",
			callback(body(`{"type":"object","properties":{"kept":{"type":"string"},"removed":{"type":"string","deprecated":true,"x-sunset":"2000-01-01"}}}`)),
			callback(body(`{"type":"object","properties":{"kept":{"type":"string"}}}`)),
			"hikyo-callback-changed",
		},
		{"request-body", callback(body(`{"type":"string"}`)), callback(responses), "hikyo-callback-changed"},
		{
			"response-status",
			callback(`"responses":{"200":{"description":"ok"},"404":{"description":"gone"}}`), callback(responses),
			"hikyo-callback-changed",
		},
		{"enum-value", callback(body(`{"type":"string","enum":["a","b"]}`)), callback(body(`{"type":"string","enum":["a"]}`)), "hikyo-callback-changed"},
		{
			"nested-callback",
			callback(responses + `,"callbacks":{"inner":{` + expression + `:` + endpoint + `}}`), callback(responses),
			"hikyo-callback-changed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := api.CheckFreeze(freezeCallbackDocument(tc.base), freezeCallbackDocument(tc.revised))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(violations, func(v api.Violation) bool {
				return v.ID == tc.wantID && v.Path == "/api/v1/thing" && v.Operation == "POST" && v.Level == "ERR" && strings.Contains(v.Text, `"notice"`)
			}) {
				t.Fatalf("missing callback violation %q: %v", tc.wantID, violations)
			}
		})
	}
}

func freezeCallbackDocument(callbacks string) []byte {
	return []byte(fmt.Sprintf(`{"openapi":"3.1.0","jsonSchemaDialect":"https://spec.openapis.org/oas/3.1/dialect/base","info":{"title":"fixture","version":"1"},"paths":{"/api/v1/thing":{"post":{"responses":{"200":{"description":"ok"}}%s}}}}`, callbacks))
}

// A callback reaching itself through a reference must not hang the gate.
func TestFreezeTerminatesOnCyclicCallbacks(t *testing.T) {
	document := func(properties string) []byte {
		return []byte(fmt.Sprintf(`{"openapi":"3.1.0","jsonSchemaDialect":"https://spec.openapis.org/oas/3.1/dialect/base","info":{"title":"fixture","version":"1"},
			"paths":{"/api/v1/thing":{"post":{"responses":{"200":{"description":"ok"}},"callbacks":{"notice":{"$ref":"#/components/callbacks/Loop"}}}}},
			"components":{"callbacks":{"Loop":{"{$request.query.url}":{"post":{
				"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{%s}}}}},
				"responses":{"200":{"description":"ok"}},
				"callbacks":{"again":{"$ref":"#/components/callbacks/Loop"}}
			}}}}}}`, properties))
	}
	violations, err := api.CheckFreeze(document(`"kept":{"type":"string"},"removed":{"type":"string"}`), document(`"kept":{"type":"string"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(violations, func(v api.Violation) bool { return v.ID == "hikyo-callback-changed" }) {
		t.Fatalf("missing cyclic callback violation: %v", violations)
	}
}

// Each of these passed the gate without a violation before the structural check
// covered them. Deleting a subschema drops its nested properties unvisited.
func TestFreezeRejectsRemovalsUpstreamChecksOmit(t *testing.T) {
	const ok = `"200":{"description":"ok"}`
	response := func(schema string) string {
		return `"responses":{"200":{"description":"ok","content":{"application/json":{"schema":` + schema + `}}}}`
	}
	object := `{"type":"object","properties":{"x":{"type":"string"}}}`
	list := func(fields string) string {
		return response(`{"type":"object","properties":{"l":{"type":"array"` + fields + `}}}`)
	}
	values := func(fields string) string {
		return response(`{"type":"object","properties":{"o":{"type":"object"` + fields + `}}}`)
	}
	parameter := func(fields string) string {
		return `"parameters":[{"name":"filter","in":"query"` + fields + `}],"responses":{` + ok + `}`
	}
	for _, tc := range []struct{ name, base, revised, wantID string }{
		{"array-items", list(`,"items":` + object), list(``), "hikyo-schema-removed"},
		{"map-values", values(`,"additionalProperties":` + object), values(``), "hikyo-schema-removed"},
		{
			"request-items",
			`"requestBody":{"content":{"application/json":{"schema":{"type":"array","items":` + object + `}}}},"responses":{` + ok + `}`,
			`"requestBody":{"content":{"application/json":{"schema":{"type":"array"}}}},"responses":{` + ok + `}`,
			"hikyo-schema-removed",
		},
		{
			"parameter-schema-to-content",
			parameter(`,"schema":{"type":"string"}`), parameter(`,"content":{"application/json":{"schema":{"type":"string"}}}`),
			"hikyo-schema-removed",
		},
		{
			"response-open-enum-value",
			response(`{"type":"string","x-extensible-enum":["a","b"]}`), response(`{"type":"string","x-extensible-enum":["a"]}`),
			"hikyo-enum-value-removed",
		},
		{
			"nested-open-enum-value",
			response(`{"type":"object","properties":{"kind":{"type":"string","x-extensible-enum":["a","b"]}}}`),
			response(`{"type":"object","properties":{"kind":{"type":"string","x-extensible-enum":["a"]}}}`),
			"hikyo-enum-value-removed",
		},
		{
			"open-enum-declaration",
			response(`{"type":"string","x-extensible-enum":["a"]}`), response(`{"type":"string"}`),
			"hikyo-enum-value-removed",
		},
		{"default-response", `"responses":{` + ok + `,"default":{"description":"d"}}`, `"responses":{` + ok + `}`, "hikyo-response-removed"},
		{"wildcard-response", `"responses":{` + ok + `,"4XX":{"description":"d"}}`, `"responses":{` + ok + `}`, "hikyo-response-removed"},
		{
			"response-link",
			`"responses":{"200":{"description":"ok","links":{"next":{"operationId":"next"}}}}`, `"responses":{` + ok + `}`,
			"hikyo-link-removed",
		},
		{
			"encoding",
			`"requestBody":{"content":{"multipart/form-data":{"schema":{"type":"object","properties":{"f":{"type":"string"}}},"encoding":{"f":{"contentType":"text/plain"}}}}},"responses":{` + ok + `}`,
			`"requestBody":{"content":{"multipart/form-data":{"schema":{"type":"object","properties":{"f":{"type":"string"}}}}}},"responses":{` + ok + `}`,
			"hikyo-encoding-removed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := api.CheckFreeze(freezeOperationDocument(tc.base), freezeOperationDocument(tc.revised))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(violations, func(v api.Violation) bool {
				return v.ID == tc.wantID && v.Path == "/api/v1/thing" && v.Operation == "POST" && v.Level == "ERR"
			}) {
				t.Fatalf("missing violation %q: %v", tc.wantID, violations)
			}
		})
	}
}

// A stability label is lifecycle metadata, like a sunset: it hides nothing.
func TestFreezeRefusesChangesUnderAnyStabilityLevel(t *testing.T) {
	for _, level := range []string{"draft", "alpha", "beta", "stable"} {
		operation := func(fields string) []byte {
			return freezeOperationDocument(`"x-stability-level":"` + level + `",` + fields)
		}
		const ok = `"responses":{"200":{"description":"ok"}}`
		enum := func(values string) string {
			return `"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"string","enum":` + values + `}}}}}`
		}
		for name, change := range map[string]struct{ base, revised string }{
			"security":      {`"security":[{"a":[]}],` + ok, ok},
			"response-enum": {enum(`["a","b"]`), enum(`["a"]`)},
		} {
			t.Run(level+"/"+name, func(t *testing.T) {
				violations, err := api.CheckFreeze(operation(change.base), operation(change.revised))
				if err != nil {
					t.Fatal(err)
				}
				if len(violations) == 0 {
					t.Fatal("a forbidden change passed the gate")
				}
			})
		}
	}
}

func TestFreezeDoesNotRefuseEdits(t *testing.T) {
	response := func(schema string) string {
		return `"responses":{"200":{"description":"ok","content":{"application/json":{"schema":` + schema + `}}}}`
	}
	const ok = `"responses":{"200":{"description":"ok"}}`
	object := `{"type":"object","properties":{"x":{"type":"string"}}}`
	for _, tc := range []struct{ name, base, revised string }{
		{"empty-items-removed", response(`{"type":"array","items":{}}`), response(`{"type":"array"}`)},
		{"empty-additional-properties-removed", response(`{"type":"object","additionalProperties":{}}`), response(`{"type":"object"}`)},
		{"empty-additional-properties-to-true", response(`{"type":"object","additionalProperties":{}}`), response(`{"type":"object","additionalProperties":true}`)},
		{"items-added", response(`{"type":"array"}`), response(`{"type":"array","items":` + object + `}`)},
		{"additional-properties-added", response(`{"type":"object"}`), response(`{"type":"object","additionalProperties":` + object + `}`)},
		{"whole-callback-added", ok, ok + `,"callbacks":{"notice":{"{$request.query.url}":{"post":{` + ok + `}}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := api.CheckFreeze(freezeOperationDocument(tc.base), freezeOperationDocument(tc.revised))
			if err != nil || len(violations) != 0 {
				t.Fatalf("a compatible edit was refused: %v, %v", violations, err)
			}
		})
	}
}

func TestFreezeStillPermitsOpenEnumGrowth(t *testing.T) {
	document := func(values string) []byte {
		return freezeOperationDocument(`"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"string","x-extensible-enum":` + values + `}}}}}`)
	}
	violations, err := api.CheckFreeze(document(`["a"]`), document(`["b","a"]`))
	if err != nil || len(violations) != 0 {
		t.Fatalf("a permitted open-enum addition was refused: %v, %v", violations, err)
	}
}

func freezeOperationDocument(operation string) []byte {
	return []byte(fmt.Sprintf(`{"openapi":"3.1.0","jsonSchemaDialect":"https://spec.openapis.org/oas/3.1/dialect/base","info":{"title":"fixture","version":"1"},"components":{"securitySchemes":{"a":{"type":"http","scheme":"bearer"}}},"paths":{"/api/v1/thing":{"post":{%s}}}}`, operation))
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
					if callback {
						return v.ID == "hikyo-callback-changed" && v.Operation == "POST"
					}
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
