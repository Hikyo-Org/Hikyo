package api

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oasdiff/oasdiff/checker"
	"github.com/oasdiff/oasdiff/diff"
	"github.com/oasdiff/oasdiff/flatten/commonparams"
	oasload "github.com/oasdiff/oasdiff/load"
)

// The freeze gate (api-cli-surface ADR § Version promise).
//
// "The freeze is a CI invariant, not a doc sentence": once the freeze tag
// exists, the pipeline diffs api/openapi.yaml against the immutable freeze
// commit with a full structural diff, FAIL-CLOSED OVER AN ALLOWLIST of
// permitted addition change-types. Any change id not on the allowlist — a
// change oasdiff classes below "breaking", or one a future oasdiff version
// introduces — is a red build.
//
// Trusting oasdiff's own breaking/non-breaking taxonomy was considered and
// rejected: it classes some forbidden changes as warning or info, notably
// removals after a deprecation window this promise does not offer. Severity
// is oasdiff's opinion; the allowlist is Hikyo's policy.
//
// The freeze-guard CI job is armed against the v1.0.0 tag. Until that tag
// exists it reports a documented dormant success; after the tag is cut it
// loads api/openapi.yaml from that immutable commit and applies this gate to
// every proposed revision.

// PermittedChanges is the fail-closed allowlist: the oasdiff change ids that
// count as permitted additions under the version promise.
//
// The promise is: new endpoints, new OPTIONAL request fields, new response
// fields, and growth of enums declared open. Everything else — including
// anything oasdiff invents in a future release — is refused until reviewed
// and added here deliberately.
var PermittedChanges = map[string]bool{
	// New surface.
	"endpoint-added":                    true,
	"api-operation-id-added":            true,
	"api-tag-added":                     true,
	"response-media-type-added":         true,
	"response-success-status-added":     true,
	"response-non-success-status-added": true,

	// New optional inputs. A new REQUIRED request property is deliberately
	// absent: it breaks every existing client on the day it ships.
	"new-optional-request-parameter":        true,
	"new-optional-request-property":         true,
	"new-optional-request-header-parameter": true,
	"request-property-became-optional":      true,
	"request-parameter-became-optional":     true,

	// New outputs. Clients must ignore unknown fields — stated in the spec —
	// so a response gaining a member is additive by construction.
	//
	// `response-property-became-optional` is deliberately ABSENT: a field
	// every existing client was entitled to require, becoming one the server
	// may omit, breaks each of them. oasdiff does not grade it as breaking;
	// this promise does.
	"response-property-added":          true,
	"response-optional-property-added": true,
	"response-required-property-added": true,

	// Enum growth is deliberately NOT allowlisted, and the reason is worth
	// stating because the naive reading is the opposite. An OPEN enum carries
	// no `enum` keyword at all — growth is an edit to `x-extensible-enum`,
	// which oasdiff does not report as an enum change — so permitting
	// `*-enum-value-added` would buy nothing for open enums and would licence
	// growth of the CLOSED ones, which never grow. oasdiff cannot tell the
	// two apart; leaving the ids off the list is what makes the distinction
	// hold.
	//
	// There is exactly one exception, and it is not in this map: see
	// MLDSA65EnumGrowth, the one-time amendment of 2026-11-04.

	// Documentation-only movement. It changes no wire behaviour, and refusing
	// it would make improving a description a breaking change.
	"api-deprecated":                        true,
	"endpoint-deprecated":                   true,
	"api-description-changed":               true,
	"api-summary-changed":                   true,
	"description-changed":                   true,
	"request-property-description-changed":  true,
	"response-property-description-changed": true,
}

// MLDSA65EnumGrowth is the single, one-time exception to the closed-enum rule
// above, declared by the api-cli-surface amendment of 2026-11-04 (ticket #25,
// "One-time growth of closed enums for mldsa65"): the Transit algorithm enum
// and the PKI key-algorithm enums grow once, by the value `mldsa65`, and by
// nothing else. It sets no precedent; closed enums otherwise never grow.
//
// The exception is a closed list of exact findings, not an allowlisted change
// id, so anything outside it still fails: `mldsa44` on the same path,
// `mldsa65` on any other enum or operation that oasdiff reports, and a future
// `mldsa87`. oasdiff has no finding for enum growth in a response header; the
// document-coordinate layer (MLDSA65EnumLocations) refuses it (#1217). Adding
// to this list is an ADR amendment, not a code edit.
const (
	requestEnumAdded  = "request-property-enum-value-added"
	responseEnumAdded = "response-property-enum-value-added"
	mldsa65           = "mldsa65"
	environmentPath   = "/api/v1/orgs/{org}/projects/{project}/environments/{environment}"
)

// EnumGrowth names one permitted enum-value addition: the oasdiff change id,
// the operation, the path and the property oasdiff reports. The added value
// is always mldsa65 and is checked separately.
type EnumGrowth struct {
	ID, Operation, Path, Property string
}

var MLDSA65EnumGrowth = []EnumGrowth{
	{requestEnumAdded, "POST", "/api/v1/instance/pki/issuers", "key_algorithm"},
	{requestEnumAdded, "POST", "/api/v1/instance/pki/issuers/{issuer}/rotate", "key_algorithm"},
	{requestEnumAdded, "POST", "/api/v1/instance/pki/profiles", "policy/key_algorithms/items/"},
	{requestEnumAdded, "PUT", "/api/v1/instance/pki/profiles/{profile}", "policy/key_algorithms/items/"},
	{requestEnumAdded, "POST", environmentPath + "/certificates", "key_algorithm"},
	{requestEnumAdded, "POST", environmentPath + "/transit-keys", "algorithm"},
	{responseEnumAdded, "GET", "/api/v1/instance/pki/profiles", "profiles/items/policy/key_algorithms/items/"},
	{responseEnumAdded, "POST", "/api/v1/instance/pki/profiles", "policy/key_algorithms/items/"},
	{responseEnumAdded, "GET", "/api/v1/instance/pki/profiles/{profile}", "policy/key_algorithms/items/"},
	{responseEnumAdded, "PUT", "/api/v1/instance/pki/profiles/{profile}", "policy/key_algorithms/items/"},
	{responseEnumAdded, "POST", "/api/v1/instance/pki/profiles/{profile}/bindings", "policy/key_algorithms/items/"},
	{responseEnumAdded, "DELETE", "/api/v1/instance/pki/profiles/{profile}/bindings/{binding}", "policy/key_algorithms/items/"},
	{responseEnumAdded, "GET", environmentPath + "/certificate-profiles", "profiles/items/policy/key_algorithms/items/"},
	{responseEnumAdded, "GET", environmentPath + "/transit-keys", "items/items/algorithm"},
	{responseEnumAdded, "POST", environmentPath + "/transit-keys", "algorithm"},
	{responseEnumAdded, "GET", environmentPath + "/transit-keys/{transit_key}", "algorithm"},
	{responseEnumAdded, "PATCH", environmentPath + "/transit-keys/{transit_key}", "algorithm"},
	{responseEnumAdded, "POST", environmentPath + "/transit-keys/{transit_key}/lifecycle", "algorithm"},
	{responseEnumAdded, "POST", environmentPath + "/transit-keys/{transit_key}/rotate", "algorithm"},
	{responseEnumAdded, "POST", environmentPath + "/transit-keys/{transit_key}/trim", "key/algorithm"},
}

// permittedEnumGrowth reports whether change is one of MLDSA65EnumGrowth.
// oasdiff's arguments for both change ids are (value, property, ...).
func permittedEnumGrowth(change checker.Change) bool {
	args := change.GetArgs()
	if len(args) < 2 || fmt.Sprint(args[0]) != mldsa65 {
		return false
	}
	got := EnumGrowth{change.GetId(), change.GetOperation(), change.GetPath(), fmt.Sprint(args[1])}
	for _, want := range MLDSA65EnumGrowth {
		if got == want {
			return true
		}
	}
	return false
}

// Violation is one refused change.
type Violation struct {
	ID        string
	Operation string
	Path      string
	Text      string
	Level     string
}

func (v Violation) String() string {
	where := v.Path
	if v.Operation != "" {
		where = v.Operation + " " + v.Path
	}
	if where == "" {
		where = "(document)"
	}
	return fmt.Sprintf("[%s] %s: %s (level %s)", v.ID, where, v.Text, v.Level)
}

// CheckFreeze diffs revised against base and returns every change that is not
// a permitted addition, plus any violation of the bound 3.1 profile.
//
// Both halves matter and neither subsumes the other: oasdiff sees structural
// change but has no opinion about `nullable` or `jsonSchemaDialect`, and
// CheckProfile sees the profile but has no memory of what the document said
// yesterday.
//
// Violations have three sources: the oasdiff allowlist, freezeRemovals, and a
// third, independent layer (freezeContract) that compares the raw documents
// and refuses every difference but an explicit short list of additions and
// prose edits. It exists because enumerating oasdiff's blind spots never
// converged (#1217).
func CheckFreeze(base, revised []byte) ([]Violation, error) {
	if err := CheckProfile(revised); err != nil {
		return []Violation{{
			ID: "hikyo-profile", Text: err.Error(), Level: "ERR",
		}}, nil
	}

	baseSpec, err := specInfo(base, "freeze-base")
	if err != nil {
		return nil, fmt.Errorf("freeze gate: loading the base document: %w", err)
	}
	revisedSpec, err := specInfo(revised, "openapi.yaml")
	if err != nil {
		return nil, fmt.Errorf("freeze gate: loading the revised document: %w", err)
	}

	// GetWithOperationsSourcesMap, not Get: several checks dereference the
	// sources map, so passing nil panics rather than degrading. A gate that
	// crashes is a gate that gets skipped.
	report, sources, err := diff.GetWithOperationsSourcesMap(diff.NewConfig(), baseSpec, revisedSpec)
	if err != nil {
		return nil, fmt.Errorf("freeze gate: diffing: %w", err)
	}

	// Every check, down to INFO. The default run reports breaking changes
	// only, which is precisely the fail-open behaviour the ADR rejects.
	// Draft: a stability label is lifecycle metadata and must hide nothing.
	config := checker.NewConfig(checker.GetAllChecks(), checker.WithStabilityLevel(checker.StabilityDraft))
	changes := checker.CheckBackwardCompatibilityUntilLevel(config, report, sources, checker.INFO)

	violations := freezeRemovals(report)
	contract, err := freezeContract(base, revised)
	if err != nil {
		return nil, fmt.Errorf("freeze gate: comparing the documents: %w", err)
	}
	violations = append(violations, contract...)
	for _, change := range changes {
		id := change.GetId()
		if PermittedChanges[id] || permittedEnumGrowth(change) {
			continue
		}
		violations = append(violations, Violation{
			ID:        id,
			Operation: change.GetOperation(),
			Path:      change.GetPath(),
			Text:      change.GetUncolorizedText(checker.NewDefaultLocalizer()),
			Level:     change.GetLevel().String(),
		})
	}
	slices.SortFunc(violations, func(a, b Violation) int {
		return cmp.Or(cmp.Compare(a.ID, b.ID), cmp.Compare(a.Path, b.Path),
			cmp.Compare(a.Operation, b.Operation), cmp.Compare(a.Text, b.Text), cmp.Compare(a.Level, b.Level))
	})
	return violations, nil
}

// specInfo parses a document into the shape oasdiff wants, with references
// resolved (it expects them resolved and does not resolve them itself).
func specInfo(raw []byte, name string) (*oasload.SpecInfo, error) {
	loader := &openapi3.Loader{IsExternalRefsAllowed: false}
	doc, err := loader.LoadFromData(raw)
	if err != nil {
		return nil, err
	}
	normalizeFreezeParameters(doc)
	return &oasload.SpecInfo{Spec: doc, Url: name}, nil
}

// Compare the effective operation contract, so moving an unchanged parameter
// between a path and its operations is neutral. The parsed documents belong to
// this check only; the authored spec bytes are never modified.
func normalizeFreezeParameters(doc *openapi3.T) {
	documents := []*openapi3.T{doc}
	seen := make(map[*openapi3.Callback]bool)
	for len(documents) > 0 {
		current := documents[len(documents)-1]
		documents = documents[:len(documents)-1]
		commonparams.Move(current)
		for _, path := range current.Paths.Map() {
			for _, operation := range path.Operations() {
				for _, ref := range operation.Callbacks {
					if ref == nil || ref.Value == nil || seen[ref.Value] {
						continue
					}
					seen[ref.Value] = true
					// commonparams handles a document's top-level paths. Adapt
					// callback expressions to that shape and normalize them too.
					paths := openapi3.NewPaths()
					for expression, item := range ref.Value.Map() {
						paths.Set(expression, item)
					}
					documents = append(documents, &openapi3.T{Paths: paths})
				}
			}
		}
	}
}
