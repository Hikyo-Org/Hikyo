# MCP conformance dependency integration (#1089)

The original Dependabot head
`de7390e739cf7e95273d4db6b899d6f65e5cb4aa` updates only the conformance
tool lockfile: both Inspector and the SDK resolve `@hono/node-server` 2.1.5
instead of 2.1.1, retaining Hono 4.13.13. The package integrity and peer range
follow the published version. Its Node requirement remains below the repository's
pinned Node 26. The change is tooling, not a production server dependency.

The update includes the fix described by
[GHSA-rmxm-3fg6-px4f](https://github.com/honojs/node-server/security/advisories/GHSA-rmxm-3fg6-px4f).
Main `2b98bcf3565864d0858f2d40cc9ba4488a895790` merged cleanly. That merge
tree equals the hosted candidate
`d70149b438c5bc4224dfd951d2e74b634a9ef94a`, whose locked tool install and
MCP 2026-07-28 conformance steps passed in run `40789870781`.

## Commit-receipt fixture repair

That full run failed the unrelated shared-node refusal test: PostgreSQL retained
38 updates while callers received 37 successful acknowledgements. The production
owner explicitly reports failed commit acknowledgements as unconfirmed accounting.
A failure cannot establish that a commit did not happen.

A bounded test-only PostgreSQL proxy now drops one acknowledgement after receiving
the server's `CommandComplete COMMIT`. The original equality assertion reproduced
the defect deterministically: one durable update, zero acknowledged updates.
The proxy forwards other bytes unchanged, retains no message values, closes its
connections and joins every worker at teardown.

The shared-node test still requires progress under 64 concurrent attempts, exact
failure-health accounting, a validated window and at most 32 samples. It checks
that every acknowledged update is durable and no more than one update per attempt
can commit. Sealing and restart recovery must preserve the exact observed durable
count, including an update whose acknowledgement was lost. The controlled fault
also requires failure health, exactly one durable update and retained attribution.
Production behavior, the 100 ms deadline, writer/waiter bounds, sampling and CI
gates are unchanged. No uncertainty is treated as a confirmed success.

The durable baseline itself acquires PostgreSQL's singleton writer lock, joining
any in-flight commit, and reads staging plus sealed counts in one transaction.
Both review axes found that a plain SELECT could otherwise race a still-settling
commit. A controlled held writer reproduced the stale snapshot before repair and
now requires the baseline to wait for settlement and include the committed update.

The original dependency commit remains intact and GitHub Verified. The canonical
DCO checker permits its existing bot exception only after checking the PR's
non-spoofable Dependabot actor; all new human commits remain signed and DCO
signed-off. Scoped normal/race tests and final feedback are recorded on the PR.
Fresh full current-base queue validation remains mandatory. No rendered UI,
release, deployment, provider acceptance or production data mutation is involved.
