package isolation

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
)

func TestAWSRealCreationIdentityVisibility(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		stale     int32
		mode      string
		wantReads int32
		wantError bool
	}{
		{"immediate", 0, "", 1, false},
		{"missing owner then visible", 2, "", 3, false},
		{"missing secret then visible", 2, "notfound", 3, false},
		{"persistent missing owner", 9, "", 3, true},
		{"persistent missing secret", 9, "notfound", 3, true},
		{"foreign owner", 0, "foreign owner", 1, true},
		{"empty present owner", 0, "empty owner", 1, true},
		{"foreign metadata ARN", 0, "foreign ARN", 1, true},
		{"foreign metadata name", 0, "foreign name", 1, true},
		{"deleted metadata", 0, "deleted", 1, true},
		{"access denied", 9, "denied", 1, true},
		{"cancelled wait", 9, "cancelled", 1, true},
		{"wrong creation account", 0, "creation account", 0, true},
		{"wrong creation region", 0, "creation region", 0, true},
		{"wrong creation name", 0, "creation name", 0, true},
		{"wrong creation ARN name", 0, "creation ARN name", 0, true},
		{"wrong creation ARN suffix", 0, "creation suffix", 0, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			m := realAWSManifestFixture(time.Now())
			name := m.Prefix + "web-identity/postgres/role-document"
			arn := "arn:aws:secretsmanager:" + m.Region + ":" + m.Account + ":secret:" + name + "-AbCd12"
			s := &realAWSSandbox{manifest: m, owned: map[string]string{}}
			var creates, reads, preReads atomic.Int32
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-amz-json-1.1")
				if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
					t.Error("missing signature")
					w.WriteHeader(400)
					return
				}
				switch r.Header.Get("X-Amz-Target") {
				case "secretsmanager.CreateSecret":
					creates.Add(1)
					var request struct {
						Name, ClientRequestToken string
						Tags                     []struct{ Key, Value string }
						SecretString             *string
					}
					if json.NewDecoder(r.Body).Decode(&request) != nil || request.Name != name || request.ClientRequestToken != strings.Repeat("a", 64) || request.SecretString != nil {
						t.Error("create request changed metadata-only identity")
						w.WriteHeader(400)
						return
					}
					out := awssm.CreatedSecret{ARN: arn, Name: name}
					switch scenario.mode {
					case "creation account":
						out.ARN = strings.Replace(arn, m.Account, "111111111111", 1)
					case "creation region":
						out.ARN = strings.Replace(arn, m.Region, "us-east-1", 1)
					case "creation name":
						out.Name += "foreign"
					case "creation ARN name":
						out.ARN = strings.Replace(arn, name, name+"foreign", 1)
					case "creation suffix":
						out.ARN += "foreign"
					}
					json.NewEncoder(w).Encode(out)
				case "secretsmanager.DescribeSecret":
					var request struct {
						SecretID string `json:"SecretId"`
					}
					if json.NewDecoder(r.Body).Decode(&request) != nil {
						t.Error("invalid metadata request")
						w.WriteHeader(400)
						return
					}
					if creates.Load() == 0 {
						preReads.Add(1)
						if request.SecretID != name {
							t.Error("pre-create check changed name")
						}
						w.WriteHeader(400)
						json.NewEncoder(w).Encode(map[string]string{"__type": "ResourceNotFoundException"})
						return
					}
					n := reads.Add(1)
					s.mu.Lock()
					recorded := s.owned[arn] == name
					s.mu.Unlock()
					if request.SecretID != arn || !recorded {
						t.Error("post-create observation preceded exact ARN custody")
						w.WriteHeader(400)
						return
					}
					if scenario.mode == "cancelled" {
						cancel()
					}
					if (n <= scenario.stale && scenario.mode == "notfound") || scenario.mode == "denied" {
						code := "ResourceNotFoundException"
						if scenario.mode == "denied" {
							code = "AccessDeniedException"
						}
						w.WriteHeader(400)
						json.NewEncoder(w).Encode(map[string]string{"__type": code})
						return
					}
					out := map[string]any{"ARN": arn, "Name": name}
					if n > scenario.stale {
						out["Tags"] = []map[string]string{{"Key": adapter.SentinelName, "Value": "offline-target"}}
					}
					switch scenario.mode {
					case "foreign owner":
						out["Tags"] = []map[string]string{{"Key": adapter.SentinelName, "Value": "foreign"}}
					case "empty owner":
						out["Tags"] = []map[string]string{{"Key": adapter.SentinelName, "Value": ""}}
					case "foreign ARN":
						out["ARN"] = arn + "foreign"
					case "foreign name":
						out["Name"] = name + "foreign"
					case "deleted":
						out["DeletedDate"] = 1
					}
					json.NewEncoder(w).Encode(out)
				default:
					t.Error("creation observer invoked another operation")
					w.WriteHeader(400)
				}
			}))
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			raw, _ := json.Marshal(awssm.Descriptor{Mode: awssm.AuthStatic, Region: m.Region, AccessKeyID: "AKIAHIKYOTEST0000001", SecretKey: "synthetic-create-observer"})
			client, err := awssm.NewClient(awssm.ClientConfig{Origin: server.URL, Credential: string(raw), Deadline: time.Second, RootCAs: roots, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Forget()
			writer := &realAWSWriter{API: client, t: t, s: s}
			err = writer.CreateSecret(ctx, awssm.CreateSecretInput{Name: name, ClientRequestToken: strings.Repeat("a", 64), Tags: map[string]string{adapter.SentinelName: "offline-target"}})
			if (err != nil) != scenario.wantError {
				t.Fatalf("creation error=%t, want %t", err != nil, scenario.wantError)
			}
			if creates.Load() != 1 || preReads.Load() != 1 || reads.Load() != scenario.wantReads || len(s.creations) != 1 {
				t.Fatalf("creates=%d prechecks=%d observations=%d reservations=%d", creates.Load(), preReads.Load(), reads.Load(), len(s.creations))
			}
			if strings.HasPrefix(scenario.mode, "creation ") {
				if len(s.owned) != 0 {
					t.Fatal("invalid creation identity entered cleanup custody")
				}
			} else if s.owned[arn] != name || s.current[name] != arn {
				t.Fatal("verified create ARN lost cleanup custody after observation")
			}
		})
	}
}
