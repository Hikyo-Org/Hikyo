# Issue 631: accepted MCP necessity decision

Entry point: [full evidence and client matrix](../reports/mcp-human-delegation-631.md).
Owning issue: [#631](https://github.com/Hikyo-Org/Hikyo/issues/631).

## Accepted outcome

On 16 October 2026 the owner selected **"A: Retain managed credentials
(recommended)"** after reviewing the evidence package. The parent independently
checked the live issue, full package, machine resolver and registry audit
dispositions and relayed explicit acceptance to this implementation owner.

Retain managed machine MCP for the currently evidenced workflows. A dedicated
bounded automation identity is available where an operator accepts provisioning,
custody, rotation and offboarding work. Successful reads remain unaudited;
machine credentials do not gain human consent, per-call human attribution or
automatic termination when an employee leaves. Current machine grants,
revocation, expiry, epoch and account deletion remain authoritative.

No OAuth code, package evaluation, new ADR or implementation tickets are needed
for this accepted no-OAuth decision. It changes no locked authority. The full
record defines explicit triggers for a new human-delegation necessity decision.
If triggered, the applicable ADR and review gates must be satisfied before
dependent work; the #807/#742 task-specific review waivers do not transfer.

The current source exposes five read tools when MCP is enabled and seven when
the separate write flag also enables stage/validate. This decision preserves
those operative amendments and the separate developer-delivery boundary.
It adds no reveal, secret-entry, write, approval, publish or provider authority.

## Evidence and limits

- Current source baseline: `c4c626a105d0139fcc8a6f5444e585e7a4e0a761`.
- Corrected historical #651 evidence uses supported automation provisioning and
  an actual scoped, nonempty configuration read with official Go SDK `v1.7.0`,
  two PostgreSQL processes and verified private-CA HTTPS. Exact source and local
  image identity are in the full report.
- Later #781 proof records actual Codex `0.155.0` and a pinned T3 client-library
  run on both engines. It supersedes the older Codex transport failure only for
  that exact tested version/profile.
- Installed Codex `0.160.1`, Claude Code `2.1.292`, cached Inspector `2.5.0`, T3
  nightly `0.0.46-nightly.20261011.2955` and Python OpenAI SDK `2.37.0` were
  inventoried read-only. Current runtime remains unverified pending the parent's
  resource slot. No version/help output is presented as a successful tool call.
- [#651](https://github.com/Hikyo-Org/Hikyo/issues/651) stays open for its exact
  candidate/public HTTPS/named-client/deployment acceptance. This decision
  supplies reusable evidence but neither closes nor narrows those requirements.
- The full report has a self-contained offline HTML companion outside Git.
  T3 HTML previews at widths 390 and 1280 showed no document-width overflow,
  four tables, two sections, zero external assets and no console errors.

## Review and validation

The source counterevidence review and draft Standards/Spec review used
GPT-6.1 Sol high. The Astra owner independently inspected the decisive resolver,
artifact admission, developer-operation restriction, registry, feature gates
and ADR governance source. Draft R1 found no actionable findings. This is
ordinary OpenAI review, not cross-provider review or fresh runtime proof.

Only Markdown documentation changes. Local validation passed for all 20
repository-relative links, whitespace, no new em-dashes and no machine-specific
paths. No product
code, dependency, generated API, schema, permission or operator configuration
changes are included. Hosted CI and final-head review remain delivery gates.

## Completion protocol

The necessity decision is accepted. Delivery is not complete merely because
the report exists: retain signed/DCO commit verification, GitHub verification,
current-head review and required CI through the protected merge path. Verify
GitHub's merge timestamp/SHA and ancestry in current main before closing #631.
The parent owns PR watching and routes failures back to this owner.

The closeout must say that no OAuth was selected, name the accepted lifecycle
limitations and revisit triggers, link the merged evidence and leave #651 open.
Release publication and production deployment remain outside authorization.
