package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func credentialRotationTable(list apigen.CredentialRotationList) Table {
	rows := make([][]string, 0, len(list.Items))
	for _, r := range list.Items {
		next := "-"
		if r.NextRunAt != nil {
			next = r.NextRunAt.UTC().Format(time.RFC3339)
		}
		expires := "-"
		if r.CredentialExpiresAt != nil {
			expires = r.CredentialExpiresAt.UTC().Format(time.RFC3339)
		}
		rows = append(rows, []string{
			string(r.Id), string(r.State), string(r.ProviderId), string(r.KeyId), string(r.UsernameKeyId),
			(time.Duration(r.IntervalSeconds) * time.Second).String(),
			(time.Duration(r.RevocationDelaySeconds) * time.Second).String(),
			next, expires, strconv.FormatInt(r.Generation, 10),
		})
	}
	return Table{
		// No credential column: this record holds configuration only; no password
		// or role handle is stored or returned.
		Columns: []string{"ID", "STATE", "PROVIDER", "KEY", "USERNAME KEY", "INTERVAL", "REVOCATION DELAY", "NEXT RUN", "CREDENTIAL EXPIRES", "GENERATION"},
		Rows:    rows,
		JSON:    list,
	}
}

func rotationRunsTable(list apigen.RotationRunList) Table {
	rows := make([][]string, 0, len(list.Items))
	for _, r := range list.Items {
		revision, code, converged := "-", "-", "-"
		if r.PublishedRevision != nil {
			revision = strconv.FormatInt(*r.PublishedRevision, 10)
		}
		if r.ErrorCode != nil {
			code = *r.ErrorCode
		}
		if r.NotConvergedConsumers != nil {
			converged = strconv.FormatInt(*r.NotConvergedConsumers, 10)
		}
		rows = append(rows, []string{
			string(r.Id), string(r.Trigger), string(r.Phase), revision,
			(time.Duration(r.RevocationDelaySeconds) * time.Second).String(),
			r.StartedAt.UTC().Format(time.RFC3339), r.PhaseAt.UTC().Format(time.RFC3339), code, converged,
		})
	}
	return Table{
		// Metadata only: no role name, candidate or provider detail is returned.
		Columns: []string{"RUN", "TRIGGER", "PHASE", "REVISION", "REVOCATION DELAY", "STARTED", "PHASE AT", "ERROR", "NOT CONVERGED"},
		Rows:    rows,
		JSON:    list,
	}
}

func runCredentialRotation(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("rotation", args, "create", "list", "show", "pause", "resume", "delete", "run-now", "runs")
	if err != nil {
		return err
	}
	var format, provider, key, usernameKey, interval, delay string
	var expectedGeneration int64
	var includeDisabled, skipOverlap, confirmDestructive, abandon bool
	var limit int
	var after string
	st, flags, err := parseCommon("rotation "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "runs" {
			fs.IntVar(&limit, "limit", 0, "runs per page, 1 to 500 (0 = the server default of 100)")
			fs.StringVar(&after, "after", "", "continue after this run id, as a previous page printed it")
		}
		if sub == "run-now" {
			fs.BoolVar(&skipOverlap, "skip-overlap", false, "revoke the superseded credential right after the new one is published, with no overlap")
			fs.BoolVar(&confirmDestructive, "confirm-destructive", false, "confirm --skip-overlap: a consumer that has not picked up the new credential loses access")
		}
		if sub == "delete" {
			fs.BoolVar(&abandon, "abandon", false, "close the rotation's unsettled runs instead of refusing; nothing already delivered is revoked")
		}
		if sub == "list" {
			fs.BoolVar(&includeDisabled, "include-disabled", false, "also list deleted rotations (history)")
			fs.IntVar(&limit, "limit", 0, "rotations per page, 1 to 500 (0 = the server default of 100)")
			fs.StringVar(&after, "after", "", "continue after this rotation id, as a previous page printed it")
		}
		if sub == "create" {
			fs.StringVar(&provider, "provider", "", "the dynamic provider that mints the credential")
			fs.StringVar(&key, "key", "", "the secret key that receives the password")
			fs.StringVar(&usernameKey, "username-key", "", "the key that receives the generated role name")
			fs.StringVar(&interval, "interval", "", "time between rotations, 1h to 90d, e.g. 30d")
			fs.StringVar(&delay, "revocation-delay", "", "how long the superseded credential stays valid, 5m to 7d, e.g. 1d")
		}
		if sub == "pause" || sub == "resume" || sub == "delete" || sub == "run-now" {
			fs.Int64Var(&expectedGeneration, "expected-generation", 0, "fail unless the rotation is at this generation (0 = current)")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	client, artifact, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	projectPath, err := projectBase(resolved)
	if err != nil {
		return err
	}
	env, err := resolved.Require(DimEnv)
	if err != nil {
		return err
	}
	base := projectPath + "/environments/" + url.PathEscape(env) + "/rotations"
	// The publish ceremony is over the keys the rotation records, which the show
	// route names.
	keysOf := func(id string) func(context.Context, string) ([]string, error) {
		return func(ctx context.Context, _ string) ([]string, error) {
			var cur apigen.CredentialRotation
			if err := client.Do(ctx, http.MethodGet, base+"/"+url.PathEscape(id), nil, &cur); err != nil {
				return nil, err
			}
			return []string{string(cur.KeyId), string(cur.UsernameKeyId)}, nil
		}
	}
	one := func(out apigen.CredentialRotation) error {
		return Render(ios.Stdout, f, credentialRotationTable(apigen.CredentialRotationList{Items: []apigen.CredentialRotation{out}}))
	}

	switch sub {
	case "list":
		if err := flags.checkNoPositionals("rotation list"); err != nil {
			return err
		}
		query := url.Values{}
		if includeDisabled {
			query.Set("include_disabled", "true")
		}
		if limit != 0 {
			query.Set("limit", strconv.Itoa(limit))
		}
		if after != "" {
			query.Set("after", after)
		}
		target := base
		if len(query) > 0 {
			target += "?" + query.Encode()
		}
		var out apigen.CredentialRotationList
		if err := client.Do(ctx, http.MethodGet, target, nil, &out); err != nil {
			return err
		}
		if err := Render(ios.Stdout, f, credentialRotationTable(out)); err != nil {
			return err
		}
		if out.NextAfter != nil {
			_, _ = fmt.Fprintf(ios.Stderr, "more rotations follow; continue with --after %s\n", *out.NextAfter)
		}
		return nil
	case "show":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "rotation show requires a rotation id")
		}
		var out apigen.CredentialRotation
		if err := client.Do(ctx, http.MethodGet, base+"/"+url.PathEscape(id), nil, &out); err != nil {
			return err
		}
		return one(out)
	case "create":
		if err := flags.checkNoPositionals("rotation create"); err != nil {
			return err
		}
		if provider == "" || key == "" || usernameKey == "" || interval == "" || delay == "" {
			return failf(ExitUsage, "rotation create requires --provider, --key, --username-key, --interval and --revocation-delay")
		}
		intervalSeconds, err := durationSeconds("--interval", interval, false)
		if err != nil {
			return err
		}
		delaySeconds, err := durationSeconds("--revocation-delay", delay, false)
		if err != nil {
			return err
		}
		body := apigen.CreateRotationRequest{
			ProviderId: apigen.ID(provider), KeyId: apigen.ID(key), UsernameKeyId: apigen.ID(usernameKey),
			IntervalSeconds: intervalSeconds, RevocationDelaySeconds: delaySeconds,
		}
		var out apigen.CredentialRotation
		act := func() error { return client.Do(ctx, http.MethodPost, base, body, &out) }
		if err := withRotationCeremony(ctx, client, st, ios, artifact, projectPath, env, []string{key, usernameKey}, act); err != nil {
			return err
		}
		return one(out)
	case "pause", "resume":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "rotation %s requires a rotation id", sub)
		}
		var body apigen.RotationStateRequest
		if expectedGeneration != 0 {
			body.ExpectedGeneration = &expectedGeneration
		}
		var out apigen.CredentialRotation
		act := func() error { return client.Do(ctx, http.MethodPost, base+"/"+url.PathEscape(id)+"/"+sub, body, &out) }
		if sub == "resume" {
			// Resume is a human act like create.
			err = withRevealCeremony(ctx, client, st, ios, artifact, projectPath, []string{env}, disclosure{purpose: "publish", keys: keysOf(id)}, act)
		} else {
			err = act()
		}
		if err != nil {
			return err
		}
		if sub == "pause" {
			noteCredentialExpiry(ios, out, "paused")
		}
		return one(out)
	case "run-now":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "rotation run-now requires a rotation id")
		}
		if skipOverlap && !confirmDestructive {
			return failf(ExitUsage, "rotation run-now --skip-overlap revokes the superseded credential right after the new one is published, so a consumer that has not picked up the new credential loses access; pass --confirm-destructive to proceed")
		}
		var body apigen.RunRotationNowRequest
		if expectedGeneration != 0 {
			body.ExpectedGeneration = &expectedGeneration
		}
		if skipOverlap {
			body.SkipOverlap = &skipOverlap
		}
		var out apigen.RotationRunRequested
		act := func() error {
			return client.Do(ctx, http.MethodPost, base+"/"+url.PathEscape(id)+"/run-now", body, &out)
		}
		if err := withRevealCeremony(ctx, client, st, ios, artifact, projectPath, []string{env}, disclosure{purpose: "publish", keys: keysOf(id)}, act); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(ios.Stderr, "run %s requested; the executor starts it shortly (hikyo rotation runs %s)\n", out.RunId, out.RotationId)
		return Render(ios.Stdout, f, Table{
			Columns: []string{"ROTATION", "RUN", "SKIP OVERLAP"},
			Rows:    [][]string{{string(out.RotationId), string(out.RunId), strconv.FormatBool(out.SkipOverlap)}},
			JSON:    out,
		})
	case "runs":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "rotation runs requires a rotation id")
		}
		query := url.Values{}
		if limit != 0 {
			query.Set("limit", strconv.Itoa(limit))
		}
		if after != "" {
			query.Set("after", after)
		}
		target := base + "/" + url.PathEscape(id) + "/runs"
		if len(query) > 0 {
			target += "?" + query.Encode()
		}
		var out apigen.RotationRunList
		if err := client.Do(ctx, http.MethodGet, target, nil, &out); err != nil {
			return err
		}
		if err := Render(ios.Stdout, f, rotationRunsTable(out)); err != nil {
			return err
		}
		if out.NextAfter != nil {
			_, _ = fmt.Fprintf(ios.Stderr, "more runs follow; continue with --after %s\n", *out.NextAfter)
		}
		return nil
	case "delete":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "rotation delete requires a rotation id")
		}
		query := url.Values{}
		if expectedGeneration != 0 {
			query.Set("expected_generation", strconv.FormatInt(expectedGeneration, 10))
		}
		if abandon {
			query.Set("abandon", "true")
		}
		target := base + "/" + url.PathEscape(id)
		if len(query) > 0 {
			target += "?" + query.Encode()
		}
		var out apigen.CredentialRotation
		if err := client.Do(ctx, http.MethodDelete, target, nil, &out); err != nil {
			return err
		}
		noteCredentialExpiry(ios, out, "deleted")
		return one(out)
	}
	return failf(ExitUsage, "unknown rotation verb %q", sub)
}

// withRotationCeremony runs a create under the publish ceremony over both keys:
// an unattended publish can never satisfy a per-publish ceremony, so the person
// configuring the rotation does, here.
func withRotationCeremony(ctx context.Context, client *Client, st *State, ios IO, artifact AuthArtifact,
	projectPath, env string, keys []string, act func() error) error {
	d := disclosure{purpose: "publish", keys: func(context.Context, string) ([]string, error) { return keys, nil }}
	return withRevealCeremony(ctx, client, st, ios, artifact, projectPath, []string{env}, d, act)
}

// noteCredentialExpiry says, on stderr, when the credential already delivered
// into the keys lapses, because pausing or deleting a rotation does not revoke
// it. Nothing is printed until a run has published a credential.
func noteCredentialExpiry(ios IO, r apigen.CredentialRotation, verb string) {
	if r.CredentialExpiresAt == nil {
		return
	}
	_, _ = fmt.Fprintf(ios.Stderr, "rotation %s %s; the credential already delivered stays valid until %s\n",
		r.Id, verb, r.CredentialExpiresAt.UTC().Format(time.RFC3339))
}
