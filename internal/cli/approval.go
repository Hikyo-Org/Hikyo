package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// Secret-change approvals (#151). `approval policy …` administers the scoped
// policies (project authority); `approval request …` drives the review queue
// (environment authority). Merge and bypass call the ordinary publish endpoint
// with the request id, never a second mutation path.

// approverList is a repeatable --approver flag: `principal:<id>` or
// `group:<groupId>:<bindingId>`.
type approverList []apigen.ApprovalApprover

func (a *approverList) String() string { return "" }
func (a *approverList) Set(v string) error {
	parts := strings.Split(v, ":")
	switch parts[0] {
	case "principal":
		if len(parts) != 2 || parts[1] == "" {
			return fmt.Errorf("approver %q: want principal:<id>", v)
		}
		*a = append(*a, apigen.ApprovalApprover{Kind: apigen.ApprovalApproverKind("principal"), SubjectId: parts[1]})
	case "group":
		if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
			return fmt.Errorf("approver %q: want group:<groupId>:<bindingId>", v)
		}
		binding := parts[2]
		*a = append(*a, apigen.ApprovalApprover{Kind: apigen.ApprovalApproverKind("scim_group"), SubjectId: parts[1], BindingId: &binding})
	default:
		return fmt.Errorf("approver %q: kind must be principal or group", v)
	}
	return nil
}

func runApproval(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("approval", args, "policy", "request")
	if err != nil {
		return err
	}
	switch sub {
	case "policy":
		return runApprovalPolicy(ctx, ios, rest)
	case "request":
		return runApprovalRequest(ctx, ios, rest)
	}
	return failf(ExitInternal, "hikyo approval: unhandled subverb %q", sub)
}

func runApprovalPolicy(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("approval policy", args, "list", "create", "update", "delete")
	if err != nil {
		return err
	}
	var format, env string
	var minApprovals, ttl int
	var allowSelf, disabled bool
	var approvers approverList
	var bypassers stringList
	st, flags, err := parseCommon("approval policy "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "create" || sub == "update" {
			fs.StringVar(&env, "covers", "", "environment id this policy covers; empty means every environment")
			fs.IntVar(&minApprovals, "min-approvals", 1, "approvals required to merge")
			fs.IntVar(&ttl, "ttl", 86400, "request expiry in seconds")
			fs.BoolVar(&allowSelf, "allow-self-approval", false, "let the requester approve their own change")
			fs.BoolVar(&disabled, "disabled", false, "create/leave the policy disabled")
			fs.Var(&approvers, "approver", "repeatable: principal:<id> or group:<groupId>:<bindingId>")
			fs.Var(&bypassers, "bypasser", "repeatable: principal id of an emergency bypasser")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	// Syntax before authentication: reject a malformed invocation before a
	// session is looked up, so the exit code is 2 regardless of login state.
	switch sub {
	case "list":
		if err := flags.checkNoPositionals("approval policy list"); err != nil {
			return err
		}
	case "update":
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "usage: hikyo approval policy update <policy> [flags]")
		}
	case "delete":
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "usage: hikyo approval policy delete <policy>")
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
	input := func() apigen.ApprovalPolicyInput {
		self := allowSelf
		in := apigen.ApprovalPolicyInput{
			EnvironmentId: &env, MinApprovals: int32(minApprovals), RequestTtlSeconds: int32(ttl),
			Enabled: !disabled, AllowSelfApproval: &self,
		}
		if len(approvers) > 0 {
			a := []apigen.ApprovalApprover(approvers)
			in.Approvers = &a
		}
		if len(bypassers) > 0 {
			b := []string(bypassers)
			in.Bypassers = &b
		}
		return in
	}
	switch sub {
	case "list":
		if err := flags.checkNoPositionals("approval policy list"); err != nil {
			return err
		}
		var out apigen.ApprovalPolicyList
		if err := client.Do(ctx, http.MethodGet, base+"/approval-policies", nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, approvalPolicyTable(out.Items))
	case "create":
		if err := flags.checkNoPositionals("approval policy create"); err != nil {
			return err
		}
		var out apigen.ApprovalPolicy
		if err := client.Do(ctx, http.MethodPost, base+"/approval-policies", input(), &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, approvalPolicyTable([]apigen.ApprovalPolicy{out}))
	case "update":
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "usage: hikyo approval policy update <policy> [flags]")
		}
		var out apigen.ApprovalPolicy
		if err := client.Do(ctx, http.MethodPut, base+"/approval-policies/"+url.PathEscape(flags.positional()), input(), &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, approvalPolicyTable([]apigen.ApprovalPolicy{out}))
	case "delete":
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "usage: hikyo approval policy delete <policy>")
		}
		return client.Do(ctx, http.MethodDelete, base+"/approval-policies/"+url.PathEscape(flags.positional()), nil, nil)
	}
	return failf(ExitInternal, "hikyo approval policy: unhandled subverb %q", sub)
}

func approvalPolicyTable(policies []apigen.ApprovalPolicy) Table {
	rows := make([][]string, 0, len(policies))
	for _, p := range policies {
		env := p.EnvironmentId
		if env == "" {
			env = "(all)"
		}
		rows = append(rows, []string{p.Id, env, strconv.Itoa(int(p.MinApprovals)),
			strconv.FormatBool(p.AllowSelfApproval), strconv.Itoa(int(p.RequestTtlSeconds)),
			strconv.FormatBool(p.Enabled), strconv.Itoa(len(p.Approvers)), strconv.Itoa(len(p.Bypassers))})
	}
	return Table{
		Columns: []string{"ID", "ENVIRONMENT", "MIN", "SELF", "TTL", "ENABLED", "APPROVERS", "BYPASSERS"},
		Rows:    rows, JSON: apigen.ApprovalPolicyList{Items: policies},
	}
}

func runApprovalRequest(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("approval request", args, "list", "approve", "reject", "merge", "bypass")
	if err != nil {
		return err
	}
	var format, reason string
	st, flags, err := parseCommon("approval request "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "bypass" {
			fs.StringVar(&reason, "reason", "", "why the approval quorum is being bypassed (required)")
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
		if err := flags.checkNoPositionals("approval request list"); err != nil {
			return err
		}
	case "approve", "reject", "merge", "bypass":
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "usage: hikyo approval request %s <request>", sub)
		}
		if sub == "bypass" && reason == "" {
			return failf(ExitUsage, "hikyo approval request bypass requires --reason")
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
	base, err := environmentBase(project, resolved, flags, "approval request "+sub)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		if err := flags.checkNoPositionals("approval request list"); err != nil {
			return err
		}
		var out apigen.ApprovalRequestList
		if err := client.Do(ctx, http.MethodGet, base+"/approval-requests", nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, approvalRequestTable(out.Items))
	case "approve", "reject":
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "usage: hikyo approval request %s <request>", sub)
		}
		var out apigen.ApprovalRequest
		id := flags.positional()
		body := apigen.ApprovalVoteRequest{Decision: apigen.ApprovalVoteRequestDecision(sub)}
		act := func() error {
			return client.Do(ctx, http.MethodPost,
				base+"/approval-requests/"+url.PathEscape(id)+"/vote", body, &out)
		}
		purpose := sub
		if err := withRevealCeremony(ctx, client, st, ios, artifact, project,
			[]string{resolved.Get(DimEnv)}, approvalDisclosure(client, base, id, purpose), act); err != nil {
			return err
		}
		return Render(ios.Stdout, f, approvalRequestTable([]apigen.ApprovalRequest{out}))
	case "merge", "bypass":
		if len(flags.positionals) != 1 {
			return failf(ExitUsage, "usage: hikyo approval request %s <request>", sub)
		}
		id := flags.positional()
		body := apigen.PublishRequest{ApprovalRequestId: &id}
		if sub == "bypass" {
			if reason == "" {
				return failf(ExitUsage, "hikyo approval request bypass requires --reason")
			}
			body.Bypass = &apigen.ApprovalBypass{Reason: reason}
		}
		var out apigen.PublishResult
		act := func() error { return client.Do(ctx, http.MethodPost, base+"/publish", body, &out) }
		purpose := "publish"
		if sub == "bypass" {
			purpose = "bypass"
		}
		if err := withRevealCeremony(ctx, client, st, ios, artifact, project,
			[]string{resolved.Get(DimEnv)}, approvalDisclosure(client, base, id, purpose), act); err != nil {
			return err
		}
		return Render(ios.Stdout, f, Table{
			Columns: []string{"PUBLISHED", "ENVIRONMENTS"},
			Rows:    [][]string{{strconv.Itoa(len(out.Published)), strconv.Itoa(len(out.Environments))}},
			JSON:    out,
		})
	}
	return failf(ExitInternal, "hikyo approval request: unhandled subverb %q", sub)
}

// approvalDisclosure resolves the immutable key set only if a zero-window
// browser handoff is needed. The action is attempted first, so a live sliding
// window and an idempotent repeated vote cost no extra queue read.
func approvalDisclosure(client *Client, environmentBase, requestID, purpose string) disclosure {
	var cached *apigen.ApprovalCeremonyBinding
	load := func(ctx context.Context) (apigen.ApprovalCeremonyBinding, error) {
		if cached != nil {
			return *cached, nil
		}
		var binding apigen.ApprovalCeremonyBinding
		if err := client.Do(ctx, http.MethodGet,
			environmentBase+"/approval-requests/"+url.PathEscape(requestID)+"/ceremony", nil, &binding); err != nil {
			return apigen.ApprovalCeremonyBinding{}, err
		}
		cached = &binding
		return binding, nil
	}
	return disclosure{
		purpose: purpose,
		keys: func(ctx context.Context, _ string) ([]string, error) {
			binding, err := load(ctx)
			return binding.KeyIds, err
		},
		window: func(ctx context.Context, _ string) (apigen.RevealWindow, error) {
			binding, err := load(ctx)
			return binding.Window, err
		},
	}
}

func approvalRequestTable(requests []apigen.ApprovalRequest) Table {
	rows := make([][]string, 0, len(requests))
	for _, r := range requests {
		rows = append(rows, []string{r.Id, r.Requester, string(r.State),
			fmt.Sprintf("%d/%d", r.Approvals, r.MinApprovals), strconv.Itoa(int(r.ChangeCount)),
			r.ExpiresAt.UTC().Format(time.RFC3339)})
	}
	return Table{
		Columns: []string{"ID", "REQUESTER", "STATE", "APPROVALS", "CHANGES", "EXPIRES"},
		Rows:    rows, JSON: apigen.ApprovalRequestList{Items: requests},
	}
}
