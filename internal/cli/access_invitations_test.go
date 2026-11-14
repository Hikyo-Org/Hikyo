package cli_test

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
)

const invitationOrg = "org_01989abc-def0-7123-8123-000000000001"

var invitationExpiry = time.Date(2030, 1, 8, 0, 0, 0, 0, time.UTC)

type invitationFake struct {
	t         *testing.T
	accepted  []string
	previews  int
	signedOut bool
	notFound  bool
	mailer    bool
	// mailerDetail makes the send refuse with this detail instead.
	mailerDetail string
	// wantToken is the token the preview must carry; "" skips the check.
	wantToken string
	// onAccept runs once the accept request arrives, before the response.
	onAccept func()
}

func (f *invitationFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, code, detail string) {
		w.WriteHeader(status)
		_ = writeJSONLine(w, map[string]any{"error": map[string]any{"code": code, "message": "refused", "detail": detail}})
	}
	base := "/api/v1/orgs/" + invitationOrg + "/email-invitations"
	switch {
	case r.Method == http.MethodPost && r.URL.Path == base:
		if f.mailerDetail != "" {
			fail(400, "bad_request", f.mailerDetail)
			return
		}
		if f.mailer {
			fail(400, "bad_request", "mailer-unconfigured")
			return
		}
		var body apigen.EmailInvitationSendRequest
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			f.t.Errorf("send body: %v", err)
		}
		if body.Email != "dana@example.test" || body.Template != "viewer" {
			f.t.Errorf("send body = %+v, want dana@example.test as viewer", body)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = writeJSONLine(w, apigen.EmailInvitation{Id: "inv_1", Email: body.Email, Template: body.Template, ExpiresAt: invitationExpiry})
	case r.Method == http.MethodGet && r.URL.Path == base:
		_ = writeJSONLine(w, apigen.EmailInvitationList{Count: 1, Items: []apigen.EmailInvitationListing{
			{Id: "inv_1", Email: "dana@example.test", Template: "viewer", InvitedBy: "usr_9", ExpiresAt: invitationExpiry}}})
	case r.Method == http.MethodDelete && r.URL.Path == base+"/inv_1":
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/me/invitations/preview":
		f.previews++
		var pbody apigen.EmailInvitationTokenRequest
		if err := json.UnmarshalRead(r.Body, &pbody); err != nil {
			f.t.Errorf("preview body: %v", err)
		}
		if f.wantToken != "" && pbody.Token != f.wantToken {
			f.t.Errorf("preview token = %q, want %q", pbody.Token, f.wantToken)
		}
		if f.notFound {
			fail(404, "not_found", "")
			return
		}
		_ = writeJSONLine(w, apigen.EmailInvitationPreview{Org: invitationOrg, OrgName: "Acme", Template: "viewer", ExpiresAt: invitationExpiry})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/me/invitations/accept":
		var body apigen.EmailInvitationTokenRequest
		_ = json.UnmarshalRead(r.Body, &body)
		f.accepted = append(f.accepted, body.Token)
		if f.onAccept != nil {
			f.onAccept()
		}
		_ = writeJSONLine(w, apigen.EmailInvitationAcceptance{Org: invitationOrg, OrgName: "Acme", Template: "viewer", GrantsCreated: 2, SignedOut: f.signedOut})
	default:
		http.NotFound(w, r)
	}
}

func TestInvitationSendListRevoke(t *testing.T) {
	ios, stdout, stderr := definitionsTestIO(t, &invitationFake{t: t})
	args := func(rest ...string) []string {
		return append(append([]string{"access", "invitation"}, rest...), "--instance", "local", "--org", invitationOrg)
	}
	if code := cli.Run(t.Context(), ios, args("send", "dana@example.test", "--template", "viewer")); code != cli.ExitOK {
		t.Fatalf("send code=%d stderr=%s", code, stderr)
	}
	out := stderr.String()
	if !strings.Contains(out, "recorded") || !strings.Contains(out, "If that address belongs to an account") || strings.Contains(out, "matched") {
		t.Fatalf("send wording: %s", out)
	}
	stdout.Reset()
	if code := cli.Run(t.Context(), ios, args("send", "dana@example.test", "--template", "viewer", "-o", "json")); code != cli.ExitOK {
		t.Fatal("json send failed")
	}
	var sent apigen.EmailInvitation
	if err := json.Unmarshal(stdout.Bytes(), &sent); err != nil || sent.Id != "inv_1" {
		t.Fatalf("json send: %v %s", err, stdout)
	}
	stdout.Reset()
	if code := cli.Run(t.Context(), ios, args("list")); code != cli.ExitOK || !strings.Contains(stdout.String(), "dana@example.test") || !strings.Contains(stdout.String(), "usr_9") {
		t.Fatalf("list code stdout=%s", stdout)
	}
	if code := cli.Run(t.Context(), ios, args("revoke", "inv_1")); code != cli.ExitOK {
		t.Fatalf("revoke stderr=%s", stderr)
	}
	if code := cli.Run(t.Context(), ios, args("send", "dana@example.test")); code != cli.ExitUsage {
		t.Fatalf("send without template code=%d", code)
	}
}

func TestInvitationSendMailerUnconfigured(t *testing.T) {
	ios, _, stderr := definitionsTestIO(t, &invitationFake{t: t, mailer: true})
	code := cli.Run(t.Context(), ios, []string{"access", "invitation", "send", "a@example.test", "--template", "viewer", "--instance", "local", "--org", invitationOrg})
	for _, want := range []string{"configured mailer", "access member invite", "SCIM", "administrator of both organisations"} {
		if code != cli.ExitRefused || !strings.Contains(stderr.String(), want) {
			t.Fatalf("code=%d missing %q in %s", code, want, stderr)
		}
	}
}

func TestInvitationSendNoPublicOrigin(t *testing.T) {
	ios, _, stderr := definitionsTestIO(t, &invitationFake{t: t, mailerDetail: "no-public-origin"})
	code := cli.Run(t.Context(), ios, []string{"access", "invitation", "send", "a@example.test", "--template", "viewer", "--instance", "local", "--org", invitationOrg})
	for _, want := range []string{"public origin", "access member invite", "SCIM"} {
		if code != cli.ExitRefused || !strings.Contains(stderr.String(), want) {
			t.Fatalf("code=%d missing %q in %s", code, want, stderr)
		}
	}
}

func TestInvitationAccept(t *testing.T) {
	for _, tc := range []struct {
		name, stdin string
	}{
		{"raw token", "tok_abc\n"},
		{"full link", "%s/invitations/accept#token=tok_abc\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &invitationFake{t: t, signedOut: true, wantToken: "tok_abc"}
			ios, stdout, stderr := definitionsTestIO(t, fake)
			ios.Stdin = strings.NewReader(strings.ReplaceAll(tc.stdin, "%s", instanceOrigin(t, ios)))
			ios.TerminalSession, _ = terminalSession(t, "y\n")
			if code := cli.Run(t.Context(), ios, []string{"access", "invitation", "accept", "--instance", "local"}); code != cli.ExitOK {
				t.Fatalf("code=%d stderr=%s", code, stderr)
			}
			if len(fake.accepted) != 1 || fake.accepted[0] != "tok_abc" || fake.previews != 1 {
				t.Fatalf("accepted=%v previews=%d", fake.accepted, fake.previews)
			}
			if !strings.Contains(stdout.String(), "Acme") || !strings.Contains(stderr.String(), "hikyo login") {
				t.Fatalf("stdout=%s stderr=%s", stdout, stderr)
			}
			st, err := cli.NewState(ios.Env)
			if err != nil {
				t.Fatal(err)
			}
			if sessions, err := st.Sessions(); err != nil || len(sessions) != 0 {
				t.Fatalf("stored session survived signed_out: %v %v", sessions, err)
			}
		})
	}
}

func instanceOrigin(t *testing.T, ios cli.IO) string {
	t.Helper()
	st, err := cli.NewState(ios.Env)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := st.Sessions()
	if err != nil || sessions["local"].Origin == "" {
		t.Fatalf("no local session: %v %v", sessions, err)
	}
	return sessions["local"].Origin
}

func TestInvitationAcceptRefusesForeignLinkOrigin(t *testing.T) {
	for _, link := range []string{
		"https://evil.example/invitations/accept#token=tok_abc",
		"/invitations/accept#token=tok_abc",
		"invitations/accept#token=tok_abc",
	} {
		fake := &invitationFake{t: t}
		ios, _, stderr := definitionsTestIO(t, fake)
		ios.Stdin = strings.NewReader(link)
		code := cli.Run(t.Context(), ios, []string{"access", "invitation", "accept", "--yes", "--instance", "local"})
		if code != cli.ExitUsage || fake.previews != 0 || len(fake.accepted) != 0 || !strings.Contains(stderr.String(), "does not belong") {
			t.Fatalf("%q: code=%d previews=%d accepted=%v stderr=%s", link, code, fake.previews, fake.accepted, stderr)
		}
	}
}

func TestInvitationAcceptReportsCleanupFailureAfterResult(t *testing.T) {
	fake := &invitationFake{t: t, signedOut: true}
	ios, stdout, stderr := definitionsTestIO(t, fake)
	st, err := cli.NewState(ios.Env)
	if err != nil {
		t.Fatal(err)
	}
	// Acceptance commits server-side; a sessions file that stopped being a
	// regular file makes the local session removal fail afterwards.
	fake.onAccept = func() {
		path := filepath.Join(st.Dir(), "sessions.json")
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Error(err)
		}
	}
	ios.Stdin = strings.NewReader("tok_abc")
	code := cli.Run(t.Context(), ios, []string{"access", "invitation", "accept", "--yes", "--instance", "local"})
	if code == cli.ExitOK || !strings.Contains(stdout.String(), "Acme") || !strings.Contains(stderr.String(), "was accepted, but removing the local session failed") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

func TestInvitationAcceptTellsSignOutWhenRenderFails(t *testing.T) {
	fake := &invitationFake{t: t, signedOut: true}
	ios, _, stderr := definitionsTestIO(t, fake)
	ios.Stdout = failingWriter{}
	ios.Stdin = strings.NewReader("tok_abc")
	code := cli.Run(t.Context(), ios, []string{"access", "invitation", "accept", "--yes", "--instance", "local"})
	if code == cli.ExitOK || len(fake.accepted) != 1 || !strings.Contains(stderr.String(), "signed out on every device and your developer credentials ended") {
		t.Fatalf("code=%d accepted=%v stderr=%s", code, fake.accepted, stderr)
	}
}

func TestInvitationAcceptYesKeepsSessionWhenNotSignedOut(t *testing.T) {
	fake := &invitationFake{t: t}
	ios, _, stderr := definitionsTestIO(t, fake)
	ios.Stdin = strings.NewReader("tok_abc")
	code := cli.Run(t.Context(), ios, []string{"access", "invitation", "accept", "--yes", "--instance", "local"})
	if code != cli.ExitOK || !strings.Contains(stderr.String(), "sign you out on every device") || strings.Contains(stderr.String(), "hikyo login") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	st, _ := cli.NewState(ios.Env)
	if sessions, _ := st.Sessions(); len(sessions) != 1 {
		t.Fatalf("session removed without signed_out: %v", sessions)
	}
}

func TestInvitationAcceptNeedsConfirmation(t *testing.T) {
	fake := &invitationFake{t: t}
	ios, _, stderr := definitionsTestIO(t, fake)
	ios.Stdin = strings.NewReader("tok_abc")
	if code := cli.Run(t.Context(), ios, []string{"access", "invitation", "accept", "--instance", "local"}); code != cli.ExitRefused || len(fake.accepted) != 0 {
		t.Fatalf("no terminal and no --yes: code=%d accepted=%v stderr=%s", code, fake.accepted, stderr)
	}
	ios, _, _ = definitionsTestIO(t, fake)
	ios.Stdin = strings.NewReader("tok_abc")
	ios.TerminalSession, _ = terminalSession(t, "n\n")
	if code := cli.Run(t.Context(), ios, []string{"access", "invitation", "accept", "--instance", "local"}); code != cli.ExitRefused || len(fake.accepted) != 0 {
		t.Fatalf("declined: code=%d accepted=%v", code, fake.accepted)
	}
}

func TestInvitationAcceptUniformNotFound(t *testing.T) {
	ios, _, stderr := definitionsTestIO(t, &invitationFake{t: t, notFound: true})
	ios.Stdin = strings.NewReader("tok_abc")
	code := cli.Run(t.Context(), ios, []string{"access", "invitation", "accept", "--yes", "--instance", "local"})
	if code != cli.ExitNotFound || !strings.Contains(stderr.String(), "not valid for this account or has expired") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestInvitationAcceptRefusesTokenArgument(t *testing.T) {
	fake := &invitationFake{t: t}
	for _, args := range [][]string{
		{"access", "invitation", "accept", "tok_abc"},
		{"access", "invitation", "accept", "--token", "tok_abc"},
	} {
		ios, _, _ := definitionsTestIO(t, fake)
		ios.Stdin = strings.NewReader("tok_abc")
		if code := cli.Run(t.Context(), ios, append(args, "--yes", "--instance", "local")); code != cli.ExitUsage {
			t.Fatalf("%v: code=%d, want usage error", args, code)
		}
	}
	if fake.previews != 0 {
		t.Fatal("a refused invocation reached the server")
	}
}

func TestInvitationSendOtherRefusalKeepsServerMessage(t *testing.T) {
	// A refusal whose detail merely mentions the marker must not be rewritten.
	ios, _, stderr := definitionsTestIO(t, &invitationFake{t: t, mailerDetail: "not mailer-unconfigured at all"})
	code := cli.Run(t.Context(), ios, []string{"access", "invitation", "send", "a@example.test", "--template", "viewer", "--instance", "local", "--org", invitationOrg})
	if code != cli.ExitRefused || strings.Contains(stderr.String(), "configured mailer") || !strings.Contains(stderr.String(), "not mailer-unconfigured at all") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}
