package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"slices"
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
		// The cell follows the state, like the HALTED line: a halted rotation always
		// says why, a missing reason reading as unknown.
		halt := "-"
		if r.State == apigen.CredentialRotationStateHalted {
			halt = haltReasonWord(r.HaltReason)
		}
		state := string(r.State)
		if r.State == apigen.CredentialRotationStateDisabled {
			state = "deleted"
		}
		last := "-"
		if r.LastRun != nil {
			last = runWord(string(r.LastRun.Phase), rotationRunPhaseWords)
			if r.LastRun.ErrorCode != nil {
				last += " (" + runWord(*r.LastRun.ErrorCode, rotationRunErrorWords) + ")"
			}
		}
		rows = append(rows, []string{
			string(r.Id), state, halt, string(r.ProviderId), string(r.KeyId), string(r.UsernameKeyId),
			(time.Duration(r.IntervalSeconds) * time.Second).String(),
			(time.Duration(r.RevocationDelaySeconds) * time.Second).String(),
			next, expires, strconv.FormatInt(r.Generation, 10), last, strconv.FormatInt(r.OwedDropRuns, 10),
		})
	}
	return Table{
		// No credential column: this record holds configuration only; no password
		// or role handle is stored or returned.
		Columns: []string{"ID", "STATE", "HALT REASON", "PROVIDER", "KEY", "USERNAME KEY", "INTERVAL", "REVOCATION DELAY", "NEXT RUN", "CREDENTIAL EXPIRES", "GENERATION", "LAST RUN", "OWED DROPS"},
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
			code = runWord(*r.ErrorCode, rotationRunErrorWords)
		}
		if r.NotConvergedAdapterTargets != nil {
			converged = strconv.FormatInt(*r.NotConvergedAdapterTargets, 10)
		}
		rows = append(rows, []string{
			string(r.Id), string(r.Trigger), runWord(string(r.Phase), rotationRunPhaseWords), revision,
			(time.Duration(r.RevocationDelaySeconds) * time.Second).String(),
			r.StartedAt.UTC().Format(time.RFC3339), r.PhaseAt.UTC().Format(time.RFC3339), code, converged,
		})
	}
	return Table{
		// Metadata only: no password, candidate or provider detail is returned.
		Columns: []string{"RUN", "TRIGGER", "PHASE", "REVISION", "REVOCATION DELAY", "STARTED", "PHASE AT", "ERROR", "NOT CONVERGED TARGETS"},
		Rows:    rows,
		JSON:    list,
	}
}

func runCredentialRotation(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("rotation", args, "create", "list", "show", "pause", "resume", "release-hold", "delete", "run-now", "runs")
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
			fs.BoolVar(&confirmDestructive, "confirm-destructive", false, "required: confirm the run publishes a new credential now; with --skip-overlap a consumer that has not picked it up loses access")
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
		if sub == "pause" || sub == "resume" || sub == "release-hold" || sub == "delete" || sub == "run-now" {
			fs.Int64Var(&expectedGeneration, "expected-generation", 0, "fail unless the rotation is at this generation (0 = current)")
		}
	})
	if err != nil {
		return err
	}
	// Once, before any login or request: list and create take no positional,
	// every other verb takes exactly one rotation id and a second would be
	// silently dropped (run-now publishes, delete closes the rotation).
	if sub == "list" || sub == "create" {
		err = flags.checkNoPositionals("rotation " + sub)
	} else {
		err = flags.checkTarget("rotation "+sub, "rotation id", "")
	}
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
		for _, r := range out.Items {
			noteRestoreHold(ios, r)
			noteHalted(ios, r)
			noteOwedSummary(ios, r)
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
		noteRestoreHold(ios, out)
		noteHalted(ios, out)
		noteOwedSummary(ios, out)
		// The rotation itself was read; a failure of the runs read only costs the
		// owed-drop warnings, so it is reported and never fails the show.
		noteAllOwedRuns(ctx, client, ios, base+"/"+url.PathEscape(id)+"/runs")
		return one(out)
	case "create":
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
	case "pause", "resume", "release-hold":
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
		if sub == "resume" || sub == "release-hold" {
			// Resume and release-hold are human acts like create.
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
		noteRestoreHold(ios, out)
		if sub == "release-hold" {
			_, _ = fmt.Fprintln(ios.Stderr, "released: the executor may now drop lingering unpublished candidate roles; check `hikyo rotation runs` first next time")
		}
		return one(out)
	case "run-now":
		id := flags.positional()
		if id == "" {
			return failf(ExitUsage, "rotation run-now requires a rotation id")
		}
		if !confirmDestructive {
			return failf(ExitUsage, "rotation run-now publishes a new credential now, outside the schedule (with --skip-overlap it also revokes the superseded one at once, so a consumer that has not picked up the new credential loses access); pass --confirm-destructive to proceed")
		}
		body := apigen.RunRotationNowRequest{ConfirmDestructive: true}
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
		// A held rotation is the first thing the runs list must say: none of the
		// runs below it will start another until the hold is released.
		// Reading the hold is secondary: when it fails the runs still render, and
		// the operator is told the hold status is unknown.
		var cur apigen.CredentialRotation
		if err := client.Do(ctx, http.MethodGet, base+"/"+url.PathEscape(id), nil, &cur); err != nil {
			_, _ = fmt.Fprintf(ios.Stderr, "WARNING: the rotation could not be read (%v), so its restore hold status is unknown; run `hikyo rotation show %s` to check it\n", err, id)
		} else {
			noteRestoreHold(ios, cur)
		}
		noteOwedRuns(ios, out)
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

// versionsChangedCode is the run error code of a drop the executor refuses because
// a restore may have put the superseded role back into service.
const versionsChangedCode = "published_versions_changed"

// maxOwedRunPages bounds how far show reads back through a rotation's runs.
const maxOwedRunPages = 20

// noteAllOwedRuns pages through a rotation's runs and notes every one that still
// owes a role drop. It never fails its caller: an unreadable page, or a history
// longer than maxOwedRunPages pages, is said on stderr instead, so the operator
// knows the warnings above may be incomplete.
func noteAllOwedRuns(ctx context.Context, client *Client, ios IO, runsPath string) {
	after := ""
	for range maxOwedRunPages {
		target := runsPath + "?limit=500"
		if after != "" {
			target += "&after=" + url.QueryEscape(after)
		}
		var page apigen.RotationRunList
		if err := client.Do(ctx, http.MethodGet, target, nil, &page); err != nil {
			_, _ = fmt.Fprintf(ios.Stderr, "WARNING: the runs could not be read (%v); run `hikyo rotation runs` to check for roles still owed a drop\n", err)
			return
		}
		noteOwedRuns(ios, page)
		if page.NextAfter == nil {
			return
		}
		after = *page.NextAfter
	}
	_, _ = fmt.Fprintf(ios.Stderr, "WARNING: only the newest %d runs were checked for roles still owed a drop; page through `hikyo rotation runs` for older ones\n", maxOwedRunPages*500)
}

// noteOwedRuns says loudly, on stderr, which runs still owe a role drop and until
// when that role stays valid: a revoke_refused run could not drop the superseded
// role and a quarantine run holds a candidate that may still be live. Hikyo has no
// operator action that releases either; the role lapses at the time printed unless
// the operator drops it at the provider, after checking that neither key serves it
// (a revision restore can put it back). A run refused as published_versions_changed
// is the case where a restore already did: it is not to be dropped until the keys
// have moved off it.
func noteOwedRuns(ios IO, runs apigen.RotationRunList) {
	for _, r := range runs.Items {
		if r.Phase != apigen.RotationRunPhaseRevokeRefused && r.Phase != apigen.RotationRunPhaseQuarantine {
			continue
		}
		until := "no recorded expiry"
		if r.CredentialValidUntil != nil {
			until = "stays valid until " + r.CredentialValidUntil.UTC().Format(time.RFC3339)
			if r.CredentialValidUntilLowerBound != nil && *r.CredentialValidUntilLowerBound {
				// An unsettled extend means the role may outlive the recorded time.
				until = "stays valid at least until " + r.CredentialValidUntil.UTC().Format(time.RFC3339) + ", and possibly longer: an extend of it is unsettled"
			}
		}
		if r.ErrorCode != nil && *r.ErrorCode == versionsChangedCode {
			// A restore put an older value back, so the keys may be serving this very
			// role again: dropping it now would cut off the consumers.
			_, _ = fmt.Fprintf(ios.Stderr, "ATTENTION: run %s is %s (%s): a restore put an earlier value back, so the keys may be serving the role it owes a drop again (%s); do NOT drop it at the provider until the keys have moved off it\n", r.Id, runWord(string(r.Phase), rotationRunPhaseWords), versionsChangedCode, until)
			continue
		}
		_, _ = fmt.Fprintf(ios.Stderr, "ATTENTION: run %s is %s and a provider role it owes a drop is still live (%s); before you drop it at the provider, check that neither key serves that role, because a revision restore can put it back\n", r.Id, runWord(string(r.Phase), rotationRunPhaseWords), until)
	}
}

// rotationRunPhaseWords and rotationRunErrorWords are the fixed words a run's phase
// and error code may carry (the server maps anything else to unknown). Like the halt
// reason, a value outside them is printed quoted: a newer server, or a bug, can never
// put control characters or a sentence on the terminal as if it were a word.
var rotationRunPhaseWords = []string{"staged", "minting", "minted", "published", "overlap", "revoking", "revoke_refused", "quarantine", "done", "failed"}

var rotationRunErrorWords = []string{
	"interrupted", "provider_unavailable", "mint_failed", "mint_ambiguous", "publish_failed",
	"publish_budget_exceeded", "rotation_paused", "rotation_held", "drop_failed", "role_owns_objects",
	"published_versions_changed", "revoke_failed", "extend_failed", "extend_skipped", "expired",
	"infrastructure_unavailable", "abandoned", "unknown",
}

// runWord prints a known word bare and anything else quoted.
func runWord(word string, known []string) string {
	if slices.Contains(known, word) {
		return word
	}
	return strconv.Quote(word)
}

// haltReasonSentence says in words why a halted rotation stopped. The sentences are
// the browser's (web/src/api/rotations.ts, haltReasonText) and the user docs'
// (rotation.mdx), word for word. The reason is one of the server's fixed words; any
// other value reads as unknown here and is printed quoted, never interpreted.
func haltReasonSentence(reason string) string {
	switch reason {
	case "retries_exhausted":
		return "the retry window ran out before a run could publish or extend the credential"
	case "authority_lost":
		return "the recorded person no longer holds the access the rotation needs, or one of its keys or its environment is gone"
	case "gate_failed":
		return "a pre-publish gate refused: the environment became protected, a key changed or a draft appeared"
	case "unknown":
		return "the server halted the rotation without a reason it can name"
	}
	return "the server recorded a reason this client does not know"
}

// haltReasonWord is the halt reason as printed: one of the server's fixed words
// bare, any other value quoted so it can never read as a sentence or carry control
// characters. A missing reason is the fixed word unknown.
func haltReasonWord(reason *string) string {
	word := "unknown"
	if reason != nil {
		word = *reason
	}
	switch word {
	case "retries_exhausted", "authority_lost", "gate_failed", "unknown":
		return word
	}
	return strconv.Quote(word)
}

// noteHalted says, on stderr, that the server halted the rotation and why.
func noteHalted(ios IO, r apigen.CredentialRotation) {
	if r.State != apigen.CredentialRotationStateHalted {
		return
	}
	word := haltReasonWord(r.HaltReason)
	_, _ = fmt.Fprintf(ios.Stderr, "HALTED: rotation %s is halted (%s): %s; it starts no run and does not extend the delivered credential until a person resumes it: hikyo rotation resume %s\n", r.Id, word, haltReasonSentence(word), r.Id)
}

// deletedMark qualifies a deleted rotation: it is gone from the list unless asked for
// but its roles can still be live, so a line about it says it was deleted.
func deletedMark(r apigen.CredentialRotation) string {
	if r.State == apigen.CredentialRotationStateDisabled {
		return " (deleted)"
	}
	return ""
}

// noteOwedSummary is the rotation-level ATTENTION line: some of its runs still owe a
// provider role a drop. It counts the runs noteOwedRuns warns about, by the same
// phases, and points at the per-run lines instead of repeating them. It carries the
// same key check as a per-run line: a restore (an earlier one that refused a run, or
// a restore hold that is still on) may have put the role back, so it never plainly
// says to drop and tells the operator to check that neither key serves the role first.
func noteOwedSummary(ios IO, r apigen.CredentialRotation) {
	if r.OwedDropRuns <= 0 {
		return
	}
	switch {
	case r.OwedDropRunsVersionsChanged > 0:
		_, _ = fmt.Fprintf(ios.Stderr, "ATTENTION: rotation %s%s has %d run(s) that owe a provider role a drop, %d of them refused after a restore (%s): the keys may be serving that role again, so do NOT drop it at the provider until the keys have moved off it; read: hikyo rotation runs %s\n", r.Id, deletedMark(r), r.OwedDropRuns, r.OwedDropRunsVersionsChanged, versionsChangedCode, r.Id)
	case r.RestoreHold:
		_, _ = fmt.Fprintf(ios.Stderr, "ATTENTION: rotation %s%s has %d run(s) that owe a provider role a drop and a restore holds the rotation: the keys may be serving that role again, so do NOT drop it at the provider until you have checked that neither key serves it; read: hikyo rotation runs %s\n", r.Id, deletedMark(r), r.OwedDropRuns, r.Id)
	default:
		_, _ = fmt.Fprintf(ios.Stderr, "ATTENTION: rotation %s%s has %d run(s) that owe a provider role a drop and the role is still live; before you drop it at the provider, check that neither key serves that role, because a revision restore can put it back; read: hikyo rotation runs %s\n", r.Id, deletedMark(r), r.OwedDropRuns, r.Id)
	}
}

// noteRestoreHold says loudly, on stderr, that a restore holds the rotation: it
// starts no run until released, and the credential already delivered keeps lapsing
// at the time printed, so the operator has until then.
func noteRestoreHold(ios IO, r apigen.CredentialRotation) {
	if !r.RestoreHold {
		return
	}
	until := "no recorded expiry"
	if r.CredentialExpiresAt != nil {
		until = "the delivered credential lapses at " + r.CredentialExpiresAt.UTC().Format(time.RFC3339)
	}
	_, _ = fmt.Fprintf(ios.Stderr, "RESTORE HOLD: rotation %s is held after a restore and starts no run (%s); reconcile its grants and provider credential, then run: hikyo rotation release-hold %s\n", r.Id, until, r.Id)
}
