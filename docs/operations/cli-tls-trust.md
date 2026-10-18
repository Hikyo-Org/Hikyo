# CLI TLS trust modes (#966)

Status: proposed contract and implementation awaiting ADR approval and merge.
Use these examples only with a verified signed CLI release containing #966.
Older releases do not support `tls_mode`; an older bundle importer may ignore
that field and will then refuse an HTTPS entry without a pin. An imported file
alone does not prove that the CLI supports this contract.

The [API/CLI ADR amendment](../adr/api-cli-surface.md) adds an explicit HTTPS
trust choice for services behind managed TLS proxies. A proxy may renew its
certificate with a different key. In `system-ca` mode that renewal remains
acceptable when the new certificate has a trusted chain, current validity and
the correct hostname. The trust decision relies on the host's trusted CA set
and control of the provisioned hostname. It does not enforce continuity of one
leaf public key.

## Bundle contract and compatibility

Trust entries and provisioned bundles share these fields:

| Transport | `tls_mode` | `spki_pin` | `cli_socket` |
| --- | --- | --- | --- |
| HTTPS, existing SPKI behavior | omitted or `spki` | Required, base64 SHA-256 of the leaf SPKI | Absent |
| HTTPS, system CA verification | `system-ca` | Absent | Absent |
| Existing loopback HTTP socket | Absent | Absent | Required for credential transport |

Unknown modes and conflicting combinations are refused. An absent mode on
HTTPS always means SPKI, so deleting a pin cannot enable CA verification. In
SPKI mode the exact leaf pin remains the identity check, including for
self-signed/private certificates; it does not add chain, time or hostname
validation to that existing behavior. A changed key is refused. An explicit
`spki` and the omitted legacy spelling describe the same effective mode.
Existing loopback socket ownership, directory permissions and peer-UID checks
continue to apply. TLS modes cannot be attached to HTTP or sockets.

An explicit system-CA bundle is:

```json
{
  "name": "ci",
  "origin": "https://secrets.example.com",
  "tls_mode": "system-ca"
}
```

Go's normal TLS verifier owns the chain, validity and hostname checks. No
custom roots field, bypass flag or automatic fallback is introduced. The
runner's trusted CA set is part of its security configuration. See the
[Go TLS configuration contract](https://pkg.go.dev/crypto/tls#Config).

## Protected provisioning for CI

Provision the bundle as a file through the same protected, out-of-repository
channel as its paired machine credential, such as the CI secret store or a
mounted Kubernetes Secret. The bundle is a trust binding, even though it does
not contain a credential. Untrusted pull-request code must not supply or edit
it. Repository configuration may name an already established instance
reference; it cannot introduce an origin or select a verification mode.

The following Bash commands require a protected file path in
`HIKYO_TRUST_BUNDLE`, an already provisioned automation `HIKYO_TOKEN` with
project read permission, the intended org/project identifiers, and a released
`hikyo` executable on `PATH`:

```bash
set +x
: "${HIKYO_TRUST_BUNDLE:?Set the protected trust-bundle file path}"
: "${HIKYO_TOKEN:?Provision the paired machine credential}"
: "${HIKYO_ORG:?Set the approved organisation identifier}"
: "${HIKYO_PROJECT:?Set the approved project identifier}"
export HIKYO_INSTANCE=ci
hikyo context create ci --instance ci --trust-file "$HIKYO_TRUST_BUNDLE"
hikyo env list --auth machine --instance ci \
  --org "$HIKYO_ORG" --project "$HIKYO_PROJECT" -o json
```

Context creation imports the bundle and records a local context. It does not
contact the server or validate a live certificate. `env list` is the first
credential-bearing check: it must succeed through the recorded origin and
mode. Certificate verification failure stops the TLS handshake before an HTTP
credential can be sent. A target URL that disagrees with the bundle is refused.
Redirects, including redirects within the same origin, are refused without a
second request or credential forwarding.

## Interactive establishment

An operator may explicitly select system-CA verification when establishing a
new HTTPS instance:

```bash
hikyo context create production \
  --instance https://secrets.example.com --tls-mode system-ca
# Or establish trust as part of the ordinary human login:
hikyo login https://secrets.example.com --tls-mode system-ca
```

Both require a controlling terminal. The trust ceremony verifies the server
using normal TLS and asks the operator to confirm the canonical origin and
system-CA tradeoff before recording it. A new origin cannot be silently
established by repository scripts. Omitting `--tls-mode` retains the existing
SPKI fingerprint confirmation. `--tls-mode` cannot override a provisioned
bundle or an already established reference with a different effective mode.

## Deliberate migration and refusal

No existing entry is migrated automatically. Before changing a mode, identify
the affected context references and paired credentials, stop dependent jobs,
and verify the intended hostname and provisioning channel. Delete the old
trust reference explicitly, then re-establish it using the new protected
bundle or interactive ceremony:

```bash
hikyo context delete --instance ci
hikyo context create ci --instance ci --trust-file "$HIKYO_TRUST_BUNDLE"
hikyo env list --auth machine --instance ci \
  --org "$HIKYO_ORG" --project "$HIKYO_PROJECT" -o json
```

The deletion command forgets only the trust-store entry. Existing contexts and
stored sessions remain, but cannot be used until their instance is trusted
again. Re-establish the intended origin deliberately; revoke or remove obsolete
credentials separately when changing servers. Machine credentials retain their
server-side grants and policies. Repeating an
import with the same effective identity is idempotent; changing the origin,
mode, pin or socket under an existing reference is refused. To return to SPKI,
independently verify and provision the new leaf fingerprint, then repeat this
deliberate re-establishment. Clearing the pin is never a migration procedure.

System-CA mode refuses untrusted chains, expired or not-yet-valid certificates,
and wrong hostnames before credentials are sent. SPKI mode continues to refuse
missing pins and changed keys. No trust mode changes machine grants, login
eligibility, token handling, reveal policy or server configuration.

## Signed release and consumer update

Local tests and a signed Git commit do not establish a published CLI release.
Keep #966 open until its ADR approval is recorded, its implementation is merged,
and a release containing that merge has passed the repository's signing and
publication process. Follow [signed nightlies](signed-nightlies.md) or the
[release signing procedure](../release/signing.md); verify the artifact using
the documented trusted release verifier before executing it.

Then update the downstream CI's exact CLI version and protected bundle together.
On an isolated runner, confirm the read-only `env list` check above against the approved origin before
resuming the normal deployment flow. Record the verified release version and
consumer result in #966. No downstream production change or TLS proxy account
access is part of this implementation. A certificate rotation test against a
local test CA is regression evidence; it is not live proxy or release evidence.
