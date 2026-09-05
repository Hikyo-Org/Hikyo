# Execution roadmap: bounded 1.0 release, then additive product lanes

Refreshed 5 September 2026 against main `0d591366874ad525b60ca12938dfd0ef2efe12d4`, actual merged work and the current review queue. This assessment uses supported-product correctness, operational safety and explicit release evidence; labels, milestones and closed dependency arrows do not prove release readiness.

The [public roadmap source](https://github.com/Hikyo-Org/Hikyo/blob/main/docs/site/src/content/docs/docs/roadmap.mdx) and [implementation status](https://github.com/Hikyo-Org/Hikyo/blob/main/docs/status/README.md) provide the capability context. This dated issue snapshot sets no release date or promised minor version.

## 1.0 critical path

**#79 and umbrella #41 remain open acceptance work.** The gate is not just signing and tagging. Required supported-product repairs include #575/#576 browser bundle/directory parity, #617 pre-freeze identity/authorization compatibility, and #638 upgrade safety/governance. Every final criterion requires evidence tied to the candidate that will actually be released.

| Work at this assessment | State and evidence |
| --- | --- |
| Production verification gaps, approval leader fencing and docs dependency security | Merged #641, #642 and #643. Their merge is implementation evidence, not an overall release verdict. |
| Browser bundle/directory parity and locked-surface corrections | PR #644 under review, issues #575/#576. Actual immutable bundle and instance directory workflows are required; catalogue editing is not equivalent. |
| Upgrade safety | PR #645 under review, issue #638. Legacy apply refusal preserves discovery/manual operations. It does not implement the complete signed compatibility graph or migration gates. Signed metadata/release binding, the applied-release ledger, exact legacy genesis and mandatory migration gates on both engines remain required implementation before 1.0. |
| Pre-freeze identity and response compatibility | PRs #646/#650 under review, issue #617 and #79. This does not implement social registration. |
| Recovery and operator resource acceptance | PRs #647/#648 under review. Harness delivery/local measurements require final-candidate arm64 resource and separate-custody evidence. |
| Acceptance inventory | PR #649 under review. Test entrypoint inventory does not mean the tests ran or every criterion passed. |
| Provider lifecycle and connection cleanup | PR #652 under review: real Forgejo lifecycle coverage on both engines and private transport connection cleanup. Final-candidate replay remains required. |
| Client revision and generated SDK compatibility | PR #653 under review: endpoint revision enforcement, archived SDK checks on both engines and PostgreSQL UTC timestamps. This is pre-freeze rehearsal, not a published 1.0 freeze. |
| Machine-MCP deployment/client proof | #651 is the separate release acceptance gate. Public HTTPS, real authenticated production-tool calls by each claimed client and deployed multi-process behavior need redacted candidate evidence. |

After reviewed changes merge, run exact-candidate non-skipped SQLite/PostgreSQL acceptance, desktop/mobile browser and locked-prototype checks, recovery/operator floor checks, real-provider tests, API/CLI freeze and bidirectional skew, self-hoster and disclosure-channel checks, then the actual signing/trust ceremony. A stable tag cannot substitute for a missing result. No signed compatibility runtime, public client proof or release version is invented by this roadmap.

## Already merged capabilities

Recovery hardening #145 (PR #590), multi-node HA #146 (PR #570), PostgreSQL dynamic credentials #147 (PR #593), approvals #151 (PR #594 with fencing repair #642) and multi-target adapters #157 (PR #592) are implemented on main. They no longer belong behind future-version clocks. Final acceptance still applies.

Machine MCP's opt-in stateless transport, five bounded read tools and deployment checker are implemented through #637/#639 and preceding work, with conformance CI repair #640. These do not prove every named client's live authenticated path. Existing managed service-account authentication remains the machine boundary.

## First after 1.0: social sign-in and open registration

The [owner's 3 September disposition](https://github.com/Hikyo-Org/Hikyo/issues/527#issuecomment-5531906184) supersedes the former claim that the full social lane is promoted into 1.0. #589/#615 and #605 through #613 remain post-1.0. The pre-freeze slice is #617.

Recommended execution sequence from the recorded handoff:

1. #605 data foundations, then #606 registration policy, then #607 federated sign-up.
2. #608 mailer/local sign-up, #609 OAuth2 identity providers and #612 CLI handoff after their owning dependencies.
3. #610 invitation claim and #611 establishment/password/unlink semantics.
4. #613 closes the complete lane's acceptance.

This ordering does not turn a recommendation into a new hard dependency. Recheck each ticket's owning contract before implementation. The future locked prototype is not a current sign-up feature.

## Other post-1.0 lanes

- #631: decide whether a concrete unmet human-delegation workflow requires embedded MCP OAuth. Evidence collection/no-OAuth disposition can proceed now; identity/consent integration respects #613/#617. Missing protocol/client evidence alone does not establish an OAuth requirement.
- #148 rotation, #152 temporary access and #153 repository/CI scanning. #153 needs its declared ADR amendment; current entry-time scanning remains supported.
- #154 PKI, #155 SSH certificates and #156 Transit/KMS are new signing/cryptographic service boundaries.
- #158 AWS, #159 GitLab, #161 Cloudflare, #162 Vault/OpenBao, #163 sealed webhooks and #164 generic file synchronization add delivery destinations.

Broad #619 deduplication or stylistic cleanup is not itself release acceptance; verified correctness defects and dark verification paths are.

## Maintenance

Update this dated snapshot when scope, merge state or acceptance evidence changes. Keep open PRs separate from merged code and merged code separate from released artifacts. #651 proof and #631 human-delegation decisions remain separate lanes. Never close #79/#41 from dependency-graph completion alone.
