package api_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
)

// The document-coordinate layer (#1217). Every refused case below is a change
// the oasdiff-based layers let through (or only catch by luck), so each must
// also produce a hikyo-contract-* violation: the layer is independent.

const contractOK = `"responses":{"200":{"description":"ok"}}`

func contractDoc(paths string) string {
	return `{"openapi":"3.1.0","jsonSchemaDialect":"https://spec.openapis.org/oas/3.1/dialect/base","info":{"title":"fixture","version":"1"},"paths":` + paths + `}`
}

func contractThing(operation string) string {
	return contractDoc(`{"/api/v1/thing":{"post":{` + operation + `}}}`)
}

// inPosition places a schema where the oasdiff layers are known to be blind.
func inPosition(position, schema string) string {
	switch position {
	case "param":
		return contractThing(`"parameters":[{"name":"p","in":"query","schema":` + schema + `}],` + contractOK)
	case "param-content":
		return contractThing(`"parameters":[{"name":"p","in":"query","content":{"application/json":{"schema":` + schema + `}}}],` + contractOK)
	case "header":
		return contractThing(`"responses":{"200":{"description":"ok","headers":{"X-Fixture":{"schema":` + schema + `}}}}`)
	case "header-content":
		return contractThing(`"responses":{"200":{"description":"ok","headers":{"X-Fixture":{"content":{"application/json":{"schema":` + schema + `}}}}}}`)
	case "request":
		return contractThing(`"requestBody":{"content":{"application/json":{"schema":` + schema + `}}},` + contractOK)
	}
	return contractThing(`"responses":{"200":{"description":"ok","content":{"application/json":{"schema":` + schema + `}}}}`)
}

// contractRich carries one of everything the layer compares, with unique
// anchors so a case can edit exactly one thing.
const contractRich = `{"openapi":"3.1.0","jsonSchemaDialect":"https://spec.openapis.org/oas/3.1/dialect/base",
"info":{"title":"fixture","version":"1","description":"d"},
"servers":[{"url":"https://a.example/v1","variables":{"v":{"enum":["x","y"],"default":"x"},
  "description":{"default":"d"},"title":{"default":"t"},"example":{"default":"e"},"summary":{"default":"s"}}}],
"tags":[{"name":"t1","description":"d"}],
"paths":{"/api/v1/thing":{
 "servers":[{"url":"https://p.example"}],
 "post":{"operationId":"makeThing","servers":[{"url":"https://o.example"}],"security":[{},{"a":[]}],
  "parameters":[
   {"name":"explode_p","in":"query","explode":true,"style":"form","allowEmptyValue":true,"allowReserved":false,"schema":{"type":"string"}},
   {"name":"cont","in":"query","content":{"application/json":{"schema":{"type":"object","required":["k"],"properties":{"k":{"type":"string","enum":["e1","e2"]}}}}}},
   {"name":"req_s","in":"query","schema":{"type":"object","required":["rq"],"properties":{"rq":{"type":"string"},"zz":{"type":"string"}}}}
  ],
  "requestBody":{"content":{
   "application/json":{"schema":{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"nested":{"type":"object","properties":{"x":{"type":"string"}}},"pi":{"type":"array","prefixItems":[{"type":"string"},{"type":"integer"}]}}}},
   "multipart/form-data":{"schema":{"type":"object","properties":{"f":{"type":"string"}}},"encoding":{"f":{"contentType":"image/png","style":"form","headers":{"X-Enc":{"schema":{"type":"object","properties":{"ea":{"type":"string"},"eb":{"type":"string"}}}}}}}},
   "application/vnd.shared+json":{"schema":{"$ref":"#/components/schemas/Shared"}},
   "application/vnd.req+json":{"schema":{"$ref":"#/components/schemas/ReqOnly"}}}},
  "responses":{"200":{"description":"ok",
   "headers":{
    "X-Fixture":{"schema":{"type":"object","required":["hn"],"properties":{"hn":{"type":"object","required":["deep"],"properties":{"deep":{"type":"string"}}}}}},
    "X-Enum":{"schema":{"type":"string","enum":["a","b"]}},
    "description":{"schema":{"type":"string"}},"title":{"schema":{"type":"string"}},"example":{"schema":{"type":"string"}},"summary":{"schema":{"type":"string"}}},
   "content":{
    "application/json":{"schema":{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"rpi":{"type":"array","prefixItems":[{"type":"string"},{"type":"integer"}]},
      "xe":{"type":"string","x-extensible-enum":["a","b"]},"xm":{"type":"string","x-extensible-enum":"notalist"},
      "description":{"type":"string"},"title":{"type":"string"},"example":{"type":"string"},"summary":{"type":"string"}}}},
    "application/vnd.shared+json":{"schema":{"$ref":"#/components/schemas/Shared"}},
    "application/vnd.resp+json":{"schema":{"$ref":"#/components/schemas/RespOnly"}}},
   "links":{"L":{"operationId":"getThing","parameters":{"pid":"$response.body#/id"},"requestBody":"$response.body#/rb"}}}},
  "callbacks":{"cb":{"{$request.body#/url}":{"post":{"responses":{"200":{"description":"ok"}}}},"{$request.body#/alt}":{"post":{"responses":{"200":{"description":"ok"}}}}}}
 }}},
"components":{
 "schemas":{
  "Shared":{"type":"object","required":["a"],"properties":{"a":{"type":"string"}}},
  "ReqOnly":{"type":"object","required":["a"],"properties":{"a":{"type":"string"}}},
  "RespOnly":{"type":"object","required":["a"],"properties":{"a":{"type":"string"}}},
  "Orphan":{"type":"object","properties":{"keep":{"type":"string"}}}},
 "securitySchemes":{"oauth":{"type":"oauth2","description":"d","flows":{
  "authorizationCode":{"authorizationUrl":"https://a.example/auth","tokenUrl":"https://a.example/token","scopes":{"read":"Read it","description":"d","title":"t","example":"e","summary":"s"}},
  "clientCredentials":{"tokenUrl":"https://a.example/token","scopes":{"read":"Read it"}}}}}}}`

// contractTree decodes doc, applies fn and re-encodes it.
func contractTree(t *testing.T, doc string, fn func(root map[string]any)) string {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal([]byte(doc), &root); err != nil {
		t.Fatal(err)
	}
	fn(root)
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// contractAt walks a path of map keys and list indexes.
func contractAt(t *testing.T, n any, path ...string) any {
	t.Helper()
	for _, p := range path {
		switch v := n.(type) {
		case map[string]any:
			n = v[p]
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil {
				t.Fatal(err)
			}
			n = v[i]
		default:
			t.Fatalf("no %q under %v", p, path)
		}
	}
	return n
}

func contractWithout(t *testing.T, doc string, path ...string) string {
	return contractTree(t, doc, func(root map[string]any) {
		parent := contractAt(t, root, path[:len(path)-1]...).(map[string]any)
		last := path[len(path)-1]
		if _, ok := parent[last]; !ok {
			t.Fatalf("nothing at %v", path)
		}
		delete(parent, last)
	})
}

func contractReplace(t *testing.T, doc, old, replacement string) string {
	t.Helper()
	if strings.Count(doc, old) != 1 {
		t.Fatalf("anchor %q occurs %d times, want 1", old, strings.Count(doc, old))
	}
	return strings.Replace(doc, old, replacement, 1)
}

func contractViolations(t *testing.T, base, revised string) []api.Violation {
	t.Helper()
	violations, err := api.CheckFreeze([]byte(base), []byte(revised))
	if err != nil {
		t.Fatalf("gate errored: %v", err)
	}
	return violations
}

func hasContractViolation(vs []api.Violation) bool {
	return slices.ContainsFunc(vs, func(v api.Violation) bool { return strings.HasPrefix(v.ID, "hikyo-contract-") })
}

func TestFreezeContractRefusesForbiddenChanges(t *testing.T) {
	type refusal struct{ name, base, revised string }
	var cases []refusal
	add := func(name, base, revised string) { cases = append(cases, refusal{name, base, revised}) }
	edit := func(name, old, replacement string) {
		add(name, contractRich, contractReplace(t, contractRich, old, replacement))
	}

	// Authentication alternatives.
	secured := func(security string) string {
		return contractThing(`"security":` + security + `,` + contractOK)
	}
	add("security alternative removed, anonymous kept", secured(`[{},{"a":[]}]`), secured(`[{}]`))
	add("security emptied", secured(`[{},{"a":[]}]`), secured(`[]`))

	// Closed enums in response headers.
	headerEnum := func(values string) string {
		return contractThing(`"responses":{"200":{"description":"ok","headers":{"X-Fixture":{"schema":{"type":"string","enum":` + values + `}}}}}`)
	}
	add("response header enum value removed", headerEnum(`["a","b"]`), headerEnum(`["a"]`))
	add("response header enum grown", headerEnum(`["a"]`), headerEnum(`["a","b"]`))
	add("response header enum grown by mldsa65", headerEnum(`["v1"]`), headerEnum(`["v1","mldsa65"]`))

	// Media types.
	media := func(name string) string {
		return contractThing(`"requestBody":{"content":{"` + name + `":{"schema":{"type":"string"}}}},` + contractOK)
	}
	add("request media type renamed to a vendor type", media("application/json"), media("application/vnd.x+json"))
	add("request media type renamed to problem+json", media("application/json"), media("application/problem+json"))

	// Positions the oasdiff layers do not descend into.
	for _, position := range []string{"param", "header", "param-content", "header-content"} {
		for _, keyword := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
			add(keyword+" branch deleted in "+position,
				inPosition(position, `{"`+keyword+`":[{"type":"string"},{"type":"integer"}]}`),
				inPosition(position, `{"`+keyword+`":[{"type":"string"}]}`))
		}
	}
	for _, position := range []string{"request", "response"} {
		add("prefixItems entry deleted in "+position,
			inPosition(position, `{"type":"array","prefixItems":[{},{}]}`),
			inPosition(position, `{"type":"array","prefixItems":[{}]}`))
	}
	for _, position := range []string{"param", "header"} {
		for keyword, value := range map[string][2]string{
			"patternProperties": {`{"^a":{},"^b":{}}`, `{"^a":{}}`},
			"dependentSchemas":  {`{"a":{},"b":{}}`, `{"a":{}}`},
			"dependentRequired": {`{"a":["b"],"c":["d"]}`, `{"a":["b"]}`},
			"discriminator":     {`{"propertyName":"t","mapping":{"a":"#/x","b":"#/y"}}`, `{"propertyName":"t","mapping":{"a":"#/x"}}`},
		} {
			add(keyword+" entry deleted in "+position,
				inPosition(position, `{"type":"object","`+keyword+`":`+value[0]+`}`),
				inPosition(position, `{"type":"object","`+keyword+`":`+value[1]+`}`))
		}
	}

	// `content` of a parameter or header.
	for _, position := range []string{"param", "header"} {
		content := inPosition(position+"-content", `{"type":"string"}`)
		schema := inPosition(position, `{"type":"string"}`)
		add("media type swapped in "+position+" content", content,
			contractReplace(t, content, `"application/json"`, `"application/xml"`))
		add(position+" content replaced by schema", content, schema)
		add(position+" content deleted", content, strings.Replace(
			contractReplace(t, content, `"content":{"application/json":{"schema":{"type":"string"}}}`, `"x-gone":true`), `"x-gone":true`, `"x-other":1`, 1))
		add("enum value removed in "+position+" content",
			inPosition(position+"-content", `{"type":"string","enum":["a","b"]}`),
			inPosition(position+"-content", `{"type":"string","enum":["a"]}`))
		add("type changed in "+position+" content",
			inPosition(position+"-content", `{"type":"string"}`),
			inPosition(position+"-content", `{"type":"integer"}`))
	}

	// Links, encodings, callbacks.
	edit("link parameters changed", `"pid":"$response.body#/id"`, `"pid":"$response.body#/other"`)
	edit("link requestBody changed", `"$response.body#/rb"`, `"$response.body#/rb2"`)
	edit("link operationId changed", `"operationId":"getThing"`, `"operationId":"getOther"`)
	edit("encoding contentType changed", `"image/png"`, `"image/jpeg"`)
	edit("encoding style changed", `"style":"form","headers"`, `"style":"deepObject","headers"`)
	add("encoding header removed", contractRich, contractWithout(t, contractRich,
		"paths", "/api/v1/thing", "post", "requestBody", "content", "multipart/form-data", "encoding", "f", "headers", "X-Enc"))
	add("property removed inside an encoding header", contractRich, contractWithout(t, contractRich,
		"paths", "/api/v1/thing", "post", "requestBody", "content", "multipart/form-data", "encoding", "f", "headers", "X-Enc", "schema", "properties", "eb"))
	add("callback expression removed", contractRich, contractWithout(t, contractRich,
		"paths", "/api/v1/thing", "post", "callbacks", "cb", "{$request.body#/alt}"))
	edit("callback expression renamed", `{$request.body#/alt}`, `{$request.body#/other}`)

	// Request narrowing.
	add("items added to a request array", inPosition("request", `{"type":"array"}`), inPosition("request", `{"type":"array","items":{"type":"string"}}`))
	edit("additionalProperties false added on a nested object", `"nested":{"type":"object",`, `"nested":{"type":"object","additionalProperties":false,`)
	edit("additionalProperties false added on the body root", `"application/json":{"schema":{"type":"object","required":["name"],`, `"application/json":{"schema":{"type":"object","additionalProperties":false,"required":["name"],`)
	edit("not added at the body root", `"application/json":{"schema":{"type":"object","required":["name"],`, `"application/json":{"schema":{"type":"object","not":{"type":"null"},"required":["name"],`)
	edit("pattern added at the body root", `"application/json":{"schema":{"type":"object","required":["name"],`, `"application/json":{"schema":{"type":"object","pattern":"^x","required":["name"],`)
	edit("required names an undeclared request property", `"required":["name"]`, `"required":["name","ghost"]`)
	edit("required grows inside a parameter schema", `"required":["rq"]`, `"required":["rq","zz"]`)
	edit("nested property type changed in a response header schema", `"deep":{"type":"string"}`, `"deep":{"type":"integer"}`)
	edit("nested required dropped in a response header schema", `"required":["deep"],`, ``)

	// Security schemes, servers, parameters.
	add("OAuth flow removed", contractRich, contractWithout(t, contractRich, "components", "securitySchemes", "oauth", "flows", "clientCredentials"))
	add("all OAuth flows removed", contractRich, contractWithout(t, contractRich, "components", "securitySchemes", "oauth", "flows"))
	edit("document server url changed", `https://a.example/v1`, `https://b.example/v1`)
	edit("path server url changed", `https://p.example`, `https://q.example`)
	edit("operation server url changed", `https://o.example`, `https://r.example`)
	edit("server variable enum changed", `"enum":["x","y"]`, `"enum":["x","y","z"]`)
	edit("server variable default changed", `"default":"x"`, `"default":"y"`)
	add("server variable enum removed", contractRich, contractWithout(t, contractRich, "servers", "0", "variables", "v", "enum"))
	add("server variable default removed", contractRich, contractWithout(t, contractRich, "servers", "0", "variables", "v", "default"))
	edit("parameter explode changed", `"explode":true`, `"explode":false`)
	edit("parameter style changed", `"style":"form","allowEmptyValue"`, `"style":"spaceDelimited","allowEmptyValue"`)
	edit("parameter allowEmptyValue changed", `"allowEmptyValue":true`, `"allowEmptyValue":false`)
	edit("parameter allowReserved changed", `"allowReserved":false`, `"allowReserved":true`)
	edit("malformed x-extensible-enum removed", `,"x-extensible-enum":"notalist"`, ``)

	// Prose keywords are exempt only as keywords. The same words as names are
	// properties, headers, variables and scopes, and stay protected.
	for _, name := range []string{"description", "title", "example", "summary"} {
		resp := []string{"paths", "/api/v1/thing", "post", "responses", "200"}
		add("property named "+name+" removed", contractRich,
			contractWithout(t, contractRich, append(slices.Clone(resp), "content", "application/json", "schema", "properties", name)...))
		add("response header named "+name+" removed", contractRich,
			contractWithout(t, contractRich, append(slices.Clone(resp), "headers", name)...))
		add("server variable named "+name+" removed", contractRich,
			contractWithout(t, contractRich, "servers", "0", "variables", name))
		add("OAuth scope named "+name+" removed", contractRich,
			contractWithout(t, contractRich, "components", "securitySchemes", "oauth", "flows", "authorizationCode", "scopes", name))
	}

	// Unreferenced components are still part of the promise.
	add("property removed from an unreferenced component", contractRich,
		contractWithout(t, contractRich, "components", "schemas", "Orphan", "properties", "keep"))
	add("component renamed", contractRich, contractReplace(t, contractRich, `"Orphan":`, `"Renamed":`))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations := contractViolations(t, tc.base, tc.revised)
			if len(violations) == 0 {
				t.Fatal("a forbidden change passed the gate")
			}
			if !hasContractViolation(violations) {
				t.Fatalf("the document-coordinate layer did not refuse it:\n%s", render(violations))
			}
			for _, v := range violations {
				if strings.HasPrefix(v.ID, "hikyo-contract-") {
					t.Log(v)
				}
			}
		})
	}
}

// Real-spec forms of the gaps listed on #1217.
func TestFreezeContractRefusesForbiddenChangesInTheRealSpec(t *testing.T) {
	spec := string(api.SpecYAML)
	after := func(anchor, old, replacement string) string {
		i := strings.Index(spec, anchor)
		if i < 0 || !strings.Contains(spec[i:], old) {
			t.Fatalf("anchor %q or text %q is gone from the spec", anchor, old)
		}
		return spec[:i] + strings.Replace(spec[i:], old, replacement, 1)
	}
	const receipt = "Hikyo-Audit-Receipt:"
	const transit = "enum: [xchacha20-poly1305, ed25519, hmac-sha256, mldsa65]"
	pairBase, _ := mldsa65Pair(t)

	for _, tc := range []struct{ name, base, revised string }{
		{"samlStart anonymous alternative kept, session removed", spec,
			after("operationId: samlStart", "        - {}\n        - hikyoSession: []\n", "        - {}\n")},
		{"samlStart security emptied", spec,
			after("operationId: samlStart", "      security:\n        - {}\n        - hikyoSession: []\n", "      security: []\n")},
		{"Hikyo-Audit-Receipt enum value removed", spec, after(receipt, "enum: [v1]", "enum: []")},
		{"Hikyo-Audit-Receipt grown by mldsa65", spec, after(receipt, "enum: [v1]", "enum: [v1, mldsa65]")},
		{"Hikyo-Audit-Receipt grown by mldsa44", spec, after(receipt, "enum: [v1]", "enum: [v1, mldsa44]")},
		{"AuditExportReceipt property removed", spec,
			after("    AuditExportReceipt:", "        rows_streamed:\n", "        rows_streamed_gone:\n")},
		{"AuditExportReceiptLine property removed", spec,
			after("    AuditExportReceiptLine:", "        export_receipt:\n", "        export_receipt_gone:\n")},
		// Under `not`, dropping a required name forbids what it used to allow.
		{"PrepareRemotePairingRequest not.required narrowed", spec,
			after("    PrepareRemotePairingRequest:", "required: [phrase, word_count]", "required: [phrase]")},
		// YAML 1.2, as every other reader of the spec: `off` is a string.
		{"UpdateStatus channel off becomes a boolean", spec, after("[stable, nightly, off]", "off]", "false]")},
		// Referencing an unreferenced component from one side does not relax it.
		{"AuditExportReceipt newly read by a request, required shrunk", spec,
			strings.Replace(after("    AuditExportReceipt:",
				"required: [version, started_event_id, snapshot_time, rows_streamed, outcome, outcome_recorded]", "required: [version]"),
				"        key_algorithm: {type: string, enum: [ecdsa-p256, ecdsa-p384, rsa-3072, rsa-4096, mldsa65]}\n        ttl_seconds:",
				"        key_algorithm: {type: string, enum: [ecdsa-p256, ecdsa-p384, rsa-3072, rsa-4096, mldsa65]}\n        zz_receipt: {$ref: \"#/components/schemas/AuditExportReceipt\"}\n        ttl_seconds:", 1)},
		// The mldsa65 amendment is exact.
		{"mldsa44 added at a listed location", spec, strings.Replace(spec, transit, "enum: [xchacha20-poly1305, ed25519, hmac-sha256, mldsa65, mldsa44]", 1)},
		{"mldsa65 added at an unlisted enum", spec, strings.Replace(spec, "enum: [software, external]", "enum: [software, external, mldsa65]", 1)},
		{"mldsa65 and another value at a listed location", pairBase, strings.Replace(spec, transit, "enum: [xchacha20-poly1305, ed25519, hmac-sha256, mldsa65, mldsa87]", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.base == tc.revised {
				t.Fatal("the mutation changed nothing")
			}
			violations := contractViolations(t, tc.base, tc.revised)
			if !hasContractViolation(violations) {
				t.Fatalf("the document-coordinate layer did not refuse it:\n%s", render(violations))
			}
		})
	}
}

func TestFreezeContractPermitsAdditionsAndProse(t *testing.T) {
	respPath := []string{"paths", "/api/v1/thing", "post", "responses", "200"}
	json200 := append(slices.Clone(respPath), "content", "application/json", "schema")
	put := func(t *testing.T, root map[string]any, value any, path ...string) {
		contractAt(t, root, path[:len(path)-1]...).(map[string]any)[path[len(path)-1]] = value
	}
	op := []string{"paths", "/api/v1/thing", "post"}
	type pair struct {
		name    string
		revised func(t *testing.T) string
	}
	tree := func(fn func(t *testing.T, root map[string]any)) func(t *testing.T) string {
		return func(t *testing.T) string {
			return contractTree(t, contractRich, func(root map[string]any) { fn(t, root) })
		}
	}
	cases := []pair{
		{"new optional request property on a shared component", tree(func(t *testing.T, r map[string]any) {
			put(t, r, map[string]any{"type": "string"}, "components", "schemas", "Shared", "properties", "b")
		})},
		{"request property becomes optional", tree(func(t *testing.T, r map[string]any) {
			put(t, r, []any{}, "components", "schemas", "ReqOnly", "required")
		})},
		{"response property added as required", tree(func(t *testing.T, r map[string]any) {
			put(t, r, map[string]any{"type": "string"}, "components", "schemas", "RespOnly", "properties", "b")
			put(t, r, []any{"a", "b"}, "components", "schemas", "RespOnly", "required")
		})},
		{"new response header", tree(func(t *testing.T, r map[string]any) {
			put(t, r, map[string]any{"schema": map[string]any{"type": "string"}}, append(slices.Clone(respPath), "headers", "X-New")...)
		})},
		{"new response status", tree(func(t *testing.T, r map[string]any) {
			put(t, r, map[string]any{"description": "gone"}, append(slices.Clone(op), "responses", "410")...)
		})},
		{"new response media type", tree(func(t *testing.T, r map[string]any) {
			put(t, r, map[string]any{"schema": map[string]any{"type": "string"}}, append(slices.Clone(respPath), "content", "application/xml")...)
		})},
		{"new component", tree(func(t *testing.T, r map[string]any) {
			put(t, r, map[string]any{"type": "string"}, "components", "schemas", "Brand")
		})},
		{"new top-level tag", tree(func(t *testing.T, r map[string]any) {
			r["tags"] = append(r["tags"].([]any), map[string]any{"name": "t2"})
		})},
		{"open enum grown", tree(func(t *testing.T, r map[string]any) {
			put(t, r, []any{"a", "b", "c"}, append(slices.Clone(json200), "properties", "xe", "x-extensible-enum")...)
		})},
		{"operation deprecated", tree(func(t *testing.T, r map[string]any) { put(t, r, true, append(slices.Clone(op), "deprecated")...) })},
		{"property deprecated", tree(func(t *testing.T, r map[string]any) {
			put(t, r, true, append(slices.Clone(json200), "properties", "id", "deprecated")...)
		})},
		{"prose edited", tree(func(t *testing.T, r map[string]any) {
			put(t, r, "new", "info", "title")
			put(t, r, "new", "tags", "0", "description")
			put(t, r, "new summary", append(slices.Clone(op), "summary")...)
			put(t, r, "new", append(slices.Clone(respPath), "headers", "X-Enum", "description")...)
			put(t, r, "new", "servers", "0", "variables", "v", "description")
		})},
		{"new optional parameter", tree(func(t *testing.T, r map[string]any) {
			p := contractAt(t, r, append(slices.Clone(op), "parameters")...).([]any)
			put(t, r, append(p, map[string]any{"name": "extra", "in": "query", "schema": map[string]any{"type": "string"}}), append(slices.Clone(op), "parameters")...)
		})},
		{"new operationId", tree(func(t *testing.T, r map[string]any) {
			put(t, r, map[string]any{"responses": map[string]any{"200": map[string]any{"description": "ok"}}}, "paths", "/api/v1/thing", "get")
		})},
	}
	// Additive by the version promise, yet refused by the oasdiff allowlist
	// (an id it does not list). Pinned, not worked around: the document layer
	// must stay silent, and changing the allowlist is a policy decision.
	allowlistRefuses := map[string]string{
		"new response header": "response-header-added",
		"property deprecated": "response-property-deprecated",
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations := contractViolations(t, contractRich, tc.revised(t))
			if hasContractViolation(violations) {
				t.Fatalf("the document layer refused a permitted change:\n%s", render(violations))
			}
			if id, known := allowlistRefuses[tc.name]; known {
				if len(violations) != 1 || violations[0].ID != id {
					t.Fatalf("want only the known allowlist refusal %s:\n%s", id, render(violations))
				}
			} else if len(violations) > 0 {
				t.Fatalf("a permitted change was refused:\n%s", render(violations))
			}
		})
	}

	// Moving an unchanged parameter between the path and its operation is
	// neutral in both directions.
	t.Run("parameter moved between path and operation", func(t *testing.T) {
		moved := contractTree(t, contractRich, func(root map[string]any) {
			params := contractAt(t, root, append([]string{"paths", "/api/v1/thing", "post"}, "parameters")...).([]any)
			first := params[0]
			put(t, root, params[1:], "paths", "/api/v1/thing", "post", "parameters")
			put(t, root, []any{first}, "paths", "/api/v1/thing", "parameters")
		})
		for _, dir := range [][2]string{{contractRich, moved}, {moved, contractRich}} {
			if violations := contractViolations(t, dir[0], dir[1]); len(violations) > 0 {
				t.Fatalf("moving a parameter was refused:\n%s", render(violations))
			}
		}
	})
}

func TestFreezeContractIdentity(t *testing.T) {
	if violations := contractViolations(t, contractRich, contractRich); len(violations) > 0 {
		t.Fatalf("the fixture is not equal to itself:\n%s", render(violations))
	}
}

// Failures of the mldsa65 amendment in document coordinates.
func TestMLDSA65EnumLocations(t *testing.T) {
	if n := len(api.MLDSA65EnumLocations); n != 5 {
		t.Fatalf("the amendment lists %d document locations, want 5", n)
	}
	base, revised := mldsa65Pair(t)
	all := api.MLDSA65EnumLocations
	t.Cleanup(func() { api.MLDSA65EnumLocations = all })
	for i, want := range all {
		api.MLDSA65EnumLocations = slices.Delete(slices.Clone(all), i, i+1)
		got := contractViolations(t, base, revised)
		if len(got) != 1 || got[0].ID != "hikyo-contract-added" || !strings.Contains(got[0].Text, want) {
			t.Errorf("without %s the guard reported:\n%s", want, render(got))
		}
	}
}

// Known and accepted false refusals (#1217 comments). They pin which source
// still refuses each, so a change in that is noticed rather than silent.
func TestFreezeKnownFalseRefusals(t *testing.T) {
	// refusedOnlyBy asserts that exactly the documented sources refuse it.
	refusedOnlyBy := func(t *testing.T, vs []api.Violation, want ...string) {
		t.Helper()
		var got []string
		for _, v := range vs {
			if !slices.Contains(got, v.ID) {
				got = append(got, v.ID)
			}
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("refused by %v, want %v:\n%s", got, want, render(vs))
		}
	}
	t.Run("prose edit to components.schemas.ID.description", func(t *testing.T) {
		spec := string(api.SpecYAML)
		i := strings.Index(spec, "\n    ID:\n")
		if i < 0 {
			t.Fatal("components.schemas.ID is gone")
		}
		// Search only the ID block, which ends at the next 4-space-indented key.
		block := spec[i : i+len("\n    ID:\n")]
		for _, line := range strings.SplitAfter(spec[i+len(block):], "\n") {
			if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "      ") {
				break
			}
			block += line
		}
		j := strings.Index(block, "\n      description:")
		if j < 0 {
			t.Skip("components.schemas.ID carries no description to edit")
		}
		j += len("\n      ")
		edited := spec[:i+j] + "description: edited. " + spec[i+j+len("description:"):]
		refusedOnlyBy(t, contractViolations(t, spec, edited), "request-body-any-of-added", "request-body-any-of-removed")
	})

	oauth := func(scopes string) string {
		return fmt.Sprintf(`{"openapi":"3.1.0","jsonSchemaDialect":"https://spec.openapis.org/oas/3.1/dialect/base","info":{"title":"f","version":"1"},"paths":{},"components":{"securitySchemes":{"o":{"type":"oauth2","flows":{"implicit":{"authorizationUrl":"https://a.example","scopes":%s}}}}}}`, scopes)
	}
	t.Run("OAuth scope reworded", func(t *testing.T) {
		refusedOnlyBy(t, contractViolations(t, oauth(`{"read":"Read"}`), oauth(`{"read":"Read it"}`)), "api-security-component-oauth-scope-changed")
	})
	t.Run("OAuth scope added", func(t *testing.T) {
		refusedOnlyBy(t, contractViolations(t, oauth(`{"read":"Read"}`), oauth(`{"read":"Read","write":"Write"}`)),
			"api-security-component-oauth-scope-added", "hikyo-contract-added")
	})
	t.Run("single-branch oneOf wrapper in a parameter", func(t *testing.T) {
		schema := `{"type":"object","properties":{"a":{"type":"string"}}}`
		refusedOnlyBy(t, contractViolations(t, inPosition("param", schema), inPosition("param", `{"oneOf":[`+schema+`]}`)),
			"hikyo-contract-added", "hikyo-contract-removed", "hikyo-property-removed")
	})
	t.Run("path-level header parameter shadowed in another case", func(t *testing.T) {
		doc := func(pathLevel string) string {
			return contractDoc(`{"/api/v1/thing":{` + pathLevel + `"post":{"parameters":[{"name":"x-a","in":"header","schema":{"type":"string"}}],` + contractOK + `}}}`)
		}
		refusedOnlyBy(t, contractViolations(t, doc(`"parameters":[{"name":"X-A","in":"header","schema":{"type":"string"}}],`), doc("")),
			"hikyo-parameter-removed", "request-parameter-removed")
	})
}

// Shapes the first review of #1217 showed passing every layer.
func TestFreezeContractRefusesDirectionlessAndMisreadChanges(t *testing.T) {
	body := func(schema string) string {
		return contractThing(`"requestBody":{"content":{"application/json":{"schema":` + schema + `}}},` + contractOK)
	}
	response := func(schema string) string { return inPosition("response", schema) }
	headerYAML := func(values string) string {
		return "openapi: 3.1.0\njsonSchemaDialect: https://spec.openapis.org/oas/3.1/dialect/base\ninfo: {title: f, version: \"1\"}\n" +
			"paths:\n  /api/v1/thing:\n    get:\n      responses:\n        \"200\":\n          description: ok\n" +
			"          headers:\n            X-Mode: {schema: {enum: " + values + "}}\n"
	}
	encoded := func(header string) string {
		return contractThing(`"requestBody":{"content":{"multipart/form-data":{"schema":{"type":"object"},"encoding":{"f":{"headers":{"X-Enc":{"schema":` + header + `}}}}}}}},` + contractOK)
	}
	component := func(header, schema string) string {
		return contractDoc(`{"/api/v1/thing":{"post":{"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/In"}}}},` +
			`"responses":{"200":{"description":"ok","headers":{"X-Deep":{"schema":` + header + `}}}}}}},"components":{"schemas":{"In":` + schema + `}}`)
	}
	branches := `{"oneOf":[{"type":"object","required":["a"],"properties":{"a":{"type":"string"},"b":{"type":"string"}}},{"type":"object","required":["b"],"properties":{"b":{"type":"string"}}}]}`
	for _, tc := range []struct{ name, base, revised string }{
		{"request oneOf branch drops required", body(branches), body(strings.Replace(branches, `"required":["a"],`, ``, 1))},
		{"response oneOf branch gains a required new property", response(branches),
			response(strings.Replace(branches, `"required":["b"],"properties":{"b":{"type":"string"}}`, `"required":["b","c"],"properties":{"b":{"type":"string"},"c":{"type":"string"}}`, 1))},
		{"request if.required narrowed", body(`{"if":{"required":["a","b"]},"then":{"required":["c"]}}`), body(`{"if":{"required":["a"]},"then":{"required":["c"]}}`)},
		{"response not gains items", response(`{"not":{"type":"array"}}`), response(`{"not":{"type":"array","items":{"type":"string"}}}`)},
		{"property added under not", body(`{"not":{"required":["a"]}}`), body(`{"not":{"required":["a"],"properties":{"b":{"type":"string"}}}}`)},
		{"header enum yes becomes true", headerYAML("[yes]"), headerYAML("[true]")},
		{"header enum off becomes false", headerYAML("[\"off\"]"), headerYAML("[false]")},
		{"request encoding header gains items", encoded(`{"type":"array"}`), encoded(`{"type":"array","items":{"type":"string"}}`)},
		{"request encoding header gains additionalProperties", encoded(`{"type":"object"}`), encoded(`{"type":"object","additionalProperties":{"type":"integer"}}`)},
		{"request encoding header gains a required property", encoded(`{"type":"object"}`), encoded(`{"type":"object","required":["n"],"properties":{"n":{"type":"string"}}}`)},
		{"deep ref: response reads a request-only subschema that drops required",
			component(`{"$ref":"#/components/schemas/In/properties/x"}`, `{"type":"object","properties":{"x":{"type":"object","required":["y"],"properties":{"y":{"type":"string"}}}}}`),
			component(`{"$ref":"#/components/schemas/In/properties/x"}`, `{"type":"object","properties":{"x":{"type":"object","properties":{"y":{"type":"string"}}}}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if violations := contractViolations(t, tc.base, tc.revised); !hasContractViolation(violations) {
				t.Fatalf("the document-coordinate layer did not refuse it:\n%s", render(violations))
			}
		})
	}

	// A schema no operation read in the base, newly read by a response, is
	// still judged as read and written.
	t.Run("unreferenced schema newly read by a response gains a required property", func(t *testing.T) {
		doc := func(content, schema string) string {
			return contractDoc(`{"/api/v1/thing":{"get":{"responses":{"200":{"description":"ok"` + content + `}}}}},"components":{"schemas":{"Lone":` + schema + `}}`)
		}
		reads := `,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Lone"}}}`
		violations := contractViolations(t, doc("", `{"type":"object","properties":{"a":{"type":"string"}}}`),
			doc(reads, `{"type":"object","required":["n"],"properties":{"a":{"type":"string"},"n":{"type":"string"}}}`))
		if !hasContractViolation(violations) {
			t.Fatalf("the document-coordinate layer did not refuse it:\n%s", render(violations))
		}
	})

	// A ref the layer cannot attribute to a reader fails closed, with a reason
	// rather than a panic. (kin-openapi itself refuses anchor refs.)
	for name, doc := range map[string]string{
		"ref into paths": contractThing(`"parameters":[{"name":"p","in":"query","schema":{"type":"string"}},{"$ref":"#/paths/~1api~1v1~1thing/post/parameters/0"}],` + contractOK),
		"deep ref into a component parameter": contractDoc(`{"/api/v1/thing":{"get":{"responses":{"200":{"description":"ok","headers":{"X-P":{"schema":{"$ref":"#/components/parameters/P/schema"}}}}}}}},` +
			`"components":{"parameters":{"P":{"name":"p","in":"query","schema":{"type":"object","required":["a"],"properties":{"a":{"type":"string"}}}}}}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := api.CheckFreeze([]byte(doc), []byte(doc)); err == nil || !strings.Contains(err.Error(), "unsupported $ref") {
				t.Fatalf("want an unsupported $ref error, got %v", err)
			}
		})
	}

	// Refs under names that look like data keywords are still followed: a
	// `default` response and a property named `default` read their targets.
	t.Run("ref under a default response is followed", func(t *testing.T) {
		doc := func(required string) string {
			return contractDoc(`{"/api/v1/thing":{"post":{"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/S"}}}},` +
				`"responses":{"default":{"description":"ok","headers":{"X-S":{"schema":{"type":"object","properties":{"default":{"$ref":"#/components/schemas/S"}}}}}}}}}},` +
				`"components":{"schemas":{"S":{"type":"object",` + required + `"properties":{"a":{"type":"string"}}}}}`)
		}
		if violations := contractViolations(t, doc(`"required":["a"],`), doc("")); !hasContractViolation(violations) {
			t.Fatalf("the document-coordinate layer did not refuse it:\n%s", render(violations))
		}
	})
	t.Run("dangling or percent-encoded ref", func(t *testing.T) {
		doc := contractDoc(`{"/api/v1/thing":{"post":{"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/A%20B"}}}},` + contractOK + `}}},` +
			`"components":{"schemas":{"A B":{"type":"string"}}}`)
		if _, err := api.CheckFreeze([]byte(doc), []byte(doc)); err == nil || !strings.Contains(err.Error(), "unresolvable $ref") {
			t.Fatalf("want an unresolvable $ref error, got %v", err)
		}
	})
}

// Findings of the PR #1232 review, each pinned in the direction it was fixed.
func TestFreezeContractReviewFollowUps(t *testing.T) {
	const reqS = `"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/S"}}}}`
	const respS = `"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/S"}}}}}`
	s := func(required string) string {
		return `"S":{"type":"object",` + required + `"properties":{"a":{"type":"string"},"n":{"type":"string"}}}`
	}
	withComponents := func(paths, components string) string {
		return strings.TrimSuffix(contractDoc(paths), "}") + `,"components":{` + components + `}}`
	}
	permitted := func(t *testing.T, base, revised string) {
		t.Helper()
		if v := contractViolations(t, base, revised); hasContractViolation(v) {
			t.Fatalf("the document layer refused a permitted change:\n%s", render(v))
		}
	}
	refused := func(t *testing.T, base, revised string) {
		t.Helper()
		if v := contractViolations(t, base, revised); !hasContractViolation(v) {
			t.Fatalf("the document layer did not refuse it:\n%s", render(v))
		}
	}

	t.Run("first component added to a document without components", func(t *testing.T) {
		permitted(t, contractThing(contractOK), withComponents(`{"/api/v1/thing":{"post":{`+contractOK+`}}}`, `"schemas":{"Brand":{"type":"string"}}`))
	})
	t.Run("operationId added to an existing operation", func(t *testing.T) {
		permitted(t, contractThing(contractOK), contractThing(`"operationId":"makeThing",`+contractOK))
	})
	t.Run("parameter moved within a reusable callback", func(t *testing.T) {
		const p = `{"name":"p","in":"query","schema":{"type":"string"}}`
		doc := func(item string) string {
			return withComponents(`{"/api/v1/thing":{"post":{`+contractOK+`}}}`, `"callbacks":{"C":{"{$request.query.url}":`+item+`}}`)
		}
		permitted(t, doc(`{"parameters":[`+p+`],"post":{`+contractOK+`}}`), doc(`{"post":{"parameters":[`+p+`],`+contractOK+`}}`))
		refused(t, doc(`{"post":{`+contractOK+`}}`), doc(`{"post":{"parameters":[`+p+`],`+contractOK+`}}`))
	})
	t.Run("path item reached through a ref keeps its directions", func(t *testing.T) {
		doc := func(op, required string) string {
			return withComponents(`{"/api/v1/thing":{"$ref":"#/components/pathItems/P"}}`,
				`"pathItems":{"P":{"post":{`+op+`}}},"schemas":{`+s(required)+`}`)
		}
		permitted(t, doc(reqS+","+contractOK, `"required":["a"],`), doc(reqS+","+contractOK, ""))
		permitted(t, strings.Replace(doc(respS, ""), `,"n":{"type":"string"}`, "", 1), doc(respS, `"required":["n"],`))
		refused(t, doc(respS, `"required":["a"],`), doc(respS, ""))
	})
	t.Run("a property named not freezes nothing", func(t *testing.T) {
		doc := func(required string) string {
			return withComponents(`{"/api/v1/thing":{"post":{"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"not":{"$ref":"#/components/schemas/S"}}}}}},`+contractOK+`}}}`,
				`"schemas":{`+s(required)+`}`)
		}
		permitted(t, doc(`"required":["a"],`), doc(""))
	})
	t.Run("an unused request body still writes its schema", func(t *testing.T) {
		doc := func(required string) string {
			return withComponents(`{"/api/v1/thing":{"post":{`+respS+`}}}`,
				`"requestBodies":{"RB":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/S"}}}}},"schemas":{"S":{"type":"object",`+required+`"properties":{"a":{"type":"string"}}}}`)
		}
		withNew := strings.Replace(doc(`"required":["n"],`), `"properties":{"a":{"type":"string"}}`, `"properties":{"a":{"type":"string"},"n":{"type":"string"}}`, 1)
		refused(t, doc(""), withNew)
	})
	t.Run("contains is frozen under maxContains", func(t *testing.T) {
		body := func(extra, required string) string {
			return inPosition("request", `{"type":"array",`+extra+`"contains":{"type":"object",`+required+`"properties":{"a":{"type":"string"}}}}`)
		}
		permitted(t, body("", `"required":["a"],`), body("", ""))
		refused(t, body(`"maxContains":1,`, `"required":["a"],`), body(`"maxContains":1,`, ""))
	})
	t.Run("integers beyond float64 precision stay distinct", func(t *testing.T) {
		refused(t, inPosition("header", `{"type":"integer","enum":[9007199254740992]}`), inPosition("header", `{"type":"integer","enum":[9007199254740993]}`))
		yamlDoc := func(v string) string {
			return "openapi: 3.1.0\njsonSchemaDialect: https://spec.openapis.org/oas/3.1/dialect/base\ninfo: {title: f, version: \"1\"}\n" +
				"paths:\n  /api/v1/thing:\n    get:\n      responses:\n        \"200\":\n          description: ok\n" +
				"          headers:\n            X-N: {schema: {type: integer, enum: [" + v + "]}}\n"
		}
		refused(t, yamlDoc("9007199254740992"), yamlDoc("9007199254740993"))
	})
	t.Run("unsupported refs error wherever they hide", func(t *testing.T) {
		for name, doc := range map[string]string{
			"callback named headers": contractThing(`"callbacks":{"real":{"{$request.query.url}":{"post":{` + contractOK + `}}},` +
				`"headers":{"$ref":"#/paths/~1api~1v1~1thing/post/callbacks/real"}},` + contractOK),
			"non-map components extension": withComponents(`{"/api/v1/thing":{"post":{`+contractOK+`}}}`, `"x-list":[{"$ref":"#/paths/x"}]`),
			"deep path item ref":           withComponents(`{"/api/v1/thing":{"$ref":"#/components/pathItems/P/post"}}`, `"pathItems":{"P":{"post":{`+contractOK+`}}}`),
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := api.CheckFreeze([]byte(doc), []byte(doc)); err == nil || !strings.Contains(err.Error(), "unsupported $ref") {
					t.Fatalf("want an unsupported $ref error, got %v", err)
				}
			})
		}
	})
	t.Run("a request body inside a callback is read by clients", func(t *testing.T) {
		doc := func(required string) string {
			return withComponents(`{"/api/v1/thing":{"post":{"callbacks":{"C":{"{$request.query.url}":{"post":{"requestBody":{"$ref":"#/components/requestBodies/RB"},`+contractOK+`}}}},`+contractOK+`}}}`,
				`"requestBodies":{"RB":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/S"}}}}},"schemas":{`+s(required)+`}`)
		}
		refused(t, doc(`"required":["a"],`), doc(""))
	})
	t.Run("reusable wrappers with inline schemas take their callback direction", func(t *testing.T) {
		const inline = `"content":{"application/json":{"schema":{"type":"object",%s"properties":{"a":{"type":"string"}%s}}}}`
		// use is a whole operation body.
		doc := func(callback bool, use, components string) string {
			op := use
			if callback {
				op = `"callbacks":{"C":{"{$request.query.url}":{"post":{` + use + `}}}},` + contractOK
			}
			return withComponents(`{"/api/v1/thing":{"post":{`+op+`}}}`, components)
		}
		rb := func(required string) string {
			return `"requestBodies":{"RB":{` + fmt.Sprintf(inline, required, "") + `}}`
		}
		const useRB = `"requestBody":{"$ref":"#/components/requestBodies/RB"},` + contractOK
		permitted(t, doc(false, useRB, rb(`"required":["a"],`)), doc(false, useRB, rb("")))
		refused(t, doc(true, useRB, rb(`"required":["a"],`)), doc(true, useRB, rb("")))

		resp := func(required, extra string) string {
			return `"responses":{"R":{"description":"ok",` + fmt.Sprintf(inline, required, extra) + `}}`
		}
		const useR = `"responses":{"201":{"$ref":"#/components/responses/R"}}`
		withN := func(callback bool) (string, string) {
			return doc(callback, useR, resp("", "")), doc(callback, useR, resp(`"required":["n"],`, `,"n":{"type":"string"}`))
		}
		base, revised := withN(false)
		permitted(t, base, revised)
		base, revised = withN(true)
		refused(t, base, revised)

		param := func(required string) string {
			return `"parameters":{"P":{"name":"p","in":"query",` + required + `"schema":{"type":"string"}}}`
		}
		const useP = `"parameters":[{"$ref":"#/components/parameters/P"}],` + contractOK
		permitted(t, doc(false, useP, param(`"required":true,`)), doc(false, useP, param("")))
		refused(t, doc(true, useP, param(`"required":true,`)), doc(true, useP, param("")))
	})
	t.Run("a parameter declared twice in one list is an error", func(t *testing.T) {
		doc := contractThing(`"parameters":[{"name":"p","in":"query","schema":{"type":"string"}},{"name":"p","in":"query","required":true,"schema":{"type":"string"}}],` + contractOK)
		if _, err := api.CheckFreeze([]byte(doc), []byte(doc)); err == nil || !strings.Contains(err.Error(), "declared twice") {
			t.Fatalf("want a duplicate parameter error, got %v", err)
		}
	})
}
