package service

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestValidIncidentObjectID(t *testing.T) {
	for id, want := range map[string]bool{
		"adp_01":                true,
		"A-b_9":                 true,
		"":                      false,
		"adp bad":               false,
		"adp;drop":              false,
		"adp/../x":              false,
		strings.Repeat("a", 80): true,
		strings.Repeat("a", 81): false,
		"adp\x00":               false,
		"ädp":                   false,
	} {
		if got := validIncidentObjectID(id); got != want {
			t.Errorf("validIncidentObjectID(%q) = %t, want %t", id, got, want)
		}
	}
}

func TestIncidentAccessSameOwnership(t *testing.T) {
	base := incidentAccess{kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "org_a", footprint: []domain.OrgID{"org_a"}}
	same := incidentAccess{kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "org_a", footprint: []domain.OrgID{"org_a"}}
	if !base.sameOwnership(same) {
		t.Error("identical access reported as changed")
	}
	for name, other := range map[string]incidentAccess{
		"kind":    {kind: domain.IncidentScopeOrg, scopeID: "usr_a", org: "org_a", footprint: []domain.OrgID{"org_a"}},
		"scope":   {kind: domain.IncidentScopeAccount, scopeID: "usr_b", org: "org_a", footprint: []domain.OrgID{"org_a"}},
		"org":     {kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "", footprint: []domain.OrgID{"org_a"}},
		"grown":   {kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "org_a", footprint: []domain.OrgID{"org_a", "org_b"}},
		"swapped": {kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "org_a", footprint: []domain.OrgID{"org_b"}},
		"shrunk":  {kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "org_a"},
	} {
		if base.sameOwnership(other) {
			t.Errorf("a changed %s was reported as the same ownership", name)
		}
	}
}

func TestAttestable(t *testing.T) {
	for name, tc := range map[string]struct {
		item authz.IncidentItem
		want bool
	}{
		"observable session":        {authz.IncidentItem{OwnerFunction: domain.OwnerSessionRevoke, Outcome: domain.ItemPending}, false},
		"observable developer cred": {authz.IncidentItem{OwnerFunction: domain.OwnerDeveloperRevoke, Outcome: domain.ItemUnknown}, false},
		"blocked factor":            {authz.IncidentItem{OwnerFunction: domain.OwnerFactorRemoval, Outcome: domain.ItemBlocked}, false},
		"blocked upstream":          {authz.IncidentItem{OwnerFunction: domain.OwnerDynamicUpstream, Outcome: domain.ItemBlocked, Reason: domain.ReasonUpstreamNotObservable}, false},
		"upstream by reason":        {authz.IncidentItem{OwnerFunction: domain.OwnerAdapterTargetPause, Outcome: domain.ItemUnknown, Reason: domain.ReasonUpstreamNotObservable}, true},
		"upstream by owner":         {authz.IncidentItem{OwnerFunction: domain.OwnerAdapterUpstream, Outcome: domain.ItemUnknown}, true},
		"consumer adoption":         {authz.IncidentItem{OwnerFunction: domain.OwnerConsumerAdoption, Outcome: domain.ItemPending}, true},
		"adapter move item":         {authz.IncidentItem{OwnerFunction: domain.OwnerAdapterMove, Outcome: domain.ItemPending}, false},
		"adapter scrub item":        {authz.IncidentItem{OwnerFunction: domain.OwnerAdapterScrub, Outcome: domain.ItemPending}, false},
		"factor stays open":         {authz.IncidentItem{OwnerFunction: domain.OwnerFactorRemoval, Outcome: domain.ItemUnknown, Reason: domain.ReasonNoRevokeOwner}, false},
		"truncated identity marker": {authz.IncidentItem{OwnerFunction: domain.OwnerExternalIdentity, ObjectKind: domain.ObjectKindEnumeration, Outcome: domain.ItemUnknown, Reason: domain.ReasonEnumerationTruncated}, false},
		"linked identity":           {authz.IncidentItem{OwnerFunction: domain.OwnerExternalIdentity, Outcome: domain.ItemUnknown, Reason: domain.ReasonNoRevokeOwner}, true},
	} {
		if got := attestable(tc.item); got != tc.want {
			t.Errorf("%s: attestable = %t, want %t", name, got, tc.want)
		}
	}
}

// The owner error log carries a classification only; the owner's own text,
// which it controls, never reaches the log.
func TestOwnerErrClassIgnoresOwnerText(t *testing.T) {
	for want, err := range map[string]error{
		"deadline":     fmt.Errorf("secret-token-1: %w", context.DeadlineExceeded),
		"not_found":    fmt.Errorf("secret-token-2: %w", domain.ErrNotFound),
		"unauthorized": domain.ErrUnauthorized,
		"owner_error":  errors.New("secret-token-3"),
	} {
		got := ownerErrClass(err)
		if got != want || strings.Contains(got, "secret") {
			t.Errorf("ownerErrClass(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestPageBoundedLimit(t *testing.T) {
	serve := func(total int) func(string) ([]int, error) {
		return func(after string) ([]int, error) {
			start := 0
			if after != "" {
				n, err := strconv.Atoi(after)
				if err != nil {
					return nil, err
				}
				start = n
			}
			var page []int
			for i := start; i < total && len(page) < authz.MaxIncidentPage; i++ {
				page = append(page, i)
			}
			return page, nil
		}
	}
	id := func(i int) string { return fmt.Sprint(i + 1) }
	for _, tc := range []struct {
		total, limit, wantLen int
		wantCut               bool
	}{
		{3, 2000, 3, false},
		{2000, 2000, 2000, false},
		{2001, 2000, 2000, true},
		{500, 500, 500, false},
		{501, 500, 500, true},
		{0, 2000, 0, false},
	} {
		out, cut, err := pageBounded(tc.limit, id, serve(tc.total))
		if err != nil || len(out) != tc.wantLen || cut != tc.wantCut {
			t.Errorf("total %d limit %d: len %d cut %v err %v, want len %d cut %v", tc.total, tc.limit, len(out), cut, err, tc.wantLen, tc.wantCut)
		}
	}
}

// calledFrom reports whether the running goroutine is inside a function whose
// name contains fn.
func calledFrom(fn string) bool {
	pcs := make([]uintptr, 16)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		f, more := frames.Next()
		if strings.Contains(f.Function, fn) {
			return true
		}
		if !more {
			return false
		}
	}
}

// An adapter incident whose work read is refused after the adapter probe passed
// (the capability moved between the two) records the unread source as its own
// blocked enumeration row and refuses a clean close, for a project incident and
// an adapter incident alike.
func TestIncidentAdapterWorkRefusalBlocksClose(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target IncidentTarget
		marker string
	}{
		{"project", IncidentTarget{Kind: domain.IncidentScopeProject, Scope: domain.Scope{Org: "org_adapter", Project: "prj_adapter"}}, "prj_adapter"},
		{"adapter", IncidentTarget{Kind: domain.IncidentScopeAdapter, Scope: domain.Scope{Org: "org_adapter", Project: "prj_adapter"}, AdapterID: "adp_1"}, "adp_1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := adapterServiceDB(t)
			ctx := t.Context()
			exec := func(q string) {
				t.Helper()
				if _, err := db.SQLiteWrite().ExecContext(ctx, q); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO grants (id,principal_id,capability,org_id,created_at) VALUES ('gr_reset','usr_adapter','credential-reset','org_adapter','2026-08-17T00:00:00Z')`)
			// The grant is removed just as the work read takes its clock, after the
			// adapter list has already passed.
			s := &Incidents{DB: db, Adapters: &Adapters{DB: db}}
			s.Now = func() time.Time {
				if calledFrom("readAdapterWork") {
					exec(`DELETE FROM grants WHERE id = 'gr_manage'`)
				}
				return time.Now()
			}
			actor := LocalPrincipal("usr_adapter")
			v, err := s.Open(ctx, actor, tc.target)
			if err != nil {
				t.Fatal(err)
			}
			items, err := s.Items(ctx, actor, v.Incident.ID, "", 500, false)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, it := range items {
				if it.OwnerFunction == domain.OwnerAdapterMove && it.ObjectKind == domain.ObjectKindEnumeration && it.ObjectID == tc.marker &&
					it.Outcome == domain.ItemBlocked && it.Reason == domain.ReasonNeedsAnotherOperator {
					found = true
				}
			}
			if !found {
				t.Fatalf("no blocked adapter_move.resume enumeration row for %s in %+v", tc.marker, items)
			}
			// The marker row above is the proof; a clean close is refused here
			// regardless because the adapter's other items are open too.
			if _, err := s.Close(ctx, actor, v.Incident.ID, false); !errors.Is(err, ErrIncidentOpenItems) {
				t.Errorf("clean close = %v, want ErrIncidentOpenItems", err)
			}
		})
	}
}
