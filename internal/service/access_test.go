package service

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestAccessCapabilitiesWithin(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                     string
		requested, allowed, want []string
		invalid                  bool
	}{
		{name: "canonical set", requested: []string{"reveal", "read", "reveal"}, allowed: []string{"read", "reveal"}, want: []string{"read", "reveal"}},
		{name: "all requestable capabilities", requested: []string{"pin", "publish", "edit", "reveal-history", "reveal", "read"}, allowed: RequestableCapabilities(), want: []string{"edit", "pin", "publish", "read", "reveal", "reveal-history"}},
		{name: "empty request", allowed: []string{"read"}, invalid: true},
		{name: "empty offer", requested: []string{"read"}, invalid: true},
		{name: "outside offer", requested: []string{"read", "reveal"}, allowed: []string{"read"}, invalid: true},
		{name: "admin capability even if offered", requested: []string{"manage-members"}, allowed: []string{"manage-members"}, invalid: true},
		{name: "unknown capability even if offered", requested: []string{"unknown"}, allowed: []string{"unknown"}, invalid: true},
		{name: "empty capability", requested: []string{"read", ""}, allowed: []string{"read", ""}, invalid: true},
		{name: "case sensitive", requested: []string{"Read"}, allowed: []string{"Read"}, invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before, offer := slices.Clone(tt.requested), slices.Clone(tt.allowed)
			got, err := accessCapabilitiesWithin(tt.requested, tt.allowed)
			if tt.invalid {
				if !errors.Is(err, ErrAccessExceedsPolicy) || !errors.Is(err, domain.ErrInvalid) || got != nil {
					t.Fatalf("got %v, %v; want no capabilities and ErrAccessExceedsPolicy wrapping ErrInvalid", got, err)
				}
			} else if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tt.want)
			}
			if !slices.Equal(tt.requested, before) || !slices.Equal(tt.allowed, offer) {
				t.Fatal("validation mutated the request or policy offer")
			}
		})
	}
}

func TestRequestableCapabilitiesReturnsIndependentSlice(t *testing.T) {
	t.Parallel()
	want := []string{"read", "reveal", "reveal-history", "edit", "publish", "pin"}
	got := RequestableCapabilities()
	if !slices.Equal(got, want) {
		t.Fatalf("capabilities = %v, want %v", got, want)
	}
	got[0] = "manage-members"
	if !slices.Equal(RequestableCapabilities(), want) {
		t.Fatal("caller changed the requestable capability set")
	}
}

func TestValidateAccessPolicyInput(t *testing.T) {
	t.Parallel()
	valid := func() AccessPolicyInput {
		return AccessPolicyInput{Capabilities: []string{"reveal", "read", "reveal"}, MaxDurationSeconds: 3600, MinApprovals: 1, RequestTTLSeconds: 86400, Approvers: []ApprovalApproverSpec{{Kind: "principal", SubjectID: "usr_approver"}}}
	}
	for _, tt := range []struct {
		name    string
		change  func(*AccessPolicyInput)
		invalid bool
	}{
		{"ordinary policy", func(p *AccessPolicyInput) {}, false},
		{"minimum bounds", func(p *AccessPolicyInput) { p.MaxDurationSeconds, p.MinApprovals, p.RequestTTLSeconds = 1, 1, 1 }, false},
		{"postgres maximum bounds", func(p *AccessPolicyInput) {
			p.MaxDurationSeconds, p.MinApprovals, p.RequestTTLSeconds = math.MaxInt32, math.MaxInt32, math.MaxInt32
		}, false},
		{"group approver", func(p *AccessPolicyInput) {
			p.Approvers = []ApprovalApproverSpec{{Kind: "scim_group", SubjectID: "group", BindingID: "binding"}}
		}, false},
		{"zero duration", func(p *AccessPolicyInput) { p.MaxDurationSeconds = 0 }, true},
		{"negative duration", func(p *AccessPolicyInput) { p.MaxDurationSeconds = -1 }, true},
		{"duration overflow", func(p *AccessPolicyInput) { p.MaxDurationSeconds = math.MaxInt32 + 1 }, true},
		{"zero quorum", func(p *AccessPolicyInput) { p.MinApprovals = 0 }, true},
		{"negative quorum", func(p *AccessPolicyInput) { p.MinApprovals = -1 }, true},
		{"quorum overflow", func(p *AccessPolicyInput) { p.MinApprovals = math.MaxInt32 + 1 }, true},
		{"zero ttl", func(p *AccessPolicyInput) { p.RequestTTLSeconds = 0 }, true},
		{"negative ttl", func(p *AccessPolicyInput) { p.RequestTTLSeconds = -1 }, true},
		{"ttl overflow", func(p *AccessPolicyInput) { p.RequestTTLSeconds = math.MaxInt32 + 1 }, true},
		{"no approvers", func(p *AccessPolicyInput) { p.Approvers = nil }, true},
		{"principal without subject", func(p *AccessPolicyInput) { p.Approvers[0].SubjectID = "" }, true},
		{"unknown approver kind", func(p *AccessPolicyInput) { p.Approvers[0].Kind = "unknown" }, true},
		{"group without binding", func(p *AccessPolicyInput) { p.Approvers[0].Kind = "scim_group" }, true},
		{"group without subject", func(p *AccessPolicyInput) {
			p.Approvers = []ApprovalApproverSpec{{Kind: "scim_group", BindingID: "binding"}}
		}, true},
		{"too many approvers", func(p *AccessPolicyInput) { p.Approvers = make([]ApprovalApproverSpec, maxApprovalPolicyMembers+1) }, true},
		{"too many bypassers", func(p *AccessPolicyInput) { p.Bypassers = make([]string, maxApprovalPolicyMembers+1) }, true},
		{"no capabilities", func(p *AccessPolicyInput) { p.Capabilities = nil }, true},
		{"administrative capability", func(p *AccessPolicyInput) { p.Capabilities = []string{"manage-members"} }, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := valid()
			tt.change(&input)
			got, err := validateAccessPolicyInput(input)
			if tt.invalid {
				if !errors.Is(err, domain.ErrInvalid) || got != nil {
					t.Fatalf("got %v, %v; want ErrInvalid and no capabilities", got, err)
				}
			} else if err != nil || !slices.Equal(got, []string{"read", "reveal"}) {
				t.Fatalf("got %v, %v; want canonical capabilities", got, err)
			}
		})
	}
}

func TestAccessReason(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, raw, want string
		invalid         bool
	}{
		{name: "ordinary", raw: "Investigate incident 42", want: "Investigate incident 42"},
		{name: "empty", invalid: true},
		{name: "controls only", raw: "\n\t\x00\x7f", invalid: true},
		{name: "strip controls", raw: "incident\n42\x00", want: "incident42"},
		{name: "invalid utf8", raw: "incident\xff", want: "incident�"},
		{name: "token redaction", raw: "investigate hik_1_sess_0123456789ABCDEF", want: "investigate [REDACTED:hikyo-token]"},
		{name: "exact byte limit", raw: strings.Repeat("a", 512), want: strings.Repeat("a", 512)},
		{name: "over byte limit", raw: strings.Repeat("a", 513), invalid: true},
		{name: "multibyte limit", raw: strings.Repeat("é", 256), want: strings.Repeat("é", 256)},
		{name: "multibyte overflow", raw: strings.Repeat("é", 257), invalid: true},
		{name: "limit after sanitization", raw: strings.Repeat("a", 512) + "\n", want: strings.Repeat("a", 512)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := accessReason(tt.raw)
			if tt.invalid {
				if !errors.Is(err, domain.ErrInvalid) || got != "" {
					t.Fatalf("got %q, %v; want empty reason and ErrInvalid", got, err)
				}
			} else if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestAccessInteractiveHuman(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		identity authz.Identity
		want     bool
	}{
		{"human session", authz.Identity{Class: domain.ClassHuman, SessionID: "session"}, true},
		{"human without session", authz.Identity{Class: domain.ClassHuman}, false},
		{"workload with session", authz.Identity{Class: domain.ClassWorkload, SessionID: "session"}, false},
		{"automation with session", authz.Identity{Class: domain.ClassAutomation, SessionID: "session"}, false},
		{"unclassified session", authz.Identity{SessionID: "session"}, false},
		{"zero identity", authz.Identity{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := interactiveHuman(tt.identity); got != tt.want {
				t.Fatalf("interactiveHuman = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestAccessBypassReauthIntent(t *testing.T) {
	t.Parallel()
	intent, err := NewAccessBypassReauthIntent("env_prod")
	if err != nil {
		t.Fatal(err)
	}
	purpose, err := intent.Purpose()
	if err != nil || purpose != PurposeAccess {
		t.Fatalf("purpose = %q, %v", purpose, err)
	}
	operation, err := intent.Operation()
	if err != nil || operation != authz.OpAccessBypass {
		t.Fatalf("operation = %q, %v", operation, err)
	}
	if intent.environmentID != "env_prod" || len(intent.keyIDs) != 0 {
		t.Fatalf("wrong environment-only binding: %+v", intent)
	}
	binding, err := intent.bindingFor("")
	if err != nil {
		t.Fatal(err)
	}
	if binding.purpose != PurposeAccess || binding.operation != authz.OpAccessBypass || binding.environmentID != "env_prod" || binding.keySet != "" {
		t.Fatalf("wrong emergency binding: %+v", binding)
	}
	if binding.challengeBinding != `{"operation":"access","environment_id":"env_prod","key_ids":null}` {
		t.Fatalf("challenge binding = %s", binding.challengeBinding)
	}
	if _, err := intent.bindingFor("env_other"); !errors.Is(err, ErrReauthUnitMismatch) {
		t.Fatalf("cross-environment binding: %v", err)
	}
	fromOperation, err := newReauthIntentForOperation(authz.OpAccessBypass, "env_prod", nil)
	if err != nil || !reflect.DeepEqual(intent, fromOperation) {
		t.Fatalf("operation constructor differs: %+v, %v", fromOperation, err)
	}
	if _, err := NewAccessBypassReauthIntent(""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty environment: %v", err)
	}
	if _, err := NewDisclosureReauthIntent(PurposeAccess, []string{"env_prod", "env_dev"}, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("multiple environments: %v", err)
	}
	if cliReauthPurposeOperation(PurposeAccess, authz.OpAccessBypass) {
		t.Fatal("emergency access admitted to unsupported CLI handoff")
	}
}
