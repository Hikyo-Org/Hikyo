# Hikyo: inviting an existing account by email (spec, #1207)

Normative contract for [#1207](https://github.com/Hikyo-Org/Hikyo/issues/1207).
The owner accepted these decisions on 2026-11-06. They are bound into the ADRs by
the amendment banners of that date in [human-auth](../adr/human-auth.md),
[permission-model](../adr/permission-model.md) and
[tenant-isolation](../adr/tenant-isolation.md).

An organisation administrator who knows a person's exact email address can
invite the existing account holding that address into the organisation, with a
role template. The person joins only by accepting, while signed in, with the
single-use link the invitation mailed to them.

## 1. Decisions

| # | Decision |
|---|---|
| D1 | An unknown address gets no mail. The request writes the same invitation row it writes for a match, appears in the pending list the same way, and expires the same way. |
| D2 | An instance without a configured mailer refuses every email invitation with one uniform precondition answer, evaluated before the address is read. The new-account invitation, SCIM attach, an administrator of both organisations and an instance administrator remain the other routes. |
| D3 | Only an account with a verified `accounts.email` can be reached. Today verified local sign-up is the only writer of that value, so invited, SCIM-provisioned and federated accounts are not reachable, and the administrator is not told which case applied. Self-service verification of an address for other accounts is the follow-up [#1238](https://github.com/Hikyo-Org/Hikyo/issues/1238). |
| D4 | A role template is required. An acceptance without one would write no grants and so would not make the person a member. |
| D5 | An invitation lives 7 days (the ops-spec invitation expiry). The new-account invitation keeps its 24-hour credential-establishment lifetime. |

## 2. Sending (`inviteOrgMemberByEmail`)

`POST /api/v1/orgs/{org}/email-invitations` with `{email, template}`, formula
`manage-members@org`.

1. Authorize the operation. Then evaluate the mailer predicate (the static
   "mailer configured" predicate of the [ops catalogue](./ops-catalogue.md));
   when it fails, answer the uniform `mailer-unconfigured` precondition refusal.
2. Canonicalize the address as local sign-up does
   ([social-signin.md](./social-signin.md) section 2.5); a malformed address or
   an unknown template is `400`.
3. Charge the per-organisation send budget and check the live pending cap; an
   overflow is the uniform `429`, independent of the address.
4. In one transaction: look the address up against verified addresses of
   active principals only (`GetAccountByEmail`), mint a single-use artifact,
   and write or replace the organisation's one pending row for that address:
   verifier, template, inviter, the matched account (or none), credential
   epoch, creation time and expiry. Write the `member.email_invited` event on
   the organisation trail. Replacing a row gives it a new artifact, inviter,
   template and expiry; the earlier link stops working.
5. Answer `202` with `{id, email, template, created_at, expires_at}`.

The request performs the same reads and writes whether or not the address
matched, and it never contacts the mail server. The transaction writes a
`member.email_invitation_dispatch` intent on the instance trail for every send,
and one background task follows the commit for every send, so every dispatch
intent receives exactly one `member.email_invitation_mail_outcome`:

- A matched account is mailed once within the mail per-send deadline. The
  outcome is `success` or `failure`, with `mail_sent` stating whether the mail
  was handed to the transport. A send dropped because the background
  concurrency bound is full records `failure` without dialling.
- An unmatched address is never dialled. Its outcome is `success` with
  `mail_sent: false`, stating that no mail was sent.

Outcomes live on the instance trail only. A failed or lost send is counted
(`hikyo_email_invitation_mail_failures_total`) but not reported to the
administrator, who can send again. Stopping the application waits for the
in-flight background tasks before the database closes.

Uniformity rules:

- The organisation trail and the pending list carry only what the
  administrator typed (address and template), never the matched account, its
  principal or its handle.
- An address belonging to an existing member, or to the administrator, is
  treated like any other address.
- A declined or ignored invitation stays pending until it expires.

## 3. Pending list and revocation

`GET /api/v1/orgs/{org}/email-invitations` lists live pending invitations:
`{id, email, template, invited_by, created_at, expires_at}`.
`DELETE /api/v1/orgs/{org}/email-invitations/{invitation}` revokes one and
writes `member.email_invitation_revoked`. Both need `manage-members@org`.
Expired rows are not listed and are deleted by the existing hourly pruning run.

## 4. Acceptance

The link is `{origin}/invitations/accept#token=...`; the token travels in the
fragment. `POST /api/v1/me/invitations/preview` and
`POST /api/v1/me/invitations/accept` take `{token}` and need the person's own
signed-in human session (browser or CLI).

An invitation is valid for the session only when the token resolves to a
pending row that is unexpired, carries the current credential epoch and is
bound to the session's own account. Email is never compared at acceptance.
Every other case (unknown token, another account, no matched account, expired,
superseded epoch, revoked, already accepted, an organisation that no longer
exists, an inviter who no longer holds the authority) is one uniform `404`.
Refusal causes are recorded on the instance trail only and leave the row as it
was.

Preview returns the organisation's name, the template and the expiry, and
writes nothing.

Accept, in one transaction:

1. Re-resolve and lock the row and check validity as above.
2. Re-authorize the inviter, not the person: the template operation at the
   organisation, against the inviter's current grants.
3. Apply the template to the person through the ordinary template writer, as
   the inviter, with a `manual` grant origin whose subject is the inviter.
   Every grant rule still applies. The only rule relaxed is the
   organisation-footprint gate, and only for the account the invitation is
   bound to, in this acceptance path.
4. Delete the row and write `member.email_invitation_accepted` (invitation,
   principal, account, grants created) on the organisation trail.

Answer `200` with `{org, org_name, template, grants_created, signed_out}`.

## 5. Effects

While an invitation is pending, nothing about the person changes: no grant, no
session or generation change, no developer-credential change, and no change to
the organisation footprint or to credential-reset classification. The table is
not part of the footprint.

Erasing the matched account (privacy erasure) unbinds the row (`account_id`
becomes null) instead of deleting it. The row stays in the pending list with
the same fields and expires on the same date as an unmatched row, but it can
never validate: acceptance answers the uniform `404`. The `account_id` foreign
key is `ON DELETE SET NULL` for the same reason.

Accepting is an ordinary privilege increase. When the template creates a grant,
the person's generation advances and all of their sessions end, the current
one included, and their developer credentials are revoked. The acceptance page
says so before the person confirms.

Afterwards the inviting organisation is part of the person's footprint:

- A person who already belonged to another organisation is multi-org. Their
  credential reset becomes an instance operation.
- A person with no organisation before has exactly one. That organisation's
  `credential-reset` holders can then reset them, as after any first grant.

## 6. Surfaces

| Surface | Send | Pending list | Revoke | Preview and accept |
|---|---|---|---|---|
| API | `inviteOrgMemberByEmail` | `listOrgEmailInvitations` | `revokeOrgEmailInvitation` | `previewEmailInvitation`, `acceptEmailInvitation` |
| CLI | `hikyo access invitation send` | `hikyo access invitation list` | `hikyo access invitation revoke` | `hikyo access invitation accept` (token read from standard input) |
| Web | Members, Invite by email | Members, pending invitations | Members, pending invitations | `/invitations/accept` |

## 7. Limits

| Entry | Default |
|---|---|
| Invitation lifetime | 7 days |
| Use | single use; bound to the account matched at send |
| Send budget | 30 per hour per organisation |
| Live pending invitations | 200 per organisation |
| Mail attempts | one per send, within the mail per-send deadline |
