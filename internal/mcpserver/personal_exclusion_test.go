package mcpserver

import (
	"reflect"
	"strings"
	"testing"
)

// MCP never reaches personal values (personal-values ADR, Delivery): no tool
// is named for them and no tool service can reach the personal surface.
func TestMCPExposesNoPersonalValueSurface(t *testing.T) {
	for _, name := range append(ProductionToolNames(), WriteToolNames()...) {
		if strings.Contains(name, "personal") {
			t.Errorf("MCP tool %s reaches personal values", name)
		}
	}
	for _, services := range []reflect.Type{reflect.TypeOf(ProductionServices{}), reflect.TypeOf(WriteServices{})} {
		for i := 0; i < services.NumField(); i++ {
			field := services.Field(i)
			if strings.Contains(strings.ToLower(field.Name+field.Type.String()), "personal") {
				t.Errorf("MCP %s.%s reaches personal values", services.Name(), field.Name)
			}
		}
	}
}
