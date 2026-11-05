package server

import (
	"bytes"
	"context"
	"io"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// chromeImageUpload names the raw-body avatar uploads, which carry a per-route
// body cap (service.MaxChromeImageBytes) instead of the global MaxRequestBytes.
var chromeImageUpload = map[string]bool{"setOrgIdentityImage": true, "setProjectIdentityImage": true}

// readChromeImage reads at most one byte past the cap, a second bound behind the
// route's MaxBytesReader, so an over-cap body is never buffered whole.
func readChromeImage(body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	return io.ReadAll(io.LimitReader(body, service.MaxChromeImageBytes+1))
}

func (a *API) SetOrgIdentity(ctx context.Context, req apigen.SetOrgIdentityRequestObject) (apigen.SetOrgIdentityResponseObject, error) {
	org, err := a.Orgs.SetIdentity(ctx, service.Bearer(bearer(ctx)), domain.OrgID(req.Org), req.Body.Hue, req.Body.Mark)
	if err != nil {
		return nil, err
	}
	return apigen.SetOrgIdentity200JSONResponse(wireOrg(org)), nil
}

func (a *API) SetOrgIdentityImage(ctx context.Context, req apigen.SetOrgIdentityImageRequestObject) (apigen.SetOrgIdentityImageResponseObject, error) {
	data, err := readChromeImage(req.Body)
	if err != nil {
		return nil, err
	}
	org, err := a.Orgs.SetIdentityImage(ctx, service.Bearer(bearer(ctx)), domain.OrgID(req.Org), req.ContentType, data)
	if err != nil {
		return nil, err
	}
	return apigen.SetOrgIdentityImage200JSONResponse(wireOrg(org)), nil
}

func (a *API) ClearOrgIdentityImage(ctx context.Context, req apigen.ClearOrgIdentityImageRequestObject) (apigen.ClearOrgIdentityImageResponseObject, error) {
	org, err := a.Orgs.ClearIdentityImage(ctx, service.Bearer(bearer(ctx)), domain.OrgID(req.Org))
	if err != nil {
		return nil, err
	}
	return apigen.ClearOrgIdentityImage200JSONResponse(wireOrg(org)), nil
}

// GetOrgIdentityImage serves the org avatar to the callers listMyOrgs lists the
// org to, and to other callers holding read@org.
func (a *API) GetOrgIdentityImage(ctx context.Context, req apigen.GetOrgIdentityImageRequestObject) (apigen.GetOrgIdentityImageResponseObject, error) {
	img, err := a.Orgs.IdentityImage(ctx, service.Bearer(bearer(ctx)), domain.OrgID(req.Org))
	if err != nil {
		if wireErrorFor(err).code == apigen.ErrorCodeUnauthenticated {
			return apigen.GetOrgIdentityImage401JSONResponse{UnauthenticatedJSONResponse: apigen.UnauthenticatedJSONResponse(errorBody(apigen.ErrorCodeUnauthenticated, ""))}, nil
		}
		return nil, err
	}
	return apigen.GetOrgIdentityImage200ImageResponse{Body: bytes.NewReader(img.Bytes), ContentType: img.ContentType, ContentLength: int64(len(img.Bytes))}, nil
}

func (a *API) SetProjectIdentity(ctx context.Context, req apigen.SetProjectIdentityRequestObject) (apigen.SetProjectIdentityResponseObject, error) {
	project, err := a.Projects.SetIdentity(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project), req.Body.Hue, req.Body.Mark)
	if err != nil {
		return nil, err
	}
	return apigen.SetProjectIdentity200JSONResponse(wireProject(project)), nil
}

func (a *API) SetProjectIdentityImage(ctx context.Context, req apigen.SetProjectIdentityImageRequestObject) (apigen.SetProjectIdentityImageResponseObject, error) {
	data, err := readChromeImage(req.Body)
	if err != nil {
		return nil, err
	}
	project, err := a.Projects.SetIdentityImage(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project), req.ContentType, data)
	if err != nil {
		return nil, err
	}
	return apigen.SetProjectIdentityImage200JSONResponse(wireProject(project)), nil
}

func (a *API) ClearProjectIdentityImage(ctx context.Context, req apigen.ClearProjectIdentityImageRequestObject) (apigen.ClearProjectIdentityImageResponseObject, error) {
	project, err := a.Projects.ClearIdentityImage(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project))
	if err != nil {
		return nil, err
	}
	return apigen.ClearProjectIdentityImage200JSONResponse(wireProject(project)), nil
}

func (a *API) GetProjectIdentityImage(ctx context.Context, req apigen.GetProjectIdentityImageRequestObject) (apigen.GetProjectIdentityImageResponseObject, error) {
	img, err := a.Projects.IdentityImage(ctx, service.Bearer(bearer(ctx)), projectScope(req.Org, req.Project))
	if err != nil {
		return nil, err
	}
	return apigen.GetProjectIdentityImage200ImageResponse{Body: bytes.NewReader(img.Bytes), ContentType: img.ContentType, ContentLength: int64(len(img.Bytes))}, nil
}
