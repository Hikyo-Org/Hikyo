package mcpserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestCursorExpiryIsRejected(t *testing.T) {
	scope := CursorScope{Tool: "hikyo_list_environments", Org: "org_a", Project: "prj_a", PageSize: 1}
	raw, err := encodeCursor(t.Context(), testCursorSealer, cursorState{
		Version: cursorVersion, Tool: scope.Tool, Org: scope.Org, Project: scope.Project,
		PageSize: 1, Pos: "b", Pages: 1, Items: 1, Expiry: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeCursor(t.Context(), testCursorSealer, scope, raw, time.Now().UTC()); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expired cursor decode = %v, want ErrInvalidCursor", err)
	}
}

func TestCursorIsRejectedAtExactExpiry(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	scope := CursorScope{Tool: ToolListDefinitions, Org: "org_a", Project: "prj_a", PageSize: 1}
	state := cursorState{
		Version: cursorVersion, Tool: scope.Tool, Org: scope.Org, Project: scope.Project,
		PageSize: scope.PageSize, Pos: "b", Pages: 1, Items: 1, Expiry: now.UnixMilli(),
	}
	raw, err := encodeCursor(t.Context(), testCursorSealer, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeCursor(t.Context(), testCursorSealer, scope, raw, now); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cursor at exact expiry decode = %v, want ErrInvalidCursor", err)
	}
}

func TestContinuationDoesNotRenewExpiry(t *testing.T) {
	ctx := withCursorSealer(context.Background(), testCursorSealer)
	scope := CursorScope{Tool: "hikyo_list_environments", Org: "org_a", Project: "prj_a", PageSize: 1}
	source := fakeEnvironments{items: []service.Environment{env("a"), env("b"), env("c"), env("d")}}
	newSpec := func(cursor string) PageSpec[service.Environment, environmentElement] {
		return PageSpec[service.Environment, environmentElement]{
			Scope: scope, Cursor: cursor, PageSize: 1,
			Fetch: func(ctx context.Context, after string, limit int) ([]service.Environment, error) {
				afterOrder, afterName, err := parseEnvironmentPosition(after)
				if err != nil {
					return nil, err
				}
				return source.ListPage(ctx, service.Actor{}, domain.Scope{}, afterOrder, afterName, limit)
			},
			Map: mapEnvironment,
			Key: environmentPosition,
			StructuredSize: func(items []environmentElement, next string) (int, error) {
				return encodedSize(environmentsOutput{OrgID: "org_a", ProjectID: "prj_a", Environments: items, NextCursor: next})
			},
		}
	}

	_, cursor1, err := Paginate(ctx, newSpec(""))
	if err != nil || cursor1 == "" {
		t.Fatalf("page 1 cursor = %q err %v", cursor1, err)
	}
	state1, err := decodeCursor(t.Context(), testCursorSealer, scope, cursor1, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	_, cursor2, err := Paginate(ctx, newSpec(cursor1))
	if err != nil || cursor2 == "" {
		t.Fatalf("page 2 cursor = %q err %v", cursor2, err)
	}
	state2, err := decodeCursor(t.Context(), testCursorSealer, scope, cursor2, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if state1.Expiry != state2.Expiry {
		t.Fatalf("continuation renewed the chain expiry: %d then %d", state1.Expiry, state2.Expiry)
	}
	if state2.Pages != 2 || state2.Items != 2 {
		t.Fatalf("chain counters did not advance: %+v", state2)
	}
}
