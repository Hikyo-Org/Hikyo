package isolation

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestRunHumanSessionDeliversThroughTheServerSQLite(t *testing.T) {
	runHumanSessionDelivery(t, store.EngineSQLite)
}

func TestRunHumanSessionDeliversThroughTheServerPostgres(t *testing.T) {
	runHumanSessionDelivery(t, store.EnginePostgres)
}

// runHumanSessionDelivery drives the locked #18 exception against the real wire
// contract, whose artifact admission a fake server cannot stand in for.
func runHumanSessionDelivery(t *testing.T, engine store.Engine) {
	rig := bootComposeRig(t, engine)
	execRaw(t, rig.db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_c_adm_reveal', '`+cAdmin+`', 'reveal', '`+cOrg+`', '`+cPrj+`', '`+cEnv+`', `+ts+`)`)
	seedOrigins(t, rig.db)
	execRaw(t, rig.db, `UPDATE environments SET reauth_window_seconds = 300 WHERE id = '`+cEnv+`'`)
	token := sessionWithWindows(t, rig.db, cAdmin, cEnv)

	run := func(t *testing.T, answer io.Reader, extra ...string) (int, string, []string, *bytes.Buffer) {
		t.Helper()
		var stdout, stderr strings.Builder
		var terminal bytes.Buffer
		session, err := disclose.NewTerminalSession(socialAcceptanceTerminal{struct {
			io.Reader
			io.Writer
		}{answer, &terminal}})
		if err != nil {
			t.Fatal(err)
		}
		var injected []string
		ios := cli.IO{
			Stdout: &stdout, Stderr: &stderr, Workdir: t.TempDir(),
			Env: cli.Env{Getenv: func(k string) string {
				if k == "HIKYO_STATE_DIR" {
					return rig.stateDir
				}
				return ""
			}},
			TerminalSession:  session,
			StderrIsTerminal: func() bool { return true },
			Exec:             func(_ string, _, env []string) error { injected = env; return nil },
		}
		state, err := cli.NewState(ios.Env)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.PutSession(cli.SessionArtifact{
			Instance: "local", Origin: rig.origin, Token: token, SessionID: "ses_" + cAdmin,
			Principal: cAdmin, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		}); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"run", "--use-human-session"}, extra...)
		args = append(args, "--instance", "local", "--org", cOrg, "--project", cPrj, "--env", cEnv, "--", "true")
		return cli.Run(t.Context(), ios, args), stderr.String(), injected, &terminal
	}
	disclosures := func() int64 {
		return queryInt(t, rig.db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'disclosure.value_revealed' AND payload LIKE '%"surface":"export"%'`)
	}

	// Enumeration precedes disclosure: a decline leaves no record and no process.
	code, stderr, injected, terminal := run(t, strings.NewReader("n\n"))
	if code != cli.ExitRefused || injected != nil || !strings.Contains(stderr, "declined") {
		t.Fatalf("declined: exit=%d started=%t stderr=%s", code, injected != nil, stderr)
	}
	if !strings.Contains(terminal.String(), "DATABASE_PASSWORD") {
		t.Fatalf("confirmation did not enumerate the secret key: %s", terminal.String())
	}
	if got := disclosures(); got != 0 {
		t.Fatalf("declined run wrote %d disclosure records, want 0", got)
	}

	code, stderr, injected, _ = run(t, strings.NewReader("y\n"))
	if code != cli.ExitOK {
		t.Fatalf("full projection: exit=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{"DATABASE_URL=postgres://dev", "DATABASE_PASSWORD=dev-secret", "WORKER_URL=http://worker"} {
		if !slices.Contains(injected, want) {
			t.Errorf("child environment is missing %q", want)
		}
	}
	if got := disclosures(); got != 1 {
		t.Fatalf("full projection wrote %d disclosure records, want 1 for the one secret", got)
	}
	if got := queryInt(t, rig.db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'disclosure.value_revealed' AND actor_id = '`+cAdmin+`' AND actor_class = 'human'`); got != 1 {
		t.Fatalf("disclosure attributed to the human %d times, want 1", got)
	}

	code, stderr, injected, terminal = run(t, strings.NewReader("y\n"), "--config-only")
	if code != cli.ExitOK {
		t.Fatalf("config-only: exit=%d stderr=%s", code, stderr)
	}
	if !slices.Contains(injected, "DATABASE_URL=postgres://dev") || slices.ContainsFunc(injected, func(kv string) bool { return strings.HasPrefix(kv, "DATABASE_PASSWORD=") }) {
		t.Fatal("config-only child environment lacks DATABASE_URL or holds DATABASE_PASSWORD")
	}
	if prompt := terminal.String(); !strings.Contains(prompt, "2 value(s)") || strings.Contains(prompt, "DATABASE_PASSWORD") {
		t.Fatalf("config-only confirmation enumerated the wrong set: %s", prompt)
	}
	if got := disclosures(); got != 1 {
		t.Fatalf("config-only wrote a disclosure record: total %d, want 1", got)
	}

	// A publish during the prompt must not disclose what the human did not see.
	publishDuringPrompt := func(key, value string) io.Reader {
		answer := strings.NewReader("y\n")
		return readerFunc(func(p []byte) (int, error) {
			if answer.Len() == 2 {
				publishComposeValues(t, rig.db, map[string]string{key: value})
			}
			return answer.Read(p)
		})
	}
	code, stderr, injected, _ = run(t, publishDuringPrompt("DATABASE_PASSWORD", "published-during-the-prompt"))
	if code != cli.ExitRefused || injected != nil || !strings.Contains(stderr, "published to during this run") {
		t.Fatalf("publish during the prompt: exit=%d started=%t stderr=%s", code, injected != nil, stderr)
	}
	if got := disclosures(); got != 1 {
		t.Fatalf("publish during the prompt wrote a disclosure record: total %d, want 1", got)
	}

	// Config-only needs no reveal-history, and still injects the confirmed revision.
	code, stderr, injected, _ = run(t, publishDuringPrompt("DATABASE_URL", "postgres://published-too-late"), "--config-only")
	if code != cli.ExitOK {
		t.Fatalf("config-only publish during the prompt: exit=%d stderr=%s", code, stderr)
	}
	if !slices.Contains(injected, "DATABASE_URL=postgres://dev") {
		t.Fatal("config-only child environment lacks the confirmed DATABASE_URL")
	}

	// With reveal-history the run injects exactly the revision that was confirmed.
	execRaw(t, rig.db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_c_adm_history', '`+cAdmin+`', 'reveal-history', '`+cOrg+`', '`+cPrj+`', '`+cEnv+`', `+ts+`)`)
	seedOrigins(t, rig.db)
	code, stderr, injected, _ = run(t, publishDuringPrompt("DATABASE_PASSWORD", "published-too-late"))
	if code != cli.ExitOK {
		t.Fatalf("publish during the prompt with reveal-history: exit=%d stderr=%s", code, stderr)
	}
	if !slices.Contains(injected, "DATABASE_PASSWORD=published-during-the-prompt") {
		t.Fatal("child environment lacks the confirmed revision's value")
	}
}
