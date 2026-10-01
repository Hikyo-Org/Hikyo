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
- After an unclean server exit, restart automatically reclaims a same-user
  `0600` socket only when its connection is refused. A private persistent startup
  lock serializes concurrent restarts. Live sockets, symlinks, and other objects
  are refused without removal.
- Argon2 cost changes are refused at startup and during runtime configuration
  replacement while current-epoch password credentials use different costs.
  Restore the previous Argon2 settings; select costs before establishing passwords.
- Kubernetes metrics stay loopback-only. The chart does not expose a scrape
  Service or support Prometheus scraping pod IPs. A separately configured
  same-pod collector can scrape loopback and forward via authenticated transport.

## Review repairs

- Legacy CRD objects compare absent creation policies as the `Owner` default.
- Pinned loopback HTTPS is accepted without a Unix socket; HTTP still requires one.
- Restore holds prevent certificate issuance while worker and manual CRL
  publication continue protecting revocation coverage.
- Optional authenticator input no longer announces itself as required.
- CLI help names Forgejo federation refusal, workflow fixtures pin each protected
  condition, and fuzz classification tolerates toolchain diagnostics around JSON.

## Validation

- Full Go suite: 8,485 tests passed across 134 packages.
- Isolation suite: 2,002 tests passed.
- Web: typecheck and 1,287 tests passed.
- Generated TypeScript client: typecheck and 21 tests passed.
- Helm, fuzz classification, Linux and Windows compile checkpoints passed.
- Commit signature, DCO, and diff hygiene checks passed.

Review-repair validation: 1,288 web tests plus typecheck/lint; password-cost,
CRL-hold and isolation invariant checks on SQLite and PostgreSQL; CLI and CRD
admission suites; 50 race-enabled socket test repetitions; Windows app/service
compilation; chart assertions and ShellCheck. Workflow mutation probes reject
widened conditions independently on the trusted validation job and step.

## Remaining gates

GitHub CI and human review remain required before merge. No merge authorization
is included in this handoff.
