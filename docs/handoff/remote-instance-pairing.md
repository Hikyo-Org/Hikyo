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

The local web run passed 2,013 tests, TypeScript, lint, design checks, all 29
affected Storybook examples in each palette, and the full Storybook build and
Docs guard. The guard indexed 856 stories; this does not mean all 856 were
browser-executed. Source review found and corrected replay-budget starvation
and a shared form-primitive bypass. The final Standards and Spec passes found
no remaining actionable source findings; they are not independent crypto audits.
The replay regression reproduced cross-peer denial before the fix and passes
after the per-key admission bound. Local browser and database validation, exact
PR-head CI, review threads, protected queue admission, and actual merge remain
separate evidence; use the pull request's current checks for delivery state.

Camera automation uses Chromium's real media API with synthetic QR frames.
It does not establish physical camera behavior on every browser or device.
The entire isolation suite previously reproduced a PostgreSQL Unicode/fullwidth
adapter-origin failure on the unchanged starting commit; selected pairing and
security suites are recorded separately from that baseline failure.

The pairing ADR records a proposed amendment to the locked outbound contract.
Cross-provider review is unavailable under the current reviewer policy. The
proposal is not a locked amendment or an independent cryptographic audit;
the locked-ADR amendment procedure still requires completion or an explicit
recorded maintainer decision before merge.
