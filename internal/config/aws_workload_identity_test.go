package config

import "testing"

// HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY is the node operator's opt-in for AWS
// adapters that borrow this node's own identity (#158). Only the two explicit
// words parse; the managed node projection carries it and nothing else
// revives it.
func TestAWSWorkloadIdentityOptIn(t *testing.T) {
	for raw, want := range map[string]bool{"": false, "deny": false, "allow": true} {
		got, err := parseAWSWorkloadIdentity(raw)
		if err != nil || got != want {
			t.Fatalf("parse(%q) = %v, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"true", "1", "ALLOW", "yes"} {
		if _, err := parseAWSWorkloadIdentity(raw); err == nil {
			t.Fatalf("parse(%q) accepted a non-explicit word", raw)
		}
	}
	input := map[string]string{"HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY": "allow"}
	standalone, _, err := Load("server", []string{"--dev"}, func(key string) string { return input[key] }, nil)
	if err != nil || !standalone.AdapterAWSWorkloadIdentity {
		t.Fatalf("standalone allow = %v, %v", standalone != nil && standalone.AdapterAWSWorkloadIdentity, err)
	}
	// The bootstrap loader defers node keys to the managed projection: the
	// raw input must reach the node seed rather than being dropped.
	cfg, _, err := LoadBootstrap("server", []string{"--dev"}, func(key string) string { return input[key] }, nil)
	if err != nil {
		t.Fatal(err)
	}
	seeded, err := cfg.SeedNodeValues()
	if err != nil {
		t.Fatal(err)
	}
	if seeded["HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY"] != "allow" {
		t.Fatalf("node seed = %q", seeded["HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY"])
	}
	values := managedNodeTestValues()
	values["HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY"] = "allow"
	applied, err := parseManagedNodeValues(&Config{}, values)
	if err != nil || !applied.AdapterAWSWorkloadIdentity {
		t.Fatalf("managed node allow = %v, %v", applied, err)
	}
	// A projection without the key turns the opt-in off; it is never inherited.
	applied, err = parseManagedNodeValues(&Config{AdapterAWSWorkloadIdentity: true}, managedNodeTestValues())
	if err != nil || applied.AdapterAWSWorkloadIdentity {
		t.Fatalf("managed node without the key = %v, %v", applied.AdapterAWSWorkloadIdentity, err)
	}
	values["HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY"] = "on"
	if _, err := parseManagedNodeValues(&Config{}, values); err == nil {
		t.Fatal("managed node accepted an invalid opt-in word")
	}
	bad := map[string]string{"HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY": "enabled"}
	if _, _, err := Load("server", []string{"--dev"}, func(key string) string { return bad[key] }, nil); err == nil {
		t.Fatal("startup accepted an invalid opt-in word")
	}
}
