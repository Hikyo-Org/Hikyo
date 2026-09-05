# Atomic fresh hierarchy initialization

Owned files in `/tmp/hikyo-upgrade-ledger`:

- `internal/store/upgrade/candidate_initialize.go`
- `internal/store/upgrade/candidate_initialize_test.go`
- `internal/crypto/initialize_fresh.go`
- `docs/reports/1.0/fresh-hierarchy.html`
- this handoff

`Session.InitializeFreshHierarchy(ctx, expected, root) error` consumes root. Only generation1, Fresh source/route, hop0, SchemaApplied, maintenance, noninvalidated, epoch0 may enter. Inside the owned SQL transaction it resumes the exact operation, verifies actual target catalog, reconciled instance identity, credential1/restore0, and empty orgs/principals/master/tier3. A private KeyStore adapter inserts canonical generated key queries and all scope generations inside the same transaction. It refuses every unrelated operation; no runtime write capability escapes.

The narrow `crypto.InitializeFreshHierarchy` uses maintained `LoadKeyring`, then zeros the successful temporary master/instance/token/scanning material. Root is consumed by both boundaries. Existing LoadKeyring failure behavior is preserved; new adapter insert errors use its existing mint rollback zeroing. There is no new general concurrent runtime Destroy API.

F5 call order: inspect CandidateKeys; if first Fresh and completely absent hierarchy, initialize; then independently prove existing hierarchy using root escrow; only then publish Healthy. If initialization committed but the process crashed before Healthy, a retry must prove the existing hierarchy rather than invoke initialization again. The second initialization call deliberately refuses.

Validation with dedicated actual PostgreSQL databases plus SQLite:

- `go test -race -p 1 -run 'TestFreshHierarchy|TestCandidateKey' ./internal/store/upgrade`: PASS22.975s, all engines configured.
- `go test -count=1 -p 1 ./internal/crypto ./internal/boundary ./internal/lint`: PASS0.405s/7.449s/33.488s.
- `go vet ./internal/store/upgrade ./internal/crypto`: PASS.

Tests force a real mid-insert scope uniqueness failure and a precommit failure, prove all key rows/scope inserts rolled back, then verify actual wrappers open under the correct root. Invalid root, root zeroing, wrong root, populated org/principal, legacy, second-call and Healthy refusal are covered. No commit or push. Parent owns independent review, exact candidate CI and signed delivery.
