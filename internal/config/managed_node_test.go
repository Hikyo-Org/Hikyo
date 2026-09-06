package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func managedNodeTestValues() map[string]string {
	return map[string]string{"HIKYO_LISTEN": "127.0.0.1:8080", "HIKYO_OPERATIONAL_LISTEN": "127.0.0.1:8081", "HIKYO_ADMISSION_BUDGET_MIB": "272"}
}

func managedNodeTestTLS(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "node.example"}, DNSNames: []string{"node.example"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))
}

func TestManagedNodeSeedImportsExactContentsOnceAndAppliesWithoutSourceFiles(t *testing.T) {
	cert, key := managedNodeTestTLS(t)
	contents := map[string]string{
		"HIKYO_TLS_CERT_FILE": "\n" + cert, "HIKYO_TLS_KEY_FILE": key + "\n",
		"HIKYO_ADAPTER_EGRESS_POLICY_FILE": `{ "https://adapter.example": ["10.1.2.3/8"] }`,
		"HIKYO_OIDC_EGRESS_POLICY_FILE":    `{ "https://identity.example": ["192.168.0.0/16"] }`,
		"HIKYO_DYNAMIC_EGRESS_POLICY_FILE": `{ "postgres://operator@db.example/app": ["10.0.0.0/8"] }`,
	}
	input := map[string]string{"HIKYO_DB": "postgres://postgres@127.0.0.1/seed?sslmode=disable", "HIKYO_LISTEN": "0.0.0.0:9090", "HIKYO_ADMISSION_BUDGET_MIB": "640", "HIKYO_PG_POOL_MAX": "7", "HIKYO_BACKUP_DIR": "/not-opened/backups", "HIKYO_EXTERNAL_ORIGIN": "https://node.example"}
	for source, bytes := range contents {
		path := filepath.Join(t.TempDir(), source)
		if err := os.WriteFile(path, []byte(bytes), 0600); err != nil {
			t.Fatal(err)
		}
		input[source] = path
	}
	cfg, _, err := LoadBootstrap("server", []string{"--dev", "--listen", "0.0.0.0:8088"}, func(key string) string { return input[key] }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:8080" || cfg.TLSCertFile != "" || cfg.AdapterEgressPolicy != nil {
		t.Fatal("bootstrap consumed deferred node sources")
	}
	captured, err := cfg.SeedNodeValues()
	if err != nil {
		t.Fatal(err)
	}
	if captured["HIKYO_LISTEN"] != "0.0.0.0:8088" || captured["HIKYO_ADMISSION_BUDGET_MIB"] != "640" || captured["HIKYO_PG_POOL_MAX"] != "7" {
		t.Fatal("seed lost effective flags or numeric settings")
	}
	for _, alias := range managedNodeFiles {
		if captured[alias.target] != contents[alias.source] {
			t.Fatalf("%s imported different bytes", alias.target)
		}
		if _, exists := captured[alias.source]; exists {
			t.Fatal("file path became a managed node value")
		}
		if err := os.Remove(input[alias.source]); err != nil {
			t.Fatal(err)
		}
	}
	seed, err := cfg.ManagedSeedForNode(captured)
	if err != nil {
		t.Fatalf("owner seed reopened a node source: %v", err)
	}
	owner := make(map[string]string)
	for _, name := range ManagedOwnerKeys() {
		if value, present := seed[name]; present {
			owner[name] = value
		}
	}
	effective, err := ApplyManagedOwnerAndNodeValues(cfg, owner, captured)
	if err != nil {
		t.Fatalf("managed application reopened source files: %v", err)
	}
	if effective.TLSCertFile != "" || effective.TLSKeyFile != "" || effective.TLSCertPEM != "\n"+cert || effective.TLSKeyPEM != key+"\n" {
		t.Fatal("managed TLS did not retain exact content and clear source paths")
	}
	if effective.AdapterEgressPolicy["https://adapter.example"][0].String() != "10.0.0.0/8" || effective.Store.DSN != cfg.Store.DSN || effective.Store.PostgresPoolMax != 7 {
		t.Fatal("node policy or bootstrap engine/DSN changed")
	}
	captured["HIKYO_TLS_KEY_PEM"] = "changed"
	if effective.TLSKeyPEM != key+"\n" {
		t.Fatal("effective config aliases node input")
	}
}

func TestManagedNodeSourcesStayDeferredAfterAdoptionAndFailNewSeedSafely(t *testing.T) {
	input := map[string]string{"HIKYO_PG_POOL_MAX": "do-not-disclose", "HIKYO_TLS_CERT_FILE": "/do-not-disclose/missing", "HIKYO_ADAPTER_EGRESS_POLICY_FILE": "/do-not-disclose/policy"}
	cfg, _, err := LoadBootstrap("server", []string{"--dev"}, func(key string) string { return input[key] }, nil)
	if err != nil {
		t.Fatalf("stale node sources blocked bootstrap: %v", err)
	}
	_, err = cfg.SeedNodeValues()
	if err == nil || strings.Contains(err.Error(), "do-not-disclose") {
		t.Fatalf("new adoption did not refuse bad source safely: %v", err)
	}
}

func TestManagedNodeValidationRejectsUnsafeContentAndUnknownInputs(t *testing.T) {
	cert, key := managedNodeTestTLS(t)
	_, otherKey := managedNodeTestTLS(t)
	tests := map[string]map[string]string{
		"raw path":                {"HIKYO_TLS_KEY_FILE": "/do-not-disclose"},
		"root":                    {"HIKYO_ROOT_KEY": "do-not-disclose"},
		"database":                {"HIKYO_DB": "postgres://do-not-disclose"},
		"node identity":           {"HIKYO_NODE_ID": "other-node"},
		"HA mode":                 {"HIKYO_HA": "true"},
		"malformed dev override":  {"HIKYO_DEV_SERVICE_BUDGETS_DISABLED": "sometimes"},
		"unpaired TLS":            {"HIKYO_TLS_CERT_PEM": cert},
		"mismatched TLS":          {"HIKYO_TLS_CERT_PEM": cert, "HIKYO_TLS_KEY_PEM": otherKey},
		"private key in chain":    {"HIKYO_TLS_CERT_PEM": cert + key, "HIKYO_TLS_KEY_PEM": key},
		"garbage before chain":    {"HIKYO_TLS_CERT_PEM": "do-not-disclose\n" + cert, "HIKYO_TLS_KEY_PEM": key},
		"duplicate policy origin": {"HIKYO_ADAPTER_EGRESS_POLICY_JSON": `{"https://adapter.example":[],"https://adapter.example":["10.0.0.0/8"]}`},
		"null policy":             {"HIKYO_OIDC_EGRESS_POLICY_JSON": "null"},
		"null policy array":       {"HIKYO_OIDC_EGRESS_POLICY_JSON": `{"https://identity.example":null}`},
		"credential origin":       {"HIKYO_OIDC_EGRESS_POLICY_JSON": `{"https://user:do-not-disclose@identity.example":[]}`},
		"dynamic password":        {"HIKYO_DYNAMIC_EGRESS_POLICY_JSON": `{"postgres://user:do-not-disclose@db.example/app":[]}`},
		"bad CIDR":                {"HIKYO_ADAPTER_EGRESS_POLICY_JSON": `{"https://adapter.example":["do-not-disclose"]}`},
	}
	for name, extra := range tests {
		t.Run(name, func(t *testing.T) {
			values := managedNodeTestValues()
			maps.Copy(values, extra)
			err := ValidateManagedNodeValues(values)
			if err == nil || strings.Contains(err.Error(), "do-not-disclose") {
				t.Fatalf("unsafe node validation result: %v", err)
			}
		})
	}
}

func TestManagedNodeOverridesAreStrictBoundedAndCanonical(t *testing.T) {
	node := managedNodeTestValues()
	raw, err := EncodeManagedNodeOverrides(map[string]map[string]string{"node-a": node})
	if err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]string{
		"version":              strings.Replace(raw, `"version":1`, `"version":2`, 1),
		"duplicate version":    strings.Replace(raw, `"version":1`, `"version":1,"version":1`, 1),
		"case alias":           strings.Replace(raw, `"version":1`, `"Version":1`, 1),
		"unknown outer":        strings.Replace(raw, `"version":1`, `"version":1,"extra":true`, 1),
		"duplicate node":       strings.Replace(raw, `"node-a":`, `"node-a":{},"node-a":`, 1),
		"duplicate key":        strings.Replace(raw, `"HIKYO_LISTEN":`, `"HIKYO_LISTEN":"127.0.0.1:1","HIKYO_LISTEN":`, 1),
		"null string":          strings.Replace(raw, `"272"`, `null`, 1),
		"null node":            `{"version":1,"nodes":{"node-a":null}}`,
		"wildcard":             strings.Replace(raw, `"node-a"`, `"*"`, 1),
		"unknown node setting": strings.Replace(raw, `"HIKYO_LISTEN"`, `"HIKYO_ROOT_KEY"`, 1),
		"oversized":            strings.Repeat(" ", MaxManagedNodeValueBytes) + raw,
		"trailing":             raw + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManagedNodeOverrides(invalid); err == nil {
				t.Fatal("invalid node document accepted")
			}
		})
	}
	decoded, err := ParseManagedNodeOverrides(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeManagedNodeOverrides(decoded)
	if err != nil || encoded != raw {
		t.Fatalf("node document is not canonical: %v", err)
	}
	decoded["node-a"]["HIKYO_LISTEN"] = "changed"
	again, err := ParseManagedNodeOverrides(raw)
	if err != nil || !reflect.DeepEqual(again["node-a"], node) {
		t.Fatal("decoded node values share mutable state")
	}
	invalidUTF8 := managedNodeTestValues()
	invalidUTF8["HIKYO_BACKUP_DIR"] = string([]byte{0xff})
	if _, err := EncodeManagedNodeOverrides(map[string]map[string]string{"node-a": invalidUTF8}); err == nil {
		t.Fatal("encoder silently replaced invalid UTF-8 bytes")
	}
}

func TestManagedNodeAndOwnerChangesValidateTogetherAndClearOldNodeContent(t *testing.T) {
	base := &Config{Listen: "0.0.0.0:8080", OperationalListen: "127.0.0.1:8081", TLSCertFile: "/stale/cert", TLSKeyFile: "/stale/key", BackupDir: "/stale/backups", Store: Datastore{Engine: EngineSQLite, Path: "unchanged.db"}}
	owner := map[string]string{"HIKYO_EXTERNAL_ORIGIN": "https://node.example"}
	node := managedNodeTestValues()
	node["HIKYO_LISTEN"] = "0.0.0.0:8080"
	node["HIKYO_TRUSTED_PROXY_CIDRS"] = "192.168.0.0/16"
	effective, err := ApplyManagedOwnerAndNodeValues(base, owner, node)
	if err != nil {
		t.Fatal(err)
	}
	if effective.TLSCertFile != "" || effective.TLSKeyFile != "" || effective.BackupDir != "" || len(effective.TrustedProxyCIDRs) != 1 {
		t.Fatal("managed node kept stale paths or lost exact node proxy policy")
	}
	owner["HIKYO_TRUSTED_PROXY_CIDRS"] = "10.0.0.0/8"
	if _, err := ApplyManagedOwnerAndNodeValues(base, owner, node); err == nil {
		t.Fatal("shared owner proxy trust accepted")
	}
	delete(owner, "HIKYO_TRUSTED_PROXY_CIDRS")
	base.TrustedProxyCIDRs = []string{"10.0.0.0/8"}
	delete(node, "HIKYO_TRUSTED_PROXY_CIDRS")
	if _, err := ApplyManagedOwnerAndNodeValues(base, owner, node); err == nil {
		t.Fatal("absent node proxy revived bootstrap trust")
	}
	node["HIKYO_TRUSTED_PROXY_CIDRS"] = ""
	if _, err := ApplyManagedOwnerAndNodeValues(base, owner, node); err == nil {
		t.Fatal("explicit node proxy removal bypassed plaintext listener safety")
	}
	node = managedNodeTestValues()
	node["HIKYO_PG_POOL_MAX"] = "3"
	if _, err := ApplyManagedOwnerAndNodeValues(base, nil, node); err == nil {
		t.Fatal("PostgreSQL pool setting accepted for SQLite")
	}
}

func TestNodeSeedRecordsEmptyProxyPolicyAndKeepsUnequalHAIngressOutOfOwnerSeed(t *testing.T) {
	var shared map[string]string
	for _, proxy := range []string{"", "192.168.0.0/16", "10.0.0.0/8"} {
		cfg, _, err := LoadBootstrap("server", []string{"--dev"}, func(key string) string {
			if key == "HIKYO_TRUSTED_PROXY_CIDRS" {
				return proxy
			}
			return ""
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		node, err := cfg.SeedNodeValues()
		if err != nil {
			t.Fatal(err)
		}
		actual, present := node["HIKYO_TRUSTED_PROXY_CIDRS"]
		if !present || actual != proxy {
			t.Fatal("node seed did not record exact empty/nonempty proxy policy")
		}
		owner, err := cfg.ManagedSeedForNode(node)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := owner["HIKYO_TRUSTED_PROXY_CIDRS"]; exists {
			t.Fatal("node ingress entered owner fingerprint")
		}
		if shared == nil {
			shared = owner
		} else if !maps.Equal(shared, owner) {
			t.Fatal("different HA ingress policies changed shared owner seed")
		}
		delete(node, "HIKYO_TRUSTED_PROXY_CIDRS")
		if _, err := cfg.ManagedSeedForNode(node); err == nil {
			t.Fatal("incomplete seed silently inherited an ingress policy")
		}
	}
}

func TestManagedNodeTLSImportRequiresPrivateKeyPermissions(t *testing.T) {
	cert, key := managedNodeTestTLS(t)
	certPath, keyPath := filepath.Join(t.TempDir(), "cert.pem"), filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(certPath, []byte(cert), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte(key), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{TLSCertFile: certPath, TLSKeyFile: keyPath}
	for _, mode := range []os.FileMode{0400, 0600, 0644, 0660, 0700} {
		if err := os.Chmod(keyPath, mode); err != nil {
			t.Fatal(err)
		}
		_, err := cfg.SeedNodeValues()
		if allowed := mode == 0400 || mode == 0600; allowed != (err == nil) {
			t.Fatalf("TLS seed key mode %04o: %v", mode, err)
		}
	}
}

func TestManagedDevelopmentNodeControlsRequireDevelopmentContext(t *testing.T) {
	keys := map[string]string{
		"HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE": "500",
		"HIKYO_DEV_SERVICE_BUDGETS_DISABLED":    "true",
		"HIKYO_DEV_ADAPTER_FAKE_PROVIDER":       "true",
	}
	for key, value := range keys {
		t.Run(key, func(t *testing.T) {
			values := map[string]string{"HIKYO_LISTEN": "127.0.0.1:8080", "HIKYO_OPERATIONAL_LISTEN": "127.0.0.1:8081", "HIKYO_ADMISSION_BUDGET_MIB": "272", key: value}
			if err := ValidateManagedNodeValues(values); err != nil {
				t.Fatal(err)
			}
			if _, err := ApplyManagedNodeValues(&Config{}, values); err == nil {
				t.Fatal("production accepted a development-only node setting")
			}
			for _, disabled := range []string{"false", "0"} {
				values[key] = disabled
				if _, err := ApplyManagedNodeValues(&Config{}, values); err == nil {
					t.Fatal("production accepted a present development setting with a disabled value")
				}
			}
		})
	}
	base, _, err := Load("server", []string{"--dev"}, func(string) string { return "" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"HIKYO_LISTEN": "127.0.0.1:8080", "HIKYO_OPERATIONAL_LISTEN": "127.0.0.1:8081", "HIKYO_ADMISSION_BUDGET_MIB": "272"}
	for key, value := range keys {
		values[key] = value
	}
	got, err := ApplyManagedNodeValues(base, values)
	if err != nil {
		t.Fatal(err)
	}
	if got.DevAdmissionPerIPPerMinute != 500 || !got.DevServiceBudgetsDisabled || !got.DevAdapterFakeProvider {
		t.Fatal("development node controls were not applied")
	}
	for key := range keys {
		delete(values, key)
	}
	got, err = ApplyManagedNodeValues(got, values)
	if err != nil {
		t.Fatal(err)
	}
	if got.DevAdmissionPerIPPerMinute != 0 || got.DevServiceBudgetsDisabled || got.DevAdapterFakeProvider {
		t.Fatal("removed controls inherited stale startup values")
	}
}

func TestManagedDevelopmentSeedImportsEffectiveControlsAndRefusesProduction(t *testing.T) {
	inputs := map[string]string{"HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE": "500", "HIKYO_DEV_SERVICE_BUDGETS_DISABLED": "true", "HIKYO_DEV_ADAPTER_FAKE_PROVIDER": "true"}
	for _, development := range []bool{false, true} {
		args := []string{}
		if development {
			args = append(args, "--dev")
		}
		getenv := func(key string) string {
			if key == "HIKYO_DB" {
				return "sqlite:unused.db"
			}
			return inputs[key]
		}
		cfg, _, err := LoadBootstrap("server", args, getenv, nil)
		if err != nil {
			t.Fatal(err)
		}
		seed, err := cfg.SeedNodeValues()
		if !development {
			if err == nil {
				t.Fatal("production imported development controls")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range inputs {
			if seed[key] != want {
				t.Fatalf("seed %s = %q, want %q", key, seed[key], want)
			}
		}
	}
}

func TestManagedDevelopmentControlsRejectMalformedAndOutOfRangeValues(t *testing.T) {
	for _, input := range []struct{ key, value string }{
		{"HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE", "0"},
		{"HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE", "-1"},
		{"HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE", "2147483648"},
		{"HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE", "1.5"},
		{"HIKYO_DEV_SERVICE_BUDGETS_DISABLED", "sometimes"},
		{"HIKYO_DEV_ADAPTER_FAKE_PROVIDER", "maybe"},
	} {
		values := managedNodeTestValues()
		values[input.key] = input.value
		if err := ValidateManagedNodeValues(values); err == nil {
			t.Fatalf("invalid %s accepted", input.key)
		}
	}
}
