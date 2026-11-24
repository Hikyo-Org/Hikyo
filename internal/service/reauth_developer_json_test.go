package service

import (
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// The persisted developer-credential binding is hashed, so the Duration keeps
// its v1 nanosecond-integer encoding under encoding/json/v2.
func TestDeveloperCredentialReauthTargetKeepsNanosecondEncoding(t *testing.T) {
	for _, tc := range []struct {
		target DeveloperCredentialReauthTarget
		want   string
	}{
		{DeveloperCredentialReauthTarget{time.Hour, true, false}, `{"lifetime":3600000000000,"consent_current_and_future":true}`},
		{DeveloperCredentialReauthTarget{8 * time.Hour, true, true}, `{"lifetime":28800000000000,"consent_current_and_future":true,"personal_values":true}`},
	} {
		got, err := json.Marshal(developerCredentialBinding{Purpose: "p", EnvironmentID: "e", KeyIDs: nil, Target: tc.target}, jsonwire.Stable)
		if err != nil {
			t.Fatal(err)
		}
		if want := `{"purpose":"p","environment_id":"e","key_ids":null,"target":` + tc.want + `}`; string(got) != want {
			t.Fatalf("encoded %s, want %s", got, want)
		}
		var back developerCredentialBinding
		if err := json.Unmarshal(got, &back); err != nil {
			t.Fatal(err)
		}
		if back.Target != tc.target {
			t.Fatalf("round trip %+v, want %+v", back.Target, tc.target)
		}
	}
}
