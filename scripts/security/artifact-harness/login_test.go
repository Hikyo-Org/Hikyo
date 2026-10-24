package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	vw "github.com/descope/virtualwebauthn"
)

func TestEnrolledFixtureLoginUsesBrowserMFAAndPKCE(t *testing.T) {
	for _, failure := range []string{"", "factor", "binding", "artifact", "principal", "token", "logout"} {
		t.Run("refusal-"+failure, func(t *testing.T) {
			var paths []string
			var state, challenge, redirect string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.Header.Get("Authorization") != "" {
					t.Error("browser ceremony reused a bearer")
				}
				var body map[string]string
				if r.URL.Path != "/api/v1/auth/login/challenge/fixture/webauthn/finish" && r.Body != nil {
					_ = json.NewDecoder(r.Body).Decode(&body)
				}
				reply := func(value any) {
					if err := json.NewEncoder(w).Encode(value); err != nil {
						t.Error(err)
					}
				}
				switch r.URL.Path {
				case "/api/v1/auth/local/login":
					if body["artifact"] != "browser" {
						http.Error(w, "enrolled local CLI refused", 400)
						return
					}
					w.WriteHeader(202)
					reply(map[string]any{"challenge_id": "fixture", "factors": []string{"webauthn"}})
				case "/api/v1/auth/login/challenge/fixture/webauthn/start":
					reply(json.RawMessage(`{"publicKey":{"challenge":"Y2FtcGFpZ24tYXNzZXJ0aW9u","rpId":"localhost"}}`))
				case "/api/v1/auth/login/challenge/fixture/webauthn/finish":
					if failure == "factor" {
						w.WriteHeader(401)
						return
					}
					http.SetCookie(w, &http.Cookie{Name: "__Host-hikyo", Value: "browser-cookie", Secure: true, Path: "/"})
					http.SetCookie(w, &http.Cookie{Name: "__Host-hikyo-csrf", Value: "csrf", Secure: true, Path: "/"})
					reply(map[string]any{"principal": map[string]string{"id": "principal"}, "session": map[string]string{"id": "browser", "artifact": "browser"}})
				case "/api/v1/auth/cli-login/start":
					state, challenge, redirect = body["state"], body["pkce_challenge"], body["redirect_uri"]
					if state == "" || challenge == "" || body["transport"] != "loopback" {
						t.Error("missing CLI binding")
					}
					w.WriteHeader(201)
					reply(map[string]string{"state": state})
				case "/api/v1/auth/cli-login/approve":
					if r.Header.Get("X-Hikyo-CSRF") != "csrf" || body["reference"] != state {
						t.Error("missing browser CSRF or reference binding")
					}
					if cookie, err := r.Cookie("__Host-hikyo"); err != nil || cookie.Value != "browser-cookie" {
						t.Error("missing browser cookie")
					}
					approvedState := state
					if failure == "binding" {
						approvedState = "wrong"
					}
					reply(map[string]string{"state": approvedState, "redirect_uri": redirect, "code": "single-use"})
				case "/api/v1/auth/cli-login/redeem":
					digest := sha256.Sum256([]byte(body["pkce_verifier"]))
					if base64.RawURLEncoding.EncodeToString(digest[:]) != challenge || body["redirect_uri"] != redirect || body["code"] != "single-use" {
						t.Error("PKCE redemption binding mismatch")
					}
					artifact := "cli"
					if failure == "artifact" {
						artifact = "browser"
					}
					principal := "principal"
					if failure == "principal" {
						principal = "different-principal"
					}
					var token any = "cli-token"
					if failure == "token" {
						token = 42
					}
					reply(map[string]any{"session_token": token, "principal": map[string]string{"id": principal}, "session": map[string]string{"id": "cli", "artifact": artifact}})
				case "/api/v1/auth/logout":
					if r.Header.Get("X-Hikyo-CSRF") != "csrf" {
						t.Error("browser logout lacks CSRF")
					}
					if failure == "logout" {
						w.WriteHeader(500)
						return
					}
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected fixture request %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			f := fixture{admin: "stale-bearer", password: "fixture password", device: &passkey{
				rp:         vw.RelyingParty{Name: "hikyo", ID: "localhost", Origin: "https://localhost:8080"},
				credential: vw.NewCredential(vw.KeyTypeEC2),
			}}
			target := &target{ctx: t.Context(), base: server.URL, client: server.Client()}
			err := f.login(target)
			if (err != nil) != (failure != "") {
				t.Fatalf("failure %q: %v", failure, err)
			}
			if failure == "" && (f.admin != "cli-token" || f.adminPrincipal != "principal") {
				t.Fatal("CLI result not retained")
			}
			if failure != "factor" && paths[len(paths)-1] != "/api/v1/auth/logout" {
				t.Fatal("browser session was not logged out")
			}
			if failure == "binding" && slices.Contains(paths, "/api/v1/auth/cli-login/redeem") {
				t.Fatal("invalid approval redeemed")
			}
		})
	}
}
