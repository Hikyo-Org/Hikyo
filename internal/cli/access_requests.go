package cli

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// Approval-mediated temporary access (#152). `access policy …` administers
// what may be requested (project authority, manage-members); `access request
// …` files, decides, withdraws and ends requests in one environment. Every
// verb is human-session only.

// capabilityList is a repeatable --capability flag.
type capabilityList []apigen.AccessCapability

// String returns an empty default display for the repeatable capability flag.
func (c *capabilityList) String() string { return "" }

// Set appends trimmed, comma-separated values without validating capability
// names. An empty value returns an error; preceding values remain appended.
func (c *capabilityList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("capability %q: empty", v)
		}
		*c = append(*c, apigen.AccessCapability(part))
	}
	return nil
}

// runAccessPolicy validates and executes project policy commands, rendering
// list and write results in the requested format. Updates preserve omitted fields
// from the current policy. Usage, authentication, HTTP, and rendering errors
// are returned to the CLI.
func runAccessPolicy(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("access policy", args, "list", "create", "update", "delete")
	if err != nil {
		return err
	}
	var format, env string
	var minApprovals, ttl int
	var maxDuration time.Duration
	var allowSelf, disabled, clearBypassers bool
	var caps capabilityList
	var approvers approverList
	var bypassers stringList
	var policyFlags *flag.FlagSet
	st, flags, err := parseCommon("access policy "+sub, ios, rest, func(fs *flag.FlagSet) {
		policyFlags = fs
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "create" || sub == "update" {
			fs.StringVar(&env, "covers", "", "environment id this policy covers; empty means every environment")
			fs.Var(&caps, "capability", "repeatable (or comma-separated): read, reveal, reveal-history, edit, publish, pin")
			fs.DurationVar(&maxDuration, "max-duration", 8*time.Hour, "longest temporary access a request may ask for")
			fs.IntVar(&minApprovals, "min-approvals", 1, "approvals required to grant")
			fs.IntVar(&ttl, "ttl", 86400, "seconds a request may wait for a decision")
			fs.BoolVar(&allowSelf, "allow-self-approval", false, "let the requester approve their own request")
			fs.BoolVar(&disabled, "disabled", false, "create/leave the policy disabled")
			fs.Var(&approvers, "approver", "repeatable: principal:<id> or group:<groupId>:<bindingId>")
			fs.Var(&bypassers, "bypasser", "repeatable: principal id allowed to take emergency access")
			fs.BoolVar(&clearBypassers, "clear-bypassers", false, "remove all emergency-access bypassers")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	// Syntax before authentication.
	switch sub {
	case "list":
		if err := flags.checkNoPositionals("access policy list"); err != nil {
			return err
		}
	case "create":
		if err := flags.checkNoPositionals("access policy create"); err != nil {
			return err
		}
	case "update", "delete":
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "usage: hikyo access policy %s <policy>", sub)
		}
	}
	if sub == "create" && (len(caps) == 0 || len(approvers) == 0) {
		return failf(ExitUsage, "hikyo access policy %s requires at least one --capability and one --approver", sub)
	}
	if (sub == "create" || sub == "update") && (maxDuration < time.Second || maxDuration/time.Second > math.MaxInt32) {
		return failf(ExitUsage, "hikyo access policy %s: --max-duration must be between 1s and %d seconds", sub, math.MaxInt32)
	}
	if (sub == "create" || sub == "update") && (minApprovals < 1 || minApprovals > math.MaxInt32 || ttl < 1 || ttl > math.MaxInt32) {
		return failf(ExitUsage, "hikyo access policy %s: --min-approvals and --ttl must be between 1 and %d", sub, math.MaxInt32)
	}
	if clearBypassers && len(bypassers) > 0 {
		return failf(ExitUsage, "--clear-bypassers cannot be combined with --bypasser")
	}
	set := make(map[string]bool)
	policyFlags.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if sub == "update" {
		changed := false
		for _, name := range []string{"covers", "capability", "approver", "max-duration", "min-approvals", "ttl", "allow-self-approval", "disabled", "bypasser", "clear-bypassers"} {
			changed = changed || set[name]
		}
		if !changed {
			return failf(ExitUsage, "access policy update requires at least one policy flag")
		}
	}
	client, _, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	base, err := projectBase(resolved)
	if err != nil {
		return err
	}
	input := func() apigen.AccessPolicyInput {
		self := allowSelf
		in := apigen.AccessPolicyInput{
			EnvironmentId: &env, Capabilities: []apigen.AccessCapability(caps),
			MaxDurationSeconds: int32(maxDuration / time.Second), MinApprovals: int32(minApprovals),
			RequestTtlSeconds: int32(ttl), Enabled: !disabled, AllowSelfApproval: &self,
			Approvers: []apigen.ApprovalApprover(approvers),
		}
		if len(bypassers) > 0 {
			b := []string(bypassers)
			in.Bypassers = &b
		}
		return in
	}
	switch sub {
	case "list":
		var out apigen.AccessPolicyList
		if err := client.Do(ctx, http.MethodGet, base+"/access-policies", nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, accessPolicyTable(out.Items))
	case "create":
		var out apigen.AccessPolicy
		if err := client.Do(ctx, http.MethodPost, base+"/access-policies", input(), &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, accessPolicyTable([]apigen.AccessPolicy{out}))
	case "update":
		var current apigen.AccessPolicyList
		if err := client.Do(ctx, http.MethodGet, base+"/access-policies", nil, &current); err != nil {
			return err
		}
		for _, policy := range current.Items {
			if policy.Id != flags.positional() {
				continue
			}
			body := mergeAccessPolicyInput(policy, input(), set)
			if clearBypassers {
				empty := []string{}
				body.Bypassers = &empty
			}
			var out apigen.AccessPolicy
			if err := client.Do(ctx, http.MethodPut, base+"/access-policies/"+url.PathEscape(flags.positional()), body, &out); err != nil {
				return err
			}
			return Render(ios.Stdout, f, accessPolicyTable([]apigen.AccessPolicy{out}))
		}
		return failf(ExitRefused, "access policy not found")
	case "delete":
		return client.Do(ctx, http.MethodDelete, base+"/access-policies/"+url.PathEscape(flags.positional()), nil, nil)
	}
	return failf(ExitInternal, "hikyo access policy: unhandled subverb %q", sub)
}

// mergeAccessPolicyInput keeps defaults from broadening an update. Explicit
// false booleans and an explicit empty --covers remain meaningful changes.
func mergeAccessPolicyInput(current apigen.AccessPolicy, requested apigen.AccessPolicyInput, set map[string]bool) apigen.AccessPolicyInput {
	if !set["covers"] {
		requested.EnvironmentId = &current.EnvironmentId
	}
	if !set["capability"] {
		requested.Capabilities = current.Capabilities
	}
	if !set["approver"] {
		requested.Approvers = current.Approvers
	}
	if !set["max-duration"] {
		requested.MaxDurationSeconds = current.MaxDurationSeconds
	}
	if !set["min-approvals"] {
		requested.MinApprovals = current.MinApprovals
	}
	if !set["ttl"] {
		requested.RequestTtlSeconds = current.RequestTtlSeconds
	}
	if !set["allow-self-approval"] {
		requested.AllowSelfApproval = &current.AllowSelfApproval
	}
	if !set["disabled"] {
		requested.Enabled = current.Enabled
	}
	if !set["bypasser"] {
		requested.Bypassers = &current.Bypassers
	}
	return requested
}

// accessPolicyTable renders project-wide coverage as "(all)" and durations
// in Go duration notation, retaining the policy list for JSON output.
func accessPolicyTable(policies []apigen.AccessPolicy) Table {
	rows := make([][]string, 0, len(policies))
	for _, p := range policies {
		env := p.EnvironmentId
		if env == "" {
			env = "(all)"
		}
		caps := make([]string, 0, len(p.Capabilities))
		for _, c := range p.Capabilities {
			caps = append(caps, string(c))
		}
		rows = append(rows, []string{p.Id, env, strings.Join(caps, ","),
			(time.Duration(p.MaxDurationSeconds) * time.Second).String(), strconv.Itoa(int(p.MinApprovals)),
			strconv.FormatBool(p.Enabled), strconv.Itoa(len(p.Approvers)), strconv.Itoa(len(p.Bypassers))})
	}
	return Table{
		Columns: []string{"ID", "ENVIRONMENT", "CAPABILITIES", "MAX", "MIN", "ENABLED", "APPROVERS", "BYPASSERS"},
		Rows:    rows, JSON: apigen.AccessPolicyList{Items: policies},
	}
}

// runAccessRequest executes temporary-access commands in one environment.
// Emergency access retries through reauthentication when required; a zero
// duration leaves the default to the server. Usage, authentication, HTTP,
// ceremony, and rendering errors are returned to the CLI.
func runAccessRequest(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("access request", args, "list", "create", "approve", "reject", "cancel", "revoke", "emergency")
	if err != nil {
		return err
	}
	var format, reason string
	var duration time.Duration
	var caps capabilityList
	st, flags, err := parseCommon("access request "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "create" || sub == "emergency" {
			fs.Var(&caps, "capability", "repeatable (or comma-separated): the capabilities needed")
			fs.StringVar(&reason, "reason", "", "why the access is needed (required)")
			fs.DurationVar(&duration, "duration", 0, "how long the access should last (emergency defaults to 1h)")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	// Syntax before authentication.
	switch sub {
	case "list":
		if err := flags.checkNoPositionals("access request list"); err != nil {
			return err
		}
	case "create", "emergency":
		if err := flags.checkNoPositionals("access request " + sub); err != nil {
			return err
		}
		if len(caps) == 0 || reason == "" {
			return failf(ExitUsage, "hikyo access request %s requires --capability and --reason", sub)
		}
		if sub == "create" && duration < time.Second {
			return failf(ExitUsage, "hikyo access request create requires --duration of at least 1s")
		}
		if duration < 0 || duration/time.Second > math.MaxInt32 {
			return failf(ExitUsage, "hikyo access request %s: --duration must be at most %d seconds", sub, math.MaxInt32)
		}
	default:
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "usage: hikyo access request %s <request>", sub)
		}
	}
	client, artifact, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	project, err := projectBase(resolved)
	if err != nil {
		return err
	}
	base, err := environmentBase(project, resolved, flags, "access request "+sub)
	if err != nil {
		return err
	}
	var out apigen.AccessRequest
	switch sub {
	case "list":
		var queue apigen.AccessQueue
		if err := client.Do(ctx, http.MethodGet, base+"/access-requests", nil, &queue); err != nil {
			return err
		}
		return Render(ios.Stdout, f, accessRequestTable(queue.Items, &queue))
	case "create":
		body := accessRequestBody(caps, reason, duration)
		if err := client.Do(ctx, http.MethodPost, base+"/access-requests", body, &out); err != nil {
			return err
		}
	case "emergency":
		body := emergencyAccessBody(caps, reason, duration)
		act := func() error { return client.Do(ctx, http.MethodPost, base+"/access-requests/emergency", body, &out) }
		// The emergency decision is bound to the environment alone. Where the
		// environment's window slides, the inline TOTP ceremony opens it; a
		// passkey-only (window 0) environment needs the browser.
		d := disclosure{purpose: "access"}
		if err := withRevealCeremony(ctx, client, st, ios, artifact, project,
			[]string{resolved.Get(DimEnv)}, d, act); err != nil {
			return err
		}
	case "approve", "reject":
		body := apigen.AccessVoteRequest{Decision: apigen.AccessVoteRequestDecision(sub)}
		if err := client.Do(ctx, http.MethodPost,
			base+"/access-requests/"+url.PathEscape(flags.positional())+"/vote", body, &out); err != nil {
			return err
		}
	case "cancel", "revoke":
		if err := client.Do(ctx, http.MethodPost,
			base+"/access-requests/"+url.PathEscape(flags.positional())+"/"+sub, nil, &out); err != nil {
			return err
		}
	default:
		return failf(ExitInternal, "hikyo access request: unhandled subverb %q", sub)
	}
	return Render(ios.Stdout, f, accessRequestTable([]apigen.AccessRequest{out}, nil))
}

// accessRequestBody converts a validated duration to whole seconds, discarding
// any fractional second. The caller must ensure it fits in an int32.
func accessRequestBody(caps capabilityList, reason string, duration time.Duration) apigen.AccessRequestInput {
	return apigen.AccessRequestInput{Capabilities: []apigen.AccessCapability(caps), Reason: reason, DurationSeconds: int32(duration / time.Second)}
}

// emergencyAccessBody leaves the duration absent when unset so the server
// applies its one-hour default capped by the policy. Positive durations are
// truncated to whole seconds; the caller must ensure they fit in an int32.
func emergencyAccessBody(caps capabilityList, reason string, duration time.Duration) apigen.EmergencyAccessInput {
	body := apigen.EmergencyAccessInput{Capabilities: []apigen.AccessCapability(caps), Reason: reason}
	if duration > 0 {
		seconds := int32(duration / time.Second)
		body.DurationSeconds = &seconds
	}
	return body
}

// accessRequestTable renders request state and UTC expiry. JSON output uses
// the supplied queue, including its offer, or wraps requests in a queue when nil.
func accessRequestTable(requests []apigen.AccessRequest, queue *apigen.AccessQueue) Table {
	rows := make([][]string, 0, len(requests))
	for _, r := range requests {
		caps := make([]string, 0, len(r.Capabilities))
		for _, c := range r.Capabilities {
			caps = append(caps, string(c))
		}
		state := string(r.State)
		if r.Bypassed {
			state += " (emergency)"
		}
		until := "-"
		if r.ExpiresAt != nil {
			until = r.ExpiresAt.UTC().Format(time.RFC3339)
		}
		rows = append(rows, []string{r.Id, r.Requester, strings.Join(caps, ","), state,
			fmt.Sprintf("%d/%d", r.Approvals, r.MinApprovals), until})
	}
	var json any = apigen.AccessQueue{Items: requests}
	if queue != nil {
		json = *queue
	}
	return Table{
		Columns: []string{"ID", "REQUESTER", "CAPABILITIES", "STATE", "APPROVALS", "EXPIRES"},
		Rows:    rows, JSON: json,
	}
}
