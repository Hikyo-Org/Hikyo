# Research: per-org SSO, SAML and SCIM designs in multi-tenant products

Date: 2026-11-12 (all sources accessed on this date)
Ticket: https://github.com/Hikyo-Org/Hikyo/issues/1309
Status: research input for an owner decision. This document describes patterns, what each one isolates and what it costs. It does not recommend one.

## How to read this

- Every product claim cites the page it came from. Quotes are verbatim.
- **Analysis** marks reasoning of my own. It cites the spec or ADR lines it rests on but is not a sourced fact.
- **Not documented at <URL>** means I read that page and it was silent. It does not mean the feature is absent.
- **Search snippet, not fetched** means the claim came from a search-result summary and I did not read the page itself.
- Scope notes:
  - I read "Okta/Auth0 Organizations" as Auth0 Organizations, the Okta Customer Identity product. Okta Workforce Identity was not covered.
  - The Keycloak claims come from the 26.8.2 Server Administration Guide (https://www.keycloak.org/docs/latest/server_admin/index.html, version string 26.8.2 in the page).

## 1. Summary

Every product studied gives a customer's IdP authority over some set of identities. The products differ in which set that is:

- **Operator-registered providers (Keycloak, Auth0, Hikyo today).** Only the platform operator registers an issuer. Orgs link to it.
- **Domain-claim authority (Atlassian, Slack, WorkOS, Doppler, the Keycloak domain gate).** The org that proves DNS ownership of an email domain gets authority over the accounts on that domain. Email becomes the de facto identity key, trusted because of the domain verification.
- **Org-scoped authentication on a shared account (GitHub SAML per org, Atlassian external-user SSO, Auth0 `org_id`, WorkOS `organizationId`).** The org's IdP gates only that org's resources. The person keeps one account, and the session or token is bound to one org.
- **Org-owned accounts (GitHub EMU, Zitadel, Slack, 1Password, Keycloak managed members).** The org's IdP can only create and authenticate accounts that live inside that org. One person in several orgs has several accounts.

For Hikyo the question reduces to three concrete holes that an org-registered provider would open in today's design (section 3): issuer squatting, SCIM idempotent attach, and session reach. Each pattern closes a different subset of them.

## 2. Hikyo's starting point (from the ADRs)

- **Providers are instance-scoped on purpose.** "Were it org-scoped, an org admin could add a provider and mint identities authenticating into the instance" (`docs/adr/human-auth.md` line 66).
- **Identity key.** The key is `(kind, issuer, subject)`, byte-exact, under one instance-wide uniqueness constraint. The provider row id is deliberately kept out of the key, and "one active provider per `(kind, issuer)`" is a database constraint (human-auth.md lines 24 and 75, `docs/adr/saml-sp.md` lines 117 and 167).
- **Email is never a linking key.** The one exception is the per-provider opt-in for SAML `emailAddress` NameIDs, consumed as opaque bytes (human-auth.md lines 24 and 75).
- **Accounts and links are instance-level.** Membership is grants only. SCIM deprovisioning is "org-grant-scoped, never account-scoped" (`docs/adr/scim-provisioning.md` line 89).
- **SCIM is per org but references instance providers.** It is an org-owned binding that references exactly one instance-configured provider (scim-provisioning.md lines 15-17). What a binding can cause is bounded by its own org subtree (line 19).
- **SCIM attaches to existing identities.** "If the asserted identity already exists instance-wide (invited earlier, or provisioned by another org's binding), create attaches that account" (scim-provisioning.md line 83).
- **SCIM and Entra.** OIDC-bound SCIM needs a provider whose `sub` is a directory attribute. The supported Entra shape is SAML, because Entra's OIDC `sub` is per-app (scim-provisioning.md line 74).
- **Registration policy.** An org-scoped registration policy can already admit unknown identities from instance-enabled providers into one org, on an issuer-specific claim allowlist (human-auth.md line 28).
- **Open item 3 asks for exactly this design.** "A customer on a shared installation will expect its own SSO … this needs a design that keeps that closed" (`docs/adr/hosted-service.md` line 324).

## 3. The three holes an org-owned provider opens (analysis)

### 3.1 Issuer squatting

**A SAML entityID is a name, not proof of ownership.**
- The metadata spec defines `entityID` only as an identifier: "Specifies the unique identifier of the SAML entity whose metadata is described by the element's contents" (OASIS saml-metadata-2.0-os §2.3.2, https://docs.oasis-open.org/security/saml/v2.0/saml-metadata-2.0-os.pdf).
- Publishing at a well-known location is optional: "Entities MAY publish their metadata documents at a well known location" (same document, §4.1.1).
- Trust is left to the relying party: "relying parties SHOULD have some means by which to establish trust in metadata information before relying on it" (same document, §4).
- **Analysis:** nothing stops an org admin from typing another company's entityID and uploading their own signing certificate.

**An OIDC issuer is bound to its URL.**
- Discovery requires that "The issuer value returned MUST be identical to the Issuer URL that was used as the prefix" and that it "MUST also be identical to the iss Claim value in ID Tokens issued from this Issuer" (OpenID Connect Discovery 1.0 §4.3, https://openid.net/specs/openid-connect-discovery-1_0.html).
- **Analysis:** to forge tokens for someone else's issuer, an org must control that issuer's host. Registering a legitimate shared issuer is still possible and legitimate, for example `https://accounts.google.com` (section 6).

**Analysis, against today's constraints:**
- Hikyo keeps "one active provider per `(kind, issuer)`" with the identity key instance-wide (saml-sp.md line 167). Under that constraint, an org-registered SAML provider is first-come-wins.
- Org B can register org A's entityID first. That denies org A its own IdP.
- Worse, org B can register it after org A's provider is removed. Links created in org A's era then resolve against org B's certificates. That is account takeover through a stale `(saml, entityID, subject)` link.
- Human-auth's restore rule already treats resurrected links as untrusted until re-confirmed (human-auth.md line 297). That is a precedent for "a link outlives the provider that vouched for it".
- For OIDC the same collision appears when two orgs legitimately want the same shared issuer. The second is refused.

### 3.2 SCIM idempotent attach

**Analysis, from scim-provisioning.md lines 19, 81 and 83:**
- The SCIM credential "is the authority to assert identities for its one `(kind, issuer)`". Create attaches to any matching identity instance-wide.
- The two cases differ:
  - **Instance-registered provider (today).** The operator vetted the issuer, so the binding is asserting subjects for an IdP that really issues them.
  - **Org-registered provider with a squatted or shared issuer.** Org B's binding can attach org A's people to org B.
- **Attach alone does not reach org A's grants.** Line 19 bounds what a binding can cause to grants inside its own org.
- **It becomes takeover only together with authentication.** That happens once org B's provider can also log in as the attached account (section 3.3).
- **There is a separate, smaller effect even today, unverified against code.** An org admin who can create a binding against a shared instance provider (for example a Google provider) could pre-create or attach accounts for arbitrary subjects of that provider. Those people would then find org B's grants on their account.

### 3.3 Session reach

**Analysis:**
- Accounts and sessions are instance-level, and a session carries every grant the account holds (human-auth.md, the assurance record around line 131).
- So any provider that can authenticate account X reaches X's grants in every org, not just the org that owns the provider.
- Today that is acceptable because every provider is operator-vetted. With org-owned providers, org B's IdP becomes a path into org A for anyone who is a member of both.
- The per-session assurance record already names the method (`oidc:<issuer>`, `saml:<entityID>` plus provider row id) (human-auth.md line 131, saml-sp.md line 137). It is the natural place to hang an org-scoped rule, but that is a design choice, not a finding.

**The counter-pattern in products** is to bind the session or token to one org and to make each org's IdP gate only that org (section 4.3).

## 4. Patterns

### 4.1 Operator-registered providers, linked to orgs

**How it works**
- Keycloak creates identity providers at realm level, then links them to organizations: "Before you can link an identity provider to an organization, you create an identity provider at the realm level" (Keycloak 26.8.2, Managing identity providers).
- One IdP can be linked to several organizations with "independent per-association configuration" (same page).
- Auth0 enterprise connections are tenant objects that are enabled per organization: "the enterprise connection must also be enabled for the organization" (https://auth0.com/docs/manage-users/organizations/organizations-overview).
- Auth0 lets a customer admin configure their own connection through a ticket the tenant admin issues. Domain verification on that ticket can be Required ("Customer admins must successfully verify a new domain via DNS"), Optional or Off (https://auth0.com/docs/authenticate/enterprise-connections/self-service-enterprise-configuration).
- Auth0's dev-docs page calls domain verification Early Access; the changelog calls it GA (search snippet, not fetched; https://dev.auth0.com/docs/authenticate/enterprise-connections/self-service-SSO/manage-self-service-sso, https://auth0.com/changelog).

**Isolation**
- Closes issuer squatting, because the operator decides which issuers exist.
- Session reach and SCIM attach stay closed for the same reason: every issuer is vetted.

**Cost**
- The operator is in the loop for every customer IdP.
- Auth0's self-service tickets show the usual middle ground: the operator issues a scoped grant, the customer fills in details, and the operator's policy decides whether DNS proof is required.

**Fit with Hikyo**
- This is today's model (human-auth.md line 66).
- A ticket-like flow would let an org admin propose a provider that an instance admin approves. That keeps the "instance capability" rule intact.

### 4.2 Domain-claim authority over global accounts

**How it works**
- **Atlassian.** One account spans organizations: "Users can log in to multiple organizations and apps with the same Atlassian account" (https://support.atlassian.com/user-management/docs/make-atlassians-global-account-model-work-for-you/).
  - An org gets authority by proving the domain: "An organization needs to prove ownership of a domain and claim accounts in order to have full administrative control over the accounts" (same page).
  - SSO applies only there: "SSO only applies to user accounts from your verified domains" (https://support.atlassian.com/security-and-access-policies/docs/configure-saml-single-sign-on-with-an-identity-provider/).
  - Verification is by DNS TXT, HTTPS file, or a Google Workspace or Entra integration. A domain already verified elsewhere shows a warning that "someone at your company might have verified the domain under another organization" (https://support.atlassian.com/user-management/docs/verify-a-domain-to-manage-accounts/).
- **Slack.** Domains are verified by DNS TXT. A restricted domain means "Members of other workspaces won't be able to send invitations" to those addresses, and on Enterprise plans members can be blocked from joining external workspaces (https://slack.com/help/articles/5513043606547-Claim-and-verify-email-domains).
- **WorkOS.**
  - "What uniquely identifies a user is their email address" (https://workos.com/docs/authkit/users-organizations).
  - Users on a verified domain who sign in through SSO are "automatically be considered verified" (same page).
  - "Only one organization can include a specific domain in its domain policy per environment," and domain policies "are enforced for all users with email domains included in the policy" whichever organization the user picks (https://workos.com/docs/authkit/organization-policies).
  - Verification is by DNS TXT (https://workos.com/docs/domain-verification).
- **Doppler.**
  - "Verify your workplace domain to restrict account Single Sign-On to that domain" (https://docs.doppler.com/docs/domain-verification).
  - Users "login using their email address and the domain of that address is mapped back to your SAML SSO login" (https://docs.doppler.com/docs/okta-saml).
  - "Doppler will only provision users whose email address belongs to a verified domain on your workplace" (https://docs.doppler.com/docs/authentik-scim).
- **Keycloak's domain gate.**
  - With an IdP shared across orgs, "a user with alice@acme.com will only be added to Org-A — even though the same identity provider handled the authentication" (Keycloak 26.8.2, Domain routing and shared identity providers).
  - "A domain cannot be shared across organizations in the same realm" (same section).
  - The email must also be verified before membership: "an OpenID Connect provider returning email_verified=false does not establish a verified address even when Trust Email is enabled" (Keycloak 26.8.2, How organization membership is determined during broker login).
  - A DNS or HTTP ownership proof for organization domains is not documented in the 26.8.2 organizations section. The domains are admin-entered.

**Isolation**
- Org B's IdP cannot authenticate or claim an account whose email is on org A's verified domain.
- That holds only as long as email is the key and domain ownership is current.

**Cost, and the conflict with Hikyo**
- **It makes email the de facto identity key**, trusted through domain verification. That contradicts human-auth's rule that "Email is never a linking key, at any point, for any provider" (human-auth.md line 75). It would reverse the rejected auto-linking-on-verified-email option (line 89), not extend it.
- **Domain ownership lapses.** Google warns that `email_verified` can be true even when Google is not authoritative, because "ownership of the third party email account may have since changed" (https://developers.google.com/identity/gsi/web/guides/verify-google-id-token).
- **Guests and contractors with other domains fall outside.** WorkOS: "SSO JIT provisioning is not fully supported for guests whose email domain has not been verified by the organization" (https://workos.com/docs/authkit/jit-provisioning).
  - Atlassian handles them with a separate external-user policy (section 4.3).
- **Shared email domains need tie-breaking.** WorkOS allows only one domain policy per domain. Keycloak resolves overlapping wildcard and exact domains by "most specific match" (Keycloak 26.8.2, Considerations).

### 4.3 Org-scoped authentication on a shared account

**How it works**
- **GitHub org-level SAML.**
  - Members keep personal accounts: "members of your organization will continue to sign into their personal accounts on GitHub.com" and "each personal account will be linked to an external identity in your IdP" (https://docs.github.com/en/enterprise-cloud@latest/organizations/managing-saml-single-sign-on-for-your-organization/about-identity-and-access-management-with-saml-single-sign-on).
  - Links are per org: "Each GitHub account can be linked to exactly one external identity per organization" and "each external identity can be linked to exactly one GitHub account in an organization" (https://docs.github.com/en/enterprise-cloud@latest/authentication/authenticating-with-saml-single-sign-on/about-authentication-with-saml-single-sign-on).
  - Access to the org needs a fresh IdP session: "you must authenticate at the SAML IdP before you can access the organization"; the default period is "24 hours unless specified otherwise by your IdP" (same page).
  - Credentials are authorized per org: a token or SSH key must be authorized "for use with the organization" (same page).
  - "SAML SSO does not replace the normal sign-in process for GitHub" (org page above).
- **Atlassian external users.** An org can require its own IdP for accounts it does not manage. The external-user policy can "require users to verify their identity with single sign-on" (https://support.atlassian.com/security-and-access-policies/docs/available-external-user-security-settings/).
  - Limits: "You're unable to connect Google Workspace to apply single sign-on to external users", and for Entra tenant-scoped profiles "User's email must be used for the NameID SAML attribute" (same page).
  - How the external user is matched to the IdP identity is not documented at https://support.atlassian.com/security-and-access-policies/docs/apply-authorization-method/.
- **Auth0 Organizations.** Tokens carry `org_id`. If the claim "cannot be validated, then the API should deem the token invalid", and APIs must restrict data by `org_id` (https://auth0.com/docs/manage-users/organizations/using-tokens).
  - With no org known at login, auto-membership goes only to "one and only one" organization that has the connection enabled with auto-membership on (https://auth0.com/docs/manage-users/organizations/login-flows-for-organizations).
- **WorkOS.** "it is important to always validate the returned profile's organization ID", because "It's unsafe to validate using email domains" (https://workos.com/docs/sso/signing-in).

**Isolation**
- Closes session reach. A login through org B's IdP yields a session or token usable only in org B.
- Org A can require its own IdP before its resources are touched, whatever the account's primary login is.
- Combined with an org-qualified link (GitHub's one identity per org, per account), it also contains squatting. A squatted issuer in org B only produces org-B-scoped sessions.

**Cost**
- Sessions become `(account, org)`-shaped, or carry a per-org assurance set.
- A person in three SSO orgs re-authenticates at three IdPs.
- Tokens and machine credentials need per-org authorization (GitHub's model).
- An org-scoped IdP cannot be the account's recovery or primary login without reopening session reach.

**Fit with Hikyo (analysis)**
- The assurance record already names the method and provider row per session (human-auth.md line 131, saml-sp.md line 137).
- The authorization chokepoint already refuses capabilities by session property (the `enrolment_required` gate, human-auth.md line 142).
- An "org requires its provider" rule would be a new predicate at that chokepoint, not a new mechanism.

### 4.4 Org-owned (managed) accounts

**How it works**
- **GitHub Enterprise Managed Users.**
  - "You control usernames, profile data, organization membership, and repository access from your IdP" (https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/understanding-iam-for-enterprises/about-enterprise-managed-users).
  - "Managed user accounts cannot create public content or collaborate outside your enterprise" (same page).
  - Usernames are namespaced: "GitHub also adds an underscore and your enterprise's shortcode to the end of each username", and "Conflicts can only occur between users within the same enterprise" (https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/iam-configuration-reference/username-considerations-for-external-authentication).
  - To collaborate outside, a managed user "must also maintain a separate, personal account" (https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/understanding-iam-for-enterprises/choosing-an-enterprise-type-for-github-enterprise-cloud).
  - Whether one managed account can span enterprises is not documented at the pages above.
- **Zitadel.**
  - "Users exist strictly within one Organization" (https://zitadel.com/docs/concepts/structure/users).
  - Login names are unique only with the org domain. The same email can be used for different accounts in different orgs (same page).
  - IdPs can be connected "exclusively to a specific organization" (https://zitadel.com/docs/guides/integrate/identity-providers/introduction).
  - Org domains can require a DNS TXT or HTTP proof, which self-hosted instances can switch off (https://zitadel.com/docs/guides/manage/console/organizations).
  - Cross-org access is by role assignment to an "external user" (https://zitadel.com/docs/concepts/features/external-user-grant).
  - Whether Zitadel's automatic linking ("username or email") searches only within the org that owns the IdP is not documented at the IdP introduction page.
- **Slack.** "You can use the same email address to join as many workspaces as you'd like, but you'll have separate Slack accounts for each one" (https://slack.com/help/articles/212675257-Join-a-Slack-workspace).
- **1Password.** Each account is joined separately. The apps support "only one account that signs in with SSO" (https://support.1password.com/multiple-accounts/).
- **Keycloak managed members.**
  - "Managed members are those managed by the organization, and they cannot exist outside of their organization" (Keycloak 26.8.2, Managed and unmanaged members).
  - "At most one organization per identity provider can have the Managed membership type" (Keycloak 26.8.2, Understanding per-association configuration).

**Isolation**
- Closes all three holes by construction.
- Org B's IdP can only resolve identities in org B's namespace, so a squatted issuer reaches nothing outside org B.
- SCIM attach can only find org B's own accounts.
- A session is an org-B account.

**Cost**
- One person in several orgs has several accounts, several factor sets and several recovery paths. Slack and 1Password show the user-facing result: separate sign-ins, separate 2FA.
- Cross-org collaboration needs a guest or external-user mechanism (Zitadel role assignments, GitHub's separate personal account).

**Conflict with Hikyo (analysis)**
- Hikyo's account is instance-level and grants span orgs.
- Org-owned accounts would be a second account class with an org-qualified identity key. That cuts against the decision to keep the provider row out of the key (human-auth.md line 24).

### 4.5 Identity namespacing: the common thread

Each pattern answers "whose subject is this?" differently:

| Pattern | Key the login resolves | Who can assert it |
| --- | --- | --- |
| Operator-registered (Hikyo today, Keycloak, Auth0) | `(issuer, subject)` instance-wide | Only operator-vetted issuers |
| Domain claim (Atlassian, WorkOS, Doppler) | email, gated by verified domain | The org holding the domain |
| Org-scoped session (GitHub SAML, Auth0 `org_id`) | `(account, org, external identity)` | Org's IdP, for that org's resources only |
| Org-owned accounts (EMU, Zitadel, Slack) | `(org, issuer, subject)` or org-suffixed username | Org's IdP, for that org's accounts only |

Microsoft states the same principle for its own multi-tenant tokens: "claims like `sub` are interpreted within the context of the issuer/tenant" (https://learn.microsoft.com/en-us/entra/identity-platform/access-tokens).

## 5. Topic by topic

### 5.1 Domain verification

- **Mechanism.**
  - DNS TXT everywhere: Slack, WorkOS, Doppler, Atlassian, Zitadel, Auth0 self-service.
  - Atlassian and Zitadel also accept an HTTP or HTTPS file.
  - Atlassian also accepts verification through a Google Workspace or Entra integration.
  - Sources are the domain-verification pages cited in 4.1 to 4.4.
- **What it unlocks varies.**
  - Account authority over a global account (Atlassian).
  - SSO routing and JIT (WorkOS, Auth0 HRD, Keycloak).
  - A SCIM provisioning filter (Doppler).
  - Login-name suffixes (Zitadel).
- **Uniqueness varies.**
  - Keycloak refuses one domain in two orgs.
  - WorkOS allows several orgs to verify a domain but only one domain policy.
  - Atlassian warns on a duplicate and requires removal before a move.
- **What it does not prove.**
  - That a given email claim is current. See Google's warning on changed ownership in 4.2.
  - That an IdP is entitled to a SAML entityID or an OIDC issuer. **Analysis:** domain verification ties an org to email addresses, not to an issuer. For a Hikyo design that keys on `(kind, issuer, subject)`, a domain proof only helps if it is used to authorize an issuer, for example by requiring the OIDC issuer host or the SAML metadata URL to sit on a verified domain. No product page I read documents doing that.

### 5.2 One person in several orgs

- **One account, per-org links and per-org sessions:** GitHub SAML per org (4.3), Atlassian external users, Auth0 shared users ("Any user may belong to multiple organizations and should be able to use the same identity", https://auth0.com/docs/manage-users/organizations/organizations-overview), Keycloak unmanaged members, WorkOS ("each user can be in any number of organizations", https://workos.com/docs/authkit/users-organizations).
- **One account per org:** GitHub EMU, Zitadel, Slack, 1Password, Keycloak managed members (4.4).
- **Microsoft's guidance for guests** matches the second shape at the token level: guest scenarios "should treat the user as if they're a brand new user to the service. Your documents and privileges in one tenant shouldn't apply in another tenant" (https://learn.microsoft.com/en-us/entra/identity-platform/id-token-claims-reference).
- **Analysis:** Hikyo already has the first shape for operator providers: one account, grants in many orgs. An org-owned provider either joins that shape with org-scoped sessions (4.3) or forces the second shape (4.4).

### 5.3 Just-in-time provisioning

**What the products do**
- **WorkOS.** On by default: "Both automatic membership by email domain and SSO JIT provisioning are enabled by default" (https://workos.com/docs/authkit/jit-provisioning). An existing user matching a verified domain "is automatically added to the organization" (same page).
- **Auth0.** Auto-membership per connection. With no org known, it goes to "one and only one organization" (4.3).
- **Keycloak.**
  - Auto-membership per IdP link, on by default, subject to the domain gate and email verification (Keycloak 26.8.2, Linking an identity provider to an organization; How organization membership is determined during broker login).
  - A domain-less org skips the domain gate. It "automatically accepts any user who authenticates through a linked identity provider" (Keycloak 26.8.2, Domain-less organization with auto-membership).
  - When no org was resolved, for example IdP-initiated login, "all organizations linked to the identity provider are evaluated" (same page).
  - Keycloak's first-broker-login matches existing users by "the same email or username". It warns that its auto-link authenticator "is dangerous in a generic environment where users can register themselves using arbitrary usernames or email addresses" (Keycloak 26.8.2, Default first login flow authenticators).
- **Zitadel.** Offers "Automatic creation" and automatic linking by "username or email" per IdP (https://zitadel.com/docs/guides/integrate/identity-providers/introduction).
- **1Password.** Matches the first SSO sign-in by email, then pins `sub`: "their 1Password email must match the `email` claim from their sign-in" and "1Password stores the `sub` claim to map future sign-ins" (https://support.1password.com/sso-configure-generic/).
- **Atlassian.** For JIT, SAML must be enforced on the default authentication policy. Without JIT, an IdP email change "must be made manually … or a second account may be created" (https://support.atlassian.com/security-and-access-policies/docs/configure-saml-single-sign-on-with-an-identity-provider/).

**Analysis for Hikyo**
- JIT was retired in favour of the registration policy, and SAML has no JIT (human-auth.md lines 24 and 28, saml-sp.md line 145).
- Every product JIT above matches existing users by email or verified domain. Hikyo's rules forbid that.
- The org-landing registration policy is the closest equivalent, because it admits unknown identities into one org on an issuer-specific claim.
- For an org-owned issuer, its admission would need the same per-org scoping as the provider itself.

### 5.4 Enforced SSO per org, and the break-glass floor

**Per-org versus account-wide enforcement**
- GitHub org SAML gates the org's resources only (4.3).
- Atlassian enforces SSO through authentication policies over managed accounts. It is account-wide because the org owns the account (https://support.atlassian.com/security-and-access-policies/docs/configure-saml-single-sign-on-with-an-identity-provider/).
- The external-user policy is the per-org form for accounts it does not own (4.3).

**Every product keeps an escape hatch**
- **Slack.** "Workspace Owners and Org Owners can bypass SSO authentication to sign in with an email address and password." Enforcement can be required for everyone, required except guests, or optional (https://slack.com/help/articles/203772216-Set-up-SAML-single-sign-on-for-Slack).
- **1Password.** "People in the Owners group can't unlock 1Password with SSO." It allows "only one identity provider" (https://support.1password.com/sso/).
- **WorkOS.** Configuring SSO means "all non-SSO authentication methods for the organization are automatically disabled", and admins can re-enable them manually (https://workos.com/docs/authkit/organization-policies).
- **Keycloak.** "If a user has first-factor credentials configured (for example, a password), the user is not automatically redirected but asked to authenticate using their credentials" (Keycloak 26.8.2, Configuring domain routing).
- **Doppler.** An SSO enforcement setting is not documented at https://docs.doppler.com/docs/workplace-settings or https://docs.doppler.com/docs/okta-saml.

**Analysis for Hikyo**
- Human-auth requires that "principals holding instance capabilities retain a local credential", and that the floor can never be removed (human-auth.md line 71, § Lockout and the local floor).
- So an account-wide "SSO only" switch is not available to an org.
- The compatible form is per org: org A's resources require a session whose assurance record names org A's provider. The account's other credentials still exist but do not satisfy org A.
- The products above all exempt an owner role from enforcement. That maps to who in an org may bypass the org rule, which is a separate decision from the instance floor.

### 5.5 SCIM per org

- **Doppler** filters SCIM to verified-domain emails (4.2). Deprovisioning "removes the user's workplace seat, which revokes their access to your workplace" (https://docs.doppler.com/docs/authentik-scim).
- **WorkOS.** Directory-provisioned users on a verified domain become active members "without an invitation". Others get pending memberships and an invitation email (search snippet, not fetched; https://workos.com/docs/authkit/directory-provisioning).
- **GitHub org SAML.** SCIM invites members and removes them. It is described as a way to have "GitHub can automatically invite members to join your organization" (org SAML page in 4.3).
- **GitHub EMU.** "Your IdP provisions new user accounts on GitHub" (EMU page in 4.4).
- **Analysis for Hikyo**
  - SCIM's org-subtree bound (scim-provisioning.md line 19) already limits grants.
  - The open question is only the instance-wide attach of line 83. Under 4.3 it stays safe if org B's attach cannot give org B's IdP a login path into the attached account outside org B.
  - Under 4.4 attach can only find org B's own accounts.
  - Under 4.2 the products' answer is "only emails on our verified domain", which again relies on email.

## 6. Shared providers: what breaks

### 6.1 Google

**What Google provides**
- One issuer for every Google account. `sub` is "unique among all Google Accounts and never reused", and Google says to "Use `sub` within your application as the unique-identifier key" (https://developers.google.com/identity/openid-connect/openid-connect).
- **There is no tenant ID claim.** The org signal is `hd`: "You must check this claim when restricting access to a resource to only members of certain domains" and "The absence of this claim indicates that the account does not belong to a Google hosted domain" (same page).
- **The `hd` request parameter is not access control:** "Don't rely on this UI optimization to control who can access your app, as client-side requests can be modified" (same page).
- **Google is authoritative for an email only in two cases:** "`email` has a `@gmail.com` suffix", or "`email_verified` is true and `hd` is set" (https://developers.google.com/identity/gsi/web/guides/verify-google-id-token).

**What breaks (analysis)**
- Two orgs that both want Google SSO both need the issuer `https://accounts.google.com`.
- Under "one active provider per `(kind, issuer)`" only one org can have it. So Google either stays an instance provider, or the key and provider uniqueness become org-qualified.
- Per-org separation can only come from an `hd` allowlist, which is a domain the org would need to prove. Atlassian notes that Google Workspace cannot be used to apply SSO to external users (4.3).

### 6.2 Microsoft Entra multi-tenant

**What Microsoft provides**
- **The issuer is per tenant.** With the tenant-independent endpoint the app must "replace the `{tenantid}` value in the issuer metadata with the tenant ID that is the target of the current request, and then check the exact match" and "Validate that the `tid` claim is a GUID and the `iss` claim is of the form `https://login.microsoftonline.com/{tid}/v2.0`" (https://learn.microsoft.com/en-us/entra/identity-platform/access-tokens).
- **`tid` belongs in the data key:** "the `tid` claim must be part of the key used to access the user's data" (same page).
- **`sub` is pairwise per app:** "If a single user signs into two different apps using two different client IDs, those apps receive two different values for the subject claim" (https://learn.microsoft.com/en-us/entra/identity-platform/id-token-claims-reference).
- **`oid` is per tenant:** "If a single user exists in multiple tenants, the user contains a different object ID in each tenant" (same page).
- **`email` is unsafe:** "This value isn't guaranteed to be correct and is mutable over time. Never use it for authorization or to save data for a user" (same page).

**What breaks (analysis)**
- **A multi-tenant registration needs a per-org `tid` pin.** If Hikyo registers one multi-tenant app (`/organizations` or `/common`) as an instance provider, any Entra tenant can produce a valid token. Each org's admission needs a `tid` pin. Without one, the provider is an open door for every Entra tenant.
- **Per-tenant issuers suit an org-owned provider.** A provider registered per tenant (`https://login.microsoftonline.com/{tid}/v2.0`) carries the tenant in the issuer. The instance-wide `(kind, issuer, subject)` key separates tenants correctly.
- **Changing app registration changes `sub`.** If an org registers its own Entra app instead of using the operator's, every user's `sub` changes. Existing links stop matching. That is the same per-app limitation scim-provisioning.md line 74 already names for OIDC-bound SCIM.
- **One Entra tenant can legitimately serve several orgs.** Keycloak documents this as a recipe: "A single corporate identity provider (for example, a central EntraId instance) serves multiple organizations, with each organization only accepting users from its own email domain" (Keycloak 26.8.2, Shared identity provider across multiple organizations). Its separation is the email-domain gate, so it inherits the costs in 4.2.

### 6.3 Customer-run SAML IdPs (Okta, ADFS, Keycloak realms)

**Analysis**
- The issuer is a self-declared entityID (3.1).
- Two genuinely different IdPs can carry the same entityID by accident or on purpose.
- Today's constraint makes that a first-come-wins conflict.
- Per-org separation needs either operator vetting (4.1) or an org-qualified key or session (4.3, 4.4).

## 7. Product appendix

| Product | Who registers the IdP | Identity key / account scope | Domain proof | One person, several orgs | JIT | Enforcement and escape |
| --- | --- | --- | --- | --- | --- | --- |
| GitHub, SAML per org | Org owner | Personal account plus one external identity per org | Not documented at the org SAML pages read | One account, per-org SSO session and per-org token authorization | No. SCIM invites | Per org. Public repo reads exempt |
| GitHub EMU | Enterprise | Managed account, `name_shortcode` | n/a, IdP-provisioned | Separate personal account needed outside | IdP provisions | All authentication through the IdP |
| Slack Enterprise Grid | Org Owner | Separate account per workspace | DNS TXT | Separate accounts | Not documented at the pages read | Required / except guests / optional. Owners bypass |
| Atlassian Guard | Org admin | One global account; managed by the domain owner | DNS TXT, HTTPS file, Workspace or Entra | One account. External in other orgs | With enforced SAML on the default policy | Policies for managed accounts; external-user SSO per org |
| Auth0 Organizations | Tenant admin, or customer via ticket | Tenant users; identities separate by default | Ticket: Required / Optional / Off | Shared users across orgs | Auto-membership per connection | Per-org connections; validate `org_id` |
| WorkOS | Customer via Admin Portal | Email | DNS TXT | Many orgs per user | On by default for verified domains | SSO disables other methods by default; re-enable possible |
| Zitadel | Instance or org | User in exactly one org | DNS TXT or HTTP, can be off self-hosted | External-user role assignment | Auto-create and auto-link per IdP | Org login policy |
| Keycloak 26.8.2 | Realm admin; linked to orgs | Realm user; managed or unmanaged member | Not documented in the organizations section | Unmanaged members span orgs | Auto-membership with domain and email gate | Password holders not auto-redirected |
| 1Password | Owner / admin, one IdP | Per account; email first, then `sub` | Not documented at the SSO page | Separate accounts; one SSO account per app | n/a | Owners excluded |
| Doppler | Workplace admin | Email domain routes to SAML | DNS TXT, required for SAML | Not documented at the pages read | SCIM limited to verified domains | Not documented at the pages read |

## 8. Gaps and unverified points

- **Auth0 domain-verification status.** Early Access per the dev docs, GA per the changelog. Both are search snippets, not fetched.
- **WorkOS Directory Provisioning membership rule.** Search snippet, not fetched.
- **Not documented at the pages I read:**
  - Zitadel's automatic-linking scope (org only, or instance).
  - Keycloak's org-domain ownership proof.
  - Doppler's SSO enforcement.
  - Whether a GitHub EMU account can span enterprises.
  - How Atlassian matches an external user to the org's IdP identity.
- **Not covered:**
  - Okta Workforce Identity.
  - Microsoft's change to omit unverified `email` claims (seen only in a Microsoft Q&A thread).
  - Reports of Google OAuth abuse through lapsed startup domains (secondary sources only).
- **Unverified against code:** the analysis in 3.1 to 3.3 rests on ADR text, not on the current implementation. That includes whether an org admin can today create a SCIM binding against a shared instance provider such as Google.

## Sources

Specifications
- OpenID Connect Core 1.0, §2 and §5.7: https://openid.net/specs/openid-connect-core-1_0.html. Quote: "the only guaranteed unique identifier for a given End-User is the combination of the iss Claim and the sub Claim"; email and similar claims "MUST NOT be used as unique identifiers".
- OpenID Connect Discovery 1.0, §4.3: https://openid.net/specs/openid-connect-discovery-1_0.html
- OASIS SAML 2.0 Metadata, §2.3.2, §4, §4.1.1: https://docs.oasis-open.org/security/saml/v2.0/saml-metadata-2.0-os.pdf

Products
- GitHub:
  - https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/understanding-iam-for-enterprises/about-enterprise-managed-users
  - https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/iam-configuration-reference/username-considerations-for-external-authentication
  - https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/understanding-iam-for-enterprises/choosing-an-enterprise-type-for-github-enterprise-cloud
  - https://docs.github.com/en/enterprise-cloud@latest/authentication/authenticating-with-saml-single-sign-on/about-authentication-with-saml-single-sign-on
  - https://docs.github.com/en/enterprise-cloud@latest/organizations/managing-saml-single-sign-on-for-your-organization/about-identity-and-access-management-with-saml-single-sign-on
- Slack:
  - https://slack.com/help/articles/5513043606547-Claim-and-verify-email-domains
  - https://slack.com/help/articles/203772216-Set-up-SAML-single-sign-on-for-Slack
  - https://slack.com/help/articles/220403548-Manage-single-sign-on-settings
  - https://slack.com/help/articles/360002069588-Fix-and-prevent-duplicate-accounts
  - https://slack.com/help/articles/212675257-Join-a-Slack-workspace
- Atlassian:
  - https://support.atlassian.com/user-management/docs/make-atlassians-global-account-model-work-for-you/
  - https://support.atlassian.com/user-management/docs/verify-a-domain-to-manage-accounts/
  - https://support.atlassian.com/security-and-access-policies/docs/configure-saml-single-sign-on-with-an-identity-provider/
  - https://support.atlassian.com/security-and-access-policies/docs/who-are-external-users/
  - https://support.atlassian.com/security-and-access-policies/docs/available-external-user-security-settings/
  - https://support.atlassian.com/security-and-access-policies/docs/apply-authorization-method/
- Auth0:
  - https://auth0.com/docs/manage-users/organizations/organizations-overview
  - https://auth0.com/docs/manage-users/organizations/using-tokens
  - https://auth0.com/docs/manage-users/organizations/login-flows-for-organizations
  - https://auth0.com/docs/get-started/architecture-scenarios/multiple-organization-architecture/multiple-idp-orgs
  - https://auth0.com/docs/manage-users/user-accounts/user-account-linking (quote: "Auth0 treats all identities as separate by default")
  - https://auth0.com/docs/authenticate/enterprise-connections/self-service-enterprise-configuration
- WorkOS:
  - https://workos.com/docs/sso/signing-in
  - https://workos.com/docs/domain-verification
  - https://workos.com/docs/authkit/jit-provisioning
  - https://workos.com/docs/authkit/users-organizations
  - https://workos.com/docs/authkit/organization-policies
- Zitadel:
  - https://zitadel.com/docs/concepts/structure/users
  - https://zitadel.com/docs/guides/manage/console/organizations
  - https://zitadel.com/docs/guides/integrate/identity-providers/introduction
  - https://zitadel.com/docs/concepts/features/external-user-grant
- Keycloak 26.8.2 Server Administration Guide, sections Managing organizations and First login flow: https://www.keycloak.org/docs/latest/server_admin/index.html
- 1Password:
  - https://support.1password.com/sso/
  - https://support.1password.com/sso-configure-generic/
  - https://support.1password.com/multiple-accounts/
- Doppler:
  - https://docs.doppler.com/docs/domain-verification
  - https://docs.doppler.com/docs/okta-saml
  - https://docs.doppler.com/docs/authentik-scim
  - https://docs.doppler.com/docs/workplace-settings
- Google:
  - https://developers.google.com/identity/openid-connect/openid-connect
  - https://developers.google.com/identity/gsi/web/guides/verify-google-id-token
- Microsoft:
  - https://learn.microsoft.com/en-us/entra/identity-platform/id-token-claims-reference
  - https://learn.microsoft.com/en-us/entra/identity-platform/access-tokens

Hikyo ADRs (read-only context): `docs/adr/human-auth.md`, `docs/adr/saml-sp.md`, `docs/adr/scim-provisioning.md`, `docs/adr/hosted-service.md`.
