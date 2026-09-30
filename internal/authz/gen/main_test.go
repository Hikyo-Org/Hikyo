package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
)

func testCatalog() catalog {
	return catalog{Operations: map[string]string{"OpRead": "read", "OpWrite": "write"}, Classes: map[string]string{"OpRead": "ClassTenant", "OpWrite": "ClassTenant"}, Events: map[string]bool{"EventRead": true}}
}
func testContract() map[string]api.Operation {
	return map[string]api.Operation{"read": {ID: "read", Method: "GET", Path: "/value", Class: "tenant", AuthzOp: "read"}}
}

func TestWirePreservesExplicitExceptions(t *testing.T) {
	ops := testContract()
	ops["login"] = api.Operation{ID: "login", Method: "POST", Path: "/login", Class: "unauthenticated"}
	extras := wireExtras{Version: 1, Extensions: []wireRow{{Key: "http:GET /value", Ops: []string{"OpWrite"}, Index: 1, Events: []string{"EventRead"}}}, Entries: []wireRow{{Key: "http:GET /healthz", Class: "ClassUnauthenticated"}, {Key: "cli:server", Class: "ClassSystem"}}}
	b, err := renderWire(ops, extras, testCatalog())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Ops: []Operation{OpWrite, OpRead}", "audit.EventRead", "http:POST /login", "cli:server"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s", want)
		}
	}
	if bytes.Contains(b, []byte("mustNew")) {
		t.Fatal("generated wire metadata adds startup validation")
	}
}

func TestWireRejectsInvalidMissingConflictingAndStaleMetadata(t *testing.T) {
	for _, name := range []string{"missing class", "unknown class", "missing primary", "unknown primary", "class conflict", "duplicate ID", "duplicate route", "bad template", "stale extension", "class override", "duplicate extension", "unknown extra op", "duplicate op", "unknown event", "duplicate event", "entry collision", "stale no-primary", "bad primary index", "empty extension", "missing version"} {
		t.Run(name, func(t *testing.T) {
			ops := testContract()
			extras := wireExtras{Version: 1}
			cat := testCatalog()
			op := ops["read"]
			switch name {
			case "missing class":
				op.Class = ""
			case "unknown class":
				op.Class = "stub"
			case "missing primary":
				op.AuthzOp = ""
			case "unknown primary":
				op.AuthzOp = "no-such-op"
			case "class conflict":
				op.Class = "instance"
			case "duplicate ID":
				ops["other"] = op
				other := ops["other"]
				other.Path = "/other"
				ops["other"] = other
			case "duplicate route":
				ops["other"] = op
				other := ops["other"]
				other.ID = "other"
				ops["other"] = other
			case "bad template":
				op.Path = "/{value"
			case "stale extension":
				extras.Extensions = []wireRow{{Key: "http:GET /removed", Events: []string{"EventRead"}}}
			case "class override":
				extras.Extensions = []wireRow{{Key: "http:GET /value", Class: "ClassInstance"}}
			case "duplicate extension":
				extras.Extensions = []wireRow{{Key: "http:GET /value", Events: []string{"EventRead"}}, {Key: "http:GET /value", Events: []string{"EventRead"}}}
			case "unknown extra op":
				extras.Extensions = []wireRow{{Key: "http:GET /value", Ops: []string{"OpMissing"}}}
			case "duplicate op":
				extras.Extensions = []wireRow{{Key: "http:GET /value", Ops: []string{"OpRead"}}}
			case "unknown event":
				extras.Extensions = []wireRow{{Key: "http:GET /value", Events: []string{"EventMissing"}}}
			case "duplicate event":
				extras.Extensions = []wireRow{{Key: "http:GET /value", Events: []string{"EventRead", "EventRead"}}}
			case "entry collision":
				extras.Entries = []wireRow{{Key: "http:GET /value", Class: "ClassTenant"}}
			case "stale no-primary":
				extras.Extensions = []wireRow{{Key: "http:GET /value", NoPrimary: "legacy dispatcher"}}
			case "bad primary index":
				extras.Extensions = []wireRow{{Key: "http:GET /value", Index: 2, Ops: []string{"OpWrite"}}}
			case "empty extension":
				extras.Extensions = []wireRow{{Key: "http:GET /value"}}
			case "missing version":
				extras.Version = 0
			}
			ops["read"] = op
			if _, err := renderWire(ops, extras, cat); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}

func TestForwarderAllowlistKeepsGoSignatureAndDocs(t *testing.T) {
	for _, test := range []struct{ name, annotation, target string }{
		{"same name", "", "Renamed"},
		{"renamed", "//hikyo:forward Original\n", "Original"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "forwarders.go")
			if err := os.WriteFile(path, []byte("package authz\nimport \"context\"\ntype txForwarded interface {\n// Renamed preserves owner documentation.\n"+test.annotation+"Renamed(ctx context.Context, values ...string) error\n}"), 0600); err != nil {
				t.Fatal(err)
			}
			var m forwarders
			if err := readForwarders(path, &m); err != nil {
				t.Fatal(err)
			}
			b, err := renderForwarders(m)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"// Renamed preserves owner documentation.", "Renamed(ctx context.Context, values ...string) error", "return a.r." + test.target + "(ctx, values...)", "var _ txForwarded = (*TxAuthorizer)(nil)"} {
				if !strings.Contains(string(b), want) {
					t.Errorf("missing %s", want)
				}
			}
		})
	}
}

func TestForwarderRejectsUnsupportedAllowlist(t *testing.T) {
	for _, source := range []string{"type txForwarded interface { Embedded }", "type txForwarded interface {\n//hikyo:forward A\n//hikyo:forward B\nRead() }", "type txForwarded interface {\n//hikyo:forward\nRead() }", "type txForwarded interface {\n//hikyo:forward \nRead() }", "type txForwarded interface {\n//hikyo:forwardOriginal\nRead() }", "type txForwarded interface {\n// hikyo:forward Original\nRead() }", "type txForwarded interface {\n/*hikyo:forward Original*/\nRead() }", "type txForwarded interface {\nRead() //hikyo:forward Original\n}", "type txForwarded interface {\n//hikyo:forward Original\n\nRead() }", "type txForwarded struct {}", "type Other interface {}"} {
		t.Run(source, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "forwarders.go")
			if err := os.WriteFile(path, []byte("package authz\n"+source), 0600); err != nil {
				t.Fatal(err)
			}
			var m forwarders
			if err := readForwarders(path, &m); err == nil {
				t.Fatal("unsupported input accepted")
			}
		})
	}
	base := forwarder{Name: "Read", Target: "Original", Signature: "func (a *TxAuthorizer) Read(value string) error"}
	for _, name := range []string{"duplicate", "private", "blank argument", "receiver", "doc injection", "invalid target"} {
		t.Run(name, func(t *testing.T) {
			m := forwarders{Version: 1, Forwarders: []forwarder{base}}
			switch name {
			case "duplicate":
				m.Forwarders = append(m.Forwarders, base)
			case "private":
				m.Forwarders[0].Name = "read"
			case "blank argument":
				m.Forwarders[0].Signature = "func (a *TxAuthorizer) Read(_ string) error"
			case "receiver":
				m.Forwarders[0].Signature = "func (a TxAuthorizer) Read(value string) error"
			case "doc injection":
				m.Forwarders[0].Doc = "var leak = true"
			case "invalid target":
				m.Forwarders[0].Target = "a.Field"
			}
			if _, err := renderForwarders(m); err == nil {
				t.Fatal("unsupported entry accepted")
			}
		})
	}
}

func TestMetadataJSONRejectsDuplicateUnknownAndTrailingInput(t *testing.T) {
	for _, source := range []string{`{"version":1,"version":2}`, `{"version":1,"unknown":true}`, `{"version":1} {}`} {
		path := filepath.Join(t.TempDir(), "wire.json")
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		var extras wireExtras
		if err := readJSON(path, &extras); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
}

func TestGeneratedAuthorityMetadataIsFreshAndTypechecked(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	if err := run(root, true); err != nil {
		t.Fatal(err)
	}
}

func TestOutputCheckRejectsMissingAndStaleFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.go")
	expected := []byte("package authz\n")
	if err := updateOutput(path, expected, true); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing output accepted: %v", err)
	}
	if err := updateOutput(path, []byte("package stale\n"), false); err != nil {
		t.Fatal(err)
	}
	if err := updateOutput(path, expected, true); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale output accepted: %v", err)
	}
	if err := updateOutput(path, expected, false); err != nil {
		t.Fatal(err)
	}
	if err := updateOutput(path, expected, true); err != nil {
		t.Fatal(err)
	}
}

func TestForwarderTypecheckRejectsMissingAndIncompatibleTargets(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var base forwarders
	if err := readForwarders(filepath.Join(root, "internal/authz/forwarders.go"), &base); err != nil {
		t.Fatal(err)
	}
	wirePath := filepath.Join(root, "internal/authz/wire_registry_gen.go")
	wire, err := os.ReadFile(wirePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"MissingResolverMethod", "SetClock"} {
		t.Run(target, func(t *testing.T) {
			m := base
			m.Forwarders = append([]forwarder(nil), base.Forwarders...)
			m.Forwarders[0].Target = target
			generated, err := renderForwarders(m)
			if err != nil {
				t.Fatal(err)
			}
			outputs := map[string][]byte{filepath.Join(root, "internal/authz/forwarders_gen.go"): generated, wirePath: wire}
			if err := validateTypes(root, outputs, m); err == nil {
				t.Fatal("invalid resolver contract accepted")
			}
		})
	}
}

func TestWireRequiresContractForHTTPExtras(t *testing.T) {
	for _, key := range []string{"http:GET /admin", "http:POST /healthz", "http:GET /healthz/child", "http:GET /metrics-extra", "http:GET /readyz?debug=true"} {
		t.Run(key, func(t *testing.T) {
			extras := wireExtras{Version: 1, Entries: []wireRow{{Key: key, Class: "ClassUnauthenticated"}}}
			if _, err := renderWire(testContract(), extras, testCatalog()); err == nil || !strings.Contains(err.Error(), "must be defined in OpenAPI") {
				t.Fatalf("noncontract HTTP extra %q: %v", key, err)
			}
		})
	}
	extras := wireExtras{Version: 1}
	for _, key := range []string{"http:GET /healthz", "http:GET /metrics", "http:GET /readyz"} {
		extras.Entries = append(extras.Entries, wireRow{Key: key, Class: "ClassUnauthenticated"})
	}
	b, err := renderWire(testContract(), extras, testCatalog())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range extras.Entries {
		if !bytes.Contains(b, []byte(row.Key)) {
			t.Fatalf("operational route missing: %s", row.Key)
		}
	}
	// Any additional HTTP route comes from the contract, even when public.
	ops := testContract()
	ops["admin"] = api.Operation{ID: "admin", Method: "GET", Path: "/admin", Class: "unauthenticated"}
	b, err = renderWire(ops, extras, testCatalog())
	if err != nil || !bytes.Contains(b, []byte("http:GET /admin")) {
		t.Fatalf("contract-owned HTTP route absent: %v", err)
	}
}
