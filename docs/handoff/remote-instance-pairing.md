# Manual remote pairing and QR import

The requester prepares a chosen password or generated phrase on their own
instance and shares only that secret outside Hikyo. The serving administrator
manually creates a finite, single-use invitation. The requester pastes it,
scans its QR code, or imports a PNG/JPEG QR image, reviews the destination, and
chooses **Connect remote**. Preparation and QR decoding do not contact the
serving instance. Existing manual remote creation remains available.

The current deployment is bundled Hikyo, including SQLite on a MacBook.
Dedicated desktop and mobile clients are future work. Their reusable boundary
is the versioned invitation and outbound pairing protocol; the current sealed
requester continuation is an instance-local custody format.

## Security and authority

- Invitation administration requires `instance-config`; anonymous callers can
  only redeem an existing random capability. There is no public request-access
  creation endpoint or approval queue.
- Secrets may be chosen freely, including four or five characters. Exact UTF-8
  bytes, case, and spaces matter. Generated phrases allow 1 through 128 words.
  The independent random Ed25519 signing key is never derived from the secret.
- The configured public HTTPS origin and verified serving certificate supply
  the connection details. A one-time owner pin override supports exceptional
  reverse-proxy deployments. An administrator-triggered bounded TLS self-probe
  discovers identity without sending HTTP or pairing secrets.
- Proton's SRP library supplies the password exchange. The context binds the
  invitation, serving identity, and requester key. Directory credentials require
  RFC 9449 DPoP proofs; ordinary bearer authentication rejects paired bindings.
- Database-backed expiry, attempts, consumption, epochs, and replay admission
  preserve the checks across processes and restore. Replay census is bounded
  per enrolled key and globally, without evicting still-live proofs.
- A pair grants directory metadata authority. Workspace access still requires
  the human's own remote sign-in and existing origin consent.

QR codes contain the invitation, never the phrase or private key. The bundled
decoder reads bounded local image pixels without uploads, external scripts,
or URL navigation. Camera access is user-triggered, same-origin, and video-only.
Cancellation, hidden pages, unmount, invitation edits, expiry, and session
retirement fence pending work and release current or late-arriving tracks.
Pasted, scanned, and imported invitations use the same destination review and
explicit connection action.

## Implementation map

- [Design and cryptographic profile](../adr/remote-instance-pairing.md).
- [Browser, CLI, configuration, and recovery guide](../operations/remote-instance-pairing.md).
- `internal/pairing/`: SRP adapter, signing keys, and DPoP validation.
- `internal/service/remote_pairing.go`: manual invitation and enrollment flow.
- `internal/remotefetch/pairing*.go`: pinned outbound exchange and directory proofs.
- `internal/app/pairing_identity.go`: configured public identity resolution.
- `internal/store/queries/*/remote_pairing.sql`: durable admission and lifecycle.
- `internal/runtimeconfig/migrations.go`: optional pin declaration on upgrade.
- `web/src/routes/RemotePairing.tsx`: sensitive input custody and confirmation.
- `web/src/routes/PairingQrImport.tsx` and `pairingQr.ts`: camera/image lifecycle
  and local decoding.
- `web/e2e/flows/workspace.spec.ts`: actual two-instance pairing with synthetic
  camera frames, displayed QR-image import, and independent workspace regression.

## Validation and delivery boundaries

The local web run passed 2,013 tests, TypeScript, lint, design checks, all 32
affected Storybook examples in each palette, and the full Storybook build and
Docs guard. The guard indexed 857 stories; this does not mean all 857 were
browser-executed. Source review found and corrected replay-budget starvation
and a shared form-primitive bypass. The final Standards and Spec passes found
no remaining actionable source findings; they are not independent crypto audits.
The replay regression reproduced cross-peer denial before the fix and passes
after the per-key admission bound. Local browser and database validation, exact
PR-head CI, review threads, protected queue admission, and actual merge remain
separate evidence; use the pull request's current checks for delivery state.

The final local two-engine pairing/remote/audit run, backend analyzers, affected
API/server/CLI/configuration/crypto packages, configuration/store upgrades,
selected application recovery checks, and pairing/outbound race checks passed.
The TypeScript client passed type checking and all 41 tests.

Desktop and Pixel 5 mobile each passed the real two-instance flow: scan synthetic
camera frames through the browser media API, import the administrator's displayed
QR image, review the destination, and explicitly redeem over pinned HTTPS with
a five-character secret. Neither scan nor image import automatically redeemed
the invitation. The workspace popup and both kill switches passed separately.
The displayed-image test first failed at the compact setup-code size; the
invitation now uses a responsive 320px design token, retaining the 176px setup
default and square containment in a 240px host.

CodeQL alert 188 identified the intended user-selected origin. A separate source
review confirmed human `instance-config` and creator-bound continuation checks
before outbound work, the existing private/LAN remote policy, and the shared
pinned transport. The alert was dismissed as a false positive with the rationale
recorded in PR 1014's review thread. No URL sanitizer or suppression was added.

Camera automation uses Chromium's real media API with synthetic QR frames.
It does not establish physical camera behavior on every browser or device.
The entire isolation suite previously reproduced a PostgreSQL Unicode/fullwidth
adapter-origin failure on the unchanged starting commit; selected pairing and
security suites are recorded separately from that baseline failure.

The owner approved the
[PR #1014-only review exception](https://github.com/Hikyo-Org/Hikyo/pull/1014#issuecomment-6300666440)
on 2026-10-20, accepting completed GPT 6.1 Sol High Standards and Spec/security
source reviews instead of the unavailable cross-provider ADR review. That pass
remains skipped, not clean, and the source reviews are not an independent
cryptographic audit. Tickets #8 and #71 were reopened for the amendment record.
The pairing, multi-instance, and threat-model ADRs record the locked owner
decision, operative when PR #1014 merges. General governance and future review
requirements remain in force; CI and the protected merge queue still gate merge.

Before this documentation-only decision record, head `9e3cdad0cbf45` passed all
four CodeQL language analyses and the aggregate CodeQL check. Alert 188 remained
dismissed with the source/test rationale above, and its review thread remained
resolved. Queue admission passed; admission alone is not candidate validation or
proof of merge. CodeRabbit skipped review because of its file/usage limits, and
Qodo skipped review because its trial ended; neither is reported as clean.

## Protected candidate integration repair

The independent diagnosis and acceptance criteria are recorded in
[#1015](https://github.com/Hikyo-Org/Hikyo/issues/1015).

The first full protected candidate, `12bf5013b14d1611418a05f66811ee300080038c`
in [run 39492849436](https://github.com/Hikyo-Org/Hikyo/actions/runs/39492849436),
exposed three integration omissions. Its failed core and race jobs shared the
same missing budget classifications; the isolation job found a stale epoch
coverage guard; and the Storybook job's later static-demo step found missing
invitation metadata. The aggregate validation and proof jobs also failed.
The 857 Storybook examples themselves passed in each palette, separately from
the failing demo checks. The candidate did not merge.

The repair explicitly classifies all five administrative pairing operations in
the budget ledger, consistent with existing bounded remote configuration.
Authenticated API admission, transport and resource limits, and public exchange
attempt controls remain enforced. The restore guard now reads pairing's actual
separately owned census query alongside the three existing queries, in both SQL
dialects, while preserving schema-derived coverage. Runtime already includes
pairing epochs; the frozen legacy query must remain compatible with archives
restored before migration roll-forward. No runtime restore query was changed.

The demo transport now supplies an empty GET-only invitation list with a
fictional HTTPS origin. A regression first failed with four missing-fixture
404s, then passed all 16 focused API tests. It checks generated-schema validity,
all three demo roles, refusal of preparation, creation, revocation and redemption,
and unchanged metadata after those refused mutations. The alert assertion and
production transport were retained. Independent GPT 6.1 Sol High reviews of the
combined repair found no actionable source issues; these are source reviews,
separate from execution evidence and the PR-only review exception above.

Local repair validation passed: the budget completeness regression normally and
with the race detector; the full service package; authenticated API wire refusal;
and the pairing/restore cases on SQLite and PostgreSQL 18, including future epoch
stamps, historical absent tables, the schema completeness guard, implausible
epochs, and forged archive epochs. Web validation passed all 2,016 unit tests,
type checking, lint, design checks, all 82 desktop/mobile static-demo browser
tests, the demo CSP build, and the production build's demo-isolation check.
These results validate the repair locally; a fresh protected candidate still
must pass before merge.

An isolated execution of the compiled schema guard additionally rejected all
four controlled omissions: either pairing table removed from either dialect's
census query copy. The untouched query copies passed before and after those
mutations. The canonical queries were never changed. This establishes that the
expanded guard still detects genuine omissions, alongside the runtime restore
checks above. The AST-only graph refresh also completed; no semantic review or
independent audit is implied by that refresh.
