package server

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type pairingProofContextKey struct{}
type RemotePairingService interface {
	Prepare(context.Context, service.Actor, service.PreparePairingRequest) (service.PairingPrepared, error)
	CreateInvite(context.Context, service.Actor, service.CreatePairingInvite) (service.PairingInviteCreated, error)
	ListInvites(context.Context, service.Actor) (service.PairingInviteList, error)
	RevokeInvite(context.Context, service.Actor, string) error
	Pair(context.Context, service.Actor, service.PairRemoteRequest) (service.RemoteView, error)
	Start(context.Context, string, string) (service.PairingStarted, error)
	Finish(context.Context, string, string, string) (service.PairingFinished, error)
}

func (a *API) PrepareRemotePairing(ctx context.Context, req apigen.PrepareRemotePairingRequestObject) (apigen.PrepareRemotePairingResponseObject, error) {
	var input service.PreparePairingRequest
	if req.Body != nil {
		input.Phrase = req.Body.Phrase
		input.WordCount = req.Body.WordCount
	}
	result, err := a.Pairings.Prepare(ctx, service.Bearer(bearer(ctx)), input)
	if err != nil {
		return nil, err
	}
	return apigen.PrepareRemotePairing201JSONResponse{Phrase: result.Phrase, RequesterState: result.RequesterState, ExpiresAt: result.ExpiresAt}, nil
}
func (a *API) CreateRemotePairingInvite(ctx context.Context, req apigen.CreateRemotePairingInviteRequestObject) (apigen.CreateRemotePairingInviteResponseObject, error) {
	result, err := a.Pairings.CreateInvite(ctx, service.Bearer(bearer(ctx)), service.CreatePairingInvite{Phrase: req.Body.Phrase, Label: req.Body.Label, InviteLifetime: time.Duration(req.Body.InviteLifetimeSeconds) * time.Second, CredentialLifetime: time.Duration(req.Body.CredentialLifetimeSeconds) * time.Second})
	if err != nil {
		return nil, err
	}
	return apigen.CreateRemotePairingInvite201JSONResponse{Invite: wirePairingInvite(result.Invite), InviteBundle: result.InviteBundle}, nil
}
func (a *API) ListRemotePairingInvites(ctx context.Context, _ apigen.ListRemotePairingInvitesRequestObject) (apigen.ListRemotePairingInvitesResponseObject, error) {
	rows, err := a.Pairings.ListInvites(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	items := make([]apigen.RemotePairingInvite, 0, len(rows.Items))
	for _, i := range rows.Items {
		items = append(items, wirePairingInvite(i))
	}
	out := apigen.ListRemotePairingInvites200JSONResponse{Items: items, Count: len(items), Origin: rows.Origin}
	if rows.ConfigurationError != "" {
		out.ConfigurationError = &rows.ConfigurationError
	}
	return out, nil
}
func (a *API) RevokeRemotePairingInvite(ctx context.Context, req apigen.RevokeRemotePairingInviteRequestObject) (apigen.RevokeRemotePairingInviteResponseObject, error) {
	if err := a.Pairings.RevokeInvite(ctx, service.Bearer(bearer(ctx)), req.Invite); err != nil {
		return nil, err
	}
	return apigen.RevokeRemotePairingInvite204Response{}, nil
}
func (a *API) PairRemote(ctx context.Context, req apigen.PairRemoteRequestObject) (apigen.PairRemoteResponseObject, error) {
	result, err := a.Pairings.Pair(ctx, service.Bearer(bearer(ctx)), service.PairRemoteRequest{Name: req.Body.Name, RequesterState: req.Body.RequesterState, InviteBundle: req.Body.InviteBundle})
	if err != nil {
		return nil, err
	}
	return apigen.PairRemote201JSONResponse(wireRemote(result)), nil
}
func (a *API) StartRemotePairing(ctx context.Context, req apigen.StartRemotePairingRequestObject) (apigen.StartRemotePairingResponseObject, error) {
	release, err := a.enterWorkspaceAdmission(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	result, err := a.Pairings.Start(ctx, req.Body.InviteCapability, req.Body.PublicJwk)
	if err != nil {
		return nil, err
	}
	return apigen.StartRemotePairing201JSONResponse{Exchange: result.Exchange, ServerMessage: result.ServerMessage}, nil
}
func (a *API) FinishRemotePairing(ctx context.Context, req apigen.FinishRemotePairingRequestObject) (apigen.FinishRemotePairingResponseObject, error) {
	release, err := a.enterWorkspaceAdmission(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	result, err := a.Pairings.Finish(ctx, req.Body.Exchange, req.Body.ClientMessage, req.Body.Proof)
	if err != nil {
		return nil, err
	}
	return apigen.FinishRemotePairing201JSONResponse{Credential: result.Credential, ServerMessage: result.ServerMessage, Connection: wireConnection(result.Connection)}, nil
}
func wirePairingInvite(i service.PairingInviteView) apigen.RemotePairingInvite {
	out := apigen.RemotePairingInvite{Id: i.ID, Label: i.Label, Origin: i.Origin, SpkiPin: i.SPKIPin, CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt, CredentialExpiresAt: i.CredentialExpiresAt}
	if !i.RevokedAt.IsZero() {
		v := i.RevokedAt
		out.RevokedAt = &v
	}
	if !i.ConsumedAt.IsZero() {
		v := i.ConsumedAt
		out.ConsumedAt = &v
	}
	return out
}
