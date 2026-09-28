# Open issue audit, 2026-09-28

Every open issue audited against `main` @ `85c0d49c`. A merged pull request
title was never taken as proof; evidence is source, tests, or a recorded run.
No issue was close-ready. Types: F feature ticket, V validation gate,
D decision ticket, U umbrella.

## Recently merged, open for incomplete acceptance

| Issue | Type | Missing | Needs |
| --- | --- | --- | --- |
| #158 AWS Secrets Manager | F | `TestAWSSecretsManagerRealSmoke` never ran; amendment 8 cross-model review skipped | Owner: sandbox AWS key |
| #161 Cloudflare | F | `TestCloudflareRealLifecycle` never ran (the only end-to-end proof) | Owner: sandbox account, token, disposable script and project |
| #164 file sync | F | macOS CI leg (this PR); amendment 9 is "pending owner ratification" | Owner: ratify amendment 9, which drops watch mode |

Watch mode was not built. The resident watcher and `--watch` rows of
mvp-boundary § 4 are Out, and declared amendment 9 drops the ticket's watch
mode. Building it needs an owner decision that reverses those rows.

## Documentation, design, or partial validation shipped

| Issue | Type | Met | Missing | Needs |
| --- | --- | --- | --- | --- |
| #807 local development | U | Ask A, `local-development.mdx` | Ask B: ADR and code for the delegated credential | Owner: split or grill |
| #791 reporting end to end | V | Service and fake-client coverage of all seven scenarios | Real cluster with real browser suite, CI job, screenshots | Agent (branch `test/791-delivery-target-reporting-e2e`) |
| #805 workload webhooks | D | Proposed ADR | Lock D1 to D10, then all code | Owner |

## Everything else

| Issue | Type | State | Needs |
| --- | --- | --- | --- |
| #819 file targets web UI | F | Nothing built; `api/parity.yaml` rows still `{issue: 819}` | Agent |
| #820 transit web UI | F | Nothing built; 15 parity rows still `{issue: 820}` | Agent |
| #148 secret rotation | F | Nothing built; mvp-boundary "Arbitrary third-party rotation: Out" unamended | Owner amendment, then agent |
| #608 mailer and sign-up | F | Mail posture and test send exist; no `/auth/signup`, no pending-row flow | Agent |
| #609 OAuth2 GitHub | F | Schema only (migration 00057) | Agent; preview needs an owner GitHub OAuth app |
| #610 invitation claim | F | Not built | Blocked by #609 |
| #611 establish purpose | F | Not built | Blocked by #609 |
| #612 CLI login handoff | F | CLI refuses both transports by name | Agent |
| #613 A7 acceptance closure | V | Partial flows and one bound row | Blocked by #608 to #612 |
| #615 social sign-in spec | U | Three of nine children closed | Closes with #613 |
| #619 slop audit | U | PRs A, B1, C-store, C-service, D, E merged | B2, C-cli, C-web (agent); four architecture questions (owner) |
| #631 MCP delegation | D | Disposition only | Evidence matrix (agent); decision (owner) |
| #651 MCP release proof | V | Local two-replica HTTPS proof | Owner: public deployment and client credentials |
| #79 1.0 gate | V | 42 internal passes at a candidate 209 commits old | Owner: scope decision, signing ceremony, tokens |
| #41 1.0 spec | U | Children closed | Closes with #79 |
| #527 roadmap | D | Snapshot stale against main | Agent draft, owner publishes |

`docs/release/release-plan-1.0.md` lists transit, PKI, SSH certificates, the
new adapters, temporary access and repository scanning as not in 1.0; all are
merged. The #79 scope decision is the most consequential open item.

## Review status

Cross-provider review of this audit's changes: skipped, not CLEAN (quota
unconfirmed).
