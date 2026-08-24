# Handoff: #461 add-remote client validation

Issue: https://github.com/Hikyo-Org/Hikyo/issues/461 (audit finding `P8-b`).

## Contract

- The add-remote form trims and canonicalizes the submitted URL, then performs
  a client pre-flight check for the server's bare HTTPS-origin grammar. The
  server remains the authoritative validator.
- Duplicate detection compares the submitted origin with the origins in the
  existing `remotesKey` query through `safeOriginOf`. A match names the existing
  remote and does not start the add mutation.
- The URL input exposes URL semantics through `type="url"`. Client validation
  still explicitly rejects HTTP because HTML URL validation accepts it.
- A server `409` retains duplicate-identity guidance for alternate URLs, stale
  cache data, and races. Exact duplicate origins get their own client refusal.

## Coverage

- `web/src/routes/Remotes.test.tsx` covers a whitespace-padded duplicate origin,
  bare-host and HTTP refusals, URL input type, and trimmed mutation payload.
- `web/src/api/remotes.ts` remains the sole owner of the shared query key,
  mutation, and safe stored-origin parsing.
