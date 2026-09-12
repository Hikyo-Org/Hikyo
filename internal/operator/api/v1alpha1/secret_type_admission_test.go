package v1alpha1_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

func TestTargetSecretTypeAdmission(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "chart/hikyo/crds/hikyo.dev_hikyosecrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Decode just the schema tree needed here. The generic JSON Schema enum
	// constraint is the CRD admission allowlist; no webhook supplies defaults.
	type propertySchema struct {
		Type    string   `yaml:"type"`
		Default string   `yaml:"default"`
		Enum    []string `yaml:"enum"`
	}
	var crd struct {
		Spec struct {
			Versions []struct {
				Schema struct {
					OpenAPIV3Schema struct {
						Properties map[string]struct {
							Properties map[string]struct {
								Properties map[string]propertySchema `yaml:"properties"`
							} `yaml:"properties"`
						} `yaml:"properties"`
					} `yaml:"openAPIV3Schema"`
				} `yaml:"schema"`
			} `yaml:"versions"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	property := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties["target"].Properties["type"]
	if property.Default != "Opaque" {
		t.Fatal("target.type must default to Opaque")
	}
	compiler := jsonschema.NewCompiler()
	var enum []any
	for _, value := range property.Enum {
		enum = append(enum, value)
	}
	if err := compiler.AddResource("target-type.json", map[string]any{"type": property.Type, "enum": enum}); err != nil {
		t.Fatal(err)
	}
	validator, err := compiler.Compile("target-type.json")
	if err != nil {
		t.Fatal(err)
	}
	allowed := []string{"Opaque", "kubernetes.io/dockerconfigjson", "kubernetes.io/tls", "kubernetes.io/basic-auth", "kubernetes.io/ssh-auth"}
	for _, typ := range allowed {
		if errs := validator.Validate(typ); errs != nil {
			t.Fatalf("allowlisted type %q rejected: %v", typ, errs)
		}
	}
	for _, typ := range []string{"kubernetes.io/service-account-token", "kubernetes.io/dockercfg", "custom.example/credentials", ""} {
		errs := validator.Validate(typ)
		if errs == nil {
			t.Fatalf("unknown type %q admitted", typ)
		}
		for _, name := range allowed {
			if !strings.Contains(errs.Error(), name) {
				t.Fatalf("admission error omits allowlisted type %q: %v", name, errs)
			}
		}
	}
}
