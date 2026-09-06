# First signed nightly legacy bridges

Catalog sequence 2 authorizes one maintenance bridge per engine from the reviewed
`legacy/v1` schema through migration 44 into
[`v0.0.1-nightly.20260906.26.g52c8b012`](https://github.com/Hikyo-Org/Hikyo/releases/tag/v0.0.1-nightly.20260906.26.g52c8b012),
whose target migrations run through 49. This is an exact source-schema route,
not an assertion that an unsigned source binary was a signed release.

| Engine | Recovery-signed statement SHA-256 |
| --- | --- |
| SQLite | `afb19f55f6e8fc47f7946653b5c9ec558cb4d5215c52edf43653e59fd6920b63` |
| PostgreSQL | `e0a262775b4d9dfccbc81de2d97879b6102f00d7fc4f5a16a835f342d3d65705` |

Both statements pin target manifest
`e54e0bdb3b9298e234070d27ad75680ed0a6abf75c59aff3815a8c1c30e9d0ee`,
compatibility declaration
`61a0216151eb90101433824dd18c095e34ed910a7cc24733e45dc38343380671`,
commit `52c8b012f1fa45634572d7266c6ab7d3a9d5eed8` and the existing nightly policy.
The exact 43 source migration-file hashes for each engine were independently
compared with recovered nightly commit
`907e2f41fccc40bb8287025763b56e38c533eb37`. Source and target schema digests also
match the authenticated published compatibility declaration.

The [release run](https://github.com/Hikyo-Org/Hikyo/actions/runs/34053156703)
verified the complete OIDC-signed inventory and actual packaged production
startup/restart on both engines before immutable publication. The complete
22-asset download was independently verified afterward. This first signed
nightly had no authenticated signed predecessor, so the previous-signed-nightly
upgrade job had no source release to exercise.

The operator's encrypted local recovery key signed these exact statements and
the advancing catalog on 2026-09-06 under the approved online/local nightly
exception documented in [the bootstrap record](BOOTSTRAP.md). Private key
material and passwords remain outside Git and Actions. Stable metadata and the
authorized nightly policy remain unchanged.

The runtime bundle loader verified both signatures and catalog 1 to 2
advancement. Both engine plans require a single maintenance bridge and local
operator attestation. Verification refused missing bridges, different source
schemas and rollback to catalog 1. These checks establish project authorization
of the exact route; the installation must still inspect its own database,
complete its backup/drill and stop every legacy writer before upgrading.

Use the [signed-nightly assembly procedure](../../docs/operations/signed-nightlies.md)
and [manual verified upgrade procedure](../../docs/operations/manual-upgrades.md).
An older binary-only self-updater cannot supply the required runtime bundle and
installation evidence for this first hop.
