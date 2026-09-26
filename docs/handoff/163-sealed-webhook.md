# Handoff: #163 Sealed webhook synchronization protocol and adapter

Issue: https://github.com/Hikyo-Org/Hikyo/issues/163 (blocked by #157, merged).
Normative contract: [docs/spec/sealed-webhook.md](../spec/sealed-webhook.md).
ADR gate: declared amendment banner in
[deployment-adapter.md](../adr/deployment-adapter.md) and declared amendment 5
in [mvp-boundary.md](../adr/mvp-boundary.md). This handoff describes the
branch; it does not claim a merge or a live third-party receiver pass.

## Implemented

- **Crypto (`internal/crypto/sealedhook`)**: the only caller of age and
  Ed25519 for this protocol. Envelope seal (age X25519 to the pinned
  recipient, Ed25519 over a length-prefixed injective encoding), receiver open
  (signature before decryption, validity window, pinned sender keys with
  `not_after` overlap), acknowledgement sign/verify bound to the envelope
  digest and idempotency key, fingerprint, destination id, idempotency key.
  Strict JSON (unknown members refused), closed reason syntax so an ack can
  never echo text. Frozen KAT vector; `FuzzParseEnvelope`, `FuzzParseAck`.
  The boundary test's age allowlist gains this package.
- **Adapter (`internal/adapter/sealedwebhook`)**: hand-rolled `net/http`
  client with a closed `API` of exactly `Deliver`, one registry route
  (`POST /hikyo/v1/sync`), the shared public-egress dialer (re-resolved and
  vetted per dial), no proxy, no redirects, TLS >= 1.2, per-endpoint timeout
  (<= 10 s) and response cap (<= 64 KiB). The module implements the four seam
  operations: `TestConnection` sends a signed probe; `Plan` is network-free
  and plans unclaimed names as unknown-until-sync; `Sync` seals each effect
  before Prepare and maps outcomes per the spec's table (forged ack =
  terminal `adapter.ErrAckForged`, missing ack = `unknown` retried under the
  same key, `conflict` = `ErrConflict`, `rejected/unauthorized` =
  `ErrProviderAuth`).
- **Seam changes (`internal/adapter`)**: `SealedWebhookProvider` registered;
  `ValidateSealedWebhookManifest`; `SyncRequest.Source` (org, project,
  environment, revision) filled by the loader; `ErrAckForged` classifies as
  `provider_ambiguous` (needs attention) and is terminal in the outbox.
- **Instance-admin registry**: `HIKYO_SEALED_WEBHOOK_FILE` (strict JSON,
  signing key file must be `0600`), validated at boot by
  `activateSealedWebhookEndpoints`; any unconfirmed or mismatched fingerprint
  refuses to serve. The module factory builds a sealed-webhook module only for
  an activated origin; the tenant credential is the receiver binding and
  travels only inside the ciphertext.
- **Schema**: migration `00060_sealed_webhook_adapter.sql` on both engines
  widens `adapters.provider`; SQLite rebuilds `adapters` and recreates the
  `adapters_active_origin` partial index. `internal/buildcompat/development.json`
  regenerated against PostgreSQL 18 (diff: one entry and `schema_sha256` per
  engine). The upgrade drill reverses 00060.
- **Surfaces**: OpenAPI `AdapterProvider` extensible enum, CLI `--provider
  sealed-webhook` (organization kind, namespace as `--owner`), web display
  label, site docs, configuration reference, variable inventory report.
- **Reference receiver**: `internal/sealedreceiver` plus
  `cmd/hikyo-receiver-example` (`keygen`, `fingerprint`, `serve`). Not in
  `.goreleaser.yaml` (only `cmd/hikyo` is built).

## Acceptance criteria evidence

| Criterion | Evidence |
|---|---|
| Protocol fields, no values in metadata | spec sections 4-5; `TestSealOpenRoundTripKeepsPlaintextInsideCiphertext`, KAT |
| Exact origin, pinned keys, bounds, fingerprint confirmation | `TestEndpointActivationRequiresConfirmedFingerprintAndBounds`, `TestSealedWebhookActivationRefusesUnconfirmedTrustBoundary`, config tests |
| Plaintext only in ciphertext | `assertNoPlaintext` over request URLs, headers, bodies, ack bodies, completions, results, errors (`TestEndToEndConvergeUpdatePruneAndTeardown`, `TestCrashWindowReplayReusesIdempotencyKey`) |
| Rebinding, redirects, proxy, private/metadata, host/key change, oversized, invalid ack, replay, expiry | `TestEgressRefusesPrivateLinkLocalAndMetadataDestinations`, `TestAmbiguousResponsesStayUnknown`, `TestClientTransportPolicy`, `TestForgedAcknowledgementsAreTerminal`, `TestReceiverRefusesReplayedExpiredAndUnpinnedEnvelopes`, `TestKeyOverlapRotationAndTrustBoundaryChange` |
| At-least-once, cursor, ambiguity visible | `TestCrashWindowReplayReusesIdempotencyKey` (unknown, then `already_applied` advances to owned) |
| #157 lifecycle, rotation, takeover | teardown sentinels-last in the e2e; `TestStaleWorkerCannotDispatchAfterLosingAuthority`; overlap rotation test; pause/resume/retain-prune reuse #157 controls unchanged |
| Reference receiver + adversarial e2e | all of the above run against `sealedreceiver` over real TLS |

Mutation checks during development: disabling ack signature verification
fails `TestForgedAcknowledgementsAreTerminal/tampered_status`; allowing
redirects fails the redirect case and the transport policy test.

## Decisions that differ from the handoff comment

1. **Endpoint custody is an operator file, not an `instance-config@none` API
   and CLI.** The registry lives on the server like the adapter egress policy;
   the admin's typed fingerprint is `confirmed_fingerprint`. This keeps the
   whole trust boundary outside anything a request can mutate and avoids new
   routes. An API/UI for instance admins is a follow-up.
2. **Signing key custody is a `0600` PEM file, not sealed under
   `InstanceSealer` with SAML-style rows.** Overlap rotation is receiver-side
   (`not_after` pins); Hikyo always signs with the configured key and the
   `key_id` in every envelope names it.
3. **No new `adapter.ack_rejected` security event or effect finding.**
   Adding a finding value needs an `adapter_effects` SQLite rebuild. A forged
   ack is instead a terminal job failure whose error class needs attention,
   with the OUTCOME audit recording the failure.
4. **One envelope per effect; the sentinel is an ordinary `upsert`.** This
   maps one-to-one onto the Journal's per-name Prepare/Finish fence.
5. **The e2e lives in `internal/adapter/sealedwebhook`**, not an env-gated
   `internal/isolation` harness: the reference receiver is in-tree, so the
   test needs no external service and always runs.

## Not done / follow-ups

- Web create form does not offer `sealed-webhook` yet (display only).
- Instance-admin API/UI for the registry; hot reload (boot-only today).
- A dedicated security audit event for forged acknowledgements.

## Verify

```sh
go test ./internal/crypto/sealedhook/ ./internal/adapter/... ./internal/sealedreceiver/ \
  ./cmd/hikyo-receiver-example/ ./internal/app/ ./internal/config/ ./internal/boundary/
HIKYO_TEST_POSTGRES_DSN=postgres://... go test ./internal/store/...
go test -run '^$' -fuzz FuzzParseEnvelope -fuzztime 30s ./internal/crypto/sealedhook/
```
