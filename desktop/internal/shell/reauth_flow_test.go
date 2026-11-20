package shell

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

const (
	testEnv1 = "env_0198aaaa-0000-7000-8000-000000000001"
	testEnv2 = "env_0198aaaa-0000-7000-8000-000000000002"
	testKey1 = "key_0198cccc-0000-7000-8000-000000000001"
)

// fakeReauth makes the fake instance answer the cli-reauth handoff and plays
// the browser: it follows the redirect_uri the shell registered. It returns
// the start requests the instance received.
func (h *harness) fakeReauth(sessionID string) func() []apigen.CLIReauthStartRequest {
	h.t.Helper()
	var redirect string
	h.inst.on("POST /api/v1/auth/cli-reauth/start", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in apigen.CLIReauthStartRequest
		if err := json.Unmarshal(body, &in); err != nil {
			h.t.Errorf("start body: %v", err)
		}
		h.inst.mu.Lock()
		redirect = in.RedirectUri
		h.inst.mu.Unlock()
		writeJSON(w, http.StatusOK, apigen.CLIReauthStart{State: "hik_1_hs_opaque", ExpiresAt: h.m.now().Add(5 * time.Minute)})
	})
	h.inst.on("POST /api/v1/auth/cli-reauth/redeem", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, apigen.CLIReauthRedeemed{SessionToken: newToken(h.t), SessionId: sessionID, Windows: []apigen.ReauthResult{}})
	})
	h.browser = func(target string) {
		u, err := url.Parse(target)
		if err != nil || u.Path != "/reauth/cli" || u.Query().Get("transaction") != "hik_1_hs_opaque" {
			h.t.Errorf("handoff target %q", target)
			return
		}
		h.inst.mu.Lock()
		to := redirect
		h.inst.mu.Unlock()
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, to+"?"+url.Values{"code": {"c"}, "state": {"hik_1_hs_opaque"}}.Encode(), nil)
		if err != nil {
			return
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}
	return func() []apigen.CLIReauthStartRequest {
		var out []apigen.CLIReauthStartRequest
		for _, s := range h.inst.requests("POST", "/api/v1/auth/cli-reauth/start") {
			var in apigen.CLIReauthStartRequest
			if err := json.Unmarshal([]byte(s.Body), &in); err != nil {
				h.t.Fatal(err)
			}
			out = append(out, in)
		}
		return out
	}
}

// Every purpose class must get through to the instance's handoff: the
// self-config ceremony carries no environment, the key-bound ones exactly one
// environment and their keys, adapter routing a set of environments.
func TestReauthSucceedsForEveryPurposeClass(t *testing.T) {
	selfConfig := `{"action":"apply","confirm_restored_credentials":false,"expected_generation":3,"owner_instance_id":"own","preview_token":"p","revision":7,"schema_version":1,"to":"v2"}`
	for name, tc := range map[string]struct {
		body    string
		purpose apigen.CLIReauthStartRequestPurpose
		envs    int
		keys    int
	}{
		"self-config":                  {`{"purpose":"self-config","operation":"self-config.apply","environment_ids":[],"self_config":` + selfConfig + `}`, "self-config", 0, 0},
		"self-config without the list": {`{"purpose":"self-config","operation":"self-config.apply","self_config":` + selfConfig + `}`, "self-config", 0, 0},
		"reveal":                       {`{"purpose":"reveal","operation":"value.reveal","environment_ids":["` + testEnv1 + `"],"key_ids":["` + testKey1 + `"]}`, "reveal", 1, 1},
		"copy":                         {`{"purpose":"copy","operation":"value.copy-source","environment_ids":["` + testEnv1 + `"],"key_ids":["` + testKey1 + `"]}`, "copy", 1, 1},
		"publish":                      {`{"purpose":"publish","operation":"value.copy-destination","environment_ids":["` + testEnv1 + `"],"key_ids":["` + testKey1 + `"]}`, "publish", 1, 1},
		"approve":                      {`{"purpose":"approve","operation":"approval.vote","environment_ids":["` + testEnv1 + `"],"key_ids":["` + testKey1 + `"]}`, "approve", 1, 1},
		"reject":                       {`{"purpose":"reject","operation":"approval.vote","environment_ids":["` + testEnv1 + `"],"key_ids":["` + testKey1 + `"]}`, "reject", 1, 1},
		"bypass":                       {`{"purpose":"bypass","operation":"approval.bypass","environment_ids":["` + testEnv1 + `"],"key_ids":["` + testKey1 + `"]}`, "bypass", 1, 1},
		"adapter":                      {`{"purpose":"adapter","operation":"adapter.sync","environment_ids":["` + testEnv1 + `","` + testEnv2 + `"]}`, "adapter", 2, 0},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			c, secret := h.connect()
			started := h.fakeReauth(secret.SessionID)
			rec := h.page(c, "POST", "/__shell/reauth", tc.body)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status %d %s", rec.Code, rec.Body)
			}
			got := started()
			if len(got) != 1 || got[0].Purpose != tc.purpose || len(got[0].EnvironmentIds) != tc.envs {
				t.Fatalf("handoff start %+v", got)
			}
			if got[0].KeyIds != nil {
				if keys := len(*got[0].KeyIds); keys != tc.keys {
					t.Errorf("keys %d", keys)
				}
			} else if tc.keys != 0 {
				t.Error("key_ids missing")
			}
			if (tc.purpose == "self-config") != (got[0].SelfConfig != nil) {
				t.Errorf("self_config %+v", got[0].SelfConfig)
			}
			if rotated, _ := h.secrets.Get(c.host); rotated.Token == secret.Token || rotated.Origin != secret.Origin {
				t.Error("the session was not rotated in the keychain")
			}
		})
	}
}

// The wrong shape for a purpose never reaches the handoff.
func TestReauthRefusesTheWrongShapeForAPurpose(t *testing.T) {
	h := newHarness(t)
	c, secret := h.connect()
	started := h.fakeReauth(secret.SessionID)
	selfConfig := `{"action":"apply","confirm_restored_credentials":false,"expected_generation":3,"owner_instance_id":"own","preview_token":"p","revision":7,"schema_version":1,"to":"v2"}`
	env := `"environment_ids":["` + testEnv1 + `"]`
	for name, body := range map[string]string{
		"self-config naming an environment": `{"purpose":"self-config","operation":"self-config.apply",` + env + `,"self_config":` + selfConfig + `}`,
		"self-config with keys":             `{"purpose":"self-config","operation":"self-config.apply","key_ids":["` + testKey1 + `"],"self_config":` + selfConfig + `}`,
		"self-config without intent":        `{"purpose":"self-config","operation":"self-config.apply"}`,
		"reveal without keys":               `{"purpose":"reveal","operation":"value.reveal",` + env + `}`,
		"reveal without environment":        `{"purpose":"reveal","operation":"value.reveal","key_ids":["` + testKey1 + `"]}`,
		"reveal over two environments":      `{"purpose":"reveal","operation":"value.reveal","environment_ids":["` + testEnv1 + `","` + testEnv2 + `"],"key_ids":["` + testKey1 + `"]}`,
		"adapter without environments":      `{"purpose":"adapter","operation":"adapter.sync"}`,
		"adapter with keys":                 `{"purpose":"adapter","operation":"adapter.sync",` + env + `,"key_ids":["` + testKey1 + `"]}`,
		"intent on another purpose":         `{"purpose":"adapter","operation":"adapter.sync",` + env + `,"self_config":` + selfConfig + `}`,
	} {
		if rec := h.page(c, "POST", "/__shell/reauth", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d %s", name, rec.Code, rec.Body)
		}
	}
	if len(started()) != 0 || len(h.openedURLs()) != 0 {
		t.Error("an invalid ceremony reached the instance or the browser")
	}
}
