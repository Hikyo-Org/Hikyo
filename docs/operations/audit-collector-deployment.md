# Independent collector deployment preparation (#1037)

Status: **PREPARATION**. Source delivery, local product exports and disposable
Unix permission exercises do not authorize protected-host installation, collection,
source-copy destruction or notifications. Keep #1037 open. The owner receives a
private, resource-specific approval packet separately; do not put private host,
credential or raw receipt inventory in Git.

An owner may approve an unprivileged LXC instead of a VM. The Proxmox host then
owns the new LUKS mapping and key. Bind only its mounted filesystem into the
guest; do not pass through block devices, enable privileged mode/nesting, or relax
encryption checks. LXC shares the host kernel and does not establish VM isolation.

## Executable assets

All paths below are relative to the repository root. Host paths are intentional
installation paths on a newly approved dedicated Debian guest.

| Asset | Purpose |
| --- | --- |
| `scripts/ops/audit-collector/rehearse-local.py` | Finite local both-engine product export and separate-user SSH/ACL exercise, with exact resource ownership and teardown. |
| `internal/isolation/audit_collector_rehearsal_test.go` | Supported project-rename and failed-login writers, real router/HTTP completion, Python receiver subprocesses, exact lost-ack replay, partial refusal and independent retained read after disposable producer rewrite/removal. |
| `scripts/ops/audit-collector/install-host.py` | Fresh-host-only receiver, four fixed stream identities, restricted SSH keys, exact sudo commands, reader/monitor ACLs, private spools and systemd units. Installs without starting services. |
| `scripts/ops/audit-collector/poll-once.py` | One finite poll under a fixed root-owned origin/expiry binding. Instance mode refuses unattended invocation and requires an explicit supervised human session. |
| `scripts/ops/audit-collector/monitor.py` | Independent expected-stream, freshness, historical refusal, poll-attempt and capacity checks. Never sends notifications. |
| `scripts/ops/audit-collector/health.py` | Content-free TLS health, including stale/stopped detector detection. |
| `scripts/ops/audit-collector/systemd/` | Bounded SSH, tenant-only polling, minute monitor and TLS health services. No instance timer exists. |

Run the self-cleaning local exercise from the repository root, with Docker,
Python 3.11+ and the pinned Go toolchain:

```sh
python3 -I -B scripts/ops/audit-collector/rehearse-local.py \
  .local/reports/audit-1037-rehearsal
python3 -I -B -m unittest discover -s scripts/ops/audit-collector -p 'test_*.py' -v
```

The output directory must not exist. It is private evidence and must be ignored
by Git. The runner registers exact task/container/image ownership before creation,
uses a task-only PostgreSQL 18.6 server bound to loopback, and cleans its containers,
anonymous volumes and image in `finally`. It verifies both engine legs executed.
It preserves private packet/receipt/log evidence for review and shared build caches.
An interrupted host/process may need teardown from `ownership.json`; inspect exact
labels before removing anything. Never use a global prune.

The Linux rehearsal is a disposable Debian container with separate Unix UIDs,
real OpenSSH and SQLite. It is not independent personnel, a protected remote guest,
encryption acceptance, power-loss proof or a final release-artifact test. The Go
fixture uses its existing WebAuthn test authenticator and authority setup, not a
human authentication ceremony on a real installation. It does not grant unattended
org/instance authority. Curl/SSH fault controls in unit tests remain synthetic.

## Resource-specific binding before installation

The independent owner must approve and retain one manifest binding:

1. Server source SHA, immutable artifact digest and build/trust identity; fixture
   installation ID, engine, origin and TLS trust; source owner and exact test lease.
2. Native tenant/org/project/environment IDs and expected instance scope; named
   project automation principal/credential/expiry and human instance reader with
   current MFA. A label is not a native instance or principal identity.
3. Destination guest/hypervisor/storage, security administrator, read and deletion
   owners, encrypted mount identity/capacity/unlock custodian, receiver revision
   and file hashes, per-stream submit public keys and pinned SSH host key.
4. Independent expected-stream inventory, monitor revision, watchdog host and
   health certificate trust. Record shared administrators, power, network, storage
   and hypervisor dependencies. Another account/container is not proof of stronger
   administration or failure-domain independence.
5. Audit metadata recipients, permissions, retention/deletion agreement, review
   window, no-spend bounds, authorized synthetic mutations, cleanup and unknown
   outcome reconciliation. #1038 notification receipts remain a separate gate.

Do not choose the current main as the final release candidate. A resource-specific
approval may authorize a fixture at a selected build without qualifying a release.
Keep any unavailable native identity, trust or artifact binding HOLD until the
authorized provisioner supplies it through the private custody mechanism.

## Fixed destination installation

Prerequisites on the approved dedicated Debian guest: Python with SQLite,
OpenSSH server/client, sudo, ACL tools, coreutils, curl, CA certificates and
cryptsetup. The independent owner mounts a dedicated unlocked encrypted local
filesystem at `/srv/hikyo-evidence`. No NAS data or source recovery keys are used.
`install-host.py` verifies an exact mount backed by an active device-mapper
cryptsetup mapping. It does not format, unlock, mount, provision a guest or change
the network. Verify the mapping's cipher, key custody and backing disk independently.

For an approved unprivileged LXC, add `--host-encryption-proof`,
`--expected-luks-uuid` and `--expected-filesystem-uuid`, using the independently
recorded approved storage IDs. Do not derive the expected IDs from the guest's
proof file. This mode verifies the
fixed host-owned read-only receipt `/run/hikyo-evidence-host/encryption.json`
instead of requesting guest block access. The host first verifies the active
LUKS mapping and filesystem UUID, then binds this metadata read-only into the
guest. Checks cover owner/mode/type/size, duplicate fields, observed filesystem
device number/mapper source, this guest hostname and a five-minute receipt window.
The key and block device stay on the host. This is trusted operator custody
evidence, not cryptographic remote attestation or protection from common host root.

Receipt fields are exactly `version` (integer 1), `guest`, `container_id`, `node`,
`mount_source`, `luks_uuid`, `filesystem_uuid`, `device_major`, `device_minor` and
`verified_ns`. Keep actual identifiers and receipts private. Refresh a receipt
only after reconciling the real mapping; never create one in the guest to bypass
a failed encryption check. Owner-authorized disposable campaigns must tear down
their exact guest, binding, mapping, newly allocated volume and ephemeral key,
then verify absence while retaining the required private receipts locally.

Private `streams.json` contains exactly `pg-tenant`, `pg-instance`, `sqlite-tenant`
and `sqlite-instance`. Each value has the runbook's six receiver configuration
fields. Use a project scope for scheduled tenant collection, an empty scope for
instance collection, 16 MiB and 20,000 row limits. The source ID must be bound to
the actual fixture installation in the private manifest. The installer assigns
the destination database path itself. The public-key directory contains exactly
one approved Ed25519 submit public key per stream, named `<stream>.pub`.

Only after resource approval and independent inspection, from the reviewed source
root on that guest, with operator-provided paths in these environment variables:

```sh
sudo python3 -I -B scripts/ops/audit-collector/install-host.py \
  "$HIKYO_AUDIT_STREAM_MANIFEST" "$HIKYO_AUDIT_SUBMIT_PUBLIC_KEYS"
sudo systemd-analyze verify scripts/ops/audit-collector/systemd/*
```

Fresh installation is exclusive: any existing code/configuration, custody file,
service unit or identity refuses. A failed/unknown installation is not retried
until the independent administrator inventories its exact partial resources.
Do not delete a database or reinitialize custody to make the installer pass.
The `--rehearsal` exception accepts only synthetic `local-1037-` sources inside
a disposable container; it is not permission to install on a protected guest.

The SSH service listens only on guest loopback port 2222. Each `hk-s-<stream>`
key is `restrict` plus a root-owned forced wrapper. The wrapper rejects all
requested commands and invokes only `timeout ... receiver.py receive` as
`hk-e-<stream>` through an exact sudo rule. It grants no shell, SFTP, init/status,
forwarding or arbitrary arguments. Four startup sessions and a 180-second receive
deadline bound resource use. The root sshd unit intentionally needs privilege
transitions for sshd/sudo; applying `NoNewPrivileges` to it would break the design.

The polling identity is `hk-p-<stream>`, with only its private spool, source read
credential and submit key. Destination readers `hk-reader` and `hk-monitor` get
read/traverse ACLs to the retained SQLite inode, with no write/delete rights.
Only the independent root/deletion administrator may change installed code, ACLs,
trust, monitor inventory or delete retained packets. A compromised receiver UID
can modify its own journal; this design is not WORM storage.

Provision source curl configuration, SSH configuration, private key and pinned
known-hosts file in `/srv/hikyo-evidence/spool-<stream>`, mode 0600, owned by that
poller. Keep directories 0700, with the installer's monitor traversal/read ACL.
Do not print secrets, use verbose tracing or pass bearer values in arguments.
Source TLS uses verified system trust or an approved CA; the SSH configuration
pins the dedicated host key for `[127.0.0.1]:2222`, sets IdentitiesOnly, User,
IdentityFile and UserKnownHostsFile. No source runtime receives destination keys.

Create root-owned, non-writable `/etc/hikyo-evidence/poll-<stream>.json` with exactly
`url` and `approved_until` (a timezone-bearing ISO timestamp). Use the full retained
project or instance export URL with exactly `receipt=v1`. The expiry bounds the
fixture lease; a nonzero result/expired lease is HOLD. Do not add from/to/sequence
filters or silent scope narrowing. Instance credentials are supplied only for
each approved supervised window, then removed; no instance timer is installed.

After TLS health certificate/key provisioning (key readable only by its custodian
and `hk-monitor`) and actual target-side acceptance, enable only the approved
units. Installation alone starts none:

```sh
sudo systemctl daemon-reload
sudo systemctl start hikyo-evidence-sshd.service
sudo systemctl start hikyo-evidence-poll@pg-tenant.service hikyo-evidence-poll@sqlite-tenant.service
```

Supervised instance collection uses the actual human/MFA credential already
provisioned privately for that window:

```sh
sudo -u hk-p-pg-instance python3 -I -B /opt/hikyo-evidence/poll-once.py pg-instance --supervised
sudo -u hk-p-sqlite-instance python3 -I -B /opt/hikyo-evidence/poll-once.py sqlite-instance --supervised
```

Before activating the campaign monitor, independently verify all four initial
complete receipts, including the supervised instance commands above, and record
cold-start historical coverage as unknown. A failed initial poll is HOLD, not a
baseline to erase. Then activate the expected-stream checks and tenant timers:

```sh
sudo systemctl start hikyo-evidence-monitor.timer hikyo-evidence-health.service
sudo systemctl start hikyo-evidence-pg-tenant.timer hikyo-evidence-sqlite-tenant.timer
```

A never-received interval observed by the active monitor stays unresolved even
after the first success. This deliberate conservative behavior is also exercised
as a separate negative-control fixture. Intentional outage controls leave their
intervals HOLD for independent review; no reset is used to obtain a passing check.

The independent watchdog must validate the health certificate and poll `/health`
from its own approved host. HTTP 503, transport failure, stale/future snapshot,
stopped detector or stopped health process are failures. The endpoint returns
only PASS/HOLD, never event contents. Proposed fixture cadence is five-minute
tenant polls, minute detector checks and fifteen-minute receipt freshness; this
is not an accepted universal SLA. Expected instance streams stay in inventory
when supervision ends, so staleness remains visible rather than disappearing.

## Acceptance and retention agreement

Stage until the exact header/terminal receipt, row count, success/outcome and EOF
checks pass, then publish only after the matching durable destination acknowledgment.
Full retained-scope replay and stable ID/content dedup remain mandatory.
Snapshot time is an inclusive recorded-at upper bound, not a continuation cursor.
Lost acknowledgments are reconciled by exact pending-byte retry before another
source read. Poll intent is durable before transport; failed or killed/unknown
attempts remain in cumulative history, and stale/unavailable/missed detector
intervals latch HOLD even after later success. Never rebuild an unknown packet or advance after partial HTTP 200.

The approved exercise must cover both source engines and trails; per-page
revocation/expiry; partial/refused export; destination outage, crash and lost ack;
commit/cutoff races; source pruning and destination capacity. Rewrite/removal is
permitted only on exclusively owned synthetic producer copies after a retained
receipt, with independent reader/ACL proof afterward. No export acknowledgment
gates source pruning. Unknown or pruned intervals stay HOLD after later success.

The destination receives authorized audit event content, including actor/scope
references, IP/user-agent/access metadata and free text permitted by the product.
A no-value route is not metadata-free. Approve encrypted local storage and any
backup recipient separately. Do not copy live SQLite files without the SQLite
backup API. This installer creates no backup job and performs no deletion.
Bound the fixture's duration, batch budget, disk allocation and evidence retention;
measure real packet/index growth and stop on capacity HOLD. Production/year-long
retention sizing and power-loss durability remain unproven.

Retained receipts prove what the producer claimed to export. A compromised
producer may omit, rewrite or fabricate future exports; these assets do not prove
independent completeness, tamper-proof source history, WORM storage, a built-in
SIEM feed or notification delivery. Source, local, hosted CI, protected destination,
final candidate and external notification evidence must be reported separately.
