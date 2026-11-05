# Nightly changelog groups handoff

Task branch: `dunky13/group-release-changelog-by-type`.

Nightly release notes now group commits by Conventional Commits type instead of
listing them as one block. The order is breaking changes, features, fixes,
performance, reverts, refactoring, tests, documentation, CI, build, chores, style
and unclassified subjects. Each heading names its SemVer level: major, minor,
patch, no bump or unclassified.

A summary line suggests a bump from the commit types in the range. Nightly versions
stay on the latest stable base, so the line is advisory and covers only that range.
A breaking change is a non-merge commit with a `!` after the type or scope, or with
a line starting `BREAKING CHANGE:` or `BREAKING-CHANGE:` in its message.

Known limits:

- The level comes from the commit type alone, so `fix(ci)` counts as a patch and a
  `refactor` counts as no bump even though the shipped bytes change.
- Unclassified subjects do not raise the suggestion, and every revert counts as a
  patch, so the suggestion can understate a range. It reports `unknown` only when
  nothing typed outranks an unclassified subject.
- Only nightly notes use this format. Stable notes come from the release ceremony.
- Published releases are immutable and keep their existing notes.

Local validation passed: `scripts/release/nightly-changelog_test.sh` under macOS
awk, mawk, gawk and busybox awk, `scripts/ci/check-nightly-release_test.sh`, and
shared fast feedback. Hosted CI and GitHub rendering are not established by these
checks.

Next steps: merge to `dev`, promote to `main`, and read the next published nightly
to confirm the notes render as intended on GitHub. That has not been observed yet.
