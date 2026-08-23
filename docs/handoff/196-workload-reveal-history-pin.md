# #196 — workload reveal-history under a pin

## Outcome

Workload principals may now receive `reveal-history` at environment scope only
when both live conditions hold:

1. The project's machine-reveal opt-in is enabled.
2. That workload has an unexpired pin whose revision differs from the
   environment's latest snapshot revision.

Automation principals remain excluded because revision pins name workload
principals. No migration or new audit event was added.

## Refusal order

The shared grant writer applies the locked order:

1. Opt-in off → `ErrMachineRevealOptIn`.
2. Opt-in on without an active non-current pin →
   `ErrMachineRevealHistoryPin` (`conflict`, HTTP 409).
3. Existing machine scope and project confinement checks.
4. Existing widening formula: actor holds `manage-identities`, holds
   `reveal-history` over newly reachable environments, and presents a live
   mint reauthentication window.

The pin state read is one `hikyo:authn-resolution` query under the target
principal lock. It returns the pin revision, expiry, and latest revision; the
service evaluates liveness against its injected clock. Missing and malformed
state fails closed.

## Release behavior

`Pins.Release` does not delete the grant. Existing use-time mechanics make it
inert:

- delivery no longer selects a historical snapshot, so current secret
  plaintext requires `reveal`, not `reveal-history`;
- release advances the pin generation, moving the workload cursor once;
- disabling the project opt-in strips both disclosure atoms.

The public workload surface remains pin-bound: `delivery.fetch` admits machine
credentials; rollback, pin mutation, and values export remain human-session
routes. Revision detail is machine-readable metadata and contains no values.

## Verification

Paired SQLite/Postgres E2E coverage proves opt-in precedence, absent/current/
expired pin refusal, current-becoming-historical admission, disclosure-authority
and reauthentication gates, both audit rows, historical plaintext delivery,
release inertness, one cursor movement, retained grant row, and automation
refusal. `TestWorkloadRevealHistoryWireSurfaceStaysPinBound` pins the public
artifact boundary.
