package service

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestCaptureScanRefusalNil(t *testing.T) {
	az := authz.NewTxAuthorizer(nil, nil)
	scope := domain.Scope{Org: "org_a", Project: "proj_b", Env: "env_c"}
	err := captureScanRefusal(t.Context(), az, "prin_1", scope, declScanResult{
		overridden: []overrideAck{{ruleID: "r1", locator: "a.b"}},
	})
	if err != nil {
		t.Fatalf("no-refusal result: want nil error, got %v", err)
	}
	if got := len(az.PendingDenials()); got != 0 {
		t.Fatalf("no-refusal result: want 0 captured events, got %d", got)
	}
}

func TestCaptureScanRefusalRejectionsOnly(t *testing.T) {
	az := authz.NewTxAuthorizer(nil, nil)
	scope := domain.Scope{Org: "org_a", Project: "proj_b", Env: "env_c"}
	res := declScanResult{rejections: []scanRejection{{Index: 0, Reason: rejectSurplus}}}
	err := captureScanRefusal(t.Context(), az, "prin_1", scope, res)

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

func TestCaptureScanRefusalOneEventPerBlockedFinding(t *testing.T) {
	az := authz.NewTxAuthorizer(nil, nil)
	scope := domain.Scope{Org: "org_a", Project: "proj_b", Env: "env_c"}
	res := declScanResult{blocked: []Finding{
		{RuleID: "aws-key", Surface: "apply", Locator: "keys.db.value"},
		{RuleID: "gh-token", Surface: "edit", Locator: "keys.ci.value"},
	}}
	err := captureScanRefusal(t.Context(), az, "prin_1", scope, res)

	var refusal *scanRefusalErr
	if !errors.As(err, &refusal) {
		t.Fatalf("blocked result: want *scanRefusalErr, got %v", err)
	}
	denials := az.PendingDenials()
	if len(denials) != len(res.blocked) {
		t.Fatalf("blocked result: want %d captured events, got %d", len(res.blocked), len(denials))
	}
	for i, denial := range denials {
		finding := res.blocked[i]
		if denial.Trail != audit.TrailTenant {
			t.Errorf("event %d: want trail %q, got %q", i, audit.TrailTenant, denial.Trail)
		}
		if denial.Scope != scope {
			t.Errorf("event %d: want scope %+v, got %+v", i, scope, denial.Scope)
		}
		if denial.Event.Type != audit.EventScanningFindingBlocked {
			t.Errorf("event %d: want type %q, got %q", i, audit.EventScanningFindingBlocked, denial.Event.Type)
		}
		if denial.Event.Object.ID != finding.Locator {
			t.Errorf("event %d: want object id %q, got %q", i, finding.Locator, denial.Event.Object.ID)
		}
		if denial.Event.Payload["ingress"] != finding.Surface {
			t.Errorf("event %d: want ingress %q, got %v", i, finding.Surface, denial.Event.Payload["ingress"])
		}
	}
}
