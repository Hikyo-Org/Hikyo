package server

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// SSH user certificates (#155). Handlers translate wire shapes only; every
// decision (authorization, profile bounds, fencing) is the service's.

func sshTime(s, field string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("server: parse ssh %s: %w", field, err)
	}
	return t, nil
}

func sshCAResponse(v service.SSHCAView) (apigen.SSHCA, error) {
	created, err := sshTime(v.CreatedAt, "CA created_at")
	if err != nil {
		return apigen.SSHCA{}, err
	}
	out := apigen.SSHCA{
		Id: apigen.ID(v.ID), Name: v.Name, AuthorityPrincipalId: apigen.ID(v.AuthorityPrincipalID),
		CreatedAt: created, Keys: make([]apigen.SSHCAKey, 0, len(v.Keys)),
	}
	for i, k := range v.Keys {
		keyCreated, err := sshTime(k.CreatedAt, "key created_at")
		if err != nil {
			return apigen.SSHCA{}, err
		}
		key := apigen.SSHCAKey{
			Id: apigen.ID(k.ID), Algorithm: apigen.SSHKeyAlgorithm(k.Algorithm), PublicKey: k.PublicKey,
			Fingerprint: k.Fingerprint, Origin: apigen.SSHCAKeyOrigin(k.Origin), State: apigen.SSHCAKeyState(k.State),
			Trusted: v.Trusted[i], CreatedAt: keyCreated,
		}
		if key.RetiringAt, err = parseOptTime(k.RetiringAt); err != nil {
			return apigen.SSHCA{}, err
		}
		if key.RetireAfter, err = parseOptTime(k.RetireAfter); err != nil {
			return apigen.SSHCA{}, err
		}
		if key.RetiredAt, err = parseOptTime(k.RetiredAt); err != nil {
			return apigen.SSHCA{}, err
		}
		out.Keys = append(out.Keys, key)
	}
	return out, nil
}

func sshProfileResponse(v service.SSHProfileView) (apigen.SSHProfile, error) {
	created, err := sshTime(v.CreatedAt, "profile created_at")
	if err != nil {
		return apigen.SSHProfile{}, err
	}
	updated, err := sshTime(v.UpdatedAt, "profile updated_at")
	if err != nil {
		return apigen.SSHProfile{}, err
	}
	out := apigen.SSHProfile{
		Id: apigen.ID(v.ID), CaId: apigen.ID(v.CAID), Name: v.Name, Principals: v.Principals,
		ForceCommand: v.ForceCommand, SourceAddresses: v.SourceAddresses,
		Extensions: make([]apigen.SSHExtension, 0, len(v.Extensions)), KeyAlgorithms: make([]apigen.SSHKeyAlgorithm, 0, len(v.KeyAlgorithms)),
		DefaultTtlSeconds: v.DefaultTTLSeconds, MaxTtlSeconds: v.MaxTTLSeconds, Enabled: v.State == "enabled",
		Requesters: make([]apigen.ID, 0, len(v.Requesters)), CreatedAt: created, UpdatedAt: updated,
	}
	for _, e := range v.Extensions {
		out.Extensions = append(out.Extensions, apigen.SSHExtension(e))
	}
	for _, a := range v.KeyAlgorithms {
		out.KeyAlgorithms = append(out.KeyAlgorithms, apigen.SSHKeyAlgorithm(a))
	}
	for _, r := range v.Requesters {
		out.Requesters = append(out.Requesters, apigen.ID(r))
	}
	return out, nil
}

func sshCertificateResponse(v service.SSHCertificateView) (apigen.SSHCertificate, error) {
	created, err := sshTime(v.CreatedAt, "certificate created_at")
	if err != nil {
		return apigen.SSHCertificate{}, err
	}
	validAfter, err := sshTime(v.ValidAfter, "valid_after")
	if err != nil {
		return apigen.SSHCertificate{}, err
	}
	validBefore, err := sshTime(v.ValidBefore, "valid_before")
	if err != nil {
		return apigen.SSHCertificate{}, err
	}
	revokedAt, err := parseOptTime(v.RevokedAt)
	if err != nil {
		return apigen.SSHCertificate{}, err
	}
	out := apigen.SSHCertificate{
		Id: apigen.ID(v.ID), CaId: apigen.ID(v.CAID), CaKeyId: apigen.ID(v.CAKeyID), ProfileId: apigen.ID(v.ProfileID),
		Serial: strconv.FormatInt(v.Serial, 10), KeyId: v.KeyID, Principals: v.Principals,
		PublicKeyFingerprint: v.PublicKeyFingerprint, KeyAlgorithm: apigen.SSHKeyAlgorithm(v.KeyAlgorithm),
		KeyOrigin: apigen.SSHCertificateKeyOrigin(v.KeyOrigin), ValidAfter: validAfter, ValidBefore: validBefore,
		RequesterPrincipalId: apigen.ID(v.RequesterPrincipalID), RequesterClass: v.RequesterClass,
		Status: apigen.SSHCertificateStatus(v.Status), InKrl: v.InKRL, RevokedAt: revokedAt, CreatedAt: created,
	}
	if v.RevocationReason != "" {
		reason := apigen.SSHCertificateRevocationReason(v.RevocationReason)
		out.RevocationReason = &reason
	}
	return out, nil
}

func sshOptString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func optAlgorithm(p *apigen.SSHKeyAlgorithm) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

// --- CAs ----------------------------------------------------------------------

func (a *API) ListSshCas(ctx context.Context, req apigen.ListSshCasRequestObject) (apigen.ListSshCasResponseObject, error) {
	rows, err := a.SSH.ListCAs(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	out := apigen.SSHCAList{Items: []apigen.SSHCA{}}
	for _, row := range rows {
		resp, err := sshCAResponse(row)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, resp)
	}
	return apigen.ListSshCas200JSONResponse(out), nil
}

func (a *API) CreateSshCa(ctx context.Context, req apigen.CreateSshCaRequestObject) (apigen.CreateSshCaResponseObject, error) {
	private := []byte(sshOptString(req.Body.PrivateKey))
	defer crypto.Zero(private)
	view, err := a.SSH.CreateCA(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), service.CreateSSHCARequest{
		Name: req.Body.Name, Algorithm: optAlgorithm(req.Body.Algorithm), PrivateKey: private,
	})
	if err != nil {
		return nil, err
	}
	resp, err := sshCAResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.CreateSshCa201JSONResponse(resp), nil
}

func (a *API) ShowSshCa(ctx context.Context, req apigen.ShowSshCaRequestObject) (apigen.ShowSshCaResponseObject, error) {
	view, err := a.SSH.GetCA(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.SshCA))
	if err != nil {
		return nil, err
	}
	resp, err := sshCAResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.ShowSshCa200JSONResponse(resp), nil
}

func (a *API) DeleteSshCa(ctx context.Context, req apigen.DeleteSshCaRequestObject) (apigen.DeleteSshCaResponseObject, error) {
	if err := a.SSH.DeleteCA(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.SshCA)); err != nil {
		return nil, err
	}
	return apigen.DeleteSshCa204Response{}, nil
}

func (a *API) RotateSshCa(ctx context.Context, req apigen.RotateSshCaRequestObject) (apigen.RotateSshCaResponseObject, error) {
	var rotate service.RotateSSHCARequest
	var private []byte
	if req.Body != nil {
		private = []byte(sshOptString(req.Body.PrivateKey))
		rotate = service.RotateSSHCARequest{Algorithm: optAlgorithm(req.Body.Algorithm), PrivateKey: private, OverlapSeconds: req.Body.OverlapSeconds}
	}
	defer crypto.Zero(private)
	view, err := a.SSH.RotateCA(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.SshCA), rotate)
	if err != nil {
		return nil, err
	}
	resp, err := sshCAResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.RotateSshCa200JSONResponse(resp), nil
}

func (a *API) RetireSshCaKey(ctx context.Context, req apigen.RetireSshCaKeyRequestObject) (apigen.RetireSshCaKeyResponseObject, error) {
	view, err := a.SSH.RetireCAKey(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.SshCA), string(req.SshCAKey))
	if err != nil {
		return nil, err
	}
	resp, err := sshCAResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.RetireSshCaKey200JSONResponse(resp), nil
}

func (a *API) GetSshTrustedKeys(ctx context.Context, req apigen.GetSshTrustedKeysRequestObject) (apigen.GetSshTrustedKeysResponseObject, error) {
	bundle, err := a.SSH.TrustedKeys(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.SshCA))
	if err != nil {
		return nil, err
	}
	return apigen.GetSshTrustedKeys200TextResponse(bundle), nil
}

func (a *API) GetSshKrl(ctx context.Context, req apigen.GetSshKrlRequestObject) (apigen.GetSshKrlResponseObject, error) {
	krl, err := a.SSH.KRL(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.SshCA))
	if err != nil {
		return nil, err
	}
	return apigen.GetSshKrl200ApplicationoctetStreamResponse{Body: bytes.NewReader(krl), ContentLength: int64(len(krl))}, nil
}

// --- Profiles -------------------------------------------------------------------

func sshProfileRequest(body *apigen.SSHProfileRequest) service.SSHProfileRequest {
	out := service.SSHProfileRequest{
		CAID: string(body.CaId), Name: body.Name, Principals: body.Principals, ForceCommand: sshOptString(body.ForceCommand),
		DefaultTTLSeconds: body.DefaultTtlSeconds, MaxTTLSeconds: body.MaxTtlSeconds, Enabled: body.Enabled == nil || *body.Enabled,
	}
	if body.SourceAddresses != nil {
		out.SourceAddresses = *body.SourceAddresses
	}
	if body.Extensions != nil {
		for _, e := range *body.Extensions {
			out.Extensions = append(out.Extensions, string(e))
		}
	}
	for _, a := range body.KeyAlgorithms {
		out.KeyAlgorithms = append(out.KeyAlgorithms, string(a))
	}
	if body.Requesters != nil {
		for _, r := range *body.Requesters {
			out.Requesters = append(out.Requesters, string(r))
		}
	}
	return out
}

func (a *API) ListSshProfiles(ctx context.Context, req apigen.ListSshProfilesRequestObject) (apigen.ListSshProfilesResponseObject, error) {
	rows, err := a.SSH.ListProfiles(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	out := apigen.SSHProfileList{Items: []apigen.SSHProfile{}}
	for _, row := range rows {
		resp, err := sshProfileResponse(row)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, resp)
	}
	return apigen.ListSshProfiles200JSONResponse(out), nil
}

func (a *API) CreateSshProfile(ctx context.Context, req apigen.CreateSshProfileRequestObject) (apigen.CreateSshProfileResponseObject, error) {
	view, err := a.SSH.CreateProfile(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), sshProfileRequest(req.Body))
	if err != nil {
		return nil, err
	}
	resp, err := sshProfileResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.CreateSshProfile201JSONResponse(resp), nil
}

func (a *API) ShowSshProfile(ctx context.Context, req apigen.ShowSshProfileRequestObject) (apigen.ShowSshProfileResponseObject, error) {
	view, err := a.SSH.GetProfile(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.SshProfile))
	if err != nil {
		return nil, err
	}
	resp, err := sshProfileResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.ShowSshProfile200JSONResponse(resp), nil
}

func (a *API) UpdateSshProfile(ctx context.Context, req apigen.UpdateSshProfileRequestObject) (apigen.UpdateSshProfileResponseObject, error) {
	view, err := a.SSH.UpdateProfile(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.SshProfile), sshProfileRequest(req.Body))
	if err != nil {
		return nil, err
	}
	resp, err := sshProfileResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.UpdateSshProfile200JSONResponse(resp), nil
}

func (a *API) DeleteSshProfile(ctx context.Context, req apigen.DeleteSshProfileRequestObject) (apigen.DeleteSshProfileResponseObject, error) {
	revoke := req.Params.RevokeIssued != nil && *req.Params.RevokeIssued
	n, err := a.SSH.DeleteProfile(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.SshProfile), revoke)
	if err != nil {
		return nil, err
	}
	return apigen.DeleteSshProfile200JSONResponse(apigen.SSHProfileDeletion{ProfileId: req.SshProfile, RevokedCertificateCount: int64(n)}), nil
}

// --- Certificates -----------------------------------------------------------------

func (a *API) ListSshCertificates(ctx context.Context, req apigen.ListSshCertificatesRequestObject) (apigen.ListSshCertificatesResponseObject, error) {
	rows, err := a.SSH.ListCertificates(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	out := apigen.SSHCertificateList{Items: []apigen.SSHCertificate{}}
	for _, row := range rows {
		resp, err := sshCertificateResponse(row)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, resp)
	}
	return apigen.ListSshCertificates200JSONResponse(out), nil
}

func (a *API) IssueSshCertificate(ctx context.Context, req apigen.IssueSshCertificateRequestObject) (apigen.IssueSshCertificateResponseObject, error) {
	body := req.Body
	issue := service.IssueSSHCertificateRequest{
		ProfileID: string(body.ProfileId), PublicKey: sshOptString(body.PublicKey), KeyAlgorithm: optAlgorithm(body.KeyAlgorithm),
	}
	if body.Principals != nil {
		issue.Principals = *body.Principals
	}
	if body.SourceAddresses != nil {
		issue.SourceAddresses = *body.SourceAddresses
	}
	if body.Extensions != nil {
		issue.Extensions = make([]string, 0, len(*body.Extensions)) // non-nil: explicit, possibly empty
		for _, e := range *body.Extensions {
			issue.Extensions = append(issue.Extensions, string(e))
		}
	}
	if body.TtlSeconds != nil {
		issue.TTLSeconds = *body.TtlSeconds
	}
	result, err := a.SSH.Issue(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), issue)
	if err != nil {
		return nil, err
	}
	cert, err := sshCertificateResponse(result.Certificate)
	if err != nil {
		return nil, err
	}
	out := apigen.SSHCertificateIssue{Certificate: cert, CertificateText: result.CertificateText, PublicKey: result.PublicKey}
	if result.PrivateKey != "" {
		private := result.PrivateKey
		out.PrivateKey = &private
	}
	return apigen.IssueSshCertificate200JSONResponse(out), nil
}

func (a *API) ShowSshCertificate(ctx context.Context, req apigen.ShowSshCertificateRequestObject) (apigen.ShowSshCertificateResponseObject, error) {
	view, err := a.SSH.GetCertificate(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.SshCertificate))
	if err != nil {
		return nil, err
	}
	resp, err := sshCertificateResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.ShowSshCertificate200JSONResponse(resp), nil
}

func (a *API) RevokeSshCertificate(ctx context.Context, req apigen.RevokeSshCertificateRequestObject) (apigen.RevokeSshCertificateResponseObject, error) {
	view, err := a.SSH.RevokeCertificate(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), string(req.SshCertificate))
	if err != nil {
		return nil, err
	}
	resp, err := sshCertificateResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.RevokeSshCertificate200JSONResponse(resp), nil
}
