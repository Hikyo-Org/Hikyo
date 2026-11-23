package cli

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// runInstanceConfig owns instance-scoped configuration verbs. SAML provider
// administration (#72) joins this existing grammar; it is never local-admin
// authority and every request still proves instance-config at the server.
func runInstanceConfig(ctx context.Context, ios IO, args []string) error {
	noun, rest, err := subverb("instance-config", args,
		"provider", "oauth2-provider", "saml-sp-key", "credential-policy", "developer-credential-policy", "federation-issuer", "status", "adopt", "apply", "test-email")
	if err != nil {
		return err
	}
	if noun == "status" || noun == "adopt" || noun == "apply" || noun == "test-email" {
		return runSelfConfig(ctx, ios, noun, rest)
	}
	if noun == "federation-issuer" {
		return runFederationIssuer(ctx, ios, rest)
	}
	if noun == "oauth2-provider" {
		return runOAuth2Provider(ctx, ios, rest)
	}
	if noun == "saml-sp-key" {
		return runSAMLSPKey(ctx, ios, rest)
	}
	if noun == "developer-credential-policy" {
		return runDeveloperCredentialPolicy(ctx, ios, rest)
	}
	if noun == "credential-policy" {
		return runCredentialPolicy(ctx, ios, rest)
	}
	if noun != "provider" {
		return failf(ExitInternal, "hikyo instance-config: unhandled noun %q", noun)
	}
	return runProvider(ctx, ios, rest)
}

func runProvider(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("instance-config provider", args,
		"create", "list", "show", "update", "disable", "remove", "refresh-metadata")
	if err != nil {
		return err
	}
	switch sub {
	case "create":
		return runProviderCreate(ctx, ios, rest)
	case "list", "show", "remove":
		return runProviderReadOrRemove(ctx, ios, sub, rest)
	case "update":
		return runProviderUpdate(ctx, ios, rest)
	case "disable":
		return runProviderDisable(ctx, ios, rest)
	case "refresh-metadata":
		return runProviderRefresh(ctx, ios, rest)
	default:
		return failf(ExitInternal, "hikyo instance-config provider: unhandled verb %q", sub)
	}
}

func runProviderCreate(ctx context.Context, ios IO, args []string) error {
	var kind, name, displayName, entityID, metadataFile, metadataURL, format string
	var assuranceContexts, confirmedFingerprints, confirmedEndpoints stringList
	var allowEmailNameID, forceSignRequests bool
	st, flags, err := parseCommon("instance-config provider create", ios, args, func(fs *flag.FlagSet) {
		fs.StringVar(&kind, "kind", "", "provider kind (saml)")
		fs.StringVar(&name, "name", "", "provider slug")
		fs.StringVar(&displayName, "display-name", "", "provider display name (default: name)")
		fs.StringVar(&entityID, "entity-id", "", "byte-exact IdP entityID")
		fs.StringVar(&metadataFile, "metadata-file", "", "IdP metadata XML file")
		fs.StringVar(&metadataURL, "metadata-url", "", "one-shot IdP metadata URL")
		fs.Var(&assuranceContexts, "assurance-context", "accepted AuthnContextClassRef; repeat for several")
		fs.Var(&confirmedFingerprints, "confirm-fingerprint", "fingerprint explicitly confirmed; repeat for several")
		fs.Var(&confirmedEndpoints, "confirm-endpoint", "endpoint URL explicitly confirmed; repeat for several")
		fs.BoolVar(&allowEmailNameID, "allow-email-nameid", false, "accept emailAddress NameID opaquely (reassignment can rebind identity)")
		fs.BoolVar(&forceSignRequests, "force-sign-requests", false, "sign AuthnRequests even when metadata does not require it")
		fs.StringVar(&format, "o", "table", "output format: table or json")
	})
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("instance-config provider create"); err != nil {
		return err
	}
	switch {
	case kind == "":
		return failf(ExitUsage, "usage: hikyo instance-config provider create --kind saml --name <name> --entity-id <entityID> (--metadata-file <xml> | --metadata-url <url>)")
	case kind != "saml":
		return failf(ExitUsage, "provider kind %q is not configurable by this command; use saml", kind)
	case name == "":
		return failf(ExitUsage, "--name <name> is required")
	case entityID == "":
		return failf(ExitUsage, "--entity-id <entityID> is required to select exactly one descriptor from metadata")
	case (metadataFile == "") == (metadataURL == ""):
		return failf(ExitUsage, "exactly one of --metadata-file or --metadata-url is required")
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	client, _, err := authenticatedClient(st, ios, flags)
	if err != nil {
		return err
	}

	input := apigen.SamlProviderInput{
		AllowEmailNameid:  allowEmailNameID,
		DisplayName:       cmp.Or(displayName, name),
		Enabled:           true,
		EntityId:          entityID,
		ForceSignRequests: forceSignRequests,
	}
	if len(assuranceContexts) > 0 {
		values := []string(assuranceContexts)
		input.AssurancePolicy = &values
	}
	if len(confirmedFingerprints) > 0 {
		values := []string(confirmedFingerprints)
		input.ConfirmedFingerprints = &values
	}
	if len(confirmedEndpoints) > 0 {
		values := []string(confirmedEndpoints)
		input.ConfirmedEndpoints = &values
	}
	if metadataFile != "" {
		document, err := readMetadataDocument(metadataFile)
		if err != nil {
			return err
		}
		input.MetadataSource = apigen.SamlMetadataSourceFile
		input.MetadataDocument = &document
	} else {
		input.MetadataSource = apigen.SamlMetadataSourceUrl
		input.MetadataUrl = &metadataURL
	}

	var result apigen.SamlProviderMutationResult
	if err := client.Do(ctx, http.MethodPut, samlProviderPath(name), input, &result); err != nil {
		return err
	}
	return finishSAMLMutation(ios, f, result)
}

type providerSummary struct {
	Kind        string                       `json:"kind"`
	Slug        string                       `json:"slug"`
	DisplayName string                       `json:"display_name"`
	Enabled     bool                         `json:"enabled"`
	Warnings    []apigen.SamlProviderWarning `json:"warnings"`
}

type providerSummaryList struct {
	Providers []providerSummary `json:"providers"`
}

func runProviderReadOrRemove(ctx context.Context, ios IO, sub string, args []string) error {
	var format, kind string
	st, flags, err := parseCommon("instance-config provider "+sub, ios, args, func(fs *flag.FlagSet) {
		fs.StringVar(&kind, "kind", "", "disambiguate provider kind: oidc or saml")
		fs.StringVar(&format, "o", "table", "output format: table or json")
	})
	if err != nil {
		return err
	}
	if err := validateProviderKind(kind); err != nil {
		return err
	}
	if sub == "list" {
		if err := flags.checkNoPositionals("instance-config provider list"); err != nil {
			return err
		}
	}
	if sub != "list" {
		if _, err := providerName(flags, "instance-config provider "+sub); err != nil {
			return err
		}
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	client, _, err := authenticatedClient(st, ios, flags)
	if err != nil {
		return err
	}
	providers, err := listProviderSummaries(ctx, client)
	if err != nil {
		return err
	}
	if sub == "list" {
		rows := make([][]string, 0, len(providers))
		for _, provider := range providers {
			rows = append(rows, []string{provider.Kind, provider.Slug, provider.DisplayName, boolString(provider.Enabled), strings.Join(warningCodes(provider.Warnings), ",")})
		}
		return Render(ios.Stdout, f, Table{
			Columns: []string{"KIND", "NAME", "DISPLAY NAME", "ENABLED", "WARNINGS"},
			Rows:    rows, JSON: providerSummaryList{Providers: providers},
		})
	}
	provider, err := selectProvider(providers, flags.positional(), kind)
	if err != nil {
		return err
	}
	if sub == "remove" {
		path := oidcProviderPath(provider.Slug)
		if provider.Kind == "saml" {
			path = samlProviderPath(provider.Slug)
		}
		if err := client.Do(ctx, http.MethodDelete, path, nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(ios.Stderr, "removed %s provider %s\n", provider.Kind, provider.Slug)
		return nil
	}
	if provider.Kind == "saml" {
		var full apigen.SamlProvider
		if err := client.Do(ctx, http.MethodGet, samlProviderPath(provider.Slug), nil, &full); err != nil {
			return err
		}
		return renderSAMLProvider(ios, f, full)
	}
	var full apigen.OidcProvider
	if err := client.Do(ctx, http.MethodGet, oidcProviderPath(provider.Slug), nil, &full); err != nil {
		return err
	}
	return Render(ios.Stdout, f, Table{
		Columns: []string{"KIND", "NAME", "DISPLAY NAME", "ENABLED"},
		Rows:    [][]string{{"oidc", full.Slug, full.DisplayName, boolString(full.Enabled)}}, JSON: full,
	})
}

func runProviderUpdate(ctx context.Context, ios IO, args []string) error {
	var kind string
	var displayName optionalString
	var assuranceContexts stringList
	var allowEmailNameID, forceSignRequests, enabled optionalBool
	var clearAssurance bool
	var lifecycleProfile optionalString
	var lifecycleSecretFile, clientSecretFile string
	st, flags, err := parseCommon("instance-config provider update", ios, args, func(fs *flag.FlagSet) {
		fs.StringVar(&kind, "kind", "", "disambiguate provider kind: oidc or saml")
		fs.Var(&displayName, "display-name", "new display name")
		fs.Var(&assuranceContexts, "assurance-context", "accepted AuthnContextClassRef; repeat for several")
		fs.BoolVar(&clearAssurance, "clear-assurance-policy", false, "clear accepted contexts (single-factor)")
		fs.Var(&allowEmailNameID, "allow-email-nameid", "accept or refuse emailAddress NameID opaquely")
		fs.Var(&forceSignRequests, "force-sign-requests", "force or stop forcing signed AuthnRequests")
		fs.Var(&enabled, "enabled", "enable or disable the provider")
		fs.Var(&lifecycleProfile, "lifecycle-profile", "OIDC directory lifecycle profile: clerk, or none to clear")
		fs.StringVar(&lifecycleSecretFile, "lifecycle-secret-file", "", "OIDC: file holding the write-only directory webhook signing secret")
		fs.StringVar(&clientSecretFile, "client-secret-file", "", "OIDC: file holding the client secret that every OIDC update re-sends")
	})
	if err != nil {
		return err
	}
	if err := validateProviderKind(kind); err != nil {
		return err
	}
	name, err := providerName(flags, "instance-config provider update")
	if err != nil {
		return err
	}
	if len(assuranceContexts) > 0 && clearAssurance {
		return failf(ExitUsage, "--assurance-context and --clear-assurance-policy are mutually exclusive")
	}
	if lifecycleProfile.set && lifecycleProfile.value != apigenClerk && lifecycleProfile.value != "none" {
		return failf(ExitUsage, "--lifecycle-profile must be clerk or none")
	}
	if lifecycleProfile.set && lifecycleProfile.value == "none" && lifecycleSecretFile != "" {
		return failf(ExitUsage, "--lifecycle-secret-file needs a lifecycle profile")
	}
	lifecycleChange := lifecycleProfile.set || lifecycleSecretFile != ""
	if !displayName.set && len(assuranceContexts) == 0 && !clearAssurance && !allowEmailNameID.set && !forceSignRequests.set && !enabled.set && !lifecycleChange {
		return failf(ExitUsage, "provider update requires at least one changed field")
	}
	client, _, err := authenticatedClient(st, ios, flags)
	if err != nil {
		return err
	}
	providers, err := listProviderSummaries(ctx, client)
	if err != nil {
		return err
	}
	provider, err := selectProvider(providers, name, kind)
	if err != nil {
		return err
	}
	if provider.Kind == "oidc" {
		if len(assuranceContexts) > 0 || clearAssurance || allowEmailNameID.set || forceSignRequests.set {
			return failf(ExitUsage, "--assurance-context, --clear-assurance-policy, --allow-email-nameid and --force-sign-requests apply to SAML providers only")
		}
		return runOIDCProviderUpdate(ctx, ios, client, name, oidcProviderChange{
			displayName: displayName, enabled: enabled, lifecycleProfile: lifecycleProfile,
			lifecycleSecretFile: lifecycleSecretFile, clientSecretFile: clientSecretFile,
		})
	}
	if lifecycleChange || clientSecretFile != "" {
		return failf(ExitUsage, "--lifecycle-profile, --lifecycle-secret-file and --client-secret-file apply to OIDC providers only")
	}
	patch := apigen.SamlProviderPatch{}
	if displayName.set {
		patch.DisplayName = &displayName.value
	}
	if len(assuranceContexts) > 0 || clearAssurance {
		values := []string(assuranceContexts)
		patch.AssurancePolicy = &values
	}
	if allowEmailNameID.set {
		patch.AllowEmailNameid = &allowEmailNameID.value
	}
	if forceSignRequests.set {
		patch.ForceSignRequests = &forceSignRequests.value
	}
	if enabled.set {
		patch.Enabled = &enabled.value
	}
	var updated apigen.SamlProvider
	if err := client.Do(ctx, http.MethodPatch, samlProviderPath(name), patch, &updated); err != nil {
		return err
	}
	return renderSAMLProvider(ios, FormatTable, updated)
}

func runProviderDisable(ctx context.Context, ios IO, args []string) error {
	var kind string
	st, flags, err := parseCommon("instance-config provider disable", ios, args, func(fs *flag.FlagSet) {
		fs.StringVar(&kind, "kind", "", "disambiguate provider kind: oidc or saml")
	})
	if err != nil {
		return err
	}
	if err := validateProviderKind(kind); err != nil {
		return err
	}
	name, err := providerName(flags, "instance-config provider disable")
	if err != nil {
		return err
	}
	client, _, err := authenticatedClient(st, ios, flags)
	if err != nil {
		return err
	}
	providers, err := listProviderSummaries(ctx, client)
	if err != nil {
		return err
	}
	provider, err := selectProvider(providers, name, kind)
	if err != nil {
		return err
	}
	if provider.Kind != "saml" {
		return failf(ExitRefused, "OIDC partial provider disable is not served by this API; SAML disable is available")
	}
	disabled := false
	var updated apigen.SamlProvider
	if err := client.Do(ctx, http.MethodPatch, samlProviderPath(name), apigen.SamlProviderPatch{Enabled: &disabled}, &updated); err != nil {
		return err
	}
	fmt.Fprintf(ios.Stderr, "disabled SAML provider %s and invalidated its sessions\n", name)
	return nil
}

func runProviderRefresh(ctx context.Context, ios IO, args []string) error {
	var metadataFile string
	var confirmedFingerprints, confirmedEndpoints stringList
	st, flags, err := parseCommon("instance-config provider refresh-metadata", ios, args, func(fs *flag.FlagSet) {
		fs.StringVar(&metadataFile, "metadata-file", "", "replacement XML for a file-backed provider")
		fs.Var(&confirmedFingerprints, "confirm-fingerprint", "fingerprint explicitly confirmed; repeat for several")
		fs.Var(&confirmedEndpoints, "confirm-endpoint", "endpoint URL explicitly confirmed; repeat for several")
	})
	if err != nil {
		return err
	}
	name, err := providerName(flags, "instance-config provider refresh-metadata")
	if err != nil {
		return err
	}
	client, _, err := authenticatedClient(st, ios, flags)
	if err != nil {
		return err
	}
	request := apigen.SamlMetadataRefreshRequest{}
	if len(confirmedFingerprints) > 0 {
		values := []string(confirmedFingerprints)
		request.ConfirmedFingerprints = &values
	}
	if len(confirmedEndpoints) > 0 {
		values := []string(confirmedEndpoints)
		request.ConfirmedEndpoints = &values
	}
	if metadataFile != "" {
		document, err := readMetadataDocument(metadataFile)
		if err != nil {
			return err
		}
		request.MetadataDocument = &document
	}
	var result apigen.SamlProviderMutationResult
	if err := client.Do(ctx, http.MethodPost, samlProviderPath(name)+"/refresh-metadata", request, &result); err != nil {
		return err
	}
	return finishSAMLMutation(ios, FormatTable, result)
}

func finishSAMLMutation(ios IO, format Format, result apigen.SamlProviderMutationResult) error {
	if !result.Applied {
		if err := Render(ios.Stdout, format, Table{
			Columns: []string{"ENDPOINTS ADDED", "ENDPOINTS REMOVED", "CERTS ADDED", "CERTS REMOVED"},
			Rows: [][]string{{
				strings.Join(result.Diff.EndpointsAdded, ","), strings.Join(result.Diff.EndpointsRemoved, ","),
				strings.Join(result.Diff.CertsAddedFps, ","), strings.Join(result.Diff.CertsRemovedFps, ","),
			}},
			JSON: result,
		}); err != nil {
			return err
		}
		var flags []string
		for _, fingerprint := range result.RequiredFingerprints {
			flags = append(flags, "--confirm-fingerprint "+shellQuote(fingerprint))
		}
		for _, endpoint := range result.RequiredEndpoints {
			flags = append(flags, "--confirm-endpoint "+shellQuote(endpoint))
		}
		return failf(ExitRefused, "metadata diff requires explicit confirmation; rerun with: %s", strings.Join(flags, " "))
	}
	if result.Provider == nil {
		return failf(ExitInternal, "server reported an applied SAML provider mutation without the provider")
	}
	return renderSAMLProvider(ios, format, *result.Provider)
}

func listProviderSummaries(ctx context.Context, client *Client) ([]providerSummary, error) {
	var oidc apigen.OidcProviderList
	if err := client.Do(ctx, http.MethodGet, api.PathPrefix+"/instance/oidc-providers", nil, &oidc); err != nil {
		return nil, err
	}
	var saml apigen.SamlProviderList
	if err := client.Do(ctx, http.MethodGet, api.PathPrefix+"/instance/saml-providers", nil, &saml); err != nil {
		return nil, err
	}
	out := make([]providerSummary, 0, len(oidc.Providers)+len(saml.Providers))
	for _, provider := range oidc.Providers {
		out = append(out, providerSummary{Kind: "oidc", Slug: provider.Slug, DisplayName: provider.DisplayName, Enabled: provider.Enabled, Warnings: []apigen.SamlProviderWarning{}})
	}
	for _, provider := range saml.Providers {
		warnings := slices.Clone(provider.Warnings)
		out = append(out, providerSummary{Kind: "saml", Slug: provider.Slug, DisplayName: provider.DisplayName, Enabled: provider.Enabled, Warnings: warnings})
	}
	slices.SortFunc(out, func(a, b providerSummary) int {
		if byName := strings.Compare(a.Slug, b.Slug); byName != 0 {
			return byName
		}
		return strings.Compare(a.Kind, b.Kind)
	})
	return out, nil
}

func selectProvider(providers []providerSummary, name, kind string) (providerSummary, error) {
	var matches []providerSummary
	for _, provider := range providers {
		if provider.Slug == name && (kind == "" || provider.Kind == kind) {
			matches = append(matches, provider)
		}
	}
	switch len(matches) {
	case 0:
		return providerSummary{}, failf(ExitNotFound, "provider %q was not found", name)
	case 1:
		return matches[0], nil
	default:
		return providerSummary{}, failf(ExitRefused, "provider name %q exists under multiple kinds; pass --kind oidc or --kind saml", name)
	}
}

func validateProviderKind(kind string) error {
	if kind != "" && kind != "oidc" && kind != "saml" {
		return failf(ExitUsage, "unknown provider kind %q: use oidc or saml", kind)
	}
	return nil
}

func renderSAMLProvider(ios IO, format Format, provider apigen.SamlProvider) error {
	return Render(ios.Stdout, format, Table{
		Columns: []string{"KIND", "NAME", "DISPLAY NAME", "ENTITY ID", "ENABLED", "WARNINGS"},
		Rows:    [][]string{{"saml", provider.Slug, provider.DisplayName, provider.EntityId, boolString(provider.Enabled), strings.Join(warningCodes(provider.Warnings), ",")}},
		JSON:    provider,
	})
}

func warningCodes(warnings []apigen.SamlProviderWarning) []string {
	codes := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		codes = append(codes, string(warning.Code))
	}
	return codes
}

func samlProviderPath(name string) string {
	return api.PathPrefix + "/instance/saml-providers/" + url.PathEscape(name)
}

func readMetadataDocument(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", failf(ExitRefused, "reading metadata file %s: %v", path, err)
	}
	if len(raw) > 256*1024 {
		return "", failf(ExitRefused, "metadata file %s exceeds the 256 KiB document limit", path)
	}
	return string(raw), nil
}

func oidcProviderPath(name string) string {
	return api.PathPrefix + "/instance/oidc-providers/" + url.PathEscape(name)
}

func providerName(flags commonFlags, verb string) (string, error) {
	if len(flags.positionals) != 1 {
		return "", failf(ExitUsage, "usage: hikyo %s <name>", verb)
	}
	return flags.positionals[0], nil
}

type stringList []string

func (v *stringList) String() string { return strings.Join(*v, ",") }
func (v *stringList) Set(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("value must not be empty")
	}
	if slices.Contains(*v, raw) {
		return fmt.Errorf("duplicate value %q", raw)
	}
	*v = append(*v, raw)
	return nil
}

// shellQuote makes server-derived metadata safe to copy into a POSIX shell.
// Endpoint strings are controlled by the IdP metadata and therefore untrusted;
// Go's %q uses double quotes, which would still expand `$()` in a shell.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type optionalString struct {
	value string
	set   bool
}

func (v *optionalString) String() string { return v.value }
func (v *optionalString) Set(raw string) error {
	v.value, v.set = raw, true
	return nil
}

type optionalBool struct {
	value bool
	set   bool
}

func (v *optionalBool) String() string   { return fmt.Sprint(v.value) }
func (v *optionalBool) IsBoolFlag() bool { return true }
func (v *optionalBool) Set(raw string) error {
	switch raw {
	case "true":
		v.value, v.set = true, true
	case "false":
		v.value, v.set = false, true
	default:
		return fmt.Errorf("must be true or false")
	}
	return nil
}

// apigenClerk is the one lifecycle profile this CLI can name today; the server
// owns the closed set and refuses anything else.
const apigenClerk = string(apigen.OidcLifecycleProfileClerk)

// oidcProviderChange is the set of fields an OIDC update may change from the CLI.
type oidcProviderChange struct {
	displayName         optionalString
	enabled             optionalBool
	lifecycleProfile    optionalString
	lifecycleSecretFile string
	clientSecretFile    string
}

// runOIDCProviderUpdate reconfigures an OIDC provider. The API replaces the whole
// row and requires the client secret on every write, so the update reads the
// stored row, applies the change, and takes the client secret from
// --client-secret-file or a prompt, never from an argument. The directory
// lifecycle secret is read the same way and is only asked for when a profile
// needs one that is not stored; a stored secret is kept when none is given.
func runOIDCProviderUpdate(ctx context.Context, ios IO, client *Client, name string, change oidcProviderChange) error {
	var old apigen.OidcProvider
	if err := client.Do(ctx, http.MethodGet, oidcProviderPath(name), nil, &old); err != nil {
		return err
	}
	input := apigen.OidcProviderInput{
		DisplayName: old.DisplayName, Issuer: old.Issuer, ClientId: old.ClientId, Scopes: old.Scopes,
		AssurancePolicy: old.AssurancePolicy, Enabled: old.Enabled, RowVersion: &old.RowVersion,
		LifecycleProfile: old.LifecycleProfile,
	}
	if change.displayName.set {
		input.DisplayName = change.displayName.value
	}
	if change.enabled.set {
		input.Enabled = change.enabled.value
	}
	switch {
	case change.lifecycleProfile.set && change.lifecycleProfile.value == "none":
		input.LifecycleProfile = nil
	case change.lifecycleProfile.set:
		profile := apigen.OidcLifecycleProfile(change.lifecycleProfile.value)
		input.LifecycleProfile = &profile
	}
	if change.lifecycleSecretFile != "" && input.LifecycleProfile == nil {
		return failf(ExitUsage, "--lifecycle-secret-file needs a lifecycle profile: pass --lifecycle-profile clerk")
	}
	var err error
	if input.ClientSecret, err = secretFromFileOrPrompt(ios, change.clientSecretFile, "client secret",
		"Client secret (required for every update): ", true); err != nil {
		return err
	}
	if input.LifecycleProfile != nil {
		storedSecret := old.LifecycleProfile != nil && old.LifecycleSecretSet
		secret, err := secretFromFileOrPrompt(ios, change.lifecycleSecretFile, "lifecycle secret",
			"Lifecycle signing secret (whsec_...): ", !storedSecret)
		if err != nil {
			return err
		}
		if secret != "" {
			input.LifecycleSecret = &secret
		}
	}
	var row apigen.OidcProvider
	if err := client.Do(ctx, http.MethodPut, oidcProviderPath(name), input, &row); err != nil {
		return err
	}
	return Render(ios.Stdout, FormatTable, Table{
		Columns: []string{"KIND", "NAME", "DISPLAY NAME", "ENABLED", "LIFECYCLE"},
		Rows:    [][]string{{"oidc", row.Slug, row.DisplayName, boolString(row.Enabled), lifecycleLabel(row)}}, JSON: row,
	})
}

func lifecycleLabel(p apigen.OidcProvider) string {
	if p.LifecycleProfile == nil {
		return "-"
	}
	if p.LifecycleSecretSet {
		return string(*p.LifecycleProfile)
	}
	return string(*p.LifecycleProfile) + " (no secret)"
}

// secretFromFileOrPrompt reads a write-only secret from a regular file of at
// most 4098 bytes, or from a no-echo prompt when no file is named and the secret
// is required. An optional secret with no file is returned empty. The secret is
// never an argv value.
func secretFromFileOrPrompt(ios IO, file, label, prompt string, required bool) (string, error) {
	var secret string
	switch {
	case file != "":
		raw, err := readSecretFile(file, label)
		if err != nil {
			return "", err
		}
		secret = raw
	case required:
		var err error
		if secret, err = ios.readPassword(prompt); err != nil {
			return "", err
		}
	default:
		return "", nil
	}
	if secret == "" {
		return "", failf(ExitUsage, "%s is empty", label)
	}
	if !utf8.ValidString(secret) || utf8.RuneCountInString(secret) > 4096 {
		return "", failf(ExitUsage, "%s must contain at most 4096 characters", label)
	}
	return secret, nil
}

func readSecretFile(path, label string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", failf(ExitUsage, "cannot read %s file", label)
	}
	if !info.Mode().IsRegular() || info.Size() > 4098 {
		return "", failf(ExitUsage, "%s file must be a regular file of at most 4098 bytes", label)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", failf(ExitUsage, "cannot read %s file", label)
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r"), nil
}
