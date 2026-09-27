package cli

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestAccessCapabilityFlag(t *testing.T) {
	t.Parallel()
	var caps capabilityList
	for _, input := range []string{"read, reveal", "reveal-history", " edit ,publish,pin "} {
		if err := caps.Set(input); err != nil {
			t.Fatal(err)
		}
	}
	want := capabilityList{"read", "reveal", "reveal-history", "edit", "publish", "pin"}
	if !slices.Equal(caps, want) {
		t.Fatalf("capabilities = %v, want %v", caps, want)
	}
	for _, input := range []string{"", " ", ",read", "read,", "read,,reveal"} {
		t.Run(input, func(t *testing.T) {
			var got capabilityList
			if err := got.Set(input); err == nil {
				t.Fatalf("accepted empty capability in %q", input)
			}
		})
	}
}

func TestAccessRequestBodies(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		duration time.Duration
		seconds  int32
	}{
		{"minimum", time.Second, 1},
		{"fractional seconds", 1500 * time.Millisecond, 1},
		{"hour", time.Hour, 3600},
		{"maximum", time.Duration(math.MaxInt32) * time.Second, math.MaxInt32},
	} {
		t.Run(tt.name, func(t *testing.T) {
			caps := capabilityList{"read", "reveal"}
			ordinary := accessRequestBody(caps, "incident 42", tt.duration)
			emergency := emergencyAccessBody(caps, "incident 42", tt.duration)
			if ordinary.DurationSeconds != tt.seconds || emergency.DurationSeconds == nil || *emergency.DurationSeconds != tt.seconds {
				t.Fatalf("duration: ordinary=%+v emergency=%+v", ordinary, emergency)
			}
			if ordinary.Reason != "incident 42" || emergency.Reason != ordinary.Reason || !slices.Equal(ordinary.Capabilities, caps) || !slices.Equal(emergency.Capabilities, caps) {
				t.Fatalf("request content lost: ordinary=%+v emergency=%+v", ordinary, emergency)
			}
		})
	}
	body := emergencyAccessBody(capabilityList{"read"}, "incident", 0)
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if _, exists := wire["duration_seconds"]; exists || body.DurationSeconds != nil {
		t.Fatalf("unset emergency duration must be omitted for server default: %s", encoded)
	}
}

func TestAccessSyntaxRejectedBeforeAuthentication(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"policy missing capability", []string{"policy", "create", "--approver", "principal:usr_one"}, "--capability"},
		{"policy missing approver", []string{"policy", "create", "--capability", "read"}, "--approver"},
		{"policy subsecond maximum", []string{"policy", "create", "--capability", "read", "--approver", "principal:usr_one", "--max-duration", "999ms"}, "--max-duration"},
		{"policy maximum overflow", []string{"policy", "create", "--capability", "read", "--approver", "principal:usr_one", "--max-duration", "2147483648s"}, "--max-duration"},
		{"policy quorum overflow", []string{"policy", "create", "--capability", "read", "--approver", "principal:usr_one", "--min-approvals", "2147483648"}, "--min-approvals"},
		{"policy ttl zero", []string{"policy", "create", "--capability", "read", "--approver", "principal:usr_one", "--ttl", "0"}, "--ttl"},
		{"policy ttl overflow", []string{"policy", "create", "--capability", "read", "--approver", "principal:usr_one", "--ttl", "2147483648"}, "--ttl"},
		{"update missing id", []string{"policy", "update"}, "<policy>"},
		{"delete extra ids", []string{"policy", "delete", "one", "two"}, "<policy>"},
		{"request missing reason", []string{"request", "create", "--capability", "read", "--duration", "1h"}, "--reason"},
		{"request missing capability", []string{"request", "create", "--reason", "incident", "--duration", "1h"}, "--capability"},
		{"request missing duration", []string{"request", "create", "--capability", "read", "--reason", "incident"}, "--duration"},
		{"request subsecond duration", []string{"request", "create", "--capability", "read", "--reason", "incident", "--duration", "999ms"}, "--duration"},
		{"request duration overflow", []string{"request", "create", "--capability", "read", "--reason", "incident", "--duration", "2147483648s"}, "--duration"},
		{"emergency negative duration", []string{"request", "emergency", "--capability", "read", "--reason", "incident", "--duration", "-1s"}, "--duration"},
		{"emergency duration overflow", []string{"request", "emergency", "--capability", "read", "--reason", "incident", "--duration", "2147483648s"}, "--duration"},
		{"approve missing id", []string{"request", "approve"}, "<request>"},
		{"reject missing id", []string{"request", "reject"}, "<request>"},
		{"cancel missing id", []string{"request", "cancel"}, "<request>"},
		{"revoke extra ids", []string{"request", "revoke", "one", "two"}, "<request>"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ios, _, stderr := composeIO(t.TempDir(), t.TempDir(), "", nil)
			code := Run(t.Context(), ios, append([]string{"access"}, tt.args...))
			if code != ExitUsage || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("exit=%d stderr=%q; want usage error naming %q", code, stderr, tt.want)
			}
		})
	}
}

func TestAccessRequestTable(t *testing.T) {
	t.Parallel()
	expires := time.Date(2026, 9, 27, 12, 0, 0, 0, time.FixedZone("offset", 2*60*60))
	requests := []apigen.AccessRequest{
		{Id: "open", Requester: "alice", Capabilities: []apigen.AccessCapability{"read", "reveal"}, State: "open", Approvals: 1, MinApprovals: 2},
		{Id: "emergency", Requester: "bob", Capabilities: []apigen.AccessCapability{"edit"}, State: "granted", Bypassed: true, ExpiresAt: &expires},
	}
	queue := apigen.AccessQueue{Items: requests, Offer: &apigen.AccessOffer{PolicyId: "policy"}}
	got := accessRequestTable(requests, &queue)
	want := [][]string{{"open", "alice", "read,reveal", "open", "1/2", "-"}, {"emergency", "bob", "edit", "granted (emergency)", "0/0", "2026-09-27T10:00:00Z"}}
	if !reflect.DeepEqual(got.Rows, want) {
		t.Fatalf("rows = %v, want %v", got.Rows, want)
	}
	if !reflect.DeepEqual(got.JSON, queue) {
		t.Fatal("queue offer lost from JSON output")
	}
	if got := accessRequestTable(requests[:1], nil).JSON; !reflect.DeepEqual(got, apigen.AccessQueue{Items: requests[:1]}) {
		t.Fatalf("single request JSON = %+v", got)
	}
}

func TestAccessPolicyUpdatePreservesOmittedFields(t *testing.T) {
	current := apigen.AccessPolicy{EnvironmentId: "env_production", Capabilities: []apigen.AccessCapability{"read"}, MaxDurationSeconds: 60, MinApprovals: 2, RequestTtlSeconds: 120, Enabled: false, AllowSelfApproval: true, Bypassers: []string{"usr_one"}, Approvers: []apigen.ApprovalApprover{{Kind: "principal", SubjectId: "usr_two"}}}
	empty, no := "", false
	defaults := apigen.AccessPolicyInput{EnvironmentId: &empty, MaxDurationSeconds: 28800, MinApprovals: 1, RequestTtlSeconds: 86400, Enabled: true, AllowSelfApproval: &no}
	got := mergeAccessPolicyInput(current, defaults, nil)
	if *got.EnvironmentId != current.EnvironmentId || got.MaxDurationSeconds != 60 || got.MinApprovals != 2 || got.RequestTtlSeconds != 120 || got.Enabled || !*got.AllowSelfApproval || !slices.Equal(*got.Bypassers, current.Bypassers) || !slices.Equal(got.Capabilities, current.Capabilities) || !reflect.DeepEqual(got.Approvers, current.Approvers) {
		t.Fatalf("omissions changed policy: %+v", got)
	}
	explicit := mergeAccessPolicyInput(current, defaults, map[string]bool{"covers": true, "allow-self-approval": true, "disabled": true})
	if *explicit.EnvironmentId != "" || *explicit.AllowSelfApproval || !explicit.Enabled {
		t.Fatalf("explicit empty/false ignored: %+v", explicit)
	}
}

func TestAccessReauthWithoutInlineTOTPReturnsAuthRefusal(t *testing.T) {
	for _, keys := range []bool{false, true} {
		d := disclosure{purpose: "access", window: func(context.Context, string) (apigen.RevealWindow, error) { return apigen.RevealWindow{}, nil }}
		if keys {
			d.keys = func(context.Context, string) ([]string, error) {
				t.Error("access tried browser key resolution")
				return nil, nil
			}
		}
		err := ensureRevealWindow(t.Context(), nil, nil, IO{OpenURL: func(string) error { t.Error("opened unsupported access browser handoff"); return nil }}, nil, "", "env_one", d, errors.New("reauth required"))
		var refusal *Error
		if !errors.As(err, &refusal) || refusal.Code != ExitAuth {
			t.Fatalf("keys=%v refusal=%v", keys, err)
		}
	}
}
