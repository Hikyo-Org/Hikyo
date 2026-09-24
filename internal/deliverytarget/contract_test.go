package deliverytarget_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/deliverytarget"
)

// TestDeliveryTargetContractIsValueFree is the ADR's value-free pin (D4,
// #788 acceptance): every string a delivery-target request can carry is a
// closed enum, the Kubernetes name grammar, the Kubernetes condition
// type/reason grammar, a UID, an RFC 3339 time or a SemVer version, each
// bounded exactly as this package bounds it, and every object is closed. A free string anywhere in a request body fails here.
func TestDeliveryTargetContractIsValueFree(t *testing.T) {
	doc, err := api.Doc()
	if err != nil {
		t.Fatal(err)
	}
	grammars := map[string]int{
		deliverytarget.DNS1123LabelPattern:     deliverytarget.MaxLabelLength,
		deliverytarget.DNS1123SubdomainPattern: deliverytarget.MaxSubdomainLength,
		deliverytarget.UIDPattern:              deliverytarget.MaxUIDLength,
		deliverytarget.SemVerPattern:           deliverytarget.MaxVersionLength,
		deliverytarget.ConditionTypePattern:    deliverytarget.MaxConditionTypeLength,
		deliverytarget.ConditionReasonPattern:  deliverytarget.MaxConditionReasonLength,
	}
	for _, id := range []string{"reportDeliveryTarget", "tombstoneDeliveryTarget"} {
		op := operation(t, doc, id)
		body := op.RequestBody.Value.Content.Get("application/json").Schema.Value
		walk(t, id, body, func(path string, s *openapi3.Schema) {
			switch {
			case len(s.Enum) > 0:
			case s.Format == "date-time" && s.Pattern == "":
			case s.Pattern != "":
				bound, ok := grammars[s.Pattern]
				if !ok {
					t.Errorf("%s: pattern %q is not one of the closed grammars", path, s.Pattern)
				} else if s.MaxLength == nil || *s.MaxLength != uint64(bound) {
					t.Errorf("%s: pattern %q must carry maxLength %d", path, s.Pattern, bound)
				}
			default:
				t.Errorf("%s: a free string; every request string must be an enum, a grammar or a time", path)
			}
		})
	}

	// The wire enums are this package's closed sets, exactly.
	schemas := doc.Components.Schemas
	condition := schemas["DeliveryTargetCondition"].Value.Properties
	var capabilities []string
	for _, v := range schemas["ProtocolCapability"].Value.Extensions["x-extensible-enum"].([]any) {
		if s := fmt.Sprint(v); strings.HasPrefix(s, deliverytarget.Capability+"/") {
			capabilities = append(capabilities, s)
		}
	}
	for _, pin := range []struct {
		name      string
		got, want []string
	}{
		{"DeliveryTargetCondition.status", enum(condition["status"].Value), deliverytarget.ConditionStatuses()},
		{"DeliveryTargetLifecycle", enum(schemas["DeliveryTargetLifecycle"].Value), deliverytarget.Lifecycles()},
		{"DeliveryTargetReporter.integration", enum(schemas["DeliveryTargetReporter"].Value.Properties["integration"].Value), deliverytarget.Reporters()},
		{"DeliveryTargetState", enum(schemas["DeliveryTargetState"].Value),
			slices.DeleteFunc(deliverytarget.States(), func(s string) bool { return s == deliverytarget.StateUnknown })},
		{"DeliveryTarget.refusal.cause", enum(schemas["DeliveryTarget"].Value.Properties["refusal"].Value.Properties["cause"].Value),
			[]string{deliverytarget.RefusalVocabulary}},
		{"ProtocolCapability", capabilities, deliverytarget.CapabilityTokens()},
	} {
		got, want := slices.Sorted(slices.Values(pin.got)), slices.Compact(slices.Sorted(slices.Values(pin.want)))
		if !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", pin.name, got, want)
		}
	}
}

// TestRefusalCausesPinTheAuditSchema holds the audit registry's closed cause
// enum to this package's set.
func TestRefusalCausesPinTheAuditSchema(t *testing.T) {
	spec, ok := audit.Spec(audit.EventDeliveryTargetRefused)
	if !ok {
		t.Fatal("identity.delivery_target_refused is not registered")
	}
	got := slices.Clone(spec.Schema["cause"].Enum)
	slices.Sort(got)
	if want := deliverytarget.RefusalCauses(); !slices.Equal(got, want) {
		t.Fatalf("audit cause enum = %v, want %v", got, want)
	}
}

func operation(t *testing.T, doc *openapi3.T, id string) *openapi3.Operation {
	t.Helper()
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			if op.OperationID == id {
				return op
			}
		}
	}
	t.Fatalf("operation %s is not in the contract", id)
	return nil
}

// walk visits every string leaf of s and fails on an open object.
func walk(t *testing.T, path string, s *openapi3.Schema, leaf func(string, *openapi3.Schema)) {
	t.Helper()
	switch {
	case s.Type.Is("string"):
		leaf(path, s)
	case s.Type.Is("array"):
		if s.MaxItems == nil {
			t.Errorf("%s: an unbounded array", path)
		}
		walk(t, path+"[]", s.Items.Value, leaf)
	case s.Type.Is("object"):
		if s.AdditionalProperties.Has == nil || *s.AdditionalProperties.Has || s.AdditionalProperties.Schema != nil {
			t.Errorf("%s: an open object; every request object must be additionalProperties: false", path)
		}
		for name, p := range s.Properties {
			walk(t, path+"."+name, p.Value, leaf)
		}
	case s.Type.Is("integer"):
	default:
		t.Errorf("%s: unexpected schema type %v", path, s.Type)
	}
}

func enum(s *openapi3.Schema) []string {
	out := make([]string, 0, len(s.Enum))
	for _, v := range s.Enum {
		out = append(out, fmt.Sprint(v))
	}
	return out
}

// TestVocabularyFitsTheConditionGrammar: every advertised type and reason
// passes the wire grammar, so a vocabulary value never meets a 400, and a
// string outside the grammar is a shape refusal before the vocabulary check.
func TestVocabularyFitsTheConditionGrammar(t *testing.T) {
	report := func(typ, reason string) deliverytarget.Report {
		return deliverytarget.Report{
			Vocabulary: 1, Generation: 1, ObservedGeneration: 1, ReportedAt: time.Unix(1_800_000_000, 0),
			ReportIntervalSeconds: 300, ReporterVersion: "1.0.0",
			Target: deliverytarget.Target{
				ClusterID: "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f00", InstanceUID: "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f01",
				Namespace: "apps", Name: "api", UID: "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f02",
			},
			Conditions: []deliverytarget.Condition{{Type: typ, Status: "True", Reason: reason, ObservedGeneration: 1}},
		}
	}
	for _, v := range deliverytarget.Vocabularies() {
		for _, typ := range deliverytarget.ConditionTypes(v) {
			for _, reason := range deliverytarget.Reasons(v, typ) {
				if err := deliverytarget.CheckShape(report(typ, reason)); err != nil {
					t.Errorf("vocabulary %d %s/%s fails the grammar: %v", v, typ, reason, err)
				}
			}
		}
	}
	for _, bad := range []deliverytarget.Report{
		report("has space", "Reconciled"), report("Ready", "has space"),
		report("Ready", "9LeadingDigit"), report(strings.Repeat("a", 317), "Reconciled"),
	} {
		if err := deliverytarget.CheckShape(bad); !errors.Is(err, deliverytarget.ErrShape) {
			t.Errorf("%+v passed the grammar", bad.Conditions[0])
		}
	}
	// Grammatical but outside the vocabulary: the 422 path.
	if err := deliverytarget.CheckShape(report("example.com/Healthy", "Fine")); err != nil {
		t.Fatalf("a grammatical unknown condition failed the shape check: %v", err)
	}
}
