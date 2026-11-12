# Research: multi-region SaaS patterns for regulated data

Date: 2026-11-12. Ticket: https://github.com/Hikyo-Org/Hikyo/issues/1310

Question: how do comparable services offer region choice? Specifically: is the region pinned at org creation or movable; what is global (login, billing, control plane) and what stays regional; how do the web app and CLI find a tenant's region without revealing which accounts exist; how do they handle cross-region HA and disaster recovery; and what does each layout cost.

Method: vendor documentation, vendor engineering blogs and vendor trust pages only. Every claim carries a numbered reference (list at the end). Anything not confirmed in a primary source is in [Unverified or not found](#unverified-or-not-found). Lines marked **Analysis** are my reading across vendors, not vendor claims. This report presents patterns and does not decide.

Hikyo context: hosted-service ADR (`docs/adr/hosted-service.md` in the Hikyo repo) decision 1 says installations share nothing, an org lives on exactly one installation, and a person with orgs on two installations has two logins.

## Summary

1. **Three layouts.** Shared-nothing regions (1Password, Bitwarden, Infisical, HCP Europe) have a separate account, login and (usually) billing per region. Global-control-plane designs (Sentry, Atlassian, Slack, Notion) have one identity across regions, and only product data is regional. Single-tenant cells (GitLab Dedicated) give each customer its own instance in a chosen region.
2. **Pinned is the norm.** Sentry, Auth0, HCP Europe, GitLab Dedicated, 1Password and Bitwarden do not move an existing tenant between regions. The customer creates a new tenant and exports and imports. Only Atlassian (self-serve, up to 24 h downtime) and Slack (existing data on request) move tenants. **Analysis:** both movers have a global identity and control plane that holds the tenant-to-region mapping.
3. **A global login means global identity data.** Sentry keeps user accounts, 2FA, org settings, audit logs, access tokens, DSN keys, SSO/SCIM metadata and billing usage in the US [14]. Slack keeps member profiles and membership outside the data region [26]. Atlassian treats user account data as out of scope for residency [22]. This is what it costs to avoid the "two logins" cost in Hikyo's decision 1.
4. **Region discovery uses four patterns.** These are: user picks the region, per-tenant hostname, global directory after authentication, and region embedded in the credential or base URL. **Analysis:** only the first and last avoid both a global identity store and a probeable tenant name.
5. **HA/DR varies widely.** The range runs from multi-AZ only (Doppler, Infisical, Atlassian) to automatic or operator-triggered cross-region failover with stated targets: GitLab Dedicated RTO 8 h / RPO 4 h, HCP Vault RTO 2 h / RPO 24 h, Auth0 private cloud RTO < 15 min / RPO < 1 min. Regulated vendors keep the DR copy in the same jurisdiction, as Slack does with Frankfurt to Paris.
6. **Cost.** The only engineering figure is Sentry's author estimate of about USD 3M for splitting a monolith into control and region silos (about 15 people, 18+ months) [16]. Vendors otherwise do not publish infrastructure cost. Pricing posture ranges from "same price everywhere" (Sentry) to "contract only" (HCP Europe).

## Per-vendor findings

### 1Password (1password.com, .ca, .eu)

| Aspect | Finding |
|---|---|
| Regions | US (1Password.com, USD), Canada (1Password.ca, CAD) and EU (1Password.eu, EUR). The region sets billing currency and where data is hosted [1]. |
| Pinned or movable | Pinned. "Your data stays in the region you choose. It is never automatically moved from one region to another." To move, create a new account in the other region and copy items; members migrate their own personal data. Files and custom icons cannot be copied across regions [1]. |
| Global vs regional | Per region: account, billing and data [1]. No shared control plane is documented. A 2018 engineering post describes 12 environments across three AWS regions (N. Virginia, Montreal, Frankfurt), with one production environment per region, each "more or less identical" [3]. |
| Discovery | Region selector at sign-in: "If you created your account on 1Password.ca or 1Password.eu, make sure you've selected the correct region." Users without a team account use my.1password.com, .ca or .eu. Team accounts have their own sign-in address, and "Find my account" takes an email address [2]. The Events API base URL is derived from the hosting server: events.1password.com, events.ent.1password.com, events.1password.ca, events.1password.eu [4]. |
| HA/DR | 2018 post: Aurora cluster; "every component has at least one backup in the neighbouring data centre" (Availability Zone) [3]. No cross-region DR is documented. |
| Cost/pricing | Billed in regional currency [1]. |

### Bitwarden (bitwarden.com, bitwarden.eu)

| Aspect | Finding |
|---|---|
| Regions | US and EU [5]. |
| Pinned or movable | Pinned. "Your account or organization only exists in the region where it was first created." "Bitwarden cannot migrate accounts from one region to another for customers." Subscriptions can be transferred by contacting Bitwarden. Data moves by export/import or a migration script [5]. "Accounts cannot change server regions once created" [6]. |
| Global vs regional | Separate: users on one server cannot access an organization on another [5]. Self-hosted servers with an EU subscription use EU identity, API and push-relay hostnames [5]. |
| Discovery | The user picks a server from the "Server" or "Logging in on" dropdown on web, extension, mobile and desktop [6]. Picking the wrong region "will mimic the experience of entering the wrong master password" [6]. |
| HA/DR | Not documented in the pages read. |

### Doppler

| Aspect | Finding |
|---|---|
| Regions | One region found. Servers "run within private VPCs in GCP's us-central1 region" [7]. No region choice is documented (see Unverified). |
| HA/DR | "Distinct primary and failover compute infrastructure"; failover servers "run hot in distinct availability zones"; Cloud SQL with automatic standby replacement; automated snapshots [7]. Client-side resilience: the CLI keeps an encrypted fallback snapshot and uses it if Doppler is unreachable after about 50 to 60 s. Synced secrets in cloud secret managers and the Kubernetes operator keep working during an outage [8]. |
| Note | **Analysis:** Doppler puts regional resilience in the client rather than in multi-region servers. Hikyo already has comparable consumer-side paths (adapter sync). |

### Infisical (app/us.infisical.com, eu.infisical.com)

| Aspect | Finding |
|---|---|
| Regions | US cloud (us.infisical.com or app.infisical.com) and EU cloud (eu.infisical.com) [9]. |
| Global vs regional | "Each region operates in a separate AWS account," with identical architecture. The only shared component named is the License API, whose RDS instance is "shared between US/EU" [9]. |
| Pinned or movable | No primary source found (see Unverified). |
| Discovery | Interactive CLI login prompts for US Cloud, EU Cloud or self-hosted, and "saves the instance you select with your login." Machine identities set `INFISICAL_DOMAIN`; without it, "every command goes to US Cloud" [10]. |
| HA/DR | Multi-AZ RDS PostgreSQL and a multi-AZ Redis replication group. Read replicas and cross-region DR are not mentioned [9]. |

### HashiCorp HCP (global and HCP Europe)

| Aspect | Finding |
|---|---|
| Regions | Global HCP, plus HCP Europe at portal.cloud.eu.hashicorp.com [11]. |
| Pinned or movable | "You must create a new HCP account and organization to use with HCP Europe"; existing global accounts "cannot connect or migrate" [11]. |
| Global vs regional | Separate portal, account, org and billing ("HCP Europe bills resources separately"). Only trial and contract plans; no pay-as-you-go. The HCP CLI is not supported in HCP Europe. Only Terraform and Waypoint are available there; Vault Dedicated is global-only [11]. |
| HA/DR (Vault Dedicated) | Production tiers run 3 nodes. With a backup network (HVN) in another region, "HCP will use this backup network to perform a failover of your cluster if HCP detects a cloud provider region outage." Targets: RTO 2 h, RPO 24 h, "targets, not guarantees" [12]. Snapshots can restore a deleted cluster "to a different region" [12]. Performance replication to other regions is a separate Plus-tier feature [13] (seen in a search excerpt of the official page, not fetched in full). |

### Sentry (sentry.io, de.sentry.io)

| Aspect | Finding |
|---|---|
| Regions | US (Iowa) and EU (Frankfurt) [14]. Available on all plan tiers at no extra cost [16]. |
| Pinned or movable | Chosen when creating the org and permanent: the location "can't be changed if you have an existing SaaS organization"; history does not transfer [14]. |
| Global vs regional | Regional: events, transactions, spans, profiles, logs, metrics, releases, source maps, replays, and their backups. Always in the US: user accounts, notification settings, 2FA, org settings, teams, audit logs, project metadata, DSN keys, access tokens, billing usage, and SSO/SAML/SCIM metadata [14]. Architecture: one control silo for globally unique data (users, integrations, org slugs) and isolated region silos that reach control by RPC [15]. Goal: "Customers shouldn't have to manage multiple login accounts, or remember what region their data was in" [15]. |
| Cross-silo writes | Transactional outbox in the same Postgres transaction; at-least-once delivery with full snapshots; "eventual consistency"; during a disruption "replicated data could become stale, but wouldn't expire" [17]. Integration webhooks land in the control silo and are forwarded to the right region [16]. |
| Discovery | Global login; region-specific API domains us.sentry.io and de.sentry.io keep requests in-region [14]. The ingest credential embeds the region: EU orgs must send to `o<number>.ingest.de.sentry.io` [18]. |
| Cost | Author estimate of about USD 3M, about 15 people for over 18 months, about 80 migrations to break cross-silo foreign keys [16]. |

### GitLab Dedicated

| Aspect | Finding |
|---|---|
| Model | Single-tenant SaaS, "hosted and maintained by GitLab," on AWS only, with a 99.9% monthly SLO. Default URL `tenant_name.gitlab-dedicated.com`, or a custom domain [19]. |
| Pinned or movable | "Your region choices are permanent and cannot be changed after provisioning." Primary, optional secondary (Geo DR) and optional backup region [20]. |
| HA/DR | Multi-AZ within the region [20]. Geo "continuously replicates" database, repositories and object storage to the secondary. The GitLab team decides on failover and updates DNS for `<customer>.gitlab-dedicated.com`. RTO ≤ 8 h, RPO ≤ 4 h. Backups are retained for 30 days in the secondary region. Advanced search is rebuilt after promotion [21]. |
| Global dependency | "An outage in `us-east-1` prevents GitLab from performing operations on tenants, including failover to secondary regions" [20]. |
| Cost | Not published; pre-production environments are an add-on [19]. |

### Atlassian (Jira, Confluence)

| Aspect | Finding |
|---|---|
| Regions | Global (default) plus Australia, Canada, EU, Germany, India, Japan, Singapore, South Korea, Switzerland, UK and USA [22]. Requires Standard, Premium or Enterprise plans [23]. |
| Pinned or movable | Movable. Admins schedule a move themselves. The process takes 2 to 24 h ("plan for your app to be offline for up to 24 hours"), and search re-indexing can take up to 3 days [23]. |
| Global vs regional | In scope: product data such as issues, pages, attachments and search. Out of scope: "user account information," held in a central identity service [22]. Central systems: Catalogue Service (system of record), Cloud Provisioner (provisioning and cross-realm migration) and Tenant Context Service (read-optimised) [25]. |
| Discovery/routing | Requests land on the nearest edge, which verifies the session; "the edge determines where your product data is located" from the TCS and forwards the request to the target region [24]. Async consumers re-check tenant location when they read a message, because the tenant may have moved [25]. |
| Moves | Copy first, delete only after all copies succeed; rollback brings the tenant back online [25]. |
| HA/DR | RDS multi-AZ with synchronous standby; "AZ failover is automated and typically takes 60-120 seconds." Daily snapshots, kept 30 days, with PITR. "Backup data is not stored offsite but is replicated to multiple data centers within a particular AWS region" [24]. No multi-region DR described [24]. |

### Slack

| Aspect | Finding |
|---|---|
| Regions | Frankfurt, Paris, Stockholm, Zurich, Tokyo, Mumbai, Singapore, Seoul, Sydney, London, Montreal and São Paulo, each with a named backup region (for example Frankfurt to Paris, Stockholm to Frankfurt) [26]. |
| Pinned or movable | Movable for existing data: "the customer can request to migrate existing data to their selected data region." Only the Primary Owner can enable residency; requests go through Sales; Business+ and Enterprise plans only [26]. |
| Global vs regional | Regional: messages, canvases, files, search index, app-generated content. Outside the region: member profiles, workspace and channel membership, seat/usage/revenue data, analytics logs and Slack-generated IDs. Flagged data may be stored in the US [26]. |

### Auth0

| Aspect | Finding |
|---|---|
| Regions | Localities AU, CA, EU, JP, UK and US, with sub-localities. The locality controls "the assigned tenant domain and the region where your data will be hosted," for example `example-co.au.auth0.com` [27]. |
| Pinned or movable | Pinned. Auth0 will not "migrate a tenant from one region to another (for example, from US to EU)" and "will not transfer data from one Auth0 tenant to another" [28]. |
| Discovery | Per-tenant hostname that embeds the region [27]. |
| HA/DR (public) | "We deploy redundant components across two cloud regions"; regional failover "switches to another region" [29]. A 2019 post describes a primary/failover region pair per environment (us-west-2 to us-west-1), with failover by updating Route53, exercised at least yearly [31]. |
| HA/DR (private cloud) | At least three AZs; real-time replication to a customer-chosen secondary region; failover "manually triggered by an operator," then automated; RPO < 1 min, RTO < 15 min [30]. |

### Notion (extra comparable, not on the ticket's list)

Notion uses the workspace ID as the routing and partitioning key and keeps "a centralized table that maps each workspace to its active region." Cloudflare workers route event traffic by a workspace-ID header. Orchestration stays in the US: "Our main instance is located in a US datacenter, and we decided not to change this" [32].

## Cross-vendor patterns

### A. Region pinned at creation vs movable

| | Pinned (new tenant plus export/import) | Movable (vendor-run copy) |
|---|---|---|
| Vendors | 1Password [1], Bitwarden [5], HCP Europe [11], Sentry [14], GitLab Dedicated [20], Auth0 [28] | Atlassian [23], Slack [26] |
| Customer cost | Re-invite members, re-create integrations; some data is lost (1Password files [1], Sentry history [14]) | Downtime window (Atlassian up to 24 h, search up to 3 days [23]) |
| Operator cost | None beyond export/import tooling | Copy/verify/delete workflow, location-aware routing, re-checking queued work [25] |

**Analysis:** movability appears only where a global control plane already owns the tenant-to-region mapping. In Hikyo, the outer layer of every Sealed slot is wrapped under the installation's `sealedSlotKey`, which the server can rotate without a holder (ADR decision 3, property 2). A vendor-run move would therefore at least rewrap the outer layer under the destination installation's key, which needs both installations' keys in one place and cuts across "installations share nothing". The ADR already uses "create a new org and import" for mode changes (decision 2), which matches the pinned pattern.

### B. What is global

| Layout | Global | Regional | Vendors |
|---|---|---|---|
| Shared nothing | Nothing, or only licensing (Infisical License API [9]) | Identity, billing, data | 1Password [1], Bitwarden [5], Infisical [9], HCP Europe [11] |
| Global control plane | Sentry: user accounts, 2FA, SSO/SCIM metadata, tokens, DSN keys, org settings, audit logs, billing usage [14]. Slack: member profiles, membership, seat/usage data, Slack-generated IDs [26]. Atlassian: user account information [22] | Product data (and, at Sentry, its backups [14]) | Sentry, Slack, Atlassian |
| Single-tenant cell | Vendor operations plane only | Whole instance | GitLab Dedicated [19] |

**Analysis:** for a secrets product, the global-control-plane layout would put tokens, audit logs and SSO metadata in the global region, as Sentry does [14]. A regulated customer may consider those in scope. Hikyo's current decision 1 is the shared-nothing layout.

### C. Finding a tenant's region without revealing accounts

Baseline rule: login, recovery and registration must give a generic response and take the same time whether or not the account exists [33].

| Pattern | Who | Enumeration property | Notes |
|---|---|---|---|
| 1. User picks the region | Bitwarden [6], Infisical CLI [10], 1Password selector [2], HCP portal [11] | Nothing is looked up, so nothing leaks. Bitwarden's wrong region looks like a wrong password [6]. | The user has to remember the region. |
| 2. Per-tenant hostname | Auth0 [27], GitLab Dedicated [19], 1Password team address [2] | **Analysis:** a predictable name such as `acme.region.example` can be checked by DNS or HTTP, which reveals that the tenant exists. | The hostname carries the region, and the CLI stores it once. |
| 3. Global directory after authentication | Sentry [15], Atlassian edge plus TCS [24], Notion [32] | The lookup happens after the global identity is verified, so it reveals nothing to unauthenticated callers. | Needs global identity (section B). |
| 4. Region in the credential or base URL | Sentry DSN `o<n>.ingest.de.sentry.io` [18], 1Password Events base URL [4], Infisical CLI saving the chosen instance [10] | Nothing is looked up. | Fits CLI and machine identities: the token or its configuration says where to go. |

### D. Cross-region HA and DR

AWS describes four strategies in rising cost and complexity: backup and restore, pilot light, warm standby, and multi-site active/active. Active/active is "the most complex and costly approach" [34]. AWS advises using "only data plane operations" during failover [34], and notes that because a false alarm costs availability and data, manually initiated failover "is therefore often used", with the steps still automated [34]. With Aurora global database, a secondary can be promoted in under one minute [34].

| Vendor | Within region | Cross region | Trigger | Targets |
|---|---|---|---|---|
| Doppler | Hot failover in distinct AZs [7] | Not documented | n/a | n/a; client snapshot fallback [8] |
| Infisical | Multi-AZ RDS and Redis [9] | Not documented | n/a | n/a |
| Atlassian | Multi-AZ, 60 to 120 s [24] | Backups stay in-region [24] | Automatic (AZ) | n/a |
| HCP Vault Dedicated | 3 nodes [12] | Backup HVN in another region [12] | HashiCorp, after confirming a disaster with the cloud provider [35] | RTO 2 h, RPO 24 h [12] |
| GitLab Dedicated | Multi-AZ [20] | Geo to secondary region [21] | GitLab team, DNS change [21] | RTO 8 h, RPO 4 h [21] |
| Auth0 private cloud | ≥ 3 AZs [30] | Real-time replication [30] | Operator, then automated [30] | RTO < 15 min, RPO < 1 min [30] |
| Slack | n/a | Named backup region, in-jurisdiction pairs [26] | n/a | n/a |

Observations:
- **Analysis:** the DR site of a regulated region stays in the same jurisdiction (Slack's pairs [26]; GitLab customers choose their own secondary region [20]).
- A global dependency can block regional failover: GitLab cannot fail over tenants during a us-east-1 outage [20]. **Analysis:** a global control plane carries the same risk. A shared-nothing layout avoids it.
- **Analysis (Hikyo):** a DR copy of a Sealed installation needs custody of the root key and `sealedSlotKey` at the DR site, because losing `sealedSlotKey` "destroys every slot in the installation" (ADR decision 3). Replicated data is ciphertext, but key custody determines whether the copy can be used.

### E. Cost of each layout

| Layout | Engineering cost evidence | Running cost (relative, per AWS [34]) | Pricing posture seen |
|---|---|---|---|
| Shared-nothing regions (current decision 1) | Infisical reuses "identical architectural patterns" per region [9] | One full stack per region; DR adds per-region cost by strategy | Same product, regional currency (1Password [1]); contract-only for a new region (HCP Europe [11]) |
| Global control plane plus region silos | Sentry about USD 3M, 18+ months [16]; outbox and RPC layer [17] | Adds a global tier and cross-region traffic | No extra charge (Sentry [16]) |
| Movable tenants on top of a control plane | Atlassian built Catalogue, Provisioner and TCS [25] | Copy capacity during moves | Standard plan and up (Atlassian [23]); Business+ and up (Slack [26]) |
| Single-tenant cells per customer | Not published | Full stack per customer; DR region optional [20] | Unpublished, sales-led [19] |
| Cross-region DR (any layout) | n/a | Backup/restore < pilot light < warm standby < active/active [34] | Paid tiers only where stated (HCP needs flex or entitlement billing [12]) |

## Implications for the Hikyo ADR (options, not decisions)

1. **Region equals installation (status quo).** Decision 1 already matches the 1Password, Bitwarden, Infisical and HCP Europe pattern. A region is one or more shared-nothing installations in one jurisdiction. Discovery works with pattern 1 or 4: a region picker on the web app and a stored server URL in the CLI and tokens. It keeps the accepted cost of one login per installation.
2. **Separate "capacity cell" from "jurisdiction".** Decision 1 adds installations for capacity. A region could hold several installations. The question is then how a user finds the right installation within a region: per-org hostname (pattern 2, enumeration risk) or picker plus stored URL.
3. **A global identity plane.** One login across installations needs a global store of identity, tokens and audit, as at Sentry [14]. That moves regulated metadata out of the region and conflicts with "installations share nothing."
4. **DR per region.** Options run from backup/restore to warm standby [34]. For Sealed orgs, the open question is key custody at the DR site within the same jurisdiction.

## Unverified or not found

- **Doppler EU residency.** Only a competitor blog (sota.io) claims enterprise EU hosting in Frankfurt. Doppler's own docs index lists no region or residency page, and its security fact sheet names only GCP us-central1 [7]. Treat as unverified.
- **1Password "Find my account" behaviour.** The official page says only that you enter an email address [2]. That it emails the result rather than showing it is stated only by a community moderator. Unverified.
- **1Password.eu on AWS Frankfurt today.** Stated in the 2018 engineering post [3]; current official pages say only "European Union" [1].
- **`ent.1password.com`.** Listed as a separate hosting server [4]. Its purpose and isolation are not documented in the sources read.
- **Auth0 public-cloud RPO 1 h / RTO 24 h.** Quoted in community posts from Auth0's Security & Privacy documentation, which I did not read. Unverified. The 2019 architecture post [31] may be out of date.
- **GitLab "failover in 90 minutes."** Appears only on a non-GitLab mirror of the docs. The official figures are RTO 8 h and RPO 4 h [21].
- **HCP Vault cross-region DR tiers.** The HA/DR page [12] and the cross-region DR page [35] both say Essentials and Standard. A search excerpt from another official page mentioned "essentials or plus tier"; I did not locate it. Treat Plus-tier DR as unverified.
- **AWS RPO/RTO ranges per DR strategy.** These sit in a figure on the whitepaper page; only the text's relative ordering is cited [34].
- **Infisical US-to-EU migration.** No primary source found.
- **Slack request routing and multi-region login.** No primary engineering source found.
- **Sentry org-subdomain routing.** The silo blogs defer API gateway details to later posts [15][16]. The gateway behaviour described in third-party mirrors of Sentry's internal docs is not cited.
- **Bitwarden HA/DR.** Not covered in the pages read.

## References

1. 1Password, "1Password in your region": https://support.1password.com/regions/
2. 1Password, "If you can't sign in to your 1Password account": https://support.1password.com/sign-in-troubleshooting/
3. 1Password blog, "Terraforming 1Password" (2018-01-25): https://1password.com/blog/terraforming-1password
4. 1Password Developer, Events API servers: https://www.1password.dev/events-api/servers/
5. Bitwarden, "Server geographies": https://bitwarden.com/help/server-geographies/
6. Bitwarden, "Account access / choose my server": https://bitwarden.com/help/choose-my-server/
7. Doppler, "Security fact sheet": https://docs.doppler.com/docs/security-fact-sheet.md
8. Doppler, "High availability": https://docs.doppler.com/docs/high-availability.md
9. Infisical, "Cloud architecture": https://infisical.com/docs/internals/architecture/cloud
10. Infisical, "CLI usage": https://infisical.com/docs/cli/usage
11. HashiCorp, "HCP Europe": https://developer.hashicorp.com/hcp/docs/hcp/europe
12. HashiCorp, "HCP Vault Dedicated high availability and disaster recovery": https://developer.hashicorp.com/hcp/docs/vault/high-avail-disaster-recover
13. HashiCorp, "HCP Vault Dedicated performance replication": https://developer.hashicorp.com/hcp/docs/vault/perf-replication
14. Sentry, "Data storage location (US or EU)": https://docs.sentry.io/organization/data-storage-location/
15. Sentry blog, "Removing risk from our multi-region design with simulations": https://blog.sentry.io/removing-risk-from-our-multiregion-design-with-simulations/
16. Sentry blog, "The $3M dropdown": https://blog.sentry.io/3m-dollar-dropdown/
17. Sentry blog, "Designing Sentry's cross-region replication": https://blog.sentry.io/designing-sentrys-cross-region-replication/
18. Sentry, "IP ranges": https://docs.sentry.io/security-legal-pii/security/ip-ranges/
19. GitLab, "GitLab Dedicated": https://docs.gitlab.com/subscriptions/gitlab_dedicated/
20. GitLab, "Data residency and high availability": https://docs.gitlab.com/administration/dedicated/create_instance/data_residency_high_availability/
21. GitLab, "Disaster recovery for GitLab Dedicated": https://docs.gitlab.com/administration/dedicated/disaster_recovery/
22. Atlassian, "Understand data residency": https://support.atlassian.com/security-and-access-policies/docs/understand-data-residency/
23. Atlassian, "Move data to another location": https://support.atlassian.com/security-and-access-policies/docs/move-data-to-another-location/
24. Atlassian, "Cloud architecture and operational practices": https://www.atlassian.com/trust/reliability/cloud-architecture-and-operational-practices
25. Atlassian engineering, "How we build data residency for Atlassian cloud": https://www.atlassian.com/blog/how-we-build/how-we-build-data-residency-for-atlassian-cloud
26. Slack, "Data residency for Slack": https://slack.com/help/articles/360035633934-Data-residency-for-Slack
27. Auth0, "Create tenants": https://auth0.com/docs/get-started/auth0-overview/create-tenants
28. Auth0, "Operational policies": https://auth0.com/docs/troubleshoot/customer-support/operational-policies
29. Auth0, "Availability trust": https://auth0.com/availability-trust
30. Auth0 blog, "The architect's view of Auth0's new private cloud platform": https://auth0.com/blog/the-architect-s-view-of-auth0-s-new-private-cloud-platform/
31. Auth0 blog, "Auth0 architecture: running in multiple cloud providers and regions" (2019-02-26): https://auth0.com/blog/auth0-architecture-running-in-multiple-cloud-providers-and-regions/
32. Notion blog, "Enabling multi-region data systems at Notion": https://www.notion.com/blog/enabling-multi-region-data-systems-at-notion
33. OWASP, "Authentication cheat sheet": https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html
34. AWS whitepaper, "Disaster recovery options in the cloud": https://docs.aws.amazon.com/whitepapers/latest/disaster-recovery-workloads-on-aws/disaster-recovery-options-in-the-cloud.html
35. HashiCorp, "HCP Vault Dedicated cross-region disaster recovery": https://developer.hashicorp.com/hcp/docs/vault/cross-region-disaster-recovery
