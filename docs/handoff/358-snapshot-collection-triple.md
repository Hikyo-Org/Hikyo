# Handoff: #358 snapshot collection triple

Issue: https://github.com/Hikyo-Org/Hikyo/issues/358 (parent #326; audit finding
`F-S03-1`). Implementation base:
`d88cd224bdf18f7949519239164a7b2cfc4af01c`.

## Contract

- `store.Snapshot.Collected` owns collection time and policy together; `nil`
  means the payload remains live.
- SQLite and PostgreSQL mappers refuse every disagreement among
  `payload_present`, `collected_at`, and `collected_policy`.
- `PayloadPresent` and `CollectionPolicy` centralize collection-state reads for
  store and service consumers.
- API, audit, database, migration, ordering, and generated wire shapes remain
  unchanged.

## Coverage

- `TestRevisionSnapshotMappersEnforceCollectionTriple` covers live, collected,
  and four contradictory triples for both generated database row shapes.
- `revision_list_detail_collection_state_agrees` covers public service parity
  with real storage and keyring behavior on both conformance engines.
- Retention GC, server revision tests, and focused store/service packages pass.
- Final local validation passed 3,559 tests across 61 packages with
  `go test -count=1 ./...`; standards and spec review both returned clean.
