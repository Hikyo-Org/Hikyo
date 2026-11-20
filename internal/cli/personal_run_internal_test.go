package cli

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
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
// notice for, lets a required key the caller holds nothing for take the shared
// value (this path skips the requirement check), and an older server without
// the operation holds none.
func TestHumanSessionRunRefusesHeldPersonalKeys(t *testing.T) {
	notice := apigen.PersonalValueNoticeCauseKeyRenamed
	required := true
	for _, revision := range []int{api.Revision, 14} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == api.PathPrefix+"/meta" {
				_ = jsonwire.Encode(w, apigen.Meta{ServerVersion: "fixture", ApiRevision: revision})
				return
			}
			_ = jsonwire.Encode(w, apigen.PersonalValueList{Items: []apigen.PersonalValueEntry{
				{Name: "MINE", Set: true},
				{Name: "NOTICED", Notice: &notice},
				{Name: "REQUIRED_ONLY", Required: &required},
			}})
		}))
		client, err := NewClient(pinnedTestEntry("test", srv), "human-test")
		if err != nil {
			t.Fatal(err)
		}
		project := api.PathPrefix + "/orgs/org_test/projects/prj_test"
		err = refuseHumanPersonalKeys(t.Context(), client, project, "env_test", []string{"SHARED_ONLY", "MINE", "NOTICED", "REQUIRED_ONLY"})
		if revision == api.Revision {
			var failure *Error
			if !errors.As(err, &failure) || failure.Code != ExitRefused {
				t.Fatalf("held keys not refused: %v", err)
			}
			for _, want := range []string{"MINE", "NOTICED", "hikyo personal list"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("refusal does not name %q: %v", want, err)
				}
			}
			if strings.Contains(err.Error(), "REQUIRED_ONLY") || strings.Contains(err.Error(), "SHARED_ONLY") {
				t.Fatalf("refusal names a key the caller holds nothing for: %v", err)
			}
		}
		if revision < api.Revision && err != nil {
			t.Fatalf("older server refused: %v", err)
		}
		if err := refuseHumanPersonalKeys(t.Context(), client, project, "env_test", []string{"SHARED_ONLY", "REQUIRED_ONLY"}); err != nil {
			t.Fatalf("unheld or required-only key refused: %v", err)
		}
		srv.Close()
	}
}

// Only a server older than the operation's minimum revision counts as holding
// nothing; a client missing the contract entry is an error, not an old server.
func TestServesPersonalListFailsClosed(t *testing.T) {
	ops, err := api.Operations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := servesPersonalList(apigen.Meta{ApiRevision: api.Revision}, map[string]api.Operation{}); err == nil {
		t.Fatal("a missing contract entry was read as an old server")
	}
	for revision, want := range map[int]bool{14: false, api.Revision: true} {
		got, err := servesPersonalList(apigen.Meta{ApiRevision: revision}, ops)
		if err != nil || got != want {
			t.Fatalf("revision %d: serves = %v, %v; want %v", revision, got, err, want)
		}
	}
}

// A failed meta or personal-values lookup refuses the run rather than
// treating the caller as holding nothing and serving the shared value.
func TestHumanSessionRunFailsClosedOnLookupError(t *testing.T) {
	for _, failing := range []string{"/meta", "/personal-values"} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, failing) {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == api.PathPrefix+"/meta" {
				_ = jsonwire.Encode(w, apigen.Meta{ServerVersion: "fixture", ApiRevision: api.Revision})
				return
			}
			_ = jsonwire.Encode(w, apigen.PersonalValueList{})
		}))
		client, err := NewClient(pinnedTestEntry("test", srv), "human-test")
		if err != nil {
			t.Fatal(err)
		}
		project := api.PathPrefix + "/orgs/org_test/projects/prj_test"
		if err := refuseHumanPersonalKeys(t.Context(), client, project, "env_test", []string{"SHARED_ONLY"}); err == nil {
			t.Fatalf("%s failure did not refuse the run", failing)
		}
		srv.Close()
	}
}
