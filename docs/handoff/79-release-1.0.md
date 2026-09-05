# #79 release acceptance inventory handoff

Worktree `/tmp/hikyo-release-acceptance`, branch `codex/1.0-release-acceptance`, base `944d23e5ffb3e7cae3accb8d9175c96d8681458c`. Parent owns final review, signed DCO commit, PR, exact-candidate CI, merge and release. No tag or release keys created.

## Delivered

- `docs/release/acceptance-1.0.md`: 49 criterion rows with actual test references, CI lanes and honest candidate-pending/gap/external/ceremony dispositions. Includes C-APV and records A7's post-1.0 scope explicitly; flat-model C1/C2 counted once; GitHub unnumbered paragraph split into its three check classes, labelled as ledger-local IDs.
- `api/acceptance_ledger_test.go`: inventory closure against current normative criterion IDs and linked test declarations parsed with Go AST. It cannot assert a release pass.
- `docs/spec/api-cli-spellings.md`: operative section 9 settles hierarchy/key/value/settings/SCIM/import/rotation spellings, precedence over stale examples, credential-reset versus account reset-credential, advertised revision 2/minimum 2 disposition, existing human approvals for error codes and binary size.
- HTML questions/options/choices and known gaps in `docs/reports/1.0/acceptance-ledger.html`.

## Real gaps, reported to parent

S1 frozen-client/current-server execution is absent at baseline. Minimum-revision checks have limited explicit callers; remote-add's existing refusal fixture cannot prove general client behavior. Freeze schema guard already exists and stays dormant until v1.0.0.

Live GitHub suite `TestGitHubDotComContract` is opt-in and skips when any required dedicated credential/configuration is missing. It is not wired by ordinary CI. Real Forgejo lifecycle is also opt-in. Link actual external evidence before certifying M4/GitHub promotion; stubs do not substitute.

Remaining likely-growable response enums: AdapterProvider and SamlProviderWarning.code. Recommended open before freeze with unknown-consumer fixtures; parent notified. #617 handles its four named identity/purpose/origin enums independently. DynamicProviderKind is deliberately closed PostgreSQL under #147; no speculative widening.

API revision baseline is 2, not 1: definitions export/check/plan/getPlan/apply/getSettings/setSettings and machine-reveal get/set require 2; 259 other operations require 1. Preserve, do not downgrade minima. Binary size and conflict/limit_exceeded already accepted by Marc; final candidate measurement remains pending.

## Checks

Targeted ledger/freeze/operation-contract tests passed; CLI frozen-help and remote-add minimum-refusal tests passed. Removing the C1 ledger row and substituting a nonexistent Go test name each failed closure as intended; files restored afterward. Full API package passed in 1.243 seconds; whitespace checks passed. Logs `/tmp/hikyo-79-ledger-check.log`, `/tmp/hikyo-79-spellings-check.log`, `/tmp/hikyo-79-missing-criterion.log`, `/tmp/hikyo-79-deleted-test.log`, `/tmp/hikyo-79-api-final.log`.

The ledger must be filled from the final reviewed candidate, never from these inventory-only checks. Inspect both engine legs, browser viewport coverage, external-suite skips, floor/operator artifacts, published docs/fallback and signing ceremony before any 1.0 claim.
