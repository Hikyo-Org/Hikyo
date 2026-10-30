package cli

import (
	"context"
	"flag"
	"net/http"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/parameters"
)

func runEnvParam(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("env param", args, "list", "add", "delete")
	if err != nil {
		return err
	}
	var name, pattern, format string
	st, flags, err := parseCommon("env param "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&name, "name", "", "parameter name")
		if sub != "delete" {
			fs.StringVar(&pattern, "pattern", "", "whole-value RE2 validation pattern")
		}
		fs.StringVar(&format, "o", "table", "output format: table or json")
	})
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("env param " + sub); err != nil {
		return err
	}
	if sub != "list" && name == "" {
		return failf(ExitUsage, "--name is required")
	}
	if sub == "add" {
		if err := parameters.CheckDeclaration(name, pattern); err != nil {
			return failf(ExitUsage, "%s", err)
		}
	}
	if sub == "delete" {
		if err := parameters.CheckName(name); err != nil {
			return failf(ExitUsage, "%s", err)
		}
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	target, err := prepareScopedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	path := target.base + "/parameters"
	if sub != "list" {
		body := struct {
			Name    string `json:"name"`
			Pattern string `json:"pattern,omitempty"`
			Action  string `json:"action"`
		}{name, pattern, sub}
		return target.client.Do(ctx, http.MethodPost, path, body, nil)
	}
	var declarations map[string]string
	if err := target.client.Do(ctx, http.MethodGet, path, nil, &declarations); err != nil {
		return err
	}
	names := make([]string, 0, len(declarations))
	for name := range declarations {
		names = append(names, name)
	}
	slices.Sort(names)
	rows := make([][]string, 0, len(names))
	for _, name := range names {
		rows = append(rows, []string{name, declarations[name]})
	}
	return Render(ios.Stdout, f, Table{Columns: []string{"NAME", "PATTERN"}, Rows: rows, JSON: declarations})
}

func parameterFlag(values map[string]string) func(string) error {
	return func(raw string) error {
		name, value, ok := strings.Cut(raw, "=")
		if !ok {
			return failf(ExitUsage, "--param requires NAME=value")
		}
		if _, exists := values[name]; exists {
			return failf(ExitUsage, "duplicate parameter %s", name)
		}
		values[name] = value
		return parameters.CheckSupplied(values)
	}
}
