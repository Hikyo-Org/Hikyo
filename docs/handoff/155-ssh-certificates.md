# Handoff: #155 SSH certificates, short-lived principal access

Issue: https://github.com/Hikyo-Org/Hikyo/issues/155 (blocked-by #146 and #147,
both merged).

Short-lived OpenSSH **user** certificates as an identity-bound dynamic
credential. An operator creates or imports an SSH user CA in an environment,
defines profiles that bound what a certificate may say, and names the human or
machine principals allowed to request through each profile. Hikyo manages
certificates only: it is not a remote shell, a session proxy, a bastion or a
host inventory.

## ADR gate

`mvp-boundary.md` §4.1 listed "SSH certificates | Out, different product | No
trigger; new map only". Per that register's reading rule a no-trigger row only
returns through a redrawn map, never by scope creep. The owner redrew it
(2026-09-26); the row now carries an operative "Redrawn" banner that promotes
SSH user certificates In and keeps host certificates, PKI/X.509 and
encryption-as-a-service Out. The dynamic-secrets row's "no SSH certs" sentence
described the #147 promotion's own scope and is left as history.

## Decisions

1. **Everything is environment-scoped.** Workload credentials are granted at
   environment depth (`machineDepths`), so a host or CI job holding
   `read@environment` must be able to fetch trust material and request a
   certificate without a project grant. Separate CAs per environment is also
   the operational best practice (a staging CA cannot sign for production).
   Tables `ssh_cas`, `ssh_ca_keys`, `ssh_profiles`, `ssh_profile_requesters`,
   `ssh_certificates` are all `class=environment chain=org_id,project_id`
   (migration 00061, both engines).

2. **CA custody.** A CA is a named container; its signing material lives in
   `ssh_ca_keys` rows (`active -> retiring -> retired`, at most one `active`
   per CA, enforced by a partial unique index). The private key is PKCS#8 DER
   sealed under the project DEK with
   `ProjectFieldAAD{OwnerTable:"ssh_ca_keys", OwnerRowID:<key id>,
   FieldTag:"private_key"}` and is covered by `reencrypt` (`ssh_ca_key` unit,
   lint coverage map). No route, CLI verb or audit payload ever returns it.
   Import accepts an unencrypted OpenSSH or PKCS#8 PEM through protected input
   only (stdin / file, never argv); passphrase-protected keys are refused.

   **Rotation** creates a new active key (generated or imported) and moves the
   old key to `retiring` with `retire_after = now + overlap`. The old key's
   ciphertext is nulled in the same transaction: a retiring key can never sign
   again, only be trusted. Overlap is bounded `[0, 30d]` and defaults to the
   longest `max_ttl` among the CA's live profiles, so certificates issued just
   before rotation stay valid for their whole life. `retire` ends the overlap
   early. The trust bundle is `active` plus `retiring` keys whose
   `retire_after` is still in the future; retirement needs no worker because
   it is evaluated from durable timestamps.

3. **Profiles** bind: allowed principals (exact strings, 1..32), an optional
   fixed `force-command`, `source-address` CIDRs (a request may only narrow
   them), extensions (closed set of the five standard `permit-*`), allowed
   user-key algorithms (`ed25519`, `ecdsa-p256`, `rsa-3072`; RSA >= 3072 bits),
   `default_ttl` and `max_ttl` (60s..30d), `enabled`, and an explicit requester
   list. Disabling stops issuance immediately (the issuance transaction reads
   the flag); it does not revoke. Delete tombstones and, with
   `revoke_issued=true`, revokes every live certificate issued through it.

4. **Authorization.**
   - CA and profile management: `manage-identities@project` on LevelEnv ops,
     human session.
   - Reads (`ssh-ca.inspect`, `ssh-profile.inspect`, `ssh-cert.inspect`, trust
     bundle, KRL): `read@environment`, machine-holdable, audited-none (public
     material and metadata only).
   - Issue (`ssh-cert.issue`): formula `read@environment` (machine-holdable,
     like `lease.mint`), plus two service-side conjuncts: the caller must be on
     the profile's requester list (this list IS the per-profile opt-in the
     issue's handoff asked for, the analogue of `machineRevealWithdrawn`), and
     a human must consume a fresh keyless mint reauthentication window
     (`NewMintReauthIntent(env, nil)`). A missing conjunct answers the uniform
     nonexistent response.
   - Revoke (`ssh-cert.revoke`): `read@environment` plus "caller is the
     requester or holds `manage-identities@project`".

5. **Issuance is one transaction.** The request authorizes, gates, re-reads
   the CA key (must be `active`), the profile (must be `enabled`) and the
   requester row, opens the sealed CA key, signs, and inserts the certificate
   row plus its audit event in the same SERIALIZABLE transaction. The signed
   certificate is only released from the attempt whose commit succeeded, so a
   node that lost a race with a rotation, profile disable or requester removal
   on another node cannot hand out a certificate (the concurrent write aborts
   the stale snapshot). There is no extend/renew: a certificate is immutable;
   a new one is a new request. Generated user keys (the slow RSA path) are
   produced before the transaction; the private key is returned once in
   OpenSSH format and never stored, not even hashed.

   Serials are random in `[1, 2^63-1]`, unique per CA. `valid_after` is
   backdated 60s for clock skew. `key_id` is `hikyo:<env>:<cert>:<principal>`
   so `sshd` logs name the Hikyo principal.

6. **Status vocabulary** (the ticket's "record deletion vs cryptographic
   revocation vs natural expiry"). Stored `state` is only `issued | revoked`.
   Read surfaces derive `status`:
   `revoked` (in the KRL while its signing key is trusted and it has not
   expired) > `expired` (past `valid_before`; nothing to publish) >
   `untrusted` (its signing key was retired or its CA deleted; hosts that
   follow the trust bundle already refuse it) > `active`.
   Plus `in_krl: bool`. Deleting a profile or CA is record deletion only: it
   never revokes on its own (the delete response says so), which is why
   profile delete carries the explicit `revoke_issued` switch.

7. **Principal and grant revocation** are enforced by a per-node sweeper
   (`app.sshSweeper`), the same shape as the dynamic worker but with no claim
   fence because its only write is idempotent: `UPDATE ... SET state='revoked'
   WHERE state='issued'`. For every live certificate it re-authorizes the
   recorded requester for `ssh-cert.issue` at the environment and re-checks
   the requester row; a definite refusal (principal deleted, grant pulled,
   removed from the profile) revokes durably with a system-actor audit row.
   Transient datastore errors never revoke: a certificate whose check fails
   is skipped and reported, and the pass continues, so one persistently
   failing row cannot shield every later certificate. Profile requester edits
   run the same check inline so removal takes effect in the same transaction.
   A profile tombstone keeps its requester rows, so deleting a profile
   without `revoke_issued` leaves its live certificates valid (the sweeper
   still revokes them if the requester loses its grant); environment purge
   removes the rows.

8. **KRL.** `GET .../ssh-cas/{ca}/krl` returns an OpenSSH KRL
   (PROTOCOL.krl, format version 1) with one `KRL_SECTION_CERTIFICATES` per
   trusted CA key carrying a `KRL_SECTION_CERT_SERIAL_LIST` of revoked,
   not-yet-expired serials. `x/crypto/ssh` has no KRL writer and shelling out
   to `ssh-keygen` is refused for crypto, so `internal/sshca/krl.go` is a
   small container-format encoder (no primitive: it writes big-endian integers
   and length-prefixed strings) with a KAT against `ssh-keygen -Q` in the
   OpenSSH end-to-end test. **Bound:** at most 65,536 serials per CA; beyond
   that the KRL request fails loud (409) rather than truncate, and
   `hikyo_ssh_krl_entries` exposes the size. The bound is naturally reached
   only with more than 65k revocations inside one `max_ttl` window.

   Host trust: `GET .../ssh-cas/{ca}/trusted-keys` returns
   `TrustedUserCAKeys` lines. The docs page gives the `sshd_config` fragment
   (`TrustedUserCAKeys`, `RevokedKeys`) and a timer that refreshes both with a
   workload credential.

9. **Restore.** A restore rolls the certificate table back to the backup's
   instant; a certificate issued and revoked after the backup has no row and
   so cannot appear in the KRL. The runbook step is: after restore, rotate
   every SSH CA with `--overlap 0`. Stated on the docs page; not automated,
   because forcing a trust change on every host is the operator's decision.

10. **Surfaces.** API revision 6; operation ids use the `Scim`-style casing
    (`listSshCas`, `issueSshCertificate`, ...) because the parity gate's
    word splitter and the TS generator disagree on runs of capitals like
    `SSHCAs`. CLI `hikyo ssh-ca`, `hikyo ssh-profile`, `hikyo ssh-cert`
    (a generated key goes through the print triad; `ssh-ca krl` writes a
    fresh file only). SPA: an "SSH certificates" tab on the machine-access
    page (`routes/SSHCertificates.tsx`): per-environment CAs (create/import,
    rotate, retire, trust bundle, KRL download, delete), profiles
    (create/edit/delete with optional revocation) and certificates (issue
    behind the passkey mint ceremony with the shared display-once lifecycle,
    revoke). Metrics (label-free): `hikyo_ssh_certificates_active`,
    `hikyo_ssh_krl_entries`, guarded by `hikyo_ssh_gauges_known`.
11. **Environment delete** refuses (409) while the environment holds a live
    SSH CA; once every CA is deleted, the environment delete purges the SSH
    rows (`ssh.PurgeEnvironment`, on both delete paths).
12. **Crypto boundary.** `golang.org/x/crypto/ssh` is confined to
    `internal/sshca` by an OpenSSH protocol import confinement in
    `internal/boundary` (the isolation suite may import it as an independent
    certificate verifier, tests only). The crypto chokepoint skips that
    subtree; every other `x/crypto` package stays confined to
    `internal/crypto`. Callers name keys through `sshca.PublicKey`.
13. **Input bounds.** Every caller-supplied second count (profile
    `default_ttl_seconds`/`max_ttl_seconds`, issue `ttl_seconds`, rotate
    `overlap_seconds`) is range-checked as an integer before conversion to
    `time.Duration`, so an oversized value cannot wrap into range. An issue
    request's `extensions` is explicit when present (non-nil, possibly empty)
    and takes the profile default when absent.

## Tests

- `internal/sshca`: signing constraints, KRL encoding KAT and bound, fuzzing
  of the principal/CIDR validators.
- `internal/isolation/ssh_e2e_test.go`: both engines. Issue (generated and
  supplied key), profile bounds, requester refusal, human ceremony, revoke,
  expiry status, rotation overlap and retire, profile disable, principal and
  grant revocation via the sweeper, profile delete without revocation
  surviving a sweep, a TTL that would wrap `time.Duration`, restart (a second service instance on
  the same datastore) and multi-node takeover (a stale instance cannot issue
  after another node rotated or disabled).
- `internal/sshca/openssh_e2e_test.go`: real `sshd` on a loopback port with
  `TrustedUserCAKeys` + `RevokedKeys`; the issued certificate authenticates
  with the real `ssh` client, is refused after it lands in the KRL, is refused
  once expired, and a certificate from the retiring key authenticates during
  overlap. Skips when `sshd` is absent unless `HIKYO_SSHE2E_REQUIRED=1`.

## Progress and evidence

- [x] ADR banners (`mvp-boundary.md` §4.1 SSH row redrawn, `system-architecture.md`
      § Jobs note) and this doc
- [x] `internal/sshca` + OpenSSH interop test (real `sshd` and `ssh`, KRL checked
      with `ssh-keygen -Q`)
- [x] Migration 00061 (both engines); `internal/buildcompat/development.json`
      regenerated with PostgreSQL 18.4 (the base declaration was first
      reproduced byte for byte, then the only diff is the 00061 entry and the
      two schema digests); legacy upgrade-drill fixture reverses 00061
- [x] Store repo + runtime, authz ops/store ops/wire routes, audit events,
      reencrypt coverage, budget classification, fence annotations
- [x] Service, server handlers, app wiring (sweeper on every node, gauges)
- [x] OpenAPI + apigen + TS client, parity, no-proxy and artifact pins
- [x] CLI verbs, auth-kind rules, help golden, CLI reference row
- [x] SPA tab, sensitivity inventory, Playwright flow (desktop and mobile)
- [x] Isolation lifecycle on SQLite and PostgreSQL; audit-emitter closure;
      formula pin and audited exemptions updated
- [x] Docs site page `ssh-certificates.mdx`

## CI notes

- `internal/sshca/openssh_e2e_test.go` runs when `sshd`, `ssh` and
  `ssh-keygen` are installed and skips otherwise. To make it mandatory in CI,
  install `openssh-server` in the core test job and export
  `HIKYO_SSHE2E_REQUIRED=1` (a workflow change needs a token with the
  `workflow` scope, so it is not part of this branch).
- `TestSAMLMetadata*` in `internal/service` fail in sandboxes that force an
  HTTP proxy; they fail identically on `main` there and are unrelated.
