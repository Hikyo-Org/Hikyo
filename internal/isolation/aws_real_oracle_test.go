package isolation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func newOracleVisibilitySandbox(t *testing.T, code string, stale int, value string) (*realAWSSandbox, string, *atomic.Int32) {
	t.Helper()
	name := "hikyo-offline/assume-role/postgres/role-document"
	arn := "arn:aws:secretsmanager:eu-central-1:099256073061:secret:" + name + "-AbCd12"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("X-Amz-Target") != "secretsmanager.GetSecretValue" || !strings.HasPrefix(req.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("observer escaped signed value-read request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var params map[string]string
		if json.NewDecoder(req.Body).Decode(&params) != nil || len(params) != 2 || params["SecretId"] != arn || params["VersionStage"] != "AWSCURRENT" {
			t.Error("observer changed exact ARN or current selector")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		if int(n) <= stale {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"__type": code})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"SecretString": value})
	}))
	t.Cleanup(server.Close)
	s := &realAWSSandbox{t: t, current: map[string]string{name: arn}, owned: map[string]string{arn: name}, oracle: externalAWS{origin: server.URL, region: "eu-central-1", account: "099256073061", creds: aws.Credentials{AccessKeyID: "AKIAHIKYOTEST0000001", SecretAccessKey: "synthetic-offline-observer"}}}
	return s, name, &calls
}

// This calls the actual fixture oracle after a signed, exact-ARN request. A
// missing first response must not fail if the same current value becomes visible.
func TestAWSRealOracleValueVisibility(t *testing.T) {
	for _, stale := range []int{0, 1, 2} {
		t.Run(string(rune('0'+stale))+" invisible reads", func(t *testing.T) {
			s, name, calls := newOracleVisibilitySandbox(t, "ResourceNotFoundException", stale, "synthetic-current")
			if s.value(t, name) != "synthetic-current" {
				t.Fatal("oracle value assertion changed")
			}
			if calls.Load() != int32(stale+1) {
				t.Fatal("oracle did not stop at the first visible current value")
			}
		})
	}
}

func TestAWSRealOracleReadGuards(t *testing.T) {
	for _, scenario := range []struct {
		name, code, value string
		stale             int
		wantCalls         int32
	}{
		{"persistent invisibility", "ResourceNotFoundException", "synthetic-current", 9, 3},
		{"access denied", "AccessDeniedException", "synthetic-current", 9, 1},
		{"decryption refused", "DecryptionFailure", "synthetic-current", 9, 1},
		{"server error", "InternalServiceError", "synthetic-current", 9, 1},
		{"wrong visible value", "", "unexpected-current", 0, 1},
		{"unrecorded ARN", "", "synthetic-current", 0, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, name, calls := newOracleVisibilitySandbox(t, scenario.code, scenario.stale, scenario.value)
			arn := s.arn(t, name)
			if scenario.name == "unrecorded ARN" {
				arn += "unrecorded"
			}
			value, err := s.currentValue(t.Context(), arn)
			if calls.Load() != scenario.wantCalls {
				t.Fatalf("oracle requests=%d, want %d", calls.Load(), scenario.wantCalls)
			}
			if scenario.name == "wrong visible value" {
				if err != nil || value != "unexpected-current" || value == "synthetic-current" {
					t.Fatal("oracle masked a mismatched value")
				}
			} else if scenario.name == "unrecorded ARN" {
				if err == nil {
					t.Fatal("oracle followed an unrecorded ARN")
				}
			} else if !realAWSCode(err, scenario.code) {
				t.Fatal("oracle masked a permanent refusal or persistent invisibility")
			}
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		s, name, calls := newOracleVisibilitySandbox(t, "ResourceNotFoundException", 9, "synthetic-current")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		arn := s.arn(t, name)
		go func() { _, err := s.currentValue(ctx, arn); done <- err }()
		// Cancel once the first read has reached the loopback oracle. Whether it
		// interrupts that response or the wait, it must prevent another request.
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for calls.Load() == 0 {
			select {
			case <-deadline.C:
				t.Fatal("oracle did not receive its first read")
			case <-tick.C:
			}
		}
		cancel()
		select {
		case err := <-done:
			if err == nil || calls.Load() != 1 {
				t.Fatal("oracle ignored cancellation or sent another read")
			}
		case <-deadline.C:
			t.Fatal("oracle did not stop after cancellation")
		}
	})
}
