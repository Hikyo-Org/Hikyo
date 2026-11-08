# Personal values: implementation handoff (#1235, PR #1167)

## Result

The [personal-values ADR](../adr/personal-values.md) is implemented on PR #1167, the same PR as the ADR, which is accepted and
operative. Every item of its Implementation acceptance list is backed by code and a test on SQLite and PostgreSQL, except where the
limitations below say otherwise.

What exists:

- **Storage.** Migration `00086_personal_values.sql` (both engines): `personal_values`, `personal_value_notices` and
  `key_personal_required_environments`. Store methods take the owner from the proof (`authz.VerifyOwner`), never from an argument.
- **Envelope.** `crypto.PersonalValueAAD` binds org, project, environment, key and owner. Reencrypt walks the rows.
- **Service `PersonalValues`.** It covers list, set, reveal, delete own, members, delete member, counts and the offboarding report.
  - Every operation refuses non-human callers, sessionless callers, developer credentials and workspace sessions, with the
    nonexistent answer.
  - The owner lists and deletes their own values whatever their grants.
  - Set and reveal take the per-key reveal ceremony. A passkey or authenticator window opens it. An OIDC window opens it only for a
    passwordless account with no factor that qualifies through that same provider, which must carry an assurance policy.
  - Set refuses protected environments. A project cap applies.
- **Account rules.**
  - Network credential reset refuses an account with a linked identity at mint and at consumption. The refusal is uniform, and the
    audit cause is `linked-identity`.
  - Losing the last factor, consuming a recovery code, establishing a password on a factorless or federated-qualified account,
    cloned-passkey disablement and an identity unlink that leaves the account unqualified each delete the owner's values in that
    transaction.
  - A privacy erase deletes the subject's values and notices, and audits the count with the `erasure` cause, without key names.
- **Delivery.**
  - Developer credentials get an overlay. Without consent, under `--config-only`, or with a suspended value, they get names only, and
    notices carry their cause.
  - A personal change never answers `current`.
  - `personal_required_in` makes a missing value fail only the owner's run.
- **Schema and lifecycle.**
  - Key delete, rename, definitions apply, environment delete, `forbidden_in` and restore cascade, and leave notices. The restore
    purge runs only for archives at schema 86 or later, and only for owners holding values.
  - A definitions plan reports how many personal values each environment deletion removes.
  - Schema publish never evaluates a personal value. The owner is warned on set and reveal, in the CLI and the browser.
  - A fetch carries at most 1 MiB of the owner's personal values, a cap apart from the shared rows.
  - The retention sweep suspends on predicate loss, clears on regain, and deletes after 14 days with a `retention` notice.
- **Report.** Matrix counts with the eligible denominator, and the offboarding report.
- **CLI.**
  - `hikyo personal list|get --reveal|set|set --clear [--member]|members|counts|offboarding-report`.
  - `hikyo dev session --personal` gives consent.
  - `hikyo run` refuses withheld, noticed and missing-required keys on the credential path. On the human-session path it refuses
    keys the caller holds a value or notice for, and fails closed when it cannot confirm the server's API revision.
- **Browser.**
  - My personal values page: set, reveal and delete, plus notices and their dismissal.
  - Matrix personal marks and counts, with a key-panel member delete.
  - The offboarding report.
  - The ceremony and the developer-credential approval page show consent.
  - There is no `.env` download.
- **ADR amendments.** Tenant isolation is amended for the scheduler's retention doors.

## Acceptance map

Isolation tests are in `internal/isolation/` and run on SQLite and PostgreSQL.

| Acceptance item | Evidence |
|---|---|
| No grant, preset or org-wide rule reads another person's value; no administrator overwrites one | `TestPersonalValueNoGrantPresetOrRuleReadsAnother`, `TestPersonalValueOverlayAndAdministratorBlindness` |
| Network reset refuses linked-identity accounts at mint and consumption; a factor holder keeps values and the password alone reaches nothing | `TestPersonalAccountNetworkResetRefusesLinkedIdentityAtMint`, `...AtConsumption` (asserts the `linked-identity` cause), `TestPersonalAccountFactorHolderKeepsValuesAfterReset` |
| Set and get refuse a non-qualifying account; a federated proof does not count while a password stands | `TestPersonalAccountFederatedQualification`, `TestPersonalValueGateRefusesFederatedWindowAndProtectedEnvironment`, `TestPersonalValueWithheldWithoutConsentConfigOnlyOrQualification` |
| Last factor, recovery code and a password on a factorless account delete values in that transaction | `TestPersonalValueFactorLossDeletesAndLeavesNotice`, `TestPersonalAccountTOTPRemovalPurges`, `TestPersonalAccountClonedPasskeyPurges`, `TestPersonalValueRecoveryCodeConsumptionPurges`, `TestPersonalAccountSetPasswordOnFactorlessAccountPurges`, `TestPersonalAccountPasswordOnFederatedAccountPurges`, `TestPersonalAccountUnlinkPurgesUnqualified`, `TestPersonalAccountFederatedUnlinkNeedsProof`, `TestPersonalAccountPrivacyEraseDeletesValuesAndNotices` |
| A federated window opens a value only through the provider the account qualifies through | `TestPersonalAccountFederatedWindowBoundToOpener` |
| A planted recovery code destroys but never reads | `TestPersonalAccountPlantedRecoveryCodeNeverReads` |
| No personal value in machine delivery, adapters, snapshots, revisions, diffs, copy, clone, receipts, `exportValues`, MCP or definitions export | `TestPersonalValueExcludedFromSharedSurfaces`, `TestPersonalValueCiphertextReadersAreConfined`, `TestPersonalValueStaysOutOfExport`, `internal/mcpserver/personal_exclusion_test.go` |
| Schema publish never evaluates a personal value; the owner is warned and still receives it | `TestPersonalValueWarningsNeverRefuseAndPublishIgnoresThem` (now also delivers the value that breaks the rule); stories `SetWarns`, `RevealWarns` |
| A missing required value fails only the owner's run, never a publish | `TestPersonalRequiredInDeliversMissing`, `TestPersonalRequiredInRulesAndAuthority`, `TestPersonalRequiredInDefinitionsRoundTripAndEnvironmentDelete` |
| AAD refuses a moved row; DEK rotation and reencrypt cover rows | `TestPersonalValueMovedToAnotherOwnerDoesNotOpen`, `TestPersonalValueReencryptAfterDEKRotation` |
| Predicate loss stops delivery at once; deletion at 14 days; regain restores | `TestPersonalValueRetentionSweep`, `TestPersonalValueWithheldWithoutConsentConfigOnlyOrQualification` |
| Restore, rename, delete plus re-declare, `forbidden_in`, lost qualification and administrator deletion leave a notice that makes `hikyo run` refuse | `TestPersonalValueRestorePurges`, `TestPersonalValuesSchemaVersionMatchesTheMigration`, `TestPersonalValueKeyRenameAndDeleteCascade`, `TestPersonalValueApplyKeyDeleteAndRenameCascade`, `TestPersonalValueForbiddenCascade`, `TestPersonalValueFactorLossDeletesAndLeavesNotice`, `TestPersonalValueOverlayAndAdministratorBlindness`; CLI refusal in `internal/cli/personal_run_internal_test.go` |
| No consent, human session, `--config-only` or a suspended value: names only, and `hikyo run` refuses | `TestPersonalValueWithheldWithoutConsentConfigOnlyOrQualification`, `TestRunRefusesWithheldAndNoticedPersonalKeys`, `TestHumanSessionRunRefusesHeldPersonalKeys`, `TestRunHumanSessionRefusesAHeldPersonalKey`, `TestServesPersonalListFailsClosed`, `TestHumanSessionRunFailsClosedOnLookupError`, and the real-server CLI test below |
| Developer credentials, workspace sessions and MCP refused; uniform `--member` answer | `TestRefusePersonalCallerAdmitsOnlyAnOrdinaryHumanSession`, `TestPersonalValueOverlayAndAdministratorBlindness`, `TestMCPExposesNoPersonalValueSurface` |
| A personal change or deletion is never answered `current` | `TestPersonalValueOverlayAndAdministratorBlindness` (deletion and the owner's own replacement) |
| Every personal audit event has a real emitter | `TestAuditCore` (personal lifecycle on its own instance); `TestPersonalRequiredInRulesAndAuthority` and `TestPersonalRequiredInDefinitionsRoundTripAndEnvironmentDelete` assert `personal_required_changed` |
| The personal render cap counts personal values only | `TestPersonalValueRenderCapCountsOnlyPersonalValues` |
| The report matches developer-credential disclosures, states its window, contains no values | `TestPersonalOffboardingReport`, `TestPersonalOffboardingReportCLISQLite`, `...Postgres` |
| Deletion plan reports the personal values it removes (counted at plan time) | `TestPersonalValueApplyEnvironmentDeleteAuditsPerOwner` |
| Browser completes every task; CLI completes every task | Playwright `web/e2e/flows/reveal.spec.ts` › personal values (desktop and mobile); `TestPersonalValueCLIAgainstARealServer{SQLite,Postgres}` drives set, list, reveal, members, counts, `run` with and without consent, `--config-only`, clear, and `--personal-required-in` keep and clear against a real local server |
| Matrix stories for the new marks at desktop and phone width | `Matrix.stories.tsx` `PersonalMarks`, `PhonePersonalMarks` (also assert the own list is fetched only where counts fail); `PersonalValues.stories.tsx`, `OffboardingReport.stories.tsx` |

## Mutation checks

1. **Owner filter dropped.** The SQLite owner reads drop their owner filter: `TestPersonalValueOverlayAndAdministratorBlindness/sqlite`
   fails.
2. **`refusePersonalCaller` returns nil.** `TestRefusePersonalCallerAdmitsOnlyAnOrdinaryHumanSession` fails for the workspace session,
   developer credential, workload, automation and sessionless human.
3. **Refusal removed from `hikyo run`.** With the personal-key refusal removed from `runDeveloperOnline`, the real-server CLI test
   fails.
4. **Required setting not kept.** With the server no longer keeping `personal_required_in` on a declaration update that omits it, the
   real-server CLI test fails at "checklist after a declare without the flag".
5. **SAML counted again.** With the previous `personal_qualification.go` (SAML qualifies), `TestPersonalAccountFederatedQualification`
   fails at "SAML identity".
6. **Render cap counts shared rows.** With the cap summing every row again, `TestPersonalValueRenderCapCountsOnlyPersonalValues`
   fails.
7. **Unlink purge removed.** Without the purge in `UnlinkIdentity`, `TestPersonalAccountUnlinkPurgesUnqualified` fails on both
   engines.

## Limitations

- **Federated-only developers.** They can set and reveal values, but minting a developer credential still needs a local factor
  (developer-credentials ADR), so they must enrol a factor before `hikyo run` can deliver them. Owner decision O9 (2026-11-07)
  keeps that rule; the `hikyo dev session` refusal, the personal values page and the docs now state it and point to
  Settings > Security.
- **SAML.** A SAML identity does not qualify an account, and no SAML window opens a personal value; the ADR now says so. SAML
  reauthentication does not bind the opening provider, and `reauth_windows.factor_class` cannot store `saml`
  (migration 00006, both engines), so no SAML window can exist today. That constraint mismatch predates this PR. Counting SAML would
  leave a SAML-qualified owner holding values it can never set or reveal.
- **CLI-approved federated windows.** A window approved in the browser and redeemed by the CLI is checked against the CLI session's
  provider, not the browser session's. Any opener must itself have passed its assurance policy with a possession factor, so the gap
  is only that provider losing qualification during the window's lifetime.
- **Test coverage gaps.**
  - The federated tests create the identity link and window by SQL. No real provider ceremony is driven.
  - Adapter exclusion rests on the source confinement pin (`TestPersonalValueCiphertextReadersAreConfined`), with no runtime adapter
    test.
  - The report's 404 rule is not mutation-checked.
- **Warnings.** Schema warnings show on set and reveal, not on list.
- **Restrict.** A privacy restrict does not purge: release reverses it. Its grant revocation suspends the values at once, and the
  retention sweep deletes them after 14 days.
- **Refusal codes.** The 403 variants (unqualified, factor required, protected) share the uniform refusal code. The browser infers the
  remedy from local facts.
- **Browser.**
  - Inside a workspace, the browser's factor checks read the home instance.
  - Phone matrix marks are truncated visually; the accessible name is full.
- **Permissions.** `PersonalRequiredList` is granted to every operation that reads the catalogue, a read-only superset.
- **Revisions.** Changing `personal_required_in` bumps the schema revision.
- **Review needed.** The sensitive-inventory pins need maintainer security review.

## Reviews

Fresh-context Standards and Spec/security reviews ran on `claude-opus-5-5` (Anthropic only; no cross-provider review) at each stage of
this PR. Their findings were fixed and re-reviewed.

The final whole-PR round found 4 Medium and 14 Low Standards findings and 8 Spec findings plus two test gaps. All were fixed or
recorded, among them: the restore purge on pre-00086 archives, unlink and privacy-erase purges, federated windows bound to their
opener, the human-session `run` refusing only held keys and failing closed, the owner's list on a read transaction, and the checklist
hidden after access loss. The re-review confirmed every fix and found 11 Low findings, all fixed except the two recorded under
Limitations (SAML, CLI-approved federated windows). A third review of that last delta found no High or Medium findings and three
Lows (the SAML exclusion missing from the ADR and untested, an unused parameter, a registry comment); all three are fixed.

## Amended ADR tickets

These fourteen ADRs carry the amendment banner:

- developer-credentials
- compose-integration
- flat-model
- audit-model
- encryption-model
- api-cli-surface
- permission-model
- human-auth
- source-of-truth
- schema-model
- mvp-boundary
- threat-model
- revision-model
- ops-spec

Tenant isolation gains a declared amendment for the retention sweep. The originating tickets found are #807 (developer-credentials,
compose-integration) and #760 (human-auth). None were reopened, as instructed.

## Next steps

1. Fix the `reauth_windows.factor_class` constraint so a SAML window can be stored, bind the SAML window to its provider, then count
   SAML qualification again with a SAML seed helper and test.
2. Decided by O9 (2026-11-07): a federated-only developer may not mint a developer credential with an identity-provider window.
   Revisiting it needs an amendment to the developer-credentials ADR.
