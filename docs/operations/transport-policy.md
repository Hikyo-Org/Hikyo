# Post-quantum key-exchange policy for controlled hops (#1127)

Status: **implemented on this branch.** The owner authorized merge to `dev` on
2026-11-04 once required checks and reviews are green, which is the maintainer
security approval for this change, and accepted publication in the next nightly.
It is operative only after it merges and a build containing it is deployed and
applied. The stale-node fence (DEC-10) is not part of that authorization: the
owner amended DEC-10 so strict is per-process (see
[Strict is a per-process property](#strict-is-a-per-process-property-until-a-fence-exists)),
and the fence stays #1127 work. The
[decision register](../adr/post-quantum-migration.md#decision-register) (DEC-05,
DEC-06, DEC-10) carries the decisions, and the DEC-10 amendment and E-22 are
registered in #1155. The [handoff](../handoff/1127-transport-policy.md) records
evidence, the hop capability matrix and the remaining work.

## What strict does and does not claim

Strict is **handshake confidentiality only**. It makes a recording adversary who
stores the key exchange today unable to decrypt the session later with a quantum
computer, on the hops named below.

Strict says **nothing about who the peer is.** Certificate authentication is
reported separately and is classical: a classical certificate chain, or for pin
based clients a leaf public-key pin that replaces chain and hostname checks. A
hybrid group with a classical certificate is never post-quantum authentication,
and no output of this feature says otherwise. A pin-based client gets
confidentiality with no authenticity claim. Post-quantum certificate chains
belong to #1133 and are not part of this change.

Strict also does not cover a proxy or ingress terminator in front of Hikyo, the
database, provider adapters, mail, federation, the update check or pairing. Those
stay at `compat` (see the matrix in the handoff).

## The two profiles

| | `compat` (default) | `strict` (opt-in) |
| --- | --- | --- |
| TLS versions | 1.2 and 1.3, exactly as before | 1.3 only |
| Groups | Go's defaults, exactly as before | exactly `X25519MLKEM768` |
| Session tickets and resumption | unchanged | off |
| Classical-only or TLS 1.2 peer | served | refused during the handshake, before any request byte or credential |
| Claim | none | handshake confidentiality on this hop |

`compat` is today's behavior and the default for every install, new or
upgraded. An upgrade records `compat` explicitly (migration 6) and never raises
the floor silently. Strict does not depend on `GODEBUG` or Go's defaults: it sets
the version bounds and the group list explicitly, so `GODEBUG=tlsmlkem=0` or a
toolchain change cannot widen what a strict hop accepts.

## Native listener

Setting: `HIKYO_TLS_PROFILE` = `compat` or `strict`. The value is matched
exactly; `Strict`, ` strict` or anything else is refused. The environment value
is only a bootstrap seed. It is validated when it seeds a first adoption and at
managed validation. Once an installation has adopted managed configuration the
environment value is not read for policy, so a stale or invalid value does not
stop startup; Hikyo logs a warning (`HIKYO_TLS_PROFILE in the environment is
ignored`) when it differs from the adopted profile, without echoing it.

Precedence: **managed > bootstrap > default `compat`.** The environment variable
only seeds the managed configuration at first adoption. After adoption, the
adopted managed value decides, and a process environment value, `GODEBUG` or a
per-request flag cannot relax it. Changing it is a managed change: edit the key
in the configuration project, publish and Apply, with the usual fresh
authentication and audit. Host-authority recovery can select an older published
revision, including one below strict (registered as E-21 in the migration
policy; not redesigned here).

Strict needs a TLS certificate pair. Selecting it on a listener that would serve
plaintext (trusted-proxy mode or loopback development) is refused at startup and
at managed Apply. The check reads the owner profile being applied together with
the node document's certificate, so a node document that drops the certificate is
refused while the applied profile is strict, and one Apply can downgrade to
`compat` and drop the certificate. Parser-only validation does not know a node's
certificate and accepts the value; the refusal happens when a node applies it.

Applying a profile change closes existing public connections even if the
certificate is unchanged, so a classical keep-alive cannot outlive strict. The
upgrade maintenance listener uses the same certificate and profile.

## Clients that call Hikyo

- **CLI.** The trust entry or provisioned bundle carries `"tls_profile":
  "strict"`. It is applied on top of the entry's trust mode (`system-ca` or
  `spki`) and never replaces it. Changing an established entry's profile needs
  deliberate re-establishment, like a changed pin. There is no interactive
  `--tls-profile` flag yet, so use a provisioned bundle. A CLI release since the system-CA
  trust change (the nightlies from 2026-10-19 and `v1.0.0`) decodes the trust
  store with unknown fields disallowed, so it refuses the store rather than
  reading a strict entry as `compat`. **Earlier CLI builds decode with
  `json.Unmarshal`, which silently drops the field: a strict entry then
  behaves as a `compat` entry.** Do not rely on `tls_profile` for a CLI older
  than that change. A local-socket entry cannot be strict because it is
  not a TLS connection. With `HTTPS_PROXY` naming an `https://` proxy the proxy
  leg shares the policy, so strict also refuses a classical-only proxy.
- **Operator client.** `client.NewClient(..., client.WithTLSProfile(...))`
  enforces the profile with the instance's CA bundle or system roots unchanged.
  There is no per-instance CRD field yet, so no deployed operator selects
  strict today.
- **MCP.** Hikyo serves MCP on the native listener, so MCP callers are covered
  by listener strictness. Hikyo contains no outbound MCP client.

## Strict is a per-process property until a fence exists

Strict is enforced by each process on its own listener. Hikyo does not yet fence
a node whose active configuration generation is below the desired one. #1155
amends DEC-10 so that strict is per-process and registers the gap as E-22; the
fence itself is not built and stays #1127 work (its criterion is HOLD). Until it
exists:

- A stale node keeps admitting credentials and data on the listener at the
  profile it last activated, including `compat`. Node status shows it as
  `pending`, but that state is derived from generation and revision data
  (`internal/service/self_config_status.go`), not from the node reporting what it
  enforces. A silent node is `pending` or `unknown` for the same reason. It is
  not a "not enforcing" report and it does not stop the node serving.
- **No fleet-wide claim can be made**, however many nodes are strict. A claim is
  only about one node, at one activated generation, on its own listener.
- Repair and recovery boots run the previous completed configuration snapshot
  (`ResolveRepairRuntimeBundle`) and host-authority recovery can select an older
  published revision (E-21). Those graphs use that snapshot's profile, which can
  be `compat` while a newer revision adopted strict. A joining node that is
  missing from the node document keeps the adopted profile and refuses to boot
  when it cannot serve it (no certificate); it does not fall back to `compat`.

## Evidence: configured, test and observed are different things

| Class | Where | Meaning |
| --- | --- | --- |
| Configured support | server log `native listener TLS policy ... evidence=configured` at each generation; `tls_profile` in the CLI trust entry | What the hop is set to enforce. Not proof any connection used it |
| Test negotiation | `go test` evidence in the PR and handoff | A handshake in an isolated fixture. Not production |
| Observed negotiation | server debug log `native listener TLS negotiation ... evidence=observed`; CLI `-vv` line `TLS hop=cli-client profile=... version=... group=... evidence=observed` | A handshake that reached verification in this process, which runs before the handshake finishes, so it may still fail afterwards. Recorded with TLS version and `ConnectionState.CurveID` mapped to closed names. An unmapped or missing group is `unknown`, never inferred |

Observation is of this process's own connection. It is never used to infer the
group of a proxy, ingress or other terminator in front of or behind it. No peer
address, certificate, header, path or secret is logged. A metrics series for
negotiations is not added: the metric registry is pinned by the locked ops-spec
ADR and needs its own amendment (see remaining work).

## Rollout and rollback

1. Inventory the hops. Only the native listener and owned clients calling Hikyo
   can be strict; confirm every client of the listener is on a build whose TLS
   stack offers `X25519MLKEM768` (Go 1.24 and later does by default). A
   browser or tool that does not will be refused.
2. Keep `compat`. Verify the new binary on every node. The offline upgrade gate
   runs migration 6, which writes `HIKYO_TLS_PROFILE=compat` into a new desired
   configuration revision; a binary older than this change refuses that revision
   because it does not know the key, as it does for earlier migrated keys. Binary
   rollback therefore uses the upgrade's backup restore, not an in-place downgrade.
3. Publish and Apply `HIKYO_TLS_PROFILE=strict` through managed configuration
   only after step 1 and 2. Watch node status until every node reports the new
   generation as active.
4. **A node still behind the desired generation keeps serving at its old
   profile and is only reported as pending.** Hikyo does not yet fence it. Do not
   treat the listener as strict until every node is active, and do not rely on
   strict for a node you cannot see.
5. Verify from a client with `-vv` that the observed line shows `tls1.3` and
   `x25519mlkem768`, and from a classical-only client that the connection is
   refused.
6. Roll back by publishing `compat` and applying it. Do not run an older binary
   that ignores the key: managed validation refuses an unknown key, which is the
   intended fail-closed behavior. Rollback is a downgrade of a security
   property and is audited as a managed change.

## Refusals and what they mean

| Message contains | Cause |
| --- | --- |
| `HIKYO_TLS_PROFILE: profile must be compat or strict` | Unknown or mis-cased value |
| `strict ... requires a TLS certificate pair` | Strict selected where the listener would serve plaintext |
| `tls: no key exchanges supported by both client and server` | A classical-only peer met a strict hop |
| `tls: client offered only unsupported versions` | A TLS 1.2 only peer met a strict hop |
| `... does not match the pin recorded` | The SPKI pin check, unchanged and still enforced under strict |
