package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func deliveredPersonal(name string, personal apigen.DeliveredKeyPersonal, reason string) apigen.DeliveredKey {
	k := apigen.DeliveredKey{Name: name, Personal: &personal}
	if reason != "" {
		k.PersonalReason = &reason
	}
	return k
}

// `hikyo run` refuses every withheld or noticed key instead of serving the
// shared value, and only names (never prints) a delivered personal value.
func TestRunRefusesWithheldAndNoticedPersonalKeys(t *testing.T) {
	for _, tc := range []struct{ personal, reason, want string }{
		{"withheld", "consent", "--personal"},
		{"withheld", "config_only", "--config-only"},
		{"withheld", "suspended", "suspended"},
		{"notice", "key_renamed", "removal notice: key_renamed"},
		{"missing", "", "required here and you have none"},
	} {
		var stderr bytes.Buffer
		err := refusePersonalKeys(IO{Stderr: &stderr}, []apigen.DeliveredKey{
			{Name: "SHARED_ONLY"},
			deliveredPersonal("MINE", apigen.DeliveredKeyPersonal(tc.personal), tc.reason),
		})
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != ExitRefused || !strings.Contains(err.Error(), "MINE") || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s/%s: refusal = %v", tc.personal, tc.reason, err)
		}
	}
	var stderr bytes.Buffer
	if err := refusePersonalKeys(IO{Stderr: &stderr}, []apigen.DeliveredKey{deliveredPersonal("MINE", apigen.DeliveredKeyPersonalValue, "")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "using your personal value for MINE") {
		t.Fatal("a delivered personal value was not named on stderr")
	}
}

// The human-session path refuses a key the caller holds a personal value or
// notice for, and an older server without the operation holds none.
func TestHumanSessionRunRefusesHeldPersonalKeys(t *testing.T) {
	for _, revision := range []int{api.Revision, 14} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == api.PathPrefix+"/meta" {
				_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "fixture", ApiRevision: revision})
				return
			}
			_ = json.NewEncoder(w).Encode(apigen.PersonalValueList{Items: []apigen.PersonalValueEntry{{Name: "MINE", Set: true}}})
		}))
		client, err := NewClient(pinnedTestEntry("test", srv), "human-test")
		if err != nil {
			t.Fatal(err)
		}
		project := api.PathPrefix + "/orgs/org_test/projects/prj_test"
		err = refuseHumanPersonalKeys(t.Context(), client, project, "env_test", []string{"SHARED_ONLY", "MINE"})
		if revision == api.Revision && (err == nil || !strings.Contains(err.Error(), "MINE")) {
			t.Fatalf("held key not refused: %v", err)
		}
		if revision < api.Revision && err != nil {
			t.Fatalf("older server refused: %v", err)
		}
		if err := refuseHumanPersonalKeys(t.Context(), client, project, "env_test", []string{"SHARED_ONLY"}); err != nil {
			t.Fatalf("unheld key refused: %v", err)
		}
		srv.Close()
	}
}
