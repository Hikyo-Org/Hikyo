package cli

// prepareDEKTarget validates a DEK command's parsed selectors, resolves its key
// scope, then authenticates and echoes the target. Malformed usage is rejected
// before context, pin-file, trust or session lookup can change the error.
func prepareDEKTarget(st *State, ios IO, flags commonFlags, instanceScope bool) (*Client, string, string, error) {
	verb := string(flags.operation)
	if err := flags.checkNoPositionals(verb); err != nil {
		return nil, "", "", err
	}
	if flags.provided["env"] || flags.Env != "" {
		return nil, "", "", failf(ExitUsage,
			"hikyo %s: --env cannot select a DEK; use --org/--project for the entire project or --instance-scope", verb)
	}
	for _, selector := range []struct{ name, value string }{
		{"instance", flags.Instance}, {"context", flags.Context},
		{"org", flags.Org}, {"project", flags.Project},
	} {
		if flags.provided[selector.name] && selector.value == "" {
			return nil, "", "", failf(ExitUsage, "hikyo %s: --%s must not be empty; omit it to use inherited selection", verb, selector.name)
		}
	}
	if instanceScope && (flags.Org != "" || flags.Project != "") {
		return nil, "", "", failf(ExitUsage,
			"hikyo %s: --instance-scope and --org/--project name two different scopes; choose one", verb)
	}

	resolved, err := Resolve(st, ios.Env, flags.Flags, ios.Workdir)
	if err != nil {
		return nil, "", "", err
	}
	// --instance remains the target reference. Only --instance-scope selects
	// the instance DEK, ignoring any inherited tenant dimensions.
	var org, project string
	if !instanceScope {
		org, err = resolved.Require(DimOrg)
		if err != nil {
			return nil, "", "", err
		}
		project, err = resolved.Require(DimProject)
		if err != nil {
			return nil, "", "", err
		}
	}
	client, _, _, err := authenticatedResolvedTarget(st, ios, flags, resolved)
	if err != nil {
		return nil, "", "", err
	}
	return client, org, project, nil
}
