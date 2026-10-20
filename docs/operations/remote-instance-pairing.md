# Pair remote instances with a phrase

This feature is implemented on the current branch. It is not yet a released
contract. The [design proposal](../adr/remote-instance-pairing.md) records its
security profile and outstanding maintainer review.

Pairing connects a viewing instance to a serving instance's directory. Each
operator needs `instance-config` on their own instance. A successful pairing
reveals directory metadata. Workspace access still requires human
authentication and origin consent on the serving instance.

The current browser and CLI flows use a local Hikyo server. Running the bundled
server with SQLite on a MacBook is supported by this deployment model; a
standalone client app is not required. Dedicated desktop and mobile clients
are future work. The invitation format and remote exchange are separate from
the local instance's storage and can be reused by those clients.

## Browser flow

1. On the viewing instance, open **Remotes** and the phrase-pairing panel.
   Choose **Use my own password or phrase**, or generate a phrase with the
   word count you prefer. Four- or five-character custom passwords are allowed;
   generated phrases default to six words. Keep that page open. Preparation
   does not contact the serving instance.
2. Share only the phrase with the serving administrator outside Hikyo. Use a
   channel appropriate for granting this access. Never share the private local
   continuation.
3. On the serving instance, its administrator opens **Remotes** and creates an
   invitation using that phrase, a peer label, and expiry settings. Check the
   read-only **Connects to** address. Hikyo uses its configured public origin
   and resolves the certificate fingerprint automatically.
4. The administrator returns the invitation text or its QR encoding. In the
   original viewing page, choose **Scan QR with camera**, select a saved image
   under **Import QR image**, or paste the invitation text. Enter a remote name
   and review the destination, fingerprint, and expiry. Choose **Connect remote**.
   Confirm that the connected card shows the intended identity and directory.

Camera scanning requires a secure browser context, normally HTTPS, and camera
permission. If permission is refused or a camera is unavailable, use image
import or pasted text. **Stop camera** cancels scanning, and hiding or closing
the page also stops the camera. PNG and JPEG images stay in the browser; they
are never uploaded. Input images are limited to 8 MB and 16 million pixels.
Scanning or importing an invitation fills the form without redeeming it;
the connection starts only after **Connect remote**.

Case and spaces are significant: the administrator must enter the exact same
secret. The browser fields accept one line and reject multiline pastes before
the browser can strip line breaks. CLI hidden prompts also read one line and
process terminal control characters; use a secret that both operators can enter
unchanged. Arbitrary multiline/control-character UTF-8 secrets are API-only.
Short passwords are easier to guess if the invitation leaks; an immediate
pairing window does not change that. The invitation remains independently
random and admits at most eight starts.
Custom secrets have a 4,096-byte UTF-8 resource bound; generated phrases allow
1 through 128 words. No minimum password strength is enforced.

The phrase and invitation are separate artifacts. The invitation contains the
approved origin and fingerprint, so its authenticity matters. Hikyo verifies
that fingerprint before sending any pairing HTTP request. QR is an alternative
encoding of the invitation, with the same trust requirements.

## Automatic connection details

Configure the serving instance's public HTTPS address once in instance settings
using `HIKYO_EXTERNAL_ORIGIN`, for example `https://serving.example.com`. A
container's internal listening address or a listen-derived development default
does not establish the public address. Invitation forms and CLI create commands
do not accept a separate origin or fingerprint.

Hikyo verifies the configured public endpoint's certificate chain and hostname
over a bounded TLS connection and derives the fingerprint from the actual peer
certificate. Its currently loaded native certificate can also anchor trust
when valid for that hostname, supporting self-signed native HTTPS. This avoids
assuming that an internal certificate identifies a TLS-terminating proxy.
No HTTP request or pairing secret
is sent during discovery. A configured directory egress proxy is honored.
Discovery runs only after an administrator creates an invitation; opening the
panel, listing invitations, preparing a request, or starting Hikyo does not probe.

If the reverse proxy uses a self-signed certificate, or the server cannot reach
its own public endpoint, set `HIKYO_PAIRING_SPKI_PIN` in instance settings through
the existing configuration validation and Apply workflow. This optional setting
is the trusted fingerprint of the external HTTPS endpoint, applies to new
invitations, and takes precedence over automatic resolution. Remove it to resume
discovery. Certificate verification is never silently disabled. If the external
TLS key changes, create a fresh invitation and pair again.

Upgrading an existing managed instance adds the optional fingerprint declaration
without setting a value. Automatic discovery remains the default, and any
existing operator-configured fingerprint is retained.

An administrator with the deployed endpoint's trusted public leaf certificate
can compute its SPKI fingerprint with OpenSSL. In Bash, from the directory
containing that certificate as `serving-certificate.pem`:

```bash
set -o pipefail
openssl x509 -in serving-certificate.pem -pubkey -noout |
  openssl pkey -pubin -outform DER |
  openssl dgst -sha256 -binary |
  openssl base64 -A
```

The result is a 44-character base64 fingerprint. Use the certificate deployed
at the HTTPS endpoint the requester will reach. A different certificate at an
internal Hikyo listener will not identify a TLS-terminating reverse proxy.

Browser pairing state is held in memory and clears when the page is closed,
the session changes, or preparation expires. Hidden disclosures can be shown
again while their pending state remains valid. A discarded or expired local
preparation requires a new phrase and invitation.

## CLI flow

Use a CLI and server built from this branch. Establish each local CLI context
and administrator session first, following the existing
[TLS trust guide](cli-tls-trust.md). These examples assume context names
`viewing` and `serving`, a controlling terminal, and a private working directory.
Files named below must not already exist; disclosure refuses to overwrite them.

On the requesting operator's machine:

```sh
hikyo remote-pairing prepare --context viewing --output-file pairing-request.json
```

To choose your own password or phrase, use a hidden terminal prompt:

```sh
hikyo remote-pairing prepare --context viewing --custom-secret \
  --output-file pairing-request.json
```

To select a generated phrase length instead, use `--words`, for example:

```sh
hikyo remote-pairing prepare --context viewing --words 4 \
  --output-file pairing-request.json
```

Choose one preparation command. `--custom-secret` and `--words` are mutually
exclusive. Secret values are never command arguments.

The command writes a private JSON file containing the phrase, encrypted
`requester_state`, and expiry. Read it locally and share only `phrase` with the
serving administrator. Do not send the whole file. The continuation is bound
to the creating principal and viewing instance.

On the serving operator's machine, create an invitation using the serving
instance's configured public HTTPS address and automatic certificate identity:

```sh
hikyo remote-invite create --context serving \
  --label viewing-peer \
  --lifetime 10m --credential-lifetime 720h \
  --output-file pairing-invitation.txt
```

The phrase is requested through a hidden terminal prompt. The command writes
the invitation to a private file and prints only metadata. Return the file
through an approved channel. On the requesting machine, preserve private file
ownership and permissions after transfer, then accept:

```sh
hikyo remote-pairing accept serving-peer --context viewing \
  --request-file pairing-request.json --invite-file pairing-invitation.txt
hikyo remote list --context viewing
```

The new remote should show a reachable directory and the serving identity.
Omitting either input-file option uses a hidden terminal prompt. Phrases,
continuations, and invitation values are never accepted as command arguments.
Remove temporary private files after successful pairing or abandonment.

List or revoke a pending invitation on the serving instance:

```sh
hikyo remote-invite list --context serving
hikyo remote-invite revoke --context serving --id INVITATION_ID
```

The list returns metadata only. Invitation lifetime is at most one hour;
the local preparation lasts one hour. Each exchange lasts two minutes and an
invitation admits at most eight starts over its lifetime. Expiry, revocation,
consumption, or exhausted attempts require a new invitation. Connection
credentials are finite and subject to the existing credential policy.

## Revocation and recovery

A redeemed invitation creates a normal directory connection entry, bound to
the enrolled signing key. Revoke that connection in the serving instance's
connection credential panel, or with `remote-credential revoke`. Revoking an
invitation after consumption does not revoke the connection it created.
Removing a viewing-side remote deletes the local credential and key; it does
not revoke serving-side authority.

Losing the requesting private key requires pairing again. It cannot be
reconstructed from the phrase. A supported restore invalidates prior
invitations, exchanges and credentials, and discards stored foreign remotes.

Someone who obtains both the phrase and invitation before redemption can
pair first. Once enrolled, stealing the connection token alone is insufficient:
each directory request must also prove possession of its independent private
key. Compromise of the viewing host can expose both.
