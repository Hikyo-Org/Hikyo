# PR #996 admission: metadata-only DCO reads

The trusted queue publisher repeatedly refused PR #996 before conflict checking.
[Run 39212605951](https://github.com/Hikyo-Org/Hikyo/actions/runs/39212605951/job/117929158448)
failed the canonical DCO checker with exit 128 while trying to read blob
`a3d336c4d2bc4b0d6538b7ca85ce6f401f8f4721`, the deleted settings route barrel.

## Cause and repair

Admission fetches immutable commits and trees into a bare repository using
`--filter=blob:none`. The canonical checker used `git show -s` for author and
message extraction. Despite suppressing patch output, that command still runs
rename detection and can require a deleted file's blob. The checker subprocess
has no reason to read source contents while validating sign-offs.

Both reads now use `git log -1 --no-patch` with the same exact commit OID and
format. Commit-range selection, author matching, trailer parsing, authenticated
Dependabot exemption, refusal behavior and signature requirements are unchanged.
No missing blob is fetched, no exception is added and no PR code is executed.

The queue publisher executes this checker from trusted `main`, so a change only
on #996 cannot authorize its own admission. This small CI dependency must land
before #996 can receive successful fresh queue assessment. Queued PR #1002 is
left intact.

## Evidence

Real filtered metadata for main `eb991c991bcad853590cb0ab2c09367052a92c02` and
#996 head `81a82f1c0e5d5912e5557c7fc6ce72d3a5138dbb` reproduced the exact missing
blob error locally on Git 2.54.0. `git show -s` failed on implementation commit
`9855696b54a3`; both `git show --no-renames -s` and metadata-only `git log -1`
succeeded. The repaired canonical checker then reported two signed commits
against the same retained metadata. The hosted failure used Git 2.55.0.

The existing real-Git DCO policy table now constructs a deleted file and an added
similar file, then removes the deleted blob from its temporary bare repository.
All five cases failed before the script repair. This covers valid sign-off,
missing sign-off, mismatched identity, a spoofed bot author and authenticated
Dependabot, preserving both acceptance and refusal expectations.

Independent Standards and Spec source reviews found no actionable issues in the
repair. Full local validation and hosted acceptance are recorded separately;
the source reviews do not establish either.

## Reproduction

```sh
go test -p 2 -parallel 2 ./scripts/ci/pr-gate \
  -run '^TestQueueUsesCanonicalDCOSignoffAndAuthenticatedException$' -count=1
sh scripts/ci/check-dco_test.sh
```

The focused Go test fails with the original two `git show -s` metadata reads and
passes with the replacement. No network or human credentials are needed by that
regression. The focused DCO/merge-proof tests, canonical shell fixture, complete
`scripts/ci/...` tests, `pr-gate` race suite and scoped vet passed. Structural
graph refresh completed with the existing seventeen Astro parser warnings;
documentation semantics were not regenerated. Protected merge-group validation
and live admission of #996 remain required after publication. Release and
deployment are outside this task.
