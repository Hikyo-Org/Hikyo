package server

import (
	"context"
	"net/http"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// EmailInvitationService is the email invitation of an existing account (#1207).
type EmailInvitationService interface {
	Send(ctx context.Context, actor service.Actor, spec service.EmailInvitationSendSpec) (service.EmailInvitationReceipt, error)
	List(ctx context.Context, actor service.Actor, org domain.OrgID) ([]service.EmailInvitationEntry, error)
	Revoke(ctx context.Context, actor service.Actor, org domain.OrgID, id string) error
	Preview(ctx context.Context, actor service.Actor, token string) (service.EmailInvitationOffer, error)
	Accept(ctx context.Context, actor service.Actor, token string) (service.EmailInvitationAcceptance, error)
}

func (a *API) InviteOrgMemberByEmail(ctx context.Context, req apigen.InviteOrgMemberByEmailRequestObject) (apigen.InviteOrgMemberByEmailResponseObject, error) {
	res, err := a.EmailInvitations.Send(ctx, service.Bearer(bearer(ctx)), service.EmailInvitationSendSpec{
		Org: domain.OrgID(req.Org), Email: req.Body.Email, Template: domain.Template(req.Body.Template),
	})
	if err != nil {
		return nil, err
	}
	return apigen.InviteOrgMemberByEmail202JSONResponse(apigen.EmailInvitation{
		Id: res.ID, Email: res.Email, Template: apigen.RoleTemplate(res.Template),
		CreatedAt: res.CreatedAt, ExpiresAt: res.ExpiresAt,
	}), nil
}

func (a *API) ListOrgEmailInvitations(ctx context.Context, req apigen.ListOrgEmailInvitationsRequestObject) (apigen.ListOrgEmailInvitationsResponseObject, error) {
	rows, err := a.EmailInvitations.List(ctx, service.Bearer(bearer(ctx)), domain.OrgID(req.Org))
	if err != nil {
		return nil, err
	}
	items := make([]apigen.EmailInvitationListing, 0, len(rows))
	for _, row := range rows {
		items = append(items, apigen.EmailInvitationListing{
			Id: row.ID, Email: row.Email, Template: apigen.RoleTemplate(row.Template),
			InvitedBy: string(row.InvitedBy), CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt,
		})
	}
	return apigen.ListOrgEmailInvitations200JSONResponse{Items: items, Count: len(items)}, nil
}

func (a *API) RevokeOrgEmailInvitation(ctx context.Context, req apigen.RevokeOrgEmailInvitationRequestObject) (apigen.RevokeOrgEmailInvitationResponseObject, error) {
	if err := a.EmailInvitations.Revoke(ctx, service.Bearer(bearer(ctx)), domain.OrgID(req.Org), req.Invitation); err != nil {
		return nil, err
	}
	return apigen.RevokeOrgEmailInvitation204Response{}, nil
}

func (a *API) PreviewEmailInvitation(ctx context.Context, req apigen.PreviewEmailInvitationRequestObject) (apigen.PreviewEmailInvitationResponseObject, error) {
	offer, err := a.EmailInvitations.Preview(ctx, service.Bearer(bearer(ctx)), req.Body.Token)
	if err != nil {
		return nil, err
	}
	return apigen.PreviewEmailInvitation200JSONResponse{
		Org: string(offer.Org), OrgName: offer.OrgName,
		Template: apigen.RoleTemplate(offer.Template), ExpiresAt: offer.ExpiresAt,
	}, nil
}

// acceptEmailInvitationResponse clears the browser cookies alongside the 200
// when the acceptance ended the person's sessions: the row is gone, so a kept
// cookie could only be refused.
type acceptEmailInvitationResponse struct {
	body    apigen.EmailInvitationAcceptance
	cookies []*http.Cookie
}

func (r acceptEmailInvitationResponse) VisitAcceptEmailInvitationResponse(w http.ResponseWriter) error {
	return writeJSONWithCookies(w, r.cookies, http.StatusOK, r.body)
}

func (a *API) AcceptEmailInvitation(ctx context.Context, req apigen.AcceptEmailInvitationRequestObject) (apigen.AcceptEmailInvitationResponseObject, error) {
	res, err := a.EmailInvitations.Accept(ctx, service.Bearer(bearer(ctx)), req.Body.Token)
	if err != nil {
		return nil, err
	}
	body := apigen.EmailInvitationAcceptance{
		Org: string(res.Org), OrgName: res.OrgName, Template: apigen.RoleTemplate(res.Template),
		GrantsCreated: res.GrantsCreated, SignedOut: res.SignedOut,
	}
	if res.SignedOut {
		if r := requestFrom(ctx); r != nil {
			if c, cerr := r.Cookie(browserSessionCookie); cerr == nil && c.Value != "" {
				return acceptEmailInvitationResponse{body: body, cookies: expiredBrowserCookies()}, nil
			}
		}
	}
	return apigen.AcceptEmailInvitation200JSONResponse(body), nil
}
