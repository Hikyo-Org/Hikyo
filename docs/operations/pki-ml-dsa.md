# Private PKI with ML-DSA-65 issuers and leaves (#1133)

Status: **mechanism implemented below the API, not operative, public surface on
HOLD.** The [PKI ADR](../adr/pki.md) amendment becomes operative only when the
#1133 implementation merges after its own maintainer security approval
(**security approval: not given**). Nothing here authorizes creating a
production root or activating trust anywhere. The [handoff](../handoff/1133-pki-ml-dsa.md)
holds the evidence and the acceptance status.

## The HOLD you must know first

`/api/v1` is frozen at `v1.0.0` and the PKI `key_algorithm` and `key_algorithms`
enums are closed, so a real server, the `hikyo` CLI and the WebUI **refuse
`mldsa65` today** and create nothing. That includes `pki issuer import`: its
algorithm comes from the key, so the service refuses an ML-DSA key unless the
instance has enabled the algorithm, and production wiring never does. The Hikyo
side of the procedure below therefore runs only in the test harness
(`TestPKIMLDSALifecycle`, both engines) until the owner decides how the enums may
grow. The OpenSSL side is real and was run (`TestMLDSAOpenSSLInterop`).

## What exists

| Item | Value |
| --- | --- |
| Algorithm | `mldsa65`: pure ML-DSA-65 (FIPS 204, category 3), the only parameter set (DEC-02). `mldsa44` and `mldsa87` are refused everywhere, including as an imported key or a CSR |
| X.509 | Go `crypto/x509`: SPKI and signature OID `2.16.840.1.101.3.4.3.18`, absent parameters, empty ML-DSA context, no pre-hash (RFC 9881 shape). Leaves carry `digitalSignature` only; key encipherment and key agreement do not apply and are dropped |
| Issuer key custody | Sealed seed-only PKCS#8 (34 byte private key field `80 20 || seed`) under the existing instance AAD and DEK. The expanded and seed-plus-expanded forms are refused on import and when opening. No export operation exists |
| Fingerprint | Unchanged spelling `sha256:` + base64url(SHA-256(SPKI)); the SPKI already carries the algorithm OID |
| CRL | Signed by the issuer's algorithm (ML-DSA-65), same validity, numbering and worker rules |
| Leaf key | A profile that lists `mldsa65` accepts an ML-DSA CSR or generates a key. The generated key is PKCS#8, 128 characters of PEM, display-once and never stored |

## Chain rules (DEC-19)

An ML-DSA chain needs an **ML-DSA root**. Hikyo enforces this instead of
labelling mixed chains:

- Every certificate in an ML-DSA issuer's path, the trust anchor's
  self-signature included, must carry an ML-DSA-65 key and signature. Create
  (`create-intermediate --parent`), install and import all refuse a mix.
- The reverse is refused too: an ECDSA or RSA intermediate cannot sit under an
  ML-DSA root. An existing classical chain, including one behind a root outside
  Hikyo's own key set, is unaffected.
- A classical root over an ML-DSA intermediate is valid X.509 and would verify in
  a relying party, which is exactly why it is not accepted as a PQ-authenticated
  chain.
- The leaf key and the issuing path are separate choices. An ML-DSA leaf under a
  classical issuer is allowed and is **not** a PQ-authenticated chain; an ECDSA
  leaf under an ML-DSA issuer has a PQ-signed path and a classical leaf key. Only
  an ML-DSA leaf under an ML-DSA path is ML-DSA end to end.
- **Rotation never crosses the line.** Profiles reference issuer names, so letting
  `pki issuer rotate` turn a classical name into an ML-DSA one would change the
  signature of every leaf under those profiles with no profile change. A rotation
  to the other family is refused. A post-quantum issuer is a new name, and adding
  it to a profile is a widening, so it needs a new profile and a new binding.
- Cross-signed and hybrid (dual-signature) chains are not offered. No composition
  is defined (DEC-03 leaves composition to the verifier's trust state).

## Offline root procedure

The offline root is ML-DSA-65 and lives only on an air-gapped machine. These
steps use OpenSSL 3.5 or later (run and verified with OpenSSL 4.0.3, see the
matrix). The OpenSSL steps are exactly what `TestMLDSAOpenSSLInterop` executes.

1. **Offline machine: root key and certificate.**

   ```sh
   umask 077
   openssl genpkey -algorithm ML-DSA-65 -out root.key
   cat > root.cnf <<'CNF'
   [req]
   distinguished_name = dn
   x509_extensions = v3_root
   prompt = no
   [dn]
   CN = Example Offline PQ Root
   [v3_root]
   basicConstraints = critical,CA:TRUE,pathlen:1
   keyUsage = critical,keyCertSign,cRLSign
   subjectKeyIdentifier = hash
   CNF
   openssl req -x509 -new -key root.key -days 3650 -config root.cnf -out root.pem
   ```

   OpenSSL writes seed plus expanded PKCS#8 by default. That is fine for a root
   that never enters Hikyo. Keep `root.key` offline, with a backup under your own
   custody rules; Hikyo never sees it.

2. **Online: Hikyo makes the intermediate key and its CSR (HOLD).**

   ```sh
   hikyo pki issuer create-intermediate issuing --key-algorithm mldsa65 \
       --common-name "Example PQ Issuing CA" --csr-out issuing.csr
   ```

3. **Offline: sign the CSR.** Check the CSR first, then sign it as a CA that may
   sign leaves only.

   ```sh
   cat > issuing.cnf <<'CNF'
   basicConstraints = critical,CA:TRUE,pathlen:0
   keyUsage = critical,keyCertSign,cRLSign
   subjectKeyIdentifier = hash
   authorityKeyIdentifier = keyid
   CNF
   openssl req -in issuing.csr -noout -verify
   openssl x509 -req -in issuing.csr -CA root.pem -CAkey root.key -CAcreateserial \
       -days 365 -extfile issuing.cnf -out issuing.pem
   ```

   Hikyo requires `keyCertSign` and `cRLSign`, a subject key identifier, a current
   validity window and the pending key. Keep the intermediate's validity inside the
   root's: Hikyo caps every leaf at its issuer's `notAfter`, not the issuer at the root's.

4. **Online: install (HOLD).** Carry back `issuing.pem` and `root.pem`, never the
   root key.

   ```sh
   hikyo pki issuer install issuing --cert-file issuing.pem --chain-file root.pem
   ```

   A certificate for another key, a classical root, a chain that does not end at
   the root you supplied, an expired window or a missing key usage is refused and
   the version stays `pending`.

5. **Create a new profile and bind it (HOLD).** `key_algorithms: ["mldsa65"]`,
   `allowed_issuers: ["issuing"]`. Do not try to add `mldsa65` to an existing
   classical profile: that is a widening and is refused.

6. **Issue, then validate with a second implementation.**

   ```sh
   openssl verify -CAfile root.pem -untrusted issuing.pem \
       -purpose sslserver -verify_hostname web.svc.example.com leaf.pem
   openssl crl -in crl.pem -CAfile issuing.pem -noout -verify
   openssl verify -crl_check -CAfile root.pem -untrusted issuing.pem \
       -CRLfile crl.pem leaf.pem        # a revoked leaf fails with "certificate revoked"
   ```

### Importing an existing ML-DSA intermediate (HOLD)

Hikyo accepts only the **seed-only** PKCS#8 form. Produce it explicitly; the
OpenSSL default is refused:

```sh
openssl genpkey -algorithm ML-DSA-65 -provparam ml-dsa.output_formats=seed-only -out issuing.key
```

An existing key in another form needs re-encoding by its owner from the seed. If
only the expanded form exists, it cannot be imported. Hikyo does not convert it.

## Relying-party matrix

Exact versions; results observed by the tests named in the handoff. Nothing is
inferred for a stack not listed.

| Relying party | Version | Result |
| --- | --- | --- |
| Go `crypto/tls` + `crypto/x509`, client and server, mutual TLS | Go 1.27.3 darwin/arm64 | Chain of three ML-DSA-65 certificates verified on both sides; client offered `MLDSA65` (0x0905); leaf keys are ML-DSA-65 so CertificateVerify can only be ML-DSA-65 |
| Same, negotiated key exchange (separate evidence) | Go 1.27.3, `tlspolicy` strict | `X25519MLKEM768` on TLS 1.3 on both sides. The same ML-DSA chain also works over `X25519`, so chain and group are independent |
| OpenSSL | 4.0.3 (Library 4.0.3, 29 Sep 2026) | Signs the Hikyo CSR, validates leaf chain, hostname and purpose, rejects a tampered leaf, verifies the ML-DSA CRL and honours a revocation; a Hikyo parse of an OpenSSL-made ML-DSA CSR succeeds |
| Go built with the FIPS 140-3 module v1.0.0 (no ML-DSA) | Go 1.27.3, `GOFIPS140=v1.0.0` | Refuses: `remote error: tls: handshake failure`, no application byte. The same program built normally accepts |
| Client trusting only a classical root | Go 1.27.3 | `x509: certificate signed by unknown authority`, no application byte |
| Peer that requires a PQ-authenticated chain (`VerifyConnection`) | Go 1.27.3 | Refuses an ECDSA chain even over the hybrid group, before any application byte |
| Browsers, OpenSSH, Java, NSS, BoringSSL, Windows and macOS platform verifiers | none tested | **Unqualified.** Do not assume support. Owned by #1134; aggregated by #1135 |

E-18 stays registered: one listener certificate pair serves every client, so a
strict ML-DSA leaf there may be rejected by browsers and compatibility clients.
Do not put an ML-DSA leaf on the public listener without a second pair or a
terminator that does.

## Sizes and bounds

Measured with `TestMLDSASizeBaseline` (bytes; the leaf and CSR have one DNS name):

| Object | ecdsa-p256 | mldsa65 | Request cap |
| --- | --- | --- | --- |
| Root certificate DER | 369 | 5494 | |
| Intermediate DER | 404 | 5530 | |
| Leaf DER | 415 | 5540 | |
| Leaf certificate PEM | 619 | 7558 | 65536 |
| Leaf CSR PEM | 371 | 7310 | 65536 |
| Three-certificate chain PEM | 1776 | 22601 | 262144 |
| Generated leaf key PEM | 241 | 128 | 16384 (issuer key import) |
| CRL DER, 0 / 100 / 1000 entries | 210 / 5117 / 49219 | 3454 / 8360 / 52454 | none |
| TLS handshake, server to client, chain of two certificates | 2273 | 15760 | |

The D9 estimate (the existing request caps suffice) holds with room: the
certificate and CSR fields use 12% of their cap, the chain 9%, and a 262144
character chain field would admit about 34 ML-DSA-65 certificates. No cap was
raised and no admission was weakened. A CRL has no size bound in the API; ML-DSA
adds a constant of about 3.2 KB and nothing per entry.

## Cost baseline (DEC-17, no threshold proposed)

See the handoff for the machine, Go version and the full table. On the
measurement host an ML-DSA-65 leaf signature costs roughly six to seven times an ECDSA
P-256 one, chain verification about twice, a CRL signature about five times at
zero entries and about 1.2 times at a thousand entries. Thresholds belong to the
owner and need amd64 numbers that were not measured.

## Reader-first deployment, trust distribution and rollback

1. **Readers first.** Every node, relying party and CRL consumer must verify
   ML-DSA-65 before an ML-DSA issuer exists. From the code, a node on an older Hikyo
   build is expected to treat `key_algorithm = 'mldsa65'` as an unknown algorithm
   and refuse renewals of such leaves; an older binary against a database holding
   an ML-DSA issuer was **not rehearsed** (NOTRUN). Roll every node forward
   before enabling.
2. **Distribute the root.** Install `root.pem` into each relying party's trust
   store through your separate trust channel and record the receipt before
   activating the issuer. An online evaluation root is not a production root.
3. **Keep classical identities.** Existing ECDSA, RSA and Ed25519 issuers, leaves
   and CRLs are untouched and keep working. Migrate leaf by leaf by issuing a new
   ML-DSA certificate under a new profile; renewal never changes an algorithm.
4. **Overlap.** `rotate` appends version N+1 of the same family, activates it and
   moves N to `retiring`; N keeps signing its CRL until `retire` is allowed.
5. **Switching the algorithm off.** With the instance gate off, existing ML-DSA
   issuers keep signing and publishing their CRLs (tested), but new and renewed
   ML-DSA leaves, rotation and new profiles are refused.
6. **Roll back by stopping, not by deleting.** Revoke or let expire the ML-DSA
   leaves, keep the issuer versions and published CRLs until every leaf has
   expired, and never destroy a key to make a downgrade work. Once an ML-DSA
   issuer exists, running an older Hikyo build is not supported.

What must be retained, and who owns it: the offline root key (the operator, offline,
for as long as any chain under it is valid), the root certificate (every relying
party), each issuer version's last CRL (until its leaves expire), the sealed issuer
keys and their backups (Hikyo's key hierarchy; restore sets the issuer hold and
stored CRLs stay readable but stale until the hold is released).

## Not in scope

No public CA, ACME, OCSP, HSM or KMIP product, no SSH certificate signature
migration, no hybrid or cross-signed chains, and no production root activation.
