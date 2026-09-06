# Self-hosted compliance support

User-authorized scope, 2026-09-05: implement privacy controls and documentation
for self-hosted Hikyo, preserving a future hosted-service path. This work supports
operator compliance; it makes no SOC 2 attestation or GDPR certification claim.

## Acceptance

1. Audit retention is bounded, policy-managed and transactionally audited on both
   database engines, without weakening append-only or export authority invariants.
2. Supported personal-data operator workflows provide appropriately scoped access,
   correction, restriction and erasure. Credential material and unrelated tenant secrets must
   not enter exports. Deletion after restoration has an explicit supported procedure.
3. Existing doctor JSON can produce a timestamped evidence artifact with server
   identity/version and honest unassessed boundaries. Existing doctor exit semantics
   remain intact and evidence contains no session credentials or configuration dump.
4. Public docs cover actual inventory, controls, responsibilities, retention,
   requests, incident response, recovery and evidence. They are linked from docs
   navigation, the docs landing page and relevant self-hosting/recovery guides.
5. Implementation and documentation agree. Relevant tests, typechecking, static
   docs build and review pass. Publication requires the repository PR/merge path;
   local output is not reported as deployed content.

## Boundaries

Organizational legal decisions, external storage custody, downstream processors,
customer-specific secret contents and independent auditing remain operator work.
The implementation must name residual limits instead of silently treating those
controls as verified. No arbitrary production data or retention is changed by
this development task.
