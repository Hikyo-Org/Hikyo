# Update-check release response limit

## Result

The shared GitHub release reader accepts metadata responses up to 8 MiB,
instead of 2 MiB. This repairs release discovery for the instance banner, CLI,
and automatic upgrade callers when GitHub returns a normal, metadata-heavy
release page. Requests remain pinned to the existing release authority and
bounded by size and time; release selection and verification are unchanged.

## Evidence

On 2026-10-30, the public metadata endpoint for `hikyo.went.io` reported
`0.0.1-nightly.20261023.91.gcd2352d2`. That revision uses the same 2 MiB limit.
GitHub's 100-record release endpoint returned more than 2 MiB of JSON. Two
requests measured 2,763,962 and 2,296,206 bytes; the response representation
varies, but both exceed 2,097,152 bytes.

A temporary probe using the real public-only Go HTTP client and release reader
failed with `updatecheck: release response exceeds size limit` before the
change, then accepted 52 releases after it. The network probe was removed;
committed tests remain deterministic.

The new regression builds a complete 100-record page with release notes,
exceeding the old limit, and checks successful discovery and newest-nightly
selection. It failed with the same size-limit error before the fix and passed
after it. Existing non-success and oversized-response rejection tests also
passed with the new bound.

Local validation passed:

```sh
GOFLAGS='-p=2' GOMAXPROCS=2 scripts/ci/fast-feedback.sh --working-tree
go test -p 2 ./internal/updatecheck -count=1
go test -p 2 ./internal/service ./internal/server ./internal/cli -run Update -count=1
go vet -p 2 ./internal/updatecheck
```

Hosted CI and protected merge proof remain separate from these local checks.

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
the warning disappears. Do not disable checks or relax signature verification
to work around discovery failure.
