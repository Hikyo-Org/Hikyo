# PR #1104: bounded release discovery

PR: <https://github.com/Hikyo-Org/Hikyo/pull/1104>

## Result

The shared GitHub release reader traverses up to 100 records in pages of 20,
then looks up the latest stable release separately. Oversized pages retry at
10, 5, then 1 record. Each smaller size divides the preceding size, preserving
its numeric offset even when a later page must shrink. Successful short or
empty pages end traversal; failures return no partial result.

The original 2 MiB per-response limit is retained. The lookup also has a 16 MiB
aggregate body budget, at most 104 requests including reductions and latest
stable, and one discovery deadline. Size detection reads at most one extra byte
past a byte limit. The deadline uses the original two-request timeout allowance,
capped at 30 seconds and subject to any earlier caller deadline. The instance's
3-second HTTP timeout therefore permits at most 6 seconds for the entire lookup;
individual requests still have their original timeout.

Only successful JSON responses that exceed the response-size limit trigger
smaller-page retries. HTTP errors, invalid content types, malformed JSON,
invalid release metadata, cancellation, and exhausted budgets remain visible
failures. A single record above 2 MiB still fails. Requests are constructed at
the pinned GitHub authority; response-supplied pagination links are ignored.
Release selection, exact-version lookup, and signature verification retain
their existing semantics and validation.

This supports release windows larger than 8 MiB without raising individual
response limits. It is still intentionally bounded: excessive single records,
aggregate metadata, latency, and rate limiting can prevent discovery. GitHub
pagination is a mutable feed, not a snapshot across requests.

## Evidence

On 2026-10-30, public metadata for `hikyo.went.io` reported
`0.0.1-nightly.20261023.91.gcd2352d2`, whose source uses a 2 MiB response limit.
GitHub's original 100-record endpoint returned 2,763,962 and 2,296,206 bytes in
two requests, both exceeding 2,097,152 bytes. A temporary real Go public-only
client probe reproduced `updatecheck: release response exceeds size limit`.

The paginated reader accepted all 52 published releases in four requests:
three 20-record pages and the latest-stable endpoint, which returned the
expected absence, HTTP 404. The final probe took 3.119 seconds within the
instance's shared 6-second budget. These are local public-source observations,
not authenticated production recovery evidence. The temporary probe is removed
before handback; committed tests remain deterministic.

Pagination regressions failed against the prior single-page implementation
and pass with the final reader. Coverage includes a release window exceeding
8 MiB, newest-nightly and latest-stable selection, all page-size reductions
both initially and after an earlier successful page, the 100-record boundary,
short and empty pages, the 104-request worst case, aggregate byte exhaustion,
response closure, cancellation, non-retryable errors, oversized single-record
refusal, and deadline expiration across multiple requests with a fake clock.

Local validation passed:

```sh
GOFLAGS='-p=2' GOMAXPROCS=2 scripts/ci/fast-feedback.sh --working-tree
go test -p 2 ./internal/updatecheck -count=1
go test -p 2 ./internal/service ./internal/server ./internal/cli -run Update -count=1
go vet -p 2 ./internal/updatecheck
```

Hosted CI and protected merge proof remain separate from local checks.

## Production boundary and next steps

Production has not been modified. The authenticated instance update-status
request and browser recovery have not been verified. The Proxmox command policy
refused read-only service-log access with `CMD_POLICY_NOT_ALLOWLISTED`, so the
server's own logged error remains unobserved.

After review, CI, and authorized merge, publish the fix through the existing
signed release process. An authorized operator must use the documented manual
signed-bundle procedure in [the upgrade guide](../site/src/content/docs/docs/upgrades.mdx)
if the installed release cannot discover updates. Reload the Projects page and
verify the authenticated `/api/v1/instance/update-status` request succeeds and
the warning disappears. Keep checks and signature verification enabled.

GitHub's documented pagination parameters are `per_page` and `page`:
[release API reference](https://docs.github.com/en/rest/releases/releases#list-releases).
