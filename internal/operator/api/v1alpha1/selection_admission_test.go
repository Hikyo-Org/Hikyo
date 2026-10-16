package v1alpha1_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/cel-go/cel"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

func TestSelectionAdmission(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "chart/hikyo/crds/hikyo.dev_hikyosecrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
	if slices.Contains(spec.Required, "mapping") || spec.Properties["allKeys"].Type != "boolean" {
		t.Fatal("allKeys must be boolean and mapping optional at the field level")
	}
	env, err := cel.NewEnv(cel.Variable("self", cel.DynType))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.XValidations) != 2 {
		t.Fatalf("expected selection and target admission rules, got %v", spec.XValidations)
	}
	var programs []cel.Program
	for _, rule := range spec.XValidations {
		ast, issues := env.Compile(rule.Rule)
		if issues.Err() != nil {
			t.Fatal(issues.Err())
		}
		program, err := env.Program(ast)
		if err != nil {
			t.Fatal(err)
		}
		programs = append(programs, program)
	}
	mapping := []any{map[string]any{"key": "API_KEY"}}
	for _, tc := range []struct {
		name string
		spec map[string]any
		want bool
	}{
		{"legacy mapping", map[string]any{"mapping": mapping}, true},
		{"explicit false", map[string]any{"allKeys": false, "mapping": mapping}, true},
		{"all keys", map[string]any{"allKeys": true}, true},
		{"all keys empty mapping", map[string]any{"allKeys": true, "mapping": []any{}}, true},
		{"all keys opaque", map[string]any{"allKeys": true, "target": map[string]any{"type": "Opaque"}}, true},
		{"all keys typed", map[string]any{"allKeys": true, "target": map[string]any{"type": "kubernetes.io/tls"}}, false},
		{"neither", map[string]any{}, false},
		{"false without mapping", map[string]any{"allKeys": false}, false},
		{"empty mapping", map[string]any{"mapping": []any{}}, false},
		{"both", map[string]any{"allKeys": true, "mapping": mapping}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tc.spec["target"]; !ok {
				tc.spec["target"] = map[string]any{"name": "app-secret"}
			}
			accepted := true
			for _, program := range programs {
				result, _, err := program.Eval(map[string]any{"self": tc.spec})
				if err != nil {
					t.Fatal(err)
				}
				accepted = accepted && result.Value() == true
			}
			if accepted != tc.want {
				t.Fatalf("accepted=%v, want %v", accepted, tc.want)
			}
		})
	}
}
