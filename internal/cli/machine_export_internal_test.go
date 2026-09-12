package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestMachineExportUsesAuthorizedDelivery(t *testing.T) {
	for _, tc := range []struct {
		name         string
		flags        []string
		projection   string
		secret       bool
		wantOK       bool
		wantRequests int
	}{
		{"config only", nil, "config-only", false, true, 1},
		{"explicit reveal", []string{"--reveal", "--dangerously-print"}, "", true, true, 1},
		{"incomplete reveal refused", []string{"--reveal", "--dangerously-print"}, "", false, false, 1},
		{"arbitrary history refused", []string{"--revision", "3"}, "", false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == api.PathPrefix+"/meta" {
					_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "fixture-current", ApiRevision: api.Revision})
					return
				}
				requests++
				if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/delivery") || r.URL.Query().Get("projection") != tc.projection || r.URL.Query().Get("cursor") != "" {
					t.Errorf("unexpected export request: %s %s", r.Method, r.URL)
				}
				if r.URL.Query().Get("parameters") != `{"PR_NUMBER":"123"}` || r.Header.Get("Authorization") != "Bearer automation-token" {
					t.Error("missing fetch parameters or machine authorization")
				}
				keys := []apigen.DeliveredKey{{Name: "APP_URL", Classification: apigen.KeyClassificationConfig, Presence: apigen.DeliveredKeyPresenceSet, Value: strPtr("https://pr-123.example.test")}}
				if tc.projection == "" {
					secret := apigen.DeliveredKey{Name: "PASSWORD", Classification: apigen.KeyClassificationSecret, Presence: apigen.DeliveredKeyPresenceSet}
					if tc.secret {
						secret.Value = strPtr("export-secret")
					}
					keys = append(keys, secret)
				}
				_ = json.NewEncoder(w).Encode(apigen.DeliveryResponse{Revision: 7, Keys: keys})
			}))
			defer srv.Close()
			_, stateDir := machineState(t, srv.URL)
			ios, stdout, stderr := composeIO(stateDir, t.TempDir(), "automation-token", nil)
			args := []string{"values", "export", "--format", "json", "--param", "PR_NUMBER=123", "--instance", "local", "--org", "org_one", "--project", "prj_one", "--env", "env_one"}
			args = append(args, tc.flags...)
			code := Run(t.Context(), ios, args)
			if (code == ExitOK) != tc.wantOK || requests != tc.wantRequests {
				t.Fatalf("exit=%d requests=%d stderr=%s", code, requests, stderr)
			}
			if !tc.wantOK {
				if stdout.Len() != 0 {
					t.Fatalf("refused export emitted partial output: %s", stdout)
				}
				return
			}
			var out apigen.ExportedValues
			if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.Revision != 7 || out.Items[0].Value == nil || *out.Items[0].Value != "https://pr-123.example.test" {
				t.Fatalf("wrong delivered export: %s", stdout)
			}
			if strings.Contains(stdout.String(), "export-secret") != tc.secret {
				t.Fatal("secret disclosure did not match explicit request")
			}
		})
	}
}

func TestMachineExportRefusesOldServerAndUnexpectedConditionalResponse(t *testing.T) {
	for _, revision := range []int{2, api.Revision} {
		t.Run(string(rune('0'+revision)), func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == api.PathPrefix+"/meta" {
					_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "fixture", ApiRevision: revision})
					return
				}
				requests++
				_ = json.NewEncoder(w).Encode(apigen.DeliveryResponse{Current: true})
			}))
			defer srv.Close()
			client, err := NewClient(TrustEntry{Origin: srv.URL}, "machine-test")
			if err != nil {
				t.Fatal(err)
			}
			out, err := machineExport(t.Context(), client, api.PathPrefix+"/orgs/org_test/projects/prj_test/environments/env_test", false, 0, nil)
			if err == nil || len(out.Items) != 0 {
				t.Fatalf("out=%v err=%v", out, err)
			}
			if revision < 3 && (requests != 0 || !strings.Contains(err.Error(), "needs revision 3")) {
				t.Fatalf("requests=%d err=%v", requests, err)
			}
			if revision >= 3 && (requests != 1 || !strings.Contains(err.Error(), "unconditional machine export")) {
				t.Fatalf("requests=%d err=%v", requests, err)
			}
		})
	}
}
