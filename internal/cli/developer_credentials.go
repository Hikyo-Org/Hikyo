package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

func runDeveloper(ctx context.Context, ios IO, args []string) error {
	_, rest, err := subverb("dev", args, "session")
	if err != nil {
		return err
	}
	action := ""
	if len(rest) > 0 && (rest[0] == "list" || rest[0] == "revoke") {
		action, rest = rest[0], rest[1:]
	}
	operation := "dev session"
	if action != "" {
		operation += " " + action
	}
	var ttlRaw, id string
	format := "table"
	var all bool
	st, flags, err := parseCommon(operation, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output: table or json")
		if action == "" {
			fs.StringVar(&ttlRaw, "ttl", "", "fixed lifetime, at most 8h; default is the instance ceiling")
		}
		if action == "revoke" {
			fs.StringVar(&id, "id", "", "credential ID to revoke")
			fs.BoolVar(&all, "all", false, "revoke all your developer credentials on this instance")
		}
	})
	if err != nil {
		return err
	}
	if _, err := ParseFormat(format); err != nil {
		return err
	}
	if err := flags.checkNoPositionals(operation); err != nil {
		return err
	}
	if action == "" {
		if _, err := ios.terminalSession(); err != nil {
			return failf(ExitRefused, "hikyo dev session requires a controlling terminal for consent and fresh reauthentication: %v", err)
		}
	}
	client, artifact, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	session, err := requireHumanSession(operation, artifact)
	if err != nil {
		return err
	}
	path := api.PathPrefix + "/auth/developer-credentials"
	switch action {
	case "list":
		var result apigen.DeveloperCredentialList
		if err := client.Do(ctx, http.MethodGet, path, nil, &result); err != nil {
			return err
		}
		return printDeveloperCredentials(ios, result.Items, format)
	case "revoke":
		if (id == "") == !all {
			return failf(ExitUsage, "use exactly one of --id or --all")
		}
		if id != "" {
			path += "/" + url.PathEscape(id)
		}
		if err := client.Do(ctx, http.MethodDelete, path, nil, nil); err != nil {
			return err
		}
		return st.deleteDeveloperCredentials(client.Entry.Origin, id)
	}
	ttl := time.Duration(0)
	if ttlRaw != "" {
		ttl, err = time.ParseDuration(ttlRaw)
		if err != nil || ttl <= 0 || ttl > 8*time.Hour || ttl%time.Second != 0 {
			return failf(ExitUsage, "--ttl must be a positive whole-second duration no greater than 8h")
		}
	}
	if err := st.developerStateOutsideRepository(); err != nil {
		return err
	}
	base, err := projectBase(resolved)
	if err != nil {
		return err
	}
	env, err := resolved.Require(DimEnv)
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
	for _, target := range []struct{ name, prefix, value string }{{"organization", "org_", org}, {"project", "prj_", project}, {"environment", "env_", env}} {
		if !strings.HasPrefix(target.value, target.prefix) {
			return failf(ExitUsage, "developer credential %s must be an immutable %s ID", target.name, strings.TrimSuffix(target.prefix, "_"))
		}
		if _, err := uuid.Parse(strings.TrimPrefix(target.value, target.prefix)); err != nil {
			return failf(ExitUsage, "developer credential %s must be an immutable %s ID", target.name, strings.TrimSuffix(target.prefix, "_"))
		}
	}
	existing, err := st.developerCredentialFor(client.Entry.Origin, org, project, env, ios.now())
	if err != nil && !errors.Is(err, errDeveloperCredentialExpired) {
		return err
	}
	if existing.ID != "" {
		return failf(ExitRefused, "a live developer credential already matches this environment; revoke it before minting another")
	}
	var revision apigen.RevisionDetail
	if err := client.Do(ctx, http.MethodGet, base+"/environments/"+url.PathEscape(env)+"/revisions/latest", nil, &revision); err != nil {
		return err
	}
	keys := make([]apigen.ID, 0, len(revision.Keys))
	names := make([]string, 0, len(revision.Keys))
	for _, key := range revision.Keys {
		keys = append(keys, key.KeyId)
		names = append(names, key.Name)
	}
	var window apigen.RevealWindow
	if err := client.Do(ctx, http.MethodGet, revealWindowPath(base, env), nil, &window); err != nil {
		return err
	}
	if window.Protected || !window.CanReveal {
		return failf(ExitRefused, "developer credentials require an unprotected environment with current read and reveal authority")
	}
	if window.DeveloperCredentialMaxLifetimeSeconds == nil || *window.DeveloperCredentialMaxLifetimeSeconds < 1 || *window.DeveloperCredentialMaxLifetimeSeconds > 28800 {
		return failf(ExitRefused, "server did not provide a valid developer credential lifetime ceiling")
	}
	ceiling := time.Duration(*window.DeveloperCredentialMaxLifetimeSeconds) * time.Second
	if ttl == 0 {
		ttl = ceiling
	}
	if ttl > ceiling {
		return failf(ExitRefused, "developer credential lifetime exceeds the current instance ceiling of %s", ceiling)
	}
	lifetime := ttl.String()
	terminal, err := ios.terminalSession()
	if err != nil {
		return err
	}
	ok, err := terminal.ConfirmEnumerated(fmt.Sprintf("Delegate your current read and reveal authority at %s\nOrganization %s, project %s, environment %s\nFixed lifetime: %s\nCurrent published keys: %s\nConsent covers ALL CURRENT AND FUTURE published keys in this environment until expiry.\nThe credential survives ordinary logout and never renews. Proceed", client.Entry.Origin, org, project, env, lifetime, strings.Join(names, ", ")))
	if err != nil {
		return failf(ExitRefused, "reading developer credential consent: %v", err)
	}
	if !ok {
		return failf(ExitRefused, "developer credential consent declined")
	}
	intent := apigen.DeveloperCredentialReauthIntent{LifetimeSeconds: int64(ttl / time.Second), ConsentCurrentAndFuture: true}
	if err := freshDeveloperReauth(ctx, client, st, ios, &session, base, env, keys, intent); err != nil {
		return err
	}
	request := apigen.MintDeveloperCredentialRequest{KeyIds: keys, ConsentCurrentAndFuture: true, LifetimeSeconds: int64(ttl / time.Second)}
	var minted apigen.MintDeveloperCredentialResult
	mintPath := base + "/environments/" + url.PathEscape(env) + "/developer-credentials"
	if err := client.Do(ctx, http.MethodPost, mintPath, request, &minted); err != nil {
		var failure *Error
		if errors.As(err, &failure) && (failure.Code == ExitAuth || failure.Code == ExitRefused || failure.Code == ExitNotFound) {
			return &Error{Code: failure.Code, Err: fmt.Errorf("developer credential mint refused: %w; if your login lacks the required MFA assurance, run `hikyo account factor step-up` and retry; the fresh developer ceremony does not upgrade login assurance", err)}
		}
		return err
	}
	c := minted.Credential
	custody := DeveloperCredentialArtifact{ID: string(c.Id), Origin: client.Entry.Origin, Org: string(c.OrgId), Project: string(c.ProjectId), Environment: string(c.EnvironmentId), Token: minted.Value, ExpiresAt: c.ExpiresAt}
	saveErr := crypto.ParseArtifact(minted.Value, crypto.ArtifactDeveloperCredential)
	// Server metadata must bind the exact immutable target the human confirmed.
	if custody.Org != org || custody.Project != project || custody.Environment != env || !c.ExpiresAt.After(c.CreatedAt) || c.ExpiresAt.Sub(c.CreatedAt) > ttl {
		saveErr = fmt.Errorf("server returned an unexpected developer credential scope or lifetime")
	}
	if saveErr == nil {
		saveErr = st.PutDeveloperCredential(custody)
	}
	if saveErr != nil {
		revokeErr := client.Do(ctx, http.MethodDelete, path+"/"+url.PathEscape(string(c.Id)), nil, nil)
		if revokeErr != nil {
			return &Error{Code: ExitInternal, Err: errors.Join(fmt.Errorf("developer credential custody failed: %w", saveErr), fmt.Errorf("server revocation failed; revoke credential %s manually: %w", c.Id, revokeErr))}
		}
		return &Error{Code: ExitInternal, Err: fmt.Errorf("developer credential custody failed; server credential revoked: %w", saveErr)}
	}
	fmt.Fprintf(ios.Stderr, "Developer credential stored privately. hikyo run will use it for this exact environment until %s.\n", c.ExpiresAt.Format(time.RFC3339))
	return printDeveloperCredentials(ios, []apigen.DeveloperCredential{c}, format)
}

func printDeveloperCredentials(ios IO, items []apigen.DeveloperCredential, output string) error {
	if output == "json" {
		return json.NewEncoder(ios.Stdout).Encode(items)
	}
	for _, c := range items {
		fmt.Fprintf(ios.Stdout, "%s\t%s\t%s\t%s\n", c.Id, c.EnvironmentId, c.ExpiresAt.Format(time.RFC3339), c.AuthorityPrincipalId)
	}
	return nil
}

func freshDeveloperReauth(ctx context.Context, client *Client, st *State, ios IO, session *SessionArtifact, base, env string, keys []apigen.ID, intent apigen.DeveloperCredentialReauthIntent) error {
	var window apigen.RevealWindow
	if err := client.Do(ctx, http.MethodGet, revealWindowPath(base, env), nil, &window); err != nil {
		return err
	}
	if window.Protected || !window.CanReveal {
		return failf(ExitRefused, "developer credentials require an unprotected environment with current read and reveal authority")
	}
	inline := window.EffectiveWindowSeconds > 0 && window.TotpOffered
	if inline {
		var status apigen.TotpStatus
		if err := client.Do(ctx, http.MethodGet, api.PathPrefix+"/auth/totp", nil, &status); err != nil {
			return err
		}
		inline = status.Confirmed
	}
	if !inline {
		ids := make([]string, len(keys))
		for i, id := range keys {
			ids[i] = string(id)
		}
		return runCLIReauthHandoffIntents(ctx, client, st, *session, "developer-credential", "developer-credential.mint", []string{env}, ids, nil, &intent, ios.OpenURL)
	}
	code, err := ios.readPassword("Fresh developer credential authorization: enter your authenticator code: ")
	if err != nil {
		return err
	}
	var body apigen.TotpReauthRequest
	if err := body.FromTotpDeveloperCredentialReauthRequest(apigen.TotpDeveloperCredentialReauthRequest{Code: code, DeveloperCredential: intent, EnvironmentId: apigen.ID(env), KeyIds: keys, Purpose: apigen.TotpDeveloperCredentialReauthRequestPurposeDeveloperCredential}); err != nil {
		return err
	}
	var opened apigen.ReauthResult
	if err := client.Do(ctx, http.MethodPost, api.PathPrefix+"/auth/reauth/totp", body, &opened); err != nil {
		return err
	}
	if opened.SessionToken == nil || *opened.SessionToken == "" {
		return failf(ExitInternal, "developer credential reauthentication returned no rotated CLI session")
	}
	session.Token = *opened.SessionToken
	session.SessionID = string(opened.SessionId)
	if err := st.PutSession(*session); err != nil {
		return err
	}
	client.Bearer = session.Token
	return nil
}

func runDeveloperCredentialPolicy(ctx context.Context, ios IO, args []string) error {
	action, rest, err := subverb("instance-config developer-credential-policy", args, "get", "set")
	if err != nil {
		return err
	}
	var raw string
	format := "table"
	st, flags, err := parseCommon("instance-config developer-credential-policy "+action, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output: table or json")
		if action == "set" {
			fs.StringVar(&raw, "max-lifetime", "", "required positive fixed ceiling at most 8h")
		}
	})
	if err != nil {
		return err
	}
	if _, err := ParseFormat(format); err != nil {
		return err
	}
	if err := flags.checkNoPositionals("instance-config developer-credential-policy " + action); err != nil {
		return err
	}
	client, artifact, _, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	if _, err := requireHumanSession("developer credential policy", artifact); err != nil {
		return err
	}
	var policy apigen.DeveloperCredentialPolicy
	method := http.MethodGet
	var body any
	if action == "set" {
		ttl, err := time.ParseDuration(raw)
		if err != nil || ttl <= 0 || ttl > 8*time.Hour || ttl%time.Second != 0 {
			return failf(ExitUsage, "--max-lifetime must be a positive whole-second duration at most 8h")
		}
		body = apigen.DeveloperCredentialPolicy{MaxLifetimeSeconds: int64(ttl / time.Second)}
		method = http.MethodPut
	}
	if err := client.Do(ctx, method, api.PathPrefix+"/instance/developer-credential-policy", body, &policy); err != nil {
		return err
	}
	if format == "json" {
		return json.NewEncoder(ios.Stdout).Encode(policy)
	}
	_, err = fmt.Fprintf(ios.Stdout, "max lifetime: %s\n", time.Duration(policy.MaxLifetimeSeconds)*time.Second)
	return err
}
