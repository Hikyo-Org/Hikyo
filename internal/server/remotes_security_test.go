package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type canonicalOriginRemoval struct {
	WorkspaceService
	removed string
}

type exhaustedRequestGrantService struct {
	GrantService
	budget *service.Budget
}

func (s *exhaustedRequestGrantService) Create(ctx context.Context, _ service.Actor, _ service.GrantSpec) (service.GrantResult, error) {
	// The cross-engine regression proves this key comes from live in-transaction
	// authentication. This transport fixture exercises the shared accounting
	// receipt and uniform wire refusal without a second datastore ceremony.
	for range service.BudgetAuthenticatedAPIBurst {
		if err := s.budget.AdmitAuthenticatedAPI("live-session"); err != nil {
			return service.GrantResult{}, err
		}
	}
	if err := operation.AdmitRequest(ctx, "live-session"); err != nil {
		return service.GrantResult{}, err
	}
	return service.GrantResult{}, domain.ErrNotFound
}

func TestAuthenticatedAPIOverflowReturnsUniformRetryAfter(t *testing.T) {
	budget := service.NewBudget()
	h := New(nil, &API{RequestBudget: budget, Grants: &exhaustedRequestGrantService{budget: budget}}, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orgs/org_01900000-0000-7000-8000-000000000001/projects/prj_01900000-0000-7000-8000-000000000002/grants", strings.NewReader(`{"principal":"usr_01900000-0000-7000-8000-000000000003","capability":"read"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer presented-session")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" || !strings.Contains(response.Body.String(), `"too_many_requests"`) {
		t.Fatalf("overflow status=%d retry=%q body=%s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
}

func (s *canonicalOriginRemoval) RemoveOrigin(_ context.Context, _ service.Actor, origin string) (int64, error) {
	s.removed = origin
	return 2, nil
}

func TestRemoveWorkspaceOriginReturnsDeletedCanonicalKey(t *testing.T) {
	workspace := &canonicalOriginRemoval{}
	a := &API{Workspace: workspace}
	response, err := a.RemoveWorkspaceOrigin(context.Background(), apigen.RemoveWorkspaceOriginRequestObject{
		Body: &apigen.RemoveWorkspaceOriginJSONRequestBody{Origin: "https://EXAMPLE.COM/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := response.(apigen.RemoveWorkspaceOrigin200JSONResponse)
	if workspace.removed != "https://example.com" || out.Origin != workspace.removed || out.SessionsRevoked != 2 {
		t.Fatalf("removed=%q response=%+v", workspace.removed, out)
	}
}
