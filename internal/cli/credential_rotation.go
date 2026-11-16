package cli

import (
	"context"
	"flag"
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
		username := "-"
		if r.UsernameKeyId != nil {
			username = *r.UsernameKeyId
		}
		rows = append(rows, []string{
			string(r.Id), string(r.State), string(r.ProviderId), string(r.KeyId), username,
			(time.Duration(r.IntervalSeconds) * time.Second).String(),
			(time.Duration(r.RevocationDelaySeconds) * time.Second).String(),
			next, strconv.FormatInt(r.Generation, 10),
		})
	}
	return Table{
		// No credential column: this record holds configuration only; no password
		// or role handle is stored or returned.
		Columns: []string{"ID", "STATE", "PROVIDER", "KEY", "USERNAME KEY", "INTERVAL", "REVOCATION DELAY", "NEXT RUN", "GENERATION"},
		Rows:    rows,
		JSON:    list,
	}
}

func runCredentialRotation(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("rotation", args, "create", "list", "show", "pause", "resume", "delete")
	if err != nil {
		return err
	}
	var format, provider, key, usernameKey, interval, delay string
	var expectedGeneration int64
	st, flags, err := parseCommon("rotation "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "create" {
			fs.StringVar(&provider, "provider", "", "the dynamic provider that mints the credential")
			fs.StringVar(&key, "key", "", "the secret key that receives the password")
			fs.StringVar(&usernameKey, "username-key", "", "the key that receives the generated role name")
			fs.StringVar(&interval, "interval", "", "time between rotations, 1h to 90d, e.g. 720h")
			fs.StringVar(&delay, "revocation-delay", "", "how long the superseded credential stays valid, 5m to 7d")
		}
		if sub == "pause" || sub == "resume" || sub == "delete" {
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
	base := adapterBase(org, project) + "/environments/" + url.PathEscape(env) + "/rotations"
	one := func(out apigen.CredentialRotation) error {
		return Render(ios.Stdout, f, credentialRotationTable(apigen.CredentialRotationList{Items: []apigen.CredentialRotation{out}}))
	}

	switch sub {
	case "list":
		if err := flags.checkNoPositionals("rotation list"); err != nil {
			return err
		}
		var out apigen.CredentialRotationList
		if err := client.Do(ctx, http.MethodGet, base, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, credentialRotationTable(out))
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
		intervalSeconds, err := durationSeconds("--interval", interval)
		if err != nil {
			return err
		}
		delaySeconds, err := durationSeconds("--revocation-delay", delay)
		if err != nil {
			return err
		}
		body := apigen.CreateRotationRequest{
			ProviderId: apigen.ID(provider), KeyId: apigen.ID(key), UsernameKeyId: apigen.ID(usernameKey),
			IntervalSeconds: intervalSeconds, RevocationDelaySeconds: delaySeconds,
		}
		var out apigen.CredentialRotation
		if err := client.Do(ctx, http.MethodPost, base, body, &out); err != nil {
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
		if err := client.Do(ctx, http.MethodPost, base+"/"+url.PathEscape(id)+"/"+sub, body, &out); err != nil {
			return err
		}
		return one(out)
	case "delete":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "rotation delete requires a rotation id")
		}
		target := base + "/" + url.PathEscape(id)
		if expectedGeneration != 0 {
			target += "?expected_generation=" + strconv.FormatInt(expectedGeneration, 10)
		}
		var out apigen.CredentialRotation
		if err := client.Do(ctx, http.MethodDelete, target, nil, &out); err != nil {
			return err
		}
		return one(out)
	}
	return failf(ExitUsage, "unknown rotation verb %q", sub)
}

// durationSeconds parses a positive duration into whole seconds. The server
// owns the bounds; this only refuses what cannot be a duration.
func durationSeconds(flagName, raw string) (int64, error) {
	d, err := time.ParseDuration(raw)
	if err != nil || d < time.Second {
		return 0, failf(ExitUsage, "%s must be a duration of at least one second, e.g. 24h", flagName)
	}
	return int64(d / time.Second), nil
}
