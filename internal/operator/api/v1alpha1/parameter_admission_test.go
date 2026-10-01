package v1alpha1_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/validation"
	"sigs.k8s.io/yaml"
)

func TestParameterAdmissionUsesUTF8Bytes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "chart/hikyo/crds/hikyo.dev_hikyosecrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	apiextensionsv1.SetDefaults_CustomResourceDefinition(&crd)
	var internal apiextensions.CustomResourceDefinition
	if err := apiextensionsv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(&crd, &internal, nil); err != nil {
		t.Fatal(err)
	}
	internal.Status.StoredVersions = []string{"v1alpha1"}
	if errs := validation.ValidateCustomResourceDefinition(t.Context(), &internal); len(errs) > 0 {
		t.Fatalf("CRD rejected by API validation: %v", errs)
	}
	rules := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties["parameters"].XValidations
	env, err := cel.NewEnv(cel.Variable("self", cel.MapType(cel.StringType, cel.StringType)))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("parameter admission rules: %v", rules)
	}
	ast, issues := env.Compile(rules[0].Rule)
	if issues.Err() != nil {
		t.Fatal(issues.Err())
	}
	program, err := env.Program(ast)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, value string
		accepted    bool
	}{
		{"ASCII limit", strings.Repeat("a", 256), true},
		{"ASCII overflow", strings.Repeat("a", 257), false},
		{"two-byte limit", strings.Repeat("é", 128), true},
		{"two-byte overflow", strings.Repeat("é", 129), false},
		{"four-byte limit", strings.Repeat("😀", 64), true},
		{"four-byte overflow", strings.Repeat("😀", 65), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, _, err := program.Eval(map[string]any{"self": map[string]string{"VALUE": tc.value}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Value() != tc.accepted {
				t.Fatalf("accepted = %v, want %v", result.Value(), tc.accepted)
			}
		})
	}
}

func TestCreationPolicyImmutabilityAllowsLegacyDefault(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "chart/hikyo/crds/hikyo.dev_hikyosecrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	rules := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties["target"].XValidations
	env, err := cel.NewEnv(cel.Variable("self", cel.MapType(cel.StringType, cel.StringType)), cel.Variable("oldSelf", cel.MapType(cel.StringType, cel.StringType)))
	if err != nil {
		t.Fatal(err)
	}
	var rule string
	for _, validation := range rules {
		if validation.Message == "target.creationPolicy is immutable" {
			rule = validation.Rule
		}
	}
	if rule == "" {
		t.Fatal("creationPolicy immutability rule absent")
	}
	ast, issues := env.Compile(rule)
	if issues.Err() != nil {
		t.Fatal(issues.Err())
	}
	program, err := env.Program(ast)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		old, current string
		allowed      bool
	}{
		{"", "", true}, {"", "Owner", true}, {"Owner", "", true},
		{"Owner", "Owner", true}, {"Orphan", "Orphan", true},
		{"", "Orphan", false}, {"Orphan", "", false}, {"Owner", "Orphan", false},
	} {
		t.Run(tc.old+" to "+tc.current, func(t *testing.T) {
			old, current := map[string]string{}, map[string]string{}
			if tc.old != "" {
				old["creationPolicy"] = tc.old
			}
			if tc.current != "" {
				current["creationPolicy"] = tc.current
			}
			result, _, err := program.Eval(map[string]any{"self": current, "oldSelf": old})
			if err != nil {
				t.Fatal(err)
			}
			if result.Value() != tc.allowed {
				t.Fatalf("allowed = %v, want %v", result.Value(), tc.allowed)
			}
		})
	}
}
