package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestOwnerRuntimeReloadsProjectKeyLimit(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	bootstrap, err := srv.owner.current.graph.auth.BootstrapAdmin(t.Context(), "key-limit-owner", "Owner", "terminal")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.selfConfig.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	actor := service.LocalPrincipal(bootstrap.PrincipalID)
	orgs := &service.Orgs{DB: srv.db}
	org, err := orgs.Create(t.Context(), actor, "capacity", true, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	projects := &service.Projects{DB: srv.db}
	project, err := projects.Create(t.Context(), actor, domain.OrgID(org.ID), "capacity")
	if err != nil {
		t.Fatal(err)
	}
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactBrowserSession)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := tx.Write(t.Context(), srv.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		generation, err := az.PrincipalGeneration(ctx, bootstrap.PrincipalID)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		return az.MintSession(ctx, authz.NewSession{ID: "ses_key_limit", PrincipalID: bootstrap.PrincipalID, Verifier: verifier, Artifact: "browser", SessionGeneration: generation, CredentialEpoch: epoch, AuthMethod: "local-passkey", Factors: `["webauthn"]`, AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour), SourceIP: "127.0.0.1", UserAgent: "key-limit-test"})
	}); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/orgs/%s/projects/%s/keys", org.ID, project.ID)
	create := func(name string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(fmt.Sprintf(`{"name":%q,"classification":"config","declaration":{"rule":{"type":"string"}}}`, name)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+artifact)
		response := httptest.NewRecorder()
		srv.publicHandler.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("create %s: status=%d, want %d: %s", name, response.Code, want, response.Body.String())
		}
	}
	apply := func(limit, maximum int) {
		t.Helper()
		candidate := ownerCandidate(t, srv, func(values map[string]string) {
			values["HIKYO_MAX_KEYS_PER_PROJECT"] = strconv.Itoa(maximum)
			values["HIKYO_DEFAULT_KEYS_PER_PROJECT"] = strconv.Itoa(limit)
		})
		prepared, err := srv.owner.Prepare(t.Context(), candidate)
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Close()
		if err := prepared.Activate(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	apply(1, 10000)
	create("FIRST", http.StatusCreated)
	create("SECOND", http.StatusConflict)
	apply(2, 10000)
	create("SECOND", http.StatusCreated)
	apply(1, 10000)
	create("THIRD", http.StatusConflict)

	callCapacity := func(method string, body *apigen.SetProjectCapacityRequest, want int) apigen.ProjectCapacity {
		t.Helper()
		var buffer bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&buffer).Encode(body); err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, strings.TrimSuffix(path, "/keys")+"/capacity", &buffer)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+artifact)
		response := httptest.NewRecorder()
		srv.publicHandler.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s capacity status=%d want=%d: %s", method, response.Code, want, response.Body.String())
		}
		var capacity apigen.ProjectCapacity
		if want == http.StatusOK {
			if err := json.NewDecoder(response.Body).Decode(&capacity); err != nil {
				t.Fatal(err)
			}
		}
		return capacity
	}
	policy := callCapacity(http.MethodGet, nil, http.StatusOK)
	if !policy.Inherited || policy.InstanceDefault != 1 || policy.InstanceMaximum != 10000 {
		t.Fatalf("unexpected initial policy: %+v", policy)
	}
	override := 3
	request := apigen.SetProjectCapacityRequest{KeyLimitOverride: &override, ExpectedRevision: policy.Revision, ExpectedInstanceDefault: policy.InstanceDefault, ExpectedInstanceMaximum: policy.InstanceMaximum}
	policy = callCapacity(http.MethodPut, &request, http.StatusOK)
	if policy.EffectiveLimit != 3 || policy.Inherited {
		t.Fatal("HTTP project override was not activated")
	}
	create("THIRD", http.StatusCreated)
	callCapacity(http.MethodPut, &request, http.StatusConflict)
	request.ExpectedRevision = policy.Revision
	request.KeyLimitOverride = nil
	policy = callCapacity(http.MethodPut, &request, http.StatusOK)
	if !policy.Inherited || policy.KeyLimitOverride != nil || policy.EffectiveLimit != 1 {
		t.Fatal("explicit null did not restore inheritance")
	}
	create("FOURTH", http.StatusConflict)
	override = 5
	request.KeyLimitOverride = &override
	request.ExpectedRevision = policy.Revision
	policy = callCapacity(http.MethodPut, &request, http.StatusOK)
	if policy.Inherited || policy.KeyLimitOverride == nil || *policy.KeyLimitOverride != 5 || policy.EffectiveLimit != 5 {
		t.Fatalf("override of 5 was not activated: %+v", policy)
	}
	apply(1, 2)
	policy = callCapacity(http.MethodGet, nil, http.StatusOK)
	if !policy.Capped || policy.KeyLimitOverride == nil || *policy.KeyLimitOverride != 5 || policy.EffectiveLimit != 2 {
		t.Fatal("instance ceiling did not cap retained project override")
	}
	create("FOURTH", http.StatusConflict)
	request.ExpectedRevision = policy.Revision
	request.ExpectedInstanceMaximum = 2
	callCapacity(http.MethodPut, &request, http.StatusBadRequest)
	request.KeyLimitOverride = nil
	callCapacity(http.MethodPut, &request, http.StatusOK)
	var count int
	if err := srv.db.SQLiteRead().QueryRowContext(t.Context(), "SELECT count(*) FROM keys WHERE project_id=?", project.ID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("lowering the limit lost existing keys: count=%d err=%v", count, err)
	}
}
