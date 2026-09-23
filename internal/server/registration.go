package server

import (
	"context"
	"fmt"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// The registration-policy transport (#606). The scope is the addressed path,
// never a body member, exactly as the grant transport does; refusals are bare
// domain errors the uniform writer renders (a 400 carries the failing item as
// its safe detail).

// RegistrationService is the registration policy surface.
type RegistrationService interface {
	Get(ctx context.Context, actor service.Actor, scope service.RegistrationScope) (*service.RegistrationPolicyView, error)
	Put(ctx context.Context, actor service.Actor, scope service.RegistrationScope, in service.RegistrationPolicyInput, proof string) (service.RegistrationPolicyView, error)
	Delete(ctx context.Context, actor service.Actor, scope service.RegistrationScope, proof string) error
	SignupDoor(ctx context.Context, scope service.RegistrationScope) (service.SignupDoor, error)
}

func (a *API) GetOrgRegistrationPolicy(ctx context.Context, req apigen.GetOrgRegistrationPolicyRequestObject) (apigen.GetOrgRegistrationPolicyResponseObject, error) {
	view, err := a.getRegistrationPolicy(ctx, service.OrgRegistrationScope(domain.OrgID(req.Org)))
	if err != nil {
		return nil, err
	}
	return apigen.GetOrgRegistrationPolicy200JSONResponse(view), nil
}

func (a *API) GetInstanceRegistrationPolicy(ctx context.Context, _ apigen.GetInstanceRegistrationPolicyRequestObject) (apigen.GetInstanceRegistrationPolicyResponseObject, error) {
	view, err := a.getRegistrationPolicy(ctx, service.InstanceRegistrationScope())
	if err != nil {
		return nil, err
	}
	return apigen.GetInstanceRegistrationPolicy200JSONResponse(view), nil
}

func (a *API) PutOrgRegistrationPolicy(ctx context.Context, req apigen.PutOrgRegistrationPolicyRequestObject) (apigen.PutOrgRegistrationPolicyResponseObject, error) {
	view, err := a.putRegistrationPolicy(ctx, service.OrgRegistrationScope(domain.OrgID(req.Org)), req.Body)
	if err != nil {
		return nil, err
	}
	return apigen.PutOrgRegistrationPolicy200JSONResponse(view), nil
}

func (a *API) PutInstanceRegistrationPolicy(ctx context.Context, req apigen.PutInstanceRegistrationPolicyRequestObject) (apigen.PutInstanceRegistrationPolicyResponseObject, error) {
	view, err := a.putRegistrationPolicy(ctx, service.InstanceRegistrationScope(), req.Body)
	if err != nil {
		return nil, err
	}
	return apigen.PutInstanceRegistrationPolicy200JSONResponse(view), nil
}

func (a *API) DeleteOrgRegistrationPolicy(ctx context.Context, req apigen.DeleteOrgRegistrationPolicyRequestObject) (apigen.DeleteOrgRegistrationPolicyResponseObject, error) {
	if err := a.Registration.Delete(ctx, service.Bearer(bearer(ctx)), service.OrgRegistrationScope(domain.OrgID(req.Org)), deleteProof(req.Body)); err != nil {
		return nil, err
	}
	return apigen.DeleteOrgRegistrationPolicy204Response{}, nil
}

func (a *API) DeleteInstanceRegistrationPolicy(ctx context.Context, req apigen.DeleteInstanceRegistrationPolicyRequestObject) (apigen.DeleteInstanceRegistrationPolicyResponseObject, error) {
	if err := a.Registration.Delete(ctx, service.Bearer(bearer(ctx)), service.InstanceRegistrationScope(), deleteProof(req.Body)); err != nil {
		return nil, err
	}
	return apigen.DeleteInstanceRegistrationPolicy204Response{}, nil
}

func deleteProof(body *apigen.RegistrationPolicyDeleteRequest) string {
	if body == nil {
		return ""
	}
	return body.Proof
}

func (a *API) getRegistrationPolicy(ctx context.Context, scope service.RegistrationScope) (apigen.RegistrationPolicy, error) {
	view, err := a.Registration.Get(ctx, service.Bearer(bearer(ctx)), scope)
	if err != nil {
		return apigen.RegistrationPolicy{}, err
	}
	if view == nil {
		// No policy: registration is closed at this scope.
		return apigen.RegistrationPolicy{}, domain.ErrNotFound
	}
	return wireRegistrationPolicy(*view), nil
}

func (a *API) putRegistrationPolicy(ctx context.Context, scope service.RegistrationScope, body *apigen.RegistrationPolicyPutRequest) (apigen.RegistrationPolicy, error) {
	if body == nil {
		return apigen.RegistrationPolicy{}, fmt.Errorf("%w: a registration policy body is required", domain.ErrInvalid)
	}
	in := service.RegistrationPolicyInput{
		Landing: service.RegistrationLanding{Kind: service.LandingKind(body.Landing.Kind)},
	}
	if body.Landing.Template != nil {
		in.Landing.Template = domain.Template(*body.Landing.Template)
	}
	if body.Landing.Cap != nil {
		in.Landing.Cap = int64(*body.Landing.Cap)
	}
	for _, e := range body.External {
		entry := service.RegistrationExternalEntry{
			Provider: domain.ProviderRef{Kind: domain.ProviderKind(e.Provider.Kind), Slug: e.Provider.Slug},
		}
		if e.Claim != nil {
			entry.Claim = *e.Claim
		}
		if e.Values != nil {
			entry.Values = *e.Values
		}
		in.External = append(in.External, entry)
	}
	if body.Local != nil {
		in.Local = &service.RegistrationLocalEntry{}
		if body.Local.Domains != nil {
			in.Local.Domains = *body.Local.Domains
		}
	}
	view, err := a.Registration.Put(ctx, service.Bearer(bearer(ctx)), scope, in, body.Proof)
	if err != nil {
		return apigen.RegistrationPolicy{}, err
	}
	return wireRegistrationPolicy(view), nil
}

func wireRegistrationPolicy(v service.RegistrationPolicyView) apigen.RegistrationPolicy {
	out := apigen.RegistrationPolicy{
		Id: v.ID, AuthorityPrincipalId: string(v.AuthorityPrincipalID),
		External:   make([]apigen.RegistrationExternalEntry, 0, len(v.External)),
		Landing:    apigen.RegistrationLanding{Kind: apigen.RegistrationLandingKind(v.Landing.Kind)},
		State:      apigen.RegistrationPolicyStateActive,
		RowVersion: int(v.RowVersion), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
	if v.FreshOrgCount != nil {
		count := int(*v.FreshOrgCount)
		out.FreshOrgCount = &count
	}
	if v.Org != "" {
		org := string(v.Org)
		out.Org = &org
	}
	if v.Landing.Template != "" {
		template := apigen.RoleTemplate(v.Landing.Template)
		out.Landing.Template = &template
	}
	if v.Landing.Cap != 0 {
		limit := int(v.Landing.Cap)
		out.Landing.Cap = &limit
	}
	for _, e := range v.External {
		entry := apigen.RegistrationExternalEntry{
			Provider: apigen.ProviderRef{Kind: apigen.IdentityProviderKind(e.Provider.Kind), Slug: e.Provider.Slug},
		}
		if e.DisplayName != "" {
			name := e.DisplayName
			entry.DisplayName = &name
		}
		if e.Claim != "" {
			claim, values := e.Claim, append([]string{}, e.Values...)
			entry.Claim, entry.Values = &claim, &values
		}
		out.External = append(out.External, entry)
	}
	if v.Local != nil {
		domains := append([]string{}, v.Local.Domains...)
		out.Local = &apigen.RegistrationLocalEntry{Domains: &domains}
	}
	if !v.Active {
		out.State = apigen.RegistrationPolicyStateInactive
		cause := apigen.RegistrationPolicyInactiveCause(v.InactiveCause)
		out.InactiveCause = &cause
		if v.InactivePrecondition != "" {
			precondition := v.InactivePrecondition
			out.InactivePrecondition = &precondition
		}
	}
	return out
}

// wireSignupDoor renders one scope's public sign-up door onto AuthMethods:
// federated entries as `{kind, slug}`, the local entry as the string
// `local` (api-cli-spellings section 8).
func wireSignupDoor(out *apigen.AuthMethods, door service.SignupDoor) error {
	out.SignupOpen, out.SignupPaused = door.Open, door.Paused
	out.SignupMethods = make([]apigen.SignupMethod, 0, len(door.Methods))
	for _, m := range door.Methods {
		var method apigen.SignupMethod
		var err error
		switch m.Kind {
		case service.SignupMethodLocal:
			err = method.FromLocalSignupMethod(apigen.Local)
		case service.SignupMethodOIDC, service.SignupMethodOAuth2:
			err = method.FromProviderRef(apigen.ProviderRef{Kind: apigen.IdentityProviderKind(m.Kind), Slug: m.Slug})
		default:
			err = fmt.Errorf("server: sign-up method kind %q has no wire spelling", m.Kind)
		}
		if err != nil {
			return err
		}
		out.SignupMethods = append(out.SignupMethods, method)
	}
	return nil
}
