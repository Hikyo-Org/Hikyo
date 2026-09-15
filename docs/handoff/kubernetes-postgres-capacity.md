# PostgreSQL capacity through Kubernetes

User request: replace the PostgreSQL "Unmeasured" diagnostic with an authoritative
database-volume measurement where the deployment can provide one.

## Implementation

- Four explicit PostgreSQL bootstrap inputs identify an HTTPS kubelet origin,
  node, namespace and PVC. Shared configuration validation rejects partial or
  malformed mappings and SQLite use. Managed configuration preserves the mapping.
- The reader uses the projected ServiceAccount token and nonempty cluster CA.
  Direct `/stats/summary` requests have verified TLS, no redirects or ambient
  proxy, a three-second timeout and an 8 MiB response limit.
- Exact node/namespace/PVC samples must have valid capacity fields, be no older
  than two minutes and no more than five seconds in the future. Conflicting or
  missing samples and runtime failures remain unknown. No local-disk fallback.
- Helm grants only `get nodes/stats` for the configured node. The server gets
  the token; helper containers do not. Enrolled rollout deployments reuse their
  existing server account and token. Default deployments gain no permissions.
- Network measurement occurs after successful health-read authorization and
  audit commit, outside the database transaction. Existing thresholds remain
  80% warning and 90% critical; runtime measurement does not affect readiness.

The mapping is an operator assertion, not discovery from the database DSN.
Update it with database moves. Kubelet permission exposes that node's statistics;
the reader filters the response to the selected PVC. The reading describes the
backing filesystem, not the claim's requested size.

## Live investigation

On 2026-09-14, `picluster` namespace `hikyo-dbugit` had PostgreSQL pod
`hikyo-dbugit-db-0` on `tokyo`, using PVC `data-hikyo-dbugit-db-0` and storage
class `local-path`. Both `df` inside PostgreSQL and kubelet statistics confirmed
roughly 251 GiB total and 210 GiB available on the backing filesystem. The 10 GiB
claim was not a dedicated filesystem quota. Direct kubelet TLS validated with
the namespace's cluster CA. No production configuration was changed.

Deployment needs a release containing this code and the four explicit Helm
`database.storageMonitoring` inputs documented in
[self-hosting](../site/src/content/docs/docs/self-hosting.mdx#postgresql-capacity-on-kubernetes).
Ordinary standards and spec reviews passed after fixes. Cross-provider review
was explicitly skipped by the user.

## Verification

All 273 storage/config tests pass. The focused diagnostics and transaction
regression pass against both SQLite and disposable PostgreSQL 18. Complete chart
checks, enrolled rollout compatibility, and chart mutation tests pass. Go vet
passes for affected production packages; ShellCheck and diff checks pass.
Documentation check passes with zero errors, warnings or hints across 38 files.
`go test -p 1 ./...` passes on the final files, including the complete app and
isolation suites. Race tests pass for storagehealth and config. Earlier runs
exposed build contention and a cache-invariant false positive in a test name;
the final serial run passed after renaming the test without changing assertions.
