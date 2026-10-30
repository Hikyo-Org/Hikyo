package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
)

const awsCLIStaticDescriptor = `{"mode":"static","region":"eu-west-1","access_key_id":"AKIAHIKYOTEST0000001","secret_access_key":"aws-cli-secret-canary","session_token":"aws-cli-temporary-token-canary"}`

// This fixture checks CLI dispatch and wire contracts only. It deliberately
// omits OpenURL, using the existing injectable transport seam; ceremony and
// provider behavior require their separate integration acceptance suites.
func runAWSAdapterCommand(t *testing.T, handler http.HandlerFunc, args ...string) (int, string, string) {
	t.Helper()
	return runAWSAdapterCommandWithInput(t, handler, strings.NewReader(awsCLIStaticDescriptor+"\n"), func(string) (string, error) {
		t.Error("AWS descriptor unexpectedly requested a terminal secret")
		return "", io.ErrUnexpectedEOF
	}, args...)
}

func runAWSAdapterCommandWithInput(t *testing.T, handler http.HandlerFunc, input io.Reader, password func(string) (string, error), args ...string) (int, string, string) {
	t.Helper()
	server := newRevisionAwareFixtureServer(handler)
	defer server.Close()
	stateDir := t.TempDir()
	state := &State{dir: stateDir}
	if err := state.Trust().Put(pinnedTestEntry("local", server)); err != nil {
		t.Fatal(err)
	}
	if err := state.PutSession(SessionArtifact{Instance: "local", Origin: server.URL, Token: "aws-cli-session-canary", SessionID: "ses_one", Principal: "usr_one", ExpiresAt: "2030-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	ios := IO{
		Stdin: input, Stdout: &stdout, Stderr: &stderr,
		Env: Env{Getenv: func(name string) string {
			if name == "HIKYO_STATE_DIR" {
				return stateDir
			}
			return ""
		}},
		ReadPassword: password,
	}
	args = append(args, "--instance", "local", "--org", "org_one", "--project", "prj_one")
	code := Run(t.Context(), ios, args)
	for _, forbidden := range []string{"aws-cli-secret-canary", "aws-cli-session-canary", "aws-cli-temporary-token-canary", "AKIAHIKYOTEST0000001", "secret_access_key", "session_token"} {
		if strings.Contains(stdout.String(), forbidden) || strings.Contains(stderr.String(), forbidden) {
			t.Error("CLI output disclosed credential material")
		}
	}
	return code, stdout.String(), stderr.String()
}

func TestAWSAdapterCreateDispatchesBothKindsAndAllCredentialModes(t *testing.T) {
	for _, kind := range []string{"json-object", "per-key"} {
		for _, mode := range []string{"static", "ambient", "assume-role", "web-identity"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				calls := 0
				destination := "prod/config"
				if kind == "per-key" {
					destination = "prod/keys/"
				}
				handler := func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != http.MethodPost || r.URL.Path != "/api/v1/orgs/org_one/projects/prj_one/adapters" {
						t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
						http.NotFound(w, r)
						return
					}
					var body apigen.CreateAdapterRequest
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						return
					}
					target := body.Target
					if body.Provider != "aws-secrets-manager" || body.Origin != "https://secretsmanager.eu-west-1.amazonaws.com" || string(target.DestinationKind) != kind || target.DestinationOwner != "123456789012" || target.DestinationName != destination || target.DestinationEnvironment != "alias/hikyo" || target.NamePrefix != "PROD_" || target.EnvironmentId != "env_one" || len(target.KeyIds) != 1 || target.KeyIds[0] != "key_one" {
						t.Error("AWS create lost provider, destination, KMS, prefix or key selection")
					}
					descriptor, err := awssm.ParseDescriptor(body.Credential)
					if err != nil || string(descriptor.Mode) != mode || descriptor.Region != "eu-west-1" {
						t.Error("AWS create lost a valid mode-specific descriptor")
					}
					if mode == "static" && (descriptor.AccessKeyID != "AKIAHIKYOTEST0000001" || descriptor.SecretKey != "aws-cli-secret-canary" || descriptor.SessionToken != "aws-cli-temporary-token-canary") {
						t.Error("static descriptor did not preserve the complete private temporary credentials")
					}
					if mode == "assume-role" && descriptor.ExternalID != "tenant-one" {
						t.Error("AssumeRole external ID lost")
					}
					if (mode == "assume-role" || mode == "web-identity") && (descriptor.RoleARN != "arn:aws:iam::123456789012:role/hikyo" || descriptor.SessionSeconds != 900) {
						t.Error("role or session duration lost")
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, `{"id":"adp_one","provider":"aws-secrets-manager","origin":"https://secretsmanager.eu-west-1.amazonaws.com","state":"active","credential_present":true,"targets":[]}`)
				}
				args := []string{"adapter", "create", "--provider", "aws-secrets-manager", "--origin", "https://secretsmanager.eu-west-1.amazonaws.com", "--env", "env_one", "--kind", kind, "--owner", "123456789012", "--secret", destination, "--kms-key", "alias/hikyo", "--prefix", "PROD_", "--keys", "key_one"}
				if mode == "static" {
					args = append(args, "--stdin")
				} else {
					args = append(args, "--aws-auth", mode, "--aws-region", "eu-west-1")
				}
				if mode == "assume-role" || mode == "web-identity" {
					args = append(args, "--aws-role-arn", "arn:aws:iam::123456789012:role/hikyo", "--aws-session-seconds", "900")
				}
				if mode == "assume-role" {
					args = append(args, "--aws-external-id", "tenant-one")
				}
				code, _, stderr := runAWSAdapterCommand(t, handler, args...)
				if code != ExitOK || calls != 1 {
					t.Fatalf("exit=%d create requests=%d stderr=%s", code, calls, stderr)
				}
			})
		}
	}
}

func TestAWSAdapterCredentialReplacementHasNoReadback(t *testing.T) {
	calls := 0
	code, stdout, stderr := runAWSAdapterCommand(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/orgs/org_one/projects/prj_one/adapters/adp_one/credential" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body apigen.SetAdapterCredentialRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		descriptor, err := awssm.ParseDescriptor(body.Credential)
		if err != nil || descriptor.Mode != awssm.AuthStatic || descriptor.SecretKey != "aws-cli-secret-canary" || descriptor.AccessKeyID != "AKIAHIKYOTEST0000001" || descriptor.SessionToken != "aws-cli-temporary-token-canary" || descriptor.Region != "eu-west-1" {
			t.Error("replacement did not carry a valid static descriptor")
		}
		w.WriteHeader(http.StatusNoContent)
	}, "adapter", "credential", "set", "--adapter", "adp_one", "--stdin")
	if code != ExitOK || calls != 1 || stdout != "" {
		t.Fatalf("exit=%d requests=%d stdout=%q stderr=%s", code, calls, stdout, stderr)
	}
}

func TestAWSAdapterTargetWorkflowUsesNamesOnlyResponse(t *testing.T) {
	const snippet = "# AWS Secrets Manager: one secret per key; the value is the SecretString.\nsecrets:\n  TOKEN: \"prod/PROD_TOKEN\"\n"
	calls := 0
	code, stdout, stderr := runAWSAdapterCommand(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/orgs/org_one/projects/prj_one/adapter-targets/tgt_one" || r.URL.Query().Get("format") != "workflow" {
			t.Errorf("unexpected workflow request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, snippet)
	}, "adapter", "target", "show", "tgt_one", "--format", "workflow")
	if code != ExitOK || calls != 1 || stdout != snippet {
		t.Fatalf("exit=%d requests=%d workflow=%q stderr=%s", code, calls, stdout, stderr)
	}
}

func TestAWSAdapterTargetAddAndKMSMoveDispatch(t *testing.T) {
	for _, kind := range []string{"json-object", "per-key"} {
		for _, update := range []bool{false, true} {
			verb := "add"
			if update {
				verb = "move"
			}
			t.Run(kind+"/"+verb, func(t *testing.T) {
				mutations := 0
				handler := func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch {
					case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/adapters/adp_one"):
						_, _ = io.WriteString(w, `{"id":"adp_one","provider":"aws-secrets-manager","origin":"https://secretsmanager.eu-west-1.amazonaws.com","state":"active","credential_present":true,"targets":[]}`)
					case update && r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/adapter-targets/tgt_one"):
						_ = json.NewEncoder(w).Encode(apigen.AdapterTargetDetail{Target: apigen.AdapterTarget{Id: "tgt_one", AdapterId: "adp_one", EnvironmentId: "env_one", DestinationKind: apigen.AdapterDestinationKind(kind), DestinationOwner: "123456789012", DestinationName: "prod/config/", DestinationEnvironment: "alias/old", NamePrefix: "PROD_", Generation: 7}, Mapping: []apigen.AdapterMappingEntry{{KeyId: "key_one"}}})
					case !update && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/adapters/adp_one/targets"):
						mutations++
						var body apigen.AdapterTargetInput
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
							return
						}
						if string(body.DestinationKind) != kind || body.DestinationName != "prod/config/" || body.DestinationEnvironment != "alias/next" || body.DestinationOwner != "123456789012" {
							t.Error("AWS target add lost kind, name, KMS or account")
						}
						w.WriteHeader(http.StatusCreated)
						_ = json.NewEncoder(w).Encode(apigen.AdapterTarget{Id: "tgt_one", DestinationKind: body.DestinationKind})
					case update && r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/adapter-targets/tgt_one"):
						mutations++
						var body apigen.UpdateAdapterTargetRequest
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
							return
						}
						if string(body.DestinationKind) != kind || body.DestinationName != "prod/config/" || body.DestinationEnvironment != "alias/next" || body.ExpectedGeneration != 7 || body.KeepRemote == nil || !*body.KeepRemote {
							t.Error("AWS KMS move lost route, generation or retain choice")
						}
						w.WriteHeader(http.StatusAccepted)
						_ = json.NewEncoder(w).Encode(apigen.AdapterMove{
							Id: "mov_one", AdapterId: "adp_one", Kind: "target", State: "activating", KeepRemote: true,
							Targets: []apigen.AdapterMoveTarget{{
								TargetId: "tgt_one", EnvironmentId: body.EnvironmentId,
								DestinationKind: body.DestinationKind, DestinationOwner: body.DestinationOwner,
								DestinationName: body.DestinationName, DestinationEnvironment: body.DestinationEnvironment,
								NamePrefix: body.NamePrefix,
							}},
						})
					default:
						t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
						http.NotFound(w, r)
					}
				}
				args := []string{"adapter", "target", "add", "--adapter", "adp_one"}
				if update {
					args = []string{"adapter", "update", "adp_one", "--target", "tgt_one", "--keep-remote"}
				}
				args = append(args, "--env", "env_one", "--kind", kind, "--owner", "123456789012", "--secret", "prod/config/", "--kms-key", "alias/next", "--prefix", "PROD_", "--keys", "key_one")
				code, stdout, stderr := runAWSAdapterCommand(t, handler, args...)
				if code != ExitOK || mutations != 1 {
					t.Fatalf("exit=%d mutations=%d stderr=%s", code, mutations, stderr)
				}
				if update {
					for _, want := range []string{"mov_one", "activating", "tgt_one", "123456789012/prod/config/"} {
						if !strings.Contains(stdout, want) {
							t.Fatalf("asynchronous KMS route move output missing %q", want)
						}
					}
				}
			})
		}
	}
}

func TestAWSAdapterCredentialServerRefusalDoesNotClaimReplacement(t *testing.T) {
	for _, refusal := range []struct {
		name, code, detail string
		status             int
	}{
		{"workload disabled", "bad_request", "server workload identity is disabled", http.StatusBadRequest},
		{"authority denied", "forbidden", "", http.StatusForbidden},
		{"move pending", "conflict", "destination move requires attention", http.StatusConflict},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			calls := 0
			code, stdout, stderr := runAWSAdapterCommand(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPut || !strings.HasSuffix(r.URL.Path, "/adapters/adp_one/credential") {
					t.Errorf("unexpected refusal request %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(refusal.status)
				_ = json.NewEncoder(w).Encode(map[string]map[string]string{"error": {"code": refusal.code, "message": "request refused", "detail": refusal.detail}})
			}, "adapter", "credential", "set", "--adapter", "adp_one", "--aws-auth", "ambient")
			if code != ExitRefused || calls != 1 || stdout != "" || !strings.Contains(stderr, "request refused") {
				t.Fatalf("exit=%d requests=%d stdout=%q stderr=%s", code, calls, stdout, stderr)
			}
			if refusal.detail != "" && !strings.Contains(stderr, refusal.detail) {
				t.Fatal("safe refusal detail lost")
			}
		})
	}
}

func TestAWSAdapterPlanNamesAndRevokeDispatch(t *testing.T) {
	for _, operation := range []string{"plan", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			calls := 0
			args := []string{"adapter", "plan", "--target", "tgt_one"}
			if operation == "revoke" {
				args = []string{"adapter", "credential", "revoke", "--adapter", "adp_one"}
			}
			code, stdout, stderr := runAWSAdapterCommand(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if operation == "revoke" {
					if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/orgs/org_one/projects/prj_one/adapters/adp_one/credential" {
						t.Errorf("unexpected revoke request %s %s", r.Method, r.URL.Path)
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/orgs/org_one/projects/prj_one/adapter-targets/tgt_one/plan" {
					t.Errorf("unexpected plan request %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(apigen.AdapterPlan{ArtifactId: "acf_one", Changes: []apigen.AdapterChange{
					{Surface: "secret", EffectiveName: "prod/config", Disposition: "conflict"},
					{Surface: "secret", EffectiveName: "prod/PROD_TOKEN", Disposition: "create"},
				}})
			}, args...)
			if code != ExitOK || calls != 1 {
				t.Fatalf("exit=%d requests=%d stderr=%s", code, calls, stderr)
			}
			if operation == "revoke" && stdout != "" {
				t.Fatalf("revoke produced unexpected output %q", stdout)
			}
			if operation == "plan" && (!strings.Contains(stdout, "prod/config") || !strings.Contains(stdout, "prod/PROD_TOKEN") || !strings.Contains(stdout, "conflict")) {
				t.Fatalf("names-only plan lost AWS names or conflict disposition: %s", stdout)
			}
		})
	}
}

// This is exact-artifact dispatch, not AWS consent validity: witness metadata
// remains server-owned and is deliberately absent from the public CLI request.
func TestAWSAdapterAdoptSelectsExactArtifactAndRejectsAmbiguity(t *testing.T) {
	for _, scenario := range []struct {
		name, artifact, selectedName string
		wantPost                     bool
	}{
		{"json-object", "acf_one", "prod/config", true},
		{"per-key", "acf_one", "prod/PROD_TOKEN", true},
		{"ambiguous", "", "prod/config", false},
		{"wrong exact name", "acf_one", "prod/CONFIG", false},
		{"wrong artifact", "acf_absent", "prod/config", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			name := "prod/config"
			if scenario.name == "per-key" {
				name = "prod/PROD_TOKEN"
			}
			entries := []apigen.AdapterConflictEntry{{Surface: "secret", EffectiveName: name}}
			artifacts := []apigen.AdapterConflictArtifact{
				{Id: "acf_one", TargetGeneration: 7, DestinationId: 123456789012, Entries: entries},
				{Id: "acf_two", TargetGeneration: 8, DestinationId: 999999999999, Entries: entries},
			}
			reads, mutations := 0, 0
			args := []string{"adapter", "adopt", "--target", "tgt_one"}
			if scenario.artifact != "" {
				args = append(args, "--artifact", scenario.artifact)
			}
			args = append(args, scenario.selectedName)
			code, stdout, stderr := runAWSAdapterCommand(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/org_one/projects/prj_one/adapter-targets/tgt_one":
					reads++
					_ = json.NewEncoder(w).Encode(apigen.AdapterTargetDetail{Target: apigen.AdapterTarget{Id: "tgt_one", AdapterId: "adp_one"}, Conflicts: artifacts})
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/org_one/projects/prj_one/adapter-targets/tgt_one/adoptions":
					mutations++
					var body apigen.AdapterAdoptionRequest
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						return
					}
					if body.ArtifactId != "acf_one" || body.TargetGeneration != 7 || body.DestinationId != 123456789012 || body.RepositoryId != 0 || len(body.Entries) != 1 || body.Entries[0] != entries[0] {
						t.Error("adoption lost the exact artifact, generation, AWS account or selected name")
					}
					w.WriteHeader(http.StatusAccepted)
					_ = json.NewEncoder(w).Encode(apigen.AdapterJob{JobId: "job_adopt", Generation: 9})
				default:
					t.Errorf("unexpected adoption request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}, args...)
			if reads != 1 {
				t.Fatalf("target reads=%d stderr=%s", reads, stderr)
			}
			if scenario.wantPost {
				if code != ExitOK || mutations != 1 || !strings.Contains(stdout, "job_adopt") {
					t.Fatalf("exit=%d mutations=%d stdout=%q stderr=%s", code, mutations, stdout, stderr)
				}
			} else if code != ExitRefused || mutations != 0 || stdout != "" || !strings.Contains(stderr, "eligible exact-match artifact") {
				t.Fatalf("exit=%d mutations=%d stdout=%q stderr=%s", code, mutations, stdout, stderr)
			}
		})
	}
}
