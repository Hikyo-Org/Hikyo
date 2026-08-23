# Handoff: #367 shared retention bounds fields

Issue: https://github.com/Hikyo-Org/Hikyo/issues/367. Base:
`428dd6a5e347479a7a3697e2953ce10b7543db58`.

## Contract

- `RetentionBoundsFields` is the single owner of the whole-day and revision
  controls, exact-seconds warning, deliberate day replacement, and absent-age
  status shared by organisation and project retention editors.
- Each editor still owns its draft bounds, validation refusal, and local mode
  selector. Editing either shared bound clears an existing refusal.
- Policy or entity-scope changes reset editor drafts through the existing
  effects. The redundant route-key remounts were removed, leaving one reset
  mechanism even when two entities have equal-valued policies.
- Organisation `unlimited` and project `inherit` saves both send null bounds.
  Existing retention wire shapes and validation remain unchanged. Generated
  outputs: none.

## Coverage

- The new render suite covers exact-second protection and replacement,
  refusal clearing, the bounded organisation payload, and both null-bound mode
  flips.
- `pnpm --dir web run typecheck` passed.
- `pnpm --dir web run test` passed: 317 tests across 38 files.
- Standards and issue-spec reviews reached `CLEAN` in round 2 of 3 after
  making entity identity part of the single reset effect and exercising edited
  bounds in the payload test.
- `pnpm --dir web run build` passed.
- `git diff --check` passed.
