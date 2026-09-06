package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
)

func TestRuntimeDrainResponseConformsToEveryOperation(t *testing.T) {
	operations, err := api.Operations()
	if err != nil {
		t.Fatal(err)
	}
	for id, op := range operations {
		t.Run(id, func(t *testing.T) {
			path := op.Path
			for strings.Contains(path, "{") {
				start := strings.Index(path, "{")
				end := strings.Index(path[start:], "}") + start
				path = path[:start] + "test-resource" + path[end+1:]
			}
			r := httptest.NewRequest(op.Method, path, nil)
			w := httptest.NewRecorder()
			WriteRuntimeUnavailable(w, r)
			if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "2" {
				t.Fatalf("drain response: %d, retry %q", w.Code, w.Header().Get("Retry-After"))
			}
			if err := api.ValidateResponse(r, w.Code, w.Header(), w.Body.Bytes()); err != nil {
				t.Fatalf("drain response violates contract: %v", err)
			}
		})
	}
}

func TestRuntimeRecoveryDoesNotAdmitBusinessOperations(t *testing.T) {
	for _, id := range []string{"createOrg", "createProject", "fetchDelivery", "mintLease", "setAdapterCredential", "rotateRootKey", "createInstanceGrant", "setCredentialPolicy", "scimCreateUser", "unknownOperation", "applyInstanceConfigExtra"} {
		if runtimeRecoveryOperation(id) || runtimeRepairHierarchyOperation(id) {
			t.Errorf("business operation %s admitted during failed activation", id)
		}
	}
	for _, id := range []string{"applyInstanceConfig", "getInstanceConfig", "authMethods", "localLogin", "reauthPasskeyFinish", "reauthTotp"} {
		if !runtimeRecoveryOperation(id) {
			t.Errorf("recovery operation %s blocked", id)
		}
	}
	for _, id := range []string{"setValue", "publishPendingChanges", "rollbackRevision", "getProject", "listValues"} {
		if runtimeRecoveryOperation(id) || !runtimeRepairHierarchyOperation(id) {
			t.Errorf("hierarchy operation %s must require protected scope authorization", id)
		}
	}
}
