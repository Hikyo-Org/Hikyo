package cli

import (
	"context"
	"flag"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// `hikyo access rule list|add|remove` (member-access-rules ADR). A rule is one
// capability for one person with a where of projects, environments and keys;
// an edit is `remove` plus `add`.
//
// Spelling: --org/--project/--env are the scope flags every verb shares, so
// the rule's own axes take distinct names. --in-project (repeatable) names
// the rule's projects and defaults to the resolved project. Environments are
// --only-env or --except-env, keys --only-folder/--only-key or
// --except-folder/--except-key; one axis takes only-items or except-items,
// never both. An environment or key id belongs to one project: qualify it as
// PROJECT:ID when the rule names several. A folder applies to every project
// of the rule unless qualified the same way.

// ruleAxisFlags collects one axis's only- and except-items.
type ruleAxisFlags struct{ only, except stringList }

func (a ruleAxisFlags) mode() (apigen.RuleAxisMode, []string) {
	if len(a.only) > 0 {
		return apigen.RuleAxisModeOnly, a.only
	}
	return apigen.RuleAxisModeAll, a.except
}

func runAccessRule(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("access rule", args, "list", "add", "remove")
	if err != nil {
		return err
	}
	var format, principal, capability string
	var projects stringList
	var envs, folders, keys ruleAxisFlags
	st, flags, err := parseCommon("access rule "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "add" {
			fs.StringVar(&principal, "principal", "", "the person receiving the rule")
			fs.StringVar(&capability, "capability", "", "the capability the rule carries")
			fs.Var(&projects, "in-project", "repeatable: a project the rule covers (default: the resolved project)")
			fs.Var(&envs.only, "only-env", "repeatable: cover only this environment ([PROJECT:]ENV)")
			fs.Var(&envs.except, "except-env", "repeatable: cover every environment except this one ([PROJECT:]ENV)")
			fs.Var(&folders.only, "only-folder", "repeatable: cover only keys in this folder ([PROJECT:]PATH)")
			fs.Var(&folders.except, "except-folder", "repeatable: cover every key except this folder and its subfolders ([PROJECT:]PATH)")
			fs.Var(&keys.only, "only-key", "repeatable: cover only this key ([PROJECT:]KEY)")
			fs.Var(&keys.except, "except-key", "repeatable: cover every key except this one ([PROJECT:]KEY)")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	// Syntax before resolution and before any session lookup.
	if sub == "remove" {
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "usage: hikyo access rule remove <rule>")
		}
	} else if err := flags.checkNoPositionals("access rule " + sub); err != nil {
		return err
	}
	if sub == "add" {
		switch {
		case principal == "" || capability == "":
			return failf(ExitUsage, "usage: hikyo access rule add --principal <id> --capability <atom> [--in-project P]... [--only-env|--except-env E]... [--only-folder|--except-folder F]... [--only-key|--except-key K]...")
		case len(envs.only) > 0 && len(envs.except) > 0:
			return failf(ExitUsage, "hikyo access rule add: --only-env and --except-env are two readings of one axis; choose one")
		case (len(folders.only) > 0 || len(keys.only) > 0) && (len(folders.except) > 0 || len(keys.except) > 0):
			return failf(ExitUsage, "hikyo access rule add: --only-folder/--only-key and --except-folder/--except-key are two readings of one axis; choose one")
		}
	}

	resolved, err := Resolve(st, ios.Env, flags.Flags, ios.Workdir)
	if err != nil {
		return err
	}
	org, err := resolved.Require(DimOrg)
	if err != nil {
		return err
	}
	base := api.PathPrefix + "/orgs/" + url.PathEscape(org)

	var body apigen.CreateRuleRequest
	if sub == "add" {
		if len(projects) == 0 {
			project, err := resolved.Require(DimProject)
			if err != nil {
				return err
			}
			projects = stringList{project}
		}
		body, err = ruleRequest(principal, capability, projects, envs, folders, keys)
		if err != nil {
			return err
		}
	}
	client, artifact, _, err := authenticatedResolvedTarget(st, ios, flags, resolved)
	if err != nil {
		return err
	}

	switch sub {
	case "list":
		path := base + "/rules"
		if project := resolved.Get(DimProject); project != "" {
			path = base + "/projects/" + url.PathEscape(project) + "/rules"
		}
		var list apigen.RuleList
		if err := client.Do(ctx, http.MethodGet, path, nil, &list); err != nil {
			return err
		}
		return Render(ios.Stdout, f, ruleTable(list))
	case "add":
		if capability == "reveal" {
			if _, err := requireHumanSession("hikyo access rule add --capability reveal", artifact); err != nil {
				return err
			}
		}
		var rule apigen.Rule
		if err := client.Do(ctx, http.MethodPost, base+"/rules", body, &rule); err != nil {
			return err
		}
		return Render(ios.Stdout, f, ruleTable(apigen.RuleList{Items: []apigen.Rule{rule}, Count: 1}))
	default: // remove
		return client.Do(ctx, http.MethodDelete, base+"/rules/"+url.PathEscape(flags.positional()), nil, nil)
	}
}

// ruleRequest builds the create body from the flags, binding every item to
// one of the rule's projects.
func ruleRequest(principal, capability string, projects []string, envs, folders, keys ruleAxisFlags) (apigen.CreateRuleRequest, error) {
	where := apigen.RuleWhere{
		Projects:     slices.Clone(projects),
		Environments: apigen.RuleEnvironmentAxis{Items: []apigen.RuleEnvironmentItem{}},
		Keys:         apigen.RuleKeyAxis{Items: []apigen.RuleKeyItem{}},
	}
	var items []string
	where.Environments.Mode, items = envs.mode()
	for _, v := range items {
		project, id, err := ruleItemProject(v, projects, "--only-env/--except-env")
		if err != nil {
			return apigen.CreateRuleRequest{}, err
		}
		where.Environments.Items = append(where.Environments.Items, apigen.RuleEnvironmentItem{Project: project, Environment: id})
	}
	folderMode, folderItems := folders.mode()
	keyMode, keyItems := keys.mode()
	where.Keys.Mode = apigen.RuleAxisModeAll
	if folderMode == apigen.RuleAxisModeOnly || keyMode == apigen.RuleAxisModeOnly {
		where.Keys.Mode = apigen.RuleAxisModeOnly
	}
	for _, v := range folderItems {
		targets := projects
		path := v
		if i := strings.Index(v, ":"); i > 0 {
			switch {
			case slices.Contains(projects, v[:i]):
				targets, path = []string{v[:i]}, v[i+1:]
			case strings.HasPrefix(v[:i], "prj_"):
				// A mistyped or uncovered project qualifier must not become a
				// literal folder name: under --except-folder that would match
				// nothing and silently leave the folder reachable.
				return apigen.CreateRuleRequest{}, failf(ExitUsage,
					"hikyo access rule add: --only-folder/--except-folder %q names project %q, which the rule does not cover", v, v[:i])
			}
		}
		for _, project := range targets {
			folder := path
			where.Keys.Items = append(where.Keys.Items, apigen.RuleKeyItem{Project: project, Folder: &folder})
		}
	}
	for _, v := range keyItems {
		project, id, err := ruleItemProject(v, projects, "--only-key/--except-key")
		if err != nil {
			return apigen.CreateRuleRequest{}, err
		}
		where.Keys.Items = append(where.Keys.Items, apigen.RuleKeyItem{Project: project, Key: &id})
	}
	return apigen.CreateRuleRequest{Principal: principal, Capability: apigen.RuleCapability(capability), Where: where}, nil
}

// ruleItemProject resolves an environment or key item to its project: a
// PROJECT:ID qualifier, or the rule's only project.
func ruleItemProject(v string, projects []string, flagName string) (string, string, error) {
	if i := strings.Index(v, ":"); i > 0 {
		if !slices.Contains(projects, v[:i]) {
			return "", "", failf(ExitUsage, "hikyo access rule add: %s %q names project %q, which the rule does not cover", flagName, v, v[:i])
		}
		return v[:i], v[i+1:], nil
	}
	if len(projects) != 1 {
		return "", "", failf(ExitUsage, "hikyo access rule add: %s %q must be qualified as PROJECT:ID when the rule covers several projects", flagName, v)
	}
	return projects[0], v, nil
}

var ruleColumns = []string{"ID", "PRINCIPAL", "CAPABILITY", "PROJECTS", "ENVIRONMENTS", "KEYS"}

func ruleTable(list apigen.RuleList) Table {
	rows := make([][]string, 0, len(list.Items))
	for _, r := range list.Items {
		w := r.Where
		multi := len(w.Projects) > 1
		qualify := func(project, v string) string {
			if multi {
				return project + ":" + v
			}
			return v
		}
		projects := strings.Join(w.Projects, ", ")
		if r.OtherProjects {
			projects += " (and other projects)"
		}
		envItems := make([]string, 0, len(w.Environments.Items))
		for _, it := range w.Environments.Items {
			envItems = append(envItems, qualify(it.Project, it.Environment))
		}
		keyItems := make([]string, 0, len(w.Keys.Items))
		for _, it := range w.Keys.Items {
			switch {
			case it.Folder != nil && *it.Folder == "":
				keyItems = append(keyItems, qualify(it.Project, "folder:(root)"))
			case it.Folder != nil:
				keyItems = append(keyItems, qualify(it.Project, "folder:"+*it.Folder))
			case it.Key != nil:
				keyItems = append(keyItems, qualify(it.Project, "key:"+*it.Key))
			}
		}
		principal := r.PrincipalId
		if r.PrincipalName != nil && *r.PrincipalName != "" {
			principal += " (" + *r.PrincipalName + ")"
		}
		rows = append(rows, []string{
			r.Id, principal, string(r.Capability), projects,
			ruleAxisText(w.Environments.Mode, envItems), ruleAxisText(w.Keys.Mode, keyItems),
		})
	}
	return Table{Columns: ruleColumns, Rows: rows, JSON: list}
}

func ruleAxisText(mode apigen.RuleAxisMode, items []string) string {
	switch {
	case mode == apigen.RuleAxisModeOnly:
		return "only " + strings.Join(items, ", ")
	case len(items) == 0:
		return "all"
	default:
		return "all except " + strings.Join(items, ", ")
	}
}
