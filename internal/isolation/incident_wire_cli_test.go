package isolation

import (
	"bytes"
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The hikyo incident verbs against the real TLS router, authorization and
// persistence (#1039 acceptance 9): open, items, attest, resume, show and close,
// each spelled as an operator types it.
func TestIncidentCLIExercise(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		iw := newIncidentWire(t, db)
		token := iw.session(t, wireResponder)
		stateDir, workdir := t.TempDir(), t.TempDir()
		env := cli.Env{Getenv: func(key string) string {
			if key == "HIKYO_STATE_DIR" {
				return stateDir
			}
			return ""
		}}
		state, err := cli.NewState(env)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Trust().Put(cli.TrustEntry{Name: "clean", Origin: iw.srv.URL, SPKIPin: cli.SPKIFingerprint(iw.srv.Certificate())}); err != nil {
			t.Fatal(err)
		}
		if err := state.PutSession(cli.SessionArtifact{Instance: "clean", Origin: iw.srv.URL, Token: token, SessionID: "ses_wire_" + string(wireResponder), Principal: string(wireResponder), ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}); err != nil {
			t.Fatal(err)
		}
		run := func(args ...string) string {
			t.Helper()
			var stdout, stderr bytes.Buffer
			code := cli.Run(t.Context(), cli.IO{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Env: env, Workdir: workdir}, append(args, "--instance", "clean"))
			if code != cli.ExitOK {
				t.Fatalf("hikyo %v: exit %d: %s", args, code, stderr.String())
			}
			return stdout.String()
		}

		var inc apigen.Incident
		if err := json.Unmarshal([]byte(run("incident", "open", "--scope", "account", "--account", string(wireTarget), "-o", "json")), &inc); err != nil || inc.Status != "open" {
			t.Fatalf("open: %+v, %v", inc, err)
		}
		// The table says "not yet run" for an item no owner has touched, and the
		// JSON carries the id the attest verb takes.
		if table := run("incident", "items", inc.Id); !strings.Contains(table, "not yet run") || !strings.Contains(table, "STATE") {
			t.Fatalf("items table:\n%s", table)
		}
		var list apigen.IncidentItemList
		if err := json.Unmarshal([]byte(run("incident", "items", inc.Id, "--open", "--limit", "500", "-o", "json")), &list); err != nil {
			t.Fatal(err)
		}
		var attestID string
		for _, it := range list.Items {
			if it.Attestable {
				attestID = it.Id
			}
		}
		if attestID == "" {
			t.Fatalf("no attestable item among %d open items", len(list.Items))
		}
		if out := run("incident", "attest", inc.Id, attestID); !strings.Contains(out, "attested "+attestID) {
			t.Fatalf("attest output %q", out)
		}
		if out := run("incident", "resume", inc.Id); !strings.Contains(out, "EXAMINED") {
			t.Fatalf("resume output %q", out)
		}
		if out := run("incident", "close", inc.Id); !strings.Contains(out, "closed") {
			t.Fatalf("close output %q", out)
		}
		var shown apigen.Incident
		if err := json.Unmarshal([]byte(run("incident", "show", inc.Id, "-o", "json")), &shown); err != nil || shown.Status != "closed" || shown.Counts.Attested != 1 {
			t.Fatalf("show after close: %+v, %v", shown, err)
		}

		// A caller with no authority sees the CLI's uniform not-found exit.
		if err := state.PutSession(cli.SessionArtifact{Instance: "clean", Origin: iw.srv.URL, Token: iw.session(t, wireForeign), SessionID: "ses_wire_" + string(wireForeign), Principal: string(wireForeign), ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		code := cli.Run(t.Context(), cli.IO{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Env: env, Workdir: workdir}, []string{"incident", "show", inc.Id, "--instance", "clean"})
		if code == cli.ExitOK || stdout.Len() != 0 {
			t.Fatalf("a foreign operator reading the incident: exit %d stdout %q", code, stdout.String())
		}
	})
}
