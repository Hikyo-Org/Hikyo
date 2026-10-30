package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
)

type awsCredentialReadProbe struct{ reads int }

func (p *awsCredentialReadProbe) Read([]byte) (int, error) {
	p.reads++
	return 0, io.ErrUnexpectedEOF
}

func TestAWSAdapterProtectedStaticDescriptorSources(t *testing.T) {
	for _, operation := range []string{"create", "replace"} {
		for _, source := range []string{"stdin", "file", "terminal"} {
			t.Run(operation+"/"+source, func(t *testing.T) {
				calls, prompts := 0, 0
				args := []string{"adapter", "credential", "set", "--adapter", "adp_one"}
				if operation == "create" {
					args = []string{"adapter", "create", "--provider", "aws-secrets-manager", "--origin", "https://secretsmanager.eu-west-1.amazonaws.com", "--env", "env_one", "--kind", "json-object", "--owner", "123456789012", "--secret", "prod/app", "--keys", "key_one"}
				}
				probe := &awsCredentialReadProbe{}
				var input io.Reader = probe
				switch source {
				case "stdin":
					input = strings.NewReader(awsCLIStaticDescriptor + "\n")
					args = append(args, "--stdin")
				case "file":
					path := filepath.Join(t.TempDir(), "descriptor.json")
					if err := os.WriteFile(path, []byte(awsCLIStaticDescriptor+"\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--value-file", path)
				}
				for _, arg := range args {
					for _, field := range []string{"AKIAHIKYOTEST0000001", "aws-cli-secret-canary", "aws-cli-temporary-token-canary"} {
						if strings.Contains(arg, field) {
							t.Fatal("static credential fixture crossed argv")
						}
					}
				}
				code, stdout, stderr := runAWSAdapterCommandWithInput(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					var credential string
					if operation == "create" {
						var body apigen.CreateAdapterRequest
						if r.Method != http.MethodPost || r.URL.Path != "/api/v1/orgs/org_one/projects/prj_one/adapters" || json.NewDecoder(r.Body).Decode(&body) != nil {
							t.Error("protected create lost its request contract")
							return
						}
						credential = body.Credential
					} else {
						var body apigen.SetAdapterCredentialRequest
						if r.Method != http.MethodPut || r.URL.Path != "/api/v1/orgs/org_one/projects/prj_one/adapters/adp_one/credential" || json.NewDecoder(r.Body).Decode(&body) != nil {
							t.Error("protected replacement lost its request contract")
							return
						}
						credential = body.Credential
					}
					descriptor, err := awssm.ParseDescriptor(credential)
					if err != nil || credential != awsCLIStaticDescriptor || descriptor.SessionToken != "aws-cli-temporary-token-canary" {
						t.Error("protected input lost part of the temporary static descriptor")
					}
					if operation == "create" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusCreated)
						_, _ = io.WriteString(w, `{"id":"adp_one","provider":"aws-secrets-manager","state":"active","credential_present":true,"targets":[]}`)
					} else {
						w.WriteHeader(http.StatusNoContent)
					}
				}, input, func(prompt string) (string, error) {
					prompts++
					if prompt != "Deployment provider credential: " {
						t.Error("protected descriptor prompt lost its purpose")
					}
					return awsCLIStaticDescriptor, nil
				}, args...)
				wantPrompts := 0
				if source == "terminal" {
					wantPrompts = 1
				}
				if code != ExitOK || calls != 1 || prompts != wantPrompts || probe.reads != 0 || (operation == "replace" && stdout != "") {
					t.Fatalf("exit=%d HTTP=%d prompts=%d unexpected stdin=%d stderr=%s", code, calls, prompts, probe.reads, stderr)
				}
			})
		}
	}
}

func TestAWSAdapterRejectsDeprecatedStaticFlagsBeforeIO(t *testing.T) {
	for _, command := range [][]string{
		{"adapter", "create", "--provider", "aws-secrets-manager", "--origin", "https://secretsmanager.eu-west-1.amazonaws.com", "--env", "env_one", "--kind", "json-object", "--owner", "123456789012", "--secret", "prod/app", "--keys", "key_one"},
		{"adapter", "update", "adp_one", "--origin", "https://secretsmanager.eu-west-1.amazonaws.com"},
		{"adapter", "update", "adp_one", "--move", "mov_one", "--origin", "https://secretsmanager.eu-west-1.amazonaws.com"},
		{"adapter", "credential", "set", "--adapter", "adp_one"},
		{"adapter", "credential", "set", "--adapter", "adp_one", "--move", "mov_one"},
	} {
		for _, deprecated := range [][]string{
			{"--aws-auth", "static", "--aws-access-key-id", "AKIAHIKYOTEST0000001"},
			{"--aws-auth=static", "--aws-access-key-id=AKIAHIKYOTEST0000001"},
			{"-aws-auth", "static", "-aws-access-key-id", "AKIAHIKYOTEST0000001"},
			{"--aws-access-key-id="},
			{"--aws-auth", "ambient", "--aws-access-key-id", "AKIAHIKYOTEST0000001"},
			{"--aws-auth", "static"},
		} {
			for _, source := range []string{"stdin", "file", "terminal"} {
				t.Run(strings.Join(command[:2], "/")+"/"+strings.Join(deprecated[:1], "")+"/"+source, func(t *testing.T) {
					requests, prompts := 0, 0
					probe := &awsCredentialReadProbe{}
					args := append(slices.Clone(command), deprecated...)
					if source == "stdin" {
						args = append(args, "--stdin")
					} else if source == "file" {
						args = append(args, "--value-file", filepath.Join(t.TempDir(), "must-not-open"))
					}
					code, stdout, stderr := runAWSAdapterCommandWithInput(t, func(w http.ResponseWriter, r *http.Request) {
						requests++
						http.Error(w, "unexpected HTTP request", http.StatusBadRequest)
					}, probe, func(string) (string, error) {
						prompts++
						return "aws-cli-secret-canary", nil
					}, args...)
					if code != ExitUsage || requests != 0 || prompts != 0 || probe.reads != 0 || stdout != "" {
						t.Fatalf("exit=%d HTTP=%d prompts=%d stdin=%d; deprecated flags must fail before IO", code, requests, prompts, probe.reads)
					}
					for _, want := range []string{"JSON", "--stdin", "--value-file", "--aws-*"} {
						if !strings.Contains(stderr, want) {
							t.Errorf("safe migration error missing %q", want)
						}
					}
					for _, forbidden := range []string{"AKIAHIKYOTEST0000001", "aws-cli-secret-canary", "aws-cli-session-canary"} {
						if strings.Contains(stderr, forbidden) || strings.Contains(stdout, forbidden) {
							t.Fatal("deprecated input diagnostic disclosed credential material")
						}
					}
				})
			}
		}
	}
}
