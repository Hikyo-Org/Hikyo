package cli

import (
	"context"
	"flag"
	"net/http"
	"strconv"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func runProjectCapacity(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("project capacity", args, "get", "set")
	if err != nil {
		return err
	}
	var format string
	var maxKeys int
	var inherit bool
	var parsedFlags *flag.FlagSet
	st, flags, err := parseCommon("project capacity "+sub, ios, rest, func(fs *flag.FlagSet) {
		parsedFlags = fs
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "set" {
			fs.IntVar(&maxKeys, "max-keys", 0, "project key limit within the instance maximum")
			fs.BoolVar(&inherit, "inherit", false, "clear the override and follow the instance default")
		}
	})
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("project capacity " + sub); err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	var override *int
	if sub == "set" {
		maxKeysSet := false
		parsedFlags.Visit(func(f *flag.Flag) {
			if f.Name == "max-keys" {
				maxKeysSet = true
			}
		})
		if inherit == maxKeysSet {
			return failf(ExitUsage, "choose exactly one of --inherit or --max-keys")
		}
		if maxKeysSet {
			if maxKeys < 1 || maxKeys > 10000 {
				return failf(ExitUsage, "--max-keys must be between 1 and 10000")
			}
			override = &maxKeys
		}
	}
	client, _, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	base, err := projectBase(resolved)
	if err != nil {
		return err
	}
	path := base + "/capacity"
	var out apigen.ProjectCapacity
	if err := client.Do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return err
	}
	if sub == "set" {
		// Bind the write to the policy just read. A concurrent project or
		// instance policy change returns a conflict for the caller to review;
		// never fetch a new policy and silently retry the write.
		body := apigen.SetProjectCapacityRequest{
			KeyLimitOverride: override, ExpectedRevision: out.Revision,
			ExpectedInstanceDefault: out.InstanceDefault, ExpectedInstanceMaximum: out.InstanceMaximum,
		}
		if err := client.Do(ctx, http.MethodPut, path, body, &out); err != nil {
			return err
		}
	}
	return Render(ios.Stdout, f, projectCapacityTable(out))
}

func projectCapacityTable(policy apigen.ProjectCapacity) Table {
	return Table{Columns: []string{"OVERRIDE", "INSTANCE DEFAULT", "INSTANCE MAXIMUM", "EFFECTIVE LIMIT", "KEYS", "ENVIRONMENTS", "INHERITED", "CAPPED"}, Rows: [][]string{{
		optionalInt(policy.KeyLimitOverride), strconv.Itoa(policy.InstanceDefault), strconv.Itoa(policy.InstanceMaximum),
		strconv.Itoa(policy.EffectiveLimit), strconv.Itoa(policy.KeyCount), strconv.Itoa(policy.EnvironmentCount),
		strconv.FormatBool(policy.Inherited), strconv.FormatBool(policy.Capped),
	}}, JSON: policy}
}
