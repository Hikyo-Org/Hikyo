# Manual remote-instance pairing

Status: owner decision locked 2026-10-20; operative when
[PR #1014](https://github.com/Hikyo-Org/Hikyo/pull/1014) merges. The owner approved
a PR-only exception accepting the completed GPT 6.1 Sol High reviews in place
of the unavailable cross-provider ADR review. Cross-provider review was skipped,
not completed. See the decision record below.

Extends [multi-instance](multi-instance.md) and
[machine-identities](machine-identities.md). Existing manual remote creation,
instance-connection permissions, and human workspace authentication remain
available under their existing contracts.

## Decision and amendment record

The owner approved manual remote pairing, arbitrary nonempty custom secrets,
local QR scanning and image import, and delivery through protected merge in
PR #1014. On 2026-10-20 the owner explicitly approved the
[PR-only review exception](https://github.com/Hikyo-Org/Hikyo/pull/1014#issuecomment-6300666440):
the completed OpenAI GPT 6.1 Sol High Standards and Spec/security source reviews
replace the required cross-provider ADR review for this PR. Anthropic review is
unavailable; it remains recorded as skipped. This is not an independent
cryptographic audit or a general change to governance or future review gates.

The originating [threat-model ticket #8](https://github.com/Hikyo-Org/Hikyo/issues/8)
and [multi-instance ticket #71](https://github.com/Hikyo-Org/Hikyo/issues/71) were
reopened to record the amendment. Their ADRs enumerate the administrator-triggered
TLS identity self-probe as the exception to the zero-remotes outbound baseline.
The owner decision is locked; these amendments become operative only when
PR #1014 merges. Signed commits, DCO, CI, resolved review findings, and the
mandatory merge queue remain required. Release and deployment are outside this
decision.

## Outcome

A requester chooses a password or phrase, or generates a phrase, and creates
an independent signing key locally,
shares only the phrase outside Hikyo, and waits for the remote administrator to
create an invitation manually. The requesting instance makes no connection to
the remote until the returned invitation is redeemed. Public request creation,
public invitation listing,
anonymous approval queues, and unsolicited approval notifications do not exist.

The administrator creates, lists, and revokes invitations under
`instance-config`. An invitation authorizes exactly the existing directory
connection class. It cannot create human accounts, grant access to secret
values, or consent to workspace origins. Humans still authenticate on the
remote's origin to open a workspace.

## Pairing and custody

1. Local authenticated preparation accepts an exact user-chosen password or
   phrase, including four or five characters, or generates a chosen count of
   independent random words from EFF's 7,776-word list. Generation defaults to
   six words, approximately 77.55 bits of entropy; repeated words are valid.
   There is no minimum strength or fixed word-count requirement for custom
   secrets. Custom secrets are bounded to 4,096 UTF-8 bytes and generation to
   1 through 128 words for resource limits. Case and whitespace are significant.
   An independently random Ed25519 key is created in every mode; the password
   or phrase never seeds it.
2. The local instance seals the temporary continuation, including the private
   key and phrase, under its instance keyring. The continuation is bound to the
   creating actor and expiry. No remote address is needed for preparation.
3. The remote administrator enters the phrase, intended peer label, and finite
   invitation and connection lifetimes. The panel confirms the instance's
   configured public HTTPS origin. Serving identity comes from the active
   application configuration, never from invitation inputs or request headers.
4. The returned opaque invitation contains the remote identity and a strong
   random capability. It never contains the phrase or the requester's private
   key. The capability is stored only as a verifier. Temporary phrase and
   exchange secrets are sealed under the remote's keyring.
5. The requester imports the invitation. Its server verifies the supplied TLS
   pin before any HTTP request or pairing secret reaches the wire. The invite
   capability is checked before the remote performs expensive PAKE work.
6. A mutually confirmed PAKE exchange binds the phrase to the invitation,
   remote origin and pin, and requester's public-key thumbprint. Successful
   redemption atomically consumes the invitation and enrolls the public key
   with a directory credential and its machine principal and grant.
7. The viewing instance verifies the server proof, performs a pinned directory
   fetch, rejects self/duplicate identities, and stores the credential and
   private key inside the existing sealed remote credential field.

An invitation is single-use, finite, revocable, and subject to both preauth
admission limits and a durable invitation attempt budget. The public exchange
surface cannot create invitations. Exchange and replay state live in the
database so another process cannot bypass single use or replay admission.

## Client deployments and invitation transport

The current delivery remains the bundled Hikyo server, including a MacBook
running with SQLite. The browser and CLI ask that local instance to prepare
and redeem pairing state. Dedicated macOS, Linux, Windows, iOS, and Android
clients are future work; this feature does not introduce those applications
or require operators to adopt a different deployment model.

Keep the remote exchange independent of that local deployment. The existing
version-1 invitation is base64url-encoded JSON containing its version, invitation
ID, HTTPS origin, TLS pin, random capability, and expiry. The same bundle is
displayed as text or a QR code. It contains no requesting-instance address,
SQLite identity, phrase, private key, or encrypted local continuation. A future
client can use outbound HTTPS to the existing start and finish endpoints;
redemption does not require a requesting client to expose an inbound listener.
Manual invitation creation, finite directory authority, and separate human
workspace authentication continue to apply to every client type.

The current `requester_state` and stored paired credential are local custody
formats, not portable client interchange formats. A future native client must
generate its own independent signing key and protect that key and credential
using storage appropriate to its platform. It must not depend on a local
Hikyo instance keyring, transfer a server's sealed continuation to another
device, or put private material into an invitation or QR code. Hardware-backed
key support and algorithm compatibility need platform-specific validation;
the current Ed25519 profile does not establish that every platform can hold
its key in non-exportable hardware storage.

QR scanning, QR-image import, and pasted text are input adapters for the same
invitation, with the same validation, expiry, revocation, and phrase
authentication. They keep the phrase separate and show the destination before
explicit confirmation makes a remote request. Camera access is requested only
after a user action, is restricted to this origin, and stops on cancellation,
successful scanning, page hiding, or leaving the pairing panel. Image decoding
runs locally using the bundled QR decoder, without uploading pixels or adding
external script sources. PNG and JPEG input is bounded before decoding; camera
frames and image pixels are downsampled before QR processing. Operating-system
link handlers remain future work.

Before shipping a dedicated client, verify interoperability against the
existing server, including exact UTF-8 secret handling, context serialization,
the SRP password profile, TLS pin verification, and Ed25519/DPoP proofs. Add
portable conformance vectors and test each target's networking and secure
storage APIs then. Version incompatible protocol changes explicitly rather
than changing version-1 semantics or weakening verification for a platform.

## Serving identity discovery

`HIKYO_EXTERNAL_ORIGIN` must explicitly name the public HTTPS endpoint. The
listen-derived development default is insufficient. Invitation listing returns
that configured origin and any origin setup error without making a connection.
The create request has no origin or fingerprint fields.

An optional owner-managed `HIKYO_PAIRING_SPKI_PIN` supplies an independently
trusted base64 SHA-256 SPKI fingerprint for exceptional deployments, including
self-signed reverse proxies and servers unable to reach their public endpoint.
It takes precedence and applies only to new invitations. Removing it restores
automatic resolution. Configuration is validated and activated through the
existing instance settings workflow.

Managed configuration migration version 5 adds this optional declaration to
older catalogues without creating a value or replacing existing operator input.
An absent value continues to select automatic discovery. Earlier mandatory
release migrations still require their explicit default or operator input.

Without an override, resolution makes a bounded TLS-only connection to the
configured origin, requiring certificate-chain and hostname validation before
deriving the pin from the actual peer certificate. The current generation's
loaded, unexpired native certificate may additionally anchor trust when its
hostname matches. This supports direct self-signed native TLS without assuming
that an internal certificate is the public endpoint's certificate. Otherwise
discovery relies on Web PKI trust; it never trusts an unverified first connection.
It sends no HTTP request
or pairing data to the endpoint. The explicit directory egress proxy, if set,
is honored through its existing verified CONNECT tunnel; ambient proxy
environment variables are ignored. Failure never bypasses certificate checks.

This extends the multi-instance outbound contract with an administrator-triggered
TLS self-probe during invitation creation, even with no configured remotes.
Preparation, listing, boot, and background work do not discover certificates.
Creation authorizes before discovery, releases the database transaction while
performing network work, then rechecks authorization when storing the invitation.
The origin, pin override, and native certificate come from one immutable active
application generation. Existing invitation and remote pins remain immutable;
an external key change requires a fresh invitation and pairing.

## Cryptographic profile

The PAKE adapter uses [Proton's Go SRP implementation](https://github.com/ProtonMail/go-srp),
which its upstream README identifies as used for ProtonMail authentication.
Hikyo pins version 0.0.7 and the upstream signed 2048-bit modulus, verified by
the library. Peers cannot supply alternative groups or signed moduli. This is
the library's version-4 application protocol, not an implementation of the
TLS-SRP wire format in RFC 5054.

The SRP password salt is the first 16 bytes of a SHA-256 hash of a
domain-separated, deterministic encoding of invitation salt and identity,
serving origin and pin, and requester public-key thumbprint. Both sides
construct the expected context. This prevents relaying a phrase proof to
enroll a different key or serving identity. The 16-byte salt is intentional:
the upstream version-4 KDF appends a constant and bcrypt consumes its first
16 salt bytes. This profile keeps 128 bits of context binding and tests the
actual upstream client/server exchange.

The entire exact UTF-8 secret is prehashed before the library's bcrypt KDF:
base64url without padding of SHA-256 over the fixed domain
`hikyo.remote-pairing.password.v1`, one NUL byte, that 16-byte context salt, and
the secret bytes. Both sides use this 43-byte printable input for every secret.
This avoids the upstream bcrypt implementation's 72-byte truncation and cyclic
NUL aliases. The slow library KDF remains in place. The public context salt
prevents reuse of a context-independent inner digest; it is not a server-only
pepper. See [the upstream key schedule](https://github.com/ProtonMail/bcrypt/blob/227b4adcab57/cipher.go)
and [OWASP's prehashing discussion](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html#pre-hashing-passwords-with-bcrypt).

TLS with the confirmed pin protects exchange confidentiality. Both SRP proofs
are checked; the requester's public key additionally proves possession during
redemption. No new PAKE arithmetic, secret exponentiation, or bespoke symmetric
encryption is introduced.

Ongoing directory access uses
[RFC 9449 DPoP](https://www.rfc-editor.org/rfc/rfc9449.html), with the existing
go-jose library providing JWS signatures and RFC 7638 thumbprints. The profile
allows only public Ed25519 JWKs and EdDSA proofs. Validation binds each proof to
the enrolled key, credential hash, HTTP method and expected resource URI, with
a short issuance window and database-backed one-time JTI admission. Duplicate
headers, duplicate JSON members, private JWK material, unknown headers, and
algorithm substitution fail closed.

Replay admission retains at most 256 live entries per enrolled key and 10,000
across the instance. The per-key bound prevents one peer from occupying the
entire shared allowance. Both censuses and admission use the same durable
transaction and instance lock; reaching either bound refuses new proofs without
evicting replay protection. Records expire at the proof's issuance time plus
65 seconds, including the five-second future-clock allowance, so the maximum
retention from acceptance is 70 seconds. These are resource bounds on directory
requests, independent of invitation attempts and credential lifetime.

The new RFC 9449 application validation and SRP context-binding glue require
security review. Using established libraries does not establish independent
audit of Hikyo's composition.

## Authority and recovery

A paired credential is never accepted as an ordinary bearer token. Its
database binding is checked inside the same transaction as authentication and
authorization. Existing manually issued directory bearer credentials remain
a separate supported path.

Connection revocation and credential epoch invalidation continue to apply.
Restore must retire pairing artifacts, preserve compatibility with archives
predating their tables, and discard stored foreign authority. Key rotation
must cover invitation and exchange sealed fields as well as the existing
sealed remote credential payload. Re-pairing provides recovery after loss of
the requesting private key; private-key recovery from the phrase is impossible.

## Limits of the guarantee

An immediate pairing window does not increase a short password's entropy.
Short custom secrets rely more on the invitation's independent random
capability remaining private and the durable eight-start attempt limit. This
is an explicit requester choice, including for two instances they own. Longer
generated or chosen secrets provide more resistance if an invitation leaks.

Someone who obtains both phrase and invitation before successful redemption
can attempt to enroll their own key. Manual phrase sharing authenticates its
holder; the remote has no previously authenticated requester key. Subsequent
key binding protects against theft of the connection credential alone. It does
not protect a requesting host whose private key and credentials are both
compromised.

The administrator and requester should exchange the phrase and invitation over
channels appropriate for that risk. Neither artifact belongs in URL query
parameters, ordinary logs, shell arguments, persistent browser storage, audit
payloads, or react-query mutation caches.
