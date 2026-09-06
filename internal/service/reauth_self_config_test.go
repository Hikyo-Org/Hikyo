package service

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
)

func TestSelfConfigIntentCannotWiden(t *testing.T) {
	target := SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: "instance-a", Revision: 2, SchemaVersion: 1, ExpectedGeneration: 4}
	intent, err := NewSelfConfigReauthIntent(target)
	if err != nil {
		t.Fatal(err)
	}
	original, err := intent.bindingFor("")
	if err != nil {
		t.Fatal(err)
	}
	if original.operation != authz.OpSelfConfigApply || original.purpose != PurposeSelfConfig {
		t.Fatalf("wrong binding: %+v", original)
	}
	parsed, ok, err := parseSelfConfigBinding(original.challengeBinding)
	if err != nil || !ok || parsed.keySet != intent.keySet {
		t.Fatalf("cannot round trip exact target: %v", err)
	}
	for _, change := range []func(*SelfConfigReauthTarget){func(v *SelfConfigReauthTarget) { v.OwnerInstanceID = "instance-b" }, func(v *SelfConfigReauthTarget) { v.Revision++ }, func(v *SelfConfigReauthTarget) { v.SchemaVersion++ }, func(v *SelfConfigReauthTarget) { v.ExpectedGeneration++ }, func(v *SelfConfigReauthTarget) { v.ConfirmRestoredCredentials = true }, func(v *SelfConfigReauthTarget) { v.Action = "mail-test"; v.To = "test@example.com" }} {
		other := target
		change(&other)
		different, err := NewSelfConfigReauthIntent(other)
		if err != nil {
			t.Fatal(err)
		}
		if different.keySet == intent.keySet {
			t.Fatal("different decision shares authorization")
		}
	}
	if _, err := NewDisclosureReauthIntent(PurposeSelfConfig, []string{"env-one"}, nil); err == nil {
		t.Fatal("disclosure constructor admitted self config")
	}
	for _, bad := range []SelfConfigReauthTarget{{Action: "apply", OwnerInstanceID: "a", Revision: 1, SchemaVersion: 1, PreviewToken: "unexpected"}, {Action: "adopt", OwnerInstanceID: "a", SchemaVersion: 1}, {Action: "mail-test", OwnerInstanceID: "a", SchemaVersion: 1, Revision: 1}, {Action: "anything", OwnerInstanceID: "a", SchemaVersion: 1}} {
		if _, err := NewSelfConfigReauthIntent(bad); err == nil {
			t.Fatalf("invalid target admitted: %+v", bad)
		}
	}
}
