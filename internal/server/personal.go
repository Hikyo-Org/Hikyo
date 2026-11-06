package server

import (
	"context"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// PersonalValueService is the per-developer value surface (#1235).
type PersonalValueService interface {
	List(ctx context.Context, actor service.Actor, scope domain.Scope) ([]service.PersonalEntry, error)
	Set(ctx context.Context, actor service.Actor, scope domain.Scope, keyName, value string) (service.PersonalEntry, error)
	Reveal(ctx context.Context, actor service.Actor, scope domain.Scope, keyName string) (service.PersonalEntry, error)
	Delete(ctx context.Context, actor service.Actor, scope domain.Scope, keyName string) error
	Members(ctx context.Context, actor service.Actor, scope domain.Scope, keyName string) ([]service.PersonalHolder, error)
	DeleteMember(ctx context.Context, actor service.Actor, scope domain.Scope, keyName, member string) error
	Counts(ctx context.Context, actor service.Actor, scope domain.Scope) (service.PersonalCounts, error)
	OffboardingReport(ctx context.Context, actor service.Actor, scope domain.Scope, member string) (service.OffboardingReport, error)
}

func wirePersonalEntry(e service.PersonalEntry, reveal bool) apigen.PersonalValueEntry {
	out := apigen.PersonalValueEntry{Name: e.Name, Set: e.Set}
	if e.KeyID != "" {
		id := e.KeyID
		out.KeyId = &id
	}
	if !e.UpdatedAt.IsZero() {
		t := e.UpdatedAt
		out.UpdatedAt = &t
	}
	if e.Notice != "" {
		cause := apigen.PersonalValueNoticeCause(e.Notice)
		out.Notice = &cause
	}
	if e.Required {
		out.Required = &e.Required
	}
	if e.Suspended {
		out.Suspended = &e.Suspended
		if !e.SuspendedAt.IsZero() {
			t := e.SuspendedAt
			out.SuspendedAt = &t
		}
	}
	if len(e.Warnings) > 0 {
		w := append([]string(nil), e.Warnings...)
		out.Warnings = &w
	}
	if reveal {
		v := e.Value
		out.Value = &v
	}
	return out
}

func (a *API) ListMyPersonalValues(ctx context.Context, req apigen.ListMyPersonalValuesRequestObject) (apigen.ListMyPersonalValuesResponseObject, error) {
	entries, err := a.PersonalValues.List(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	out := apigen.PersonalValueList{Items: make([]apigen.PersonalValueEntry, 0, len(entries))}
	for _, e := range entries {
		out.Items = append(out.Items, wirePersonalEntry(e, false))
	}
	return apigen.ListMyPersonalValues200JSONResponse(out), nil
}

func (a *API) SetMyPersonalValue(ctx context.Context, req apigen.SetMyPersonalValueRequestObject) (apigen.SetMyPersonalValueResponseObject, error) {
	if req.Body == nil {
		return nil, domain.ErrInvalid
	}
	entry, err := a.PersonalValues.Set(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), req.Key, req.Body.Value)
	if err != nil {
		return nil, err
	}
	return apigen.SetMyPersonalValue200JSONResponse(wirePersonalEntry(entry, false)), nil
}

func (a *API) RevealMyPersonalValue(ctx context.Context, req apigen.RevealMyPersonalValueRequestObject) (apigen.RevealMyPersonalValueResponseObject, error) {
	entry, err := a.PersonalValues.Reveal(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), req.Key)
	if err != nil {
		return nil, err
	}
	return apigen.RevealMyPersonalValue200JSONResponse(wirePersonalEntry(entry, true)), nil
}

func (a *API) DeleteMyPersonalValue(ctx context.Context, req apigen.DeleteMyPersonalValueRequestObject) (apigen.DeleteMyPersonalValueResponseObject, error) {
	if err := a.PersonalValues.Delete(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), req.Key); err != nil {
		return nil, err
	}
	return apigen.DeleteMyPersonalValue204Response{}, nil
}

func (a *API) ListPersonalValueMembers(ctx context.Context, req apigen.ListPersonalValueMembersRequestObject) (apigen.ListPersonalValueMembersResponseObject, error) {
	holders, err := a.PersonalValues.Members(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), req.Key)
	if err != nil {
		return nil, err
	}
	out := apigen.PersonalValueHolderList{Items: make([]apigen.PersonalValueHolder, 0, len(holders))}
	for _, h := range holders {
		item := apigen.PersonalValueHolder{PrincipalId: string(h.Principal), Set: h.Set}
		if h.Username != "" {
			name := h.Username
			item.Username = &name
		}
		if !h.UpdatedAt.IsZero() {
			t := h.UpdatedAt
			item.UpdatedAt = &t
		}
		if h.Notice != "" {
			cause := apigen.PersonalValueNoticeCause(h.Notice)
			item.Notice = &cause
		}
		out.Items = append(out.Items, item)
	}
	return apigen.ListPersonalValueMembers200JSONResponse(out), nil
}

func (a *API) DeletePersonalValueMember(ctx context.Context, req apigen.DeletePersonalValueMemberRequestObject) (apigen.DeletePersonalValueMemberResponseObject, error) {
	if err := a.PersonalValues.DeleteMember(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), req.Key, req.Member); err != nil {
		return nil, err
	}
	return apigen.DeletePersonalValueMember204Response{}, nil
}
