# Handoff: CLI trust for proxied origins (branch `dunky13/cli-pin-rotation`)

## Result

- Decision locked in the
  [API/CLI ADR](../adr/api-cli-surface.md#proxied-origin-trust-decision):
  origins behind a re-keying proxy or CDN use the existing `tls_mode: "system-ca"`.
  Leaf SPKI pinning stays for operator-keyed origins. The verification contract
  is unchanged.
- The pin mismatch error in `internal/clienttrust`, which the CLI and the
  desktop app share, now names `system-ca` trust as the remedy. Covered by
  `TestTLSConfigPinAcceptsOnlyTheExactLeaf`.
- [Operations guide](../operations/cli-tls-trust.md#origins-behind-a-re-keying-proxy-or-cdn)
  gives the migration steps.

## Evidence

- Observed on 2026-11-23: hikyo.went.io and hikyo.dbugit.io both resolve to
  Cloudflare and serve Google Trust Services `WE1` wildcard leaves. Neither
  went.io nor dbugit.io has a CAA record. The hikyo.dbugit.io leaf expires 2027-01-15, so expect a re-key
  around mid-December 2026.
- `go test ./internal/clienttrust/ ./internal/cli/` passes.

## Remaining operator steps (outside this repository)

1. Add CAA records for went.io and dbugit.io that allow only the CAs Cloudflare
   issues from, per Cloudflare's current documentation.
2. PokéTracker: replace the bundle where it is provisioned (today the Forgejo
   Actions variable `HIKYO_TRUST_BUNDLE`, which its wrapper writes to a file)
   with
   `{"name":"hikyo-went","origin":"https://hikyo.went.io","tls_mode":"system-ca"}`.
   The operations guide expects the same protected channel as `HIKYO_TOKEN`;
   consider moving it to a secret.
   Its wrapper `scripts/hikyo-export-env.sh` currently refuses a bundle without
   `spki_pin` (lines 72-77, per review), so remove that check first, as
   DBugIT's repair below does. Confirm its pinned CLI release supports
   `tls_mode` (v1.0.0 and later do). Then re-run deploy-preview and update the "Trust bundle"
   section of `docs/handoffs/hikyo-migration.md`.
3. DBugIT (hikyo.dbugit.io): apply the
   [system-ca consumer repair](966-dbugit-system-ca-repair.md) and switch its
   bundle the same way before the December re-key.
