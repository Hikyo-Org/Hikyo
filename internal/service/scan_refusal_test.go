package service

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// captureScanRefusal is the one Surface-2 refusal path all three scan ingresses
// share (ADR §7): it captures the finding_blocked events that survive the
// rollback and returns the typed refusal. These tests pin its three outcomes.

var refusalScope = domain.Scope{Org: "org_a", Project: "proj_b", Env: "env_c"}

// A result that does not refuse captures nothing and returns nil, so the caller
// proceeds to emit its override events with the write.
func TestCaptureScanRefusalNil(t *testing.T) {
	az := authz.NewTxAuthorizer(nil, nil)
	err := captureScanRefusal(t.Context(), az, "prin_1", refusalScope, declScanResult{
		overridden: []overrideAck{{ruleID: "r1", locator: "a.b"}},
	})
	if err != nil {
		t.Fatalf("no-refusal result: want nil error, got %v", err)
	}
	if got := len(az.PendingDenials()); got != 0 {
		t.Fatalf("no-refusal result: want 0 captured events, got %d", got)
	}
}

// A rejections-only result refuses with zero blocked findings: it captures no
// events (there is no blocked finding to stamp) yet still returns the typed
// refusal carrying the named tokens.
func TestCaptureScanRefusalRejectionsOnly(t *testing.T) {
	az := authz.NewTxAuthorizer(nil, nil)
	res := declScanResult{rejections: []scanRejection{{Index: 0, Reason: rejectSurplus}}}
	err := captureScanRefusal(t.Context(), az, "prin_1", refusalScope, res)

	var refusal *scanRefusalErr
	if !errors.As(err, &refusal) {
		t.Fatalf("rejections-only result: want *scanRefusalErr, got %v", err)
	}
	if len(refusal.rejections) != 1 || len(refusal.blocked) != 0 {
		t.Fatalf("rejections-only result: want 1 rejection and 0 blocked, got %d and %d",
			len(refusal.rejections), len(refusal.blocked))
	}
	if got := len(az.PendingDenials()); got != 0 {
		t.Fatalf("rejections-only result: want 0 captured events, got %d", got)
	}
}

// A blocked result captures one finding_blocked event per finding, each stamped
// from Finding.Surface and carrying the full scope (Org, Project and Env).
func TestCaptureScanRefusalOneEventPerBlockedFinding(t *testing.T) {
	az := authz.NewTxAuthorizer(nil, nil)
	res := declScanResult{blocked: []Finding{
		{RuleID: "aws-key", Surface: "apply", Locator: "keys.db.value"},
		{RuleID: "gh-token", Surface: "edit", Locator: "keys.ci.value"},
	}}
	err := captureScanRefusal(t.Context(), az, "prin_1", refusalScope, res)

	var refusal *scanRefusalErr
	if !errors.As(err, &refusal) {
		t.Fatalf("blocked result: want *scanRefusalErr, got %v", err)
	}
	denials := az.PendingDenials()
	if len(denials) != len(res.blocked) {
		t.Fatalf("blocked result: want %d captured events, got %d", len(res.blocked), len(denials))
	}
	for i, d := range denials {
		f := res.blocked[i]
		if d.Trail != audit.TrailTenant {
			t.Errorf("event %d: want trail %q, got %q", i, audit.TrailTenant, d.Trail)
		}
		if d.Scope != refusalScope {
			t.Errorf("event %d: want scope %+v, got %+v", i, refusalScope, d.Scope)
		}
		if d.Event.Type != audit.EventScanningFindingBlocked {
			t.Errorf("event %d: want type %q, got %q", i, audit.EventScanningFindingBlocked, d.Event.Type)
		}
		if d.Event.Object.ID != f.Locator {
			t.Errorf("event %d: want object id %q, got %q", i, f.Locator, d.Event.Object.ID)
		}
		if d.Event.Payload["ingress"] != f.Surface {
			t.Errorf("event %d: want ingress %q, got %v", i, f.Surface, d.Event.Payload["ingress"])
		}
	}
}
