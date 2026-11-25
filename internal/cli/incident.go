package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// Incident containment (#1039). `incident open` only enumerates what a scope
// leaves behind, as items; `items` shows each one with its outcome, the owner's
// refusal apart from work not yet run, and a rotation's live state; `resume`
// does the work, running the open items, after you confirm the listed items with
// --yes and prove your account again; `attest` records a statement the owner did
// not confirm; `close` ends the ledger. Every verb is human-session only.

const incidentBase = api.PathPrefix + "/incidents"

// runIncident executes the incident verbs. Usage, authentication, HTTP and
// rendering errors are returned to the CLI.
func runIncident(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("incident", args, "open", "show", "items", "resume", "attest", "close")
	if err != nil {
		return err
	}
	var format, scope, account, adapter, after string
	var openOnly, withResiduals, yes bool
	var limit int
	st, flags, err := parseCommon("incident "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		switch sub {
		case "open":
			fs.StringVar(&scope, "scope", "", "what to open the incident on: account, org, project or adapter")
			fs.StringVar(&account, "account", "", "the account principal, for --scope account")
			fs.StringVar(&adapter, "adapter", "", "the deployment adapter, for --scope adapter")
		case "items":
			fs.BoolVar(&openOnly, "open", false, "only items that still need work")
			fs.IntVar(&limit, "limit", 0, "items per page, 1 to 500 (0 = the server default of 100)")
			fs.StringVar(&after, "after", "", "continue after this item id, as a previous page printed it")
		case "resume":
			fs.BoolVar(&yes, "yes", false, "confirm the listed open items and run them (it grants no authority and is no proof)")
		case "close":
			fs.BoolVar(&withResiduals, "with-residuals", false, "close although items are open, recording them as residuals")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	// Syntax before authentication.
	var body any
	switch sub {
	case "open":
		if err := flags.checkNoPositionals("incident open"); err != nil {
			return err
		}
		if body, err = incidentOpenBody(scope, account, adapter, flags); err != nil {
			return err
		}
	case "attest":
		if len(flags.positionals) != 2 {
			return failf(ExitUsage, "usage: hikyo incident attest <incident> <item>")
		}
	default:
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "usage: hikyo incident %s <incident>", sub)
		}
	}
	client, _, _, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	id := url.PathEscape(flags.positional())
	switch sub {
	case "open":
		var out apigen.Incident
		if err := client.Do(ctx, http.MethodPost, incidentBase, body, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, incidentTable(out))
	case "show":
		var out apigen.Incident
		if err := client.Do(ctx, http.MethodGet, incidentBase+"/"+id, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, incidentTable(out))
	case "items":
		query := url.Values{}
		if openOnly {
			query.Set("open_only", "true")
		}
		if limit != 0 {
			query.Set("limit", strconv.Itoa(limit))
		}
		if after != "" {
			query.Set("after", after)
		}
		target := incidentBase + "/" + id + "/items"
		if len(query) > 0 {
			target += "?" + query.Encode()
		}
		var out apigen.IncidentItemList
		if err := client.Do(ctx, http.MethodGet, target, nil, &out); err != nil {
			return err
		}
		if err := Render(ios.Stdout, f, incidentItemTable(out)); err != nil {
			return err
		}
		if out.NextAfter != nil {
			_, _ = fmt.Fprintf(ios.Stderr, "more items may follow; continue with --after %s\n", *out.NextAfter)
		}
		return nil
	case "resume":
		// The operator sees exactly which items resume will run before anything runs:
		// the open items are listed, and without --yes that listing is all that
		// happens. The call that does the work is never sent unconfirmed.
		open, err := incidentOpenItems(ctx, client, id)
		if err != nil {
			return err
		}
		if err := Render(ios.Stderr, FormatTable, incidentItemTable(apigen.IncidentItemList{Items: open})); err != nil {
			return err
		}
		if len(open) == 0 {
			_, err := fmt.Fprintln(ios.Stderr, "no open items: there is nothing to resume")
			return err
		}
		if !yes {
			return failf(ExitRefused, "resume runs the %d open item(s) listed above and cannot be undone; run it again with --yes to confirm", len(open))
		}
		proof, err := ios.readPassword("Account-security proof (your TOTP code, or password if no factor): ")
		if err != nil {
			return err
		}
		var out apigen.IncidentResumeResult
		if err := client.Do(ctx, http.MethodPost, incidentBase+"/"+id+"/resume", incidentResumeRequest(proof, open), &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, Table{
			Columns: []string{"EXAMINED", "CONFIRMED", "REFUSED", "BLOCKED", "UNCONFIRMED", "STALE", "SKIPPED"},
			Rows: [][]string{{strconv.Itoa(int(out.Examined)), strconv.Itoa(int(out.Confirmed)), strconv.Itoa(int(out.Failed)),
				strconv.Itoa(int(out.Blocked)), strconv.Itoa(int(out.Unconfirmed)), strconv.Itoa(int(out.Stale)), strconv.Itoa(int(out.Skipped))}},
			JSON: out,
		})
	case "attest":
		item := url.PathEscape(flags.positionals[1])
		if err := client.Do(ctx, http.MethodPost, incidentBase+"/"+id+"/items/"+item+"/attest", nil, nil); err != nil {
			return err
		}
		_, err := fmt.Fprintf(ios.Stdout, "attested %s: recorded as stated by you, not confirmed by its owner\n", flags.positionals[1])
		return err
	case "close":
		var out apigen.Incident
		req := apigen.IncidentCloseRequest{WithResiduals: &withResiduals}
		if err := client.Do(ctx, http.MethodPost, incidentBase+"/"+id+"/close", req, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, incidentTable(out))
	}
	return failf(ExitInternal, "hikyo incident: unhandled subverb %q", sub)
}

// incidentOpenBody builds the open request from the scope flags and refuses a
// flag that does not belong to the chosen scope before anything is sent.
func incidentOpenBody(scope, account, adapter string, flags commonFlags) (apigen.IncidentOpenRequest, error) {
	body := apigen.IncidentOpenRequest{ScopeKind: scope}
	set := func(dst **string, v string) {
		if v != "" {
			*dst = &v
		}
	}
	switch scope {
	case "account":
		if account == "" || flags.Org != "" || flags.Project != "" || adapter != "" {
			return body, failf(ExitUsage, "incident open --scope account takes --account only")
		}
		set(&body.Principal, account)
	case "org":
		if flags.Org == "" || account != "" || flags.Project != "" || adapter != "" {
			return body, failf(ExitUsage, "incident open --scope org takes --org only")
		}
		set(&body.Org, flags.Org)
	case "project":
		if flags.Org == "" || flags.Project == "" || account != "" || adapter != "" {
			return body, failf(ExitUsage, "incident open --scope project takes --org and --project")
		}
		set(&body.Org, flags.Org)
		set(&body.Project, flags.Project)
	case "adapter":
		if flags.Org == "" || flags.Project == "" || adapter == "" || account != "" {
			return body, failf(ExitUsage, "incident open --scope adapter takes --org, --project and --adapter")
		}
		set(&body.Org, flags.Org)
		set(&body.Project, flags.Project)
		set(&body.Adapter, adapter)
	default:
		return body, failf(ExitUsage, "incident open requires --scope account, org, project or adapter")
	}
	return body, nil
}

// incidentOpenItemPages bounds how many pages the resume confirmation reads, so a
// ledger the surface cannot list in full is refused rather than half confirmed.
const incidentOpenItemPages = 40

// incidentResumeRequest is the resume call for the open items the operator was shown:
// the proof, and the digest of exactly those items, so the server refuses a set
// that changed since.
func incidentResumeRequest(proof string, listed []apigen.IncidentItem) apigen.IncidentResumeRequest {
	ids := make([]string, len(listed))
	for i, it := range listed {
		ids[i] = it.Id
	}
	return apigen.IncidentResumeRequest{Proof: proof, ItemsDigest: api.IncidentItemsDigest(ids)}
}

// incidentOpenItems lists every open item of one incident, page by page.
func incidentOpenItems(ctx context.Context, client *Client, id string) ([]apigen.IncidentItem, error) {
	var all []apigen.IncidentItem
	after := ""
	for page := 0; page < incidentOpenItemPages; page++ {
		query := url.Values{"open_only": {"true"}, "limit": {"500"}}
		if after != "" {
			query.Set("after", after)
		}
		var out apigen.IncidentItemList
		if err := client.Do(ctx, http.MethodGet, incidentBase+"/"+id+"/items?"+query.Encode(), nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Items...)
		if out.NextAfter == nil {
			return all, nil
		}
		after = *out.NextAfter
	}
	return nil, failf(ExitRefused, "more than %d open items: list them with `incident items --open`, attest or resolve some, then resume", len(all))
}

func incidentTable(i apigen.Incident) Table {
	c := i.Counts
	closed := "-"
	if i.ClosedAt != nil {
		closed = i.ClosedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return Table{
		Columns: []string{"ID", "SCOPE", "STATUS", "NOT YET RUN", "REFUSED", "UNCONFIRMED", "BLOCKED", "DONE", "ATTESTED", "OUT OF SCOPE", "CLOSED"},
		Rows: [][]string{{i.Id, i.ScopeKind + ":" + i.ScopeId, i.Status, strconv.Itoa(int(max(c.Pending-c.Refused, 0))), strconv.Itoa(int(c.Refused)), strconv.Itoa(int(c.Unknown)),
			strconv.Itoa(int(c.Blocked)), strconv.Itoa(int(c.Done)), strconv.Itoa(int(c.Attested)), strconv.Itoa(int(c.OutOfScope)), closed}},
		JSON: i,
	}
}

// incidentItemState names an item the way the web does: an item its owner
// refused is a different state from one that has not run yet.
func incidentItemState(it apigen.IncidentItem) string {
	if it.Refused {
		return "refused"
	}
	if it.Outcome == apigen.IncidentItemOutcomePending {
		return "not yet run"
	}
	return string(it.Outcome)
}

func incidentItemTable(list apigen.IncidentItemList) Table {
	rows := make([][]string, 0, len(list.Items))
	for _, it := range list.Items {
		reason, rotation := "-", "-"
		if it.Reason != nil {
			reason = *it.Reason
		}
		if it.ObjectState != nil {
			rotation = *it.ObjectState
		}
		rows = append(rows, []string{it.Id, it.ObjectKind + ":" + it.ObjectId, incidentItemState(it), reason, rotation})
	}
	return Table{Columns: []string{"ID", "OBJECT", "STATE", "REASON", "ROTATION"}, Rows: rows, JSON: list}
}
