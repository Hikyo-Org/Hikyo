package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
)

// The personal value verbs (#1235, personal-values ADR): a developer's own
// value for a declared key in one environment, delivered only to that
// developer's own credential. The value never reaches argv, exactly as for
// `values set`; a reveal goes through the print triad.

var personalColumns = []string{"KEY", "PRESENCE", "UPDATED", "NOTICE", "STATUS"}

// personalSetStatement is printed before `personal set` reads or sends a value
// (personal-values ADR, What this guarantee is).
const personalSetStatement = "Hikyo does not show this to administrators. Your instance operator could read it, " +
	"and anyone who can change shared values here could capture it once your program runs. " +
	"If someone gave you your password, authenticator or recovery codes, replace them, " +
	"regenerate your recovery codes and check your linked identities first."

func runPersonal(ctx context.Context, ios IO, args []string) (returnErr error) {
	sub, rest, err := subverb("personal", args, "list", "get", "set", "members", "counts", "offboarding-report")
	if err != nil {
		return err
	}
	var format, valueFile, member, outputFile string
	var stdin, clear, reveal, dangerous bool
	st, flags, err := parseCommon("personal "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		switch sub {
		case "get":
			fs.BoolVar(&reveal, "reveal", false, "disclose your own value; audited, and refused without the reveal capability")
			fs.StringVar(&outputFile, "output-file", "", "write the revealed value to a file this command creates (0600)")
			fs.BoolVar(&dangerous, "dangerously-print", false, "print the revealed value to stdout")
		case "set":
			fs.BoolVar(&stdin, "stdin", false, "read the value from stdin")
			fs.StringVar(&valueFile, "value-file", "", "read the value from a file the caller names")
			fs.BoolVar(&clear, "clear", false, "delete your value (or, with --member, another person's)")
			fs.StringVar(&member, "member", "", "with --clear: the username or principal id whose value to delete")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	switch sub {
	case "list", "counts":
		if err := flags.checkNoPositionals("personal " + sub); err != nil {
			return err
		}
	case "offboarding-report":
		if err := flags.checkTarget("personal offboarding-report", "member", ""); err != nil {
			return err
		}
		if flags.positional() == "" {
			return failf(ExitUsage, "usage: hikyo personal offboarding-report <NAME>")
		}
	default:
		if err := flags.checkTarget("personal "+sub, "key", ""); err != nil {
			return err
		}
		if flags.positional() == "" {
			return failf(ExitUsage, "usage: hikyo personal %s <KEY>", sub)
		}
	}
	switch {
	case sub == "get" && !reveal:
		return failf(ExitUsage, "hikyo personal get requires --reveal; hikyo personal list shows presence")
	case sub == "set" && clear && (stdin || valueFile != ""):
		return failf(ExitUsage, "hikyo personal set --clear takes no value")
	case sub == "set" && member != "" && !clear:
		return failf(ExitUsage, "hikyo personal set --member requires --clear: no one sets another person's value")
	}
	var value string
	if sub == "set" && !clear {
		fmt.Fprintln(ios.Stderr, personalSetStatement)
		if value, err = readValue(ios, stdin, valueFile, flags.positional()); err != nil {
			return err
		}
	}

	client, artifact, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	project, err := projectBase(resolved)
	if err != nil {
		return err
	}
	if sub == "offboarding-report" {
		return personalOffboardingReport(ctx, ios, client, project, flags.positional(), f)
	}
	env, err := addressed(resolved, DimEnv, flags.Env, "personal "+sub)
	if err != nil {
		return err
	}
	base := project + "/environments/" + url.PathEscape(env) + "/personal-values"
	key := flags.positional()
	target := base + "/" + url.PathEscape(key)
	// Set and reveal take the same per-key reauthentication ceremony the server
	// gates them behind; the unit is this one key, whatever its classification.
	ceremony := func(attempt func() error) error {
		d := disclosure{purpose: "reveal", keys: func(ctx context.Context, env string) ([]string, error) {
			return declaredKeyIDs(ctx, client, project, env, key)
		}}
		return withRevealCeremony(ctx, client, st, ios, artifact, project, []string{env}, d, attempt)
	}

	switch sub {
	case "counts":
		var counts apigen.PersonalValueCounts
		if err := client.Do(ctx, http.MethodGet, project+"/environments/"+url.PathEscape(env)+"/personal-value-counts", nil, &counts); err != nil {
			return err
		}
		t := Table{Columns: []string{"KEY", "HOLDERS", "NOTICES", "REQUIRED"}, JSON: counts}
		for _, c := range counts.Items {
			holders := strconv.Itoa(c.Holders)
			if c.Required {
				holders = fmt.Sprintf("%d/%d", c.Holders, counts.Eligible)
			}
			required := "no"
			if c.Required {
				required = "yes"
			}
			t.Rows = append(t.Rows, []string{c.Name, holders, strconv.Itoa(c.Notices), required})
		}
		if f == FormatTable {
			fmt.Fprintf(ios.Stderr, "eligible here (read and reveal): %d\n", counts.Eligible)
		}
		return Render(ios.Stdout, f, t)

	case "list":
		var list apigen.PersonalValueList
		if err := client.Do(ctx, http.MethodGet, base, nil, &list); err != nil {
			return err
		}
		t := personalTable(list.Items)
		t.JSON = list
		personalWarnings(ios, list.Items...)
		return Render(ios.Stdout, f, t)

	case "get":
		var entry apigen.PersonalValueEntry
		if err := ceremony(func() error {
			return client.Do(ctx, http.MethodPost, target+"/reveal", nil, &entry)
		}); err != nil {
			return err
		}
		sink, err := ios.prepareDisclosure(disclose.Options{OutputFile: outputFile, DangerouslyPrint: dangerous, Stdout: ios.Stdout})
		if err != nil {
			return failf(ExitRefused, "the value has nowhere to go: %v", err)
		}
		defer sink.AbortOnReturn(&returnErr)
		plain := ""
		if entry.Value != nil {
			plain = *entry.Value
		}
		if _, err := sink.WriteOnce("personal value of "+entry.Name, plain); err != nil {
			return failf(ExitRefused, "disclosing the value: %v", err)
		}
		return nil

	case "set":
		if clear && member != "" {
			if err := client.Do(ctx, http.MethodDelete, target+"/members/"+url.PathEscape(member), nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(ios.Stderr, "deleted %s's personal value for %s; they are left a notice\n", member, key)
			return nil
		}
		if clear {
			if err := client.Do(ctx, http.MethodDelete, target, nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(ios.Stderr, "deleted your personal value for %s; the shared value applies again\n", key)
			return nil
		}
		var entry apigen.PersonalValueEntry
		if err := ceremony(func() error {
			return client.Do(ctx, http.MethodPut, target, apigen.SetPersonalValueRequest{Value: value}, &entry)
		}); err != nil {
			return err
		}
		t := personalTable([]apigen.PersonalValueEntry{entry})
		t.JSON = entry
		personalWarnings(ios, entry)
		return Render(ios.Stdout, f, t)

	case "members":
		var list apigen.PersonalValueHolderList
		if err := client.Do(ctx, http.MethodGet, target+"/members", nil, &list); err != nil {
			return err
		}
		t := Table{Columns: []string{"MEMBER", "PRESENCE", "UPDATED", "NOTICE"}, JSON: list}
		for _, h := range list.Items {
			who := h.PrincipalId
			if h.Username != nil {
				who = *h.Username
			}
			t.Rows = append(t.Rows, []string{who, presenceWord(h.Set), personalTime(h.UpdatedAt), personalNotice(h.Notice)})
		}
		return Render(ios.Stdout, f, t)
	}
	return nil
}

// personalOffboardingReport prints what to rotate after someone leaves: the
// shared keys they are recorded as receiving and the service accounts they
// created, names only. The evidence window and the report's limits go to
// stderr so the table stays parseable.
func personalOffboardingReport(ctx context.Context, ios IO, client *Client, project, member string, f Format) error {
	var report apigen.PersonalOffboardingReport
	if err := client.Do(ctx, http.MethodGet, project+"/personal-offboarding-report/"+url.PathEscape(member), nil, &report); err != nil {
		return err
	}
	t := Table{Columns: []string{"KIND", "NAME", "ENVIRONMENT", "AT", "CHANGED SINCE"}, JSON: report}
	for _, x := range report.Received {
		env := x.EnvironmentName
		if env == "" {
			env = x.EnvironmentId
		}
		changed := "no"
		if x.ChangedSince {
			changed = "yes"
		}
		t.Rows = append(t.Rows, []string{"received", x.KeyName, env, personalTime(&x.LastReceivedAt), changed})
	}
	for _, sa := range report.ServiceAccounts {
		t.Rows = append(t.Rows, []string{"service account", sa.Name, "", personalTime(&sa.CreatedAt), ""})
	}
	if f == FormatTable {
		fmt.Fprintf(ios.Stderr, "evidence from %s to %s\n", personalTime(&report.EvidenceFrom), personalTime(&report.GeneratedAt))
		for _, l := range report.Limits {
			fmt.Fprintln(ios.Stderr, "limit: "+l)
		}
	}
	return Render(ios.Stdout, f, t)
}

func personalTable(items []apigen.PersonalValueEntry) Table {
	t := Table{Columns: personalColumns}
	for _, e := range items {
		t.Rows = append(t.Rows, []string{e.Name, presenceWord(e.Set), personalTime(e.UpdatedAt), personalNotice(e.Notice), personalStatus(e)})
	}
	return t
}

// personalStatus names the checklist and suspension state in words.
func personalStatus(e apigen.PersonalValueEntry) string {
	var parts []string
	if e.Required != nil && *e.Required {
		parts = append(parts, "required")
	}
	if e.Suspended != nil && *e.Suspended {
		parts = append(parts, "suspended since "+personalTime(e.SuspendedAt))
	}
	return strings.Join(parts, ", ")
}

// personalWarnings names, on stderr, schema rules an owner's value does not
// satisfy. They never refuse the value.
func personalWarnings(ios IO, entries ...apigen.PersonalValueEntry) {
	for _, e := range entries {
		if e.Warnings == nil {
			continue
		}
		for _, w := range *e.Warnings {
			fmt.Fprintf(ios.Stderr, "warning: your personal value for %s: %s\n", e.Name, w)
		}
	}
}

func personalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func personalNotice(n *apigen.PersonalValueNoticeCause) string {
	if n == nil {
		return ""
	}
	return string(*n)
}

// declaredKeyIDs resolves one declared key's id in env from the non-revealing
// value list, which discloses nothing and names every declared key.
func declaredKeyIDs(ctx context.Context, client *Client, project, env, name string) ([]string, error) {
	var list apigen.ValueList
	if err := client.Do(ctx, http.MethodGet, project+"/environments/"+url.PathEscape(env)+"/values", nil, &list); err != nil {
		return nil, err
	}
	for _, cell := range list.Items {
		if cell.Name == name {
			return []string{string(cell.KeyId)}, nil
		}
	}
	return nil, nil
}
