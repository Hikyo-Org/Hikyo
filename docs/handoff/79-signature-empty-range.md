# #79: Empty-range signature verification repair

An ordinary fast-forward of the existing qualification branch to integrated
canonical main failed in the installed pre-push hook with
`Signature check: empty commit range`. The comparison had no new commits because
canonical main already equaled the requested head. Owner decision B requires a
reviewed source repair and protected integration before selecting a replacement
qualification candidate. No hook exception is authorized.

The checker now resolves both input commits before finding their merge base.
For an empty range it verifies the requested tip's cryptographic signature using
the existing locally trusted `G` requirement. Nonempty ranges retain verification
of every commit, including merge commits. Missing, noncommit and disjoint inputs
still fail. The installed hook and canonical comparison reference are unchanged.
The canonical DCO checker and its provenance requirement for bot exemptions are
unchanged.

## Validation

Integration base: `218513bb72586931511fc712e5e74d64dbaf20da`, including #1100.
The extended existing signature fixture fails against the old checker at the
empty-range regression. With the repair it exercises:

- A real ordinary branch fast-forward through the installed hook to an isolated
  local bare remote, with canonical main already equal to the signed head.
- Trusted signed nonempty, equal-tip and ancestor cases.
- Missing and noncommit inputs, missing canonical main, and disjoint histories.
- Unsigned commits and merges, an unsigned empty-range tip, a tampered signature,
  and a valid signature without local trust, through the checker and hook.
- The existing fork-base regression, which prevents an unsigned ancestor from
  being hidden behind fork main.

Run from the repository root:

```sh
scripts/ci/check-commit-signatures_test.sh
FAST_FEEDBACK_BASE=218513bb72586931511fc712e5e74d64dbaf20da \
  FAST_FEEDBACK_HEAD=HEAD scripts/ci/fast-feedback.sh --working-tree --plan
FAST_FEEDBACK_BASE=218513bb72586931511fc712e5e74d64dbaf20da \
  FAST_FEEDBACK_HEAD=HEAD scripts/ci/fast-feedback.sh --working-tree
scripts/release/stable-ceremony_test.sh
```

The fixture removes its temporary repository, signing key and bare remote on
exit. The ceremony fixture is an isolated consumer test with mocked publication;
it does not invoke the real ceremony, signing authority or publication. Hosted
feedback, independent Standards/Spec review and protected merge proof must remain
bound to their actual reviewed head. Automated security review does not establish
independent human review.

## Qualification custody

The earlier candidate remains
`9b6f60cf8ad6a843e18350d34e9fb63b22401be4`, tree
`aecdf887d4781cb1184b7fdb54d5e59189fc2b1e`.
[Full-main run 40978837124, attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/40978837124)
and its retained artifact bytes belong only to that source. The failed branch
push and all partial results remain evidence. They cannot qualify the repair or
its later integrated source.

After actual protected integration, verify the resulting main SHA/tree and its
parents, select one clean replacement candidate, and fast-forward the existing
qualification branch normally through the repaired hook. Re-read the remote
before mutation and retain its SHA/tree readback. Run fresh full-main and then
the mapped specialists sequentially on that pinned branch. Check each dispatch
ref and resulting run head; retain run attempts and artifact digests separately
from source/tree identity. The existing #79 coordinator remains open.

TLS/disclosure, final artifact/resource/manifest acceptance and all existing
notification, tag/freeze, Apple, publication and production holds remain scoped
and unchanged. This repair does not activate any external acceptance campaign or
create release authority.
