package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// `hikyo access registration show|set|delete` (#606, api-cli-spellings
// section 8): the registration policy of one scope, `--org O` or
// `--instance-scope`. `set` is a full replacement read from a JSON file and
// makes the caller the policy's authority; `set` and `delete` are
// reauthentication-gated, so the proof is prompted for, never a flag. Sign-up
// itself has no verb: it is a browser ceremony (#596).

func runAccessRegistration(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("access registration", args, "show", "set", "delete")
	if err != nil {
		return err
	}
	var format, file string
	var instanceScope bool
	st, flags, err := parseCommon("access registration "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.BoolVar(&instanceScope, "instance-scope", false, "address the instance policy rather than an org's")
		if sub == "show" {
			fs.StringVar(&format, "o", "table", "output format: table or json")
		}
		if sub == "set" {
			fs.StringVar(&file, "file", "", "the policy JSON (a full replacement)")
		}
	})
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("access registration " + sub); err != nil {
		return err
	}
	var f Format
	if sub == "show" {
		if f, err = ParseFormat(format); err != nil {
			return err
		}
	}
	var body apigen.RegistrationPolicyPutRequest
	if sub == "set" {
		if file == "" {
			return failf(ExitUsage, "usage: hikyo access registration set [--org O | --instance-scope] --file <policy.json>")
		}
		if body, err = readRegistrationPolicyFile(file); err != nil {
			return err
		}
	}
	resolved, err := Resolve(st, ios.Env, flags.Flags, ios.Workdir)
	if err != nil {
		return err
	}
	scope, err := resolveAccessScope(resolved, flags, instanceScope, "access registration "+sub)
	if err != nil {
		return err
	}
	// A policy belongs to an organisation or to the instance; a project has
	// no sign-up of its own.
	if strings.Contains(scope.path, "/projects/") {
		return failf(ExitUsage,
			"hikyo access registration addresses an organisation (--org) or the instance (--instance-scope): a project has no registration policy")
	}
	path := strings.TrimSuffix(scope.path, "/grants") + "/registration-policy"
	client, _, _, err := authenticatedResolvedTarget(st, ios, flags, resolved)
	if err != nil {
		return err
	}
	switch sub {
	case "show":
		var policy apigen.RegistrationPolicy
		if err := client.Do(ctx, http.MethodGet, path, nil, &policy); err != nil {
			return err
		}
		return Render(ios.Stdout, f, registrationTable(policy))
	case "set":
		proof, err := ios.readPassword("Account-security proof (your TOTP code, or password if no factor): ")
		if err != nil {
			return err
		}
		body.Proof = &proof
		var policy apigen.RegistrationPolicy
		if err := client.Do(ctx, http.MethodPut, path, body, &policy); err != nil {
			return err
		}
		fmt.Fprintf(ios.Stderr, "registration policy saved at %s; you are its authority (%s)\n", scope.label, policy.State)
		return Render(ios.Stdout, FormatTable, registrationTable(policy))
	default:
		proof, err := ios.readPassword("Account-security proof (your TOTP code, or password if no factor): ")
		if err != nil {
			return err
		}
		if err := client.Do(ctx, http.MethodDelete, path, apigen.RegistrationPolicyDeleteRequest{Proof: &proof}, nil); err != nil {
			return err
		}
		fmt.Fprintf(ios.Stderr, "registration closed at %s\n", scope.label)
		return nil
	}
}

// readRegistrationPolicyFile parses the policy file strictly: unknown members
// are refused, and so is a proof, which is prompted for and never stored.
func readRegistrationPolicyFile(path string) (apigen.RegistrationPolicyPutRequest, error) {
	var body apigen.RegistrationPolicyPutRequest
	raw, err := os.ReadFile(path)
	if err != nil {
		return body, failf(ExitUsage, "reading the registration policy %s: %v", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return body, failf(ExitUsage, "the registration policy %s is not a policy document: %v", path, err)
	}
	if dec.More() {
		return body, failf(ExitUsage, "the registration policy %s holds more than one JSON document", path)
	}
	if body.Proof != nil {
		return body, failf(ExitUsage, "the registration policy %s carries a proof: the proof is prompted for, never read from a file", path)
	}
	if body.External == nil {
		body.External = []apigen.RegistrationExternalEntry{}
	}
	return body, nil
}

func registrationTable(p apigen.RegistrationPolicy) Table {
	state := string(p.State)
	if p.InactiveCause != nil {
		state += ": " + string(*p.InactiveCause)
		if p.InactivePrecondition != nil {
			state += " (" + *p.InactivePrecondition + ")"
		}
	}
	landing := string(p.Landing.Kind)
	if p.Landing.Template != nil {
		landing += " " + string(*p.Landing.Template)
	}
	if p.Landing.Cap != nil {
		count := "0"
		if p.FreshOrgCount != nil {
			count = strconv.Itoa(*p.FreshOrgCount)
		}
		landing += " " + count + " / " + strconv.Itoa(*p.Landing.Cap)
	}
	rows := [][]string{
		{"id", p.Id},
		{"state", state},
		{"authority", p.AuthorityPrincipalId},
		{"landing", landing},
	}
	for _, e := range p.External {
		entry := string(e.Provider.Kind) + ":" + e.Provider.Slug
		if e.Claim != nil && e.Values != nil {
			entry += " " + *e.Claim + " in {" + strings.Join(*e.Values, ", ") + "}"
		}
		rows = append(rows, []string{"external", entry})
	}
	if p.Local != nil {
		domains := "any address"
		if p.Local.Domains != nil && len(*p.Local.Domains) > 0 {
			domains = strings.Join(*p.Local.Domains, ", ")
		}
		rows = append(rows, []string{"local", domains})
	}
	return Table{Columns: []string{"FIELD", "VALUE"}, Rows: rows, JSON: p}
}
