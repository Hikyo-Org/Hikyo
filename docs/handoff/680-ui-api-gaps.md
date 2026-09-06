# Issue #680: API support for audited UI behaviours

Source: [#680](https://github.com/Hikyo-Org/Hikyo/issues/680),
[UI audit](../reports/ui-audit-2026-09-05/README.md), and the locked ADRs linked
from [the UI specification](../spec/ui-spec.md).

## Resource contracts

- Revision summaries retain the publisher principal ID and add its display
  name. Missing principals retain their lineage ID. Revision comparison accepts
  two retained revision numbers; the ordinary response compares configuration
  values but exposes only write-presence for secret rows. A separate per-key
  disclosure applies each side's current or historical reveal authorization,
  the existing ceremony, and an audit event for each opened revision.
- Machine-credential minting returns its effective expiry at the top level,
  preserving the display-once client projection. The panel distinguishes finite
  expiry, explicit indefinite lifetime, and unavailable metadata.
- GitHub adapters expose the GHES API base URL and require explicit consent
  before creating an absent GitHub environment. That consent names
  `Administration:write`. Adapter effect findings persist in migration 48 and
  project into current-generation per-name findings in target status.
- Pending-draft validity is evaluated against the current declaration and
  presence rules for the caller's own drafts. Secret bytes never enter the
  response. Project reads separately report policy-management and deletion
  capabilities; the settings page gates the corresponding sections.
- `/api/v1/meta` exposes the persisted instance identity used by the existing
  authenticated, certificate-pinned self-connection refusal. SCIM mapping
  capabilities carry explicit binding, mapping, and group origin metadata.

## Kubernetes design follow-up

The user explicitly moved Kubernetes condition reporting into
[#683](https://github.com/Hikyo-Org/Hikyo/issues/683). Conditions currently live
inside each cluster; server-side status requires a separately reviewed reporting
design. This change adds no reporting channel and makes no live-status claim.

## Superseded and excluded requirements

The shared-secret-default request contradicts the normative ripple register in
[flat-model.md](../adr/flat-model.md#ripple-register-normative): there is no
defaulting mechanism, and a schema `default` is forbidden. This change corrects
the stale UI-spec advisory rather than restoring inheritance or defaults.

Social sign-in remains explicitly outside this ticket, owned by #615.

## Verification

Sensitivity review: minting adds only effective expiry to its existing
display-once projection. Revision comparisons and per-key disclosures use
uncached sensitive mutations and component-owned state. Secret rows in an
ordinary diff are validated as masked at the client boundary. Blur,
visibility changes, session retirement, closing, and the 30-second reveal
deadline discard disclosed values; stale preflight and pending results cannot
restore them. Inventory pins cover the reviewed identities API, revision-diff
API, and history drawer changes.

Parent verification:

- Web: 895 tests in 104 files; web and client typechecks; 20 client tests.
- Browser: the full desktop run passed 226 cases, with four viewport skips,
  one stale assertion, and two serial dependents not run. The assertion expected
  a disabled Danger-zone form while permission was unknown; the implemented
  capability gate hides it. After correcting that pin, all 24 settings cases
  passed. The full mobile run passed 231 cases with two viewport skips.
- Browser cases use the real embedded instances and the existing trusted CI
  specs. The development provider replaces only the external provider; both
  finding cases drive the real journal and survive reload. Masked diff
  screenshots were visually checked on desktop and mobile; accessibility
  assertions passed. Tests cover publisher names, mint expiry, GHES consent,
  findings, per-key diff disclosure, invalid drafts, capability gates, SCIM
  origins, and persisted instance identity.
- Go: the full run passed every package except isolation. Isolation exposed
  missing diff route classifications and a full/page draft-projection mismatch;
  both were fixed, reviewed, and their focused checks passed on both engines.
  Authz, API, service, and conformance reruns pass. The full isolation rerun
  uses a 25-minute cumulative timeout after the default 10-minute timeout was
  insufficient; all isolation tests passed in 691.307 seconds. Every Go
  package is green across the full run and the affected-package reruns.
- Go vet, embedded-UI API/server tests, documentation verification, exact
  regeneration of SQLC/OpenAPI/client files, and PostgreSQL 18 development
  compatibility verification pass. Standards and Spec reviews are CLEAN after
  all fixes.

The source-owned development compatibility declaration must be generated from
the actual final migrations using PostgreSQL 18, matching CI. The pinned legacy
genesis declarations are historical evidence and must not change.
