**OBJECTIONS**

R2 verification:

1. Legacy retirement: **fixed**. Apply is explicitly disabled across admission/helper paths before proposal becomes operative (proposal:21 (historical `docs/research/signed-upgrade-compatibility.md:21`, file no longer present)).

2. Shared/exclusive upgrade fence: **fixed**. Transaction-held shared row locks, exclusive maintenance activation, generation increment, stale-process refusal, and crash/partition tests are specified (proposal:145 (historical `docs/research/signed-upgrade-compatibility.md:145`, file no longer present)).

3. Receipt custody and operator attestation: **fixed**. Receipt identity, ciphertext verification, scratch restore drill, external attestation-key custody, expiry, and atomic nonce consumption close R1’s proof gap (proposal:162 (historical `docs/research/signed-upgrade-compatibility.md:162`, file no longer present)).

4. Keyless/recovery trust: **partial, two blockers remain**.

   - Recovery bridge authority conflicts with the general rule that every edge requires target-manifest authorization (proposal:51 (historical `docs/research/signed-upgrade-compatibility.md:51`, file no longer present), proposal:78 (historical `docs/research/signed-upgrade-compatibility.md:78`, file no longer present)). A post-compromise bridge cannot authorize an otherwise absent edge under both rules. Recommendation: define recovery-root bridge as the exceptional edge authority, while still requiring independent normal authentication of the exact target artifact and migration identity.

   - `nightly/v1` binds “binary/image digests” but lacks closed asset-set semantics (proposal:62 (historical `docs/research/signed-upgrade-compatibility.md:62`, file no longer present)). Nightlies currently publish archives, native packages, checksums, provenance, and metadata ([nightly.yml:220](../../../.github/workflows/nightly.yml) (historical line 220)). Recommendation: signed schema must enumerate exact version, tag, source commit, every published asset digest, compatibility/provenance/checksum digests, and OCI identities when present; missing, extra, or substituted assets refuse.

5. Flux authority: **partial, blocking**. Dedicated repository plus fixed object names constrains object selection, not object contents (proposal:212 (historical `docs/research/signed-upgrade-compatibility.md:212`, file no longer present)). Compromised Git credentials can still alter allowed Deployment fields such as service account, secret mounts, commands, init containers, or security context. Signed-image enforcement does not prevent those mutations. Recommendation: require server-side/admission enforcement of the exact permitted patch shape for every repository layout, limited to approved image digest and controller-owned generation fields. Deny all other pod-spec/RBAC mutations.

Legacy retirement remains correctly treated as an independent prerequisite, not evidence against the proposed foundation.

**OBJECTIONS**

