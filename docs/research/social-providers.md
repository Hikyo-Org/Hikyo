# Google, Microsoft Entra, GitHub as sign-in providers — primary-source research

**Date:** 2026-09-02
**Context:** Wayfinder map [#578](https://github.com/Hikyo-Org/Hikyo/issues/578) (social sign-in and open registration), ticket [#581](https://github.com/Hikyo-Org/Hikyo/issues/581). The map's locked rules that collide with provider behaviour: identity key `(kind, issuer, subject)` with a **byte-exact issuer** (human-auth, `internal/oidcrp`); **`email_verified == true` required on every external sign-up entry** ([#579](https://github.com/Hikyo-Org/Hikyo/issues/579) decision 5); **PKCE S256 always, per-provider callback as mix-up defence, no offline scopes, access token used once** ([#580](https://github.com/Hikyo-Org/Hikyo/issues/580)); an explicitly configured public origin as the registration precondition (#579 decision 6). This document records what each provider *does*; it decides nothing. Decisions belong to the grilling tickets it feeds (§7).
**Method:** Primary sources only: each provider's own protocol reference, live discovery documents fetched on 2026-09-02, brand pages, console/portal help pages, GitHub's changelog, and the `coreos/go-oidc` source at the version Hikyo pins (`v3.20.0`; `golang.org/x/oauth2 v0.36.0`). Every claim carries its source in §8. Where a provider's documentation is silent, the row says "not documented" rather than guessing.

**Checklist this document answers** (merged from the ticket body, the #579 inheritance comment, and #580's "consequences for #581"):

| # | Item | Where |
|---|---|---|
| 1 | Issuer form, discovery document, byte-exact collisions | §2.1, §3.1, §4.1, §6 |
| 2 | `email_verified` asserted or not, **per account type** | §2.3, §3.3, §4.3, §6 |
| 3 | Google `hd` presence and reliability | §2.3 |
| 4 | Entra `tid`, `email`, `preferred_username`, `xms_edov`; multi-tenant issuer; per-customer-tenant row cost | §3.1–§3.4 |
| 5 | GitHub endpoints, stable id, `/user/emails` scope and semantics, rate limits, Apps vs OAuth Apps, token lifetime, OIDC discovery | §4 |
| 6 | Login/consent parameters for a login-only flow; refresh-token avoidance | §2.4, §3.5, §4.4 |
| 7 | Client-secret rotation practice | §2.6, §3.6, §4.6 |
| 8 | Brand rules per button | §2.7, §3.7, §4.7 |
| 9 | Redirect-URI / verified-domain constraints on the public-origin precondition | §2.5, §3.5, §4.5, §6 |
| 10 | GitHub PKCE: `code_verifier` enforced at exchange; GHES support; API host from origin; `Accept: application/json`; minimal scope set | §4.2, §4.4 |

---

## 1. Executive summary

- **All three providers can satisfy the identity key, but only Google does so with no wrinkle.** Google's `sub` is global, immutable and never reused. Entra's `sub` is **pairwise per app registration and per tenant**: re-registering the Hikyo client (new `client_id`) changes every subject; `oid` is the cross-app stable id but is not what OIDC's `sub` carries. GitHub's numeric `id` is stable and `login` is mutable, as #580 assumed.
- **The byte-exact issuer rule collides twice.** (a) Entra's `common` and `organizations` discovery documents publish `"issuer": "https://login.microsoftonline.com/{tenantid}/v2.0"` — the literal placeholder — while every token carries the tenant GUID; `go-oidc` refuses that mismatch unless `InsecureIssuerURLContext` is used, which its own doc comment calls "meant for integration with off-spec providers such as Azure". A **tenant-specific** discovery document (`/<tid>/v2.0/.well-known/openid-configuration`) publishes the literal GUID issuer and passes byte-exact discovery unchanged. (b) Google documents that a token's `iss` may be **either** `https://accounts.google.com` **or** the bare `accounts.google.com`; `go-oidc` tolerates the bare form for Google alone, but Hikyo's belt check in `internal/oidcrp` (`tok.Issuer != p.issuer`) would refuse it.
- **`email_verified` is asserted differently by each provider, and never uniformly per account type.** Google emits a boolean `email_verified` with no consumer/Workspace distinction, and separately warns that for a non-Gmail address with no `hd` claim "Google is not authoritative … even if `email_verified` is true". **Entra emits no `email_verified` claim at all** (not in `claims_supported`, not on UserInfo); the only verification signal is the optional `xms_edov` claim (domain-owner verified), and since June 2023 new multi-tenant apps have unverified-domain emails **omitted** from tokens by default. GitHub has no claim; `/user/emails` returns `primary` and `verified` booleans per address behind the `user:email` scope.
- **PKCE:** Google advertises `plain` and `S256`; Entra supports both and recommends PKCE for confidential clients too; GitHub.com accepts `S256` only (changelog 2025-07-14) and documents `code_verifier` as "Required if `code_challenge` was sent". **GitHub Enterprise Server docs through 3.20 state PKCE parameters "are not supported at this time"** — a GHES row cannot honour #580's "PKCE always".
- **Mix-up defence:** none of the three discovery documents advertises `authorization_response_iss_parameter_supported`; GitHub has no discovery at all. Per-provider callback paths are the operative defence for all three.
- **Refresh tokens are avoidable at every provider** — Google `access_type` defaults to `online`; Entra issues one only for `offline_access`; GitHub OAuth apps created today default to **expiring tokens** (8 h access, 6-month refresh) and return a refresh token with the access token, which the callback must discard.
- **Public-origin precondition:** Google is the strictest — redirect URIs must be HTTPS (localhost exempt), the host must not be a raw IP and its TLD must be on the public suffix list, no wildcards, exact match including trailing slash. A self-hoster on `https://192.168.1.10` or `https://hikyo.lan` cannot register a Google client at all. Entra: HTTPS except localhost, case-sensitive, no IDN, ≤256 chars, ≤256 URIs. GitHub: host (excluding subdomains) and port must match exactly, up to 10 callback URLs, HTTPS not stated as mandatory.
- **Reauth wrinkle for Google:** Google's documented `prompt` values are `none`, `consent`, `select_account` — **`login` is not listed**, and `max_age`/`auth_time` are not documented on the OIDC page. human-auth's OIDC reauth recipe (`prompt=login`, `max_age=0`, `auth_time`) is therefore unverified for Google. Entra documents `prompt=login` and offers `auth_time`/`amr` as optional claims that must be enabled per app registration.
- **Brand rules:** Google mandates the exact strings "Sign in with Google" / "Sign up with Google" / "Continue with Google", the standard-colour "G", three fixed themes, and says compliance "is required for app verification". Microsoft mandates the Microsoft logo with "Sign in with Microsoft" (or "Sign in"), forbids "Azure"/"Active Directory"/"Microsoft 365 ID" toward end users, and asks for "work or school account" wording. GitHub permits the Invertocat "to inform others that your project integrates with GitHub", in black or white only, unmodified, less prominent than the host's own brand; no button text is mandated.

---

## 2. Google (OIDC)

### 2.1 Issuer and discovery

- Discovery: `https://accounts.google.com/.well-known/openid-configuration`. Live document (2026-09-02): `issuer` = `https://accounts.google.com`; `authorization_endpoint` = `https://accounts.google.com/o/oauth2/v2/auth`; `token_endpoint` = `https://oauth2.googleapis.com/token`; `userinfo_endpoint` = `https://openidconnect.googleapis.com/v1/userinfo`; `jwks_uri` = `https://www.googleapis.com/oauth2/v3/certs`; `code_challenge_methods_supported` = `["plain","S256"]`; `subject_types_supported` = `["public"]`; `id_token_signing_alg_values_supported` = `["RS256"]`; `claims_supported` includes `email`, `email_verified`, `sub`, `iss`, `aud` (no `hd` listed, though it is emitted); `grant_types_supported` includes `authorization_code`, `refresh_token`, `urn:ietf:params:oauth:grant-type:device_code`. No `authorization_response_iss_parameter_supported` field. [G1]
- **Issuer in tokens.** Google's OIDC reference instructs: "Verify that the value of the `iss` claim in the ID token is equal to `https://accounts.google.com` or `accounts.google.com`." [G2] The verification page repeats it: "The value of `iss` in the ID token is equal to `accounts.google.com` or `https://accounts.google.com`." [G3]
- **Library behaviour.** `go-oidc` `Verify` hard-codes this: `issuerGoogleAccounts = "https://accounts.google.com"`, `issuerGoogleAccountsNoScheme = "accounts.google.com"`, with the comment "Google sometimes returns "accounts.google.com" as the issuer claim instead of the required "https://accounts.google.com". Detect this case and allow it only for Google. We will not add hooks to let other providers go off spec like this." [L1] Hikyo's `internal/oidcrp.Provider.Verify` then re-checks `tok.Issuer != p.issuer` and returns `ErrIssuer` — so a bare-form token would pass go-oidc and fail Hikyo. Whether Google still emits the bare form on the v2 authorization endpoint is **not documented**; the docs only say it may.

### 2.2 Subject

"An identifier for the user, unique among all Google Accounts and never reused… the `sub` value is never changed. Use `sub` within your application as the unique-identifier key for the user." [G2] Fits `(oidc, https://accounts.google.com, sub)` with nothing to add.

### 2.3 Verified email, `hd`, account types

- `email_verified`: "True if the user's email address has been verified; otherwise false." [G2] The reference makes **no distinction between consumer and Workspace accounts** for this claim.
- `hd`: "The domain associated with the Google Workspace or Cloud organization of the user. Provided only if the user belongs to a Google Cloud organization." Reliability warning: "Don't rely on this UI optimization to control who can access your app, as client-side requests can be modified. Be sure to validate that the returned ID token has an `hd` claim value that matches what you expect." [G2] The verification page: "The absence of this claim indicates that the account does not belong to a Google hosted domain." [G3]
- **Authoritativeness caveat (the important one for #579's predicate).** Google's verification page distinguishes the cases: with `email` ending in `@gmail.com`, or with an `hd` claim present, Google hosts and is authoritative for the address; when `email` is not a Gmail address **and** `hd` is absent, "Google is not authoritative and password or other challenge methods are recommended to verify the user", **even if `email_verified` is true**, since "ownership of the third party email account may have since changed." [G3] In other words, a Google Account created on a third-party address (e.g. `alice@example.org`) can carry `email_verified: true` while Google asserts only that the address was verified once, at some past time.
- Basic scopes: `openid email profile`. [G2]

### 2.4 Login-only request parameters

- `prompt`: "Space-delimited list: `none`, `consent`, `select_account`." **`login` is not among the documented values.** `login_hint`: email or `sub`. `nonce`, `state` as usual. `access_type`: `online` (default) or `offline`; `offline` "instructs the Google authorization server to return a refresh token and an access token the first time"; with the default no refresh token is issued. `include_granted_scopes` is for incremental authorization. [G2][G4]
- PKCE: the web-server guide does not mention it; the discovery document advertises `plain` and `S256`. [G1][G4] `max_age` and `auth_time` are not covered on the OIDC reference page fetched. [G2]
- Token response: `access_token`, `expires_in`, `token_type` ("always Bearer"), `scope`, `refresh_token` only with `access_type=offline`, `refresh_token_expires_in` only for time-based access grants. [G4]

### 2.5 Redirect-URI and domain constraints

Google's validation rules [G4]: "Redirect URIs must use the HTTPS scheme, not plain HTTP. Localhost URIs (including localhost IP address URIs) are exempt from this rule." "Hosts cannot be raw IP addresses. Localhost IP addresses are exempted from this rule." Host TLDs must belong to the public suffix list; `googleusercontent.com` is prohibited; no path traversal, no open redirects, no fragment, no userinfo, no wildcards, no non-printable ASCII, no invalid percent-encoding. Matching is exact: the request's `redirect_uri` "must exactly match one of the authorized redirect values that you set in the Cloud Console Clients page (including the HTTP or HTTPS scheme, case, and trailing '/', if any)." [G2]

Policy layer [G5]: "Only use domains you own" — redirect URIs and JavaScript origins must "refer to domains that you own, that you have been authorized to use… or that you have been explicitly given license to use." Consent-screen authorized domains must be verified by a project owner/editor in Google Search Console. [G6] For production apps "brand information must be verified"; for apps requesting only `profile`/`email`, the branding guidelines are "recommended" and the sensitive-scope verification process does not apply. [G5][G6] An app in *Testing* publishing status is capped (the help page references a 100-user cap for unverified external apps) and shows the "unverified app" screen. [G4][G6]

### 2.6 Client secrets

Console help [G7]: "You can only have two client secrets at maximum." Rotation = *Add Secret* (new secret is *Enabled* immediately) → update the app → disable the old one → delete it. Since the hashed-secret change, "After the initial creation, the Google Cloud Console will only display the last four characters of the client secret." No expiry on secrets. Separate operational fact: "OAuth 2.0 clients that have been inactive for six months are automatically deleted," with 30 days' notice — an idle self-host with a Google row configured can lose its client.

### 2.7 Brand rules

Google's branding guidelines [G8]: button text must be "Sign in with Google", "Sign up with Google" or "Continue with Google" (localisation allowed); "Regardless of the text, you can't change the size or color of the Google "G" logo. It must be the standard color version." Three themes: light `#FFFFFF` fill + `#747775` 1 px inside stroke; dark `#131314` + `#8E918F`; neutral `#F2F2F2`, no stroke. Font Google Sans Medium 14/20. Rectangular or pill shape. Padding 12 px before the logo, 10 px after (web). Custom-built buttons are permitted but the GIS SDK button is "the recommended way", and "Following these guidelines on displaying the Sign in with Google button is required for app verification."

---

## 3. Microsoft Entra ID (OIDC)

### 3.1 Issuer, discovery, multi-tenant

- Authority URL `https://login.microsoftonline.com/{tenant}/v2.0` with `{tenant}` ∈ `common` | `organizations` | `consumers` | tenant GUID | `contoso.onmicrosoft.com`; discovery at `…/v2.0/.well-known/openid-configuration`. [M1]
- **Live documents (2026-09-02).** `common`: `issuer` = `https://login.microsoftonline.com/{tenantid}/v2.0` (literal placeholder), `authorization_endpoint` = `https://login.microsoftonline.com/common/oauth2/v2.0/authorize`, `token_endpoint` = `…/common/oauth2/v2.0/token`, `jwks_uri` = `…/common/discovery/v2.0/keys`, `userinfo_endpoint` = `https://graph.microsoft.com/oidc/userinfo`, `subject_types_supported` = `["pairwise"]`, `scopes_supported` = `openid profile email offline_access`, `claims_supported` = `sub iss cloud_instance_name cloud_instance_host_name cloud_graph_host_name msgraph_host aud exp iat auth_time acr nonce preferred_username name tid ver at_hash c_hash email` (**no `email_verified`**), no `code_challenge_methods_supported`, no `authorization_response_iss_parameter_supported`. [M2] `organizations`: same placeholder issuer, endpoints under `/organizations/`. [M3] Tenant-specific example (the consumer tenant GUID): `issuer` = `https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0`, endpoints under that GUID. [M4]
- **Token `iss`.** "Identifies the issuer… It also identifies the tenant for which the user was authenticated. If the token was issued by the v2.0 endpoint, the URI ends in `/v2.0`. The GUID that indicates that the user is a consumer user from a Microsoft account is `9188040d-6c67-4c5b-b112-36a304b66dad`. Your app should use the GUID portion of the claim to restrict the set of tenants that can sign in to the app." [M5] `tid`: "the immutable tenant ID of the organization that the user is signing in to. For sign-ins to the personal Microsoft account tenant… the value is `9188040d-…`." [M5]
- **Microsoft's multi-tenant validation rule.** "A multitenant application is configured to consume keys metadata from `/organizations` or `/common` keys URLs. The application must validate that the `issuer` property in the published metadata matches the `iss` claim in the token, in addition to the usual check that the `iss` claim in the token contains the tenant ID (`tid`) claim." "When a response returns from the `/common` endpoint, the issuer value in the token corresponds to the user's tenant." [M6]
- **Library behaviour.** `go-oidc` `NewProvider` fails with `IssuerMismatchError` ("oidc: issuer URL provided to client (%q) did not match the issuer URL returned by provider (%q)") when the discovery `issuer` differs from the URL given, unless the context carries `InsecureIssuerURLContext`, whose doc comment reads: "allows discovery to work when the issuer_url reported by upstream is mismatched with the discovery URL. This is meant for integration with off-spec providers such as Azure." [L2] Hikyo's `oidcrp.Discover` calls `oidc.NewProvider(ctx, issuer)` with a plain context. Consequence: a row whose issuer is `https://login.microsoftonline.com/common/v2.0` cannot pass discovery at all; a row whose issuer is `https://login.microsoftonline.com/<tid>/v2.0` passes byte-exact discovery and byte-exact token verification without any relaxation.
- **What a per-customer-tenant row costs.** The Hikyo operator registers **one** app in their own Entra tenant with *Supported account types* = any organizational directory (multi-tenant); the same `client_id`/secret serves every tenant. [M6] For each customer tenant the operator needs its tenant GUID (or `*.onmicrosoft.com` name) to form the row's issuer; the customer side needs nothing pre-provisioned: "When a user from a different tenant signs in to the application for the first time, Microsoft Entra ID asks them to consent to the permissions requested by the application. If they consent, then… a *service principal* is created in the user's tenant." If the customer tenant has disabled user consent, an admin must consent (`prompt=consent` from an admin). [M6] Publisher verification is not required for basic sign-in: the November 2020 risk-based step-up consent restriction "applies to apps… which use OAuth 2.0 to request permissions that extend beyond the basic sign-in and read user profile". [M7] Hikyo's "one enabled provider per `(kind, issuer)`" is untouched because each tenant row has a distinct issuer.

### 3.2 Subject and other identifiers

- `sub`: "This value is immutable and can't be reassigned or reused. The subject is a pairwise identifier and is unique to an application ID. If a single user signs into two different apps using two different client IDs, those apps receive two different values for the subject claim." Also: "The value is based on a combination of the token recipient, tenant, and user." [M5] Consequence: an operator who deletes and re-creates the Entra app registration (new `client_id`) changes `sub` for every user; under Hikyo's key those are new identities.
- `oid`: "The immutable identifier for an object… uniquely identifies the user across applications… If a single user exists in multiple tenants, the user contains a different object ID in each tenant." Requires the `profile` scope. [M5] Microsoft's guidance: "use `sub` or `oid` alone (which as GUIDs are unique), with `tid` used for routing or sharding if needed." [M5]
- Guests: "Guest scenarios, where a user is homed in one tenant, and authenticates in another, should treat the user as if they're a brand new user to the service." `idp` differs from `iss` for guests. [M5]
- `preferred_username`: "Its value is mutable and might change over time. Since it's mutable, this value can't be used to make authorization decisions." v2 only, needs `profile`. [M5] `upn`: "Not a durable identifier… shouldn't be used for authorization or to uniquely identity user information (for example, as a database key)." [M8]

### 3.3 Verified email

- `email`: "Present by default for guest accounts that have an email address. Your app can request the email claim for managed users… using the `email` optional claim. This value isn't guaranteed to be correct and is mutable over time. Never use it for authorization or to save data for a user… On the v2.0 endpoint, your app can also request the `email` OpenID Connect scope." [M5]
- **No `email_verified` claim.** It is absent from `claims_supported` [M2][M3] and the UserInfo endpoint returns only `sub`, `name`, `family_name`, `given_name`, `picture`, `email` ("These values are the same values included in an ID token"). [M9]
- `xms_edov` (optional claim, JWT): "Boolean value indicating whether the user's email domain owner has been verified. An email is considered to be domain verified if it belongs to the tenant where the user account resides and the tenant admin has done verification of the domain. Also, the email must be from a Microsoft account (MSA), a Google account, or used for authentication using the one-time passcode (OTP) flow. Facebook and SAML/WS-Fed accounts **do not** have verified domains. For this claim to be returned in the token, the presence of the `email` claim is required." [M8] It must be configured as an optional claim on the app registration (Token configuration / manifest `optionalClaims.idToken`). [M8]
- **Default omission since June 2023.** "For **multi-tenant applications**, emails that aren't domain-owner verified are omitted by default when the optional `email` claim is requested in a token payload." Same four verified cases as above. [M10] Search-surfaced commentary adds that this default applies to apps registered after June 2023 and not to single-tenant apps or multi-tenant apps with prior unverified-email sign-in activity; that nuance is from Microsoft Q&A, not the reference page, and is recorded as such. [M11]
- `verified_primary_email` / `verified_secondary_email` optional claims exist ("Sourced from the user's PrimaryAuthoritativeEmail / SecondaryAuthoritativeEmail") with no further definition on the page. [M8]
- Per account type: work/school managed user → `email` only via optional claim or `email` scope, verification only via `xms_edov`; guest → `email` present by default, `xms_edov` per the four cases; personal Microsoft account (MSA, tenant `9188040d-…`) → counts as domain-verified for `xms_edov`. [M5][M8][M10]

### 3.4 Assurance claims

`acr` is in `claims_supported`; `auth_time` and `amr` are **optional claims** that must be requested per app registration (`amr` is in the v2.0-specific optional set; `multipleauthn`/`mfa` values "are emitted only when the user has completed MFA"). [M8] `prompt=login` "forces the user to enter their credentials on that request, which negates single sign-on." [M1]

### 3.5 Login-only parameters, PKCE, redirect URIs

- Authorize: `response_type=code`, `scope` must include `openid` (`profile`, `email` optional), `nonce` required for an ID token, `response_mode` ∈ `query` | `fragment` | `form_post` (Microsoft recommends `form_post` for web apps), `prompt` ∈ `login` | `none` | `consent` | `select_account`, `login_hint`, `domain_hint`. `redirect_uri` "must exactly match one of the redirect URIs you registered". Authorization codes expire "after about 1 minute". [M1][M12]
- PKCE: `code_challenge` "is now recommended for all application types, both public and confidential clients, and required… for single page apps"; `code_challenge_method` `S256` or `plain`; `code_verifier` "Required if PKCE was used in the authorization code grant request"; `invalid_grant` covers a bad verifier. Confidential web apps send `client_secret` (or a certificate assertion) at the token endpoint. [M12]
- Refresh token "Only provided if `offline_access` scope was requested." [M12]
- Redirect URIs [M13]: must begin with `https` except localhost (`http://localhost` and `http://127.0.0.1` via manifest; `[::1]` unsupported); case-sensitive; ≤256 characters each; ≤256 URIs for work/school audiences, ≤100 when personal accounts are included; query parameters allowed only for org-only audiences; no `! $ ' ( ) , ;`; no Internationalized Domain Names; wildcards unsupported when personal accounts are included and "strongly" discouraged otherwise; a URI without a path is returned with a trailing slash in `query`/`fragment` modes. Mismatch → `AADSTS50011`.

### 3.6 Client secrets

"Client secret lifetime is limited to two years (24 months) or less… Microsoft recommends that you set an expiration value of less than 12 months." The value "is *never displayed again* after you leave this page." Microsoft recommends certificates over secrets for production; the page does not state a cap on concurrent secrets. [M14]

### 3.7 Brand rules

"It's the association of the Microsoft logo and the "Sign in with Microsoft" terms that uniquely represent Microsoft Entra ID amongst other identity providers your app may support. If you don't have enough space for "Sign in with Microsoft," it's ok to shorten it to "Sign in." You can use a light or dark color scheme for the buttons." Official PNG/SVG assets (light/dark, long/short) are provided with redlines. DO use "work or school account" beside the button; DON'T use "enterprise account", "business account", "corporate account", "Microsoft 365 ID", "Azure ID"; DON'T alter the logo; DON'T expose end users to the Azure or Active Directory brands. [M15]

---

## 4. GitHub (OAuth 2.0 only)

### 4.1 No OIDC for users

GitHub publishes exactly one OpenID discovery document, `https://token.actions.githubusercontent.com/.well-known/openid-configuration` (issuer `https://token.actions.githubusercontent.com`, claims `repository`, `workflow`, `actor`, …) — GitHub Actions workload identity, not user sign-in. [H1] User sign-in is OAuth 2.0 with no ID token, no `iss`, no discovery. [H2]

### 4.2 Endpoints, PKCE, GHES

- github.com: `GET https://github.com/login/oauth/authorize`, `POST https://github.com/login/oauth/access_token`, REST at `https://api.github.com`. [H2]
- GitHub Enterprise Server: `GET http(s)://HOSTNAME/login/oauth/authorize`, `POST http(s)://HOSTNAME/login/oauth/access_token`, REST at `http(s)://HOSTNAME/api/v3`. [H3][H4] So the API host derives from the origin **only on GHES** (`<origin>/api/v3`); for `https://github.com` the profile must map to the separate host `https://api.github.com`.
- PKCE on github.com [H2][H5]: `code_challenge` "Must be a 43 character SHA-256 hash of a random string"; `code_challenge_method` "Must be `S256` - the `plain` code challenge method is not supported"; at the token endpoint `code_verifier` is "Required if `code_challenge` was sent during the user authorization. Must be the original value used to generate the `code_challenge`." Changelog (2025-07-14): "Only the S256 code challenge method is accepted." "GitHub is not requiring PKCE for any authentication flow at this time, as GitHub does not distinguish between public and confidential clients." Enforcement is documented as "Required if"; there is no published negative test beyond that sentence.
- **PKCE on GHES:** the 3.17 and 3.20 authorization docs both state "The PKCE (Proof Key for Code Exchange) parameters `code_challenge` and `code_challenge_method` are not supported at this time." [H3][H6] The changelog does not mention GHES. [H5]

### 4.3 Subject and verified email

- `GET /user` returns `id` (int64) and `login`; `id` is the durable identifier, `login` can change; `node_id` also present. `email` is `null` when the user keeps it private. `two_factor_authentication` appears only in the *private user* schema, i.e. only with the `user` scope. "OAuth app tokens… need the `user` scope in order for the response to include private profile information." [H7]
- Scopes [H8]: "(no scope) — Grants read-only access to public information (including user profile info, repository info, and gists)"; `read:user` — "Grants access to read a user's profile data"; `user:email` — "Grants read access to a user's email addresses"; `user` includes both. Scopes are normalised (`user:email` is dropped when `user` is present). **Minimal set for #580's flow: `user:email` alone** — `/user` needs no scope for `id` and `login`, and `/user/emails` needs exactly `user:email`; `read:user` adds nothing the flow reads.
- `GET /user/emails` [H9]: "OAuth app tokens and personal access tokens (classic) need the `user:email` scope"; each entry has `email`, `primary` (boolean), `verified` (boolean), `visibility` (`public` | `private` | `null`); paginated (`per_page` ≤ 100). GitHub App user access tokens need the "Email addresses" user permission (read). [H10] The API reference does not define `verified` beyond the boolean; account docs say verification means clicking a link GitHub mails to the address, that "Without a verified email address, you won't be able to use all of GitHub's features", and that the user may change the primary address at any time from Settings → Emails. [H11][H12] So `primary && verified` is one address at any instant and may be a different address at the next sign-up.

### 4.4 Login-only parameters, tokens, expiry

- Authorize parameters [H2]: `client_id`; `redirect_uri` ("strongly recommended" — it is **optional**, and when omitted "GitHub will redirect users to the first callback URL configured in the OAuth app settings", so Hikyo must always send it); `login` (suggests an account); `scope`; `state`; `allow_signup` (default `true`; `false` "when a policy prohibits signups"); `prompt=select_account` forces the account picker; PKCE pair. The code "will expire after 10 minutes".
- Token exchange [H2]: `client_id`, `client_secret`, `code`, `redirect_uri` ("We can use this to match against the URI originally provided when the `code` was issued, to prevent attacks"), `code_verifier`. Response is form-encoded by default; `Accept: application/json` returns JSON (`application/xml` also offered). Fields: `access_token`, `scope`, `token_type` (`bearer`), plus `expires_in` and `refresh_token` when tokens expire.
- Expiry [H2][H13][H14]: for new OAuth apps "**Expire user access tokens** is enabled by default" (opt-out available); with it "The access token expires after eight hours, and the refresh token expires after six months without use." Non-expiring tokens: "GitHub will automatically revoke an OAuth token or personal access token when the token hasn't been used in one year." Users can revoke an app's authorisation from account settings. None of this matters to a callback that discards the token, except that a refresh token *will* arrive and must be dropped.
- Device flow exists as an opt-in app setting ("Enable Device Flow"). [H13]

### 4.5 Callback URL constraints

"You can enter up to 10 callback URLs." [H13] Matching [H2]: "When wildcard matching is enabled, the redirect URL's host (excluding subdomains) and port must exactly match the callback URL, and the redirect URL's path must reference a subdirectory of the callback URL"; the example table shows `https://example.com/path` matching `https://example.com/path/subdir/other` and `https://oauth.example.com/path` but not `https://example.com/bar` or `https://example.com:8080/path`. Loopback: "OAuth RFC recommends not to use `localhost`, but instead to use loopback literal `127.0.0.1` or IPv6 `::1`", and a loopback callback may be redirected to with a different port. The docs do not state an HTTPS requirement for callback URLs, nor a public-suffix or IP-literal rule.

### 4.6 Client secrets

Settings offer "Generate a new client secret"; the docs fetched do not state how many secrets may be active at once or any expiry. [H15]

### 4.7 Rate limits

Primary: 5,000 requests/hour per authenticated user, "combined with any requests that another GitHub App or OAuth app makes on that user's behalf" (per user, not per app); 60/hour per IP unauthenticated; 15,000/hour for apps owned/approved by a GitHub Enterprise Cloud organisation. Secondary: ≤100 concurrent requests, ≤900 points/minute (GET = 1 point), ≤90 s CPU per 60 s. Headers `x-ratelimit-limit|remaining|used|reset`. [H16] A sign-up costs two GET calls against the *user's* budget. GHES: site administrators enable and configure API and secondary rate limits in the Management Console with "prefilled default limits"; the numbers are not published. [H17]

### 4.8 GitHub Apps vs OAuth Apps

"GitHub Apps are preferred to OAuth apps because they use fine-grained permissions, give more control over which repositories the app can access, and use short-lived tokens." Both identify users through the same web flow; GitHub App user tokens expire after 8 hours with refresh; OAuth App tokens are long-lived unless expiry is enabled (now the default for new apps); GitHub Apps use permissions (here: "Email addresses" read) instead of scopes. [H18][H10] For a sign-in-only integration the wire protocol is identical; the difference is the registration object and how email access is declared.

### 4.9 Brand rules

GitHub's logo page [H19]: permitted — "Use a permitted GitHub logo to inform others that your project integrates with GitHub", "to link to GitHub", "as a social button". Required — the logo must appear "less prominently than your own company or product name or logo". Prohibited — "Do not modify the permitted GitHub logos, including changing the color, dimensions, or combining with other words or design elements"; no use as your own icon/logo/domain; nothing "that suggests you are GitHub". Colour: "The Invertocat and our wordmark should only appear in white, black, or in few cases grey or green." No mandated button text. Other uses need written permission (`github.com/contact`).

---

## 5. Where Hikyo's existing OIDC code stands against these facts

Read from `internal/oidcrp/oidcrp.go` (main, 2026-09-02) and `web/src/routes/Login.tsx`:

- `Discover` = `oidc.NewProvider(ctx, issuer)` with a plain context → Entra `common`/`organizations` issuers cannot be discovered; tenant-specific issuers can. Google discovers fine.
- `Verify` re-checks `tok.Issuer != p.issuer` after go-oidc → Google's bare `accounts.google.com` form (tolerated by go-oidc) would be refused as `ErrIssuer`.
- `AuthCodeURL` always sends PKCE S256 and, for reauth, `prompt=login` + `max_age=0` — parameters Google does not document (§2.4) and Entra documents (§3.4).
- The login page renders one "Continue with `display_name`" button per `oidc` provider with no provider mark; Google's mandated strings and "G" asset (§2.7) and Microsoft's logo lockup (§3.7) are not satisfied by that rendering. GitHub's rules are satisfied by any unmodified black/white Invertocat that is smaller than Hikyo's own mark.
- `HIKYO_EXTERNAL_ORIGIN` is validated as an exact canonical origin (`internal/config/config.go`), which is the right input for Google's exact-match rule; Google additionally rejects IP-literal and non-public-suffix hosts that the Hikyo validator accepts.

---

## 6. Collision matrix — provider fact × locked rule

| Locked rule | Google | Microsoft Entra | GitHub (github.com) | GitHub Enterprise Server |
|---|---|---|---|---|
| **Byte-exact issuer** (human-auth; `oidcrp` belt check) | Discovery `https://accounts.google.com` matches. Token `iss` **may** be bare `accounts.google.com` per Google's docs; go-oidc allows it, Hikyo's belt check refuses it. | `common`/`organizations` discovery issuer is the literal `{tenantid}` placeholder → discovery fails without go-oidc's insecure hatch. Tenant-specific issuer `…/<tid>/v2.0` is byte-exact end to end. | N/A — no issuer; #580 uses the origin `https://github.com`. | Origin `https://<host>`. |
| **`email_verified == true` on sign-up** (#579 d5) | Claim exists, boolean, no account-type split. Google says it is *not authoritative* for non-Gmail addresses without `hd` even when true. | **No `email_verified` claim.** Nearest: optional `xms_edov` (needs `email` claim + app-registration config); unverified-domain emails omitted by default for new multi-tenant apps since June 2023. | No claim; `/user/emails` entry with `primary && verified` (#580 d4), scope `user:email`. | Same API at `<origin>/api/v3`. |
| **PKCE S256 always** (#580 d3; `oidcrp`) | `plain` + `S256` advertised. | `plain` + `S256`; recommended for confidential clients. | `S256` only; `code_verifier` "Required if `code_challenge` was sent"; not mandatory. | **Docs 3.17–3.20: not supported.** |
| **Mix-up defence: RFC 9207 `iss` or per-provider callback** | Discovery lacks `authorization_response_iss_parameter_supported`. | Same. | No `iss`; `redirect_uri` optional on GitHub's side. | Same. |
| **No offline scopes / token used once** (#580 d3) | Default `access_type=online` → no refresh token. | Refresh token only with `offline_access`. | Expiring-token default returns a refresh token with the access token; must be discarded. | Depends on instance config. |
| **OIDC reauth = `prompt=login` + `max_age=0` + `auth_time`** (human-auth) | `prompt` documented as `none|consent|select_account` only; `max_age`/`auth_time` not on the OIDC page. | `prompt=login` documented; `auth_time`/`amr` are optional claims to enable per registration. | N/A (#580: OAuth2 never reauths). | N/A. |
| **Public origin precondition** (#579 d6) | HTTPS (localhost exempt), no IP literal, public-suffix TLD, no wildcard, exact incl. trailing slash. Domain must be owned/verified for the consent screen. | HTTPS (localhost exempt), case-sensitive, no IDN, ≤256 chars, ≤256/100 URIs; publisher domain only for publisher verification. | Host (excl. subdomains) + port exact, path subdirectory when wildcard matching is on; ≤10 URLs; HTTPS not stated. | Same rules. |
| **Subject stability** (identity key) | `sub` global, never reused. | `sub` pairwise per `client_id` + tenant → new app registration = new identities; `oid` stable but is not `sub`. | `id` int64 stable; `login` mutable. | Same, per instance. |
| **One enabled provider per `(kind, issuer)`** | One Google row. | One row **per tenant** (distinct issuers); one app registration may back them all. | One row per origin. | One row per GHES host. |
| **Single-factor federated session** (#580 d5) | Google does not return `amr`; `acr` not in `claims_supported`. | `amr`/`acr` available (optional claim). | `two_factor_authentication` is account state, only in the private schema (`user` scope). | Same. |

---

## 7. Facts for downstream tickets (no decisions made here)

- **[#588](https://github.com/Hikyo-Org/Hikyo/issues/588) Entra multi-tenant issuer.** The placeholder issuer is in the `common` *and* `organizations` documents (§3.1). Tenant-specific documents carry the literal GUID and pass Hikyo's discovery unchanged. Microsoft's own rule is "metadata issuer matches `iss`" plus "`iss` contains `tid`". go-oidc's only relaxation is `InsecureIssuerURLContext`, self-described as for "off-spec providers such as Azure". A per-tenant row reuses one client registration; the customer side costs a first-sign-in consent (or admin consent where user consent is disabled). `sub` is pairwise per client registration and per tenant. Adjacent fact: Google's documented bare-`iss` form is the same rule class (§2.1) and is refused by Hikyo's belt check, not by go-oidc.
- **[#582](https://github.com/Hikyo-Org/Hikyo/issues/582) invitation claiming.** Nothing here needs an email call, consistent with #580 d4. Entra guests in a customer tenant present that tenant's `tid` and a tenant-specific `oid`/`sub` (§3.2) — an invitee who is a guest in the operator's tenant is a different identity from the same human in their home tenant.
- **[#584](https://github.com/Hikyo-Org/Hikyo/issues/584) local sign-up / [#579](https://github.com/Hikyo-Org/Hikyo/issues/579) predicate.** The "verified email" input is provider-shaped: Google boolean with the non-authoritative caveat (§2.3); Entra `xms_edov` in place of a nonexistent `email_verified` (§3.3); GitHub `primary && verified` (§4.3), an address the user can swap at will. Google's `hd` is the only provider-asserted organisation signal among the three; Entra's is `tid`.
- **[#586](https://github.com/Hikyo-Org/Hikyo/issues/586) linking / credential adding.** Entra's pairwise `sub` means a re-registered client cannot re-find existing links (§3.2). GitHub's `two_factor_authentication` needs the `user` scope, which the minimal set omits (§4.3) — consistent with #580 d5 refusing it anyway.
- **[#587](https://github.com/Hikyo-Org/Hikyo/issues/587) prototype.** Google's button text is one of three fixed strings with the standard-colour "G" and three fixed themes; "Continue with Google" is the one that fits Hikyo's current "Continue with …" label (§2.7). Microsoft's button is the logo + "Sign in with Microsoft" (or "Sign in") with "work or school account" helper text (§3.7). GitHub imposes only logo integrity, black/white, and subordinate prominence (§4.9). Google also requires the button to comply for app verification, and Google's consent screen shows the operator's verified brand, not Hikyo's.
- **[#596](https://github.com/Hikyo-Org/Hikyo/issues/596) CLI through federated providers.** Google's discovery advertises the `device_code` grant; Entra has a device-code flow; GitHub's device flow is a per-app opt-in (§4.4). All three also work through the browser code flow the #194 handoff already uses; none of the fetched docs restricts a browser-based callback to a public origin beyond §6's redirect rules (loopback exemptions exist at all three).
- **[#580](https://github.com/Hikyo-Org/Hikyo/issues/580) follow-ups.** Minimal scope = `user:email`; `read:user` not needed (§4.3). API host: `<origin>/api/v3` on GHES, `https://api.github.com` for `https://github.com` (§4.2). Token exchange: send `Accept: application/json` (§4.4). Always send `redirect_uri` (GitHub treats it as optional). `code_verifier` is documented "Required if `code_challenge` was sent". GHES documents no PKCE support through 3.20 (§4.2).
- **Synthesis ([#589](https://github.com/Hikyo-Org/Hikyo/issues/589)).** Provider facts that touch amendment text: Google's dual `iss` form vs the belt check; Entra's absent `email_verified`; Google's non-authoritative-email caveat; Google's undocumented `prompt=login`; Entra `auth_time`/`amr` being opt-in optional claims; per-tenant rows for Entra; Google's public-suffix/no-IP redirect rule as a tighter bound than `HIKYO_EXTERNAL_ORIGIN`'s validator; Google's six-month idle-client deletion.

---

## 8. Sources

Google
- [G1] Live discovery document, `https://accounts.google.com/.well-known/openid-configuration` (fetched 2026-09-02).
- [G2] OpenID Connect — Google Identity: <https://developers.google.com/identity/openid-connect/openid-connect>
- [G3] Verify the Google ID token on your server side: <https://developers.google.com/identity/gsi/web/guides/verify-google-id-token>
- [G4] Using OAuth 2.0 for Web Server Applications (incl. "Redirect URI validation rules"): <https://developers.google.com/identity/protocols/oauth2/web-server>
- [G5] OAuth 2.0 Policies: <https://developers.google.com/identity/protocols/oauth2/policies>
- [G6] Configure the OAuth consent screen / verification (Cloud Console help): <https://support.google.com/cloud/answer/13464321>
- [G7] Manage OAuth Clients (client secret rotation, hashed secrets, idle-client deletion): <https://support.google.com/cloud/answer/15549257>
- [G8] Sign in with Google branding guidelines: <https://developers.google.com/identity/branding-guidelines>

Microsoft Entra
- [M1] OpenID Connect on the Microsoft identity platform: <https://learn.microsoft.com/en-us/entra/identity-platform/v2-protocols-oidc>
- [M2] Live `common` discovery document, `https://login.microsoftonline.com/common/v2.0/.well-known/openid-configuration` (fetched 2026-09-02).
- [M3] Live `organizations` discovery document, `https://login.microsoftonline.com/organizations/v2.0/.well-known/openid-configuration` (fetched 2026-09-02).
- [M4] Live tenant-specific discovery document for `9188040d-6c67-4c5b-b112-36a304b66dad` (fetched 2026-09-02).
- [M5] ID token claims reference: <https://learn.microsoft.com/en-us/entra/identity-platform/id-token-claims-reference>
- [M6] Convert single-tenant app to multitenant ("Update your code to handle multiple issuer values", consent): <https://learn.microsoft.com/en-us/entra/identity-platform/howto-convert-app-to-be-multi-tenant>
- [M7] Publisher verification overview: <https://learn.microsoft.com/en-us/entra/identity-platform/publisher-verification-overview>
- [M8] Optional claims reference (`xms_edov`, `email`, `upn`, `amr`, `auth_time`): <https://learn.microsoft.com/en-us/entra/identity-platform/optional-claims-reference>
- [M9] UserInfo endpoint: <https://learn.microsoft.com/en-us/entra/identity-platform/userinfo>
- [M10] Updates and breaking changes — "June 2023: Omission of email claims with an unverified domain owner": <https://learn.microsoft.com/en-us/entra/identity-platform/reference-breaking-changes>
- [M11] Microsoft Q&A threads surfaced by search on the June 2023 default (secondary; used only for the "apps registered after June 2023 / prior activity" nuance): <https://learn.microsoft.com/en-us/answers/questions/2122133/how-can-i-include-an-email-claim-in-an-open-id-tok>
- [M12] OAuth 2.0 authorization code flow (PKCE rows, token endpoint, error codes): <https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow>
- [M13] Redirect URI (reply URL) best practices and limitations: <https://learn.microsoft.com/en-us/entra/identity-platform/reply-url>
- [M14] Add and manage app credentials: <https://learn.microsoft.com/en-us/entra/identity-platform/how-to-add-credentials>
- [M15] Sign in with Microsoft branding guidelines: <https://learn.microsoft.com/en-us/entra/identity-platform/howto-add-branding-in-apps>

GitHub
- [H1] Live Actions OIDC discovery, `https://token.actions.githubusercontent.com/.well-known/openid-configuration` (fetched 2026-09-02).
- [H2] Authorizing OAuth apps (github.com): <https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps>
- [H3] Authorizing OAuth apps — GitHub Enterprise Server 3.17: <https://docs.github.com/en/enterprise-server@3.17/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps>
- [H4] REST API quickstart — GitHub Enterprise Server 3.17 (`http(s)://HOSTNAME/api/v3`): <https://docs.github.com/en/enterprise-server@3.17/rest/quickstart>
- [H5] Changelog 2025-07-14, "PKCE support for OAuth and GitHub App authentication": <https://github.blog/changelog/2025-07-14-pkce-support-for-oauth-and-github-app-authentication/>
- [H6] Authorizing OAuth apps — GitHub Enterprise Server 3.20: <https://docs.github.com/en/enterprise-server@3.20/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps>
- [H7] REST: Get the authenticated user: <https://docs.github.com/en/rest/users/users?apiVersion=2022-11-28>
- [H8] Scopes for OAuth apps: <https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps>
- [H9] REST: Emails (list email addresses for the authenticated user): <https://docs.github.com/en/rest/users/emails?apiVersion=2022-11-28>
- [H10] Permissions required for GitHub Apps ("Email addresses"): <https://docs.github.com/en/rest/authentication/permissions-required-for-github-apps?apiVersion=2022-11-28>
- [H11] Verifying your email address: <https://docs.github.com/en/account-and-profile/setting-up-and-managing-your-personal-account-on-github/managing-email-preferences/verifying-your-email-address>
- [H12] Changing your primary email address: <https://docs.github.com/en/account-and-profile/setting-up-and-managing-your-personal-account-on-github/managing-email-preferences/changing-your-primary-email-address>
- [H13] Creating an OAuth app (callback URLs, device flow, "Expire user access tokens"): <https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/creating-an-oauth-app>
- [H14] Token expiration and revocation: <https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/token-expiration-and-revocation>
- [H15] Best practices for creating an OAuth app: <https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/best-practices-for-creating-an-oauth-app>
- [H16] Rate limits for the REST API: <https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api?apiVersion=2022-11-28>
- [H17] Configuring rate limits — GitHub Enterprise Server 3.17: <https://docs.github.com/en/enterprise-server@3.17/admin/configuring-settings/configuring-user-applications-for-your-enterprise/configuring-rate-limits>
- [H18] Differences between GitHub Apps and OAuth apps: <https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/differences-between-github-apps-and-oauth-apps>
- [H19] GitHub brand — logo usage: <https://brand.github.com/foundations/logo>

Libraries
- [L1] `coreos/go-oidc` v3 `oidc/verify.go` (issuer check, Google special case, `SkipIssuerCheck`): <https://github.com/coreos/go-oidc/blob/v3/oidc/verify.go>
- [L2] `coreos/go-oidc` v3 `oidc/oidc.go` (`InsecureIssuerURLContext`, `IssuerMismatchError`): <https://github.com/coreos/go-oidc/blob/v3/oidc/oidc.go>
