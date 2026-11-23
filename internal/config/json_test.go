package config

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// TestConfigDurationsKeepNanosecondIntegers pins the exact bytes
// encoding/json (v1) produced for every time.Duration in Config, in struct
// order, and the round trip back.
func TestConfigDurationsKeepNanosecondIntegers(t *testing.T) {
	want := Config{
		BackupInterval: 24 * time.Hour, BackupRPO: 26 * time.Hour, BackupRTOTarget: 30 * time.Minute, ReauthWindow: 15 * time.Minute,
		SealedWebhook: &SealedWebhookConfig{InstanceID: "i", Targets: []SealedWebhookTarget{{ID: "t", Timeout: 5 * time.Second}}},
	}
	got, err := json.Marshal(want, jsonwire.Stable)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"BackupInterval":86400000000000,"BackupRPO":93600000000000,`,
		`"BackupRTOTarget":1800000000000,`,
		`"ReauthWindow":900000000000,`,
		`"Timeout":5000000000,`,
	} {
		if !strings.Contains(string(got), fragment) {
			t.Fatalf("missing %s in %s", fragment, got)
		}
	}
	var back Config
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(back, jsonwire.Stable)
	if err != nil || string(again) != string(got) {
		t.Fatalf("round trip changed bytes: %v\n%s\n%s", err, got, again)
	}
	if back.BackupInterval != want.BackupInterval || back.BackupRPO != want.BackupRPO || back.BackupRTOTarget != want.BackupRTOTarget ||
		back.ReauthWindow != want.ReauthWindow || back.SealedWebhook.Targets[0].Timeout != 5*time.Second {
		t.Fatalf("durations changed: %+v", back)
	}
}

// TestSealedWebhookTargetDurationExactBytes pins a whole standalone target,
// including through a pointer and with default options.
func TestSealedWebhookTargetDurationExactBytes(t *testing.T) {
	target := SealedWebhookTarget{ID: "t", Timeout: 5 * time.Second, Protocol: 1}
	const want = `{"ID":"t","Origin":"","RecipientKey":"","AckKey":"","Generation":0,"Timeout":5000000000,"MaxResponseBytes":0,"ConfirmedFingerprint":"","Protocol":1,"AckKeyMLDSA65":""}`
	for name, value := range map[string]any{"value": target, "pointer": &target} {
		got, err := json.Marshal(value)
		if err != nil || string(got) != want {
			t.Fatalf("%s: %v\n%s", name, err, got)
		}
	}
	var back SealedWebhookTarget
	if err := json.Unmarshal([]byte(want), &back); err != nil || back != target {
		t.Fatalf("round trip: %v %+v", err, back)
	}
}
