package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/oasdiff/yaml"
)

// The document-coordinate freeze layer (#1217).
//
// oasdiff plus the allowlist and freezeRemovals each refuse what they know to
// look for; enumerating the blind spots of a third-party diff has not
// converged. This layer is independent of both and fails closed: it compares
// the two RAW documents (not kin-openapi's model, where a keyword the model
// does not carry would silently drop out) and refuses every difference except
// a short, explicit list of additions and prose edits.
//
// The walk is typed. At each node it knows whether a map's keys are KEYWORDS
// or NAMES (paths, properties, headers, scopes...), because prose exemptions
// apply to keywords only: a property literally named `description` is a name
// and stays protected. `$ref` values are compared as strings and never
// followed; the target is compared once, in the components walk.

// MLDSA65EnumLocations is the same amendment as MLDSA65EnumGrowth (the
// api-cli-surface ADR banner of 2026-11-04), in document coordinates: the
// only enums that may grow, and only by exactly `mldsa65`. Adding to it is an
// ADR amendment, not a code edit.
var MLDSA65EnumLocations = []string{
	"/components/schemas/PkiIssuerCreateRequest/properties/key_algorithm/enum",
	"/components/schemas/PkiIssuerRotateRequest/properties/key_algorithm/enum",
	"/components/schemas/PkiPolicy/properties/key_algorithms/items/enum",
	"/components/schemas/CertificateIssueRequest/properties/key_algorithm/enum",
	"/components/schemas/TransitAlgorithm/enum",
}

// contractCtx records whether a schema is read by clients (response), written
// by them (request), or both. It decides which `required` edits are additive.
type contractCtx uint8

const (
	ctxRequest contractCtx = 1 << iota
	ctxResponse
	// ctxFrozen marks a schema under `not`, `if` or `oneOf`, where making a
	// subschema wider or narrower can break either direction: requiredness,
	// open enums and element schemas may not change there.
	ctxFrozen
	// ctxInverted marks a schema under `not` or `if`, whose meaning is
	// inverted, so even a new property is not an addition.
	ctxInverted
	ctxBoth = ctxRequest | ctxResponse
)

// contractFrozen are the keywords whose subschemas are compared frozen.
var contractFrozen = map[string]contractCtx{"not": ctxFrozen | ctxInverted, "if": ctxFrozen | ctxInverted, "oneOf": ctxFrozen}

// contractNameMaps are keywords whose value maps names to subschemas or
// objects, so the reachability walk does not read those names as keywords.
var contractNameMaps = map[string]bool{
	"properties": true, "patternProperties": true, "$defs": true, "dependentSchemas": true,
	"encoding": true, "headers": true,
}

// contractProse are keywords whose edits change no wire behaviour.
var contractProse = map[string]bool{
	"description": true, "summary": true, "title": true, "example": true,
	"examples": true, "externalDocs": true, "$comment": true,
}

var contractMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

type loc []string

func (l loc) with(s string) loc { return append(l[:len(l):len(l)], s) }

func (l loc) String() string {
	var b strings.Builder
	for _, s := range l {
		b.WriteString("/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(s))
	}
	return b.String()
}

// rule compares one keyword or name; hb/hr say which side carries it.
type rule func(l loc, b, r any, hb, hr bool)

type contractWalker struct {
	base, revised map[string]any
	ctxs          map[string]contractCtx
	out           []Violation
	err           error
}

type nameOpts struct {
	add   bool // a new name is permitted
	lower bool // names are case-insensitive (HTTP headers)
	ext   bool // `x-` names are extensions, not entries: strict
}

func freezeContract(base, revised []byte) ([]Violation, error) {
	b, err := parseContractDocument(base)
	if err != nil {
		return nil, fmt.Errorf("base document: %w", err)
	}
	r, err := parseContractDocument(revised)
	if err != nil {
		return nil, fmt.Errorf("revised document: %w", err)
	}
	w := &contractWalker{base: b, revised: r, ctxs: map[string]contractCtx{}}
	w.reach(b)
	w.reach(r)
	w.document(nil, b, r)
	if w.err != nil {
		return nil, w.err
	}
	return w.out, nil
}

// parseContractDocument decodes exactly as kin-openapi's loader does (JSON
// first, then oasdiff/yaml), so `off` or `yes` stays the string every other
// consumer of the spec reads, not a YAML 1.1 boolean. Numbers stay
// json.Number, so integers beyond float64 precision remain distinct (in YAML,
// up to the uint64 range; yaml.v3 decodes larger ones as float64).
func parseContractDocument(raw []byte) (map[string]any, error) {
	useNumber := func(d *json.Decoder) *json.Decoder { d.UseNumber(); return d }
	var v any
	dec := useNumber(json.NewDecoder(bytes.NewReader(raw)))
	err := dec.Decode(&v)
	if err == nil {
		if _, end := dec.Token(); end != io.EOF {
			err = errors.New("trailing data after the JSON document")
		}
	}
	if err != nil {
		v = nil
		if _, err := yaml.Unmarshal(raw, &v, yaml.DecodeOpts{DisableTimestamps: true}, useNumber); err != nil {
			return nil, err
		}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("the document is not a mapping")
	}
	return m, nil
}

func (w *contractWalker) report(id string, l loc, detail, what string) {
	v := Violation{ID: id, Level: "ERR", Text: fmt.Sprintf("%s%s %s the frozen API", l, detail, what)}
	if len(l) >= 2 && l[0] == "paths" {
		v.Path = l[1]
		if len(l) >= 3 && slices.Contains(contractMethods, l[2]) {
			v.Operation = strings.ToUpper(l[2])
		}
	}
	w.out = append(w.out, v)
}

func (w *contractWalker) removed(l loc, detail string) {
	w.report("hikyo-contract-removed", l, detail, "removed from")
}
func (w *contractWalker) added(l loc, detail string) {
	w.report("hikyo-contract-added", l, detail, "added to")
}
func (w *contractWalker) changed(l loc, detail string) {
	w.report("hikyo-contract-changed", l, detail, "changed in")
}

// strict demands deep equality.
func (w *contractWalker) strict(l loc, b, r any, hb, hr bool) {
	switch {
	case hb && !hr:
		w.removed(l, "")
	case !hb && hr:
		w.added(l, "")
	case !reflect.DeepEqual(b, r):
		w.changed(l, "")
	}
}

func noop(loc, any, any, bool, bool) {}

// keywords walks a map whose keys are keywords. dflt handles keys without a
// rule (nil means strict).
func (w *contractWalker) keywords(l loc, b, r map[string]any, rules map[string]rule, dflt rule) {
	for _, k := range unionKeys(b, r) {
		if contractProse[k] {
			continue
		}
		bv, hb := b[k]
		rv, hr := r[k]
		kl := l.with(k)
		switch {
		case k == "deprecated":
			bt, ok1 := bv.(bool)
			rt, ok2 := rv.(bool)
			if (hb && !ok1) || (hr && !ok2) {
				w.strict(kl, bv, rv, hb, hr)
			} else if bt && !rt {
				w.changed(kl, "")
			}
		case rules[k] != nil:
			rules[k](kl, bv, rv, hb, hr)
		case dflt != nil:
			dflt(kl, bv, rv, hb, hr)
		default:
			w.strict(kl, bv, rv, hb, hr)
		}
	}
}

// obj recurses with fn when both sides are maps, and is strict otherwise.
func (w *contractWalker) obj(fn func(l loc, b, r map[string]any)) rule {
	return func(l loc, b, r any, hb, hr bool) {
		bm, ok1 := b.(map[string]any)
		rm, ok2 := r.(map[string]any)
		if hb && hr && ok1 && ok2 {
			fn(l, bm, rm)
			return
		}
		w.strict(l, b, r, hb, hr)
	}
}

// names walks a map whose keys are names; removal is always refused.
func (w *contractWalker) names(o nameOpts, each rule) rule {
	return func(l loc, b, r any, hb, hr bool) {
		bm, rm := map[string]any{}, map[string]any{}
		var ok1, ok2 bool = true, true
		if hb {
			bm, ok1 = b.(map[string]any)
		}
		if hr {
			rm, ok2 = r.(map[string]any)
		}
		if o.lower && ok1 && ok2 {
			bm, ok1 = lowerKeys(bm)
			rm, ok2 = lowerKeys(rm)
		}
		if !ok1 || !ok2 {
			w.strict(l, b, r, hb, hr)
			return
		}
		for _, k := range unionKeys(bm, rm) {
			bv, inB := bm[k]
			rv, inR := rm[k]
			kl := l.with(k)
			switch {
			case o.ext && strings.HasPrefix(k, "x-"):
				w.strict(kl, bv, rv, inB, inR)
			case inB && !inR:
				w.removed(kl, "")
			case !inB && inR:
				if !o.add {
					w.added(kl, "")
				}
			default:
				each(kl, bv, rv, true, true)
			}
		}
	}
}

// list compares positional lists of equal length.
func (w *contractWalker) list(each rule) rule {
	return func(l loc, b, r any, hb, hr bool) {
		ba, ok1 := b.([]any)
		ra, ok2 := r.([]any)
		if !hb || !hr || !ok1 || !ok2 || len(ba) != len(ra) {
			w.strict(l, b, r, hb, hr)
			return
		}
		for i := range ba {
			each(l.with(strconv.Itoa(i)), ba[i], ra[i], true, true)
		}
	}
}

func unionKeys(a, b map[string]any) []string {
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

// lowerKeys fails on a collision, so two spellings of one header cannot hide.
func lowerKeys(m map[string]any) (map[string]any, bool) {
	out := make(map[string]any, len(m))
	for k, v := range m {
		lk := strings.ToLower(k)
		if _, dup := out[lk]; dup {
			return nil, false
		}
		out[lk] = v
	}
	return out, true
}

func has(set []any, v any) bool {
	return slices.ContainsFunc(set, func(x any) bool { return reflect.DeepEqual(x, v) })
}

// missing returns the members of a that b lacks.
func missing(a, b []any) []any {
	var out []any
	for _, v := range a {
		if !has(b, v) {
			out = append(out, v)
		}
	}
	return out
}

// strs reads a list of strings; an absent value is the empty list.
func strs(v any, present bool) ([]string, bool) {
	if !present {
		return nil, true
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, ok := x.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func (w *contractWalker) document(l loc, b, r map[string]any) {
	w.keywords(l, b, r, map[string]rule{
		"info":    noop, // documentation metadata
		"servers": w.servers(),
		"paths":   w.names(nameOpts{add: true, ext: true}, w.obj(func(l loc, b, r map[string]any) { w.pathItem(l, b, r) })),
		"tags":    w.tags(),
		"components": func(l loc, b, r any, hb, hr bool) {
			if !hb && hr {
				b, hb = map[string]any{}, true // the first component is an addition
			}
			w.obj(w.components)(l, b, r, hb, hr)
		},
	}, nil)
}

// tags are keyed by name: new tags are additive, removal is not.
func (w *contractWalker) tags() rule {
	return func(l loc, b, r any, hb, hr bool) {
		bm, ok1 := tagsByName(b, hb)
		rm, ok2 := tagsByName(r, hr)
		if !ok1 || !ok2 {
			w.strict(l, b, r, hb, hr)
			return
		}
		w.names(nameOpts{add: true}, w.obj(func(l loc, b, r map[string]any) {
			w.keywords(l, b, r, nil, nil)
		}))(l, bm, rm, true, true)
	}
}

func tagsByName(v any, present bool) (map[string]any, bool) {
	out := map[string]any{}
	if !present {
		return out, true
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	for _, t := range arr {
		m, ok := t.(map[string]any)
		name, isStr := m["name"].(string)
		if _, dup := out[name]; !ok || !isStr || dup {
			return nil, false
		}
		out[name] = m
	}
	return out, true
}

func (w *contractWalker) servers() rule {
	free := w.obj(func(l loc, b, r map[string]any) { w.keywords(l, b, r, nil, nil) })
	server := w.obj(func(l loc, b, r map[string]any) {
		w.keywords(l, b, r, map[string]rule{"variables": w.names(nameOpts{}, free)}, nil)
	})
	return w.list(server)
}

func (w *contractWalker) pathItem(l loc, b, r map[string]any) {
	rules := map[string]rule{"parameters": noop, "servers": w.servers()}
	for _, m := range contractMethods {
		rules[m] = func(l loc, bv, rv any, hb, hr bool) {
			if !hb {
				return // a new operation is additive
			}
			bm, ok1 := bv.(map[string]any)
			rm, ok2 := rv.(map[string]any)
			if !hr || !ok1 || !ok2 {
				w.strict(l, bv, rv, hb, hr)
				return
			}
			w.operation(l, bm, rm, b["parameters"], r["parameters"])
		}
	}
	w.keywords(l, b, r, rules, nil)
}

func (w *contractWalker) operation(l loc, b, r map[string]any, bp, rp any) {
	w.parameters(l, b, r, bp, rp)
	w.keywords(l, b, r, map[string]rule{
		"parameters":  noop,
		"requestBody": w.obj(func(l loc, b, r map[string]any) { w.requestBody(l, b, r) }),
		"responses":   w.names(nameOpts{add: true, ext: true}, w.obj(func(l loc, b, r map[string]any) { w.response(l, b, r) })),
		"callbacks":   w.names(nameOpts{add: true}, w.callback),
		"servers":     w.servers(),
		"operationId": func(l loc, b, r any, hb, hr bool) {
			if hb || !hr {
				w.strict(l, b, r, hb, hr)
			}
		},
		"tags": func(l loc, b, r any, hb, hr bool) {
			bs, ok1 := strs(b, hb)
			rs, ok2 := strs(r, hr)
			if !ok1 || !ok2 {
				w.strict(l, b, r, hb, hr)
				return
			}
			for _, t := range bs {
				if !slices.Contains(rs, t) {
					w.removed(l, fmt.Sprintf(" tag %q", t))
				}
			}
		},
	}, nil)
}

// callback compares an existing callback strictly, after normalization.
func (w *contractWalker) callback(l loc, b, r any, _, _ bool) {
	w.strict(l, w.normalizeCallback(w.base, b), w.normalizeCallback(w.revised, r), true, true)
}

// normalizeCallback rewrites each path item of a callback to its effective
// parameters, so moving one between path and operation stays neutral inside a
// callback. Everything else in a callback is compared strictly.
func (w *contractWalker) normalizeCallback(root map[string]any, callback any) any {
	cb, ok := callback.(map[string]any)
	if !ok {
		return callback
	}
	out := make(map[string]any, len(cb))
	for expression, item := range cb {
		im, ok := item.(map[string]any)
		if !ok {
			out[expression] = item
			continue
		}
		cp := maps.Clone(im)
		delete(cp, "parameters")
		for _, method := range contractMethods {
			om, ok := im[method].(map[string]any)
			if !ok {
				continue
			}
			op := maps.Clone(om)
			effective := w.effective(root, im["parameters"], om["parameters"])
			keys := slices.SortedFunc(maps.Keys(effective), func(a, b paramKey) int {
				return strings.Compare(a.in+"\x00"+a.name, b.in+"\x00"+b.name)
			})
			delete(op, "parameters")
			if len(keys) > 0 {
				list := make([]any, len(keys))
				for i, k := range keys {
					list[i] = effective[k].node
				}
				op["parameters"] = list
			}
			cp[method] = op
		}
		out[expression] = cp
	}
	return out
}

type paramKey struct{ in, name string }

type effParam struct {
	node     any
	required bool
}

// effective is the operation's parameter set: path-level parameters overridden
// by operation-level ones with the same key. A $ref is resolved only to find
// the key (HTTP header names are case-insensitive).
func (w *contractWalker) effective(root map[string]any, lists ...any) map[paramKey]effParam {
	out := map[paramKey]effParam{}
	for _, list := range lists {
		if list == nil {
			continue
		}
		arr, ok := list.([]any)
		if !ok {
			w.fail("parameters is not a list")
			return out
		}
		inList := map[paramKey]bool{}
		for _, p := range arr {
			res, err := resolveContractRef(root, p)
			m, isMap := res.(map[string]any)
			in, ok1 := m["in"].(string)
			name, ok2 := m["name"].(string)
			if err != nil || !isMap || !ok1 || !ok2 {
				w.fail(fmt.Sprintf("a parameter has no resolvable name and location (%v)", err))
				return out
			}
			if in == "header" {
				name = strings.ToLower(name)
			}
			key := paramKey{in, name}
			if inList[key] {
				w.fail(fmt.Sprintf("parameter %s %q is declared twice in one list", in, name))
				return out
			}
			inList[key] = true
			required, _ := m["required"].(bool)
			out[key] = effParam{p, required}
		}
	}
	return out
}

func (w *contractWalker) fail(msg string) {
	if w.err == nil {
		w.err = fmt.Errorf("freeze contract: %s", msg)
	}
}

func (w *contractWalker) parameters(l loc, b, r map[string]any, bp, rp any) {
	be := w.effective(w.base, bp, b["parameters"])
	re := w.effective(w.revised, rp, r["parameters"])
	keys := make([]paramKey, 0, len(be)+len(re))
	for k := range be {
		keys = append(keys, k)
	}
	for k := range re {
		if _, ok := be[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b paramKey) int {
		return strings.Compare(a.in+"\x00"+a.name, b.in+"\x00"+b.name)
	})
	param := w.obj(w.parameter)
	for _, k := range keys {
		kl := l.with("parameters").with(k.in).with(k.name)
		bv, inB := be[k]
		rv, inR := re[k]
		switch {
		case inB && !inR:
			w.removed(kl, "")
		case !inB && inR:
			if rv.required {
				w.added(kl, " required parameter")
			}
		default:
			bm, ok1 := bv.node.(map[string]any)
			rm, ok2 := rv.node.(map[string]any)
			_, bref := bm["$ref"]
			_, rref := rm["$ref"]
			if !ok1 || !ok2 || bref || rref {
				w.strict(kl, bv.node, rv.node, true, true)
			} else {
				param(kl, bv.node, rv.node, true, true)
			}
		}
	}
}

func (w *contractWalker) requestBody(l loc, b, r map[string]any) {
	w.keywords(l, b, r, map[string]rule{"content": w.content(ctxRequest, false)}, nil)
}

func (w *contractWalker) content(c contractCtx, add bool) rule {
	return w.names(nameOpts{add: add}, w.obj(func(l loc, b, r map[string]any) { w.mediaType(l, b, r, c) }))
}

func (w *contractWalker) mediaType(l loc, b, r map[string]any, c contractCtx) {
	w.keywords(l, b, r, map[string]rule{
		"schema":   w.schemaRule(c),
		"encoding": w.names(nameOpts{}, w.obj(func(l loc, b, r map[string]any) { w.encoding(l, b, r, c) })),
	}, nil)
}

// encoding headers are written by whoever writes the body: the client in a
// request, the server in a response.
func (w *contractWalker) encoding(l loc, b, r map[string]any, c contractCtx) {
	w.keywords(l, b, r, map[string]rule{
		"headers": w.names(nameOpts{lower: true}, w.obj(func(l loc, b, r map[string]any) { w.header(l, b, r, c) })),
	}, nil)
}

func (w *contractWalker) response(l loc, b, r map[string]any) {
	link := w.obj(func(l loc, b, r map[string]any) { w.keywords(l, b, r, nil, nil) })
	w.keywords(l, b, r, map[string]rule{
		"headers": w.names(nameOpts{add: true, lower: true}, w.obj(func(l loc, b, r map[string]any) { w.header(l, b, r, ctxResponse) })),
		"content": w.content(ctxResponse, true),
		"links":   w.names(nameOpts{add: true}, link),
	}, nil)
}

func (w *contractWalker) header(l loc, b, r map[string]any, c contractCtx) {
	w.keywords(l, b, r, map[string]rule{
		"schema":  w.schemaRule(c),
		"content": w.content(c, false),
	}, nil)
}

func (w *contractWalker) parameter(l loc, b, r map[string]any) {
	w.keywords(l, b, r, map[string]rule{
		"schema":  w.schemaRule(ctxRequest),
		"content": w.content(ctxRequest, false),
		// Only true -> false/absent is additive.
		"required": func(l loc, b, r any, hb, hr bool) {
			bt, ok1 := b.(bool)
			rt, ok2 := r.(bool)
			if (hb && !ok1) || (hr && !ok2) {
				w.strict(l, b, r, hb, hr)
			} else if !bt && rt {
				w.changed(l, "")
			}
		},
	}, nil)
}

func (w *contractWalker) schemaRule(c contractCtx) rule {
	return w.obj(func(l loc, b, r map[string]any) { w.schema(l, b, r, c) })
}

func (w *contractWalker) schema(l loc, b, r map[string]any, c contractCtx) {
	sch := w.schemaRule(c)
	frozen := c&ctxFrozen != 0
	rules := map[string]rule{
		"properties":        w.names(nameOpts{add: c&ctxInverted == 0}, sch),
		"required":          w.required(c, b, r),
		"enum":              w.enum,
		"x-extensible-enum": w.openEnum,
		"patternProperties": w.names(nameOpts{}, sch),
		"dependentSchemas":  w.names(nameOpts{}, sch),
		"$defs":             w.names(nameOpts{}, sch),
	}
	// A schema that only describes responses may gain a description of its
	// elements or extra members: a client reads, it never has to conform.
	describes := func(l loc, b, r any, hb, hr bool) {
		if _, isSchema := r.(map[string]any); !hb && hr && isSchema && c&(ctxRequest|ctxFrozen) == 0 {
			return
		}
		sch(l, b, r, hb, hr)
	}
	rules["items"], rules["additionalProperties"] = describes, describes
	// With maxContains, widening the predicate can push a valid array over the
	// limit, so neither direction is safe.
	rules["contains"] = sch
	_, maxB := b["maxContains"]
	_, maxR := r["maxContains"]
	if maxB || maxR {
		rules["contains"] = w.schemaRule(c | ctxFrozen)
	}
	for _, k := range []string{"then", "else",
		"propertyNames", "unevaluatedItems", "unevaluatedProperties", "contentSchema"} {
		rules[k] = sch
	}
	for _, k := range []string{"allOf", "anyOf", "prefixItems"} {
		rules[k] = w.list(sch)
	}
	rules["not"] = w.schemaRule(c | contractFrozen["not"])
	rules["if"] = w.schemaRule(c | contractFrozen["if"])
	rules["oneOf"] = w.list(w.schemaRule(c | contractFrozen["oneOf"]))
	if frozen {
		rules["x-extensible-enum"] = w.strict
	}
	w.keywords(l, b, r, rules, nil)
}

// required has set semantics. Growth is additive only in a schema no client
// writes, for a property this revision introduces; shrinking only in a schema
// no client reads.
func (w *contractWalker) required(c contractCtx, b, r map[string]any) rule {
	props := func(m map[string]any) map[string]any {
		p, _ := m["properties"].(map[string]any)
		return p
	}
	return func(l loc, bv, rv any, hb, hr bool) {
		bs, ok1 := strs(bv, hb)
		rs, ok2 := strs(rv, hr)
		if !ok1 || !ok2 {
			w.strict(l, bv, rv, hb, hr)
			return
		}
		for _, n := range rs {
			_, wasProp := props(b)[n]
			_, isProp := props(r)[n]
			if !slices.Contains(bs, n) && (c&(ctxRequest|ctxFrozen) != 0 || wasProp || !isProp) {
				w.added(l, fmt.Sprintf(" entry %q", n))
			}
		}
		for _, n := range bs {
			if !slices.Contains(rs, n) && c&(ctxResponse|ctxFrozen) != 0 {
				w.removed(l, fmt.Sprintf(" entry %q", n))
			}
		}
	}
}

// enum has set semantics and never changes, except the mldsa65 amendment.
func (w *contractWalker) enum(l loc, b, r any, hb, hr bool) {
	ba, ok1 := b.([]any)
	ra, ok2 := r.([]any)
	if !hb || !hr || !ok1 || !ok2 {
		w.strict(l, b, r, hb, hr)
		return
	}
	gone, grown := missing(ba, ra), missing(ra, ba)
	for _, v := range gone {
		w.removed(l, fmt.Sprintf(" value %v", v))
	}
	if len(gone) == 0 && len(grown) == 1 && grown[0] == mldsa65 && slices.Contains(MLDSA65EnumLocations, l.String()) {
		return
	}
	for _, v := range grown {
		w.added(l, fmt.Sprintf(" value %v", v))
	}
}

// openEnum may grow: that is what declaring an enum open means.
func (w *contractWalker) openEnum(l loc, b, r any, hb, hr bool) {
	ba, ok1 := b.([]any)
	ra, ok2 := r.([]any)
	if !hb || !hr || !ok1 || !ok2 {
		w.strict(l, b, r, hb, hr)
		return
	}
	for _, v := range missing(ba, ra) {
		w.removed(l, fmt.Sprintf(" value %v", v))
	}
}

// ctx is how a component is reached. One nothing references is assumed to be
// read and written.
func (w *contractWalker) ctx(section string, l loc) contractCtx {
	c := w.ctxs[section+"/"+l[len(l)-1]]
	if c&ctxBoth == 0 {
		c |= ctxBoth
	}
	return c
}

func (w *contractWalker) components(l loc, b, r map[string]any) {
	section := func(each rule) rule { return w.names(nameOpts{add: true}, each) }
	free := w.obj(func(l loc, b, r map[string]any) { w.keywords(l, b, r, nil, nil) })
	w.keywords(l, b, r, map[string]rule{
		"schemas": section(func(l loc, b, r any, hb, hr bool) {
			w.schemaRule(w.ctx("schemas", l))(l, b, r, hb, hr)
		}),
		"responses":       section(w.obj(func(l loc, b, r map[string]any) { w.response(l, b, r) })),
		"parameters":      section(w.obj(w.parameter)),
		"requestBodies":   section(w.obj(func(l loc, b, r map[string]any) { w.requestBody(l, b, r) })),
		"headers":         section(w.obj(func(l loc, b, r map[string]any) { w.header(l, b, r, w.ctx("headers", l)) })),
		"links":           section(free),
		"callbacks":       section(w.callback),
		"pathItems":       section(w.obj(func(l loc, b, r map[string]any) { w.pathItem(l, b, r) })),
		"securitySchemes": section(w.obj(w.securityScheme)),
	}, nil)
}

// securityScheme: scope descriptions are prose, scope names are not.
func (w *contractWalker) securityScheme(l loc, b, r map[string]any) {
	flow := w.obj(func(l loc, b, r map[string]any) {
		w.keywords(l, b, r, map[string]rule{"scopes": w.names(nameOpts{}, noop)}, nil)
	})
	w.keywords(l, b, r, map[string]rule{
		"flows": w.obj(func(l loc, b, r map[string]any) { w.keywords(l, b, r, nil, flow) }),
	}, nil)
}

// resolveContractRef follows local JSON-pointer $refs to a non-ref node.
func resolveContractRef(root map[string]any, n any) (any, error) {
	for range 32 {
		m, ok := n.(map[string]any)
		ref, isRef := m["$ref"].(string)
		if !ok || !isRef {
			return n, nil
		}
		parts, ok := strings.CutPrefix(ref, "#/")
		if !ok {
			return nil, fmt.Errorf("unsupported $ref %q", ref)
		}
		n = root
		for _, p := range strings.Split(parts, "/") {
			parent, isMap := n.(map[string]any)
			next, found := parent[unescapePointer(p)]
			if !isMap || !found {
				return nil, fmt.Errorf("unresolvable $ref %q", ref)
			}
			n = next
		}
	}
	return nil, fmt.Errorf("$ref chain too deep")
}

func unescapePointer(s string) string { return strings.NewReplacer("~1", "/", "~0", "~").Replace(s) }

// componentRef splits `#/components/<section>/<name>[/...]`. Only such refs
// are supported, and a deep one only into schemas and headers, whose
// comparison takes the reader's context. Anything else (a ref into `paths`,
// into a parameter's schema, an anchor) would let one position read another's
// schema without that reader's rules reaching it.
func componentRef(ref string) (section, name string, ok bool) {
	rest, ok := strings.CutPrefix(ref, "#/components/")
	section, rest, ok2 := strings.Cut(rest, "/")
	name, deep, _ := strings.Cut(rest, "/")
	if deep != "" && section != "schemas" && section != "headers" {
		return "", "", false
	}
	return section, unescapePointer(name), ok && ok2 && name != ""
}

// reach records, per component schema and header, whether requests,
// responses or both reach it in this document, following every $ref
// transitively; a deep ref marks the whole component. A component this
// document does not reach counts as read and written, before the union with
// the other document, so referencing a formerly unreferenced component from
// one side cannot relax it. A path item reached through `$ref` keeps the
// directions of an inline one, and every component reaches what it references
// in its own direction, used or not. Subschemas of `not`, `if` and `oneOf`
// carry ctxFrozen. Any other kind of $ref anywhere in the document is an error.
func (w *contractWalker) reach(root map[string]any) {
	seen := map[string]bool{}
	ctxs := map[string]contractCtx{}
	components, _ := root["components"].(map[string]any)
	var visit func(n any, c contractCtx)
	follow := func(ref string, c contractCtx) {
		section, name, ok := componentRef(ref)
		if !ok {
			w.fail(fmt.Sprintf("unsupported $ref %q: only #/components/<section>/<name> refs are compared", ref))
			return
		}
		switch {
		case c&ctxBoth == ctxBoth:
			// Inside a callback the server writes the request and reads the
			// response, so a wrapper there stays read and written.
		case section == "parameters" || section == "requestBodies":
			c = ctxRequest | c&(ctxFrozen|ctxInverted)
		case section == "responses":
			c = ctxResponse | c&(ctxFrozen|ctxInverted)
		case section == "schemas" || section == "headers":
			// Read by whoever reads the referrer.
		default:
			c = ctxBoth | c&(ctxFrozen|ctxInverted)
		}
		key := fmt.Sprintf("%s/%s/%d", section, name, c)
		if seen[key] {
			return
		}
		seen[key] = true
		ctxs[section+"/"+name] |= c
		sec, _ := components[section].(map[string]any)
		target, found := sec[name]
		if !found {
			w.fail(fmt.Sprintf("unresolvable $ref %q", ref))
			return
		}
		visit(target, c)
	}
	visit = func(n any, c contractCtx) {
		switch v := n.(type) {
		case map[string]any:
			for _, k := range slices.Sorted(maps.Keys(v)) {
				// Every `$ref` key counts, even inside example data: telling data
				// from schema here would need the typed walk, and an error is the
				// fail-closed side of that ambiguity.
				x := v[k]
				if s, ok := x.(string); ok && k == "$ref" {
					follow(s, c)
				} else if names, ok := x.(map[string]any); ok && contractNameMaps[k] && names["$ref"] == nil {
					// Keys here are names: a property called `not` freezes nothing.
					for _, n := range slices.Sorted(maps.Keys(names)) {
						visit(names[n], c)
					}
				} else if f, ok := contractFrozen[k]; ok {
					visit(x, c|f)
				} else {
					visit(x, c)
				}
			}
		case []any:
			for _, x := range v {
				visit(x, c)
			}
		}
	}
	var pathItem func(m map[string]any)
	// pathItemRef walks a referenced path item with the same directions as an
	// inline one.
	pathItemRef := func(ref string) {
		_, name, ok := componentRef(ref)
		if !ok || strings.Contains(strings.TrimPrefix(ref, "#/components/pathItems/"), "/") {
			w.fail(fmt.Sprintf("unsupported $ref %q: only #/components/<section>/<name> refs are compared", ref))
			return
		}
		if seen["pathItems/"+name] {
			return
		}
		seen["pathItems/"+name] = true
		sec, _ := components["pathItems"].(map[string]any)
		target, found := sec[name]
		if !found {
			w.fail(fmt.Sprintf("unresolvable $ref %q", ref))
			return
		}
		m, _ := target.(map[string]any)
		pathItem(m)
	}
	operation := func(op map[string]any) {
		for _, k := range slices.Sorted(maps.Keys(op)) {
			v := op[k]
			switch k {
			case "parameters", "requestBody":
				visit(v, ctxRequest)
			case "responses":
				visit(v, ctxResponse)
			case "callbacks":
				visit(v, ctxBoth)
			}
		}
	}
	pathItem = func(m map[string]any) {
		for _, k := range slices.Sorted(maps.Keys(m)) {
			v := m[k]
			ref, isRef := v.(string)
			switch {
			case k == "parameters":
				visit(v, ctxRequest)
			case k == "$ref" && isRef && strings.HasPrefix(ref, "#/components/pathItems/"):
				pathItemRef(ref)
			case k == "$ref":
				visit(map[string]any{"$ref": v}, ctxBoth)
			case slices.Contains(contractMethods, k):
				op, _ := v.(map[string]any)
				operation(op)
			}
		}
	}
	paths, _ := root["paths"].(map[string]any)
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		m, _ := paths[path].(map[string]any)
		pathItem(m)
	}
	for _, section := range []string{"schemas", "headers"} {
		sec, _ := components[section].(map[string]any)
		for _, name := range slices.Sorted(maps.Keys(sec)) {
			if ctxs[section+"/"+name]&ctxBoth == 0 {
				follow("#/components/"+section+"/"+strings.NewReplacer("~", "~0", "/", "~1").Replace(name), ctxBoth)
			}
		}
	}
	// Every other component reaches its schemas in its own direction, used or
	// not (an unreferenced component is frozen too); this also validates refs.
	for _, section := range slices.Sorted(maps.Keys(components)) {
		sec, isMap := components[section].(map[string]any)
		if !isMap {
			visit(components[section], 0)
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(sec)) {
			ref := "#/components/" + section + "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
			if section == "pathItems" {
				pathItemRef(ref)
			} else {
				follow(ref, 0)
			}
		}
	}
	for k, c := range ctxs {
		w.ctxs[k] |= c
	}
}
