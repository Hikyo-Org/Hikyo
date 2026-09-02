# Member Invitation (#568) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `manage-members` holder can invite a human at org or instance scope from the API, the CLI and the WebUI; the invitee claims the display-once authority on a public `/establish` page and signs in.

**Architecture:** One service method `Grants.InviteMember` runs in one transaction: authorize the new `member.invite-org` / `member.invite-instance` operation, create the human principal + account, optionally expand a role template at that scope through the existing `applyTemplate` writer, mint a credential-establishment authority exactly as `credential-reset` does (`IssuedBy: "invitation"`, `ResetLifetime`), and audit `member.invited` on the scope's trail plus `auth.credential_authority_minted` on the instance trail. Two new POST routes wrap it; the CLI verb `access member invite` and the WebUI Invite dialog call them; the existing public `establishCredential` consumes the authority, now also from a new chromeless SPA page.

**Tech Stack:** Go 1.27 (chi + oapi-codegen strict server, sqlc), TypeScript (hey-api client, React 19, TanStack Query 5, Zod 4), Playwright 1.62, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-01-chrome-settings-invite-design.md` (PR2 section). Ticket: #568. Branch: `t3code/member-invitation` from main `5276ba8d`.

## Global Constraints

- Every commit GPG-signed AND DCO signed-off: plain `git commit -s` (signing is configured; verify once with `echo x | gpg --batch --pinentry-mode loopback -u EA4208DC5ABEB135 --sign >/dev/null`). Never `-c commit.gpgsign=false`.
- Node: `eval "$(fnm env)" && fnm use 24` before any `pnpm`. Go: `go test ./...` skips `//go:build` files; run the isolation package explicitly.
- No `as` casts; parse, don't cast; no `z.any`. en-GB "Organisation". Every user-visible label from the navigation table.
- Codegen is checked, not hand-edited: `go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml` (repo root) and `pnpm --dir clients/ts run generate`; CI diffs `api/apigen`, `clients/ts/src/generated`.
- Pins that MUST move with a new route/op/event: `api/noproxy_test.go` `pinnedContractSurface`; `internal/authz/classify.go` route table; `internal/isolation/testdata/operation_formulas.json`; `internal/audit/registry.go` event schema; `internal/cli/testdata/help.txt`; `web/e2e/registry.ts` (a new surface's flow must ride a spec already in a `ci.yml` group on main — `establish-credential` rides `login.spec.ts`, group 1).
- Uniform refusals: tenant-class routes answer 403/404 through the one wire writer (`return nil, err`); never a bespoke switch.
- Playwright: desktop then mobile SEQUENTIALLY (shared `web/e2e/.auth`), `NODE_OPTIONS=--dns-result-order=ipv4first`, seven `HIKYO_E2E_PORT*` values per run.
- Campsite rule: fix what you find; no follow-up tickets for things this PR touches.

---

### Task 1: Contract — two invitation routes, schemas, generated code, pins

**Files:**
- Modify: `api/openapi.yaml` (after the `/api/v1/orgs/{org}/grants/template` path block ~1760-1800; after `/api/v1/instance/grants/template` ~1685-1724; schemas beside `CredentialResetResult` ~10738; `establishCredential` description at 135-150)
- Modify: `api/noproxy_test.go:210-330` (`pinnedContractSurface`)
- Generate: `api/apigen/apigen.gen.go`, `clients/ts/src/generated/*`
- Test: `go test ./api/...` (noproxy + freeze + match), `pnpm --dir clients/ts run generate && git diff --exit-code clients/ts/src/generated`

**Interfaces:**
- Produces operationIds `inviteOrgMember` (`POST /api/v1/orgs/{org}/invitations`) and `inviteInstanceMember` (`POST /api/v1/instance/invitations`); schemas `InviteMemberRequest { username: string (1..128), display_name?: string (..256), template?: RoleTemplate }`, `InvitationResult { principal_id: ID, account_id: ID, authority: string, expires_at: Timestamp }`; generated Go types `apigen.InviteOrgMemberRequestObject{Org, Body}`, `apigen.InviteInstanceMemberRequestObject{Body}`, `apigen.InviteOrgMember201JSONResponse`, `apigen.InvitationResult`; TS ops `inviteOrgMemberOp`, `inviteInstanceMemberOp`, `zInvitationResult`.

- [ ] **Step 1: Add the org route**

Insert after the `/api/v1/orgs/{org}/grants/template` block:

```yaml
  /api/v1/orgs/{org}/invitations:
    parameters:
      - $ref: "#/components/parameters/Org"
    post:
      operationId: inviteOrgMember
      tags: [access]
      summary: Invite a human into this organisation with a local credential.
      description: |
        The account-creation path the human-auth ADR names ("accounts are
        created by invitation from a `manage-members` holder"). In ONE
        transaction: a human principal and an account with the given login
        handle are created, the optional role template is expanded into
        grants at organisation scope exactly as `applyOrgTemplate` would
        write them, and a credential-establishment authority is minted for
        the new account with issuer `invitation` and the credential-reset
        lifetime. The authority is returned once and never re-displayed:
        hand it to the invitee out of band, who consumes it at
        `establishCredential` and then signs in like anyone else.

        No session, no assurance and no identity link are created. An
        invitation carries no email and no delivery channel of its own.
        A username already in use is a `conflict`.
      x-hikyo-class: tenant
      x-hikyo-operation: member.invite-org
      x-hikyo-formula: [manage-members@org]
      x-hikyo-artifacts: [human-session]
      x-hikyo-min-revision: 1
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/InviteMemberRequest"
      responses:
        "201":
          description: The invited principal and its single-use authority.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/InvitationResult"
        "400":
          $ref: "#/components/responses/BadRequest"
        "401":
          $ref: "#/components/responses/Unauthenticated"
        "403":
          $ref: "#/components/responses/Forbidden"
        "404":
          $ref: "#/components/responses/NotFound"
        "409":
          $ref: "#/components/responses/Conflict"
        "429":
          $ref: "#/components/responses/TooManyRequests"
        "500":
          $ref: "#/components/responses/Internal"
```

Check the exact org path-parameter ref name used by the neighbouring blocks (`grep -n -B3 "operationId: applyOrgTemplate" api/openapi.yaml`) and use the same.

- [ ] **Step 2: Add the instance route**

Insert after the `/api/v1/instance/grants/template` block, same shape, with:
`operationId: inviteInstanceMember`, summary "Invite a human at instance scope with a local credential.", `x-hikyo-class: instance`, `x-hikyo-operation: member.invite-instance`, `x-hikyo-formula: [manage-members@instance]`, description adding: "The optional template is expanded at instance scope (`operator`), so this is how a second instance operator comes to exist without host access." No 404 is needed for an instance route unless the neighbours list it — mirror `applyInstanceTemplate`'s response set exactly.

- [ ] **Step 3: Add the schemas and amend `establishCredential`**

Beside `CredentialResetResult`:

```yaml
    InviteMemberRequest:
      type: object
      additionalProperties: false
      required: [username]
      properties:
        username:
          type: string
          minLength: 1
          maxLength: 128
          description: The invitee's local login handle; globally unique.
        display_name:
          type: string
          maxLength: 256
          description: Defaults to the username.
        template:
          $ref: "#/components/schemas/RoleTemplate"
          description: |
            Optional initial grants, expanded at the invitation's scope in the
            same transaction. Absent means the account can sign in and see
            nothing until someone grants it something.

    InvitationResult:
      type: object
      additionalProperties: false
      required: [principal_id, account_id, authority, expires_at]
      properties:
        principal_id:
          $ref: "#/components/schemas/ID"
        account_id:
          $ref: "#/components/schemas/ID"
        authority:
          type: string
          description: |
            The single-use credential-establishment authority for the new
            account, returned once. It creates no session and may only ever
            establish a password; hand it to the invitee out of band.
        expires_at:
          $ref: "#/components/schemas/Timestamp"
```

In `establishCredential`'s description change "issued only by the bootstrap path, `credential-reset`, or local break-glass" to "issued only by the bootstrap path, `credential-reset`, a member invitation, or local break-glass".

- [ ] **Step 4: Regenerate and pin**

```bash
go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml
eval "$(fnm env)" && fnm use 24 && pnpm --dir clients/ts run generate
```
Add to `pinnedContractSurface` (alphabetical among the POST lines):
```go
	"POST /api/v1/instance/invitations":                                                        true,
	"POST /api/v1/orgs/{org}/invitations":                                                      true,
```
Run: `go test ./api/...`
Expected: PASS (freeze guard is dormant until the v1 tag; noproxy set equality holds; the artifact-admission test accepts `human-session`).

- [ ] **Step 5: Commit**

```bash
git add api/openapi.yaml api/noproxy_test.go api/apigen clients/ts/src/generated
git commit -s -m "feat(api): member invitation routes at org and instance scope (#568)"
```

---

### Task 2: Authz + audit registries — operations, events, classification, formula pin

**Files:**
- Modify: `internal/authz/registry.go` (Operation consts near line 330; entries near `OpTemplateApplyOrg` 2974 / `OpTemplateApplyInstance` 3009)
- Modify: `internal/authz/classify.go:428-434` (route table)
- Modify: `internal/audit/registry.go` (EventType consts near 200; `EventAuthAuthorityMinted` schema at ~896-906; new schema entry)
- Modify: `internal/audit/audit_test.go:415` table (add the enum case)
- Modify: `internal/isolation/testdata/operation_formulas.json` (regenerated from the test's "current" output)
- Test: `go test ./internal/authz/... ./internal/audit/... && go test -run 'TestInvariant06aFormulaPinning|TestRouteClassification' ./internal/isolation/...`

**Interfaces:**
- Produces: `authz.OpMemberInviteOrg Operation = "member.invite-org"`, `authz.OpMemberInviteInstance Operation = "member.invite-instance"`; `audit.EventMemberInvited EventType = "member.invited"`.

- [ ] **Step 1: Write the failing tests**

In `internal/audit/audit_test.go` enum table (beside the "reset issuer" row) add:

```go
		{"authority issuer", EventAuthAuthorityMinted, OutcomeSuccess, TrailInstance, domain.Scope{}, "issued_by",
			Payload{"authority_id": "cea_1", "account_id": "acc_1", "issued_by": "invitation", "delivery": "response"},
			[]string{"bootstrap", "credential-reset", "break-glass", "recovery", "invitation"}},
```
(look at how that table's rows are consumed a few lines below: the last column is the enum the test asserts the schema declares; the payload must validate).

Run: `go test ./internal/audit/ -run TestPayloadEnums` (find the actual test name: `grep -n "func Test" internal/audit/audit_test.go | sed -n 1,40p`).
Expected: FAIL — `invitation` not in the enum.

- [ ] **Step 2: Audit registry**

1. Consts (after `EventGrantMembershipRead`):

```go
	// member.invited records an invitation (#568): a human principal and its
	// account came to exist under a `manage-members` holder's authority, with
	// the optional template named. The minted authority is its own instance
	// event (auth.credential_authority_minted, issued_by=invitation); this one
	// lives on the scope's trail so an org administrator can answer "who
	// invited whom" without instance access.
	EventMemberInvited EventType = "member.invited"
```
2. `EventAuthAuthorityMinted` schema: `"issued_by"` enum becomes `[]string{"bootstrap", "credential-reset", "break-glass", "recovery", "invitation"}`.
3. Schema entry (beside `EventGrantTemplateApplied`):

```go
	EventMemberInvited: {
		SchemaVersion: 1,
		Retention:     RetentionSecurity,
		Outcomes:      map[Outcome]bool{OutcomeSuccess: true},
		Trails:        map[Trail]bool{TrailTenant: true, TrailInstance: true},
		Schema: Schema{
			"principal_id":   {Kind: KindString, Required: true},
			"account_id":     {Kind: KindString, Required: true},
			"username":       {Kind: KindFreeText, Required: true},
			"scope":          {Kind: KindString, Required: true},
			"template":       {Kind: KindString},
			"grants_created": {Kind: KindInt, Required: true},
			"authority_id":   {Kind: KindString, Required: true},
			"delivery":       {Kind: KindString, Required: true, Enum: []string{"file", "terminal", "stdout", "response"}},
		},
	},
```

- [ ] **Step 3: Authz registry**

Consts (beside `OpTemplateApplyOrg`):
```go
	// Member invitation (#568): account creation under manage-members, the
	// human-auth ADR's named path. Two operations because the formula differs
	// per depth exactly as grant.create does; the route names one each.
	OpMemberInviteOrg      Operation = "member.invite-org"
	OpMemberInviteInstance Operation = "member.invite-instance"
```
Entries (after `OpTemplateApplyInstance`'s entry):
```go
	OpMemberInviteOrg: {
		class:    ClassTenant,
		level:    domain.LevelOrg,
		formula:  Formula{{Cap: domain.CapManageMembers, At: domain.LevelOrg}},
		storeOps: withCure(map[StoreOp]bool{StoreAuditTenantInsert: true}),
		// The optional template rides the same writer applyOrgTemplate uses,
		// so its events — and §2.4's cure — are reachable from this operation.
		events: append([]audit.EventType{
			audit.EventMemberInvited, audit.EventGrantTemplateApplied,
			audit.EventGrantCreated, audit.EventGrantModified,
		}, grantCureEvents...),
	},
	OpMemberInviteInstance: {
		class:    ClassInstance,
		formula:  Formula{{Cap: domain.CapManageMembers, At: domain.LevelNone}},
		storeOps: map[StoreOp]bool{StoreAuditInstanceInsert: true},
		events: append([]audit.EventType{
			audit.EventMemberInvited, audit.EventGrantTemplateApplied,
			audit.EventGrantCreated, audit.EventGrantModified,
		}, grantCureEvents...),
	},
```
Copy the exact `events`/`storeOps` shape from `OpTemplateApplyOrg`/`OpTemplateApplyInstance` (they may list more, e.g. session-invalidation events); the invite op must be a superset of the template op's declarations because it calls the same writer.

Classification (`classify.go`, beside the grants rows):
```go
	"http:POST /api/v1/orgs/{org}/invitations": {Class: ClassTenant, Ops: []Operation{OpMemberInviteOrg}},
	"http:POST /api/v1/instance/invitations":   {Class: ClassInstance, Ops: []Operation{OpMemberInviteInstance}},
```

- [ ] **Step 4: Regenerate the formula pin and run**

Run: `go test ./internal/isolation/ -run TestInvariant06aFormulaPinning` → it fails printing `current:` JSON; write that JSON to `internal/isolation/testdata/operation_formulas.json` (the two new entries appear alphabetically as `member.invite-instance` / `member.invite-org`). Re-run; then `go test ./internal/authz/... ./internal/audit/...` and the isolation registry/classification tests (`go test ./internal/isolation/ -run 'Invariant|Classif|Registry'`).
Expected: PASS. Note: an isolation test may assert every registered event has an emitter — if it fails now, it passes after Task 3; run it again there.

- [ ] **Step 5: Commit**

```bash
git add internal/authz internal/audit internal/isolation/testdata/operation_formulas.json
git commit -s -m "feat(authz,audit): member.invite operations, member.invited event, invitation issuer (#568)"
```

---

### Task 3: Service — `Grants.InviteMember`

**Files:**
- Create: `internal/service/invite.go`
- Create: `internal/service/invite_test.go` (unit, sqlite in-memory via the package's existing test harness — mirror `internal/service/credential_reset_test.go` or the grants tests' `newTestDB`/`seedOrg` helpers: `grep -n "func newTest\|func seed" internal/service/*_test.go | head`)
- Modify: `internal/isolation/reauth_e2e_test.go` (append an invitation section to the network-reset test at ~930-990, same harness) — the emitter proof

**Interfaces:**
- Produces:
```go
type InviteSpec struct {
	Scope       domain.Scope    // zero = instance
	Username    string
	DisplayName string          // "" → Username
	Template    domain.Template // "" → no initial grants
	Delivery    string          // audit-only: "response" from HTTP, the sink destination from the CLI host path (not used here)
}
type InvitationResult struct {
	PrincipalID   domain.PrincipalID
	AccountID     string
	Authority     string    // the one-time value; in memory only
	AuthorityID   string
	ExpiresAt     time.Time
	GrantsCreated int
}
func (s *Grants) InviteMember(ctx context.Context, actor Actor, spec InviteSpec) (InvitationResult, error)
```
- Consumes: `opsFor`-style level derivation (`spec.Scope.Level()`), `actor.resolve`, `az.Authorize`, `newID`, `crypto.NewArtifact(crypto.ArtifactBootstrap)`, `az.CreateHumanPrincipal`, `az.CreateAccount`, `s.applyTemplate` (Task-internal, same file/package), `az.CredentialEpoch`, `az.MintAuthority`, `ResetLifetime`, `newAuditEvent`/`domainEvent`, `az.RecordAuthEvent`, `r.Audit().InsertTenant/InsertInstance`.

- [ ] **Step 1: Write the failing unit test**

`internal/service/invite_test.go` — use the same fixture the grants tests use (find it: `grep -ln "ApplyTemplate(" internal/service/*_test.go`; copy its setup: sqlite DB, a `factor-admin`-style caller with `manage-members` at `org_a`, and a second caller with `manage-members` at instance). Assert:

```go
func TestInviteMember(t *testing.T) {
	// setup: db, grants := ..., orgAdmin := actor with manage-members@org_a, op := actor with manage-members@instance
	t.Run("org invite creates account, template grants and a claimable authority", func(t *testing.T) {
		res, err := grants.InviteMember(ctx, orgAdmin, service.InviteSpec{
			Scope: domain.Scope{Org: "org_a"}, Username: "dana", Template: domain.TemplateEditor, Delivery: "response",
		})
		if err != nil { t.Fatal(err) }
		if res.GrantsCreated != 2 { t.Fatalf("editor at org expands to read+edit, got %d", res.GrantsCreated) }
		// the authority establishes a credential and the invitee can log in
		if err := auth.EstablishCredential(ctx, res.Authority, "a long enough password 1"); err != nil { t.Fatal(err) }
		if _, err := auth.LocalLogin(ctx, "dana", "a long enough password 1", service.ArtifactCLI); err != nil { t.Fatal(err) }
		// audited on BOTH trails
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'member.invited' AND org_id = 'org_a'"); n != 1 { t.Errorf("tenant member.invited = %d", n) }
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.credential_authority_minted' AND payload LIKE '%\"issued_by\":\"invitation\"%'"); n != 1 { t.Errorf("minted = %d", n) }
	})
	t.Run("instance invite without a template creates a grantless account", func(t *testing.T) {
		res, err := grants.InviteMember(ctx, op, service.InviteSpec{Username: "sam", Delivery: "response"})
		if err != nil { t.Fatal(err) }
		if res.GrantsCreated != 0 { t.Fatalf("got %d grants", res.GrantsCreated) }
		if n := queryInt(t, db, "SELECT COUNT(*) FROM grants WHERE principal_id = ?", string(res.PrincipalID)); n != 0 { t.Errorf("grants = %d", n) }
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'member.invited'"); n != 1 { t.Errorf("instance member.invited = %d", n) }
	})
	t.Run("a duplicate username is a conflict and writes nothing", func(t *testing.T) {
		_, err := grants.InviteMember(ctx, orgAdmin, service.InviteSpec{Scope: domain.Scope{Org: "org_a"}, Username: "dana", Delivery: "response"})
		if !errors.Is(err, domain.ErrConflict) { t.Fatalf("want ErrConflict, got %v", err) }
		if n := queryInt(t, db, "SELECT COUNT(*) FROM accounts WHERE username = 'dana'"); n != 1 { t.Errorf("accounts = %d", n) }
	})
	t.Run("org manage-members cannot invite at instance scope", func(t *testing.T) {
		_, err := grants.InviteMember(ctx, orgAdmin, service.InviteSpec{Username: "x", Delivery: "response"})
		if !errors.Is(err, domain.ErrUnauthorized) && !errors.Is(err, domain.ErrNotFound) { t.Fatalf("want a uniform refusal, got %v", err) }
	})
	t.Run("a project scope is invalid", func(t *testing.T) {
		_, err := grants.InviteMember(ctx, orgAdmin, service.InviteSpec{Scope: domain.Scope{Org: "org_a", Project: "prj_a"}, Username: "y", Delivery: "response"})
		if !errors.Is(err, domain.ErrInvalid) { t.Fatalf("want ErrInvalid, got %v", err) }
	})
	t.Run("an empty username is invalid", func(t *testing.T) {
		_, err := grants.InviteMember(ctx, orgAdmin, service.InviteSpec{Scope: domain.Scope{Org: "org_a"}, Username: "   ", Delivery: "response"})
		if !errors.Is(err, domain.ErrInvalid) { t.Fatalf("want ErrInvalid, got %v", err) }
	})
}
```
Adapt helper names (`queryInt`, `execRaw`, DB opening, `Auth` construction) to what the package's tests already export; the assertions are the contract. `Grants` needs an `Auth` field already (see `type Grants struct`); the test constructs both.

Run: `go test ./internal/service/ -run TestInviteMember`
Expected: FAIL — `InviteMember` undefined.

- [ ] **Step 2: Implement `internal/service/invite.go`**

```go
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// InviteSpec is one invitation: WHERE (org or instance), WHO (the login
// handle) and, optionally, WHAT they start with (a role template expanded at
// that scope).
type InviteSpec struct {
	// Scope is the organisation invited into; the zero Scope is instance scope.
	Scope       domain.Scope
	Username    string
	DisplayName string
	// Template is optional: "" invites an account that can sign in and see
	// nothing, the human-auth ADR's "provisioning and authorizing are
	// separate acts" default.
	Template domain.Template
	// Delivery names how the caller hands the authority over; it is recorded
	// in the mint event because delivery mode IS the security property.
	Delivery string
}

// InvitationResult carries the one-time authority out to the caller. The
// value is in memory only and is never re-displayed: if it lapses, invite
// again (a second invitation of an existing username is a conflict; use
// credential-reset instead).
type InvitationResult struct {
	PrincipalID   domain.PrincipalID
	AccountID     string
	Authority     string
	AuthorityID   string
	ExpiresAt     time.Time
	GrantsCreated int
}

// InviteMember is the human-auth ADR's named account-creation path (#568):
// "accounts are created by invitation from a manage-members holder".
//
// It is bootstrap minus the first-account check, under an authorization
// instead of host authority: create the principal and account, expand the
// optional template through the SAME writer applyOrgTemplate uses (so the
// grants, their events and §2.4's cure are indistinguishable from an
// ordinary template application), then mint a credential-establishment
// authority exactly as credential-reset does — same artifact, same lifetime,
// same establish endpoint — with `invitation` as the recorded issuer.
//
// Everything commits in one transaction (ADR § Identity linking: "invitation
// consumption, account creation and any initial grants occur in ONE
// transaction"), so a username collision — the store's UNIQUE constraint,
// surfaced as domain.ErrConflict — leaves nothing behind.
func (s *Grants) InviteMember(ctx context.Context, actor Actor, spec InviteSpec) (InvitationResult, error) {
	username := strings.TrimSpace(spec.Username)
	if username == "" {
		return InvitationResult{}, fmt.Errorf("%w: a username is required", domain.ErrInvalid)
	}
	displayName := strings.TrimSpace(spec.DisplayName)
	if displayName == "" {
		displayName = username
	}
	level, err := spec.Scope.Level()
	if err != nil {
		return InvitationResult{}, fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	var op authz.Operation
	switch level {
	case domain.LevelNone:
		op = authz.OpMemberInviteInstance
	case domain.LevelOrg:
		op = authz.OpMemberInviteOrg
	default:
		// Membership is an org or instance fact; a project has no accounts
		// of its own, so a deeper scope is a malformed address, not a
		// narrower invitation.
		return InvitationResult{}, fmt.Errorf("%w: invitations are addressed at organisation or instance scope", domain.ErrInvalid)
	}
	value, verifier, err := crypto.NewArtifact(crypto.ArtifactBootstrap)
	if err != nil {
		return InvitationResult{}, err
	}
	principalID, err := newID("prn")
	if err != nil {
		return InvitationResult{}, err
	}
	accountID, err := newID("acc")
	if err != nil {
		return InvitationResult{}, err
	}
	authorityID, err := newID("cea")
	if err != nil {
		return InvitationResult{}, err
	}
	now := s.now()
	expires := now.Add(ResetLifetime)
	var out InvitationResult
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		p, err := az.Authorize(ctx, caller, op, spec.Scope)
		if err != nil {
			return err
		}
		target := domain.PrincipalID(principalID)
		if err := az.CreateHumanPrincipal(ctx, target, now); err != nil {
			return err
		}
		if err := az.CreateAccount(ctx, authz.Account{
			ID: accountID, PrincipalID: target,
			Username: username, DisplayName: displayName, CreatedAt: now,
		}); err != nil {
			return err // a UNIQUE violation arrives as domain.ErrConflict
		}
		grantsCreated := 0
		if spec.Template != "" {
			results, err := s.applyTemplate(ctx, r, az, p, caller, spec.Template, target, spec.Scope, level)
			if err != nil {
				return err
			}
			for _, res := range results {
				if res.Outcome == GrantCreated() {
					grantsCreated++
				}
			}
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if err := az.MintAuthority(ctx, authz.NewCredentialAuthority{
			ID: authorityID, Verifier: verifier, AccountID: accountID,
			Purpose: "establish-credential", IssuedBy: "invitation",
			CredentialEpoch: epoch, ExpiresAt: expires, CreatedAt: now,
		}); err != nil {
			return err
		}
		// The mint is its own instance-trail record, like every other
		// authority issuance (factors MEDIUM-7).
		minted, err := newAuditEvent(ctx, audit.EventAuthAuthorityMinted, target,
			audit.Object{Type: "authority", ID: authorityID}, audit.OutcomeSuccess, "",
			audit.Payload{"authority_id": authorityID, "account_id": accountID, "issued_by": "invitation", "delivery": spec.Delivery})
		if err != nil {
			return err
		}
		if err := az.RecordAuthEvent(ctx, minted); err != nil {
			return err
		}
		// The invitation itself lives on the scope's trail, bound to this
		// operation's proof, so an org administrator can answer "who invited
		// whom" from the org trail alone.
		payload := audit.Payload{
			"principal_id": principalID, "account_id": accountID,
			"username": audit.SanitizeFreeText(username), "scope": renderScope(spec.Scope),
			"grants_created": grantsCreated, "authority_id": authorityID, "delivery": spec.Delivery,
		}
		if spec.Template != "" {
			payload["template"] = string(spec.Template)
		}
		invited, err := domainEvent(ctx, audit.EventMemberInvited, caller.Principal,
			audit.Object{Type: "account", ID: accountID}, payload)
		if err != nil {
			return err
		}
		if level == domain.LevelNone {
			if err := r.Audit().InsertInstance(ctx, p, invited); err != nil {
				return err
			}
		} else if err := r.Audit().InsertTenant(ctx, p, invited); err != nil {
			return err
		}
		out = InvitationResult{
			PrincipalID: target, AccountID: accountID, AuthorityID: authorityID,
			ExpiresAt: expires, GrantsCreated: grantsCreated,
		}
		return nil
	})
	if err != nil {
		return InvitationResult{}, err
	}
	out.Authority = value
	return out, nil
}
```
Check: `renderScope` exists in grants.go (used in grant payloads); `GrantCreated()` is the outcome constructor; `s.now()` exists on `*Grants` (`grep -n "func (s \*Grants) now" internal/service/grants.go`; if it is a field `Now func() time.Time`, mirror how `ApplyTemplate` obtains `now`). If `tx.Write` needs the serialized variant for account creation (see `hierarchy.go:185` `tx.WriteSerialized(ctx, s.DB, "hikyo:org-create", …)`), use `tx.WriteSerialized(ctx, s.DB, "hikyo:member-invite", …)` — the username UNIQUE index arbitrates either way.

- [ ] **Step 3: Run the unit test**

Run: `go test ./internal/service/ -run TestInviteMember -count=1`
Expected: PASS.

- [ ] **Step 4: Emitter proof in the isolation harness**

Append to the network-reset e2e test (`internal/isolation/reauth_e2e_test.go` after the break-glass section, same `auth`/`db`/`adminToken` variables; construct a `service.Grants{DB: db, Auth: auth}` the way other isolation tests do — `grep -n "service.Grants{" internal/isolation/*.go | head -2`):

```go
	// An invitation is the ADR's account-creation path (#568): the same
	// authority artifact, minted under manage-members instead of host access.
	grantsSvc := &service.Grants{DB: db, Auth: auth}
	inv, err := grantsSvc.InviteMember(ctx, service.Bearer(adminToken), service.InviteSpec{
		Scope: domain.Scope{Org: "org_a"}, Username: "invitee", Template: domain.TemplateViewer, Delivery: "response",
	})
	if err != nil {
		t.Fatalf("invite into org_a: %v", err)
	}
	if err := auth.EstablishCredential(ctx, inv.Authority, "invitee's first password 1"); err != nil {
		t.Fatalf("establish with the invitation authority: %v", err)
	}
	if _, err := auth.LocalLogin(ctx, "invitee", "invitee's first password 1", service.ArtifactCLI); err != nil {
		t.Fatalf("the invitee cannot log in: %v", err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'member.invited' AND org_id = 'org_a'"); n != 1 {
		t.Errorf("member.invited on the org trail = %d, want 1", n)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.credential_authority_minted' AND payload LIKE '%invitation%'"); n != 1 {
		t.Errorf("invitation mint on the instance trail = %d, want 1", n)
	}
```
The admin in that test holds `manage-members`? Check the seeding at the top of the test (`grep -n "factor-admin" internal/isolation/reauth_e2e_test.go | head -3`); if it only holds `credential-reset`, insert a `manage-members` grant row at `org_a` for it with the same `execRaw` shape used above.

Run: `go test -tags isolation ./internal/isolation/ -run 'Reauth|Reset' -count=1` (check the build tag at the file head: `head -3 internal/isolation/reauth_e2e_test.go`) plus the whole isolation package once: `go test -tags <tag> ./internal/isolation/...`.
Expected: PASS, including any "every event has an emitter" invariant.

- [ ] **Step 5: Commit**

```bash
git add internal/service/invite.go internal/service/invite_test.go internal/isolation/reauth_e2e_test.go
git commit -s -m "feat(service): Grants.InviteMember — account, optional template, invitation authority in one tx (#568)"
```

---

### Task 4: HTTP handlers + contract test

**Files:**
- Modify: `internal/server/access.go` (`GrantService` interface at 27-32; handlers after `ApplyInstanceTemplate`)
- Modify: `internal/server/contract_test.go` (add the two routes to whichever table exercises tenant/instance POSTs with a fake service — `grep -n "ApplyOrgTemplate\|applyOrgTemplate" internal/server/contract_test.go internal/server/*_test.go | head`)
- Test: `go test ./internal/server/...`

**Interfaces:**
- Consumes: `service.InviteSpec`, `service.InvitationResult`, `(*Grants).InviteMember`.
- Produces: `GrantService.InviteMember(ctx, actor, spec) (service.InvitationResult, error)`; handlers `InviteOrgMember`, `InviteInstanceMember`.

- [ ] **Step 1: Write the failing contract test**

Mirror the existing `ApplyOrgTemplate` contract case: the fake `GrantService` gains `InviteMember` returning a fixed result; assert `POST /api/v1/orgs/{org}/invitations` with `{"username":"dana","template":"editor"}` answers 201 with `authority`, `principal_id`, `account_id`, `expires_at`; `{"username":""}` answers 400 (schema minLength); a fake returning `domain.ErrConflict` answers 409 with code `conflict`; the instance route with a fake returning `domain.ErrUnauthorized` answers 403. Also extend the strict-server compile: `var _ apigen.StrictServerInterface = (*API)(nil)` fails until the handlers exist.

Run: `go test ./internal/server/ -run Contract -count=1`
Expected: FAIL to compile (missing methods).

- [ ] **Step 2: Implement**

`GrantService` interface: add
```go
	InviteMember(ctx context.Context, actor service.Actor, spec service.InviteSpec) (service.InvitationResult, error)
```
Handlers (in `access.go`, after the instance template handler):

```go
// Member invitation (#568): the human-auth ADR's account-creation path. The
// authority is delivered in the HTTP response to the inviter, who hands it to
// the invitee out of band — the same delivery credential-reset uses.
func (a *API) InviteOrgMember(ctx context.Context, req apigen.InviteOrgMemberRequestObject) (apigen.InviteOrgMemberResponseObject, error) {
	res, err := a.Grants.InviteMember(ctx, service.Bearer(bearer(ctx)), inviteSpec(domain.Scope{Org: domain.OrgID(req.Org)}, req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.InviteOrgMember201JSONResponse(wireInvitation(res)), nil
}

func (a *API) InviteInstanceMember(ctx context.Context, req apigen.InviteInstanceMemberRequestObject) (apigen.InviteInstanceMemberResponseObject, error) {
	res, err := a.Grants.InviteMember(ctx, service.Bearer(bearer(ctx)), inviteSpec(domain.Scope{}, req.Body))
	if err != nil {
		return nil, err
	}
	return apigen.InviteInstanceMember201JSONResponse(wireInvitation(res)), nil
}

func inviteSpec(scope domain.Scope, body *apigen.InviteMemberRequest) service.InviteSpec {
	spec := service.InviteSpec{Scope: scope, Username: body.Username, Delivery: "response"}
	if body.DisplayName != nil {
		spec.DisplayName = *body.DisplayName
	}
	if body.Template != nil {
		spec.Template = domain.Template(*body.Template)
	}
	return spec
}

func wireInvitation(res service.InvitationResult) apigen.InvitationResult {
	return apigen.InvitationResult{
		PrincipalId: string(res.PrincipalID), AccountId: res.AccountID,
		Authority: res.Authority, ExpiresAt: res.ExpiresAt,
	}
}
```
Check the generated field names/types (`grep -n "type InvitationResult struct" -A6 api/apigen/apigen.gen.go`, and whether `Template` is `*RoleTemplate`); adapt without casts (a `RoleTemplate` is a string-typed enum: `domain.Template(*body.Template)` is a conversion of a named string type, which is allowed).

- [ ] **Step 3: Run and commit**

Run: `go test ./internal/server/... -count=1 && go vet ./...`
Expected: PASS.
```bash
git add internal/server
git commit -s -m "feat(server): inviteOrgMember and inviteInstanceMember handlers (#568)"
```

---

### Task 5: CLI — `hikyo access member invite`

**Files:**
- Modify: `internal/cli/access.go` (header comment 15-36; `runAccessMember` 248-320)
- Modify: `internal/cli/testdata/help.txt:172-173` (and wherever the help text source lives: `grep -rn "access member remove --principal" internal/cli/*.go`)
- Modify: `internal/cli/identities_test.go` (add `{"member invite", []string{"access", "member", "invite", "dana", "--org", "org_a"}}` to the destination-refusal table at ~104-110 and a positional-grammar case beside 231-240)
- Modify: `docs/spec/api-cli-spellings.md` (if it tabulates access verbs; otherwise nothing) and `docs/adr/api-cli-surface.md` row 78 already names `member invite` — no change.
- Test: `go test ./internal/cli/... -count=1`

**Interfaces:**
- Consumes: `resolveAccessScope`, `disclose.Options`, `ios.prepareDisclosure`, `authenticatedResolvedTarget`, `client.Do`, `apigen.InviteMemberRequest`, `apigen.InvitationResult`.

- [ ] **Step 1: Write the failing tests**

Add the two table rows above. Run: `go test ./internal/cli/ -run 'DisplayOnceDestination|Positional' -count=1` (find the real test names around those tables).
Expected: FAIL — `unknown verb invite` (ExitUsage instead of ExitRefused).

- [ ] **Step 2: Implement**

In `runAccessMember`: `subverb("access member", args, "list", "invite", "remove")`; parse for `invite`:
```go
	var displayName, template, outputFile string
	var dangerous bool
	// inside the parseCommon flag func:
	if sub == "invite" {
		fs.StringVar(&displayName, "display-name", "", "display name (defaults to the username)")
		fs.StringVar(&template, "template", "", "role template to expand at this scope (optional)")
		fs.StringVar(&outputFile, "output-file", "", "write the authority to a file this command creates (0600)")
		fs.BoolVar(&dangerous, "dangerously-print", false, "print the authority to stdout")
	}
```
Positionals: `invite` takes exactly one (`<username>`); `list`/`remove` keep `checkNoPositionals`. After scope resolution, `invite` refuses project/environment scope with ExitUsage: "hikyo access member invite addresses an organisation (--org) or the instance (--instance-scope): a project has no accounts of its own". Path: `strings.TrimSuffix(scope.path, "/grants") + "/invitations"`. Then the display-once shape from `runResetCredential` (verbs.go 775-815): prepare the sink BEFORE the request, `client.Do(ctx, http.MethodPost, path, apigen.InviteMemberRequest{Username: username, DisplayName: optional(displayName), Template: optional(template)}, &result)`, `sink.WriteOnce(fmt.Sprintf("credential-establishment authority for %s (single-use, expires %s)", username, result.ExpiresAt.Format(time.RFC3339)), result.Authority)`, then on stderr:
```
invited <username> (principal <id>) at <scope label>; hand the authority above to them out of band.
They establish their credential with:  hikyo account establish-credential --instance <origin> --as <username>
or in the browser at <origin>/establish
```
(`<origin>` is the resolved target's origin — the same value `runEstablishCredential` prints at verbs.go ~897 via `entry.Origin`.) For the optional pointer fields use small helpers (`func optional(s string) *string { if s == "" { return nil }; return &s }`) — no casts. Template value validation stays server-side (400 on an unknown template is the uniform writer's business).

Update the header comment: replace the "`member invite` is NOT implemented" paragraph with one sentence: "`member invite` (#568) is the ADR's local-credential invitation: create the account, expand an optional template at that scope, mint the establish authority, deliver display-once under the print triad." Leave `service.ErrNoInvitationPath` alone (the OIDC-claim seam is a different decision).

Help text: add `  hikyo access member invite <username> [--display-name NAME] [--template T] [--org O | --instance-scope] [--output-file PATH | --dangerously-print]` after the `member list` line; regenerate/adjust `testdata/help.txt` the way the package's help golden test expects (`go test ./internal/cli/ -run Help -update` if such a flag exists; else edit by hand).

- [ ] **Step 3: Run and commit**

Run: `go test ./internal/cli/... -count=1`
Expected: PASS.
```bash
git add internal/cli docs/spec/api-cli-spellings.md
git commit -s -m "feat(cli): hikyo access member invite with display-once delivery (#568)"
```

---

### Task 6: Web — Invite dialog, Reset credential action, `/establish` page

**Files:**
- Modify: `web/src/api/access.ts` (invite + reset calls, refusal text)
- Create: `web/src/routes/InviteDialog.tsx`, `web/src/routes/InviteDialog.test.tsx`
- Create: `web/src/routes/EstablishCredential.tsx`, `web/src/routes/EstablishCredential.test.tsx`
- Modify: `web/src/routes/Sections.tsx` (export `DisplayOnceCopy`, moved from `AccountSecurity.tsx:769-800`; `AccountSecurity.tsx` imports it)
- Modify: `web/src/routes/Members.tsx` (`panel__actions` ~427-445: Invite button; row action Reset credential; result panel)
- Modify: `web/src/routes/Login.tsx` (lede link to `/establish`, as a `.btn` link below the form — 44px pin)
- Modify: `web/src/app/navigation.ts` (surface `establish-credential`, `/establish`, label `Establish credential`, `section: null`, `mode: 'public'`, `chrome: 'none'`), `web/src/app/App.tsx` ELEMENTS + `route-groups/auth.ts` export, `web/src/app/navigation.test.ts` expected list
- Modify: `web/e2e/registry.ts` (login flow claims `establish-credential`)
- Modify: `web/prototype/mock-api.ts` (POST invitations both scopes → fixed result; POST `/api/v1/auth/credential/establish` → 204)
- Modify: `web/src/styles/app.css` only if a new class is needed (`.display-once__value` for the mono, wrapping authority box)

**Interfaces:**
- Produces in `access.ts`:
```ts
export type InviteScope = { kind: 'org'; org: string } | { kind: 'instance' };
export async function inviteMember(scope: InviteScope, input: { username: string; displayName: string; template: string }): Promise<Invitation>; // parsed(inviteOrgMemberOp|inviteInstanceMemberOp), template '' → omitted
export async function resetCredential(principal: string): Promise<{ authority: string; expiresAt: string }>;
export function inviteFailureText(error: unknown): string; // 409 → 'That username is already taken.'; 400 → 'The invitation was refused: check the username and template.'; 403/404 → 'This session may not invite members here.'; else transportRefusalText
export function resetFailureText(error: unknown): string; // 401 → 'No credential was reset: the principal has no resettable account, or this session may not reset it.'
```
Both are plain async functions (display-once discipline from #498: never `useMutation`, whose cache would retain the authority).

- [ ] **Step 1: Write the failing unit tests**

`InviteDialog.test.tsx` (harness: `renderForm` + `vi.mock('../api/access.ts')` stubbing `inviteMember`): renders username/display name/template controls with "No initial grants" as the default template option and `templatesAt(level)` ids after it; submit with empty username shows an inline alert and calls nothing; submit with `dana` + `editor` calls `inviteMember({kind:'org', org:'org_a'}, {username:'dana', displayName:'', template:'editor'})` once and then renders the authority in a `code` element, the expiry, the CLI hint containing `--as dana`, and a link to `/establish`; the Close button calls `onDone('Invited dana. …')`; a 409 renders "That username is already taken." inline and keeps the form.

`EstablishCredential.test.tsx`: renders authority + password + repeat fields; mismatched passwords refuse locally without a request; a 204 from `fetch` renders the success state with a `/login` link; a 401 renders the uniform refusal text "The authority was not accepted. It may have expired or already been used." (mock `fetch` the way `InstanceAdmin.crypto.test.tsx` does).

`navigation.test.ts`: add `'establish-credential:public:none'` after `'login:public:none'`, and to the anonymous-reachable list.

Run: `pnpm --dir web vitest run src/routes/InviteDialog.test.tsx src/routes/EstablishCredential.test.tsx src/app/navigation.test.ts`
Expected: FAIL — modules missing.

- [ ] **Step 2: `access.ts` calls**

```ts
import { inviteInstanceMemberOp, inviteOrgMemberOp, resetCredentialOp } from '@hikyo/operations';
// …
export type InviteScope = { readonly kind: 'org'; readonly org: string } | { readonly kind: 'instance' };
export type Invitation = z.infer<typeof zInvitationResult>; // from '@hikyo/zod'

/** Display-once: a plain async call, never a mutation cache that would retain the authority. */
export async function inviteMember(
  scope: InviteScope,
  input: { username: string; displayName: string; template: string },
): Promise<Invitation> {
  const body = {
    username: input.username,
    ...(input.displayName === '' ? {} : { display_name: input.displayName }),
    ...(input.template === '' ? {} : { template: templateOf(input.template) }),
  };
  return scope.kind === 'instance'
    ? parsed(inviteInstanceMemberOp, { body })
    : parsed(inviteOrgMemberOp, { path: { org: scope.org }, body });
}

export async function resetCredential(principal: string): Promise<{ authority: string; expiresAt: string }> {
  const result = await parsed(resetCredentialOp, { path: { principal } });
  return { authority: result.authority, expiresAt: result.expires_at };
}

export function inviteFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 409) return 'That username is already taken.';
    if (error.status === 400) return 'The invitation was refused: check the username and the template.';
    if (error.status === 403 || error.status === 404) return 'This session may not invite members here.';
  }
  return transportRefusalText(error);
}

export function resetFailureText(error: unknown): string {
  if (error instanceof ApiError && error.status === 401) {
    return 'No credential was reset: the principal has no resettable account, or this session may not reset it.';
  }
  return transportRefusalText(error);
}
```
(`transportRefusalText` lives in `web/src/api/client.ts` per #452 — `grep -n "export function transportRefusalText" web/src/api/*.ts`.)

- [ ] **Step 3: `InviteDialog.tsx`**

A `<dialog className="ceremony">` (via `useModalDialog`) with props `{ scope: InviteScope; level: 'org' | 'instance'; scopeName: string; origin: string; onDone(text: string): void; onCancel(): void }`. Two stages: `form` and `issued`. Form: username (required), display name, template `<select>` with `<option value="">No initial grants</option>` + `templatesAt(level)`; hint under the template: "Expanded at {scopeName} in the same transaction; each grant stays individually revocable." Submit → `inviteMember` → stage `issued` renders:

```tsx
<h2 id={titleId}>Invitation for {username}</h2>
<p className="ceremony__lede">Hand this authority to {username} out of band. It is shown once, expires {new Date(expiresAt).toLocaleString()}, and only ever establishes a password — no session, no assurance.</p>
<code className="mono display-once__value" data-testid="invitation-authority">{authority}</code>
<DisplayOnceCopy value={authority} success="Authority copied." />
<p className="settings-note">They establish it in the browser at <Link to={surfaceById('establish-credential').path}>{origin}/establish</Link>, or from a terminal:</p>
<code className="instance-cli">$ hikyo account establish-credential --instance {origin} --as {username}</code>
<div className="ceremony__actions"><button type="button" className="btn btn--primary" onClick={() => onDone(`Invited ${username} at ${scopeName}. The authority was shown once; invite again if it lapses.`)}>Close</button></div>
```
`origin` = `window.location.origin` passed by the caller. Failure: `inviteFailureText` inline via `<Alert>`; the form stays.

- [ ] **Step 4: `Members.tsx` wiring**

- `panel__actions`: after New grant, `<button type="button" className="btn" disabled={!topologyReady} onClick={() => { feedback.clear(); setModal('invite'); }}>Invite</button>` (the modal union gains `'invite'`; render `<InviteDialog scope={instance ? {kind:'instance'} : {kind:'org', org}} level={instance ? 'instance' : 'org'} scopeName={scopeName} origin={window.location.origin} onDone={(text) => { feedback.ok(text); setModal('none'); void grants.refetch(); }} onCancel={() => setModal('none')} />`). Hide the Invite button on the project projection (`projectId !== ''`): a project has no accounts of its own; the org page is one click up.
- Row action: in each member row, after the capabilities list, a `btn btn--quiet` "Reset credential" button (`aria-label={`Reset credential for ${row.principal}`}`) → `resetCredential(row.principal)` → on success set `resetIssued = { principal, authority, expiresAt }` rendered in a small `<dialog className="ceremony">` using the same "issued" markup as the invite dialog (extract `IssuedAuthority` into `InviteDialog.tsx` and export it); on error `feedback.report(new Error(resetFailureText(error)))` — check `useFeedback`'s `report` contract (it takes `unknown` and formats through the failure-text function you passed; pass a `SurfaceMessage`-style error if that is the pattern in `Members.tsx`).
- Only in `compactPresentation === false` (org and instance pages), not on the project projection.

- [ ] **Step 5: `/establish` surface**

`navigation.ts` entry right after `login`:
```ts
  // Credential establishment (#568): where an invitee or a reset target turns
  // the display-once authority into a password. Public and chromeless like
  // login — the holder has no session yet — and reached from the login page
  // and from the invitation hand-off, never from the sidebar.
  {
    id: 'establish-credential',
    path: '/establish',
    label: 'Establish credential',
    section: null,
    mode: 'public',
    chrome: 'none',
  },
```
`App.tsx`: lazy `EstablishCredential` from the auth route group; `ELEMENTS['establish-credential'] = withRouteFallback(<EstablishCredential />)`. Because it is `public`, it is served in both the anonymous and the authenticated route trees automatically.

`EstablishCredential.tsx`: `<main className="login">` + `<form className="login__card">`: h1 "Establish your credential", lede "Paste the setup authority you were handed. It works once, and it only sets a password — you sign in afterwards like anyone else.", fields authority (`autoComplete="off"`, `spellCheck={false}`), new password (`autoComplete="new-password"`, `minLength={12}`), repeat; local refusal on mismatch; submit → `ok(establishCredentialOp, { body: { authority, password } })` (the `ok` helper for 204 responses — `grep -n "export async function ok" web/src/api/client.ts`); success state replaces the form: "Credential established. Sign in with your username and the password you just set." + `<Link className="btn btn--primary" to={surfaceById('login').path}>Sign in</Link>`; failure `<Alert>The authority was not accepted. It may have expired or already been used.</Alert>` (the server answers a uniform 401; do not distinguish).

`Login.tsx`: below the form's last control add `<Link className="btn login__establish" to={surfaceById('establish-credential').path}>Have a setup authority? Establish your credential</Link>` (a `.btn` link meets the 44px mobile pin; add `.login__establish { margin-top: 8px; }` if spacing needs it).

`route-groups/auth.ts`: export `EstablishCredential`.

- [ ] **Step 6: Mock + closure**

`mock-api.ts`: `POST /api/v1/orgs/${ids.org}/invitations` and `POST /api/v1/instance/invitations` → 201 `{ principal_id: 'prn_77777777-7777-4777-8777-777777777777', account_id: 'acc_77777777-7777-4777-8777-777777777777', authority: 'hik_cea_prototype_authority_value', expires_at: <fixtureTime + 24h> }` after `zInviteMemberRequest.parse`; `POST /api/v1/auth/credential/establish` → 204; `POST /api/v1/accounts/{principal}/credential-reset` → 200 `{ authority: 'hik_cea_prototype_reset_value', expires_at }`.

`web/e2e/registry.ts`: `{ id: 'login', spec: 'flows/login.spec.ts', surfaces: ['login', 'oidc-done', 'establish-credential'] }` with a comment: new surface, rides group 1.

- [ ] **Step 7: Run unit tests, typecheck, build**

Run: `cd web && pnpm typecheck && pnpm vitest run && pnpm build`
Expected: PASS (the registry closure vitest now expects `establish-credential` claimed).

- [ ] **Step 8: Commit**

```bash
git add web/src web/prototype web/e2e/registry.ts
git commit -s -m "feat(web): Invite and Reset credential on Members; public /establish page (#568)"
```

---

### Task 7: Playwright — invite → establish → sign in; reset; pinned sets

**Files:**
- Modify: `web/e2e/flows/members.spec.ts` (org invite + establish + login as invitee; reset credential row action; pinned set on the invite dialog both themes)
- Modify: `web/e2e/flows/instance-admin.spec.ts` (instance invite without template + with `operator`, cleanup by revoking the created lines)
- Modify: `web/e2e/flows/login.spec.ts` (`/establish` pinned set both themes; mismatch refusal; uniform 401 text)
- Modify: `web/e2e/fixtures/instance.ts` only if a "login as arbitrary user" helper is missing — add `export async function loginAs(page: Page, username: string, password: string)` wrapping `sessionScript` with `enrol: false, stepUp: false`.

- [ ] **Step 1: Org invite end to end (`members.spec.ts`)**

```ts
  test('invites a member, who establishes a credential and signs in', async ({ browser, page }, testInfo) => {
    const username = `invitee-${testInfo.project.name}-${Date.now()}`;
    await page.getByRole('button', { name: 'Invite' }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('Username').fill(username);
    await dialog.getByLabel('Role template').selectOption('viewer');
    await dialog.getByRole('button', { name: 'Invite', exact: true }).click();
    const authority = (await dialog.getByTestId('invitation-authority').textContent()) ?? '';
    expect(authority.length).toBeGreaterThan(16);
    await expect(dialog).toContainText(`--as ${username}`);
    await dialog.getByRole('button', { name: 'Close' }).click();
    await expect(page.locator('.notice')).toContainText(`Invited ${username}`);
    // The invitee has ONE viewer line at org scope, granted by the inviter.
    const row = page.getByRole('row').filter({ hasText: username }).first();
    await expect(row).toContainText('read');

    const fresh = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    try {
      const invitee = await fresh.newPage();
      await invitee.goto('/establish');
      await invitee.getByLabel('Setup authority').fill(authority);
      await invitee.getByLabel('New password').fill('a first password long enough');
      await invitee.getByLabel('Repeat the password').fill('a first password long enough');
      await invitee.getByRole('button', { name: 'Establish credential' }).click();
      await expect(invitee.getByText('Credential established')).toBeVisible();
      await invitee.getByRole('link', { name: 'Sign in' }).click();
      await invitee.getByLabel('Username').fill(username);
      await invitee.getByLabel('Password').fill('a first password long enough');
      await invitee.getByRole('button', { name: 'Sign in' }).click();
      await expect(invitee).toHaveURL(/\/projects$/);
      // The authority is single-use: a second establish is refused uniformly.
      await invitee.goto('/establish');
      await invitee.getByLabel('Setup authority').fill(authority);
      await invitee.getByLabel('New password').fill('another password long enough');
      await invitee.getByLabel('Repeat the password').fill('another password long enough');
      await invitee.getByRole('button', { name: 'Establish credential' }).click();
      await expect(invitee.getByRole('alert')).toContainText('was not accepted');
    } finally {
      await fresh.close();
    }
    // Reset the invitee's credential from the row action: display-once again.
    await row.getByRole('button', { name: `Reset credential for ` }).click(); // use the exact aria-label with the principal id read from the row's mono cell
    await expect(page.getByRole('dialog').getByTestId('invitation-authority')).not.toBeEmpty();
    await page.getByRole('dialog').getByRole('button', { name: 'Close' }).click();
  });
```
Adjust the reset step: read the principal id from the row (`row.locator('.mono').first().textContent()`) and use `{ name: \`Reset credential for ${principal}\` }`. Add the invite dialog to the "both grant dialogs" pinned-set test (or a sibling) so its controls meet the pinned assertion set in both themes.

- [ ] **Step 2: Instance invite (`instance-admin.spec.ts`)**

On `/instance/members`: invite `op-${…}` with template `operator`; assert the new rows carry `manual: ${seed.principal}`; then revoke every created line through the API in `finally` (as the template test does); a second invite with the same username answers "That username is already taken." inline.

- [ ] **Step 3: `/establish` pins (`login.spec.ts`)**

Pinned assertion set for `establish-credential` in both schemes (mirror the login pinned block: heading, the primary button as control, the card as container, ui font); "refuses mismatched passwords locally" (no request — `page.route` counter); "answers a bad authority uniformly" (alert text, no distinction).

- [ ] **Step 4: Run**

```bash
cd web && export NODE_OPTIONS=--dns-result-order=ipv4first HIKYO_E2E_PORT=45900 HIKYO_E2E_PORT_B=45901 HIKYO_E2E_PORT_TLS=45902 HIKYO_E2E_PORT_OIDC=45903 HIKYO_E2E_PORT_OPERATIONAL=45904 HIKYO_E2E_PORT_OPERATIONAL_B=45905 HIKYO_E2E_PORT_OIDC2=45906
pnpm exec playwright test --project=desktop            # unfiltered: closure gate
pnpm exec playwright test --project=mobile e2e/flows/login.spec.ts e2e/flows/members.spec.ts e2e/flows/instance-admin.spec.ts
```
Expected: PASS, closure gate reports `establish-credential` pinned on both themes.

- [ ] **Step 5: Commit**

```bash
git add web/e2e
git commit -s -m "test(web): invite → establish → sign in, reset credential, /establish pins (#568)"
```

---

### Task 8: Docs, handoff, review, PR

- [ ] **Step 1: Docs**
  - `docs/handoff/568-member-invitation.md`: what landed per layer, the decision (local-credential invite; OIDC-claim seam untouched), pins that moved, how to run, the uniform-refusal notes (409 on username, 401 on reset), and the CLI/browser hand-off text.
  - `docs/status/ledger.json`: add or amend the administration capability line ("member invitation at org and instance scope from API, CLI and WebUI; browser credential establishment") + evidence pointer; then `node scripts/ci/check-doc-status.mjs --write --root .` (needs `pnpm --dir docs/site install --frozen-lockfile` under Node 24) so README tables regenerate — the docs CI job fails otherwise.
  - `docs/spec/ops-catalogue.md`: one row — invitation authority lifetime reuses the credential-reset lifetime (`ResetLifetime`), single-use, refused after consumption/expiry with the uniform 401.
  - `docs/adr/human-auth.md`: no amendment (invitation was always the named path); note in the handoff.
- [ ] **Step 2: Full verification** — `go vet ./... && go test ./... && go test -tags <isolation tag> ./internal/isolation/...`; web typecheck/vitest/build; Playwright as in Task 7.
- [ ] **Step 3: Push, PR** — `git push -u origin HEAD`; `gh pr create` with the handoff as body + prototype screenshots of the Invite dialog (issued stage) and `/establish` (take them in `pnpm prototype` on a free port; commit under `docs/handoff/568-screens/`).
- [ ] **Step 4: Codex R1–R3 at high effort** (cross-model-review skill, `codex exec … < /dev/null`, background, brief with the round cap). Fix all; R3 CLEAN or an explicit blocking list.
- [ ] **Step 5: Merge gate** — CI green + Codex CLEAN + screens in PR → stop and ask Marc.
