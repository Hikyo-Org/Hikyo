# Issue #339: central trust-metadata validation

Issue: https://github.com/Hikyo-Org/Hikyo/issues/339. Fixed point before this
work: `052cc9b23ec325473eae48753102ad04a04b506a`.

## Contract

`validate_trust_metadata` in `scripts/lib/release.sh` is the single owner of
trust-metadata v1 validity. It validates the schema, trust sequence, closed
event vocabulary, release and pending-release state, highest-release binding,
primary-key records, and uniqueness of release versions, release sequences,
key IDs, and key paths.

Release candidate resolution and signed-bundle verification call the same
validator. `verify-bundle.sh` retains only checks that bind otherwise-valid
metadata to the pinned recovery root and bootstrap primary key. Candidate
selection retains its release-specific interval, pending, and revocation
checks.

Duplicate release versions and sequences remain distinct failures. Structural
validation runs first, duplicate classification runs before highest-release
state validation, and the existing `release version is duplicated`, `release
sequence is duplicated`, and `invalid trust metadata` strings remain stable.

## Generated output

None. No generated source, client, schema, or release artifact changed.

## Validation

- Resolver fixtures cover wrong schema, unknown event type, an absent
  `highest_release`, duplicate current-highest metadata, and existing candidate
  selection/refusal behavior.
- Manifest binding fixtures use complete trust-metadata v1 input.
- Full signed release trust/refusal fixture passed with checksum-matched cosign
  v3.1.3.
- Complete local `supply-chain-checks` command set passed.
- ShellCheck and `git diff --check` passed.
