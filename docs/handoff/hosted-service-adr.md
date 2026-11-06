# Handoff: hosted-service ADR

ADR: [`docs/adr/hosted-service.md`](../adr/hosted-service.md), status locked on 2026-11-06.
This PR changes documentation only. It writes no implementation.

## Result

The owner chose a shared PostgreSQL deployment over one instance per customer,
Sealed orgs for the hosted service, server-side decryption and rolling upgrades.
The owner then required adapter sync and Kubernetes workload identity on the hosted
service, which added decision 11. The owner also asked for a new ADR that supersedes the old ones. The ADR records
those decisions, a normative supersession register, and banners on each affected
ADR. Banners say each superseded section stays in force until the matching implementation PR merges.

## Evidence

- Code and ADR reads pinned to the ADR's citations.
- `pgbench` against the real v1.0.0 schema and triggers: 2 CPUs in Docker, with a
  simulated 1 ms commit. It shows shape only. Method: migrate an empty PostgreSQL
  18 database with Hikyo 1.0.0, then have each transaction run
  `SELECT 1 FROM upgrade_control c FOR SHARE OF c` and insert one
  `audit_tenant_events` row with a random org out of 1,000, at 1, 4, 16 and 64
  clients. Isolation and retries were set in `pgbench`, and lock variants by
  replacing `assign_audit_tenant_commit_seq()`. Commit latency used
  `commit_delay=1000` and `commit_siblings=0`.
- Two live HA nodes were not achieved in `--dev`, so cross-node behavior is
  unverified.
- Independent adversarial reviews on `claude-opus-5-5`. See the ADR's review record.

## Owner decisions on 2026-11-06

- Resolution 9: unattended access per environment (decision 11), shipped after launch as phase 2.
- Decision 2's accepted trust covers capture from production.
- Decision 12 added: presented keys encrypted to the installation. Reviewed in rounds 7 and 8.
- The owner waived ticket reopening and cross-provider review, and the ADR records that.

## Remaining before any paid launch

The ADR's acceptance gates, including an independent cryptographic review and a
real multi-node load test. A Sealed protocol specification must be written and
reviewed before implementation. Decision 12 is filed as #1240. Other implementation tickets are not yet filed.

## Next steps

Start with the throughput prerequisites in decision 10, because a real
multi-node load test is the first check on whether the topology holds. Then
build tenancy and Sealed orgs, once design partners have seen the trade-offs.
