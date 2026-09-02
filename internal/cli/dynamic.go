package cli

import (
	"context"
	"flag"
	"net/http"
	"net/url"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
)

func dynamicProviderTable(list apigen.DynamicProviderList) Table {
	rows := make([][]string, 0, len(list.Items))
	for _, p := range list.Items {
		credential := "absent"
		if p.CredentialPresent {
			credential = "present"
		}
		rows = append(rows, []string{string(p.Id), string(p.Kind), p.Origin, p.GrantRole, credential, string(p.State)})
	}
	return Table{
		// No credential column: the admin credential is write-only and never
		// read back.
		Columns: []string{"ID", "KIND", "ORIGIN", "GRANT ROLE", "CREDENTIAL", "STATE"},
		Rows:    rows,
		JSON:    list,
	}
}

func dynamicLeaseTable(list apigen.DynamicLeaseList) Table {
	rows := make([][]string, 0, len(list.Items))
	for _, l := range list.Items {
		expires := "-"
		if l.ExpiresAt != nil {
			expires = l.ExpiresAt.UTC().Format(time.RFC3339)
		}
		rows = append(rows, []string{string(l.Id), l.ProviderHandle, string(l.State), expires, l.PrincipalClass})
	}
	return Table{
		// No secret column: the credential is disclosed once at mint and never
		// stored or read back.
		Columns: []string{"ID", "HANDLE", "STATE", "EXPIRES", "PRINCIPAL"},
		Rows:    rows,
		JSON:    list,
	}
}

func runDynamicProvider(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("dynamic-provider", args, "create", "list", "show", "credential", "delete")
	if err != nil {
		return err
	}
	if sub == "credential" {
		return runDynamicProviderCredential(ctx, ios, rest)
	}
	var format, provider, origin, grantRole, tlsMode string
	var revokeAll bool
	var source adapterCredentialSource
	st, flags, err := parseCommon("dynamic-provider "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "create" {
			fs.StringVar(&provider, "provider", "postgres", "provider kind (postgres)")
			fs.StringVar(&origin, "origin", "", "postgres://user@host:port/db")
			fs.StringVar(&grantRole, "grant-role", "", "parent role every minted lease role inherits")
			fs.StringVar(&tlsMode, "tls-mode", "verify-full", "TLS mode (verify-full)")
			fs.BoolVar(&source.stdin, "stdin", false, "read the admin credential from stdin")
			fs.StringVar(&source.file, "value-file", "", "read the admin credential from a file")
		}
		if sub == "delete" {
			fs.BoolVar(&revokeAll, "revoke-all", false, "revoke every live lease before deleting")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
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
	base := adapterBase(org, project) + "/dynamic-providers"

	switch sub {
	case "list":
		if err := flags.checkNoPositionals("dynamic-provider list"); err != nil {
			return err
		}
		var out apigen.DynamicProviderList
		if err := client.Do(ctx, http.MethodGet, base, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, dynamicProviderTable(out))
	case "show":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "dynamic-provider show requires a provider id")
		}
		var out apigen.DynamicProvider
		if err := client.Do(ctx, http.MethodGet, base+"/"+url.PathEscape(id), nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, dynamicProviderTable(apigen.DynamicProviderList{Items: []apigen.DynamicProvider{out}}))
	case "delete":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "dynamic-provider delete requires a provider id")
		}
		target := base + "/" + url.PathEscape(id)
		if revokeAll {
			target += "?revoke_all=true"
		}
		var out apigen.DynamicProviderDeletion
		if err := client.Do(ctx, http.MethodDelete, target, nil, &out); err != nil {
			return err
		}
		return nil
	case "create":
		if err := flags.checkNoPositionals("dynamic-provider create"); err != nil {
			return err
		}
		if origin == "" || grantRole == "" {
			return failf(ExitUsage, "dynamic-provider create requires --origin and --grant-role")
		}
		credential, err := source.read(ios)
		if err != nil {
			return err
		}
		kind := apigen.DynamicProviderKind(provider)
		mode := apigen.CreateDynamicProviderRequestTlsMode(tlsMode)
		body := apigen.CreateDynamicProviderRequest{
			Kind: kind, Origin: origin, GrantRole: grantRole, TlsMode: &mode,
			Credential: string(credential),
		}
		var out apigen.DynamicProvider
		if err := client.Do(ctx, http.MethodPost, base, body, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, dynamicProviderTable(apigen.DynamicProviderList{Items: []apigen.DynamicProvider{out}}))
	}
	return failf(ExitUsage, "unknown dynamic-provider verb %q", sub)
}

func runDynamicProviderCredential(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("dynamic-provider credential", args, "set", "revoke")
	if err != nil {
		return err
	}
	var provider string
	var source adapterCredentialSource
	st, flags, err := parseCommon("dynamic-provider credential "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&provider, "provider", "", "the provider to modify")
		if sub == "set" {
			fs.BoolVar(&source.stdin, "stdin", false, "read the admin credential from stdin")
			fs.StringVar(&source.file, "value-file", "", "read the admin credential from a file")
		}
	})
	if err != nil {
		return err
	}
	if provider == "" {
		return failf(ExitUsage, "dynamic-provider credential %s requires --provider", sub)
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
	path := adapterBase(org, project) + "/dynamic-providers/" + url.PathEscape(provider) + "/credential"
	if sub == "revoke" {
		return client.Do(ctx, http.MethodDelete, path, nil, nil)
	}
	credential, err := source.read(ios)
	if err != nil {
		return err
	}
	return client.Do(ctx, http.MethodPut, path, apigen.SetDynamicProviderCredentialRequest{Credential: string(credential)}, nil)
}

func runLease(ctx context.Context, ios IO, args []string) (returnErr error) {
	sub, rest, err := subverb("lease", args, "mint", "list", "show", "renew", "revoke", "settle")
	if err != nil {
		return err
	}
	var format, provider, ttl, outputFile string
	var dangerous bool
	st, flags, err := parseCommon("lease "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "mint" {
			fs.StringVar(&provider, "provider", "", "the dynamic provider to mint from")
			fs.StringVar(&ttl, "ttl", "", "maximum lifetime, e.g. 1h")
			fs.StringVar(&outputFile, "output-file", "", "write the credential to a fresh 0600 file")
			fs.BoolVar(&dangerous, "dangerously-print", false, "write the credential to stdout (and whatever collects it)")
		}
		if sub == "renew" {
			fs.StringVar(&ttl, "ttl", "", "new maximum lifetime, e.g. 1h; empty keeps the lease's ceiling")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}

	// Prepare the display-once sink BEFORE any network call, so the reserved
	// destination is the exact destination the credential is written to.
	var sink *disclose.PreparedSink
	if sub == "mint" {
		sink, err = ios.prepareDisclosure(disclose.Options{OutputFile: outputFile, DangerouslyPrint: dangerous, Stdout: ios.Stdout})
		if err != nil {
			return failf(ExitRefused, "the credential has nowhere to go: %v", err)
		}
		defer sink.AbortOnReturn(&returnErr)
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
	env, err := resolved.Require(DimEnv)
	if err != nil {
		return err
	}
	base := adapterBase(org, project) + "/environments/" + url.PathEscape(env) + "/leases"

	switch sub {
	case "list":
		if err := flags.checkNoPositionals("lease list"); err != nil {
			return err
		}
		var out apigen.DynamicLeaseList
		if err := client.Do(ctx, http.MethodGet, base, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, dynamicLeaseTable(out))
	case "show":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "lease show requires a lease id")
		}
		var out apigen.DynamicLease
		if err := client.Do(ctx, http.MethodGet, base+"/"+url.PathEscape(id), nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, dynamicLeaseTable(apigen.DynamicLeaseList{Items: []apigen.DynamicLease{out}}))
	case "renew":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "lease renew requires a lease id")
		}
		var body apigen.RenewLeaseRequest
		if ttl != "" {
			secs, err := ttlSeconds(ttl)
			if err != nil {
				return err
			}
			body.MaxTtlSeconds = &secs
		}
		var out apigen.DynamicLease
		if err := client.Do(ctx, http.MethodPost, base+"/"+url.PathEscape(id)+"/renew", body, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, dynamicLeaseTable(apigen.DynamicLeaseList{Items: []apigen.DynamicLease{out}}))
	case "revoke":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "lease revoke requires a lease id")
		}
		var out apigen.DynamicLease
		if err := client.Do(ctx, http.MethodPost, base+"/"+url.PathEscape(id)+"/revoke", nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, dynamicLeaseTable(apigen.DynamicLeaseList{Items: []apigen.DynamicLease{out}}))
	case "settle":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "lease settle requires a lease id")
		}
		var out apigen.DynamicLease
		if err := client.Do(ctx, http.MethodPost, base+"/"+url.PathEscape(id)+"/settle", nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, dynamicLeaseTable(apigen.DynamicLeaseList{Items: []apigen.DynamicLease{out}}))
	case "mint":
		if err := flags.checkNoPositionals("lease mint"); err != nil {
			return err
		}
		if provider == "" || ttl == "" {
			return failf(ExitUsage, "lease mint requires --provider and --ttl")
		}
		secs, err := ttlSeconds(ttl)
		if err != nil {
			return err
		}
		var out apigen.MintLeaseResult
		if err := client.Do(ctx, http.MethodPost, base, apigen.MintLeaseRequest{ProviderId: apigen.ID(provider), MaxTtlSeconds: secs}, &out); err != nil {
			return err
		}
		if _, err := sink.WriteOnce("hikyo dynamic credential (shown once)", out.Password); err != nil {
			return failf(ExitRefused, "disclosing the credential: %v", err)
		}
		return Render(ios.Stdout, f, dynamicLeaseTable(apigen.DynamicLeaseList{Items: []apigen.DynamicLease{out.Lease}}))
	}
	return failf(ExitUsage, "unknown lease verb %q", sub)
}

// ttlSeconds parses a positive duration into whole seconds.
func ttlSeconds(raw string) (int64, error) {
	d, err := time.ParseDuration(raw)
	if err != nil || d < time.Second {
		return 0, failf(ExitUsage, "--ttl must be a duration of at least one second, e.g. 1h")
	}
	return int64(d / time.Second), nil
}
