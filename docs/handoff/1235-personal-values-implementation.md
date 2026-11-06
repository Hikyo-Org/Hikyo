# Personal values: implementation handoff (#1235, PR #1167)

## Result

The server, store, CLI and delivery slice of the [personal-values ADR](../adr/personal-values.md) is implemented on PR #1167, the
same PR as the ADR. The ADR is marked accepted and operative. The PR is not merged: the ADR's Implementation acceptance list is only
partly met, and the owner authorized the merge only once all of it holds.

What exists:

- Migration `00086_personal_values.sql` (both engines): `personal_values` and `personal_value_notices`, with owner-bound store methods.
  The owner comes from the proof (`authz.VerifyOwner`), never from an argument.
- Envelope: `crypto.PersonalValueAAD` binds org, project, environment, key and owner. Reencrypt walks the rows.
- Service `PersonalValues`: list, set, reveal, delete own, members (metadata only, audited as `personal_value.holders_read`) and
  delete member. Every operation refuses non-human callers, sessionless callers and workspace sessions with the nonexistent answer.
  Set and reveal take the per-key reveal ceremony, and only a passkey or authenticator window opens it: a window an identity provider
  opened (OIDC or SAML) is refused. Set refuses protected environments. Qualification counts standing TOTP and passkey factors.
- Delivery overlay for developer credentials only. Withheld causes are `consent`, `config_only` and `suspended`, and notices carry their
  cause. A value crosses only for a key the credential's projection serves. Personal changes, and losing or regaining the predicate,
  move the cursor.
- The `--personal` consent is bound into the developer-credential ceremony (CLI, server and the browser approval page, which shows it).
- Cascades: key delete, key rename (not a same-name rename), definitions apply, environment delete (audited per owner), and factor loss
  (last factor or recovery code) delete values and leave notices.
- CLI: `hikyo personal list|get --reveal|set|set --clear [--member]|members`, and `hikyo dev session --personal` for consent.
  `hikyo run` refuses withheld and noticed keys on the credential path, and refuses held keys on the human-session path.
- API Revision 15 with six operations, regenerated Go and TypeScript clients.

## Acceptance map

Tests are in `internal/isolation/personal_values_e2e_test.go` unless noted. Each runs on SQLite and PostgreSQL.

| Acceptance item | State |
|---|---|
| No grant or administrator reads or overwrites another person's value | Done for the administrator template, including the administrator's own set on the same key: `TestPersonalValueOverlayAndAdministratorBlindness`. **Remaining:** member-access rules, org-wide rules and presets |
| Network reset refuses linked-identity accounts at mint and consumption | **Remaining** |
| Set and get refuse a non-qualifying account | Done for factorless accounts and federated windows (`TestPersonalValueGateRefusesFederatedWindowAndProtectedEnvironment`): `TestPersonalValueWithheldWithoutConsentConfigOnlyOrQualification`, `TestPersonalValueFactorLossDeletesAndLeavesNotice`. **Remaining:** federated proof while a password stands (federated qualification not implemented) |
| Last factor, recovery code, password on factorless account delete values | Done for passkey removal and recovery codes: `TestPersonalValueFactorLossDeletesAndLeavesNotice`, `TestPersonalValueRecoveryCodeConsumptionPurges`. **Remaining:** password established on a factorless account, and tests for the TOTP-removal and cloned-passkey purges (implemented, untested) |
| Planted recovery code destroys but never reads | Partly: `TestPersonalValueRecoveryCodeConsumptionPurges` proves deletion in the consuming transaction |
| No personal value in machine delivery, adapters, snapshots, revisions, diffs, copy, clone, receipts, `exportValues`, MCP, definitions export | Done for `exportValues` (`TestPersonalValueStaysOutOfExport`), MCP (`internal/mcpserver/personal_exclusion_test.go`) and custodian delivery (overlay test). **Remaining:** explicit tests for the other surfaces |
| Schema publish never evaluates a personal value; owner warned | **Remaining** (the owner warning is not implemented) |
| Missing required personal value fails only the owner's run | **Remaining** (`personal_required_in` not implemented) |
| AAD refuses a moved row; DEK rotation and reencrypt cover rows | Done: `TestPersonalValueMovedToAnotherOwnerDoesNotOpen`, `TestPersonalValueReencryptAfterDEKRotation` |
| Predicate loss stops delivery; deletion at 14 days; regain restores | Partly: suspension stops delivery and is not answered `current` (withheld test). **Remaining:** 14-day sweep and restore on regain |
| Notices for restore, rename, delete plus re-declare, `forbidden_in`, lost qualification, administrator deletion | Done for rename and delete plus re-declare through `Keys` (`TestPersonalValueKeyRenameAndDeleteCascade`), lost qualification and administrator deletion; a same-name rename keeps values (`TestPersonalValueSameNameRenameKeepsAndEnvironmentDeleteAudits`). **Remaining:** restore, `forbidden_in`, and tests for the definitions-apply cascades (implemented, untested) |
| No consent, human session, `--config-only`, suspended: names only and `hikyo run` refuses | Done: server side in the withheld test, CLI refusal in `internal/cli/personal_run_internal_test.go`. **Remaining:** a CLI test against a real local server |
| Developer credentials, workspace sessions, MCP refused; uniform `--member` answer | Done: `internal/service/personal_internal_test.go`, overlay test, MCP exclusion test |
| A personal change is never answered `current` | Done: overlay test (administrator deletion and notice dismissal are not answered `current`) |
| Disclosure report | **Remaining** |
| Browser completes every task; Playwright and CLI tests; matrix stories at desktop and phone width | **Remaining** (no browser UI yet) |

Also remaining from the ADR:

- Owner list and delete still require `read` on the environment, where the ADR says the owner manages their own values whatever their
  grants.
- `members` and member delete require `manage-members` at project level; the ADR allows a holder covering the key in the environment.
  This is stricter than the ADR, not a leak.

## Reviews

Fresh-context Standards and Spec/security reviews ran on `claude-opus-5-5` (Anthropic only; no cross-provider review). Fixed from them:
the federated-window gate (High), the cursor on predicate changes, protected environments, same-name rename, missing wire-registry
purge events on recovery and cloned-passkey routes, the personal cap (now locked, excluding the caller's own cell, `ErrLimitExceeded`),
environment-delete audit, holder lookup errors, consent binding into the ceremony, the projection bound on delivered values, and weak
test assertions. The re-review of these fixes is recorded on the PR.

## Mutation check

Two central guards were broken on purpose and the failures captured.

1. The SQLite owner reads (`GetPersonalValueForOwner`, `ListPersonalValuesForOwner`) drop the owner filter:
   `TestPersonalValueOverlayAndAdministratorBlindness/sqlite` fails with `administrator list: err=<nil> entries=1`.
2. `refusePersonalCaller` returns nil: `TestRefusePersonalCallerAdmitsOnlyAnOrdinaryHumanSession` fails for the workspace session,
   developer credential, workload, automation and sessionless human. The e2e overlay test still passes under this mutation because
   authorization refuses developer credentials independently.

## Amended ADR tickets

Fourteen ADRs carry the amendment banner: developer-credentials, compose-integration, flat-model, audit-model, encryption-model,
api-cli-surface, permission-model, human-auth, source-of-truth, schema-model, mvp-boundary, threat-model, revision-model and ops-spec.
The originating tickets found by search are #807 (developer-credentials, compose-integration) and #760 (human-auth); the others have
no ticket recorded in the repository. None were reopened, as instructed.

## Next steps

1. Implement the remaining rows above, starting with the browser surface and its Playwright tests, then the retention sweep and
   restore, `personal_required_in`, the schema warning, and the linked-identity reset refusal.
2. Add the real-server CLI test and the remaining exclusion tests.
3. Rerun the Standards and Spec reviews on the complete change, then merge with
   `gh pr merge 1167 --merge --auto --match-head-commit <sha>` once `ci-dev-required` is green and no thread is unresolved.
