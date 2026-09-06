package server

import (
	"context"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func wireRevisionDiff(diff service.RevisionDiff) apigen.RevisionDiff {
	out := apigen.RevisionDiff{LeftRevision: diff.LeftRevision, RightRevision: diff.RightRevision, Items: []apigen.RevisionDiffRow{}}
	for _, row := range diff.Items {
		out.Items = append(out.Items, apigen.RevisionDiffRow{KeyId: row.KeyID, Name: row.Name, Classification: apigen.KeyClassification(row.Classification), Status: apigen.RevisionDiffRowStatus(row.Status), Revealed: row.Revealed, Before: row.Before, After: row.After})
	}
	return out
}
func (a *API) DiffRevisions(ctx context.Context, req apigen.DiffRevisionsRequestObject) (apigen.DiffRevisionsResponseObject, error) {
	out, err := a.Revisions.Diff(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), req.Body.LeftRevision, req.Body.RightRevision, "")
	if err != nil {
		return nil, err
	}
	return apigen.DiffRevisions200JSONResponse(wireRevisionDiff(out)), nil
}
func (a *API) RevealRevisionDiff(ctx context.Context, req apigen.RevealRevisionDiffRequestObject) (apigen.RevealRevisionDiffResponseObject, error) {
	out, err := a.Revisions.Diff(ctx, service.Bearer(bearer(ctx)), envScope(req.Org, req.Project, req.Environment), req.Body.LeftRevision, req.Body.RightRevision, req.Body.KeyId)
	if err != nil {
		return nil, err
	}
	return apigen.RevealRevisionDiff200JSONResponse(wireRevisionDiff(out)), nil
}
