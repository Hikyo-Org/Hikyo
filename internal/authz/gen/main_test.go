package main

import (
	"bytes"
	"os"
	"os/exec"
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
	extras := wireExtras{Version: 1, Extensions: map[string]wireRow{"http:GET /value": {Ops: []string{"OpWrite"}, Index: 1, Events: []string{"EventRead"}}}, Entries: map[string]wireRow{"http:GET /healthz": {Class: "ClassUnauthenticated"}, "cli:server": {Class: "ClassSystem"}}}
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
	for _, name := range []string{"missing class", "unknown class", "missing primary", "unknown primary", "class conflict", "duplicate ID", "duplicate route", "bad template", "stale extension", "class override", "unknown extra op", "duplicate op", "unknown event", "duplicate event", "entry collision", "stale no-primary", "bad primary index", "empty extension", "missing version"} {
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
				extras.Extensions = map[string]wireRow{"http:GET /removed": {Events: []string{"EventRead"}}}
			case "class override":
				extras.Extensions = map[string]wireRow{"http:GET /value": {Class: "ClassInstance"}}
			case "unknown extra op":
				extras.Extensions = map[string]wireRow{"http:GET /value": {Ops: []string{"OpMissing"}}}
			case "duplicate op":
				extras.Extensions = map[string]wireRow{"http:GET /value": {Ops: []string{"OpRead"}}}
			case "unknown event":
				extras.Extensions = map[string]wireRow{"http:GET /value": {Events: []string{"EventMissing"}}}
			case "duplicate event":
				extras.Extensions = map[string]wireRow{"http:GET /value": {Events: []string{"EventRead", "EventRead"}}}
			case "entry collision":
				extras.Entries = map[string]wireRow{"http:GET /value": {Class: "ClassTenant"}}
			case "stale no-primary":
				extras.Extensions = map[string]wireRow{"http:GET /value": {NoPrimary: "legacy dispatcher"}}
			case "bad primary index":
				extras.Extensions = map[string]wireRow{"http:GET /value": {Index: 2, Ops: []string{"OpWrite"}}}
			case "empty extension":
				extras.Extensions = map[string]wireRow{"http:GET /value": {}}
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
			if strings.Contains(string(b), "hikyo:forward") {
				t.Fatal("target annotation leaked into public docs")
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
	for _, source := range []string{"type txForwarded interface { private() }", "type txForwarded interface { Read(_ string) }", "type txForwarded interface { Read(string) }", "type txForwarded interface { Read(); Read() }", "type txForwarded interface { Embedded }", "type txForwarded interface {\n//hikyo:forward A\n//hikyo:forward B\nRead() }", "type txForwarded interface {\n//hikyo:forward\nRead() }", "type txForwarded interface {\n//hikyo:forward \nRead() }", "type txForwarded interface {\n//hikyo:forwardOriginal\nRead() }", "type txForwarded interface {\n// hikyo:forward Original\nRead() }", "type txForwarded interface {\n/*hikyo:forward Original*/\nRead() }", "type txForwarded interface {\nRead() //hikyo:forward Original\n}", "type txForwarded interface {\n//hikyo:forward Original\n\nRead() }", "type txForwarded struct {}", "type Other interface {}"} {
		t.Run(source, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "forwarders.go")
			if err := os.WriteFile(path, []byte("package authz\n"+source), 0600); err != nil {
				t.Fatal(err)
			}
			var m forwarders
			err := readForwarders(path, &m)
			if err == nil {
				_, err = renderForwarders(m)
			}
			if err == nil {
				t.Fatal("unsupported input accepted")
			}
		})
	}
}

func TestGeneratedAuthorityMetadataIsFresh(t *testing.T) {
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

// These failures are Go compiler contracts, not a second typechecker in generation.
func TestCompilerRejectsMissingIncompatibleAndCollidingForwarders(t *testing.T) {
	for _, tc := range []struct{ name, resolver, collision string }{
		{"valid", "func (*resolver) Original() error { return nil }", ""},
		{"missing", "", ""},
		{"incompatible", "func (*resolver) Original() string { return \"\" }", ""},
		{"collision", "func (*resolver) Original() error { return nil }", "func (*TxAuthorizer) Read() error { return nil }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source := "package authz\ntype txForwarded interface { Read() error }\ntype resolver struct {}\ntype TxAuthorizer struct { r *resolver }\n" + tc.resolver + "\n" + tc.collision
			generated, err := renderForwarders(forwarders{Version: 1, Forwarders: []forwarder{{Name: "Read", Target: "Original", Signature: "func (a *TxAuthorizer) Read() error", Returns: true}}})
			if err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string][]byte{"go.mod": []byte("module compilerfixture\ngo 1.26\n"), "types.go": []byte(source), "forwarders_gen.go": generated} {
				if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "test", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off")
			out, err := cmd.CombinedOutput()
			if tc.name == "valid" && err != nil {
				t.Fatalf("valid fixture failed: %v\n%s", err, out)
			}
			if tc.name != "valid" && err == nil {
				t.Fatal("invalid contract compiled")
			}
		})
	}
}

func TestWireRequiresContractForHTTPExtras(t *testing.T) {
	for _, key := range []string{"http:GET /admin", "http:POST /healthz", "http:GET /healthz/child", "http:GET /metrics-extra", "http:GET /readyz?debug=true"} {
		t.Run(key, func(t *testing.T) {
			extras := wireExtras{Version: 1, Entries: map[string]wireRow{key: {Class: "ClassUnauthenticated"}}}
			if _, err := renderWire(testContract(), extras, testCatalog()); err == nil || !strings.Contains(err.Error(), "must be defined in OpenAPI") {
				t.Fatalf("noncontract HTTP extra %q: %v", key, err)
			}
		})
	}
	extras := wireExtras{Version: 1, Entries: map[string]wireRow{}}
	for _, key := range []string{"http:GET /healthz", "http:GET /metrics", "http:GET /readyz"} {
		extras.Entries[key] = wireRow{Class: "ClassUnauthenticated"}
	}
	b, err := renderWire(testContract(), extras, testCatalog())
	if err != nil {
		t.Fatal(err)
	}
	for key := range extras.Entries {
		if !bytes.Contains(b, []byte(key)) {
			t.Fatalf("operational route missing: %s", key)
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

func TestWireRejectsInvalidExplicitRows(t *testing.T) {
	for _, row := range []wireRow{
		{Class: "ClassMissing"},
		{Class: "ClassStub", Ops: []string{"OpRead"}},
		{Class: "ClassStub", Events: []string{"EventRead"}},
		{Class: "ClassTenant", Ops: []string{"OpMissing"}},
		{Class: "ClassTenant", Ops: []string{"OpRead", "OpRead"}},
		{Class: "ClassTenant", Events: []string{"EventMissing"}},
		{Class: "ClassTenant", Events: []string{"EventRead", "EventRead"}},
	} {
		if _, err := renderWire(testContract(), wireExtras{Version: 1, Entries: map[string]wireRow{"cli:invalid": row}}, testCatalog()); err == nil {
			t.Fatalf("invalid row accepted: %+v", row)
		}
	}
	if _, err := renderWire(testContract(), wireExtras{Version: 1, Entries: map[string]wireRow{"": {Class: "ClassTenant"}}}, testCatalog()); err == nil {
		t.Fatal("empty key accepted")
	}
}
