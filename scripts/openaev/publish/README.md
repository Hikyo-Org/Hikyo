# Exact release resolution and publication

Automatic nightly resolution scans at most 40 pages of 25 releases. Each page
has a 4 MiB response ceiling and a 30-second request deadline. Pages are consumed
one at a time, and at most one matching release is retained. The complete bounded
history must establish a unique exact producer run number. A duplicate candidate,
response overflow, malformed page, or exhausted history window fails closed with
no release mutation. A completed scan with no candidate skips a producer that
reused an earlier nightly tag.

For an older release outside that window, replay its exact tag from the default
branch after the workflow is available:

```sh
gh workflow run openaev-release.yml --repo Hikyo-Org/Hikyo --ref main \
  --raw-field tag=v0.0.1-nightly.20261012.75.g868703c7
```

The command starts a read-only rehearsal and uses direct tag lookup. Rehearsals
always run the full suite, even when an earlier success check exists, and cannot
create a success check or mutate a release, even when all 313 assertions pass. Inspect the run
linked by the CLI for the result. Production publication remains limited to a
successful automatic post-publication run with complete native proof, isolation,
owned cleanup, trusted harness identity, and an unchanged immutable release.


The production publisher creates only a completed successful GitHub CheckRun
named `OpenAEV release validation` on the released source commit. Its external
identity includes the exact release ID and source SHA. Idempotence accepts only
an exact completed success owned by the GitHub Actions app (ID 15368,
`github-actions`), with the expected check name, external identity and commit.
Check lookup is bounded to five pages of 100 matching names, with the same
response and request limits. Exhaustion fails closed. Automatic duplicate
triggers may skip an existing owned PASS; explicit operator replays always run.

The publisher token has `contents: read` and `checks: write`. It never updates
release bodies, titles, tags or assets. Concurrent human note edits therefore
remain untouched. [GitHub does not support conditional PATCH requests unless an
endpoint documents an exception](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api),
and release updates document no such exception. Adding a result asset after
publication is also prohibited by [immutable release
rules](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases).
The success-only check avoids both release mutation paths.
