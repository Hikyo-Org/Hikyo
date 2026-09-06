# Hikyo: social sign-in and open registration (handoff spec, synthesis 2026-09-03)

> **Declared mail-configuration amendment (2026-09-06):** the [approved self-configuration design](./self-configuration-proposal.md) extends section 7's mail transport with a managed source. Before adoption, external mail inputs are the seed. After atomic adoption, an explicitly applied project snapshot exclusively owns the supported mail settings, including managed `HIKYO_MAIL_PASSWORD` and managed-only `HIKYO_MAIL_CA_PEM`. Password/trust files are imported once; apply cannot read arbitrary files. SMTP TLS, egress policy, local-only validation, explicit test, deadlines, intent/outcome and no-outbox semantics remain unchanged. Managed credentials participate in ordinary project encryption, re-encryption and backups. Restore invalidates acknowledgements and fences outbound use until reconciliation and explicit confirmation/re-entry plus apply, superseding section 7's process-only exemption. Unrelated sign-up, registration policy and invitation work remains owned by its original tickets.

> **Pre-1.0 amendment (2026-09-05, #617):** Automatic OIDC account creation is retired before the API freeze. Migration `00044_retire_oidc_provisioning.sql` removes its provider policy on both engines. Section 2.3 is now a no-op. Organisation rename already uses `manage-members@org`; delete remains instance administration. Existing protocol-kind, OIDC/SAML-purpose and grant-origin wire enums are open without adding future values. Migration numbers 00042 and 00043 below are historical planning placeholders already consumed by MCP; implementation tickets must allocate the next available pair.

The destination of wayfinder map [#578](https://github.com/Hikyo-Org/Hikyo/issues/578), produced at [#589](https://github.com/Hikyo-Org/Hikyo/issues/589). Google, Microsoft Entra and GitHub sign-in beside local credentials and generic OIDC, plus operator-enabled open registration, designed as the seam a hosted deployment could later use. Self-hosted first; positioning unchanged.

**How this document works.** Every decision lives in exactly one ticket resolution (linked below) and is bound into the ADRs by the 2026-09-03 amendment banners in [human-auth](../adr/human-auth.md), [tenant-isolation](../adr/tenant-isolation.md), [audit-model](../adr/audit-model.md), [permission-model](../adr/permission-model.md), [threat-model](../adr/threat-model.md), [ops-spec](../adr/ops-spec.md), [mvp-boundary](../adr/mvp-boundary.md) (declared amendment 4, criterion A7) and [api-cli-surface](../adr/api-cli-surface.md). This document indexes those decisions and discharges what they delegated to synthesis: DDL on both engines, wire spellings, ceremony order, UI references, mailer configuration, e2e pitfalls, and the sequenced implementation tickets. Where wording here diverges from a banner or a ticket resolution, the banner wins and the contradiction reopens [#589](https://github.com/Hikyo-Org/Hikyo/issues/589). Read every ticket through its dated amendment lines; the final state is what binds. Section 14 lists the clarifications the synthesis itself had to make (each posted as a dated amendment line on the owning ticket).

## 1. Decision index

| Area | Ticket | Gist |
|---|---|---|
| Registration policy | [#579](https://github.com/Hikyo-Org/Hikyo/issues/579) | One row per scope (0..1 org, 0..1 instance), replaces retired automatic provisioning, landing by scope, standing delegation with an authority principal, preconditions at write and use, `signup` budget, `registration.*` audit, reauth-gated mutations, editor on Members |
| OAuth2 kind (GitHub) | [#580](https://github.com/Hikyo-Org/Hikyo/issues/580) | Third kind `oauth2` with profile enum, key `(oauth2, origin, numeric id)`, own tables and package, PKCE always, per-provider callback, `primary && verified` email on sign-up only, always single-factor, never reauth, purpose `establish` |
| Provider facts | [#581](https://github.com/Hikyo-Org/Hikyo/issues/581) | [social-providers.md](../research/social-providers.md) |
| Invitation claim | [#582](https://github.com/Hikyo-Org/Hikyo/issues/582) | Purpose `claim` spends the credential-establishment authority through a federated round-trip; token first on the establish page; atomic consume + bind + session; first credential only; no provider pin; `identity-exists` leaves the authority unspent |
| Mailer facts | [#583](https://github.com/Hikyo-Org/Hikyo/issues/583) | [mailer-seam.md](../research/mailer-seam.md) |
| Local sign-up | [#584](https://github.com/Hikyo-Org/Hikyo/issues/584) | Pending row + reaper, fragment token, `accounts.email`, always 202 with one send, charge at request, verification writes everything and mints no session, go-mail over TLS, text-only English mail |
| Fresh-org landing | [#585](https://github.com/Hikyo-Org/Hikyo/issues/585) | Authority principal is the caller, one serialized transaction with fixed order, `org-<id>` naming or a local `org_name`, rename = `manage-members(org)`, `orgs.origin`, per-policy cap |
| Linking and local factors | [#586](https://github.com/Hikyo-Org/Hikyo/issues/586) | Social-only is a steady state; `establish` proves local-credential creation only via a session stamp; `PUT /auth/password` set-or-change; unlink predicate; kind-agnostic link |
| Surfaces | [#587](https://github.com/Hikyo-Org/Hikyo/issues/587) | Staged entry, iteration 2 locked; intent is a server fact; inactive cause off the public page; brand rules |
| Issuer departures and reauth | [#588](https://github.com/Hikyo-Org/Hikyo/issues/588) | Entra tenant-specific rows only; Google bare `iss` tolerated by the library, Hikyo belt deleted; pairwise `client_id` guard; reauth policy-gated at start; no `form_post` |
| CLI | [#596](https://github.com/Hikyo-Org/Hikyo/issues/596) | Login handoff (unbuilt) rides unchanged, always `sign-in`; `--provider` preselect; establish, link, claim browser-only; step-up refusal before the browser |
| Verified email | [#598](https://github.com/Hikyo-Org/Hikyo/issues/598) | Quality bar; fixed set `email_verified` or `xms_edov` boolean `true` beside `email`; ID token only; write-time `email` scope; cause `no-verified-email` |
| Intent | [#604](https://github.com/Hikyo-Org/Hikyo/issues/604) | `intent` field on the `login` start, absent = `sign-in`; known identity signs in under either; unknown branches; budget charged only for unknown + `sign-up`; start stays policy-blind |

## 2. Data model (both engines)

Two migrations, each present on both engines and following the existing rebuild discipline (sqlite recreates a table whose CHECK changes and carries rows across; postgres mirrors every rebuild that carries a `hikyo:table` directive, and may `ALTER` where the sqlite side also alters). Historical migration files are never edited; corrected commentary lives in the new files' headers and in this document.

- **`00042_social_signin.sql`** (ticket T1): purely **additive and backward-compatible**. New tables; nullable new columns; widened CHECKs that admit the old values; no column drops; no CHECK that today's code would violate. Existing OIDC login keeps working with this migration applied and no other change.
- **`00043_registration_switch.sql`** (ticket T3, deployed with the code that honours it): the `intent`-required CHECK on `oidc_transactions`, and the live-transaction purge.

Column types follow the file conventions. Every new table carries its `hikyo:table … class=… chain=…` directive on both engines. Both dialects are given for the new tables; for altered tables the per-engine form is stated in the table.

### 2.1 New tables (00042)

**Provider selection scope.** A sign-up always names the scope whose policy admits it: the instance, or one org. The federated start and the local request carry `signup_scope` (`instance` | an org id); the transaction and the pending row persist it; the callback or verification resolves *that* scope's policy and re-checks it live. The start remains policy-blind (it records the scope, reads no policy). The public login page offers the instance scope; an org's sign-up page is the same surface addressed with the org id (`/signup?org=<id>`); an unknown org resolves to "no policy" and refuses uniformly as `closed`.

sqlite:

```sql
-- hikyo:table registration_policies class=authn chain=-
CREATE TABLE registration_policies (
    id TEXT PRIMARY KEY,
    org_id TEXT REFERENCES orgs (id) ON DELETE CASCADE,        -- NULL = instance scope
    authority_principal_id TEXT REFERENCES principals (id),     -- NULL only for the historical fold row
    landing TEXT NOT NULL CHECK (landing IN ('org-template', 'none', 'fresh-org')),
    template TEXT,
    local_enabled INTEGER NOT NULL CHECK (local_enabled IN (0, 1)),
    fresh_org_cap INTEGER,
    row_version INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK ((org_id IS NOT NULL AND landing = 'org-template' AND template IS NOT NULL)
        OR (org_id IS NULL AND landing IN ('none', 'fresh-org') AND template IS NULL)),
    CHECK ((landing = 'fresh-org') = (fresh_org_cap IS NOT NULL)),
    CHECK (fresh_org_cap IS NULL OR fresh_org_cap > 0)
);
CREATE UNIQUE INDEX registration_policies_org ON registration_policies (org_id) WHERE org_id IS NOT NULL;
CREATE UNIQUE INDEX registration_policies_instance ON registration_policies (landing IS NOT NULL) WHERE org_id IS NULL;

-- hikyo:table registration_policy_domains class=authn chain=-
-- The local entry's domain allowlist, one row per domain (no JSON arrays: the
-- database refuses empty strings, duplicates and non-strings by shape).
CREATE TABLE registration_policy_domains (
    policy_id TEXT NOT NULL REFERENCES registration_policies (id) ON DELETE CASCADE,
    domain TEXT NOT NULL CHECK (typeof(domain) = 'text' AND domain <> '' AND domain = lower(domain)),
    PRIMARY KEY (policy_id, domain)
);

-- hikyo:table registration_policy_entries class=authn chain=-
CREATE TABLE registration_policy_entries (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES registration_policies (id) ON DELETE CASCADE,
    provider_kind TEXT NOT NULL CHECK (provider_kind IN ('oidc', 'oauth2')),
    provider_id TEXT NOT NULL,                                  -- the provider row id, never the slug
    claim TEXT CHECK (claim IS NULL OR (claim <> '' AND claim <> 'email')),
    created_at TEXT NOT NULL,
    UNIQUE (policy_id, provider_kind, provider_id)
);

-- hikyo:table registration_policy_entry_values class=authn chain=-
-- Accepted values of an entry's allowlist claim, one row per value. A row may
-- exist only for an entry with a claim; the writer refuses a claim with no
-- values and values without a claim, and a conformance test pins both.
CREATE TABLE registration_policy_entry_values (
    entry_id TEXT NOT NULL REFERENCES registration_policy_entries (id) ON DELETE CASCADE,
    value TEXT NOT NULL CHECK (typeof(value) = 'text' AND value <> ''),
    PRIMARY KEY (entry_id, value)
);

-- hikyo:table registration_signups class=authn chain=-
-- One live row per canonical address. Verification DELETES the row it
-- consumes (#584 d1); the reaper prunes expired rows; a request against an
-- expired row replaces it in the same transaction. policy_id is a trail
-- pointer without cascade: deleting a policy deletes its pending rows
-- explicitly, inside the delete transaction, emitting signup_expired.
CREATE TABLE registration_signups (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    token_verifier BLOB NOT NULL UNIQUE,
    policy_id TEXT NOT NULL,
    signup_scope_org_id TEXT,                                   -- NULL = instance scope
    credential_epoch INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX registration_signups_expiry ON registration_signups (expires_at);
CREATE INDEX registration_signups_policy ON registration_signups (policy_id);

-- hikyo:table oauth2_providers class=authn chain=-
CREATE TABLE oauth2_providers (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind = 'oauth2'),
    profile TEXT NOT NULL CHECK (profile IN ('github')),
    issuer TEXT NOT NULL,                                       -- canonical origin, immutable
    client_id TEXT NOT NULL,
    client_secret BLOB NOT NULL,
    redirect_uri TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    dek_version INTEGER NOT NULL,
    row_version INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX oauth2_providers_issuer_enabled ON oauth2_providers (kind, issuer) WHERE enabled = 1;

-- hikyo:table oauth2_transactions class=authn chain=-
-- oidc_transactions minus nonce (#580 d3), plus intent and signup scope
-- (#604, section 2.1) and authority_id (#582). Purposes: login | link |
-- establish | claim; reauth is refused before a row exists (#580 d6). One
-- exhaustive CHECK per purpose fixes the binding kind and the bound fields.
CREATE TABLE oauth2_transactions (
    id TEXT PRIMARY KEY,
    state_verifier BLOB NOT NULL UNIQUE,
    pkce_verifier TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES oauth2_providers (id) ON DELETE CASCADE,
    issuer TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('login', 'link', 'establish', 'claim')),
    intent TEXT CHECK (intent IN ('sign-in', 'sign-up')),
    signup_scope_org_id TEXT,
    binding_kind TEXT NOT NULL CHECK (binding_kind IN ('session', 'browser-cookie')),
    initiating_session_id TEXT,
    browser_binding_verifier BLOB,
    account_id TEXT,
    authority_id TEXT REFERENCES credential_authorities (id),
    ceremony_id TEXT,
    browser INTEGER NOT NULL DEFAULT 0 CHECK (browser IN (0, 1)),
    credential_epoch INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    CHECK ((binding_kind = 'session' AND initiating_session_id IS NOT NULL AND browser_binding_verifier IS NULL)
        OR (binding_kind = 'browser-cookie' AND initiating_session_id IS NULL AND browser_binding_verifier IS NOT NULL)),
    CHECK (
        (purpose = 'login' AND binding_kind = 'browser-cookie' AND intent IS NOT NULL
            AND account_id IS NULL AND ceremony_id IS NULL AND authority_id IS NULL
            AND (intent = 'sign-up' OR signup_scope_org_id IS NULL))
     OR (purpose = 'link' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NOT NULL AND authority_id IS NULL)
     OR (purpose = 'establish' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'claim' AND binding_kind = 'browser-cookie' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NULL AND authority_id IS NOT NULL)
    )
);
```

postgres (the mirror, exact):

```sql
-- hikyo:table registration_policies class=authn chain=-
CREATE TABLE registration_policies (
    id TEXT PRIMARY KEY,
    org_id TEXT REFERENCES orgs (id) ON DELETE CASCADE,
    authority_principal_id TEXT REFERENCES principals (id),
    landing TEXT NOT NULL CHECK (landing IN ('org-template', 'none', 'fresh-org')),
    template TEXT,
    local_enabled BOOLEAN NOT NULL,
    fresh_org_cap BIGINT,
    row_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK ((org_id IS NOT NULL AND landing = 'org-template' AND template IS NOT NULL)
        OR (org_id IS NULL AND landing IN ('none', 'fresh-org') AND template IS NULL)),
    CHECK ((landing = 'fresh-org') = (fresh_org_cap IS NOT NULL)),
    CHECK (fresh_org_cap IS NULL OR fresh_org_cap > 0)
);
CREATE UNIQUE INDEX registration_policies_org ON registration_policies (org_id) WHERE org_id IS NOT NULL;
CREATE UNIQUE INDEX registration_policies_instance ON registration_policies ((org_id IS NULL)) WHERE org_id IS NULL;

-- hikyo:table registration_policy_domains class=authn chain=-
CREATE TABLE registration_policy_domains (
    policy_id TEXT NOT NULL REFERENCES registration_policies (id) ON DELETE CASCADE,
    domain TEXT NOT NULL CHECK (domain <> '' AND domain = lower(domain)),
    PRIMARY KEY (policy_id, domain)
);

-- hikyo:table registration_policy_entries class=authn chain=-
CREATE TABLE registration_policy_entries (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES registration_policies (id) ON DELETE CASCADE,
    provider_kind TEXT NOT NULL CHECK (provider_kind IN ('oidc', 'oauth2')),
    provider_id TEXT NOT NULL,
    claim TEXT CHECK (claim IS NULL OR (claim <> '' AND claim <> 'email')),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (policy_id, provider_kind, provider_id)
);

-- hikyo:table registration_policy_entry_values class=authn chain=-
CREATE TABLE registration_policy_entry_values (
    entry_id TEXT NOT NULL REFERENCES registration_policy_entries (id) ON DELETE CASCADE,
    value TEXT NOT NULL CHECK (value <> ''),
    PRIMARY KEY (entry_id, value)
);

-- hikyo:table registration_signups class=authn chain=-
CREATE TABLE registration_signups (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    token_verifier BYTEA NOT NULL UNIQUE,
    policy_id TEXT NOT NULL,
    signup_scope_org_id TEXT,
    credential_epoch BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX registration_signups_expiry ON registration_signups (expires_at);
CREATE INDEX registration_signups_policy ON registration_signups (policy_id);

-- hikyo:table oauth2_providers class=authn chain=-
CREATE TABLE oauth2_providers (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind = 'oauth2'),
    profile TEXT NOT NULL CHECK (profile IN ('github')),
    issuer TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret BYTEA NOT NULL,
    redirect_uri TEXT NOT NULL,
    enabled BIGINT NOT NULL CHECK (enabled IN (0, 1)),          -- BIGINT as oidc_providers.enabled, so the partial index is verbatim
    dek_version BIGINT NOT NULL,
    row_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX oauth2_providers_issuer_enabled ON oauth2_providers (kind, issuer) WHERE enabled = 1;

-- hikyo:table oauth2_transactions class=authn chain=-
CREATE TABLE oauth2_transactions (
    id TEXT PRIMARY KEY,
    state_verifier BYTEA NOT NULL UNIQUE,
    pkce_verifier TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES oauth2_providers (id) ON DELETE CASCADE,
    issuer TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('login', 'link', 'establish', 'claim')),
    intent TEXT CHECK (intent IN ('sign-in', 'sign-up')),
    signup_scope_org_id TEXT,
    binding_kind TEXT NOT NULL CHECK (binding_kind IN ('session', 'browser-cookie')),
    initiating_session_id TEXT,
    browser_binding_verifier BYTEA,
    account_id TEXT,
    authority_id TEXT REFERENCES credential_authorities (id),
    ceremony_id TEXT,
    browser BOOLEAN NOT NULL DEFAULT FALSE,
    credential_epoch BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    CHECK ((binding_kind = 'session' AND initiating_session_id IS NOT NULL AND browser_binding_verifier IS NULL)
        OR (binding_kind = 'browser-cookie' AND initiating_session_id IS NULL AND browser_binding_verifier IS NOT NULL)),
    CHECK (
        (purpose = 'login' AND binding_kind = 'browser-cookie' AND intent IS NOT NULL
            AND account_id IS NULL AND ceremony_id IS NULL AND authority_id IS NULL
            AND (intent = 'sign-up' OR signup_scope_org_id IS NULL))
     OR (purpose = 'link' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NOT NULL AND authority_id IS NULL)
     OR (purpose = 'establish' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'claim' AND binding_kind = 'browser-cookie' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NULL AND authority_id IS NOT NULL)
    )
);
```

No JSON-typed policy column exists: domains and claim values are child rows, so the database itself refuses empty strings, duplicates and non-text values (sqlite's `typeof` CHECK closes its affinity loophole; postgres `TEXT` is typed). The one cross-table rule (an entry has values iff it has a claim) cannot be a CHECK on either engine and the codebase uses no triggers; it is the writer's, pinned by a conformance test on both engines, the same disposition as `principals.class` (enforced in Go, fail-closed). Codex round 3 asked for a database-level cardinality guard; left for human disposition (section 14.13).

### 2.2 Altered tables (exact statements)

**`oidc_transactions` (00042, both engines rebuild; sqlite shown, postgres mirrors with `BYTEA`/`BIGINT`/`TIMESTAMPTZ`/`BOOLEAN` and the same CHECKs):**

```sql
CREATE TABLE oidc_transactions_new (
    id TEXT PRIMARY KEY,
    state_verifier BLOB NOT NULL UNIQUE,
    nonce BLOB NOT NULL,
    pkce_verifier TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES oidc_providers (id) ON DELETE CASCADE,
    issuer TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('login', 'link', 'reauth', 'establish', 'claim')),
    intent TEXT CHECK (intent IN ('sign-in', 'sign-up')),
    signup_scope_org_id TEXT,
    binding_kind TEXT NOT NULL CHECK (binding_kind IN ('session', 'browser-cookie')),
    initiating_session_id TEXT,
    browser_binding_verifier BLOB,
    account_id TEXT,
    environment_id TEXT,
    ceremony_id TEXT,
    authority_id TEXT REFERENCES credential_authorities (id),
    browser INTEGER NOT NULL DEFAULT 0 CHECK (browser IN (0, 1)),
    credential_epoch INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    CHECK ((binding_kind = 'session' AND initiating_session_id IS NOT NULL AND browser_binding_verifier IS NULL)
        OR (binding_kind = 'browser-cookie' AND initiating_session_id IS NULL AND browser_binding_verifier IS NOT NULL)),
    CHECK (
        (purpose = 'login' AND binding_kind = 'browser-cookie'
            AND account_id IS NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NULL
            AND (intent = 'sign-up' OR signup_scope_org_id IS NULL))
     OR (purpose = 'link' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NOT NULL AND environment_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'reauth' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NOT NULL AND ceremony_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'establish' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'claim' AND binding_kind = 'browser-cookie' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NOT NULL)
    )
);
INSERT INTO oidc_transactions_new
    (id, state_verifier, nonce, pkce_verifier, provider_id, issuer, redirect_uri, purpose, intent, signup_scope_org_id,
     binding_kind, initiating_session_id, browser_binding_verifier, account_id, environment_id, ceremony_id, authority_id,
     browser, credential_epoch, created_at, expires_at, consumed_at)
SELECT id, state_verifier, nonce, pkce_verifier, provider_id, issuer, redirect_uri, purpose, NULL, NULL,
     binding_kind, initiating_session_id, browser_binding_verifier, account_id, environment_id, ceremony_id, NULL,
     browser, credential_epoch, created_at, expires_at, consumed_at
FROM oidc_transactions;
DROP TABLE oidc_transactions;
ALTER TABLE oidc_transactions_new RENAME TO oidc_transactions;
```

postgres 00042 for `oidc_transactions`:

```sql
-- hikyo:table oidc_transactions_new class=authn chain=-
CREATE TABLE oidc_transactions_new (
    id TEXT PRIMARY KEY,
    state_verifier BYTEA NOT NULL UNIQUE,
    nonce BYTEA NOT NULL,
    pkce_verifier TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES oidc_providers (id) ON DELETE CASCADE,
    issuer TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('login', 'link', 'reauth', 'establish', 'claim')),
    intent TEXT CHECK (intent IN ('sign-in', 'sign-up')),
    signup_scope_org_id TEXT,
    binding_kind TEXT NOT NULL CHECK (binding_kind IN ('session', 'browser-cookie')),
    initiating_session_id TEXT,
    browser_binding_verifier BYTEA,
    account_id TEXT,
    environment_id TEXT,
    ceremony_id TEXT,
    authority_id TEXT REFERENCES credential_authorities (id),
    browser BOOLEAN NOT NULL DEFAULT FALSE,
    credential_epoch BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    CHECK ((binding_kind = 'session' AND initiating_session_id IS NOT NULL AND browser_binding_verifier IS NULL)
        OR (binding_kind = 'browser-cookie' AND initiating_session_id IS NULL AND browser_binding_verifier IS NOT NULL)),
    CHECK (
        (purpose = 'login' AND binding_kind = 'browser-cookie'
            AND account_id IS NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NULL
            AND (intent = 'sign-up' OR signup_scope_org_id IS NULL))
     OR (purpose = 'link' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NOT NULL AND environment_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'reauth' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NOT NULL AND ceremony_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'establish' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'claim' AND binding_kind = 'browser-cookie' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NOT NULL)
    )
);
INSERT INTO oidc_transactions_new
    (id, state_verifier, nonce, pkce_verifier, provider_id, issuer, redirect_uri, purpose, intent, signup_scope_org_id,
     binding_kind, initiating_session_id, browser_binding_verifier, account_id, environment_id, ceremony_id, authority_id,
     browser, credential_epoch, created_at, expires_at, consumed_at)
SELECT id, state_verifier, nonce, pkce_verifier, provider_id, issuer, redirect_uri, purpose, NULL, NULL,
     binding_kind, initiating_session_id, browser_binding_verifier, account_id, environment_id, ceremony_id, NULL,
     browser, credential_epoch, created_at, expires_at, consumed_at
FROM oidc_transactions;
DROP TABLE oidc_transactions;
ALTER TABLE oidc_transactions_new RENAME TO oidc_transactions;
```

00042 first runs `DELETE FROM oidc_transactions;` (rows are minutes-lived, and today's `link` rows may carry an empty rather than NULL `environment_id`, which the exhaustive CHECK would refuse), then the rebuild; `intent` stays nullable here so today's `login` writer keeps working.

**`oidc_transactions` (00043, both engines):** `DELETE FROM oidc_transactions;` then the identical rebuild statements above (table `oidc_transactions_new`, same columns, same `INSERT … SELECT` copying `intent` and `signup_scope_org_id` through instead of NULL) with the `login` arm of the purpose CHECK reading `(purpose = 'login' AND binding_kind = 'browser-cookie' AND intent IS NOT NULL AND …)`.

**`oidc_providers`: no registration-switch migration.** Issue #617 already removed the retired provider policy with migration 00044. Do not rebuild this table, drop any column, or recreate the issuer index for that retirement. The provider and its existing identity/session references remain in place.

**`external_identities` (00042):**

```sql
-- sqlite: the 00010 shape
ALTER TABLE external_identities RENAME TO external_identities_old;
CREATE TABLE external_identities (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts (id),
    kind TEXT NOT NULL CHECK (kind IN ('oidc', 'saml', 'oauth2')),
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    credential_epoch INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (kind, issuer, subject)
);
INSERT INTO external_identities (id, account_id, kind, issuer, subject, provider_id, credential_epoch, created_at)
SELECT id, account_id, kind, issuer, subject, provider_id, credential_epoch, created_at FROM external_identities_old;
DROP TABLE external_identities_old;
-- postgres
ALTER TABLE external_identities DROP CONSTRAINT external_identities_kind_check;
ALTER TABLE external_identities ADD CONSTRAINT external_identities_kind_check CHECK (kind IN ('oidc', 'saml', 'oauth2'));
```

**`sessions` (00042, both engines rebuild, the 00020 shape):**

```sql
-- sqlite
DELETE FROM reauth_windows;
CREATE TABLE sessions_rebuilt (
    id TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    verifier BLOB NOT NULL UNIQUE,
    artifact TEXT NOT NULL CHECK (artifact IN ('cli', 'browser', 'workspace')),
    session_generation INTEGER NOT NULL,
    credential_epoch INTEGER NOT NULL,
    auth_method TEXT NOT NULL,
    factors TEXT NOT NULL,
    authenticated_at TEXT NOT NULL,
    ceremony_id TEXT,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    idle_expires_at TEXT NOT NULL,
    absolute_expires_at TEXT NOT NULL,
    source_ip TEXT NOT NULL,
    user_agent TEXT NOT NULL,
    csrf_verifier BLOB,
    provider_id TEXT REFERENCES oidc_providers (id) ON DELETE CASCADE,
    saml_provider_id TEXT REFERENCES saml_providers (id) ON DELETE CASCADE,
    oauth2_provider_id TEXT REFERENCES oauth2_providers (id) ON DELETE CASCADE,
    requesting_origin TEXT,
    handoff_id TEXT,
    CHECK ((provider_id IS NOT NULL) + (saml_provider_id IS NOT NULL) + (oauth2_provider_id IS NOT NULL) <= 1),
    CHECK (
        (artifact = 'workspace' AND requesting_origin IS NOT NULL AND handoff_id IS NOT NULL)
        OR (artifact <> 'workspace' AND requesting_origin IS NULL AND handoff_id IS NULL)
    )
);
INSERT INTO sessions_rebuilt
    (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at,
     ceremony_id, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent, csrf_verifier,
     provider_id, saml_provider_id, oauth2_provider_id, requesting_origin, handoff_id)
SELECT id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at,
     ceremony_id, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent, csrf_verifier,
     provider_id, saml_provider_id, NULL, requesting_origin, handoff_id
FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_rebuilt RENAME TO sessions;
CREATE INDEX sessions_principal_idx ON sessions (principal_id);
CREATE INDEX sessions_origin_idx ON sessions (requesting_origin);
```

postgres 00042 for `sessions`:

```sql
DELETE FROM reauth_windows;
-- hikyo:table sessions_rebuilt class=authn chain=-
CREATE TABLE sessions_rebuilt (
    id TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    verifier BYTEA NOT NULL UNIQUE,
    artifact TEXT NOT NULL CHECK (artifact IN ('cli', 'browser', 'workspace')),
    session_generation BIGINT NOT NULL,
    credential_epoch BIGINT NOT NULL,
    auth_method TEXT NOT NULL,
    factors TEXT NOT NULL,
    authenticated_at TIMESTAMPTZ NOT NULL,
    ceremony_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    idle_expires_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    source_ip TEXT NOT NULL,
    user_agent TEXT NOT NULL,
    csrf_verifier BYTEA,
    provider_id TEXT REFERENCES oidc_providers (id) ON DELETE CASCADE,
    saml_provider_id TEXT REFERENCES saml_providers (id) ON DELETE CASCADE,
    oauth2_provider_id TEXT REFERENCES oauth2_providers (id) ON DELETE CASCADE,
    requesting_origin TEXT,
    handoff_id TEXT,
    CONSTRAINT sessions_one_federated_provider
    CHECK (num_nonnulls(provider_id, saml_provider_id, oauth2_provider_id) <= 1),
    CHECK (
        (artifact = 'workspace' AND requesting_origin IS NOT NULL AND handoff_id IS NOT NULL)
        OR (artifact <> 'workspace' AND requesting_origin IS NULL AND handoff_id IS NULL)
    )
);
INSERT INTO sessions_rebuilt
    (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at,
     ceremony_id, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent, csrf_verifier,
     provider_id, saml_provider_id, oauth2_provider_id, requesting_origin, handoff_id)
SELECT id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at,
     ceremony_id, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent, csrf_verifier,
     provider_id, saml_provider_id, NULL, requesting_origin, handoff_id
FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_rebuilt RENAME TO sessions;
CREATE INDEX sessions_principal_idx ON sessions (principal_id);
CREATE INDEX sessions_origin_idx ON sessions (requesting_origin);
```

**`credential_authorities` (00042, both engines rebuild, the 00037 shape):**

```sql
CREATE TABLE credential_authorities_new (
    id TEXT PRIMARY KEY,
    verifier BLOB NOT NULL UNIQUE,
    account_id TEXT NOT NULL REFERENCES accounts (id),
    purpose TEXT NOT NULL CHECK (purpose IN ('establish-credential')),
    issued_by TEXT NOT NULL CHECK (issued_by IN ('bootstrap', 'credential-reset', 'break-glass', 'recovery', 'invitation')),
    established_credential_kind TEXT NOT NULL DEFAULT 'password' CHECK (established_credential_kind IN ('password', 'oidc', 'oauth2')),
    credential_epoch INTEGER NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    created_at TEXT NOT NULL,
    CHECK (issued_by <> 'recovery' OR established_credential_kind = 'password')
);
INSERT INTO credential_authorities_new
    (id, verifier, account_id, purpose, issued_by, established_credential_kind, credential_epoch, expires_at, consumed_at, created_at)
SELECT id, verifier, account_id, purpose, issued_by, established_credential_kind, credential_epoch, expires_at, consumed_at, created_at
FROM credential_authorities;
DROP TABLE credential_authorities;
ALTER TABLE credential_authorities_new RENAME TO credential_authorities;
```

postgres: the same block with `verifier BYTEA NOT NULL UNIQUE`, `credential_epoch BIGINT NOT NULL`, `expires_at TIMESTAMPTZ NOT NULL`, `consumed_at TIMESTAMPTZ`, `created_at TIMESTAMPTZ NOT NULL`, preceded by `-- hikyo:table credential_authorities_new class=authn chain=-`.

**`orgs`, `accounts` (00042, both engines `ALTER`):**

```sql
ALTER TABLE orgs ADD COLUMN origin TEXT NOT NULL DEFAULT 'manual' CHECK (origin IN ('manual', 'registration'));
ALTER TABLE orgs ADD COLUMN registration_policy_id TEXT;
ALTER TABLE accounts ADD COLUMN email TEXT;
CREATE UNIQUE INDEX accounts_email ON accounts (email) WHERE email IS NOT NULL;
```

**`grant_origins` (00042):**

```sql
-- sqlite
CREATE TABLE grant_origins_new (
    id TEXT PRIMARY KEY,
    grant_id TEXT NOT NULL REFERENCES grants (id),
    kind TEXT NOT NULL CHECK (
        kind IN ('manual', 'break-glass', 'scim', 'structural', 'lockout-retention', 'registration')
    ),
    subject TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (grant_id, kind, subject)
);
INSERT INTO grant_origins_new (id, grant_id, kind, subject, created_at)
SELECT id, grant_id, kind, subject, created_at FROM grant_origins;
DROP TABLE grant_origins;
ALTER TABLE grant_origins_new RENAME TO grant_origins;
CREATE INDEX grant_origins_grant ON grant_origins (grant_id);
-- postgres (mirrors the rebuild because the sqlite side carries a hikyo:table grant_origins_new directive; same block with TIMESTAMPTZ)
```

`subject` for `registration` = the authority principal id.

Migration order inside 00042: `oauth2_providers` before `sessions` (FK), `registration_policies` before `registration_policy_domains` and `registration_policy_entries` before `registration_policy_entry_values`, `credential_authorities` before `oauth2_transactions` and the `oidc_transactions` rebuild (FK on `authority_id`).

Commentary corrections (recorded in the 00042 header, never by editing 00005/00007): "there is no self-registration, ever, and none is representable here" is superseded by the registration policy; "there is no email column, ever" is superseded by `accounts.email` as a login identifier for verified local sign-up only; email is still never a linking key.

### 2.3 Retired automatic-provisioning fold: no-op

Issue #617 removes the provider policy before this feature ships. The registration switch must not read or drop the retired column, create a folded policy, or remove audit entries again. Existing provisioned accounts and explicit identity links remain intact; only the provider policy is retired. Allocate fresh migration numbers because MCP already uses 00042 and 00043.

### 2.4 Go domain additions

`domain.OriginRegistration = "registration"`; `crypto.ArtifactSignup ArtifactType = "su"` (hashed verifier at rest, redacted like `hs`/`hc`); `service.ResetLifetime` reused for the sign-up token; `IdentityProviderKind` gains `oauth2`; `OAuth2Kind = "oauth2"` beside `OIDCKind`; a provider reference type `{kind, slug}` for every surface that names a provider (2.6).

### 2.5 Email canonical form

`accounts.email` and `registration_signups.email` store the canonical addr-spec: parsed by `net/mail.ParseAddress`; a display name, comment, or group is refused; the address must be `local@domain` with an ASCII-only domain (IDNA refused in v1, named refusal); the **domain part is lowercased**, the local part is byte-preserved; a trailing dot on the domain is refused; length ≤ 254 bytes. Domain-allowlist matching compares the lowercased domain. Uniqueness is over this canonical string. Recorded as a synthesis decision (section 14).

### 2.6 Provider references

Slugs are unique per provider table only. Every request, response and flag that names a provider carries `{kind, slug}` (`AuthMethodProvider` already carries `kind`; `signup_methods` entries carry both; policy entries store `(provider_kind, provider_id)`); `hikyo login --provider` takes `<kind>:<slug>` and accepts a bare `<slug>` only when it is unambiguous across the enabled providers, refusing "ambiguous provider slug" otherwise.

## 3. Wire surface and pins

Spellings are in [api-cli-spellings.md § 8](./api-cli-spellings.md); every addition rides the pinned contract chain (`api/openapi.yaml` → `api/oapi-codegen.yaml` → `api/apigen` → handler → `AuthService` seam → `contract_test.go` stub → `api/noproxy_test.go`), gets a `api/parity.yaml` row in the same commit, and a row in `internal/isolation/testdata/operation_formulas.json` for each new operation.

### 3.1 OpenAPI deltas

- `OidcStartRequest.purpose` enum: `[login, link, reauth, establish, claim]`; new optional `intent: [sign-in, sign-up]` (valid only with `login`; absent = `sign-in`); new optional `signup_org` (org id; valid only with `intent: sign-up`; absent = instance scope); `proof` description widens: the pre-existing proof for `link` (password or TOTP code per `proofSelection`), the credential-establishment authority for `claim`.
- New `Oauth2StartRequest` (same shape, purpose enum `[login, link, establish, claim]`) and `POST /api/v1/auth/oauth2/{provider}/start`, `GET /api/v1/auth/oauth2/{provider}/callback` (`x-hikyo-class: unauthenticated`, parity `exception: identity-protocol`).
- `IdentityProviderKind` enum gains `oauth2`; `AuthMethodProvider` gains optional `profile` (`[github]`); `AuthMethods` gains `signup_open: boolean` and `signup_methods: [{kind, slug} | local]` for the instance scope; `GET /api/v1/auth/methods?org=<id>` returns the org scope's door (unknown org = closed, uniform).
- `AuthMethod` extensible enum documents `oauth2:<origin>`.
- `PUT /api/v1/auth/password` `{password, proof?}` → `LoginResult` (reissued session), `operationId: setPassword`, parity `webui: settings`.
- `POST /api/v1/auth/signup {email, org?}` → `202`; `POST /api/v1/auth/signup/verify {token, password, display_name, org_name?}` → `204`; both `security: []`, `x-hikyo-class: unauthenticated`, parity `exception: identity-protocol` (pre-auth provisioning ceremony endpoints; the api-cli-surface banner admits them by name).
- Registration policy: `GET|PUT|DELETE /api/v1/orgs/{org}/registration-policy` (`x-hikyo-formula: [manage-members@org]`, tenant class) and `/api/v1/instance/registration-policy` (`[manage-members@instance]`, instance class); `PUT` and `DELETE` carry `x-hikyo-reauth: account-security`; parity `webui: members` / `webui: instance-members`.
- OAuth2 providers: `/api/v1/instance/oauth2-providers` and `/{slug}` mirroring the SAML admin block (`instance-config@instance`); parity `webui: instance-admin`.
- `Org` schema gains required `origin` and nullable `registration_policy_id`; `GET /api/v1/orgs` gains `?origin=` filter; `PATCH /api/v1/orgs/{org}` formula moves to `manage-members@org`.
- `GET /api/v1/instance/mail` `{configured}` and `POST /api/v1/instance/mail/test {to}` (`instance-config@instance`, reauth; bounds in section 8); parity `webui: instance-settings`.
- `EstablishCredentialRequest` description: issuers list gains `recovery` (pre-existing doc/DDL asymmetry, fixed in passing).

### 3.2 Operation registry rows

`registration-policy.get|put|delete` at org (tenant class, level org, `manage-members@org`) and instance (instance class, `manage-members@instance`); `oauth2-provider.list|get|create|update|delete` (instance, `instance-config@instance`); `auth.set-password` (unauthenticated class, human-session artifact); `mail.test` (instance, `instance-config@instance`); `org.rename` formula becomes `manage-members@org` (pin update in `operation_formulas.json`); sign-up request and verification are unauthenticated-class operations with `events:` declared (the audit completeness invariant refuses `audited: none` for them). Budget classification (`budget_classification.go`): `signup` is a new named category charged at the points section 4 fixes; `mail.test` takes its own row; every other new operation takes the default or is exempt by the totality map's existing rules.

## 4. Ceremonies (order is normative)

**Federated login start** (`oidc` or `oauth2`, purpose `login`): admission → provider snapshot (enabled) → purpose validation (`reauth` on `oauth2`, or on an `oidc` row with a NULL assurance policy, refused here by name with the remedy, no row written) → `intent ⇒ purpose = login`, `signup_org ⇒ intent = sign-up` (`ErrBadPurpose` shape) → transaction row (`intent` stored, absent = `sign-in`; `signup_scope_org_id`) → redirect. No policy read.

**Federated login callback**: transaction by state, single-use CAS → provider revalidation (`GuardProviderForMint`) → token exchange only at the recorded provider (`oidc`: full ID-token validation by go-oidc, issuer check unconditional, no Hikyo belt; `oauth2`: PKCE verifier sent, `Accept: application/json`, then `/user` once) → epoch → instance-wide `(kind, issuer, subject)` resolution (the tenant-isolation third member; intent-blind) → **known**: mint session (assurance per policy for `oidc`, single-factor for `oauth2`), `auth.*_login {intent}` → **unknown and `sign-in`**: `auth.*_refused {cause: unknown-identity, intent}`, uniform response, no charge → **unknown and `sign-up`**: resolve the scope's policy: **admission gate before any charge** (policy row present and active, and this provider is an entry; failure → `registration.signup_refused {closed | precondition | authority-lost | predicate}`, uniform response, **uncharged**) → `signup` budget (overflow → `budget`) → verified-email assertion (`oidc`: `email_verified` or `xms_edov` boolean `true` beside non-empty `email` from `claims.Raw`; `oauth2`: `/user/emails` `primary && verified`; failure → `no-verified-email`, `verified_by: none`) → claim allowlist (`predicate`) → `fresh-org` cap (`cap`) → write (`tx.Write`, or `tx.WriteSerialized` for `fresh-org`): authority principal re-authorized for the landing (`applyTemplate` at scope, or `org.create` + admin template) → create principal + account (`username = oidc-<id>` / `oauth2-<id>`, `email` NULL) → create identity under UNIQUE (`identity-exists` refuses here, before any org row) → org row (`origin = registration`, `registration_policy_id`, name `org-<org id>`) → template grant (origin `registration`, subject = authority principal) → session → events `registration.signup_admitted` (actor unauthenticated), `settings.org_created` (actor = authority principal), `registration.signup_completed {account_id, landing, org_id?}` (actor = new principal), `auth.*_login`. The wire response and its timing are identical between a known login and a fresh sign-up; every refusal cause shares one uniform shape.

The admission gate before the charge is a synthesis reorder of [#604](https://github.com/Hikyo-Org/Hikyo/issues/604) decision 3 (section 14): a forged `sign-up` against a closed scope or an unadmitted provider must not drain the instance-wide budget; the gate reads one policy row, discloses nothing the door does not already render, and the response stays uniform.

**Local sign-up request** (`POST /auth/signup {email, org?}`): `Admission.Enter` → canonicalize the address (2.5; malformed → uniform `202`, audited `predicate`) → resolve the scope's policy: present, active, `local_enabled`, mailer configured (static predicate), domain admitted; failure → uniform `202`, `registration.signup_refused` by cause, **uncharged** → `signup` budget (overflow → uniform `429`, the one non-202 answer, shared with every pre-auth path) → **one write transaction**: existing verified `accounts.email` → no row, mark "existing"; live pending row for the address → re-issue the token on that row (resend); expired pending row → replace it; none → insert; write the durable `registration.mail_intent {kind: verification | existing-address, recipient, policy_id}` event → commit → **exactly one synchronous send** (verification mail with `#token=<su artifact>&landing=<none|org|fresh-org>` and the org id for an org scope, or the "you already have an account" mail with no link) → `registration.mail_outcome {intent_id, outcome: success | failure}` written after the dial returns (a crash between commit and this event leaves an intent with no outcome, the same shape the adapter outbox records; the resend path is the retry) → `202` always. The `signup_admitted` / `signup_refused {email-exists}` event is written in the same transaction as the intent and carries no delivery result; delivery lives on `mail_outcome` only. Budget is charged once per request and never refunded; no latch.

**Local verification** (`POST /auth/signup/verify`): `CheckPassword` loud first → admission → token grammar (`su`) → phase-1 checks in a read transaction (`malformed | unknown | expired | epoch-superseded` → uniform 401, `registration.signup_refused`; a consumed token is deleted with its row and therefore reads `unknown`, section 14) → derive Argon2id outside any transaction → one write transaction: policy live re-check by the row's `policy_id` (row exists and active, local entry enabled, domain still admitted, authority still holds the template grant or `org.create`) → CAS-delete the pending row (zero rows = lost the race, uniform 401) → principal + account (`username = local-<id>`, `email`, `display_name`) → password credential → landing (org template, or fresh org with `org_name` from the form when the hint was `fresh-org`, else `org-<id>`; `orgs.name` UNIQUE hit → 400 on `org_name`, transaction aborted, token unspent) → `registration.signup_completed`. `204`; no session; the SPA redirects to login. Deleting a policy deletes its pending rows inside the delete transaction, emitting `registration.signup_expired {cause: policy-deleted}` per row.

**Claim** (`/establish` page): start with purpose `claim` and the authority in the proof slot → `EstablishCredential` phase-1 checks without consuming (`auth.credential_authority_refused` by cause on failure); an authority with `issued_by = recovery` is refused here (cause `purpose`), keeping the recovery ⇒ password CHECK intact (section 14) → `authority_id` on the transaction, `browser-cookie` binding → callback: re-run the checks on the live row, precondition *account holds no credential of any kind* (else `purpose`), CAS `consumed_at`, `established_credential_kind = oidc | oauth2`, create identity (`identity-exists` refuses **without** consuming), mint session (no reauth window) → `auth.credential_established {established_credential_kind, identity_id, provider_id, kind}` + `auth.*_login {purpose: claim}`.

**Establish**: start with purpose `establish` on the acting session (`binding_kind = session`) → callback: same `(kind, issuer, subject)` as an identity of the acting account (else `unknown-identity`), precondition *no local proof credential* (else `purpose`) → stamp the session with an `establish` evidence record (purpose `account-security`, TTL 5 min) → `auth.credential_establish`. Password set, TOTP enrol, passkey enrol and recovery-code regeneration accept an empty proof slot when the stamp is live **and** the precondition still holds inside the write transaction; `reissueSession` spends the stamp.

**Link / unlink**: proof by `proofSelection` (password, else confirmed TOTP; never `establish`); `oauth2` links through its own start with purpose `link`; unlink refuses `ErrLastCredential` iff no password, no passkey set satisfying the passkey-only invariant, and no other external identity would remain; sessions through the identity deleted.

**Password set-or-change** (`PUT /auth/password`): `CheckPassword` → proof (`proofSelection` or live establish stamp) → seal → write with session deletion and reissue → `auth.password_changed {first, authorizing_credential}`.

**Provider PUT guard** (`oidc`): a `client_id` change on a row whose discovery advertises `subject_types_supported` = pairwise only and which has ≥1 linked identity is refused by name (`ErrIssuerImmutable` shape).

**Operator test send** (`POST /instance/mail/test`): `instance-config@instance` ∧ reauth → its bound (section 8) → `registration.mail_intent {kind: test, recipient}` → send → `registration.mail_outcome`. The only reachability probe.

**CLI**: `hikyo login <url> [--provider <kind>:<slug>]` opens the login page under the (to-be-built) RFC 8252 login handoff with the hint; the page hides the sign-up door and sends `sign-in`; approval binds the browser session; redeem mints an `ArtifactCLISession` snapshotting `auth_method`, `factors`, provider id, and records the issuing handoff id. `account password set|change`, `account factor enrol-totp` refuse by name on a social-only account and print the Settings › Security URL. Step-up on a session whose method is `oauth2:` or a policy-less `oidc:` with no local factor refuses before opening the browser, naming the remedy; the browser reauth page (`CLIReauth.tsx`) gains the same branch in place of `offersOIDC`. Link, establish, claim and sign-up have no CLI verb: they are identity-protocol ceremonies in the api-cli-surface exception class (banner 2026-09-03), a documented exemption, not a parity gap.

## 5. Audit registry deltas

New types (all `security` class): `registration.policy_created | policy_updated | policy_deleted` (tenant or instance trail by scope; payload `policy_id`, `landing`, `authority_principal_id`, `previous_authority_principal_id?`), `registration.signup_admitted`, `registration.signup_refused` (cause enum in the audit-model banner; the token causes are `malformed | unknown | expired | epoch-superseded`), `registration.signup_completed`, `registration.signup_expired {cause: expired | policy-deleted}`, `registration.mail_intent {kind: verification | existing-address | test, recipient, policy_id?}` (outcome `intent`, joining the intent-outcome licence beside `adapter.push_intent` and `audit.export_started`) and `registration.mail_outcome {intent_id, outcome}` (the durable INTENT/OUTCOME pair around every SMTP dial; outcomes `success | failure`); `signup_admitted` carries no `delivery` field (it is written before the dial); `auth.oauth2_login`, `auth.oauth2_refused` (cause enum per #580 d9 minus `no-verified-email`, plus `identity-exists`), `auth.credential_establish`, `auth.password_changed`. Field additions: `intent`, `signup_org?` and `purpose` on `auth.oidc_login|oidc_refused|oauth2_login|oauth2_refused`; `established_credential_kind`, `identity_id`, `provider_id`, `kind` on `auth.credential_established`; `origin` (required), `policy_id` on `settings.org_created`; `authorizing_credential` gains `establish` on `auth.factor_enrolled`, `auth.passkey_added`, `auth.recovery_codes_generated`. Provider-provisioning audit removal already shipped in #617. `auth.oidc_refused` cause `identity-exists` added; `issuer` narrowed to the row-versus-transaction check (an ID-token issuer mismatch audits as `signature`, the callback's default token-validation cause; accepted name, no new cause). Every row lands with a real emitter, asserted by `every_registered_type_is_actually_emitted`.

## 6. UI surfaces

Reference: the locked prototype family `docs/site/public/prototypes/social-signin/2/` (index row in [ui-spec.md](./ui-spec.md)). Surfaces and the navigation ids they extend: `login` (staged entry, sign-up door for the instance scope or the addressed org, confirmation, check-your-mail), `signup-verify` (new public chromeless route `/signup/verify`), `establish-credential` (provider buttons beside the password form, one refusal sentence), `members` and `instance-members` (Open registration panel + editor, inactive-with-cause, `n / cap`, write-time 400 naming the row, the org's sign-up link to copy), `settings` › Account & security (Set/Change password, "Verify with <provider>" establish branch with the five-minute status line, link/unlink refusal copy, identities by display name + kind/profile, a provider picker in the link dialog), `instance-settings` (mailer line + test send), `instance-admin` (OAuth2 provider CRUD beside OIDC/SAML; org list with `origin` column and filter), org settings (rename). Brand rules: Google standard-colour G with "Continue with Google" / "Sign up with Google"; Microsoft logo + "Sign in with Microsoft · <tenant>" on every surface with the "work or school account" hint; GitHub Invertocat white/black subordinate to the Hikyo mark; generic OIDC "Continue with <display_name>". No remote fonts. Playwright flows per the S3 closed registry: `login.spec.ts` gains the staged entry and federated doors; new `signup.spec.ts` (request, mail sink, verify, fresh-org name); `members.spec.ts` gains the policy editor; `account-security.spec.ts` gains the establish branch and password set; axe both schemes.

## 7. Mailer configuration

Variables (all registered in `knownEnv`, validated by name at boot, never dialed): `HIKYO_MAIL_ADDR` (`host:port`, required to enable), `HIKYO_MAIL_TLS` (`implicit` | `starttls`, required), `HIKYO_MAIL_CA_FILE` (PEM, optional), `HIKYO_MAIL_USER` (optional), `HIKYO_MAIL_PASSWORD_FILE` or `HIKYO_MAIL_PASSWORD` (exactly one when a user is set; env is the documented weakest tier), `HIKYO_MAIL_FROM` (RFC 5322, required), `HIKYO_MAIL_EHLO` (hostname, optional), `HIKYO_MAIL_ALLOWED_CIDRS` (comma list, optional; the only way a non-global relay address is dialed). Library `github.com/wneessen/go-mail` v0.8.1 (adds a direct `go.mod` entry; `golang.org/x/text` promoted from indirect) behind a Hikyo `internal/mail` package that pins the posture in a conformance test: TLS mandatory, `MinVersion` 1.2, `ServerName` = host, no skip-verify, `WithDialContextFunc(netpolicy.PublicDialer)`, per-send deadline 15 s via `DialAndSendWithContext`. Templates: embedded `text/template`, English, text-only; two mails (verification with link; existing-address notice without link); the body and link are never logged or audited. Test sink: in-repo `internal/mailtest` over `emersion/go-smtp` with a TLS listener and a generated CA the test passes through `HIKYO_MAIL_CA_FILE`; no cleartext path in any mode. `scripts/ci/no-egress.sh` runs unchanged (no mailer configured = no dial) and gains a second run with a mailer configured to assert boot still originates zero connections. "Mailer configured" = the static predicate of [mailer-seam.md § 7.3](../research/mailer-seam.md); the operator test send is the only reachability probe.

## 8. Operational values

Catalogued in [ops-catalogue.md](./ops-catalogue.md): `signup` 20/h instance-wide; sign-up token 24 h; establish window 5 min; send deadline 15 s; operator test send **5/hour per principal, 1 concurrent per instance** (its own budget category `mail-test`); reaper on the hourly scheduler run; fresh-org cap 100 per policy; the Entra, Google and GitHub app-registration recipes. Bound registry (`internal/conformance/boundregistry_test.go`) gains rows for the `signup` budget, the `mail-test` budget, the sign-up token lifetime, the establish window, the send deadline and the fresh-org cap, each with an executable fixture.

## 9. e2e and implementation pitfalls

- **Two engines for every CHECK**: sqlite rebuilds; postgres mirrors when a `hikyo:table` directive is involved; run `go test ./internal/store/...` and the isolation suite on both.
- **00042 must ship alone safely**: no CHECK it adds may be violated by today's writers (`intent` stays nullable until 00043); the retired provider policy is already absent (#617).
- **Live transactions across 00043**: delete `oidc_transactions` rows in the migration rather than backfill (minutes-lived), and say so in the migration comment.
- **`sessions` rebuild** deletes `reauth_windows` as 00020 did; sessions survive; every open reauth window closes. Release note.
- **Order inside the federated sign-up write**: identity row before org row, or an `identity-exists` refusal leaks an org; `WriteSerialized` on the fresh-org branch only.
- **Admission gate before charge, budget before any write** (section 4); the gate must not change the wire shape or timing between causes.
- **Uniform wire on every refusal**: sign-up request always `202` (except the shared pre-auth `429`); verification, claim, callbacks return the existing uniform `ErrUnauthenticated` shape; the cause lives in the trail. The `org_name` collision is the single loud exception, by decision.
- **Fragment token**: the SPA reads `location.hash`; the server never sees the token on a GET; the verify POST carries it in the body.
- **`/user/emails`** is called only on the `sign-up` branch of an unknown `oauth2` identity, never on login, link, establish or claim; the access token is dropped after the callback; any refresh token is discarded; scopes are `user:email` only.
- **Google**: `prompt=login` and `max_age` are never sent (reauth refused at start when the row has no policy); go-oidc's bare-`iss` tolerance is the issuer check; `oidcrp.Verify`'s belt line and `ErrIssuer` are deleted; `Claims.Issuer` returns the pinned issuer.
- **Entra**: tenant GUID issuer rows; discovery error for `common`/domain-name forms surfaces go-oidc's `IssuerMismatchError` text, which names the GUID issuer to use.
- **Establish stamp**: principal-bound, session-bound, never inherited by a reissued session; one mutation per round-trip; TTL bounds an unused stamp.
- **Handoff page**: the login page under a CLI handoff or device approval must hide the sign-up door and send `sign-in`; there is no server marker, so a Playwright flow asserts the door is absent.
- **`sessions.auth_method` has no CHECK**; `oauth2:<origin>` is enforced in Go and in the contract enum; `CLIReauth.tsx` and `AccountSecurity.tsx` key on the `oidc:` prefix and the `IdentityProviderKind` union and must be widened.
- **`AccountSecurity.tsx` takes `providers[0]`** at the link entry; the link dialog must let the user pick the provider by `{kind, slug}`.
- **Parity**: every new operation gets its `parity.yaml` row in the same commit; `oauth2Callback`, `signupRequest`, `signupVerify` are `exception: identity-protocol`; the class predicate in `parity_test.go` must admit them (unauthenticated class, `security: []`).
- **Audit emitter closure**: a `registration.*` or `auth.oauth2_*` type declared without a lifecycle in `audit_e2e_test.go` fails `every_registered_type_is_actually_emitted`; add `runRegistrationLifecycle`, including a mail intent with no outcome (simulated crash).
- **Google redirect rules** are tighter than the `HIKYO_EXTERNAL_ORIGIN` validator (public-suffix host, no IP literal); the docs runbook says so; Hikyo does not enforce Google's rule.
- **GitHub Enterprise Server** rows: refuse `profile = github` with a non-`github.com` origin until PKCE support is verified for the target version (research §4.2), a named write-time refusal.

## 10. Fog dispositions

- **SCIM interplay** (was Not yet specified): discharged by the locked SCIM text, no new decision. A SCIM create whose `(kind, issuer, subject)` already exists attaches the existing account (scim-provisioning § 5.2), so a self-served federated account later pushed by an IdP is attached, not duplicated; the reverse (a SCIM pre-linked account signing in) is the known-identity path and is never a sign-up, whatever the intent. SCIM deprovision releases that binding's org grants and advances the generation; a self-served org's `registration`-origin grants are not SCIM origins and survive, exactly as manual grants do, under the existing "IdP deprovisioned this user; manual grants remain" flag. Nothing touches fresh orgs.
- **Self-service for self-served accounts** (leaving or deleting an org you signed up into, deleting your own account, the fate of a fresh org whose only admin leaves): **ruled out of scope** on the map. It is account-lifecycle work no locked ADR carries (no account deletion path exists by design, #584 d1 and #585 d9), it is not among the destination's enumerated areas, and it returns as its own effort with the instance org list's origin filter and `OpOrgDelete` as the operator's tools meanwhile. Reversible by the owner.

## 11. Residuals recorded, not fixed

- Possession-first proof preference (human-auth letter) is implemented nowhere; `proofSelection` is password-first across every mutation (#586 d5).
- ops-spec § 4 credential-reset lifetime row (1 h) versus the code's `ResetLifetime = BootstrapLifetime` (24 h); editorial amendment on its own ticket.
- `EstablishCredentialRequest` description omits the `recovery` issuer the DDL admits; fixed in passing by T6.
- Google's non-authoritative `email_verified` caveat for non-Gmail addresses without `hd`: recorded, not enforced (#598 d1).
- Entra tenant rows top out at the app registration's redirect-URI cap; the `{tenantid}` template is the named reopen trigger (#588).
- Engine microtiming on the third resolution member (tenant-isolation's standing residual).
- A crash between the pending-row commit and the SMTP dial leaves a `mail_intent` with no `mail_outcome`; the resend path is the retry, and no outbox exists by decision (#584 d6).

## 12. Implementation preconditions

1. **Login handoff and device flow** (api-cli-surface, locked, unbuilt): CLI social sign-in waits on them; `hikyo login --local` stays the floor. T8 below; a dependency of the A7 closure (T9), since A7 names CLI social sign-in.
2. **Member invitation (#568)**: merged; `claim` builds on its authority.
3. **Prototype PR #603** merged (this spec is stacked on it).

## 13. Implementation tickets (sequenced; `ready-for-agent`, one `model:*` label each)

Routing scheme, as evidenced by the label set and prior handoffs: `model:opus-4.8` for feature work reviewed by Codex `gpt-5.6-sol` high; `model:gpt-5.6` for front-end polish reviewed by Opus high; `model:fable-5` for the security-critical auth core. Every ticket: DCO, both engines, Codex R1–R3 high (cap 3), preview verified, handoff doc under `docs/handoff/<n>-…md`, no em-dash in new text.

| # | Ticket | Label | Blocked by |
|---|---|---|---|
| T1 [#605](https://github.com/Hikyo-Org/Hikyo/issues/605) | Migration **00042** (additive only) + domain types: section 2.1 tables, nullable columns, widened CHECKs, `accounts.email`, `orgs.origin`, `grant_origins.registration`, `su` artifact, `ArtifactType` + redaction pin, 2.5 canonicalizer, 2.6 provider reference; directive parity; existing OIDC login unchanged with only this applied | `model:fable-5` | none |
| T2 [#606](https://github.com/Hikyo-Org/Hikyo/issues/606) | Registration policy service + API + CLI + Members editor: standing delegation with authority re-check, preconditions at write and use (mailer predicate stubbed to "unconfigured"), `signup` budget category, `registration.policy_*` events, `AuthMethods.signup_open/signup_methods` incl. `?org=`, inactive-with-cause, `n / cap`, org sign-up link; no legacy policy fold or re-save path (#617) | `model:opus-4.8` | T1 [#605](https://github.com/Hikyo-Org/Hikyo/issues/605) |
| T3 [#607](https://github.com/Hikyo-Org/Hikyo/issues/607) | Migration **00043** + federated sign-up on the OIDC kind: `intent` + `signup_org` on start/transaction, `completeLogin` branch (unknown-by-intent, admission gate, budget, verified-email assertion for `oidc`, allowlist, write with landing incl. fresh org under the authority principal, `org-<id>`), `registration.signup_*` events, tenant-isolation third member + fixture pin, `settings.org_created` widening, org rename formula and surface already shipped in #617, org list origin filter, staged login entry + sign-up door + confirmation copy (prototype iteration 2), Google bare-`iss` belt deletion, reauth refused at start for a NULL policy, pairwise `client_id` guard | `model:fable-5` | T2 [#606](https://github.com/Hikyo-Org/Hikyo/issues/606) |
| T4 [#608](https://github.com/Hikyo-Org/Hikyo/issues/608) | Mailer + local sign-up: `internal/mail` with the posture test, `HIKYO_MAIL_*`, static predicate wired into T2's precondition, `internal/mailtest`, `POST /auth/signup` (+ `org`), `/auth/signup/verify`, pending row (replace expired, resend), reaper, mail intent/outcome events, fragment-driven verification page with `org_name`, check-your-mail (under T3's staged door), `email-exists` mail, `accounts.email` login resolution (`@` rules), instance mailer line + test send with its `mail-test` budget, no-egress second run | `model:fable-5` | T3 [#607](https://github.com/Hikyo-Org/Hikyo/issues/607) |
| T5 [#609](https://github.com/Hikyo-Org/Hikyo/issues/609) | OAuth2 kind (GitHub): `internal/oauth2rp`, provider + transaction service, start/callback, admin API + CLI + WebUI (mirror SAML), `sessions.oauth2_provider_id`, single-factor assurance, reauth refused at start, `/user/emails` on sign-up only, `auth.oauth2_*` events, login button with brand rules, `CLIReauth.tsx` and `AccountSecurity.tsx` kind widening, GHES write-time refusal | `model:fable-5` | T3 [#607](https://github.com/Hikyo-Org/Hikyo/issues/607) |
| T6 [#610](https://github.com/Hikyo-Org/Hikyo/issues/610) | Claim: purpose `claim` on both kinds, `authority_id`, phase-1 reuse without consumption, recovery-issued authority refused, atomic consume + bind + session, `established_credential_kind` recording, `identity-exists` without consumption, establish page provider buttons and single refusal sentence, `auth.credential_established` widening, `EstablishCredentialRequest` doc fix | `model:fable-5` | T5 [#609](https://github.com/Hikyo-Org/Hikyo/issues/609) |
| T7 [#611](https://github.com/Hikyo-Org/Hikyo/issues/611) | Establish + local factors for social-only accounts: purpose `establish` on both kinds, session stamp (5 min), empty-proof acceptance with the live precondition, `PUT /auth/password` + CLI verb + Security panel, unlink predicate fix, `proofSelection` on link/unlink, `oauth2` link via its own start, provider picker, `auth.credential_establish` + `auth.password_changed`, `authorizing_credential: establish`, step-up refusal copy (browser + CLI) | `model:fable-5` | T5 [#609](https://github.com/Hikyo-Org/Hikyo/issues/609) |
| T8 [#612](https://github.com/Hikyo-Org/Hikyo/issues/612) | Login handoff + device flow (api-cli-surface § Login transports; pre-existing debt): RFC 8252 loopback listener, `hikyo login <url>` and `--device`, approval page, hidden sign-up door + `sign-in` under a handoff, `--provider <kind>:<slug>` preselect, CLI session snapshot with issuing handoff id, `meta.protocol_capabilities` transports | `model:opus-4.8` | T3 [#607](https://github.com/Hikyo-Org/Hikyo/issues/607) |
| T9 [#613](https://github.com/Hikyo-Org/Hikyo/issues/613) | A7 acceptance closure: S3 Playwright flows, bound-registry rows with fixtures, `runRegistrationLifecycle` in the audit e2e, both-engine full run, parity rows verified, docs site pages (operator runbooks, mailer), release notes; status ledger evidence | `model:opus-4.8` | T2 to T8 |

Ticket bodies (the `ready-for-agent` shape of #151) are filed: T1 #605, T2 #606, T3 #607, T4 #608, T5 #609, T6 #610, T7 #611, T8 #612, T9 #613; each links this document and its owning ticket resolutions rather than restating them.

## 14. Clarifications the synthesis made (each posted as a dated amendment line on the owning ticket)

1. **Scope selection** (#579, #604): every sign-up names its scope (`signup_org` on the federated start, `org` on the local request), persisted on the transaction or pending row; the start stays policy-blind; unknown org = `closed`.
2. **Admission gate before charge** (#604 d3 reordered): policy present and active and provider admitted, evaluated before the `signup` budget is charged, uniform response; the budget protects the write, not the door.
3. **Consumed sign-up tokens read `unknown`** (#584 d1 kept, d7 narrowed): verification deletes the row, so `consumed` leaves the token cause set.
4. **Recovery-issued authorities never claim** (#582 d4 narrowed): refused at claim phase 1; the recovery ⇒ password CHECK stands.
5. **Mail INTENT/OUTCOME events** (#584 d6, d11): a durable `registration.mail_intent` in the pending-row transaction (joining the audit-model intent-outcome licence) and a `registration.mail_outcome` after the dial, for verification, existing-address and test sends; `signup_admitted` and `signup_refused {email-exists}` carry no `delivery` field; no outbox.
6. **Email canonical form** (#584 d3): section 2.5.
7. **Provider references are `{kind, slug}`** (#587, #596): section 2.6.
8. **Grant origin subject** (#584 d7, #585): the authority principal id; the policy id rides the org row and the trail.
9. **Migrations split** (T1/T3): 00042 additive, 00043 the switch; historical files untouched.
10. **No legacy policy fold** (#617): policy retirement predates registration for enabled and disabled providers alike.
11. **Test send bound**: 5/h per principal, 1 concurrent per instance, own category.
12. **Issuer-mismatch cause name** (#588 d3): `signature`.
13. **Policy allowlists are child rows** (#579 d5): `registration_policy_domains` and `registration_policy_entry_values` replace JSON arrays, so the database refuses empty, duplicate and non-text values. **Left for human disposition after Codex round 3:** the cross-table cardinality rule (values iff claim) is writer-enforced with a conformance pin, not a database guard; and the audit-model *body*'s two-member intent-outcome licence is widened by the banner only (the corpus rule that banner text wins over the body, never by editing the locked body).
14. **Round cap.** Codex `gpt-5.6-sol` high: R1 24 findings (5 blocking), R2 21 resolved, R3 closed 8 (exact migration SQL now given for every altered table on both engines) and left items 5 and 19 as above for the owner.
