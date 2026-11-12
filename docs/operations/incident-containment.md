# Incident containment and credential recovery runbook (DRAFT)

> **Draft, pending the implementation of the [accepted incident-containment ADR](../adr/incident-containment.md).**
> It is for [#1039](https://github.com/Hikyo-Org/Hikyo/issues/1039). The ADR is not
> accepted, so the owner has not approved any new containment state, capability or
> command. This runbook uses only commands and behavior that exist on `dev` at
> `426bf59ceb8787234d425f1d5bd2055569fd92d3`. It adds no incident state and no
> bulk command. Parts that the ADR would automate are listed under
> [What the ADR would change](#what-the-adr-would-change) and are not available today.
>
> **Not executed against a deployed instance.** The commands are source-verified
> against `internal/cli/testdata/help.txt` and the host usage strings, and their
> behavior is covered by the tests named in [Evidence](#evidence-and-limits).
> Exercise every step first against a disposable instance with synthetic credentials.
> The [verified inventory](../design/incident-containment.md) explains why each step
> exists.

## Read this first: what does not contain anything

Operators lose time by mistaking one of these for containment.

| Action | What it does | What it does not do |
| --- | --- | --- |
| Reset issuance (`account reset-credential`, `admin reset-credential`) | Advances the target's generation, deletes its sessions, revokes its developer delegations, issues a one-time establishment authority | It does not replace the password. The old password and every enrolled factor or linked IdP still sign in until establishment completes, and even then they are not revoked |
| Ordinary `logout` | Ends one session | Leaves developer delegations active until expiry or explicit revocation (a locked decision) |
| `adapter credential revoke` | Clears Hikyo's encrypted copy of the PAT and retires its jobs | Does not revoke the PAT at the provider, and removes the credential a later remote scrub needs |
| `dynamic-provider credential revoke` | Clears the stored admin credential | Does not drop any lease role, and leaves `revoking` or `unknown` leases unable to finish |
| `lease revoke` | Queues a revoke that a worker performs later | Is not immediate. `revoking` and `unknown` are pending, not revoked. A dropped PostgreSQL role does not end sessions already connected |
| `remote remove` | Deletes the local stored credential for a peer | Does not revoke the credential on the serving instance |
| `instance-config provider disable` | Stops new sign-in through that provider and revokes delegations minted through it | Does not log the user out of the provider, and does not end sessions minted through other methods |
| DEK, master or root rotation | Changes envelope keys | Revokes no bearer and no upstream credential, and does not make already stolen plaintext or backups safe |
| `rotate-token-key`, `rotate-scanning-key` | Changes delivery change tokens, drops scanning dismissals | Is not authentication-token revocation |
| Privacy `restrict`, `erase`, `release` | Data-subject actions with their own receipts | Not an incident tool. Never use them for containment |

Hikyo cannot recall plaintext it has already delivered. Rotation never proves that a
consumer, cache, CI variable, process environment or copied export has stopped using
an old value.

## Prerequisites

1. **Versions.** The CLI and the server must be built from the same `dev` revision or
   later. `rotate-dek` and `reencrypt` need a build that includes
   [#1042](https://github.com/Hikyo-Org/Hikyo/pull/1042) (merged 2026-10-28); earlier
   builds panic. Confirm with `hikyo --help` that these lines exist:
   `hikyo rotate-dek --instance-scope --yes` and `hikyo rotate-dek --org O --project P --yes`.
   Do not use the nightly 59 build (`daa2bd76`) mentioned in the parent assessment.
2. **A trusted response environment.** A workstation and network path that the
   suspected attacker does not control, with a CLI profile that has no inherited
   `--org`, `--project` or `--env` defaults, and a verified instance identity before
   any mutation. Target resolution order is flag, `HIKYO_*`, `./.hikyo.json`, context.
3. **Authority.** Record before starting who can act. Network reset needs
   `credential-reset` and an MFA-mandatory session. Org-scope holders reach only
   targets whose grants lie in exactly one org. Instance-capability targets have no
   network reset path and need the host-local command. Removing a member needs
   `manage-members` at the addressed scope.
4. **A clean second manager** at every affected org and at instance scope, with
   independent authentication and recoverable custody, before any grant is removed.
   The last-manager invariant refuses removal of the last holder. Do not widen a
   capability to get around a refusal.
5. **An incident journal outside the affected datastore and backup failure domain.**
   Write each planned step before running it and each committed outcome after. A lost
   response, a restart or a missing audit event means `UNKNOWN`, not success. Read
   current state before resuming. Never record bearer values, passwords, root keys or
   delivered plaintext in the journal.
6. **Output files, never argv.** Pass secrets with `--output-file`, `--stdin` or
   `--value-file`. Do not use `--dangerously-print` and never paste a secret into chat.

## 1. Classify the incident

The classification decides how far to go. Uncertainty widens the investigation. It
never becomes a claim of confirmed exposure.

| Situation | Containment depth |
| --- | --- |
| **Blocked attempts**: refusals and throttling, no established disclosure | Preserve evidence, tighten edge limits, review the instance trail. No revocation and no shutdown. Whole-instance shutdown from a detection signal is an attacker-triggered outage |
| **Credential theft**: a human, developer, machine or federation credential is known or suspected to be held by someone else | Steps 1 to 6 for each affected principal and each credential it could reach. Step 7 only if key material was exposed |
| **Provider disclosure**: a platform that holds Hikyo configuration or Hikyo-held upstream secrets disclosed data | Steps 1 to 3, then step 5 for every secret reachable from the exposure window, even without a suspicious Hikyo audit event. Steps 6 and 7 as exposure warrants |
| **Host or database compromise**: an attacker may have run code on the host or written to the database | External isolation and clean recovery. Do not trust any answer from the compromised process or datastore. Steps 1, 2, then 7 to 9 on a clean installation. Hikyo audit and ledger state from the affected datastore is evidence to investigate, not proof of containment |

## 2. Preserve evidence and isolate

1. Record the incident owner, UTC timeline, suspected entry point, exact binary or
   image digest, instance identity, and the affected principals, scopes, credentials
   and consumers.
2. Preserve trusted edge, provider, cloud or cluster, database and host records, and
   a consistent encrypted archive, before destructive changes where that is safe.
   When exfiltration is active, do not delay isolation to collect everything. Keep
   incident copies apart from the last approved recovery point.
3. Isolate at the firewall, proxy, cloud or cluster boundary using an independently
   trusted account. Cover every HA node, alternate listener, automation writer, SCIM
   caller and peer instance, and stop delivery consumers and jobs where needed.
   The scheduler and provider queues are writers too. Verify a denied request from
   an untrusted synthetic source and a working clean operator path.
4. Instance audit export and the F2 to F4 evidence work are owned by
   [#1035](https://github.com/Hikyo-Org/Hikyo/issues/1035),
   [#1036](https://github.com/Hikyo-Org/Hikyo/issues/1036) and
   [#1037](https://github.com/Hikyo-Org/Hikyo/issues/1037). Do not accept HTTP 200
   or partial JSONL as proof that an export is complete. Missing evidence is reported
   as missing.

Hikyo-level steps below run behind this isolation. They are not a substitute for it.

## 3. Enumerate affected authority

There is no single listing today. Run each, in every organization, and record the
result in the journal. Expand the set to principals, service accounts and grants the
attacker created or changed.

```sh
hikyo access grant list --instance-scope -o json
hikyo access grant list --org ORG -o json
hikyo access rule list --org ORG -o json
hikyo access member list --org ORG -o json
hikyo personal offboarding-report NAME -o json    # shared keys received, service accounts created
hikyo sa list -o json
hikyo sa credential list --sa SA -o json
hikyo dev session list -o json
hikyo scim binding list
hikyo scim credential list BINDING
hikyo instance-config federation-issuer list -o json
hikyo instance-config provider list
hikyo instance-config oauth2-provider list
hikyo remote list -o json
hikyo remote-credential list
hikyo remote-invite list -o json
hikyo adapter list -o json
hikyo adapter target list --adapter ADAPTER
hikyo dynamic-provider list -o json
hikyo lease list --env ENV -o json                # per environment only
```

Known enumeration gaps (the ADR addresses them): developer delegations of other users
are listed only per project through the project view or API; leases are listed only
per environment; there is no record of what an operator already handled. Treat every
object you could not list as unresolved.

## 4. Revoke Hikyo authentication

Do these in order inside each affected principal's scope, with the isolation in place.

### 4.1 A human principal

Use the sequence that [#1085](https://github.com/Hikyo-Org/Hikyo/pull/1085) tested
(`TestManualIncidentContainment`). Reset last, because removing authority first means
a session created by a still-valid old password or factor cannot read or regrant.

1. Remove every effective grant and rule of the principal, one exact row at a time,
   re-listing after each. Include instance scope, every org, project and environment
   depth, and rules. SCIM, structural and lockout-retention origins need their own
   owner (4.4) and block completion.

   ```sh
   hikyo access grant remove --instance-scope --principal PRINCIPAL --capability CAPABILITY
   hikyo access grant remove --org ORG --project PROJECT --principal PRINCIPAL --capability CAPABILITY
   hikyo access rule remove RULE --org ORG
   ```

   **Personal-values clock.** Losing read and reveal suspends the person's personal values
   and deletes them after 14 days (`PersonalRetentionWindow`,
   `internal/service/personal.go:696`). If the person must stay without authority for
   longer, tell them, or regrant read and reveal where the incident allows. The
   accepted ADR (O8) will pause this clock once implemented.

2. Issue the final reset. Use the tier that applies:

   ```sh
   hikyo account reset-credential PRINCIPAL --output-file ./reset-authority      # network
   hikyo admin reset-credential --principal PRINCIPAL --output-file ./reset-authority   # host-local
   ```

   After the sweep the target has no grants, so a network reset needs an
   instance-scope `credential-reset` holder; an org-only holder is refused. An
   instance-capability target or an account with a linked identity needs the host-local
   command. Never expose the host command as a network route.
3. Deliver the authority out of band to the verified owner, who runs
   `hikyo account establish-credential --instance INSTANCE --as USER`.
4. **Do not regrant while a compromised password, factor, IdP, provisioner or policy
   writer can still act.** A reset replaces at most the password. Remove or replace the
   other methods through their owners: enrolled factors, passkeys and recovery codes
   (`account factor`, `account passkey`, `account recovery-codes regenerate`) and
   linked identities (4.5).

Expected result: old session and developer bearers are refused. A fresh login through
a method that is still allowed reaches no read, disclosure, self-regrant or developer
mint. Prove it with synthetic equivalents in a drill. Never replay a stolen production
secret to generate evidence.

### 4.2 Developer delegations

A person ends their own with `hikyo dev session revoke --all` or
`--id CREDENTIAL`. This covers only their own credentials on that instance. An
authorized project administrator inspects and revokes other users' credentials through
Project settings or the project developer-credential API, never by borrowing a bearer.
A reset also retires all the target's delegations. Explicit revocation of a session
revokes its delegations. Ordinary logout does not.

### 4.3 Machine and federation credentials

Reset does not touch these. Enumerate every principal the attacker used or created.

```sh
hikyo sa credential list --sa SA
hikyo sa credential revoke --sa SA --id CREDENTIAL
```

Federation bindings are listed and revoked through `sa credential`. Removing an issuer
(`instance-config federation-issuer remove --id ID`) does not replace enumerating and
revoking its bindings. `sa delete --id ID` releases the identity's grants, so use it
deliberately. `sa credential rotate` revokes its predecessor right after delivery of
the replacement, without waiting for adoption, and a failed final revoke leaves both
live, so check its result. Turn off a project's machine delivery where it is not needed
with `hikyo project-settings machine-reveal set --enabled false`. That does not disable
human access.

### 4.4 SCIM

Fence the provisioner first. A still-authorized provisioner can regrant.

```sh
hikyo scim credential revoke BINDING CREDENTIAL
hikyo scim binding delete BINDING
```

Grants that come from SCIM origins are released through SCIM, not by a manual grant
removal. A surviving lockout-retention origin means the org has no other manager:
assign and verify a clean manager first.

### 4.5 Login providers and linked identities

A compromised allowed IdP or factor can sign in again. Disable or replace the provider
(`instance-config provider disable NAME`, `instance-config oauth2-provider delete SLUG`)
and unlink identities through the account owner. Disabling a provider revokes the
delegations minted through it and does not revoke the user's session at that provider.
Instance-capability holders keep a local credential by design (the human-auth
lockout floor), so do not remove the last one.

### 4.6 Peers

`remote remove` is not revocation. Revoke on the serving instance and reconcile both
sides.

```sh
hikyo remote-credential revoke --id CONNECTION
hikyo remote-invite revoke --id INVITATION
```

## 5. Replace upstream secrets

Replace first, clear local custody last. Hikyo records custody. The provider decides
validity.

| Credential | Order | Verify |
| --- | --- | --- |
| API, cloud, AI, billing, registry or Git provider key | Revoke and replace at the provider with a minimum-scope key from clean custody; then update the value in Hikyo | The old key fails at the provider. Editing the value in Hikyo invalidates nothing |
| Adapter PAT | Pause targets (`adapter target pause`); replace at the provider; `adapter credential set --adapter ADAPTER --value-file PRIVATE_FILE`; remove targets while the old PAT still authenticates if remote names must be scrubbed; only then `adapter credential revoke` | Provider refuses the old PAT. `credential revoke` alone is local only, and a dead PAT leaves remote names orphaned |
| Dynamic provider admin credential | Keep a usable clean admin credential until every lease is terminal. Use `dynamic-provider credential set`, not `credential revoke`, to replace it. Then revoke the exposed credential at the database | `lease list --env ENV` shows no `revoking`, `unknown`, `renewing` or `minting` rows. `hikyo_dynamic_effects_unknown` is zero |
| Dynamic leases | `lease revoke LEASE --env ENV`, then wait for a terminal state. For an `unknown` effect, `lease settle LEASE --env ENV` asks the worker to reconcile it | A dropped PostgreSQL role refuses new logins only. Terminate existing sessions in the database with separately authorized DBA action (`pg_terminate_backend` or the pooler), and verify the exact old role and session set |
| Remote and SCIM credentials | Section 4.4 and 4.6 on the serving side | Peer or provisioner call is refused |
| SSH CA, PKI, transit, TLS and signing keys | Replace through their own owners and revoke issued certificates or signatures where they apply | Relying parties refuse the old material. Envelope rotation is not a replacement |

For a confirmed compromise, revoke exposed authority promptly even if that interrupts
service. For planned rotation, mint the replacement, deploy and test all consumers, then
revoke the predecessor. Scheduled provider rotation belongs to
[#148](https://github.com/Hikyo-Org/Hikyo/issues/148); this runbook is manual.

## 6. Consumer adoption

For every replaced secret, list its consumers: environment variables, rendered files,
Kubernetes Secrets, CI variables, caches, long-lived processes, exports and archives.
Redeploy or restart them only through the clean deployment path, confirm each uses the
replacement, and only then treat the old value as dead. Record any consumer you could
not reach as an unresolved residual. Do not use an exposed secret as a rollback.

## 7. Encryption custody (only when key material may be exposed)

First recover a clean host and configuration. Preserve the independently approved
archive, the original matching root and the age identity in separate failure domains.
New keys do not make a stolen readable copy unreadable. Run each phase to completion
before the next.

1. Generate the new root through the clean custody owner, enroll its alias in
   `rollout.rootSources`, and mount it through the
   [configuration rollout](../site/src/content/docs/docs/configuration-rollouts.mdx)
   procedure. `HIKYO_NEW_ROOT_KEY_FILE` must name the enrolled mounted projection
   `/run/hikyo/rollout/sources/root/ALIAS/root-key`. An arbitrary path is refused
   (`internal/app/next_root_source.go:15`).
2. `hikyo rotate-root-key --prepare --yes`, install the enrolled candidate on every
   node, `hikyo rotate-root-key --verify --yes`, then `--finalize --yes`. An interrupted
   dual-wrapper state is pending, not final.
3. `hikyo rotate-master-key --yes` only after the root is finalized.
4. `hikyo rotate-dek --instance-scope --yes`, then `hikyo reencrypt --instance-scope`,
   and for each affected project `hikyo rotate-dek --org ORG --project PROJECT --yes`
   then `hikyo reencrypt --org ORG --project PROJECT`. Resume re-encryption until no
   rows move; a new DEK version is not completed recovery.
5. If exposed, `hikyo rotate-scanning-key --yes` (drops scanning dismissals) and
   `hikyo rotate-token-key --yes`. Neither revokes a bearer or a provider key.
6. Prove independent root escrow for the finalized root and create and test a new
   encrypted recovery point.

Historical stolen ciphertext stays readable to whoever holds the old key material.

## 8. Restore and reconcile (host or database compromise)

Rebuild on a verified clean installation. Choose an archive believed to predate the
compromise using independent evidence, because restore does not establish that fact.

1. Stop every writer and scheduler on every node. Authenticate the archive.
2. **Drill first** against a disposable, empty scratch target of the matching engine.
   A drill writes health and audit state to the source. It is not read-only.

   ```sh
   hikyo restore drill --from ARCHIVE --identity-file IDENTITY --root-key-file ROOT_KEY_FILE --principal PRINCIPAL --project ORG/PROJECT --target-sqlite NEW_EMPTY_FILE
   ```

   `--identity-file` and `--passphrase-file` are alternatives, not both. The drill
   needs the original root key as a file (`--root-key-file` is required) and never
   persists it.

3. Restore to the empty production target with the installation's configuration
   loaded and `HIKYO_DB` naming the empty target:

   ```sh
   hikyo restore run --from ARCHIVE --identity-file IDENTITY
   hikyo restore status
   hikyo restore reconcile --principal INDEPENDENTLY_APPROVED_PRINCIPAL
   ```

4. Restored authentication state is inert. Reconcile each human and their roles
   independently, prove identity again, enrol clean factors, and review grants,
   rules, providers, federation bindings, remotes and dynamic leases against current
   provider reality before re-enabling any worker. Restore clears outbound credential
   custody. Re-enter clean credentials. It does not revoke any upstream secret.
5. Reapply independently approved privacy receipts through their owner. A restore
   from before a containment action does not carry that action, so re-apply it from
   your journal.
6. Complete the fresh recovery-incarnation evidence and admission before serving.

## 9. Prove the outcome before reopening

1. Revoked human, machine, developer and federation artifacts fail. Intended
   least-privilege replacements succeed. Check running database sessions and consumer
   caches separately.
2. Every affected upstream credential is revoked, provider usage is understood and
   asynchronous lease cleanup reached a verified terminal state.
3. Final key epochs, re-encryption completion, new custody and a successful isolated
   restore drill are recorded. `hikyo doctor` is supporting health evidence, not a
   compromise detector.
4. Generate a synthetic failed login and a denied authenticated action. Require
   independent evidence receipt and a real operator notification. A mechanism that is
   not yet delivered is reported as unavailable, not passed.
5. No unexpected privileged change, process, destination, binding or grant remains.
   Reopen ingress in stages, regrant the independently approved minimum, and keep
   enhanced review for a period chosen from the exposure window and log coverage.
   Keep `HOLD` for any scope you could not verify.

## What the ADR would change

None of the following exists yet. They are listed so the owner can see which manual
steps the accepted decisions will replace once implemented.

- An incident record with per-item `pending`, `done`, `unknown` and `out of scope`
  outcomes, replacing the external journal for non-host incidents.
- One enumeration of affected authority per scope, replacing the listing loop in
  section 3.
- A reversible account containment state, replacing the grant sweep in 4.1.
- A replace-before-clear guard for provider admin credentials.

## Evidence and limits

Source-verified at `426bf59ce`. Tests that cover the behavior this runbook relies on:

| Behavior | Test |
| --- | --- |
| Reset-only fails, grants plus final reset succeed, allowed factor cannot recover authority | `TestManualIncidentContainment`, `TestManualIncidentContainmentAllowedIdPCannotRemintAuthority` |
| Reset revokes developer custody, attributed to the operator | `TestCredentialResetTerminallyRevokesDeveloperCustody`, `TestCredentialResetConcurrentDeveloperMint` |
| Logout leaves delegations, explicit revocation ends them | `TestDeveloperLogoutAndExplicitSessionRevocation` |
| Network reset boundaries | `TestCredentialResetNetwork`, `TestManualIncidentContainmentCLI` |
| Dynamic cleanup stays pending and resumes | `TestManualIncidentDynamicCleanupResumes` |
| Role drop leaves an open PostgreSQL session | `TestProviderRevocationLeavesExistingConsumerSession` |
| Restore clears outbound credentials | `TestCompleteRestoreClearsEveryRestoredOutboundCredential` |
| Recovery order on live data | `TestFullRecoveryOrderOnLiveData` |

This PR ran none of them. It changes no product code. Running them, a disposable
exercise of this procedure, notification delivery, provider cleanup and deployed
behavior are separate claims that remain open.
