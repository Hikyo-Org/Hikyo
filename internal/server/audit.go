package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// auditExportPageSize bounds each page the export reads from the store. It is
// well under the store's own 1000-row cap so a single export page is a bounded
// unit of work and memory; the ops spec owns the ceiling, this is the read
// granularity the transport asks for.
const auditExportPageSize = 500

// auditPrincipal resolves the caller's session to a principal. The audit
// service is principal-keyed (its export budget is charged before it touches
// the store, so it cannot itself authenticate); the transport resolves the
// session here, exactly as `whoami` does, and hands the principal down.
func (a *API) auditPrincipal(ctx context.Context) (domain.PrincipalID, error) {
	id, err := a.Auth.Identity(ctx, bearer(ctx))
	if err != nil {
		return "", err
	}
	return id.Principal, nil
}

// auditQueryFilter builds the store filter from the paged query parameters. An
// absent limit defaults to 100; the store clamps anything above its cap.
func auditQueryFilter(from, to *time.Time, afterSeq, toSeq *int64, limit *int, actor, operation, outcome, objectType, objectID, correlation *string) service.AuditFilter {
	f := auditFilterFields(from, to, actor, operation, outcome, objectType, objectID, correlation)
	f.Limit = 100
	if afterSeq != nil {
		f.AfterSeq = *afterSeq
	}
	if toSeq != nil {
		f.ToSeq = *toSeq
	}
	if limit != nil {
		f.Limit = *limit
	}
	return f
}

// auditFilterFields sets the equality and time-range fields shared by query and
// export. The store applies the equality fields after the authorized page read.
func auditFilterFields(from, to *time.Time, actor, operation, outcome, objectType, objectID, correlation *string) service.AuditFilter {
	var f service.AuditFilter
	if from != nil {
		f.From = *from
	}
	if to != nil {
		f.To = *to
	}
	if actor != nil {
		f.Actor = *actor
	}
	if operation != nil {
		f.Type = *operation
	}
	if outcome != nil {
		f.Outcome = *outcome
	}
	if objectType != nil {
		f.ObjectType = *objectType
	}
	if objectID != nil {
		f.ObjectID = *objectID
	}
	if correlation != nil {
		f.CorrelationID = *correlation
	}
	return f
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// outcomeStr narrows a generated, per-operation outcome enum pointer to a plain
// string pointer for the shared filter builder. The enum members are validated
// against the closed set by request validation before the handler runs.
func outcomeStr[T ~string](p *T) *string {
	if p == nil {
		return nil
	}
	s := string(*p)
	return &s
}

// wireAuditPage renders a service page as the wire response. It unmarshals each
// event's stored payload so additive members render rather than fail.
func wireAuditPage(page service.AuditPage) (apigen.AuditPage, error) {
	items := make([]apigen.AuditEvent, 0, len(page.Events))
	for _, e := range page.Events {
		wire, err := wireAuditEvent(e)
		if err != nil {
			return apigen.AuditPage{}, err
		}
		wire.ActorName = optStr(page.ActorNames[e.Actor.ID])
		items = append(items, wire)
	}
	return apigen.AuditPage{
		Items:        items,
		Count:        len(items),
		NextAfterSeq: page.NextSeq,
		UpperSeq:     page.UpperSeq,
		Exhausted:    page.Exhausted,
	}, nil
}

func wireAuditEvent(e service.AuditEvent) (apigen.AuditEvent, error) {
	payload := map[string]interface{}{}
	if e.RawPayload != "" {
		if err := json.Unmarshal([]byte(e.RawPayload), &payload); err != nil {
			return apigen.AuditEvent{}, fmt.Errorf("server: audit event %s: stored payload is not an object: %w", e.ID, err)
		}
	}
	return apigen.AuditEvent{
		Seq:               e.Seq,
		Id:                e.ID,
		Type:              string(e.Type),
		SchemaVersion:     e.SchemaVersion,
		OccurredAt:        e.OccurredAt,
		OccurredAsserted:  e.OccurredAsserted,
		RecordedAt:        e.RecordedAt,
		ActorId:           optStr(e.Actor.ID),
		ActorClass:        string(e.Actor.Class),
		ActorCredentialId: optStr(e.Actor.CredentialID),
		AuthorityId:       optStr(e.AuthorityID),
		ScopeClass:        e.ScopeClass,
		OrgId:             optStr(e.OrgID),
		ProjectId:         optStr(e.ProjectID),
		EnvId:             optStr(e.EnvID),
		ObjectType:        optStr(e.Object.Type),
		ObjectId:          optStr(e.Object.ID),
		Outcome:           string(e.Outcome),
		CorrelationId:     optStr(e.CorrelationID),
		SourceIp:          optStr(e.SourceIP),
		UserAgent:         optStr(e.UserAgent),
		Origin:            string(e.Origin),
		Payload:           payload,
	}, nil
}

// --- query handlers ---

func (a *API) QueryOrgAudit(ctx context.Context, req apigen.QueryOrgAuditRequestObject) (apigen.QueryOrgAuditResponseObject, error) {
	principal, err := a.auditPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	f := auditQueryFilter(p.From, p.To, p.AfterSeq, p.ToSeq, p.Limit, p.Actor, p.Operation, outcomeStr(p.Outcome), p.ObjectType, p.ObjectId, p.CorrelationId)
	page, err := a.Audits.Query(ctx, principal, domain.Scope{Org: domain.OrgID(req.Org)}, f)
	if err != nil {
		return nil, err
	}
	wire, err := wireAuditPage(page)
	if err != nil {
		return nil, err
	}
	return apigen.QueryOrgAudit200JSONResponse(wire), nil
}

func (a *API) QueryProjectAudit(ctx context.Context, req apigen.QueryProjectAuditRequestObject) (apigen.QueryProjectAuditResponseObject, error) {
	principal, err := a.auditPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	f := auditQueryFilter(p.From, p.To, p.AfterSeq, p.ToSeq, p.Limit, p.Actor, p.Operation, outcomeStr(p.Outcome), p.ObjectType, p.ObjectId, p.CorrelationId)
	page, err := a.Audits.Query(ctx, principal, projectScope(req.Org, req.Project), f)
	if err != nil {
		return nil, err
	}
	wire, err := wireAuditPage(page)
	if err != nil {
		return nil, err
	}
	return apigen.QueryProjectAudit200JSONResponse(wire), nil
}

func (a *API) QueryEnvAudit(ctx context.Context, req apigen.QueryEnvAuditRequestObject) (apigen.QueryEnvAuditResponseObject, error) {
	principal, err := a.auditPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	f := auditQueryFilter(p.From, p.To, p.AfterSeq, p.ToSeq, p.Limit, p.Actor, p.Operation, outcomeStr(p.Outcome), p.ObjectType, p.ObjectId, p.CorrelationId)
	page, err := a.Audits.Query(ctx, principal, envScope(req.Org, req.Project, req.Environment), f)
	if err != nil {
		return nil, err
	}
	wire, err := wireAuditPage(page)
	if err != nil {
		return nil, err
	}
	return apigen.QueryEnvAudit200JSONResponse(wire), nil
}

// --- export handlers ---

func (a *API) ExportOrgAudit(ctx context.Context, req apigen.ExportOrgAuditRequestObject) (apigen.ExportOrgAuditResponseObject, error) {
	principal, err := a.auditPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	f := auditFilterFields(p.From, p.To, p.Actor, p.Operation, outcomeStr(p.Outcome), p.ObjectType, p.ObjectId, p.CorrelationId)
	return auditExportStream{
		export: func(w io.Writer) error {
			return a.Audits.Export(ctx, principal, domain.Scope{Org: domain.OrgID(req.Org)}, f, auditExportPageSize, w)
		},
		filename: auditFilename(req.Org),
	}, nil
}

func (a *API) ExportProjectAudit(ctx context.Context, req apigen.ExportProjectAuditRequestObject) (apigen.ExportProjectAuditResponseObject, error) {
	principal, err := a.auditPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	f := auditFilterFields(p.From, p.To, p.Actor, p.Operation, outcomeStr(p.Outcome), p.ObjectType, p.ObjectId, p.CorrelationId)
	return auditExportStream{
		export: func(w io.Writer) error {
			return a.Audits.Export(ctx, principal, projectScope(req.Org, req.Project), f, auditExportPageSize, w)
		},
		filename: auditFilename(req.Org, req.Project),
	}, nil
}

func (a *API) ExportEnvAudit(ctx context.Context, req apigen.ExportEnvAuditRequestObject) (apigen.ExportEnvAuditResponseObject, error) {
	principal, err := a.auditPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	f := auditFilterFields(p.From, p.To, p.Actor, p.Operation, outcomeStr(p.Outcome), p.ObjectType, p.ObjectId, p.CorrelationId)
	return auditExportStream{
		export: func(w io.Writer) error {
			return a.Audits.Export(ctx, principal, envScope(req.Org, req.Project, req.Environment), f, auditExportPageSize, w)
		},
		filename: auditFilename(req.Org, req.Project, req.Environment),
	}, nil
}

func auditFilename(parts ...string) string {
	name := "hikyo-audit"
	for _, p := range parts {
		name += "-" + p
	}
	return name + ".jsonl"
}

// auditExportStream streams the service export straight to the response body,
// so neither the server nor (via a native browser download) the client holds
// the whole trail in memory. The export writes its own INTENT/OUTCOME audit
// events regardless; a mid-stream failure leaves the 200 already sent and its
// cause durable in the trail, while a pre-stream refusal (authorization or
// budget) becomes the normal status because no byte was written yet.
type auditExportStream struct {
	export   func(w io.Writer) error
	filename string
}

func (s auditExportStream) visit(w http.ResponseWriter) error {
	tw := &auditExportWriter{w: w, filename: s.filename}
	err := s.export(tw)
	if err != nil {
		if !tw.wrote {
			// Pre-stream refusal: no byte on the wire yet, so the usual status
			// mapping still applies.
			writeError(w, wireErrorFor(err), "")
			return nil
		}
		// Mid-stream failure: the header is already 200 and the terminal audit
		// event recorded the cause. Nothing safe to add to the wire.
		return nil
	}
	if !tw.wrote {
		// A successful export that matched nothing still owes the caller the
		// headers and an empty 200 body.
		tw.writeHeader()
	}
	return nil
}

func (s auditExportStream) VisitExportOrgAuditResponse(w http.ResponseWriter) error {
	return s.visit(w)
}

func (s auditExportStream) VisitExportProjectAuditResponse(w http.ResponseWriter) error {
	return s.visit(w)
}

func (s auditExportStream) VisitExportEnvAuditResponse(w http.ResponseWriter) error {
	return s.visit(w)
}

// auditExportWriter defers the 200 header and download headers until the first
// byte, so a refusal the service raises before it streams anything can still
// set a non-200 status.
type auditExportWriter struct {
	w        http.ResponseWriter
	filename string
	wrote    bool
}

func (a *auditExportWriter) writeHeader() {
	a.wrote = true
	a.w.Header().Set("Content-Type", "application/x-ndjson")
	a.w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", a.filename))
	a.w.Header().Set("X-Content-Type-Options", "nosniff")
	a.w.WriteHeader(http.StatusOK)
}

func (a *auditExportWriter) Write(b []byte) (int, error) {
	if !a.wrote {
		a.writeHeader()
	}
	return a.w.Write(b)
}
