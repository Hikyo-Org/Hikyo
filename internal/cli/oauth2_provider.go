package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// OAuth2 configuration uses profile-fixed endpoints and a write-only secret.
func runOAuth2Provider(ctx context.Context, ios IO, args []string) error {
	verb, rest, err := subverb("instance-config oauth2-provider", args, "create", "list", "show", "update", "delete")
	if err != nil {
		return err
	}
	var slug, profile, origin, secretFile, format string
	var name, clientID optionalString
	var enabled optionalBool
	st, flags, err := parseCommon("instance-config oauth2-provider "+verb, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if verb == "create" {
			fs.StringVar(&slug, "slug", "", "provider slug")
			fs.StringVar(&profile, "profile", "github", "provider profile (github)")
			fs.StringVar(&origin, "origin", "https://github.com", "canonical provider origin")
		}
		if verb == "create" || verb == "update" {
			fs.Var(&name, "display-name", "provider display name")
			fs.Var(&clientID, "client-id", "OAuth client ID")
			fs.StringVar(&secretFile, "client-secret-file", "", "file holding the write-only client secret")
			fs.Var(&enabled, "enabled", "enable or disable sign-in")
		}
	})
	if err != nil {
		return err
	}
	if verb == "create" || verb == "list" {
		if err := flags.checkNoPositionals("instance-config oauth2-provider " + verb); err != nil {
			return err
		}
	} else {
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "%s requires one provider slug", verb)
		}
		slug = flags.positionals[0]
	}
	if verb != "list" && (slug == "" || strings.ContainsAny(slug, "/?#")) {
		return failf(ExitUsage, "a provider slug is required")
	}
	if verb == "create" && (profile != "github" || origin != "https://github.com" || !clientID.set || clientID.value == "" || secretFile == "") {
		return failf(ExitUsage, "create requires --client-id and --client-secret-file; only profile github at https://github.com is supported")
	}
	if verb == "update" && !name.set && !clientID.set && !enabled.set && secretFile == "" {
		return failf(ExitUsage, "update requires a changed field")
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	client, _, err := authenticatedClient(st, ios, flags)
	if err != nil {
		return err
	}
	base := api.PathPrefix + "/instance/oauth2-providers"
	path := base + "/" + url.PathEscape(slug)
	render := func(rows []apigen.Oauth2Provider) error {
		table := make([][]string, 0, len(rows))
		for _, p := range rows {
			table = append(table, []string{p.Slug, p.DisplayName, string(p.Profile), p.Issuer, fmt.Sprint(p.Enabled)})
		}
		return Render(ios.Stdout, f, Table{Columns: []string{"SLUG", "NAME", "PROFILE", "ORIGIN", "ENABLED"}, Rows: table, JSON: apigen.Oauth2ProviderList{Providers: rows}})
	}
	switch verb {
	case "list":
		var rows apigen.Oauth2ProviderList
		if err := client.Do(ctx, http.MethodGet, base, nil, &rows); err != nil {
			return err
		}
		return render(rows.Providers)
	case "show":
		var row apigen.Oauth2Provider
		if err := client.Do(ctx, http.MethodGet, path, nil, &row); err != nil {
			return err
		}
		return render([]apigen.Oauth2Provider{row})
	case "delete":
		return client.Do(ctx, http.MethodDelete, path, nil, nil)
	}
	if verb == "create" {
		var existing apigen.Oauth2Provider
		err := client.Do(ctx, http.MethodGet, path, nil, &existing)
		if err == nil {
			return failf(ExitUsage, "provider already exists; use update")
		}
		var refused *Error
		if !errors.As(err, &refused) || refused.Code != ExitNotFound {
			return err
		}
	}
	input := apigen.Oauth2ProviderInput{Profile: apigen.Oauth2ProviderInputProfileGithub, Issuer: origin, Enabled: true}
	if verb == "update" {
		var old apigen.Oauth2Provider
		if err := client.Do(ctx, http.MethodGet, path, nil, &old); err != nil {
			return err
		}
		input.Profile = apigen.Oauth2ProviderInputProfile(old.Profile)
		input.Issuer = old.Issuer
		input.DisplayName = old.DisplayName
		input.ClientId = old.ClientId
		input.Enabled = old.Enabled
	}
	if verb == "create" && !name.set {
		input.DisplayName = slug
	}
	if name.set {
		input.DisplayName = name.value
	}
	if clientID.set {
		input.ClientId = clientID.value
	}
	if enabled.set {
		input.Enabled = enabled.value
	}
	if secretFile != "" {
		info, err := os.Stat(secretFile)
		if err != nil {
			return failf(ExitUsage, "cannot read client secret file")
		}
		if !info.Mode().IsRegular() || info.Size() > 4098 {
			return failf(ExitUsage, "client secret file must be a regular file of at most 4098 bytes")
		}
		raw, err := os.ReadFile(secretFile)
		if err != nil {
			return failf(ExitUsage, "cannot read client secret file")
		}
		input.ClientSecret = strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	} else {
		input.ClientSecret, err = ios.readPassword("Client secret (required for every update): ")
		if err != nil {
			return err
		}
	}
	if input.ClientSecret == "" {
		return failf(ExitUsage, "client secret is empty")
	}
	if !utf8.ValidString(input.ClientSecret) || utf8.RuneCountInString(input.ClientSecret) > 4096 {
		return failf(ExitUsage, "client secret must contain at most 4096 characters")
	}
	var row apigen.Oauth2Provider
	if err := client.Do(ctx, http.MethodPut, path, input, &row); err != nil {
		return err
	}
	return render([]apigen.Oauth2Provider{row})
}
