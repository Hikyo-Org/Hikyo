package server

import (
	"context"

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

// AcceptEmailInvitation answers a plain 200 even when the acceptance ended the
// person's sessions, and leaves the browser cookies alone: the browser treats a
// changed session cookie as a possibly replaced session and keeps the response
// only if whoami still proves the same person, which a revoked session cannot,
// so the page could not show the outcome. The page ends its own session once it
// has shown it.
func (a *API) AcceptEmailInvitation(ctx context.Context, req apigen.AcceptEmailInvitationRequestObject) (apigen.AcceptEmailInvitationResponseObject, error) {
	res, err := a.EmailInvitations.Accept(ctx, service.Bearer(bearer(ctx)), req.Body.Token)
	if err != nil {
		return nil, err
	}
	return apigen.AcceptEmailInvitation200JSONResponse{
		Org: string(res.Org), OrgName: res.OrgName, Template: apigen.RoleTemplate(res.Template),
		GrantsCreated: res.GrantsCreated, SignedOut: res.SignedOut,
	}, nil
}
