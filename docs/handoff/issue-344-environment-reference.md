# Handoff: #344 environment reference ownership

Issue: https://github.com/Hikyo-Org/Hikyo/issues/344. Base:
`21267cccd4dd977fb23a6d9d7c8fe38934db1b05`.

## Contract

- `EnvInput.Ref()` and `EnvPlan.Ref()` are the public owners for an
  environment's artifact reference. Both delegate to one policy: created
  environments use their name; existing environments use their server id.
- `BuildProjectPlan` refuses `Create == true` with a non-empty `EnvID` as
  malformed input. Created environments remain tokenless and name-addressed.
- Values paths, plan summaries, mapping rows, overwrite and trim rows,
  manifest import state, and values digests consume the owned reference.
- Existing valid artifact bytes, ordering, dual-dialect behavior, and generated
  outputs remain unchanged. Generated outputs: none.

## Coverage

- `TestBuildProjectPlanRefusesCreatedEnvironmentWithID` was observed failing
  before the validation change and passing afterward.
- Importer package: 145 tests passed with `-count=1`.
- CLI package: 300 tests passed with `-count=1`.
- Import conformance package: 76 tests passed with `-count=1`.
- Full Go validation: 3,519 tests passed across 61 packages with
  `go test -p 4 -count=1 ./...`.
- Standards review reached `CLEAN` in round 2 of 3 after extracting the shared
  reference policy. Issue-spec review was clean in round 1.
