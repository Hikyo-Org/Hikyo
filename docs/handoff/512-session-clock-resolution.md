# Handoff: #512 reuse request authentication for session-clock sliding

Issue: https://github.com/Hikyo-Org/Hikyo/issues/512

## Outcome

`SlideSessionClocks` now consumes a narrow request-local activity hint recorded
by the in-transaction authentication chokepoint. A recent authenticated GET no
longer opens a second read transaction, and an unresolvable bearer on a public
route performs no datastore work.

The hint contains only the resolved last-seen time and whether the identity
owns the generic session or service-account touch path. It contains no
principal, proof, scope, raw bearer, or transaction token. A due write still
re-authenticates the raw bearer inside its own transaction, so revocation and
the database remain authoritative.

## Preserved boundaries

- The touch remains post-response.
- `SlideGranularity` is checked first against request-time state, then again
  against the fresh row inside the write transaction for multi-instance races.
- SCIM provisioning credentials and instance-connection credentials retain
  their dedicated touch paths; the generic path handles only sessions and
  service-account credentials.
- Public routes do not authenticate a presented bearer solely to slide it.

## Verification

- SQLite and PostgreSQL HTTP conformance cover one session resolution, zero
  fabricated-bearer queries, due-clock sliding, cross-instance suppression,
  and instance-connection path isolation.
- `go vet ./...` passed.
- `HIKYO_TEST_POSTGRES_DSN=... go test -p 1 -count=1 ./...` passed: 5,173 tests
  across 69 packages.
- Standards and issue-spec reviews returned `CLEAN` after valid findings were
  fixed and rechecked.
