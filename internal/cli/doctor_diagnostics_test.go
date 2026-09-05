package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestDoctorDiagnosticVerdictsAndExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		diagnostics *[]apigen.OpsDiagnosticFinding
		status      string
		exit        int
		code        string
		severity    string
	}{
		{"old server", nil, "warning", ExitOK, "diagnostics", "unknown"},
		{"empty diagnostics", &[]apigen.OpsDiagnosticFinding{}, "warning", ExitOK, "diagnostics", "unknown"},
		{"healthy", &[]apigen.OpsDiagnosticFinding{{Code: "database-durability", Severity: apigen.OpsDiagnosticFindingSeverityOk, Message: "validated durability"}}, "ok", ExitOK, "database-durability", "ok"},
		{"unmeasured volume", &[]apigen.OpsDiagnosticFinding{{Code: "data-volume", Severity: apigen.OpsDiagnosticFindingSeverityUnknown, Message: "remote database capacity is not measured"}}, "warning", ExitOK, "data-volume", "unknown"},
		{"unverified escrow", &[]apigen.OpsDiagnosticFinding{{Code: "root-escrow", Severity: apigen.OpsDiagnosticFindingSeverityWarn, Message: "verify the root escrow"}}, "warning", ExitOK, "root-escrow", "warn"},
		{"error dominates unknown", &[]apigen.OpsDiagnosticFinding{
			{Code: "database-durability", Severity: apigen.OpsDiagnosticFindingSeverityError, Message: "durability check failed"},
			{Code: "data-volume", Severity: apigen.OpsDiagnosticFindingSeverityUnknown, Message: "remote database capacity is not measured"},
		}, "error", ExitRefused, "database-durability", "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			health := apigen.RetentionHealth{LastPruneSuccess: &now, Backup: healthyBackup(now), Diagnostics: tc.diagnostics}
			requests := 0
			server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Header.Get("Authorization") != "Bearer doctor-test-token" {
					t.Error("doctor did not authenticate its health read")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				switch r.URL.Path {
				case api.PathPrefix + "/instance/retention-health":
					_ = json.NewEncoder(w).Encode(health)
				case api.PathPrefix + "/instance/saml-providers":
					_ = json.NewEncoder(w).Encode(apigen.SamlProviderList{Providers: []apigen.SamlProvider{}})
				default:
					t.Errorf("unexpected doctor request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			stateDir := t.TempDir()
			st := &State{dir: stateDir}
			if err := st.Trust().Put(TrustEntry{Name: "local", Origin: server.URL}); err != nil {
				t.Fatal(err)
			}
			if err := st.PutSession(SessionArtifact{Instance: "local", Origin: server.URL, Token: "doctor-test-token",
				SessionID: "ses_doctor", Principal: "usr_doctor", ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)}); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			ios := IO{Stdout: &stdout, Stderr: &stderr, Workdir: t.TempDir(), Env: Env{Getenv: func(key string) string {
				switch key {
				case "HIKYO_STATE_DIR":
					return stateDir
				default:
					return ""
				}
			}}}
			if code := Run(t.Context(), ios, []string{"doctor", "--instance", "local", "-o", "json"}); code != tc.exit {
				t.Fatalf("exit=%d want=%d stderr=%s", code, tc.exit, stderr.String())
			}
			var result doctorResult
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Status != tc.status || requests != 2 {
				t.Fatalf("status=%s want=%s requests=%d", result.Status, tc.status, requests)
			}
			found := false
			for _, finding := range result.Findings {
				if finding.Code == tc.code {
					found = true
					if finding.Severity != tc.severity || finding.Message == "" {
						t.Fatalf("diagnostic lost verdict/message: %+v", finding)
					}
				}
			}
			if !found {
				t.Fatalf("missing diagnostic %q: %+v", tc.code, result)
			}
			_, rows := doctorResults(apigen.SamlProviderList{}, health, now)
			found = false
			for _, row := range rows {
				if row[2] == tc.code && row[0] == tc.severity && strings.TrimSpace(row[4]) != "" {
					found = true
				}
			}
			if !found {
				t.Fatalf("table lost diagnostic %s: %v", tc.code, rows)
			}
		})
	}
}

func TestDoctorPreservesAllOperationalDiagnosticCodes(t *testing.T) {
	codes := []string{"data-volume", "root-escrow", "pin-expiry", "root-rotation", "reencrypt", "database-durability", "argon2-floor"}
	diagnostics := make([]apigen.OpsDiagnosticFinding, 0, len(codes))
	for _, code := range codes {
		diagnostics = append(diagnostics, apigen.OpsDiagnosticFinding{Code: code, Severity: apigen.OpsDiagnosticFindingSeverityUnknown, Message: "server measurement: " + code})
	}
	result, _ := doctorResults(apigen.SamlProviderList{}, apigen.RetentionHealth{Diagnostics: &diagnostics}, time.Now())
	if len(result.Findings) != 12 || result.Status != "warning" {
		t.Fatalf("doctor lost existing or new families: %+v", result)
	}
	for i, code := range codes {
		if got := result.Findings[i+5]; got.Code != code || got.Severity != "unknown" || got.Message != diagnostics[i].Message {
			t.Fatalf("diagnostic was recalculated or discarded: %+v", got)
		}
	}
}
