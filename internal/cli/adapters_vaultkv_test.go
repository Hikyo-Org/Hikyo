package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/importer"
)

func TestKVAddressingMapsMountAndPathOntoRepositoryDestination(t *testing.T) {
	kind, owner, repo := "", "", ""
	if err := kvAddressing(&kind, &owner, &repo, "kv/team-a", "apps/pay", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if kind != "repository" || owner != "kv/team-a" || repo != "apps/pay" {
		t.Fatalf("kvAddressing = %q %q %q", kind, owner, repo)
	}
	kind, owner, repo = "", "", ""
	if err := kvAddressing(&kind, &owner, &repo, "", "", "", "", ""); err != nil || kind != "" || owner != "" || repo != "" {
		t.Fatalf("absent KV flags changed addressing: %v %q %q %q", err, kind, owner, repo)
	}
	for name, input := range map[string][4]string{
		"owner mixed":   {"", "acme", "", ""},
		"repo mixed":    {"", "", "app", ""},
		"kind mismatch": {"organization", "", "", ""},
		"environment":   {"", "", "", "prod"},
	} {
		kind, owner, repo := input[0], input[1], input[2]
		if err := kvAddressing(&kind, &owner, &repo, "secret", "apps", input[3], "", ""); err == nil {
			t.Errorf("%s: accepted mixed addressing", name)
		}
	}
	kind, owner, repo = "", "", ""
	if err := kvAddressing(&kind, &owner, &repo, "secret", "", "", "", ""); err == nil {
		t.Error("--mount without --path accepted")
	}
}

func TestAdapterCreateRefusesVaultKVWithoutKVAddressing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	stateDir := t.TempDir()
	ios := IO{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Env: Env{Getenv: func(name string) string {
		if name == "HIKYO_STATE_DIR" {
			return stateDir
		}
		return ""
	}}}
	code := Run(t.Context(), ios, []string{"adapter", "create", "--provider", "vault-kv", "--origin", "https://vault.example:8200", "--kind", "organization", "--owner", "secret", "--visibility", "all", "--keys", "key_one"})
	if code != ExitUsage || !strings.Contains(stderr.String(), "vault-kv targets take --mount and --path") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestVaultImportLoopRefusesOverlappingActiveSyncDestination(t *testing.T) {
	adapters := apigen.AdapterList{Items: []apigen.Adapter{{
		Id: "adp_kv", Provider: "vault-kv", Origin: "https://vault.example:8200/team-a", State: apigen.AdapterStateActive,
		Targets: []apigen.AdapterTarget{{Id: "tgt_kv", State: apigen.AdapterTargetStateActive, DestinationOwner: "secret", DestinationName: "apps/pay"}},
	}, {
		Id: "adp_gh", Provider: "github-actions", Origin: "https://vault.example:8200", State: apigen.AdapterStateActive,
		Targets: []apigen.AdapterTarget{{Id: "tgt_gh", State: apigen.AdapterTargetStateActive, DestinationOwner: "secret", DestinationName: "apps"}},
	}}}
	source := func(namespace, mount, path string) importer.Result {
		return importer.Result{Identity: "https://VAULT.example:8200", Namespace: namespace, Scope: importer.Scope{Mount: mount, PathPrefix: path}}
	}
	for _, tc := range []struct {
		name   string
		source importer.Result
		loop   bool
	}{
		{"same tree", source("team-a", "secret", "apps/pay"), true},
		{"root namespace mount alias", source("", "team-a/secret", "apps/pay"), true},
		{"root namespace alias ancestor", source("", "team-a/secret", "apps"), true},
		{"root namespace alias sibling", source("", "team-a/secret", "apps/payroll"), false},
		{"namespace case remains distinct", source("", "Team-A/secret", "apps/pay"), false},
		{"parent of destination", source("team-a", "secret", "apps"), true},
		{"whole mount", source("team-a", "secret", ""), true},
		{"inside destination", source("team-a/", "secret", "apps/pay/api"), true},
		{"sibling tree", source("team-a", "secret", "apps/payroll"), false},
		{"other mount", source("team-a", "kv", "apps/pay"), false},
		{"other namespace", source("", "secret", "apps/pay"), false},
	} {
		name, loop, err := vaultImportLoop(tc.source, adapters)
		if err != nil {
			t.Fatal(err)
		}
		if loop != tc.loop {
			t.Errorf("%s: loop = %v (%s), want %v", tc.name, loop, name, tc.loop)
		}
		if loop && !strings.Contains(name, "tgt_kv") {
			t.Errorf("%s: refusal names %q, want the target", tc.name, name)
		}
	}
	// The scheme's default port is the same endpoint however it is spelled.
	defaultPort := apigen.AdapterList{Items: []apigen.Adapter{{
		Id: "adp_kv", Provider: "vault-kv", Origin: "https://vault.example/team-a", State: apigen.AdapterStateActive,
		Targets: []apigen.AdapterTarget{{Id: "tgt_kv", State: apigen.AdapterTargetStateActive, DestinationOwner: "secret", DestinationName: "apps/pay"}},
	}}}
	for identity, loop := range map[string]bool{"https://vault.example:443": true, "https://Vault.Example": true, "https://vault.example:8443": false} {
		result := importer.Result{Identity: identity, Namespace: "team-a", Scope: importer.Scope{Mount: "secret", PathPrefix: "apps/pay"}}
		if _, got, err := vaultImportLoop(result, defaultPort); got != loop || err != nil {
			t.Errorf("%s: loop = %v, want %v", identity, got, loop)
		}
	}
	adapters.Items[0].Targets[0].State = apigen.AdapterTargetStateTombstoned
	if _, loop, err := vaultImportLoop(source("team-a", "secret", "apps/pay"), adapters); loop || err != nil {
		t.Fatal("tombstoned target still refuses imports")
	}
}

func TestVaultImportLoopRefusesUnverifiableOrigins(t *testing.T) {
	valid := importer.Result{Identity: "https://vault.example", Scope: importer.Scope{Mount: "secret", PathPrefix: "apps"}}
	for _, identity := range []string{"", "vault.example", "https://", "https://vault.example/%zz", "ftp://vault.example", "https://user:pass@vault.example"} {
		source := valid
		source.Identity = identity
		if _, _, err := vaultImportLoop(source, apigen.AdapterList{}); err == nil {
			t.Errorf("accepted source identity %q", identity)
		}
	}
	for _, origin := range []string{"", "vault.example", "https://", "https://vault.example/%zz"} {
		adapters := apigen.AdapterList{Items: []apigen.Adapter{{Provider: "vault-kv", Origin: origin, State: apigen.AdapterStateActive}}}
		if _, _, err := vaultImportLoop(valid, adapters); err == nil {
			t.Errorf("accepted active adapter origin %q", origin)
		}
		adapters.Items[0].State = apigen.AdapterStateTombstoned
		if _, _, err := vaultImportLoop(valid, adapters); err != nil {
			t.Errorf("retired adapter blocked import: %v", err)
		}
	}
}

func TestVaultImportLoopNamespaceMountAliasesAreSymmetric(t *testing.T) {
	for _, tc := range []struct{ origin, destinationMount, sourceNamespace, sourceMount string }{
		{"https://vault.example", "team-a/secret", "team-a", "secret"},
		{"https://vault.example/team-a", "child/secret", "team-a/child", "secret"},
		{"https://vault.example/team-a/child", "secret", "team-a", "child/secret"},
	} {
		adapters := apigen.AdapterList{Items: []apigen.Adapter{{Id: "adp_kv", Provider: "vault-kv", Origin: tc.origin, State: apigen.AdapterStateActive, Targets: []apigen.AdapterTarget{{Id: "tgt_kv", State: apigen.AdapterTargetStateActive, DestinationOwner: tc.destinationMount, DestinationName: "apps/pay"}}}}}
		source := importer.Result{Identity: "https://vault.example", Namespace: tc.sourceNamespace, Scope: importer.Scope{Mount: tc.sourceMount, PathPrefix: "apps/pay"}}
		if _, loop, err := vaultImportLoop(source, adapters); err != nil || !loop {
			t.Fatalf("alias %+v: loop=%v err=%v", tc, loop, err)
		}
	}
}
