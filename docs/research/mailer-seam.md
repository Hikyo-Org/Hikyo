# The mailer seam — primary-source research

**Date:** 2026-09-02
**Context:** Wayfinder map [#578](https://github.com/Hikyo-Org/Hikyo/issues/578) (social sign-in and open registration), ticket [#583](https://github.com/Hikyo-Org/Hikyo/issues/583). No mail code exists in `internal/` today. Open registration admits local email + password sign-up ([#579](https://github.com/Hikyo-Org/Hikyo/issues/579) decision 5), which needs a verification mail, and #579 decision 6 delegates one definition here by name: *"mailer configured when the local entry is enabled (definition from the mailer research)"*. This document records what the transports, libraries, and comparable products *do*, what the locked ADRs already say about mail and outbound connections, and hands the decisions to the grilling tickets it feeds (§8). It decides nothing.
**Method:** Primary sources only: package documentation and source on pkg.go.dev and GitHub at the versions named, GitHub's release API for dates, the RFCs, each provider's own SMTP and pricing pages, the Gitea/Forgejo/Grafana/Infisical configuration references and source, Mailpit's documentation and Swagger definition, and Hikyo's own ADRs, config loader, CI scripts, and migrations. Every claim carries its source in §9. Where a source is silent, the row says "not documented".

**Checklist this document answers** (the ticket's six bullets):

| # | Item | Where |
|---|---|---|
| 1 | Transport options: SMTP variants, provider HTTP APIs, cost to a homelab operator, SMTP-first verdict | §2 |
| 2 | Go libraries: `net/smtp` (frozen), `wneessen/go-mail`, `emersion/go-smtp`; maintenance, TLS defaults, DKIM | §3 |
| 3 | Templating and localisation in Gitea/Forgejo, Infisical, Grafana; plain + HTML or plain only | §4 |
| 4 | Failure semantics: synchronous send vs outbox, both engines, what fails closed | §5 |
| 5 | Testing: local capture, e2e assertions without a live relay | §6 |
| 6 | Config surface, secrets handling, the "mailer configured" precondition | §7 |

---

## 1. Executive summary

- **SMTP submission over TLS covers every transport a self-hoster or a hosted deployment would use, including all three named providers.** Resend, Postmark and Amazon SES each publish an SMTP relay (§2.2) beside their HTTP API, so an HTTP-provider seam buys nothing a homelab operator cannot already reach with host, port, user, password. The seam is one SMTP client with one message shape; a provider abstraction is speculative.
- **`net/smtp` is frozen and thin**: two SASL mechanisms (PLAIN, CRAM-MD5), STARTTLS only through `Client.StartTLS`, no implicit-TLS `SendMail`, no message builder, no context. Gitea uses it and consequently hand-rolls LOGIN auth, its own MIME, and ships **no TLS `MinVersion` and no timeouts** (§3.1, §3.4). `wneessen/go-mail` v0.8.1 (released 2026-07-09, MIT, `go 1.25`, dependencies `x/crypto` + `x/text` only, both already in Hikyo's module graph) ships secure defaults that match RFC 8314: TLS mandatory by default, TLS 1.2 floor, `ServerName` = host, implicit TLS on 465 via `WithSSL`, PLAIN/LOGIN refused on a cleartext non-localhost connection, a 15 s default timeout, `DialAndSendWithContext`, a replaceable dial function (`WithDialContextFunc`) that accepts Hikyo's `netpolicy.PublicDialer`, `html/template` + `text/template` bodies with a real plain-text alternative, and a CVE handled by a fix release (§3.2). It meets two of mvp-boundary §2.2's three "known library" criteria outright; the third (independent audit or named production use) is **not established** by this research (§3.5).
- **`emersion/go-smtp` v0.25.0 (2026-08-13) is the right *server* for a test sink**, not the client: its `Backend`/`Session` interfaces are a few dozen lines to a capture server, while its client has no message builder (§3.3, §6.2).
- **Comparable OSS control planes send HTML with a derived or separate text part, from Go templates, English-only or via their web i18n bundle**: Gitea/Forgejo render `html/template` bodies under `custom/templates/mail/…`, split subject/body with a `---` line, derive text by stripping HTML, and localise through `.locale.Tr` keys from the web locale files; Grafana loads `emails/*.html, emails/*.txt` and prefers `text/html`; Infisical is env-configured and disables every mail-dependent feature when SMTP is absent. Hikyo's web UI has **no i18n framework** (only `toLocaleString` date calls), so an English-only stdlib-template mailer with an explicit text alternative matches the codebase (§4).
- **The locked architecture forbids a generic job framework and admits domain outboxes only by amendment**: system-architecture named the adapter outbox as *"the single deliberate exception"* and was amended on 2026-09-02 to add a second (`dynamic_effects`). A mail outbox would be a **third**, and an outbox row would carry a verification link — a credential — at rest. Gitea and Grafana both dispatch mail asynchronously and **neither retries** a failed send (§5.2). Human-auth already fixes the frame: *"Email is never on the critical path of any recovery flow; where configured, it is an alternative transport for an artifact that already exists"*, and ops-spec lists *"SMTP transport off by default"* (§5.1). The research therefore leans **synchronous send, no outbox**, and surfaces the one tension #584 must resolve: system-architecture's *"no external effect may escape before commit"* rule means the send runs **after** the pending sign-up row commits, so a failed send must still yield the uniform, enumeration-safe response with an audited cause (§5.3).
- **Boot may validate, boot may not dial.** CI invariant 4 (`scripts/ci/no-egress.sh`) straces a boot-plus-idle `hikyo server` and fails on any non-loopback `connect`. A mailer therefore validates its configuration statically at boot and probes only on an operator-triggered test send or a real send (§7.4).
- **The mailer is a new enumerated outbound class under the threat model**, exactly as multi-instance added "configured remote Hikyo instances" by amendment with a control set. The SMTP analogue is sketched in §7.5 for the synthesis ticket: explicit `host:port`, TLS mandatory (implicit 465 or STARTTLS 587, never opportunistic), TLS 1.2 floor and hostname verification, `PublicDialer` address policy with private ranges permitted only by explicit CIDR list (a LAN relay is the homelab case multi-instance already names), per-send deadline, zero-configured = zero outbound.
- **No varlock exists in this repository** despite the ticket's phrasing. Configuration is `HIKYO_*` environment variables read in `internal/config/config.go`, with a file tier above the env tier for secrets (`--root-key-file` / `HIKYO_ROOT_KEY` "documented weakest tier"), and a test (`TestKnownEnvCoversEveryGetenv`) that fails when a new variable is not registered in the known-env map (§7.1). The live alternative — a DB row sealed under the instance DEK and edited from the WebUI, as OIDC provider client secrets are — is laid out beside it with verdicts (§7.2); the choice is #584's.
- **The "mailer configured" predicate** #579 asked for is proposed in §7.3 as a static, boot-safe, testable predicate over configuration plus the explicit public origin, with the live-send outcome feeding inactive-with-cause at use time rather than the predicate itself.

---

## 2. Transport options

### 2.1 SMTP submission

| Mode | Port | RFC | Notes |
|---|---|---|---|
| Implicit TLS ("SMTPS", "TLS Wrapper") | 465 | RFC 8314 §3.3 | TLS from the first byte; RFC 8314 recommends implicit TLS for submission and says clients and servers SHOULD implement both 465 and 587 "for this transition period". |
| STARTTLS | 587 (also 25, provider-specific 2587) | RFC 3207, RFC 6409 | Cleartext EHLO, then `STARTTLS`; a client that *requires* TLS must fail when the extension is absent rather than continue. RFC 8314: a session whose server certificate cannot be validated MUST NOT be treated as meeting the confidentiality requirement. |
| Cleartext | 25 | — | Never for authenticated submission. `net/smtp`'s own `PlainAuth` refuses to send credentials unless the connection is TLS or the peer is localhost; go-mail inherits the same rule (`ErrUnencrypted`). |
| Unix socket / `sendmail` binary | — | — | Gitea/Forgejo offer `smtp+unix` and `sendmail` protocols. Neither fits a single static binary in a container; recorded for completeness. |

RFC 8314 §4 requires TLS 1.2 or later on servers and clients and RFC 7817-style server identity verification (hostname match, chain validation). Every relay in §2.2 accepts TLS 1.2+.

Authentication mechanisms seen across the relays in §2.2: PLAIN and LOGIN everywhere; CRAM-MD5 at Postmark; SCRAM/XOAUTH2 at some providers not in scope. PLAIN over TLS is the common denominator.

### 2.2 Provider HTTP APIs, and the SMTP relays they also expose

| Provider | HTTP API | SMTP relay | Ports | Auth | Free tier | First paid tier |
|---|---|---|---|---|---|---|
| Resend | yes (SDKs; not evaluated) | `smtp.resend.com` | implicit 465/2465; STARTTLS 25/587/2587 | user `resend`, password = API key | 3,000/month, 100/day, 3 domains | Pro $20/month, 50,000/month |
| Postmark | yes | `smtp.postmarkapp.com` (transactional) | 25/2525/587, **STARTTLS only** | Server API token as user+password (+ `X-PM-Message-Stream` header), or per-stream SMTP access/secret key; CRAM-MD5, DIGEST-MD5, PLAIN, LOGIN | 100/month developer tier, no expiry | Basic $15/month, 10,000/month |
| Amazon SES | yes (`aws-sdk-go-v2/service/sesv2`) | `email-smtp.<region>.amazonaws.com` | STARTTLS 25/587/2587; TLS Wrapper 465/2465; **TLS required, no cleartext** | SMTP credentials **distinct from IAM access keys**, per region | $200 credits for new accounts, 6 months; no permanent free tier | $0.10 per 1,000 à la carte |
| Own relay (Postfix or similar on the LAN, or the operator's mailbox provider's submission port) | — | operator's | 465 or 587 | operator's | 0 | 0 |

Facts that bear on the seam:

- **Every provider in scope can be reached over authenticated SMTP submission with TLS.** Resend documents that its SMTP path shares the API's rate limit and has no server-side logs; Postmark's SMTP path lacks implicit TLS (STARTTLS on 587 is its TLS form); SES requires TLS on every connection. None of these limitations affects a verification-mail workload.
- **An HTTP seam would cost a new direct dependency per provider.** `aws-sdk-go-v2` is in Hikyo's module graph only as an indirect dependency of `sops`; `service/sesv2` is not present and would be a new direct requirement. Resend and Postmark SDKs are not in the graph at all.
- **DKIM/SPF/DMARC are the relay's job on this path.** Authenticated submission hands the message to a relay that signs it under the operator's domain (this is how all three providers and a Postfix relay work). A client-side DKIM signer is only needed when Hikyo *is* the MTA delivering directly to recipient MXs, which no option here involves (§3.2 notes that go-mail has native DKIM regardless).
- **Verdict (research lean, not decision):** SMTP-first, and SMTP-only in v1. The "provider seam" the ticket asked to price is not genuinely cheap: it is a second client, a second config surface, a second failure vocabulary, and per-provider dependencies, for zero reach a homelab operator does not already have through the relay column. The only thing an HTTP API adds is structured bounce/complaint feedback, which no ticket on the map needs.

---

## 3. Go libraries

### 3.1 `net/smtp` (standard library)

- Package doc: *"The smtp package is frozen and is not accepting new features. Some external packages provide more functionality."*
- Mechanisms: `PlainAuth` (RFC 4616) and `CRAMMD5Auth` (RFC 2195) only. No LOGIN, no SCRAM, no XOAUTH2.
- `PlainAuth` *"will only send the credentials if the connection is using TLS or is connected to localhost. Otherwise authentication will fail with an error, without sending the credentials."*
- TLS: `Client.StartTLS(*tls.Config)` only. `SendMail` does not do implicit TLS; a 465 connection needs a manual `tls.Dial` + `NewClient`.
- No `context.Context`, no deadlines, no message construction (`net/mail` is parse-only: it "follows the syntax as specified by RFC 5322", `Address.String()` RFC 2047-encodes non-ASCII names, and offers no serialiser). MIME multipart bodies would be assembled by hand from `mime/multipart` + `mime/quotedprintable`.
- **What using it looks like in practice — Gitea** (`services/mailer/sender/smtp.go`): dials plain / `tls.Dial` for `smtps` / `StartTLS` post-HELO, builds `tls.Config{ServerName, InsecureSkipVerify: ForceTrustServerCert}` with **no `MinVersion`**, selects CRAM-MD5 → PLAIN → a **hand-written LOGIN** → NTLM from the server's `AUTH` extension, does **not** refuse auth over cleartext itself, and sets **no timeouts** on connect, commands, or TLS handshake. That is the surface Hikyo would have to own and keep correct if it chose the stdlib.

### 3.2 `github.com/wneessen/go-mail`

Verified through GitHub's API on 2026-09-02:

| Fact | Value |
|---|---|
| Latest release | **v0.8.1**, published **2026-07-09** (GitHub releases API); last commit on `main` 2026-07-19 |
| Previous releases | v0.8.0 "Native NTLM and DKIM support" (2026-07-04); v0.7.3 (2026-05-12) |
| License | MIT |
| `go.mod` | `go 1.25.0`; requires only `golang.org/x/crypto v0.54.0` and `golang.org/x/text v0.40.0` (Hikyo already has `x/crypto v0.55.0` direct and `x/text v0.41.0` indirect) |
| SMTP implementation | its own `smtp` package, *"originally forked from Go's standard library `net/smtp` package and has since been extended"*; the module does not import `net/smtp` |
| Security response | v0.7.1 fixed **CVE-2025-59937**: *"insufficient address encoding when passing mail addresses to the SMTP client, which could lead to possible wrong address routing or even to ESMTP parameter smuggling"* |
| Go support policy | from v0.7.0, aligned with the official Go release policy (actively maintained versions only) |

Defaults and options relevant to the seam (package docs and `client.go`):

- `DefaultTLSPolicy = TLSMandatory`. The three policies are `TLSMandatory` (refuse if STARTTLS is not offered: *"STARTTLS mode set to: %q, but target host does not support STARTTLS"*), `TLSOpportunistic` (continue unencrypted if unavailable), `NoTLS`.
- `NewClient` builds `tls.Config{ServerName: host, MinVersion: DefaultTLSMinVersion}` with `DefaultTLSMinVersion = tls.VersionTLS12`. `WithTLSConfig` replaces it (custom CA bundle for a private relay).
- `WithSSL()` / `WithSSLPort(fallback bool)` for implicit TLS on 465; `DefaultPort = 25`, `DefaultPortSSL = 465`, `DefaultPortTLS = 587`.
- `WithDialContextFunc(fn)` *"overrides the default DialContext function used by the Client when establishing a connection"* — i.e. the whole dialer is replaceable, so `netpolicy.PublicDialer.DialContext` can be injected unchanged (§7.5).
- `WithSMTPAuth` with PLAIN, LOGIN, CRAM-MD5, NTLM, SCRAM-SHA-1/256 (+PLUS), XOAUTH2, and auto-discovery. `smtp.PlainAuth(identity, username, password, host, allowUnenc bool)`: with `allowUnenc == false` the `Start` method returns `ErrUnencrypted` when `!server.TLS && !isLocalhost(server.Name)` — the stdlib rule, kept.
- `DefaultTimeout = 15 * time.Second`, applied per connection; `DialAndSendWithContext(ctx, msgs...)` for caller-owned deadlines.
- `Msg.SetBodyTextTemplate` / `SetBodyHTMLTemplate` take `text/template` / `html/template` values; `AddAlternativeString` builds `multipart/alternative` with a **real** text part rather than a stripped HTML derivative.
- Native DKIM signing since v0.8.0 (imports `internal/dkim`; RSA/ECDSA/Ed25519 via `crypto/*`). Unneeded on the authenticated-submission path (§2.2) but present.

### 3.3 `github.com/emersion/go-smtp` (and `go-msgauth`)

- **v0.25.0, published 2026-08-13** (GitHub releases API); last commit 2026-08-18. MIT. Pre-v1 (`v0.x`) — API may still move.
- ESMTP client **and server**: RFC 5321 plus AUTH (via `emersion/go-sasl`), STARTTLS, DSN, SMTPUTF8, LMTP. Client: `Dial`, `DialTLS`, `DialStartTLS`, `SendMail`. Server: implement `Backend` (session factory) and `Session` (`Mail`, `Rcpt`, `Data`, `Reset`, `Logout`), then `NewServer(backend).ListenAndServe()` / `ListenAndServeTLS()`.
- No message builder (that is `emersion/go-message`, a further dependency). As a *client* it is therefore a step behind go-mail; as a **server** it is the smallest route to an in-repo SMTP capture sink (§6.2).
- `github.com/emersion/go-msgauth/dkim` v0.7.0 (2025-04-20): `Sign(w io.Writer, r io.Reader, *SignOptions{Domain, Selector, Signer, Hash, HeaderKeys, …})`. Recorded because the ticket asked about DKIM expectations; not needed on the authenticated-submission path.

### 3.4 Comparison

| | `net/smtp` | `wneessen/go-mail` | `emersion/go-smtp` |
|---|---|---|---|
| Maintained | frozen | release 2026-07-09 | release 2026-08-13 |
| Implicit TLS 465 | manual `tls.Dial` | `WithSSL` | `DialTLS` |
| STARTTLS mandatory by default | no (caller policy) | **yes** | caller policy |
| TLS 1.2 floor by default | no | **yes** | no (caller `tls.Config`) |
| Refuses PLAIN/LOGIN on cleartext | PLAIN yes; no LOGIN | **yes** (both) | via go-sasl / caller |
| LOGIN / SCRAM / XOAUTH2 | no | yes | via go-sasl |
| Context + timeout | no | **yes** (15 s default) | partial |
| Replaceable dialer for `netpolicy` | manual (`NewClient(conn, host)`) | `WithDialContextFunc` | manual |
| Message builder, templates, text+HTML alternative | no | **yes** | no (go-message) |
| Server for a test sink | no | no | **yes** |
| Extra dependencies | none | `x/crypto`, `x/text` (both present) | `go-sasl` (+ `go-message` for building) |

**Research lean:** go-mail as the client; go-smtp (server half) or Mailpit for the sink (§6). The stdlib is not "no dependency" in practice — it is the Gitea surface from §3.1 written and maintained by Hikyo, which human-auth's *"no hand-rolled primitive"* stance argues against wherever a maintained library exists.

### 3.5 Against mvp-boundary §2.2's "known library means proven" bar

The bar (three criteria, all required, set for SAML libraries and applied by human-auth's SAML amendment): (1) a tagged release or substantive commit in the last 12 months; (2) at least one published advisory handled with a fix release; (3) a published independent security review **or** named production use by ≥ 2 identifiable organisations in public engineering documentation or vendored inclusion.

| Criterion | go-mail | go-smtp |
|---|---|---|
| (1) activity ≤ 12 months | **met** — v0.8.1 2026-07-09 | **met** — v0.25.0 2026-08-13 |
| (2) advisory + fix release | **met** — CVE-2025-59937 fixed in v0.7.1 | not established by this research |
| (3) audit or ≥ 2 named production users | **not established** by this research (README, project site and wiki list no adopters) | not established (used as a server by Mailpit? — not verified) |

Whether the mail transport is a "security-sensitive library" in the sense that bar was written for (it does not verify tokens, parse foreign assertions, or hold long-lived trust material; it opens an outbound TLS connection and writes a message) is a question for #584/#589, not this document. If the bar applies, criterion 3 needs evidence this research did not find, or the fallback ladder applies.

---

## 4. Templating and localisation in comparable OSS control planes

| Product | Body format | Template engine | Text part | Localisation | Where templates live |
|---|---|---|---|---|---|
| Gitea / Forgejo | HTML (`SEND_AS_PLAIN_TEXT=false` default) | Go `html/template` for body, `text/template` for subject; optional subject/body split by a line of ≥ 3 dashes | *"obtained by stripping the HTML markup"* (`multipart/alternative`) | `{{.locale.Tr "mail.activate_account.text_1" …}}` keys from the web locale files; the auth templates (`templates/mail/user/auth/{activate,activate_email,register_notify,reset_passwd}.tmpl`) are HTML-only documents with `<!DOCTYPE html>`, UTF-8 meta, and `.DisplayName`, `.Code`, `.ActiveCodeLives`, `AppUrl`, `AppName` | `templates/mail/…`; overridable under `custom/templates/mail/…`; loaded at start-up (restart to change) |
| Grafana | HTML preferred; `content_types = text/html` default, `text/plain` optional, order = preference | Go templates with Sprig; `Subject()` inline; `__dangerouslyInjectHTML` escape hatch | separate `.txt` templates when listed in `templates_pattern` (`emails/*.html, emails/*.txt`) | none at the mail layer | `public/emails/`; `[smtp.static_headers]` for fixed headers |
| Infisical | HTML (not documented further) | not documented | not documented | not documented | in-binary; no override documented |

Facts that bear on Hikyo:

- **Hikyo's web UI has no i18n framework.** The only "locale" hits under `web/src` are `Date.toLocaleString()` / `toLocaleDateString()` calls. An English-only mailer matches the product; a locale bundle for mail alone would be the first localisation surface in the codebase.
- **`html/template` is the stdlib's contextual auto-escaper** (*"implements data-driven templates for generating HTML output safe against code injection … understands HTML, CSS, JavaScript, and URIs"*, wraps `text/template`). Display names and org names in a verification mail are attacker-influenced strings; the auto-escaper covers the HTML part, and the text part needs no escaping beyond what `text/template` does (none — text is text).
- **Text-first is the cheaper and safer floor.** A plain-text verification mail is one `text/template`, has no rendering matrix, survives every client, and cannot smuggle markup. Gitea's stripped-HTML text part is a derivative, not an authored part; go-mail lets both parts be authored (§3.2). Whether v1 ships text-only or text + HTML is #584/#587's call; nothing on the map needs HTML.
- **Templates embedded (`embed.FS`), not operator-overridable, is the shape the rest of Hikyo takes** (the web UI is embedded into the single binary, system-architecture). Gitea's `custom/templates` override is a feature a control plane with a brand story wants; it is not a v1 need here.
- **Link, not code.** All three products send a link carrying a single-use token; Gitea also renders the code for copy-paste (`mail.link_not_working_do_paste`). Hikyo's existing display-once establish surface (`/establish` with a pasted authority) is that fallback already.

---

## 5. Failure semantics

### 5.1 What the locked ADRs already say about mail

- human-auth § *Recovery*: *"Self-hosted installations frequently have no SMTP. **Email is never on the critical path of any recovery flow**; where configured, it is an alternative transport for an artifact that already exists, never a different mechanism."* The administrator-issued reset token is *"displayed once, transmitted out of band. If SMTP is configured it may be mailed instead — same token, different transport."* *Rejected: SMS or email as a factor* — *"Neither is available in an install with no guaranteed mail server."*
- ops-spec composable-maxima table: *"Expiry warnings … in-product first (locked); **SMTP transport off by default**."*
- system-architecture § *Jobs*: *"**no generic framework; one domain-specific outbox**"* — the adapter outbox is *"the single deliberate exception"*, justified by the threat model's INTENT/OUTCOME/retry-with-dedup requirements for adapter pushes. **Amended 2026-09-02 (#147):** dynamic secrets add *"a **second** domain-specific outbox … It is **not** a generalization of the first into a shared framework — the two stay separate domain outboxes"*. *Rejected:* a generic DB-backed job framework; river/gocraft (postgres-only or unmaintained).
- system-architecture § *Transactions*: *"no external effect (adapter push, SSE emit, response write) may escape before commit; effects are emitted after successful commit, or through the outbox … which is itself committed state."*
- multi-instance § *Freshness*: *"There is no background poller … a directory poller would re-open that [the no-job-framework decision]"*.
- threat-model: *"No plaintext in caches, logs, audit records, SSE events, or error strings"*; single-use tokens are *"high-entropy (≥128-bit random minimum), hashed AND expiring"*.
- #579 decision 6: preconditions run at write time and use time, never at boot; use-time failure = uniform refusal + audited cause; the policy row stays and renders `inactive` with cause; nothing auto-deletes.

### 5.2 Synchronous send vs outbox — what comparable products do, and what each costs here

| | Synchronous send inside the request | Outbox table + retry worker |
|---|---|---|
| Gitea | — | `mailQueue` worker-pool queue; failures **logged, not retried** (`services/mailer/mailer.go`) |
| Grafana | `SendEmailCommandSync` | `SendEmailCommand` → buffered channel (cap 10) drained by `Run()`; failures **logged, not retried**; in-memory, lost on restart |
| Locked-ADR cost in Hikyo | none | a **third** domain outbox by system-architecture amendment; `no generic framework` argument to overcome; both engines (`adapter_outbox` shape: `attempt_count`, `next_attempt_at`, `lease_owner`, `lease_expires_at`, `state ∈ {queued, running, succeeded, failed, superseded}`, `FOR UPDATE SKIP LOCKED` on postgres, partial-unique dedup index on sqlite) |
| HA | any node sends; no coordination | needs the lease/claim discipline the adapter outbox already implements (or the scheduler's singleton lease: hourly tick, 10-minute job deadline — far too slow for a verification mail) |
| Data at rest | nothing | the queued message **contains the verification link** — a credential. Either the row is sealed under the instance DEK (`reencrypt` set grows) or the row stores only the token id and the body is rendered at send time |
| Latency | request waits one SMTP round trip (go-mail default deadline 15 s; a per-send deadline is a composable maximum) | request returns immediately |
| Failure visibility | immediate; audited on the request; can drive inactive-with-cause in the same transaction context | delayed; needs a health row + prune, like the DR health row (#145) |
| Fail-closed story | sign-up refused by name when the send fails | sign-up "accepted" with a mail that may never arrive |

**Research lean:** synchronous send with no retry beyond the SMTP client's own connection attempts. It is what the locked frame ("alternative transport for an artifact that already exists") implies, what the two comparators effectively do (their queues do not retry either), and it avoids a third outbox amendment and a credential-at-rest problem. The user's retry is "send me the mail again", under the §179 `signup` budget #579 named.

### 5.3 What fails closed, per surface

| Surface | Mailer absent or precondition failed | Send attempted and failed |
|---|---|---|
| Local email + password sign-up (#584) | **Refused**: policy's local entry is `inactive: mailer-unconfigured` (#579 decision 6); login page shows the local sign-up entry as inactive with cause | Refused by name, audited `registration.signup_refused` cause `precondition` (or a mail-specific cause #584 names); the pending row, if one was committed, expires by its own clock |
| Social/OIDC sign-up | unaffected — no mail in the ceremony | — |
| Local invitation (#568) | display-once stays the transport — no change | if a mailed invite is ever added: display-once still shown, mail is additive (fog: *Other mailer consumers*) |
| Admin credential reset (human-auth) | display-once — already designed as mail-optional | same |
| Expiry warnings (ops-spec) | in-product — mail off by default | same |
| Boot | **unaffected**: static config validation only; no dial (§7.4) | n/a |

**The open question for #584** (research surfaces, does not resolve): with synchronous send *after* commit (system-architecture's rule), a sign-up whose mail fails has already created its pending row. Two consequences need a decision there: (a) the response must still be **uniform** with the "email already registered" path — human-auth § *Enumeration resistance* requires a *"bounded dummy … path so timing is comparable"*, and an SMTP round trip on one branch and none on the other is exactly a timing oracle; (b) the audited cause must not leak the address's existence. The comparators do not solve this: Gitea's `REGISTER_EMAIL_CONFIRM` simply requires the mailer to be enabled and reports failures in the log.

---

## 6. Testing

### 6.1 Mailpit (capture sink with UI and API)

- *"A small, fast, low memory, zero-dependency, multi-platform email testing tool & API"*, written in Go, MIT; latest release **v1.31.0 (2026-08-22)**. Single static binary and multi-arch Docker image `axllent/mailpit` (arm64 included — relevant to the Pi-4 floor CI runs on).
- Defaults: SMTP `0.0.0.0:1025`, HTTP UI + API `0.0.0.0:8025`; keeps the most recent 500 messages (`--max`).
- SMTP options relevant to matching Hikyo's mandatory-TLS policy: `--smtp-tls-cert` / `--smtp-tls-key` (enables STARTTLS), `--smtp-require-starttls`, `--smtp-require-tls` (implicit), `--smtp-auth-accept-any`, `--smtp-auth-allow-insecure`, `--smtp-auth-file`, `--smtp-disable-rdns`.
- REST API (Swagger 2.0 at `server/ui/api/v1/swagger.json`):
  - `GET /api/v1/messages?start=&limit=` — list
  - `GET /api/v1/search?query=to:addr@example.test subject:"…"` — search (`to:`, `subject:`, `tag:`, `before:`, `after:`)
  - `GET /api/v1/message/{ID}` — summary with `Text` and `HTML` bodies (`{ID}` may be `latest`)
  - `GET /api/v1/message/{ID}/raw` — RFC 5322 source
  - `DELETE /api/v1/messages` with `{"IDs": []}` — delete all; `DELETE /api/v1/search?query=` — delete by search
  - `GET /api/v1/info` — version and totals

  An e2e assertion is therefore: search by recipient → fetch `latest` → regex the establish link out of `Text` → drive the browser to it → delete all before the next spec.

### 6.2 In-repo Go sink (the `oidctest` precedent)

- Hikyo already ships a test double for an external protocol party as Go code: `internal/oidctest` (IdP + federation) with a `cmd/` main that the e2e fixture builds on demand (`go build -o oidctest-idp-<port> ./internal/oidctest/cmd`, `web/e2e/fixtures/instance.ts`). Go isolation tests use the same package in-process.
- The same shape for mail is `emersion/go-smtp`'s server: a `Backend` returning a `Session` whose `Data` reads the message into memory, plus a few-line HTTP JSON listing for the e2e fixture. `ListenAndServeTLS` with a generated self-signed certificate gives STARTTLS so the **release-shaped app under test keeps its mandatory-TLS policy** (CI builds *"the release-shaped app once for browser legs plus ui-tagged/no-egress"*, `.github/workflows/ci.yml`).
- Trade: one more `internal/*test` package to maintain, versus one more CI service container (CI already runs `postgres:18` as a `services:` container for the postgres legs) and a Docker-only dependency for local dev. Parallel e2e sessions claim port blocks (`HIKYO_E2E_PORT*`); a sink port joins that block either way.

### 6.3 The TLS gap between production policy and a loopback sink

Production policy (§7.5) is TLS-mandatory with hostname verification. A capture sink on loopback either (a) presents a certificate — Mailpit `--smtp-tls-cert`, or go-smtp `ListenAndServeTLS`/STARTTLS with a generated cert — and Hikyo is pointed at it through a **CA-file knob** the production config needs anyway for private relays; or (b) Hikyo carries a **dev-only cleartext allowance** for loopback (`cfg.Dev` gates eight other dev-only behaviours today, e.g. `HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE`), which is the stdlib's own precedent (`PlainAuth` sends credentials to localhost without TLS). Option (a) exercises the shipped path end to end and needs no policy branch in production code; option (b) is less setup. Research lean: (a); the choice is #584's, and #587's prototype should assume whichever it picks.

### 6.4 Unit tests without any network

go-mail's client dials through `WithDialContextFunc`; a unit test injects an in-memory `net.Pipe` or an in-process go-smtp server. The mailer interface Hikyo exposes to the service layer (one method: send this rendered message to this recipient under this deadline) is trivially faked for service tests — the same seam the sign-up service uses to record "mail sent" for audit.

---

## 7. Configuration, secrets, and the "mailer configured" precondition

### 7.1 The actual config convention (no varlock)

- `rg -il varlock .` returns nothing outside `node_modules`. Configuration is `HIKYO_*` environment variables parsed in `internal/config/config.go` (`getenv` + `durationEnv` + validation), flags for a few (`--root-key-file`).
- **Known-env gate:** `TestKnownEnvCoversEveryGetenv` (`internal/config/knownenv_test.go`) parses every non-test `.go` file under `internal/` and `cmd/` and fails if a `Getenv` key is missing from the `knownEnv` map. Every new mailer variable must be registered there or CI goes red.
- **Secret tiers already in force:** file above env. `--root-key-file` *"also covers systemd LoadCredential paths"*; `HIKYO_ROOT_KEY` is *"documented weakest tier"*; the two together are refused (*"configure exactly one root-key source"*). `HIKYO_TLS_CERT_FILE` / `HIKYO_TLS_KEY_FILE` are file paths. Grafana's equivalent is `$__file{/etc/secrets/password}` expansion in `[smtp] password`; Infisical is env-only (`SMTP_PASSWORD`).
- **Validation is fail-fast and named:** e.g. `HIKYO_EXTERNAL_ORIGIN must be an exact canonical HTTP(S) origin without credentials, path, query, or fragment`; `HIKYO_BACKUP_INTERVAL: %s is below the %s minimum`. Grafana likewise refuses to start the notification service on an invalid `from_address`.
- **Egress policy files are JSON maps of CIDR lists** (`HIKYO_ADAPTER_EGRESS_POLICY_FILE`, `HIKYO_DYNAMIC_EGRESS_POLICY_FILE` → `map[string][]netip.Prefix`), consumed by `netpolicy.PublicDialer.AllowedCIDRs`.

Shape the comparators converge on (Gitea `[mailer]`, Grafana `[smtp]`, Infisical `SMTP_*`), translated to the `HIKYO_*` convention as **facts about what a complete surface contains**, not names to lock:

| Purpose | Gitea | Grafana | Infisical | Hikyo analogue |
|---|---|---|---|---|
| enable / presence | `ENABLED` | `enabled` | presence of `SMTP_HOST` | presence of the address (no second master switch — #579 charting note) |
| server | `SMTP_ADDR` + `SMTP_PORT` | `host` (`host:port`) | `SMTP_HOST` + `SMTP_PORT` (587) | `host:port`, canonical, required |
| TLS mode | `PROTOCOL ∈ smtps / smtp+starttls / smtp` | `startTLS_policy ∈ Opportunistic / Mandatory / NoStartTLS`; 465 = implicit | `SMTP_IGNORE_TLS`, `SMTP_REQUIRE_TLS` (true) | closed enum `implicit` \| `starttls`; **no opportunistic, no cleartext** (dev-only loopback allowance per §6.3 if chosen) |
| trust | `FORCE_TRUST_SERVER_CERT` (*"unsafe"*) | `skip_verify` | `SMTP_TLS_REJECT_UNAUTHORIZED` (true), `SMTP_CUSTOM_CA_CERT` | CA **file** for private relays; **no skip-verify knob** (RFC 8314 §4.4; fail-closed default) |
| credentials | `USER`, `PASSWD` | `user`, `password` (+ `$__file{}`) | `SMTP_USERNAME`, `SMTP_PASSWORD` | user + password **file** (env as documented weakest tier, exactly one source) |
| identity | `FROM` (RFC 5322), `ENVELOPE_FROM`, `HELO_HOSTNAME` | `from_address`, `from_name`, `ehlo_identity` | `SMTP_FROM_ADDRESS`, `SMTP_FROM_NAME`, `SMTP_HELO_HOST` | `From` parsed with `net/mail.ParseAddress` at boot; EHLO name |
| link base | `AppUrl` | root_url | `SITE_URL` (required, absolute) | `HIKYO_EXTERNAL_ORIGIN`, **explicitly set** (#579 decision 6) |
| address policy | — | — | — | allowed-CIDR list for a LAN relay (§7.5) |
| body format | `SEND_AS_PLAIN_TEXT` | `content_types` | — | fixed by #584 (§4), not operator-configurable |
| test send | admin UI "send test mail" | admin UI | — | operator test send (§7.4) |

### 7.2 Environment vs a sealed database row

Two live precedents:

| | Env/file config (root key, TLS pair, backup settings) | Sealed DB row edited in the WebUI (OIDC provider `client_secret`: `providerSecretAAD`, `sealSecret` via `Keyring.ForInstance()`) |
|---|---|---|
| Who configures | the operator who owns the process | an instance admin with the right capability, reauth-gated |
| HA | must be identical on every node (as `HIKYO_HA` already demands for the root-key source) | shared by construction |
| Restore | outside the database; survives | ops-spec restore checklist: *"adapter outbound credentials re-entered (write-only, unreadable from the DB by design)"* — an SMTP password would follow the same rule and be re-entered after restore |
| Boot validation | static, fail-fast, named error | cannot fail boot (row may be absent); validated at write time |
| Audit | none (process config) | `account-security`-class mutation, audited, like OIDC provider edits |
| Precondition check | "is the env complete" | "does the row exist and validate" |
| Air-gap / no-egress | trivially zero outbound when unset | same |
| Fits "no second master switch" | presence = enabled | presence = enabled |
| Where #579 expects it | *"mailer configured … definition from the mailer research"* — either satisfies | same |

The comparators are all process-config (Gitea/Forgejo ini, Grafana ini, Infisical env). Hikyo's own precedent splits: transport-level trust material (TLS, root key, egress policy) is process config; per-integration credentials that admins rotate (OIDC, SAML, adapters, dynamic providers) are sealed rows. An SMTP relay credential is closer to the second class in *rotation* behaviour and to the first in *who owns it* (the deployment, not a tenant). **This is #584's decision**; the research records that both are consistent with the locked rules and that a sealed row means the mailer joins the `reencrypt` set and the restore re-entry checklist.

### 7.3 The "mailer configured" predicate (the definition #579 delegated here)

Proposed as a **static, boot-safe, testable** predicate, evaluated wherever #579 decision 6 runs preconditions (policy write, sign-up use), never by dialing:

`mailer_configured ⇔ all of:`

1. **Transport present and well-formed**: server address parses as `host:port` with a non-empty host and a port in 1..65535; TLS mode is one of the closed enum values (`implicit` | `starttls`); if a CA file is named it loads as at least one PEM certificate; if a username is set a password source is set, and exactly one password source (file or env) is set.
2. **Sender identity well-formed**: `From` parses under `net/mail.ParseAddress`; EHLO name, if set, is a hostname (no scheme, no port).
3. **Public origin explicit**: `HIKYO_EXTERNAL_ORIGIN` is set (the mail carries a link; a listen-derived origin would put `127.0.0.1` or a container hostname into a public mail). This is #579's own precondition restated because the mailer cannot be "configured" for its purpose without it.
4. **Not the loopback-cleartext dev allowance** (if #584 adopts one): a `Dev`-only cleartext transport counts as configured **only** when `cfg.Dev` is true — a production process with that shape is `inactive: mailer-unconfigured`.

What the predicate deliberately **excludes**: reachability. A relay that is down does not make the policy "unconfigured"; it makes the *send* fail, which #579 already routes to uniform refusal + audited cause + `inactive` with a cause. Whether one failed send latches the local entry `inactive: mailer-unreachable` until an operator test send clears it, or every sign-up re-attempts, is #584's call; the DR health row (`BackupState`, single row, *"loud and durable: instance trail + health row"*) is the existing shape for a latched cause if one is wanted.

Boot behaviour under this definition: a **malformed** mailer config (predicate clause 1 or 2 false while any mailer variable is set) is a boot error by name, like every other `HIKYO_*` variable; an **absent** mailer is fine (off by default). Reachability is never probed at boot (§7.4).

### 7.4 Boot, air-gap, and the test send

- `scripts/ci/no-egress.sh` (mvp-boundary O7, ops-spec §13, CI invariant 4): an strace'd boot-plus-idle of `hikyo server` *"with NOTHING configured (no remotes, no recipients, no adapters, no IdPs), originates ZERO outbound connections and still boots AND serves"*. It fails on any `connect`/`sendto`/`sendmsg` to a non-loopback address. With the mailer unset this holds by construction. With the mailer set, boot still must not dial — the invariant's spirit (ops-spec: *"the server boots and serves with outbound network denied"*) and #579's *"never at boot"* both say so.
- The probe therefore lives on an **operator-triggered test send** (Gitea and Grafana both expose one in the admin UI) and on real sends. A test send is an audited instance-scope action to a recipient the operator types, under the same deadline and dialer as production sends.
- HA: any node may send; the config is per-process and must be identical across nodes, as `HIKYO_HA` already requires for the root-key source.

### 7.5 A new enumerated outbound class — facts for the threat-model amendment

The threat model enumerates outbound destinations (*"the configured datastore, the K8s API for the operator, and deployment-module targets"*) and was amended once by multi-instance to add *"configured remote Hikyo instances"* under a normative control set. A configured SMTP relay is a further class; the control set that transfers, with the SMTP-specific reading:

| multi-instance control | SMTP analogue | Available mechanism |
|---|---|---|
| canonical `https://` origin only | explicit `host:port`, no URL, no userinfo | config validation |
| pin before bytes (SPKI) | WebPKI or operator CA-file validation with `ServerName` = configured host, TLS 1.2 floor; **no skip-verify** | go-mail defaults (`ServerName: host`, `MinVersion: TLS 1.2`), `WithTLSConfig` for the CA file |
| no redirects | n/a (SMTP has none) | — |
| DNS-rebinding hardening; private ranges **explicitly permitted** for LAN remotes | `netpolicy.PublicDialer` resolves then dials the exact resolved address; non-public ranges refused unless listed in an allowed-CIDR list — a Postfix on `192.168.x.x` is the homelab norm multi-instance already names for remotes | `WithDialContextFunc(publicDialer.DialContext)`; a CIDR-list variable in the existing JSON-file or comma-list shape |
| per-remote deadline, response cap | per-send deadline (go-mail default 15 s); message size is Hikyo-authored so no ingest cap | `DialAndSendWithContext` |
| fan-out / aggregate trigger rates | sends are bounded by the §179 `signup` budget #579 named, plus the operator test send under its own admission class | existing admission machinery |
| explicit proxy only, `CONNECT`-tunneled | no proxy support in v1 (Gitea/Grafana/Infisical offer none either); a SOCKS/CONNECT hop for SMTP would be new machinery | — |
| zero configured = zero outbound | mailer unset ⇒ no dial ever; `no-egress.sh` unchanged | by construction |

Also transferring: threat-model's *"No plaintext in … logs"* — a rendered verification mail **is** a credential-bearing artifact; logs and audit carry the recipient (or a hash), the message id and the outcome, never the body or the link.

For the ops-spec composable-maxima catalogue: the per-send deadline and the test-send admission class are the two new numbers.

---

## 8. Per-ticket facts

### [#584 Grilling: local email + password sign-up ceremony and account model](https://github.com/Hikyo-Org/Hikyo/issues/584)

- Delivery is one SMTP client (research lean: go-mail, §3) over TLS-mandatory submission; no provider seam (§2). Decide: env/file vs sealed row (§7.2); text-only vs text + HTML (§4); dev-loopback allowance vs CA-file + TLS sink (§6.3).
- **Synchronous send after commit, no outbox** is the research lean (§5.2); the ticket must then resolve the uniform-response/timing-oracle question and the audited cause for a failed send (§5.3), and whether a failed send latches `inactive: mailer-unreachable` (§7.3).
- The "mailer configured" predicate (§7.3) is what the policy's local entry checks at write and use; reachability is excluded by design.
- Resend-verification is the user's retry; it sits under the §179 `signup` budget.
- Nothing in the mail body may be logged or audited; the link is the credential (§7.5).

### [#587 Prototype: login, sign-up, invitation-claim, and registration-policy surfaces](https://github.com/Hikyo-Org/Hikyo/issues/587)

- Inactive-with-cause on the Members "Open registration" panel and on the login page needs the causes this research names: `mailer-unconfigured` (predicate false) and, if #584 adopts latching, `mailer-unreachable`.
- An instance-admin **test send** surface exists in every comparator's admin UI (§7.4) — it is the only probe permitted; the prototype should place it beside the mailer status.
- The sign-up "check your mail" state and the resend action are the user-facing halves of §5.3.

### [#589 Synthesis: ADR amendment set and handoff spec](https://github.com/Hikyo-Org/Hikyo/issues/589)

- **threat-model**: new enumerated outbound class "configured SMTP relay" with the §7.5 control set (declared amendment, as multi-instance did).
- **system-architecture**: no amendment if #584 takes synchronous send; a third domain outbox otherwise (§5.2).
- **ops-spec**: composable-maxima entries for the per-send deadline and the test-send admission class; the restore checklist gains "SMTP credential re-entered" only if #584 chooses a sealed row (§7.2).
- **human-auth**: the mail-as-alternative-transport frame (§5.1) already fits; the amendment records the verification mail as the one place mail is *required* for a ceremony (local sign-up) and that the requirement is enforced by the policy precondition, not by the credential model.
- Handoff spec "mailer config" section: the §7.1 surface table, the known-env gate, the CA-file knob, the CIDR list.
- Implementation-ticket facts: register new `HIKYO_*` names in `knownEnv`; `go.mod` gains `github.com/wneessen/go-mail` (research lean) with `x/text` promoted from indirect; e2e picks Mailpit-as-service or an in-repo `internal/mailtest` sink (§6.2).

### Fog, unchanged

- *Other mailer consumers* (local invites, credential reset, expiry warnings) stays in **Not yet specified**: human-auth and ops-spec already frame each as display-once / in-product first with mail as an optional additional transport, and nothing here sharpens *which* adopt it or *when* — that question gets precise only once #584 fixes the mailer's shape.

---

## 9. Sources

Standards

- RFC 8314, *Cleartext Considered Obsolete: Use of TLS for Email Submission and Access* — §3.3 (implicit TLS on 465 preferred; implement both 465/587), §4 (TLS 1.2+; certificate validation; no confidentiality claim on an unvalidated certificate). https://www.rfc-editor.org/rfc/rfc8314
- RFC 3207 (SMTP STARTTLS), RFC 6409 (Message Submission, port 587), RFC 5322 (Internet Message Format), RFC 4616 (PLAIN), RFC 2195 (CRAM-MD5), RFC 6376 (DKIM) — cited by number.

Go standard library

- `net/smtp` package documentation: frozen notice; `PlainAuth`, `CRAMMD5Auth`; `Client.StartTLS`; `SendMail`. https://pkg.go.dev/net/smtp
- `net/mail` package documentation: RFC 5322 scope, `Address.String()` RFC 2047 encoding, parse-only. https://pkg.go.dev/net/mail
- `html/template` package documentation: contextual auto-escaping, wraps `text/template`. https://pkg.go.dev/html/template

`wneessen/go-mail`

- Repository README: features (auth mechanisms, TLS policies, custom dial-context, DKIM, alternative bodies, templates), MIT, fork-of-`net/smtp` note. https://github.com/wneessen/go-mail
- Package documentation: `TLSPolicy` constants, `DefaultTLSPolicy`, `DefaultTLSMinVersion`, `DefaultPort*`, `DefaultTimeout`, `NewClient`, `WithSSL`/`WithSSLPort`, `WithSMTPAuth`, `WithDialContextFunc`, `WithTLSConfig`, `DialAndSendWithContext`, `Msg.SetBody*Template`, `AddAlternativeString`. https://pkg.go.dev/github.com/wneessen/go-mail
- Imports tab (only `x/crypto`, `x/text` beyond the module and stdlib). https://pkg.go.dev/github.com/wneessen/go-mail?tab=imports
- `client.go` on `main`: TLSMandatory enforcement error text, default `tls.Config`, `WithDialContextFunc` semantics, timeout application. https://raw.githubusercontent.com/wneessen/go-mail/main/client.go
- `smtp/auth_plain.go` on `main`: `PlainAuth(identity, username, password, host, allowUnenc bool)`; `ErrUnencrypted` when `!allowUnencryptedAuth && !server.TLS && !isLocalhost(server.Name)`. https://raw.githubusercontent.com/wneessen/go-mail/main/smtp/auth_plain.go
- `go.mod` on `main` (`go 1.25.0`; `x/crypto v0.54.0`, `x/text v0.40.0`). https://github.com/wneessen/go-mail/blob/main/go.mod
- Releases: v0.8.1 (2026-07-09), v0.8.0 (2026-07-04), v0.7.3 (2026-05-12); v0.7.1 CVE-2025-59937 note; v0.7.0 Go support policy. Dates verified via `GET /repos/wneessen/go-mail/releases/latest` and `/commits?per_page=1` on 2026-09-02. https://github.com/wneessen/go-mail/releases
- Project site `go-mail.dev` redirects (301) to the repository wiki; neither names adopters or production users. https://github.com/wneessen/go-mail/wiki

`emersion/go-smtp`, `go-msgauth`

- Package documentation: client (`Dial`, `DialTLS`, `DialStartTLS`, `SendMail`), server (`Backend`, `Session`, `NewServer`), extensions, MIT, pre-v1. https://pkg.go.dev/github.com/emersion/go-smtp — v0.25.0 date verified via the GitHub releases API (2026-08-13; last commit 2026-08-18).
- `go-msgauth/dkim` v0.7.0 (2025-04-20): `Sign`, `SignOptions`. https://pkg.go.dev/github.com/emersion/go-msgauth/dkim

Comparable OSS control planes

- Gitea config cheat sheet: `[mailer]` keys and `PROTOCOL` values, `USER`/`PASSWD` TLS-only note, `FORCE_TRUST_SERVER_CERT` "unsafe", `SEND_AS_PLAIN_TEXT`; `[service] REGISTER_EMAIL_CONFIRM` "requires Mailer to be enabled", `DISABLE_REGISTRATION`; `[queue.mail]`. https://docs.gitea.com/administration/config-cheat-sheet
- Gitea mail templates: `custom/templates/mail/…`, `text/template` subject + `html/template` body, `---` separator, text part "obtained by stripping the HTML markup", restart to reload. https://docs.gitea.com/administration/mail-templates
- Gitea `services/mailer/mailer.go` (`main`): `mailQueue` worker-pool queue, sender selection (`sendmail`/`dummy`/SMTP), failures logged not retried. https://raw.githubusercontent.com/go-gitea/gitea/main/services/mailer/mailer.go
- Gitea `services/mailer/sender/smtp.go` (`main`): `net/smtp`, smtps/starttls dialing, `tls.Config` without `MinVersion`, `InsecureSkipVerify` from `ForceTrustServerCert`, CRAM-MD5 → PLAIN → custom LOGIN → NTLM selection, no timeouts. https://raw.githubusercontent.com/go-gitea/gitea/main/services/mailer/sender/smtp.go
- Gitea `templates/mail/user/auth/activate.tmpl` (`main`) and the directory listing (`activate`, `activate_email`, `register_notify`, `reset_passwd`): `.locale.Tr` keys, variables, HTML-only. https://raw.githubusercontent.com/go-gitea/gitea/main/templates/mail/user/auth/activate.tmpl
- Forgejo config cheat sheet: same `[mailer]` protocol set and TLS-only auth note; `REGISTER_EMAIL_CONFIRM`, `REGISTER_MANUAL_CONFIRM`, `DISABLE_REGISTRATION`, `REQUIRE_EXTERNAL_REGISTRATION_PASSWORD`. https://forgejo.org/docs/latest/admin/config-cheat-sheet/
- Grafana configuration reference: `[smtp]` (`enabled`, `host`, `user`, `password` with `$__file{}`/`${ENV}`, `skip_verify`, `from_address`, `from_name`, `ehlo_identity`, `startTLS_policy`), `[smtp.static_headers]`, `[emails]` (`templates_pattern`, `content_types`). https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/
- Grafana `pkg/services/notifications/notifications.go` (`main`): `mailQueue` channel (cap 10), `Run()` loop, failures logged not retried, `SendEmailCommandSync`, `from_address` validation at start-up. https://raw.githubusercontent.com/grafana/grafana/main/pkg/services/notifications/notifications.go
- Infisical self-hosting environment variables: `SITE_URL` required absolute; `SMTP_HOST/PORT/USERNAME/PASSWORD/FROM_ADDRESS/FROM_NAME/HELO_HOST`, `SMTP_IGNORE_TLS`, `SMTP_REQUIRE_TLS` (true), `SMTP_TLS_REJECT_UNAUTHORIZED` (true), `SMTP_CUSTOM_CA_CERT`; without email, mail-dependent features are disabled while sign-up/login work. https://infisical.com/docs/self-hosting/configuration/envars

Providers

- Resend pricing (free 3,000/month, 100/day, 3 domains; Pro $20/month, 50,000). https://resend.com/pricing — Resend SMTP (`smtp.resend.com`, 465/2465 implicit, 25/587/2587 STARTTLS, user `resend`, API key as password, API rate limit shared, no SMTP logs). https://resend.com/docs/send-with-smtp
- Postmark pricing (100/month developer tier; Basic $15/month, 10,000; SMTP on all tiers). https://postmarkapp.com/pricing — Postmark SMTP (`smtp.postmarkapp.com`, 25/2525/587, STARTTLS, token auth, `X-PM-Message-Stream`, CRAM-MD5/DIGEST-MD5/PLAIN/LOGIN). https://postmarkapp.com/developer/user-guide/send-email-with-smtp
- Amazon SES pricing ($0.10 per 1,000 à la carte; $200 credits for 6 months for new accounts). https://aws.amazon.com/ses/pricing/ — SES SMTP interface (credentials distinct from IAM keys, per region, TLS-capable client required). https://docs.aws.amazon.com/ses/latest/dg/send-email-smtp.html — SES SMTP endpoints (STARTTLS 25/587/2587, TLS Wrapper 465/2465, TLS required). https://docs.aws.amazon.com/ses/latest/dg/smtp-connect.html

Mailpit

- Documentation index (single binary, multi-arch Docker, STARTTLS/SSL and auth options, pruning at 500). https://mailpit.axllent.org/docs/
- Runtime options (SMTP `0.0.0.0:1025`, HTTP `0.0.0.0:8025`, `--smtp-tls-cert`, `--smtp-require-starttls`, `--smtp-require-tls`, `--smtp-auth-accept-any`, `--smtp-auth-allow-insecure`, `--smtp-auth-file`, `--smtp-disable-rdns`, `--max`, image `axllent/mailpit`). https://mailpit.axllent.org/docs/configuration/runtime-options/
- API v1 Swagger definition (`/api/v1/messages`, `/api/v1/search`, `/api/v1/message/{ID}`, `/api/v1/message/{ID}/raw`, `DELETE /api/v1/messages`, `/api/v1/info`). https://raw.githubusercontent.com/axllent/mailpit/master/server/ui/api/v1/swagger.json
- Repository (MIT, Go, ports); v1.31.0 (2026-08-22) via the GitHub releases API. https://github.com/axllent/mailpit

Hikyo (this repository, `origin/main` at `ec90c724` on 2026-09-02)

- `docs/adr/human-auth.md` § *Recovery* (email never on the critical path; reset token "may be mailed instead"; *Rejected: SMS or email as a factor*), § *Enumeration resistance* (bounded dummy path), § *No hand-rolled primitive*.
- `docs/adr/ops-spec.md` composable-maxima table ("SMTP transport off by default"), restore checklist ("adapter outbound credentials re-entered"), air-gap CI invariant 4.
- `docs/adr/system-architecture.md` § *Jobs — no generic framework; one domain-specific outbox* and its 2026-09-02 #147 amendment; § *Transactions* (no external effect before commit); § *Modes* (embedded web UI).
- `docs/adr/multi-instance.md` § *The outbound client* control set and the threat-model amendment header; § *Freshness* ("There is no background poller").
- `docs/adr/threat-model.md` (enumerated outbound destinations; no plaintext in logs; token entropy/hash/expiry).
- `docs/adr/mvp-boundary.md` §2.2 (three-criteria "known library" bar; fallback ladder).
- `internal/config/config.go` (`HIKYO_*` parsing, root-key tiers, `HIKYO_EXTERNAL_ORIGIN` validation, egress policy JSON loaders, `Dev`); `internal/config/knownenv_test.go` (`TestKnownEnvCoversEveryGetenv`).
- `internal/netpolicy/public.go` (`PublicDialer`: resolve, refuse non-public unless allowed, dial the exact address).
- `internal/app/scheduler.go` (hourly tick, 10-minute deadline, HA singleton lease); `internal/store/migrations/sqlite/00024_adapter_outbox.sql` (`adapter_outbox` columns and indexes); `internal/store/adapter_runtime.go` (`FOR UPDATE SKIP LOCKED` claim, `Retry`); `internal/store/backup_state.go` (single-row DR health record).
- `internal/service/oidc.go` (`providerSecretAAD`, `sealSecret` under `Keyring.ForInstance()`); `internal/store/authn/oidc.go` (`ClientSecret []byte` sealed record).
- `internal/oidctest/` and `web/e2e/fixtures/instance.ts` (in-repo protocol test double built on demand); `.github/workflows/ci.yml` (release-shaped app for browser legs, `postgres:18` service container, `no-egress` job); `scripts/ci/no-egress.sh`.
- `go.mod` (`go 1.27.0`; `golang.org/x/crypto v0.55.0` direct; `golang.org/x/text v0.41.0` indirect; `aws-sdk-go-v2` indirect via `sops`; no mail library).
- `web/src` (`rg -i 'i18n|useTranslation|locale'` → only `toLocaleString`/`toLocaleDateString`).
- Issues: [#579](https://github.com/Hikyo-Org/Hikyo/issues/579) resolution (decisions 4–6, 9–11), [#568](https://github.com/Hikyo-Org/Hikyo/issues/568) (display-once invite), [#584](https://github.com/Hikyo-Org/Hikyo/issues/584), [#587](https://github.com/Hikyo-Org/Hikyo/issues/587), [#589](https://github.com/Hikyo-Org/Hikyo/issues/589).
