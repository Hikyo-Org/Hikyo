package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// `hikyo file-target` administers generic file destinations (#164): a
// pull-class target binding one environment, one key selection and one
// workload service account. Nothing here names a host path; the destination
// lives only in the client's hikyo-file-sync.yaml.

func runFileTarget(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("file-target", args, "create", "list", "show", "update", "delete")
	if err != nil {
		return err
	}
	var format, name, serviceAccount, keys string
	var expected int64
	var selection adapterKeySelection
	st, flags, err := parseCommon("file-target "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "create" {
			fs.StringVar(&name, "name", "", "target name, unique in the environment")
			fs.StringVar(&serviceAccount, "sa", "", "workload service account to bind")
		}
		if sub == "update" {
			fs.Int64Var(&expected, "expected-generation", 0, "the generation being replaced")
		}
		if sub == "create" || sub == "update" {
			fs.StringVar(&keys, "keys", "", "comma-separated immutable key ids")
			selection.flags(fs)
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
	case "show", "update", "delete":
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "file-target %s takes one positional target", sub)
		}
	default:
		if err := flags.checkNoPositionals("file-target " + sub); err != nil {
			return err
		}
	}
	if (sub == "create" || sub == "update") && splitAdapterKeys(keys) == nil && selection.empty() {
		return failf(ExitUsage, "file-target %s requires keys via --keys, --names, --include or --classification", sub)
	}
	if sub == "create" && (name == "" || serviceAccount == "") {
		return failf(ExitUsage, "file-target create requires --name and --sa")
	}
	if sub == "update" && expected <= 0 {
		return failf(ExitUsage, "file-target update requires --expected-generation")
	}
	client, _, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	org, err := resolved.Require(DimOrg)
	if err != nil {
		return err
	}
	project, err := resolved.Require(DimProject)
	if err != nil {
		return err
	}
	base := adapterBase(org, project) + "/file-targets"
	var ids *[]apigen.ID
	if list := splitAdapterKeys(keys); list != nil {
		converted := make([]apigen.ID, len(list))
		for i, id := range list {
			converted[i] = apigen.ID(id)
		}
		ids = &converted
	}
	switch sub {
	case "list":
		var out apigen.FileTargetList
		if err := client.Do(ctx, http.MethodGet, base, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, fileTargetTable(out.Items, out))
	case "create":
		env, err := resolved.Require(DimEnv)
		if err != nil {
			return err
		}
		body := apigen.CreateFileTargetRequest{
			EnvironmentId: apigen.ID(env), Name: name, ServiceAccountId: apigen.ID(serviceAccount),
			KeyIds: ids, KeySelection: selection.body(),
		}
		var out apigen.FileTarget
		if err := client.Do(ctx, http.MethodPost, base, body, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, fileTargetTable([]apigen.FileTarget{out}, out))
	case "show":
		var out apigen.FileTarget
		if err := client.Do(ctx, http.MethodGet, base+"/"+url.PathEscape(flags.positional()), nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, fileTargetTable([]apigen.FileTarget{out}, out))
	case "update":
		body := apigen.UpdateFileTargetRequest{ExpectedGeneration: expected, KeyIds: ids, KeySelection: selection.body()}
		var out apigen.FileTarget
		if err := client.Do(ctx, http.MethodPatch, base+"/"+url.PathEscape(flags.positional()), body, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, fileTargetTable([]apigen.FileTarget{out}, out))
	case "delete":
		if err := client.Do(ctx, http.MethodDelete, base+"/"+url.PathEscape(flags.positional()), nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(ios.Stderr, "deleted file target %s; the destination host keeps its last generation\n", flags.positional())
		return nil
	}
	return nil
}

func fileTargetTable(items []apigen.FileTarget, raw any) Table {
	rows := make([][]string, 0, len(items))
	for _, t := range items {
		names := make([]string, 0, len(t.Keys))
		for _, k := range t.Keys {
			names = append(names, k.Name)
		}
		state, revision, reported := "never", "", ""
		if r := t.Report; r != nil {
			state = string(r.State)
			revision = fmt.Sprintf("%d (generation %d)", r.Revision, r.Generation)
			reported = r.ReportedAt.UTC().Format(time.RFC3339)
		}
		rows = append(rows, []string{
			string(t.Id), t.Name, string(t.EnvironmentId), string(t.ServiceAccountId),
			fmt.Sprint(t.Generation), strings.Join(names, ","), state, revision, reported,
		})
	}
	return Table{
		Columns: []string{"ID", "NAME", "ENVIRONMENT", "SERVICE ACCOUNT", "GENERATION", "KEYS", "REPORTED", "APPLIED REVISION", "REPORTED AT"},
		Rows:    rows, JSON: raw,
	}
}
