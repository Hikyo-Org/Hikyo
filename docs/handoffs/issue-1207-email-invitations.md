# Issue 1207: invite an existing account by email

## Result

An organisation administrator with `manage-members@org` invites an existing
account by its exact email address. The person accepts while signed in.

- API: `POST/GET /api/v1/orgs/{org}/email-invitations`,
  `DELETE /api/v1/orgs/{org}/email-invitations/{invitation}` and
  `POST /api/v1/me/invitations/{preview,accept}`.
- CLI: the email invitation commands in `internal/cli/access_invitations.go`.
- Web: "Invite by email" and the pending list on Members, and the
  `/invitations/accept` page.
- Store: migration `00087_org_email_invitations` on SQLite and Postgres.
- Design: `docs/spec/email-invitations.md`, plus the 2026-11-06 #1207 banners
  in `docs/adr/human-auth.md`, `permission-model.md`, `tenant-isolation.md` and
  `audit-model.md`.

Owner decisions (2026-11-06) are table D1 onward in spec section 1. Invitations
last 7 days. Only an account with a verified email is reachable; reaching other
accounts is the follow-up #1238. After acceptance a multi-organisation account
can be reset only by the instance (spec section 5).

## Behaviour to keep

- The send answers the same status and body whether or not the address has an
  account. Only a matched address is mailed, from a background send per
  request, owned by `service.MailSends` (required; `Send` refuses without it).
- A pending invitation changes nothing for the target account.
- Acceptance needs the person's own signed-in human session and the
  single-use token. The footprint exemption is the narrow `invitee` case, and
  the inviter's authority is re-checked inside the acceptance transaction.
  Every refusal is one uniform `404`.
- Accepting a grant ends every session of the person. The response is a plain
  `200` that leaves the browser cookies alone: the browser's session fence
  discards a response whose session cookie changed unless whoami still proves
  the same person. The page shows the outcome, then ends its own session.
- A sign-in redirect from a public page (`mode: 'public'` surface) drops the
  URL fragment, so a fragment token never enters the `returnTo` query.

## Evidence

- Go: `./internal/server/... ./internal/service/... ./internal/isolation/...
  ./internal/app/...` on SQLite and Postgres, and `./internal/lint`.
- Web: typecheck, oxlint, the Vitest suites for `src/app`,
  `InvitationAccept` and the sensitive inventory.
- Browser: the "invites an existing account by email" flow (local run on
  2026-11-07, both viewports passed) in
  `web/e2e/flows/login.spec.ts`. It covers sign-up, the
  invite from Members, the mail, enrolment, preview, acceptance, sign-out, the
  sign-in page with the revoked cookie, and the single-use replay refusal.
- Reviews: independent Standards and Spec reviews in six rounds, plus the
  CodeRabbit findings on PR #1250. Findings were fixed or answered; the timing
  residuals were accepted by the owner. Cross-provider review was waived by the
  owner.

## Decisions and limits

- The owner accepted the timing residuals in spec section 2 on 2026-11-07:
  the foreign-key check, the immediate unmatched outcome write on a
  single-writer database, and relay throttling.
- Two implementation choices from the PR #1250 review, not owner decisions
  (spec section 1 records them as such):
  - Invitation sends need an explicitly configured public origin
    (`no-public-origin`), like local sign-up.
  - Invitation mail has no instance-wide in-flight cap: a shared cap would let
    one organisation's matched sends delay another's mail. The send budget
    allows at most 30 attempts in flight per organisation, so the instance
    bound is 30 times the number of organisations, which also grows through
    `fresh-org` sign-ups up to `fresh_org_cap` (ops catalogue Pi-4 fit).
- A crash loses in-flight attempts without an outcome; the administrator can
  send again.
- The CLI refuses an invitation link whose origin is not the selected
  instance's.
- The per-organisation send budget is node-local, like every other budget in
  `internal/service/budget.go` (single-node v1 envelope).
- #1238: verified email for accounts other than local sign-ups.
- For owner triage, not changed here: under a `fresh-org` registration
  policy a self-registrant administers their new organisation, so they can
  send invitation mail to any account with a verified email under an
  organisation name they choose. The send budget, `fresh_org_cap` and the
  `signup` budget bound the volume.

## Next steps

1. Before merge, confirm the email invitation browser flow passes in CI on both
   viewports.
