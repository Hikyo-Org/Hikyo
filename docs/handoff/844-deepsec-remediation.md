# PR 844: DeepSec remediation

## Outcome

The complete DeepSec finding set has been remediated across the Go services,
CLI, web client, operator, deployment chart, CI, and release tooling.

The final medium finding no longer sends CLI passwords or bearer credentials
over loopback TCP. Local CLI authentication now uses a Unix-domain socket with
strict path custody and kernel-reported peer UID verification on Linux and
macOS. Remote HTTPS retains certificate pinning. Unsupported platforms fail
closed and require pinned HTTPS.

## Operator impact

- Configure the server with `--cli-socket` or `HIKYO_CLI_SOCKET`.
- Create the socket parent as an existing, real, owner-controlled `0700`
  directory. The server creates the socket as `0600`.
- Pass `--socket` during local credential establishment. The binding is stored
  in the immutable trust entry.
- Re-establish legacy loopback-only trust entries before credential-bearing CLI
  requests. They remain readable for migration but cannot send credentials.
- After an unclean server exit, verify that no server owns the old socket path,
  then remove it explicitly before restart.

## Validation

- Full Go suite: 8,485 tests passed across 134 packages.
- Isolation suite: 2,002 tests passed.
- Web: typecheck and 1,287 tests passed.
- Generated TypeScript client: typecheck and 21 tests passed.
- Helm, fuzz classification, Linux and Windows compile checkpoints passed.
- Commit signature, DCO, and diff hygiene checks passed.

## Remaining gates

GitHub CI and human review remain required before merge. No merge authorization
is included in this handoff.
