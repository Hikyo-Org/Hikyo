package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// `hikyo access invitation send|list|revoke|accept` (#1207,
// docs/spec/email-invitations.md section 6): inviting an existing account into
// an organisation by its exact email. The invitation token is read from
// standard input only: argv is visible in shell history and process lists.

const invitationUsage = "usage: hikyo access invitation send <email> --template T [--org O] | list [--org O] | revoke <id> [--org O] | accept [--yes]  (accept reads the token or link from standard input)"

// invitationTokenLimit bounds the token or link read from standard input.
const invitationTokenLimit = 4096

func runAccessInvitation(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("access invitation", args, "send", "list", "revoke", "accept")
	if err != nil {
		return err
	}
	if sub == "accept" {
		return runAccessInvitationAccept(ctx, ios, rest)
	}
	var format, template string
	command := "access invitation " + sub
	st, flags, err := parseCommon(command, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "send" {
			fs.StringVar(&template, "template", "", "role template the person receives on acceptance (required)")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	want := 0
	if sub != "list" {
		want = 1
	}
	if len(flags.positionals) != want || (sub == "send" && template == "") {
		return failf(ExitUsage, "%s", invitationUsage)
	}
	resolved, err := Resolve(st, ios.Env, flags.Flags, ios.Workdir)
	if err != nil {
		return err
	}
	scope, err := resolveAccessScope(resolved, flags, false, command)
	if err != nil {
		return err
	}
	if strings.Contains(scope.path, "/projects/") {
		return failf(ExitUsage, "hikyo %s addresses an organisation (--org): a project has no accounts of its own", command)
	}
	client, _, _, err := authenticatedResolvedTarget(st, ios, flags, resolved)
	if err != nil {
		return err
	}
	path := strings.TrimSuffix(scope.path, "/grants") + "/email-invitations"

	switch sub {
	case "send":
		email := flags.positionals[0]
		var out apigen.EmailInvitation
		body := apigen.EmailInvitationSendRequest{Email: email, Template: apigen.RoleTemplate(template)}
		if err := client.Do(ctx, http.MethodPost, path, body, &out); err != nil {
			var ce *Error
			const alternatives = "Use `hikyo access member invite` to create a new account, SCIM, or an administrator of both organisations instead."
			if asCLIError(err, &ce) && ce.Detail == "mailer-unconfigured" {
				return failf(ExitRefused, "email invitations need a configured mailer, and this instance has none. %s", alternatives)
			}
			if asCLIError(err, &ce) && ce.Detail == "no-public-origin" {
				return failf(ExitRefused, "email invitations need the instance's public origin: its operator must set HIKYO_EXTERNAL_ORIGIN. %s", alternatives)
			}
			return err
		}
		if f == FormatJSON {
			return Render(ios.Stdout, f, Table{JSON: out})
		}
		// Uniform on purpose: the answer is the same whether or not the
		// address belongs to an account, so the wording never says it matched.
		fmt.Fprintf(ios.Stderr,
			"invitation %s recorded for %s as %s at %s, valid until %s.\n"+
				"If that address belongs to an account with a verified email on this instance, the person receives the invitation by email.\n",
			out.Id, out.Email, out.Template, scope.label, out.ExpiresAt.UTC().Format(time.RFC3339))
		return nil

	case "list":
		var out apigen.EmailInvitationList
		if err := client.Do(ctx, http.MethodGet, path, nil, &out); err != nil {
			return err
		}
		rows := make([][]string, 0, len(out.Items))
		for _, it := range out.Items {
			rows = append(rows, []string{it.Id, it.Email, string(it.Template), it.InvitedBy, it.ExpiresAt.UTC().Format(time.RFC3339)})
		}
		return Render(ios.Stdout, f, Table{
			Columns: []string{"ID", "EMAIL", "TEMPLATE", "INVITED BY", "EXPIRES"}, Rows: rows, JSON: out,
		})

	default: // revoke
		id := flags.positionals[0]
		return client.Do(ctx, http.MethodDelete, path+"/"+url.PathEscape(id), nil, nil)
	}
}

// invitationTokenFromInput accepts the bare token or the whole invitation link
// (`.../invitations/accept#token=...`) and returns the token and, for a link,
// its origin ("" for a bare token).
func invitationTokenFromInput(raw string) (token, linkOrigin string) {
	raw = strings.TrimSpace(raw)
	i := strings.Index(raw, "#")
	if i < 0 {
		return raw, ""
	}
	q, err := url.ParseQuery(raw[i+1:])
	if err != nil {
		return "", ""
	}
	u, err := url.Parse(raw[:i])
	if err != nil || u.Host == "" {
		// An origin-less link cannot be matched to an instance, so it must
		// not pass for one that matches.
		return q.Get("token"), "?"
	}
	return q.Get("token"), normalizedOrigin(u)
}

// normalizedOrigin is the URL's scheme and host, lowercased, without the
// scheme's default port, so https://h:443 and https://h compare equal.
func normalizedOrigin(u *url.URL) string {
	scheme, host := strings.ToLower(u.Scheme), strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && !(scheme == "https" && port == "443") && !(scheme == "http" && port == "80") {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
}

func runAccessInvitationAccept(ctx context.Context, ios IO, args []string) error {
	var format string
	var yes bool
	st, flags, err := parseCommon("access invitation accept", ios, args, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		fs.BoolVar(&yes, "yes", false, "accept without the interactive confirmation")
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	if len(flags.positionals) != 0 {
		return failf(ExitUsage, "a token is never accepted as an argument (it would reach shell history and process lists): pipe it to standard input.\n%s", invitationUsage)
	}
	if ios.Stdin == nil {
		return failf(ExitUsage, "no standard input: pipe the invitation token or link to `hikyo access invitation accept`")
	}
	raw, err := io.ReadAll(io.LimitReader(ios.Stdin, invitationTokenLimit+1))
	if err != nil {
		return failf(ExitUsage, "reading the invitation token: %v", err)
	}
	token, linkOrigin := invitationTokenFromInput(string(raw))
	if token == "" || len(raw) > invitationTokenLimit {
		return failf(ExitUsage, "standard input held no invitation token: pipe the token, or the whole link ending in #token=...")
	}
	client, artifact, err := authenticatedClient(st, ios, flags)
	if err != nil {
		return err
	}
	session, err := requireHumanSession("hikyo access invitation accept", artifact)
	if err != nil {
		return err
	}
	// A link from another origin would hand its token to this instance (or a
	// look-alike), so it is refused before anything is sent.
	if linkOrigin != "" {
		want, perr := url.Parse(client.Entry.Origin)
		if perr != nil || linkOrigin != normalizedOrigin(want) {
			return failf(ExitUsage, "the invitation link does not belong to instance %q (%s): nothing was sent. Open the link in a browser, select the instance it points at with --instance, or pipe only the token after #token=",
				session.Instance, client.Entry.Origin)
		}
	}

	const invalid = "this invitation is not valid for this account or has expired"
	body := apigen.EmailInvitationTokenRequest{Token: token}
	var preview apigen.EmailInvitationPreview
	if err := client.Do(ctx, http.MethodPost, api.PathPrefix+"/me/invitations/preview", body, &preview); err != nil {
		return invitationNotFound(err, invalid)
	}
	warning := "Accepting may sign you out on every device and end your developer credentials."
	summary := fmt.Sprintf("Organisation: %s\nTemplate: %s\nExpires: %s\n%s",
		preview.OrgName, preview.Template, preview.ExpiresAt.UTC().Format(time.RFC3339), warning)
	if !yes {
		// Standard input carries the token, so the answer comes from the
		// controlling terminal; with none, --yes is the only way through.
		terminal, err := ios.terminalSession()
		if err != nil {
			return failf(ExitRefused, "accepting needs a confirmation and there is no terminal to ask on: pass --yes to proceed")
		}
		ok, err := terminal.ConfirmEnumerated(summary + "\nAccept")
		if err != nil {
			return failf(ExitRefused, "reading the confirmation: %v", err)
		}
		if !ok {
			return failf(ExitRefused, "invitation not accepted")
		}
	} else {
		fmt.Fprintln(ios.Stderr, summary)
	}

	var out apigen.EmailInvitationAcceptance
	var cleanupErr error
	if err := client.Do(ctx, http.MethodPost, api.PathPrefix+"/me/invitations/accept", body, &out); err != nil {
		return invitationNotFound(err, invalid)
	}
	if out.SignedOut {
		// The server ended every session, this one included: drop the dead
		// local artifact the way `hikyo logout` does. The notice comes before
		// the result so a failed render cannot hide it.
		cleanupErr = st.DeleteSession(session.Instance)
		fmt.Fprintf(ios.Stderr, "You were signed out on every device and your developer credentials ended. Run `hikyo login` again.\n")
	}
	if err := Render(ios.Stdout, f, Table{
		Columns: []string{"ORGANISATION", "TEMPLATE", "GRANTS CREATED"},
		Rows:    [][]string{{out.OrgName, string(out.Template), fmt.Sprint(out.GrantsCreated)}},
		JSON:    out,
	}); err != nil {
		// Acceptance already committed: keep a cleanup failure visible too.
		return errors.Join(err, cleanupFailure(cleanupErr))
	}
	// Acceptance already committed, so a failed local cleanup is reported
	// after the result rather than instead of it.
	return cleanupFailure(cleanupErr)
}

func cleanupFailure(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("the invitation was accepted, but removing the local session failed (run `hikyo logout`): %w", err)
}

// invitationNotFound collapses the server's uniform 404 into one generic
// message, so the CLI adds no distinction the server withholds.
func invitationNotFound(err error, message string) error {
	var ce *Error
	if errors.As(err, &ce) && ce.Code == ExitNotFound {
		return failf(ExitNotFound, "%s", message)
	}
	return err
}
