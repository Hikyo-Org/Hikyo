package server

import (
	"context"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// Private PKI (#154, docs/adr/pki.md). Instance issuer and profile routes run
// under `instance-config`; environment certificate routes under
// `issue-certificate` (writes) or `read` (inspection). No response type in
// this file has a field for a CA private key. The one private key that ever
// crosses the wire is a server-generated leaf key, in the issue response only.

func seconds(p *int64) time.Duration {
	if p == nil {
		return 0
	}
	return time.Duration(*p) * time.Second
}

func pkiIssuerResponse(v service.PKIIssuerView) apigen.PkiIssuer {
	return apigen.PkiIssuer{
		Id: v.ID, Name: v.Name, Version: v.Version, Kind: apigen.PkiIssuerKind(v.Kind),
		Origin: apigen.PkiIssuerOrigin(v.Origin), ParentId: optString(v.ParentID), State: apigen.PkiIssuerState(v.State),
		KeyAlgorithm: v.KeyAlgorithm, KeyFingerprint: v.KeyFingerprint, CertificatePem: optString(v.CertificatePEM),
		CsrPem: optString(v.CSRPEM), ChainPem: optString(v.ChainPEM), SubjectCn: v.SubjectCN,
		SubjectOrg: optString(v.SubjectOrg), NotBefore: v.NotBefore, NotAfter: v.NotAfter,
		CrlDistributionUrl: optString(v.CRLDistributionURL), RestoreHold: v.RestoreHold, IssuedCount: v.IssuedCount,
		CrlNumber: v.CRLNumber, CrlThisUpdate: v.CRLThisUpdate, CrlNextUpdate: v.CRLNextUpdate,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func pkiIssuerList(views []service.PKIIssuerView) apigen.PkiIssuerList {
	out := apigen.PkiIssuerList{Issuers: make([]apigen.PkiIssuer, 0, len(views))}
	for _, v := range views {
		out.Issuers = append(out.Issuers, pkiIssuerResponse(v))
	}
	return out
}

func pkiPolicyResponse(p pki.Policy) apigen.PkiPolicy {
	out := apigen.PkiPolicy{
		AllowedIssuers: nonNilStrings(p.AllowedIssuers), DnsPatterns: nonNilStrings(p.DNSPatterns),
		IpRanges: nonNilStrings(p.IPRanges), UriPatterns: nonNilStrings(p.URIPatterns),
		AllowWildcardNames: p.AllowWildcardNames, MaxTtlSeconds: int64(p.MaxTTL / time.Second),
		DefaultTtlSeconds: int64(p.DefaultTTL / time.Second), RenewWindowSeconds: int64(p.RenewWindow / time.Second),
		AllowCsr: p.AllowCSR, AllowGeneratedKey: p.AllowGeneratedKey, MachineIssuance: p.MachineIssuance,
		Organization: p.Organization, KeyAlgorithms: []apigen.PkiPolicyKeyAlgorithms{},
		KeyUsages: []apigen.PkiPolicyKeyUsages{}, ExtKeyUsages: []apigen.PkiPolicyExtKeyUsages{},
	}
	for _, a := range p.KeyAlgorithms {
		out.KeyAlgorithms = append(out.KeyAlgorithms, apigen.PkiPolicyKeyAlgorithms(a))
	}
	for _, u := range p.KeyUsages {
		out.KeyUsages = append(out.KeyUsages, apigen.PkiPolicyKeyUsages(u))
	}
	for _, u := range p.ExtKeyUsages {
		out.ExtKeyUsages = append(out.ExtKeyUsages, apigen.PkiPolicyExtKeyUsages(u))
	}
	return out
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func pkiPolicyRequest(p apigen.PkiPolicy) pki.Policy {
	out := pki.Policy{
		AllowedIssuers: p.AllowedIssuers, DNSPatterns: p.DnsPatterns, IPRanges: p.IpRanges, URIPatterns: p.UriPatterns,
		AllowWildcardNames: p.AllowWildcardNames, MaxTTL: time.Duration(p.MaxTtlSeconds) * time.Second,
		DefaultTTL: time.Duration(p.DefaultTtlSeconds) * time.Second, RenewWindow: time.Duration(p.RenewWindowSeconds) * time.Second,
		AllowCSR: p.AllowCsr, AllowGeneratedKey: p.AllowGeneratedKey, MachineIssuance: p.MachineIssuance,
		Organization: p.Organization,
	}
	for _, a := range p.KeyAlgorithms {
		out.KeyAlgorithms = append(out.KeyAlgorithms, pki.KeyAlgorithm(a))
	}
	for _, u := range p.KeyUsages {
		out.KeyUsages = append(out.KeyUsages, pki.KeyUsage(u))
	}
	for _, u := range p.ExtKeyUsages {
		out.ExtKeyUsages = append(out.ExtKeyUsages, pki.ExtKeyUsage(u))
	}
	return out
}

func pkiProfileResponse(v service.PKIProfileView) apigen.PkiProfile {
	out := apigen.PkiProfile{
		Id: v.ID, Name: v.Name, Policy: pkiPolicyResponse(v.Policy), RowVersion: v.RowVersion,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, Bindings: make([]apigen.PkiProfileBinding, 0, len(v.Bindings)),
	}
	for _, b := range v.Bindings {
		out.Bindings = append(out.Bindings, apigen.PkiProfileBinding{
			Id: b.ID, OrgId: b.OrgID, ProjectId: b.ProjectID, EnvironmentId: optString(b.EnvironmentID), CreatedAt: b.CreatedAt,
		})
	}
	return out
}

func certificateResponse(v service.CertificateView) apigen.Certificate {
	return apigen.Certificate{
		Id: v.ID, EnvironmentId: v.EnvironmentID, Profile: v.ProfileName, IssuerId: v.IssuerID,
		IssuerName: v.IssuerName, IssuerVersion: v.IssuerVersion, Serial: v.Serial,
		State: apigen.CertificateState(v.State), KeySource: apigen.CertificateKeySource(v.KeySource),
		KeyAlgorithm: v.KeyAlgorithm, KeyFingerprint: v.KeyFingerprint, CommonName: optString(v.CommonName),
		DnsNames: nonNilStrings(v.DNSNames), IpAddresses: nonNilStrings(v.IPAddresses), Uris: nonNilStrings(v.URIs),
		NotBefore: v.NotBefore, NotAfter: v.NotAfter, CertificatePem: optString(v.CertificatePEM),
		ChainPem: optString(v.ChainPEM), PrincipalId: v.PrincipalID, PrincipalClass: v.PrincipalClass,
		RenewedFrom: optString(v.RenewedFrom), RenewedBy: optString(v.RenewedBy), RevokedAt: v.RevokedAt,
		RevocationReason: optString(v.RevocationReason), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func (a *API) pkiService() (*service.PKI, error) {
	if a.PKI == nil {
		return nil, fmt.Errorf("%w: private PKI is not configured on this server", domain.ErrNotFound)
	}
	return a.PKI, nil
}

// --- Issuers -----------------------------------------------------------------

func (a *API) ListPkiIssuers(ctx context.Context, _ apigen.ListPkiIssuersRequestObject) (apigen.ListPkiIssuersResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	views, err := svc.ListIssuers(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	return apigen.ListPkiIssuers200JSONResponse(pkiIssuerList(views)), nil
}

func issuerRequest(mode, name, cn, org string, alg *string, ttl *int64, parent *string, crlURL *string, key []byte, cert, chain *string) service.PKIIssuerRequest {
	return service.PKIIssuerRequest{
		Mode: mode, Name: name, CommonName: cn, Organization: org, KeyAlgorithm: deref(alg),
		TTL: seconds(ttl), ParentName: deref(parent), CRLDistributionURL: deref(crlURL),
		PrivateKeyPEM: key, CertificatePEM: deref(cert), ChainPEM: deref(chain),
	}
}

func (a *API) CreatePkiIssuer(ctx context.Context, req apigen.CreatePkiIssuerRequestObject) (apigen.CreatePkiIssuerResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	b := req.Body
	key := []byte(deref(b.PrivateKeyPem))
	defer crypto.Zero(key)
	var alg *string
	if b.KeyAlgorithm != nil {
		alg = strPtr(b.KeyAlgorithm)
	}
	result, err := svc.CreateIssuer(ctx, service.Bearer(bearer(ctx)), issuerRequest(string(b.Mode), b.Name,
		deref(b.CommonName), deref(b.Organization), alg, b.TtlSeconds, b.Parent, b.CrlDistributionUrl, key, b.CertificatePem, b.ChainPem))
	if err != nil {
		return nil, err
	}
	return apigen.CreatePkiIssuer200JSONResponse(pkiIssuerResponse(result.Issuer)), nil
}

func (a *API) ShowPkiIssuer(ctx context.Context, req apigen.ShowPkiIssuerRequestObject) (apigen.ShowPkiIssuerResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	views, err := svc.ShowIssuer(ctx, service.Bearer(bearer(ctx)), req.Issuer)
	if err != nil {
		return nil, err
	}
	return apigen.ShowPkiIssuer200JSONResponse(pkiIssuerList(views)), nil
}

func (a *API) RotatePkiIssuer(ctx context.Context, req apigen.RotatePkiIssuerRequestObject) (apigen.RotatePkiIssuerResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	b := apigen.PkiIssuerRotateRequest{}
	if req.Body != nil {
		b = *req.Body
	}
	key := []byte(deref(b.PrivateKeyPem))
	defer crypto.Zero(key)
	var alg *string
	if b.KeyAlgorithm != nil {
		alg = strPtr(b.KeyAlgorithm)
	}
	result, err := svc.RotateIssuer(ctx, service.Bearer(bearer(ctx)), req.Issuer,
		issuerRequest("", req.Issuer, "", "", alg, b.TtlSeconds, nil, b.CrlDistributionUrl, key, b.CertificatePem, b.ChainPem))
	if err != nil {
		return nil, err
	}
	return apigen.RotatePkiIssuer200JSONResponse(pkiIssuerResponse(result.Issuer)), nil
}

func (a *API) InstallPkiIssuerCertificate(ctx context.Context, req apigen.InstallPkiIssuerCertificateRequestObject) (apigen.InstallPkiIssuerCertificateResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	view, err := svc.InstallIssuerCertificate(ctx, service.Bearer(bearer(ctx)), req.Issuer, req.Body.CertificatePem, req.Body.ChainPem)
	if err != nil {
		return nil, err
	}
	return apigen.InstallPkiIssuerCertificate200JSONResponse(pkiIssuerResponse(view)), nil
}

func (a *API) ReleasePkiIssuerHold(ctx context.Context, req apigen.ReleasePkiIssuerHoldRequestObject) (apigen.ReleasePkiIssuerHoldResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	views, err := svc.ReleaseIssuerHold(ctx, service.Bearer(bearer(ctx)), req.Issuer)
	if err != nil {
		return nil, err
	}
	return apigen.ReleasePkiIssuerHold200JSONResponse(pkiIssuerList(views)), nil
}

func (a *API) RetirePkiIssuer(ctx context.Context, req apigen.RetirePkiIssuerRequestObject) (apigen.RetirePkiIssuerResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	view, err := svc.RetireIssuer(ctx, service.Bearer(bearer(ctx)), req.Issuer, req.Version)
	if err != nil {
		return nil, err
	}
	return apigen.RetirePkiIssuer200JSONResponse(pkiIssuerResponse(view)), nil
}

func (a *API) RevokePkiIssuer(ctx context.Context, req apigen.RevokePkiIssuerRequestObject) (apigen.RevokePkiIssuerResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	view, err := svc.RevokeIssuer(ctx, service.Bearer(bearer(ctx)), req.Issuer, req.Version)
	if err != nil {
		return nil, err
	}
	return apigen.RevokePkiIssuer200JSONResponse(pkiIssuerResponse(view)), nil
}

func (a *API) GetPkiIssuerCrl(ctx context.Context, req apigen.GetPkiIssuerCrlRequestObject) (apigen.GetPkiIssuerCrlResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	der, err := svc.IssuerCRL(ctx, service.Bearer(bearer(ctx)), req.Issuer, req.Version)
	if err != nil {
		return nil, err
	}
	return apigen.GetPkiIssuerCrl200JSONResponse(apigen.PkiCrl{CrlPem: pki.CRLPEM(der)}), nil
}

func (a *API) PublishPkiIssuerCrl(ctx context.Context, req apigen.PublishPkiIssuerCrlRequestObject) (apigen.PublishPkiIssuerCrlResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	view, err := svc.PublishIssuerCRL(ctx, service.Bearer(bearer(ctx)), req.Issuer, req.Version)
	if err != nil {
		return nil, err
	}
	return apigen.PublishPkiIssuerCrl200JSONResponse(pkiIssuerResponse(view)), nil
}

// --- Profiles ------------------------------------------------------------------

func (a *API) ListPkiProfiles(ctx context.Context, _ apigen.ListPkiProfilesRequestObject) (apigen.ListPkiProfilesResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	views, err := svc.ListProfiles(ctx, service.Bearer(bearer(ctx)))
	if err != nil {
		return nil, err
	}
	out := apigen.PkiProfileList{Profiles: make([]apigen.PkiProfile, 0, len(views))}
	for _, v := range views {
		out.Profiles = append(out.Profiles, pkiProfileResponse(v))
	}
	return apigen.ListPkiProfiles200JSONResponse(out), nil
}

func (a *API) CreatePkiProfile(ctx context.Context, req apigen.CreatePkiProfileRequestObject) (apigen.CreatePkiProfileResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	view, err := svc.CreateProfile(ctx, service.Bearer(bearer(ctx)), req.Body.Name, pkiPolicyRequest(req.Body.Policy))
	if err != nil {
		return nil, err
	}
	return apigen.CreatePkiProfile200JSONResponse(pkiProfileResponse(view)), nil
}

func (a *API) ShowPkiProfile(ctx context.Context, req apigen.ShowPkiProfileRequestObject) (apigen.ShowPkiProfileResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	view, err := svc.ShowProfile(ctx, service.Bearer(bearer(ctx)), req.Profile)
	if err != nil {
		return nil, err
	}
	return apigen.ShowPkiProfile200JSONResponse(pkiProfileResponse(view)), nil
}

func (a *API) UpdatePkiProfile(ctx context.Context, req apigen.UpdatePkiProfileRequestObject) (apigen.UpdatePkiProfileResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	var expected int64
	if req.Body.RowVersion != nil {
		expected = *req.Body.RowVersion
	}
	view, err := svc.UpdateProfile(ctx, service.Bearer(bearer(ctx)), req.Profile, pkiPolicyRequest(req.Body.Policy), expected)
	if err != nil {
		return nil, err
	}
	return apigen.UpdatePkiProfile200JSONResponse(pkiProfileResponse(view)), nil
}

func (a *API) DeletePkiProfile(ctx context.Context, req apigen.DeletePkiProfileRequestObject) (apigen.DeletePkiProfileResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	if err := svc.DeleteProfile(ctx, service.Bearer(bearer(ctx)), req.Profile); err != nil {
		return nil, err
	}
	return apigen.DeletePkiProfile204Response{}, nil
}

func (a *API) BindPkiProfile(ctx context.Context, req apigen.BindPkiProfileRequestObject) (apigen.BindPkiProfileResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	view, err := svc.BindProfile(ctx, service.Bearer(bearer(ctx)), req.Profile, req.Body.OrgId, req.Body.ProjectId, deref(req.Body.EnvironmentId))
	if err != nil {
		return nil, err
	}
	return apigen.BindPkiProfile200JSONResponse(pkiProfileResponse(view)), nil
}

func (a *API) UnbindPkiProfile(ctx context.Context, req apigen.UnbindPkiProfileRequestObject) (apigen.UnbindPkiProfileResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	view, err := svc.UnbindProfile(ctx, service.Bearer(bearer(ctx)), req.Profile, req.Binding)
	if err != nil {
		return nil, err
	}
	return apigen.UnbindPkiProfile200JSONResponse(pkiProfileResponse(view)), nil
}

// --- Certificates -----------------------------------------------------------------

func (a *API) ListCertificateProfiles(ctx context.Context, req apigen.ListCertificateProfilesRequestObject) (apigen.ListCertificateProfilesResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	views, err := svc.BoundProfiles(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	out := apigen.CertificateProfileList{Profiles: make([]apigen.CertificateProfile, 0, len(views))}
	for _, v := range views {
		out.Profiles = append(out.Profiles, apigen.CertificateProfile{Name: v.Name, Policy: pkiPolicyResponse(v.Policy)})
	}
	return apigen.ListCertificateProfiles200JSONResponse(out), nil
}

func (a *API) ListCertificates(ctx context.Context, req apigen.ListCertificatesRequestObject) (apigen.ListCertificatesResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	views, err := svc.ListCertificates(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	out := apigen.CertificateList{Certificates: make([]apigen.Certificate, 0, len(views))}
	for _, v := range views {
		out.Certificates = append(out.Certificates, certificateResponse(v))
	}
	return apigen.ListCertificates200JSONResponse(out), nil
}

func stringsOf(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

func (a *API) IssueCertificate(ctx context.Context, req apigen.IssueCertificateRequestObject) (apigen.IssueCertificateResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	b := req.Body
	issue := service.CertificateIssueRequest{
		Profile: b.Profile, Issuer: deref(b.Issuer), CSRPEM: deref(b.CsrPem),
		GenerateKey: b.GenerateKey != nil && *b.GenerateKey, CommonName: deref(b.CommonName),
		DNSNames: stringsOf(b.DnsNames), IPAddresses: stringsOf(b.IpAddresses), URIs: stringsOf(b.Uris),
		TTL: seconds(b.TtlSeconds),
	}
	if b.KeyAlgorithm != nil {
		issue.KeyAlgorithm = string(*b.KeyAlgorithm)
	}
	result, err := svc.IssueCertificate(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), issue)
	if err != nil {
		return nil, err
	}
	out := apigen.CertificateIssueResult{Certificate: certificateResponse(result.Certificate)}
	if result.PrivateKeyPEM != nil {
		// The display-once key: this response is its only copy. The Go string
		// the encoder needs cannot be zeroed; the source buffer is.
		key := string(result.PrivateKeyPEM)
		crypto.Zero(result.PrivateKeyPEM)
		out.PrivateKeyPem = &key
	}
	return apigen.IssueCertificate200JSONResponse(out), nil
}

func (a *API) ShowCertificate(ctx context.Context, req apigen.ShowCertificateRequestObject) (apigen.ShowCertificateResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	view, err := svc.ShowCertificate(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), req.Certificate)
	if err != nil {
		return nil, err
	}
	return apigen.ShowCertificate200JSONResponse(certificateResponse(view)), nil
}

func (a *API) RenewCertificate(ctx context.Context, req apigen.RenewCertificateRequestObject) (apigen.RenewCertificateResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	view, err := svc.RenewCertificate(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), req.Certificate)
	if err != nil {
		return nil, err
	}
	return apigen.RenewCertificate200JSONResponse(certificateResponse(view)), nil
}

func (a *API) RevokeCertificate(ctx context.Context, req apigen.RevokeCertificateRequestObject) (apigen.RevokeCertificateResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	reason := ""
	if req.Body != nil && req.Body.Reason != nil {
		reason = string(*req.Body.Reason)
	}
	view, err := svc.RevokeCertificate(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), req.Certificate, reason)
	if err != nil {
		return nil, err
	}
	return apigen.RevokeCertificate200JSONResponse(certificateResponse(view)), nil
}

func (a *API) GetCertificateCrl(ctx context.Context, req apigen.GetCertificateCrlRequestObject) (apigen.GetCertificateCrlResponseObject, error) {
	svc, err := a.pkiService()
	if err != nil {
		return nil, err
	}
	der, err := svc.CertificateCRL(ctx, service.Bearer(bearer(ctx)), dynamicEnvScope(req.Org, req.Project, req.Environment), req.Certificate)
	if err != nil {
		return nil, err
	}
	return apigen.GetCertificateCrl200JSONResponse(apigen.PkiCrl{CrlPem: pki.CRLPEM(der)}), nil
}
