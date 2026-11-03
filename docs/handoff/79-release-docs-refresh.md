# 1.0 landing page and documentation refresh

Related release tracker: [#79](https://github.com/Hikyo-Org/Hikyo/issues/79).

## Result

The landing page, README, installation and introductory guides now describe
[published v1.0.0](https://github.com/Hikyo-Org/Hikyo/releases/tag/v1.0.0).
The [publication record](../release/1.0.0.md) binds the announcement to source,
manifest, image and chart identities and retains the published qualification
limits. The installation guide includes a consumer bundle-verification procedure.

The canonical `CAP-PUBLIC-RELEASE` entry records publication while retaining
partial status for its separate verification and qualification closeout.
Generated README and status tables follow that entry. Historical acceptance
rows remain unchanged. Dated planning snapshots are labeled as historical,
and the roadmap's browser boundary agrees with the shipped Transit guide.

The OSS policy gate now requires the published release link and refuses stale
unpublished wording. Its negative fixtures cover both the landing page and
installation page. Signing and release pipelines are unchanged.

## Validation and review

Run from the repository root with the pinned contributor toolchain:

```sh
scripts/ci/fast-feedback.sh --working-tree --plan
scripts/ci/fast-feedback.sh --working-tree
node scripts/ci/check-doc-status.mjs --check
scripts/ci/check-doc-status_test.sh
pnpm --dir docs/site run verify
scripts/ci/check-oss-policy_test.sh
go test -p 2 ./api -run '^TestReleaseAcceptanceLedgerReferences$' -count=1
```

Expected results are zero selected feedback failures, no Astro diagnostics,
passing docs/PWA/CSP/content checks, rejected stale-status fixtures and a
passing criterion-reference inventory. Local builds omit optional analytics
unless deployment configuration is provided; they do not prove deployed
analytics behavior. The verification command's flags are checked against the
installed CLI; no complete public release download or installation is claimed
by this documentation task.

Independent Standards and Spec reviews cover the complete change range.
Initial findings concerned a broken acceptance anchor, a stale linked browser
guide and a maintainer-only verification destination. Each was repaired before
the final source review. Review and validation results belong to their exact
candidate, separately from hosted CI and publication.

## Remaining delivery steps

The authorized endpoint is a signed documentation PR. Hosted checks, maintainer
review and merge remain separate. For visual evidence, desktop and mobile
before/after captures must be attached to the PR and embedded in its description.
Captures are local-only until attached; no portable hosted image location is
available in this handoff. Keep the PR in draft while that evidence is missing.

After approved merge, the existing Pages workflow owns publication. This task
does not claim a site deployment, change release trust, close qualification
issues or certify an installed server.
