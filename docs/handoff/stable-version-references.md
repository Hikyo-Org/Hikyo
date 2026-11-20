# Stable-version references after 1.1.0

## Result

- `CAP-PUBLIC-RELEASE` names stable v1.1.0, published on 2026-11-08 from
  `c56b8b44820a6e088920759f974aabc456d737ef` with 80 assets. Its evidence links
  the v1.1.0 release. v1.0.0 keeps the API/CLI freeze and its readback claim.
- `CAP-MEMBER-ACCESS-RULES` cites the new tickets: #1446 (all/future-project
  rules) and #1447 (environment-name binding).
- Documentation that described "the current stable release" as 1.0.0 now uses
  one of three forms:
  - On the site, the `%STABLE_VERSION%` token, which follows
    `release/trust/metadata.json`.
  - In repository Markdown, version-free wording or a `releases/latest` link.
  - For statements about the 1.0.0 artifact itself, an explicit, verified
    1.1.0 statement.
- Historical and technical 1.0.0 references are unchanged on purpose: the API
  freeze baseline, the FIPS module `v1.0.0`, frozen `v1.0.0` clients, the 1.0
  release plan and acceptance records, and examples.

## Keeping it current

- `scripts/ci/check-doc-status.mjs` now refuses a ledger whose
  `CAP-PUBLIC-RELEASE` does not name `v<highest_release>` from
  `release/trust/metadata.json` and cite that release's URL.
- The ceremony's sync-trust PR changes that file, which selects docs CI
  (`classify-changed-paths.sh`), so the trust PR fails until the release
  coordinator updates the ledger on it.
- `ceremony.sh sync-trust`, `signing.md` and `online-signing.md` all state
  that step.
- Fixture coverage: `check-doc-status_test.sh` covers a ledger behind the
  published stable release and a ledger naming only a prerelease of it.
- A `sync-trust` retry keeps that ledger commit (the ledger and its two
  generated READMEs) and still refuses any other change on the trust branch;
  `stable-ceremony_test.sh` covers both.

## Evidence

- The `v1.1.0` tag resolves to `c56b8b44…`, which matches the trust metadata
  `source_commit`.
- The chart at `v1.1.0` sets `HIKYO_ROOT_KEY_FILE` and sanitizes the
  `helm.sh/chart` label.
- On 2026-11-19, `scripts/deploy/render_gitops.py` with Helm 4.2.3 accepted the
  published 1.1.0 chart
  (`sha256:bab52fb111e56d05d4178c938f7d12409139067176d4082e3cce91d89f150d6d`)
  for Flux and Argo CD. This included Cosign verification of the chart and the
  image. No cluster was contacted.
- `security-maintenance` is present at `v1.1.0` and absent at `v1.0.0`.

## Remaining

- The GitOps pages now carry `dev`'s record of a 15 November zero-replica Flux
  and Argo CD install of the 1.1.0 chart. That is controller mechanics only; no
  application or database install of a stable chart is recorded.
- Machine access rules (whether a machine rule may be key-narrowed) and a
  server-side resolver still need owner decisions before they can be ticketed.
- Milestone hygiene for v1.0, v1.0.1 and v1.1 was not changed.
