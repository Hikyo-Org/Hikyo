package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// Transit handlers (#156, transit ADR D10). They decode base64 inputs, call the
// service and render its views; every decision is the service's. Plaintext
// that crosses this layer (decrypt output, a revealed data key) is zeroed as
// soon as the response value is built.

// transitScope builds the environment scope addressed by a transit request.
func transitScope(org apigen.OrgID, project apigen.ProjectID, env apigen.EnvironmentID) domain.Scope {
	return domain.Scope{Org: domain.OrgID(org), Project: domain.ProjectID(project), Env: domain.EnvID(env)}
}

// decodeB64 decodes a standard-base64 request member. The contract's pattern
// has already bounded it; a residual decode failure is a caller error.
func decodeB64(field string, v *string) ([]byte, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	b, err := base64.StdEncoding.Strict().DecodeString(*v)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not standard base64", domain.ErrInvalid, field)
	}
	return b, nil
}

// optVersion maps an omitted version to zero (latest). Explicit values outside
// 1 through the maximum uint32 return an error wrapping domain.ErrInvalid.
func optVersion(v *int64) (uint32, error) {
	if v == nil {
		return 0, nil
	}
	if *v < 1 || *v > int64(^uint32(0)) {
		return 0, fmt.Errorf("%w: key_version out of range", domain.ErrInvalid)
	}
	return uint32(*v), nil
}

// transitKeyResponse converts key metadata to an API response, encoding public
// keys as standard base64. Malformed key or version timestamps return errors.
func transitKeyResponse(v service.TransitKeyView) (apigen.TransitKey, error) {
	created, err := time.Parse(time.RFC3339Nano, v.CreatedAt)
	if err != nil {
		return apigen.TransitKey{}, fmt.Errorf("server: parse transit key created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339Nano, v.UpdatedAt)
	if err != nil {
		return apigen.TransitKey{}, fmt.Errorf("server: parse transit key updated_at: %w", err)
	}
	deletionAfter, err := parseOptTime(v.DeletionAfter)
	if err != nil {
		return apigen.TransitKey{}, err
	}
	out := apigen.TransitKey{
		Id: apigen.ID(v.ID), EnvironmentId: apigen.ID(v.EnvironmentID), Name: v.Name,
		Algorithm: apigen.TransitAlgorithm(v.Algorithm), Custody: apigen.TransitCustody(v.Custody),
		AllowedOperations: []apigen.TransitOperation{}, Exportable: false,
		State: apigen.TransitKeyState(v.State), LatestVersion: int64(v.LatestVersion),
		MinEncryptVersion: int64(v.MinEncryptVersion), MinDecryptVersion: int64(v.MinDecryptVersion),
		CompromisedThroughVersion: int64(v.CompromisedThroughVersion),
		RotationPeriodSeconds:     v.RotationPeriodSeconds, RotationDue: v.RotationDue,
		DeletionAfter: deletionAfter, CreatedBy: v.CreatedBy, CreatedAt: created, UpdatedAt: updated,
		Versions: []apigen.TransitKeyVersion{}, Callers: []apigen.TransitCaller{},
	}
	for _, op := range v.AllowedOperations {
		out.AllowedOperations = append(out.AllowedOperations, apigen.TransitOperation(op))
	}
	for _, ver := range v.Versions {
		createdAt, err := time.Parse(time.RFC3339Nano, ver.CreatedAt)
		if err != nil {
			return apigen.TransitKey{}, fmt.Errorf("server: parse transit version created_at: %w", err)
		}
		material := apigen.TransitKeyVersionMaterialErased
		switch {
		case ver.HasMaterial:
			material = apigen.TransitKeyVersionMaterialSealed
		case ver.ExternalHeld:
			material = apigen.TransitKeyVersionMaterialExternal
		}
		item := apigen.TransitKeyVersion{Version: int64(ver.Version), Material: material, CreatedAt: createdAt}
		if len(ver.PublicKey) > 0 {
			public := base64.StdEncoding.EncodeToString(ver.PublicKey)
			item.PublicKey = &public
		}
		out.Versions = append(out.Versions, item)
	}
	for _, c := range v.Callers {
		entry := apigen.TransitCaller{PrincipalId: c.PrincipalID, Operations: []apigen.TransitOperation{}}
		for _, op := range c.Operations {
			entry.Operations = append(entry.Operations, apigen.TransitOperation(op))
		}
		out.Callers = append(out.Callers, entry)
	}
	return out, nil
}

// transitCallers converts caller entries while preserving nil as unchanged and
// a present empty list as a request to clear the entries.
func transitCallers(in *[]apigen.TransitCaller) *[]service.TransitCallerEntry {
	if in == nil {
		return nil
	}
	out := make([]service.TransitCallerEntry, 0, len(*in))
	for _, c := range *in {
		entry := service.TransitCallerEntry{PrincipalID: c.PrincipalId}
		for _, op := range c.Operations {
			entry.Operations = append(entry.Operations, string(op))
		}
		out = append(out, entry)
	}
	return &out
}

// --- Management -----------------------------------------------------------------

// ListTransitKeys returns the environment's key metadata. Service and response
// conversion errors propagate to the HTTP error handler.
func (a *API) ListTransitKeys(ctx context.Context, req apigen.ListTransitKeysRequestObject) (apigen.ListTransitKeysResponseObject, error) {
	views, err := a.Transit.ListKeys(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment))
	if err != nil {
		return nil, err
	}
	out := apigen.TransitKeyList{Items: []apigen.TransitKey{}}
	for _, v := range views {
		item, err := transitKeyResponse(v)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, item)
	}
	return apigen.ListTransitKeys200JSONResponse(out), nil
}

// CreateTransitKey creates a key and returns its metadata with status 201.
// Service and response conversion errors propagate to the HTTP error handler.
func (a *API) CreateTransitKey(ctx context.Context, req apigen.CreateTransitKeyRequestObject) (apigen.CreateTransitKeyResponseObject, error) {
	body := req.Body
	in := service.CreateTransitKeyRequest{Name: body.Name, Algorithm: string(body.Algorithm)}
	if body.Custody != nil {
		in.Custody = string(*body.Custody)
	}
	if body.AllowedOperations != nil {
		for _, op := range *body.AllowedOperations {
			in.AllowedOperations = append(in.AllowedOperations, string(op))
		}
	}
	if body.RotationPeriodSeconds != nil {
		in.RotationPeriodSeconds = *body.RotationPeriodSeconds
	}
	if body.Exportable != nil {
		in.Exportable = *body.Exportable
	}
	if callers := transitCallers(body.Callers); callers != nil {
		in.Callers = *callers
	}
	view, err := a.Transit.CreateKey(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), in)
	if err != nil {
		return nil, err
	}
	resp, err := transitKeyResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.CreateTransitKey201JSONResponse(resp), nil
}

// ShowTransitKey returns metadata for the named key. Service and response
// conversion errors propagate to the HTTP error handler.
func (a *API) ShowTransitKey(ctx context.Context, req apigen.ShowTransitKeyRequestObject) (apigen.ShowTransitKeyResponseObject, error) {
	view, err := a.Transit.GetKey(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey)
	if err != nil {
		return nil, err
	}
	resp, err := transitKeyResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.ShowTransitKey200JSONResponse(resp), nil
}

// ConfigureTransitKey applies supplied policy fields and returns updated metadata.
// Version validation, service, and response conversion errors propagate.
func (a *API) ConfigureTransitKey(ctx context.Context, req apigen.ConfigureTransitKeyRequestObject) (apigen.ConfigureTransitKeyResponseObject, error) {
	body := req.Body
	in := service.ConfigureTransitKeyRequest{RotationPeriodSeconds: body.RotationPeriodSeconds, Callers: transitCallers(body.Callers)}
	if body.MinEncryptVersion != nil {
		v, err := optVersion(body.MinEncryptVersion)
		if err != nil {
			return nil, err
		}
		in.MinEncryptVersion = &v
	}
	if body.MinDecryptVersion != nil {
		v, err := optVersion(body.MinDecryptVersion)
		if err != nil {
			return nil, err
		}
		in.MinDecryptVersion = &v
	}
	view, err := a.Transit.ConfigureKey(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey, in)
	if err != nil {
		return nil, err
	}
	resp, err := transitKeyResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.ConfigureTransitKey200JSONResponse(resp), nil
}

// RotateTransitKey appends a key version and returns updated metadata.
// Service and response conversion errors propagate to the HTTP error handler.
func (a *API) RotateTransitKey(ctx context.Context, req apigen.RotateTransitKeyRequestObject) (apigen.RotateTransitKeyResponseObject, error) {
	view, err := a.Transit.RotateKey(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey)
	if err != nil {
		return nil, err
	}
	resp, err := transitKeyResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.RotateTransitKey200JSONResponse(resp), nil
}

// ChangeTransitKeyState applies a lifecycle action with an optional deletion
// delay in seconds. Delay validation, service, and response conversion errors
// propagate to the HTTP error handler.
func (a *API) ChangeTransitKeyState(ctx context.Context, req apigen.ChangeTransitKeyStateRequestObject) (apigen.ChangeTransitKeyStateResponseObject, error) {
	var delay time.Duration
	if req.Body.DelaySeconds != nil {
		if *req.Body.DelaySeconds < 0 || *req.Body.DelaySeconds > int64(service.MaxTransitDeletionDelay/time.Second) {
			return nil, fmt.Errorf("%w: delay_seconds must be nonnegative and within the maximum deletion delay", domain.ErrInvalid)
		}
		delay = time.Duration(*req.Body.DelaySeconds) * time.Second
	}
	view, err := a.Transit.ChangeKeyState(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey, string(req.Body.Action), delay)
	if err != nil {
		return nil, err
	}
	resp, err := transitKeyResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.ChangeTransitKeyState200JSONResponse(resp), nil
}

// TrimTransitKey removes eligible old versions and returns the key metadata
// and deletion count. Service and response conversion errors propagate.
func (a *API) TrimTransitKey(ctx context.Context, req apigen.TrimTransitKeyRequestObject) (apigen.TrimTransitKeyResponseObject, error) {
	view, deleted, err := a.Transit.TrimKey(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey)
	if err != nil {
		return nil, err
	}
	resp, err := transitKeyResponse(view)
	if err != nil {
		return nil, err
	}
	return apigen.TrimTransitKey200JSONResponse(apigen.TransitTrimResult{Key: resp, VersionsDeleted: deleted}), nil
}

// --- Data plane -------------------------------------------------------------------

// TransitEncrypt decodes standard-base64 plaintext and context and returns
// versioned ciphertext. The decoded plaintext is zeroed on return; decoding,
// version validation, and service errors propagate.
func (a *API) TransitEncrypt(ctx context.Context, req apigen.TransitEncryptRequestObject) (apigen.TransitEncryptResponseObject, error) {
	plaintext, err := decodeB64("plaintext", &req.Body.Plaintext)
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(plaintext)
	aad, err := decodeB64("context", req.Body.Context)
	if err != nil {
		return nil, err
	}
	version, err := optVersion(req.Body.KeyVersion)
	if err != nil {
		return nil, err
	}
	out, err := a.Transit.Encrypt(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey, plaintext, aad, version)
	if err != nil {
		return nil, err
	}
	return apigen.TransitEncrypt200JSONResponse(apigen.TransitCiphertextResult{Ciphertext: out.Value, KeyVersion: int64(out.KeyVersion)}), nil
}

// TransitDecrypt returns standard-base64 plaintext and its key version, zeroing
// the decoded plaintext buffer after encoding. Context decoding and service
// errors propagate.
func (a *API) TransitDecrypt(ctx context.Context, req apigen.TransitDecryptRequestObject) (apigen.TransitDecryptResponseObject, error) {
	aad, err := decodeB64("context", req.Body.Context)
	if err != nil {
		return nil, err
	}
	out, err := a.Transit.Decrypt(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey, req.Body.Ciphertext, aad)
	if err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(out.Plaintext)
	crypto.Zero(out.Plaintext)
	return apigen.TransitDecrypt200JSONResponse(apigen.TransitDecryptResult{Plaintext: encoded, KeyVersion: int64(out.KeyVersion)}), nil
}

// TransitRewrap returns ciphertext under the latest permitted key version
// without returning plaintext. Context decoding and service errors propagate.
func (a *API) TransitRewrap(ctx context.Context, req apigen.TransitRewrapRequestObject) (apigen.TransitRewrapResponseObject, error) {
	aad, err := decodeB64("context", req.Body.Context)
	if err != nil {
		return nil, err
	}
	out, err := a.Transit.Rewrap(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey, req.Body.Ciphertext, aad)
	if err != nil {
		return nil, err
	}
	return apigen.TransitRewrap200JSONResponse(apigen.TransitCiphertextResult{Ciphertext: out.Value, KeyVersion: int64(out.KeyVersion)}), nil
}

// TransitDataKey returns a wrapped data key and, when requested, standard-base64
// plaintext. The plaintext buffer is zeroed after encoding. Context decoding and
// service errors propagate.
func (a *API) TransitDataKey(ctx context.Context, req apigen.TransitDataKeyRequestObject) (apigen.TransitDataKeyResponseObject, error) {
	aad, err := decodeB64("context", req.Body.Context)
	if err != nil {
		return nil, err
	}
	bits := 0
	if req.Body.Bits != nil {
		bits = int(*req.Body.Bits)
	}
	reveal := req.Body.Plaintext != nil && *req.Body.Plaintext
	out, err := a.Transit.DataKey(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey, bits, aad, reveal)
	if err != nil {
		return nil, err
	}
	result := apigen.TransitDataKeyResult{Ciphertext: out.Value, KeyVersion: int64(out.KeyVersion)}
	if out.Plaintext != nil {
		encoded := base64.StdEncoding.EncodeToString(out.Plaintext)
		crypto.Zero(out.Plaintext)
		result.Plaintext = &encoded
	}
	return apigen.TransitDataKey200JSONResponse(result), nil
}

// TransitSign decodes a standard-base64 message and returns a versioned
// signature. Decoding, version validation, and service errors propagate.
func (a *API) TransitSign(ctx context.Context, req apigen.TransitSignRequestObject) (apigen.TransitSignResponseObject, error) {
	message, err := decodeB64("message", &req.Body.Message)
	if err != nil {
		return nil, err
	}
	version, err := optVersion(req.Body.KeyVersion)
	if err != nil {
		return nil, err
	}
	out, err := a.Transit.Sign(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey, message, version)
	if err != nil {
		return nil, err
	}
	return apigen.TransitSign200JSONResponse(apigen.TransitSignatureResult{Signature: out.Value, KeyVersion: int64(out.KeyVersion)}), nil
}

// TransitVerify returns the signature verification result and key version.
// A mismatch returns Valid=false with status 200; decoding and service errors
// propagate to the HTTP error handler.
func (a *API) TransitVerify(ctx context.Context, req apigen.TransitVerifyRequestObject) (apigen.TransitVerifyResponseObject, error) {
	message, err := decodeB64("message", &req.Body.Message)
	if err != nil {
		return nil, err
	}
	out, err := a.Transit.Verify(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey, message, req.Body.Signature)
	if err != nil {
		return nil, err
	}
	return apigen.TransitVerify200JSONResponse(apigen.TransitVerifyResult{Valid: out.Valid, KeyVersion: int64(out.KeyVersion)}), nil
}

// TransitHMAC decodes a standard-base64 message and returns a versioned MAC.
// Decoding, version validation, and service errors propagate.
func (a *API) TransitHMAC(ctx context.Context, req apigen.TransitHMACRequestObject) (apigen.TransitHMACResponseObject, error) {
	message, err := decodeB64("message", &req.Body.Message)
	if err != nil {
		return nil, err
	}
	version, err := optVersion(req.Body.KeyVersion)
	if err != nil {
		return nil, err
	}
	out, err := a.Transit.HMAC(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey, message, version)
	if err != nil {
		return nil, err
	}
	return apigen.TransitHMAC200JSONResponse(apigen.TransitHMACResult{Mac: out.Value, KeyVersion: int64(out.KeyVersion)}), nil
}

// TransitVerifyHMAC returns the MAC verification result and key version.
// A mismatch returns Valid=false with status 200; decoding and service errors
// propagate to the HTTP error handler.
func (a *API) TransitVerifyHMAC(ctx context.Context, req apigen.TransitVerifyHMACRequestObject) (apigen.TransitVerifyHMACResponseObject, error) {
	message, err := decodeB64("message", &req.Body.Message)
	if err != nil {
		return nil, err
	}
	out, err := a.Transit.VerifyHMAC(ctx, service.Bearer(bearer(ctx)), transitScope(req.Org, req.Project, req.Environment), req.TransitKey, message, req.Body.Mac)
	if err != nil {
		return nil, err
	}
	return apigen.TransitVerifyHMAC200JSONResponse(apigen.TransitVerifyResult{Valid: out.Valid, KeyVersion: int64(out.KeyVersion)}), nil
}
