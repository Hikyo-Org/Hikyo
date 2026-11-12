# Research: how hosted secrets managers isolate tenants, grant support access and handle breaches

Date: 2026-11-12. Ticket: https://github.com/Hikyo-Org/Hikyo/issues/1304

All live pages were retrieved on 2026-11-12. Many of them carry no version or update date, so a claim here is "as published on that day". Every claim has a link. "Not found" means I looked in the cited primary source and it does not say. "Unverified" means I could not reach a primary source.

Context read: `docs/adr/hosted-service.md`, decisions 2 and 6 and the honest statement. The owner's requirement has two parts: (a) the hoster's super admin may help an org only after the org's owner approves, and (b) the super admin can never read the org's secrets.

## Short answer

- **No vendor documents both parts of the owner's requirement for a service that decrypts on the server.** The vendors split into three groups:
  - **End-to-end vendors (1Password, Bitwarden).** Support cannot read secrets because the server never holds the keys. There is also no support path into customer data that would need approval. Recovery is done by the customer's own admins.
  - **AWS.** Support gets in without per-case approval, but only through a role limited to metadata. Every call it makes shows up in the customer's CloudTrail.
  - **Doppler.** Support gets in only after the customer invites it, but once invited it can read whatever it was granted. Access ends only when someone revokes it by hand.
- **Infisical Cloud and HCP Vault Dedicated publish no customer approval step for staff access.** Infisical says customer data "can only be accessed currently by executive management". HashiCorp says support access is "need-to-access", and it holds each cluster's unseal key and a root token during updates.
- **Every vendor's statement about staff access is a policy or tooling claim, except where the keys are absent.** Only 1Password, Bitwarden and AWS KMS (for the key material itself) make guarantees that rest on the architecture. Even 1Password and Bitwarden state, or have had shown by others, that a malicious server can get at keys (ETH Zurich, 2026).
- **The only cross-tenant isolation failure found in any vendor's published record is Doppler PSA-2024-002.** A GitHub App could be linked into another tenant's workplace [DOP-PSA]. The rest are privilege escalations within a tenant, or failures outside the store itself.
- **Published incidents cluster in three places:** the supply chain (Bitwarden CLI on npm, the HashiCorp signing keys), identity providers (1Password through Okta), and plaintext leaking into telemetry (Doppler through FullStory and BugSnag). The telemetry case is the closest real-world match to the TLS-termination logging risk in Hikyo's honest statement.

## Comparison table

| | Key custody: who can decrypt on the server | Support: approval, scope, expiry, what it sees | Staff and production separation | Breach-notice commitment | Published incidents | Claim versus guarantee |
|---|---|---|---|---|---|---|
| **1Password** | Nobody on the server. Keys are made on the client. Each vault has its own key, wrapped to its members' public keys. The server holds only encrypted keys [1P-WP p.2, p.47] | No support route into data: "We don't have the ability to recover your data" [1P-WP p.2]. Recovery is done by the customer's own Recovery Group or admins [1P-WP §12; 1P-REC] | Staff controls not found in the white paper. The architecture limits what a server breach reveals to the cleartext columns listed [1P-WP §15.2] | DPA v4.8: "within 72 hours (or otherwise without undue delay)" for a material personal data breach [1P-DPA §8.1] | Okta support-system breach reached 1Password's Okta tenant. Detected 2023-09-29, posted 2023-10-23, "no 1Password user data was accessed" [1P-OKTA] | The white paper itself names its limits: a malicious server can serve a malicious web client (A.1) or supply dishonest public keys "with little ability for users to detect" (A.3) [1P-WP App. A]. ETH found 6 attacks [ETH] |
| **Bitwarden** (Password Manager and Secrets Manager) | Nobody on the server. "The unprotected Organization Symmetric Key is never stored on Bitwarden servers" [BW-WP]. An SM access token carries its own key, which decrypts the org key "locally" [BW-SMDEC] | "Bitwarden cannot access Vault Data" [BW-WP]. Support will not disable 2FA [BW-WP]. Recovery is an Enterprise admin feature, and works only for members who enrolled beforehand [BW-WP; BW-FORGOT] | Least privilege, manager approval of access changes, offboarding, SIEM and SOC monitoring [BW-WP]. Procedural | Incident response plan exists [BW-WP]. DPA notice window: **unverified**, no public DPA found | CLI 2026.4.0 on npm was malicious from 17:57 to 19:30 ET on 2026-04-22. Statement posted 2026-04-23, CVE-2026-42994, "no evidence that end user vault data was accessed" [BW-NPM] | ETH found 12 attacks against a malicious server. Bitwarden: 7 fixed or in remediation, 3 "accepted" as "necessary for product functionality" [ETH; BW-ETH] |
| **Doppler** | Doppler. A separate tokenization service encrypts with a 256-bit key per workplace, which is wrapped by an HSM-backed GCP KMS key. Web servers hold only opaque tokens [DOP-FS]. EKM adds a customer KMS layer, and the data "still resides on Doppler's infrastructure" [DOP-EKM] | **Customer approval required:** the customer invites the support agent's email on the team page, ideally scoped to projects and environments. No automatic expiry: "Once the support ticket has been closed, you should revoke" [DOP-FS]. Once invited, support sees whatever the grant allows | "Employees cannot access sensitive customer data without explicit customer approval". "Does not and will never build a 'God Mode'". Tokenization egress allowlist, security-key MFA [DOP-FS]. Logging of employee access not described | DPA §4.3: "without undue delay of any reasonably suspected or actual Security Incident", with a summary and corrective action. Notice is not an admission of fault [DOP-DPA]. Public advisory list, plus email when data is affected [DOP-FS] | 10 advisories from 2021 to 2024 [DOP-PSA]. Notable: **PSA-2022-001**, FullStory session recordings did not mask secrets. **PSA-2024-001**, secrets "may have been sent to BugSnag". Five privilege escalations within a workplace (2021-001, 2023-001/002/003, 2024-003). Operator namespace escape (2022-002). **PSA-2024-002, a cross-tenant failure:** any GitHub org member could link that org's existing Doppler GitHub App to a new workplace. Latest entry is 2024-004, with none listed for 2025–2026 | "No God Mode" is a tooling promise. Keys sit in Doppler's KMS and the server decrypts per request, so production access is enough to decrypt [DOP-FS] |
| **Infisical Cloud** | Infisical. An operator root key (from an env var or an HSM) wraps an internal KMS root key. Each org and each project has its own data key. Project keys can use an external KMS. "The server ... decrypts requested data" [INF-SEC] | No published support-access flow (**unverified**). "Customer data can only be accessed currently by executive management". Changes are "prohibited without explicit customer approval" [INF-SEC]. The org admin console's "Join as Admin" is audited, but that is an org-admin feature, not a staff one [INF-ORGADM] | Least privilege [INF-SEC]. Anomaly monitoring on authentication [INF-SEC] | DPA §8: "without undue delay, and in any event within seventy-two (72) hours" [INF-DPA]. Disclosure policy tells self-hosted and enterprise customers before announcing [INF-VDP] | No GitHub security advisories published on the main repo [INF-GHSA]. Its eMudhra CA post (Oct 2026) describes a third-party CA compromise, not an Infisical compromise [INF-EMU] | E2EE was made optional in 2023 because it was "perceived more as a nice-to-have" [INF-2023]. A Sept 2026 PR removed the docs' E2EE references, saying E2EE was removed "~1.5 years ago" [INF-PR]. "Executive management" access is a staff policy, not a key boundary |
| **HCP Vault Dedicated** | HashiCorp. Each customer gets a dedicated HCP org and cloud account, and each cluster gets its own KMS key, which HashiCorp manages. "Cluster auto unseal is managed by HashiCorp". No external KMS [HCP-SEC]. **A cluster per tenant, unlike Hikyo's many orgs per installation** | "Restricted to support staff on a need-to-access basis". No customer approval step is documented [HCP-SEC]. During updates a root token is generated, "visible in the audit logs", and used "only ... on non-user managed paths" [HCP-SEC] | The MSP policy updates every cluster [HCP-SEC]. Shared-responsibility model: HashiCorp runs backups, OS and upgrades [HCP-OV] | Not in the security overview [HCP-SEC]. DPA: **unverified** | HCSEC-2021-12: the Codecov breach exposed the release-signing GPG key. Bulletin published 2021-04-22, a week after Codecov's own disclosure [HC-2021]. HCSEC-2026-33: an incident in the internal delivery pipeline, key rotated 2026-09-10 [HC-2026] | "Non-user paths only" is a policy: a root token can read every path. The token is described as used "only root level sys paths". The docs say: "Please ignore any root token audit logs tied to the msp, dr-primary, or pr-secondary policies" [HCP-SEC] |
| **AWS Secrets Manager** | AWS KMS. Each secret version has its own data key, wrapped by a KMS key. The encryption context binds the secret ARN and version. A customer-managed key lets the customer write the key policy. The `aws/secretsmanager` key policy cannot be changed [AWS-SM]. No plaintext export of KMS keys, and HSM admin needs a quorum of operators [AWS-KMS] | **No per-case approval.** The `AWSServiceRoleForSupport` role exists in every account. It gives "access to resource metadata, not customer data", and every call it makes is in the customer's CloudTrail [AWS-SUP]. Its policy (v60) lists `secretsmanager:describeSecret/listSecrets/...` and `kms:describeKey/...`, The only `secretsmanager:` actions are `describeSecret`, `getResourcePolicy`, `listSecrets` and `listSecretVersionIds`. There is no `getSecretValue` or `kms:Decrypt` (checked against the raw page) [AWS-SUPPOL]. Enterprise Support customers can delete the role [AWS-SUP] | KMS HSM quorum [AWS-KMS]. Nitro "no operator access" covers **EC2 hosts only** [AWS-NITRO], not the Secrets Manager service fleet. DPA: personnel confidentiality and least privilege [AWS-DPA] | DPA §9.1: notify "without undue delay". §9.3: unsuccessful incidents are excluded, and notice is not an admission of fault [AWS-DPA] | None found for Secrets Manager or KMS. The bulletins page shows only recent items [AWS-BULL] | Secrets Manager decrypts in the service, on the caller's behalf. With a customer-managed key, each Decrypt appears in the customer's CloudTrail [AWS-SM]. The metadata-only claim for support is backed by an IAM policy the customer can inspect |

### The owner's two parts, per vendor

| Vendor | Support needs the customer's approval? | Can support or the operator read secrets? | Source |
|---|---|---|---|
| 1Password | No path exists | No (cryptographic), apart from the malicious-server limits A.1 and A.3 | [1P-WP] |
| Bitwarden | No path exists | No (cryptographic), apart from the ETH malicious-server findings | [BW-WP; ETH] |
| Doppler | **Yes**, the customer invites support | **Yes**, within the grant. Revocation is manual | [DOP-FS] |
| Infisical Cloud | Not published | Yes, as a matter of policy ("executive management") | [INF-SEC] |
| HCP Vault Dedicated | Not documented | Technically yes: HashiCorp holds the unseal key and creates root tokens | [HCP-SEC] |
| AWS Secrets Manager | No | The support role has no read access to secret values. The service itself decrypts | [AWS-SUP; AWS-SUPPOL] |

## Per-vendor notes

### 1Password
- **Isolation.** Keys are made and used only on the client. The server never learns the account password or the keys. Two-secret key derivation (the account password plus a locally held Secret Key, "128-bit strong") means data the server stores "cannot be used in cracking attempts" [1P-WP p.2].
- **What a breach reveals.** The white paper lists the cleartext columns "subject to disclosure in the event of a data breach": team domain and name, device IPs, MFA secrets, device make, model and OS, public keys, group names, and users' names and emails [1P-WP §15.2].
- **Recovery.** A copy of each vault key is encrypted to the Recovery Group. A recovering member gets the encrypted vault keys only after the user re-creates their account. Recovery is cancelled if the user signs in with the old credentials [1P-WP §12.4–12.5]. Recovery keys add server-side policies: recovery aborts if the user authenticated in the prior hour, or if the key had an aborted attempt in the prior 24 hours [1P-WP §12.6.3].
- **Limits it publishes.** A.1: "An attacker capable of changing the web client on the server could deliver a malicious client". A.3: a compromised server could "provide dishonest public keys". A.9: governments may obtain server data [1P-WP App. A].
- **ETH Zurich response.** 1Password says the paper adds nothing beyond these documented limits. It names the root cause as "server-mediated key distribution without strong key provenance", and says fixing it needs public-key verification and a new group-encryption model, with no timeline given [1P-ETH].

### Bitwarden
- **Isolation.** Master keys never leave the client. The org key is stored only in encrypted form [BW-WP]. An SM access token has two parts: an API key, and "a unique encryption key" that decrypts the org key "locally" [BW-SMDEC].
- **Token revocation.** Revoking a token blocks new authentication, but "a machine that has already authenticated may continue to retrieve and decrypt secrets for up to one hour" [BW-TOK].
- **Support.** Bitwarden cannot access vault data, and its policy forbids disabling 2FA on a user's behalf [BW-WP]. Without a recovery option set up in advance, "there is no way for Bitwarden to recover the account" [BW-FORGOT].
- **Audits.** Published third-party reports, including ETH Zurich's 2025 cryptography report "assuming a fully malicious server" [BW-AUD].

### Doppler
- **Isolation.** A per-workplace key is "only made available to the tokenization service during the lifetime of the request" and "never persisted outside of application memory". If a web server or its database is breached, the attacker gets "only ... opaque tokens" [DOP-FS].
- **Support access.** It is an ordinary team invite: the customer adds the agent's email and should limit it to the relevant projects and environments. After the ticket closes, the customer "should revoke" it, or the agent can remove themselves [DOP-FS]. There is no time limit and no separate support role.
- **The only cross-tenant isolation failure found across all six vendors** is Doppler PSA-2024-002. Any member of a GitHub organization could link that organization's existing Doppler GitHub App to a new Doppler workplace. Customers who needed to act were emailed [DOP-PSA]. The other isolation failures on the list are privilege escalations inside a workplace (2021-001, 2023-001, 2023-002, 2023-003, 2024-003) and a Kubernetes operator namespace escape (2022-002) [DOP-PSA].
- **Incident history.** The list's latest entry is 2024-004. As of 2026-11-12 it lists nothing from 2025 or 2026. Doppler keeps a public list of customer-affecting issues [DOP-PSA], linked from the fact sheet [DOP-FS]. Two entries are plaintext reaching third-party telemetry: FullStory recordings (2022-001) and BugSnag error reports (2024-001). One is an audit gap: revealed secret versions "were not tracked" (2024-004).

### Infisical Cloud
- **Isolation.** "Each organization and each project has its own dedicated data key, providing cryptographic isolation between tenants". The root key "never leaves the server's memory" [INF-SEC]. This is the same shape as Hikyo's Managed mode: all keys sit under one operator root.
- **Staff access.** Least privilege, and "customer data can only be accessed currently by executive management of Infisical" [INF-SEC]. No support-approval flow is published.
- **Public wording drift.** The June 2023 post made E2EE optional [INF-2023]. A merged PR of 2026-09-03 removed the E2EE references from the docs, saying E2EE had been removed about 1.5 years earlier [INF-PR]. The exact removal date is **unverified**.

### HCP Vault Dedicated
- **Isolation by infrastructure.** "Both the HCP organization and CSP are dedicated to a single HCP customer". "Vault instances are not shared between organizations". The per-cluster key "is not shared between clusters or tenants" [HCP-SEC].
- **Operator powers.** HashiCorp manages auto-unseal and does not support external KMS keys [HCP-SEC]. During updates the MSP policy generates a root token, which is visible in the audit logs and destroyed afterwards [HCP-SEC]. No customer approval step is documented for support access to the storage account [HCP-SEC].
- **Incident handling.** In 2021 HashiCorp learned of the Codecov exposure from Codecov's public disclosure (2021-04-15) and published HCSEC-2021-12 on 2021-04-22. It rotated the GPG key and re-signed releases, and found "no evidence of HashiCorp customer data disclosure" [HC-2021]. HCSEC-2026-33 (2026-09-10) reports an internal pipeline incident and a precautionary rotation of the Linux signing key. It does not say how the incident was detected [HC-2026].

### AWS Secrets Manager
- **Isolation.** Each secret value is encrypted with its own data key. "None of the Secrets Manager APIs return the encrypted secret or the encrypted data key". The name, description, rotation settings, KMS key ARN and tags are not encrypted [AWS-SM].
- **The customer can fence the service.** With a customer-managed key, the customer can require `kms:ViaService` and conditions on the encryption context [AWS-SM].
- **KMS operators.** "There is no mechanism to export plaintext KMS keys". "Multiple Amazon employees with role-specific access to quorum-based access controls are required to perform administrative actions on the HSMs" [AWS-KMS].
- **Support.** The service-linked role gives metadata only and redacts fields that may hold customer data. All its calls appear in CloudTrail. The role cannot be deleted except by Enterprise Support customers in AWS Organizations, through their TAM [AWS-SUP].
- **Contract.** "We do not access or use your content for any purpose without your agreement" [AWS-PRIV]. Incident notice "without undue delay" [AWS-DPA §9.1].

## Breach detection and disclosure timing

| Vendor | How detection is described | Detection to public disclosure (published cases) |
|---|---|---|
| 1Password | Own controls: "Our systems and policies were able to identify and terminate this attack". Tooling not named [1P-OKTA] | Okta case: detected 2023-09-29, cause confirmed 2023-10-20, blog post 2023-10-23, updated 2023-10-25 [1P-OKTA] |
| Bitwarden | SIEM, SOC and continuous monitoring. Incident response plan covers detection through post-incident [BW-WP] | npm case: malicious window ended 2026-04-22 at 19:30 ET, statement posted 2026-04-23, CVE issued [BW-NPM]. Initial access not explained in the statement |
| Doppler | Not described [DOP-FS] | Public advisory list, plus email to affected customers [DOP-FS; DOP-PSA]. Advisory dates are not shown beyond the year |
| Infisical | "Monitors for any anomalous activities such as authentication attempts from previously unseen sources" [INF-SEC] | No Infisical Cloud incident found. The DPA's outer limit is 72 hours [INF-DPA] |
| HashiCorp (HCP) | Not described [HCP-SEC]. In 2021 it learned of the exposure from a supplier's public disclosure [HC-2021] | Codecov case: supplier disclosure 2021-04-15, bulletin 2021-04-22 [HC-2021]. HCSEC-2026-33 does not say how the incident was detected [HC-2026] |
| AWS | DPA: "defined processes to detect, mitigate, investigate, and report security incidents" [AWS-DPA, Annex] | No Secrets Manager or KMS incident found [AWS-BULL] |

## Patterns worth copying

1. **State the limits in the security document itself.** 1Password's Appendix A ("Beware of the leopard") names the malicious-server, malicious-client and key-substitution limits in its own white paper [1P-WP App. A]. When outside researchers found these attacks, its response could point to limits already in the white paper [1P-ETH]. This is the closest peer to Hikyo's honest statement.
2. **Publish the list of cleartext fields.** 1Password lists exactly which columns a breach would expose [1P-WP §15.2]. AWS lists what Secrets Manager does not encrypt [AWS-SM].
3. **A support identity the customer can see and audit, with metadata-only scope written as policy.** AWS's support role is a named principal in the customer's account. Its permissions are published and versioned, and every call is in the customer's audit trail [AWS-SUP; AWS-SUPPOL].
4. **Support access granted by the customer and scoped to resources.** Doppler requires the customer to invite support and suggests scoping it to projects and environments [DOP-FS]. Its weak point is that expiry is manual.
5. **Bind ciphertext to its identity.** AWS's encryption context (secret ARN plus version) makes every KMS call self-describing in logs and usable in policy conditions [AWS-SM].
6. **Keep the decrypting service apart from internet-facing servers.** Doppler's tokenization service, with its egress allowlist, means a web-tier breach yields only tokens [DOP-FS].
7. **A public advisory register, including "no action needed" items.** Doppler's PSA list discloses issues that were fixed and never exploited [DOP-PSA]. HashiCorp's HCSEC bulletins give dates and rotation steps [HC-2021; HC-2026].
8. **Server-side guards on recovery.** 1Password aborts recovery if the real user signs in, and rate-limits recovery attempts [1P-WP §12.6.3].

## Patterns worth avoiding

1. **Telling customers to ignore operator audit entries.** HCP's docs say: "Please ignore any root token audit logs tied to the msp, dr-primary, or pr-secondary policies" [HCP-SEC]. That trains customers not to watch the one actor they cannot otherwise see.
2. **A root-equivalent operator credential limited only by policy.** HCP's update token "will only be used to access non-user managed paths (i.e., only root level sys paths)" [HCP-SEC]. That limit is a promise, because a root token is not limited by anything.
3. **Support grants without expiry.** Doppler leaves revocation to the customer or the agent [DOP-FS].
4. **Staff-access claims with no technical backing.** Infisical says access is limited to "executive management" [INF-SEC]. Doppler says there is no "God Mode" [DOP-FS]. Both services decrypt on the server with keys the operator holds, so these are statements about people and tooling, not guarantees.
5. **Letting public wording lag behind the architecture.** Infisical's E2EE references outlived E2EE by about 1.5 years [INF-PR].
6. **Plaintext reaching telemetry and session-replay tools.** Doppler's FullStory and BugSnag advisories [DOP-PSA] are concrete cases of the "capture at TLS termination by logging or tracing tools" risk in Hikyo's honest statement.
7. **Revocation that does not end existing sessions.** Bitwarden SM machines keep decrypting for up to an hour after their token is revoked [BW-TOK].
8. **Supply chain and the identity provider as the way in.** Three of the incidents above did not touch the secret store itself: Bitwarden's npm package [BW-NPM], HashiCorp's CI and delivery pipeline [HC-2021; HC-2026], and 1Password's Okta tenant [1P-OKTA]. Each vendor reported that customer data was not accessed.

## Gaps and unverified items

- Bitwarden's, HashiCorp's and Infisical Cloud's support-access procedures (approval, duration, logging): not published in the sources found.
- Bitwarden's and HashiCorp's DPA breach-notice windows: no public DPA found.
- 1Password's internal staff and production-access controls: not in the white paper. Its SOC 2 report is not public.
- Exactly when Infisical removed E2EE: only "~1.5 years" before 2026-09-03, per the PR.
- Doppler's advisory pages list entries without dates beyond the year in their IDs. I did not open the individual entries.
- ETH counts are from ePrint 2026/058 revision 4 (2026-08-12): 12 attacks on Bitwarden, 7 on LastPass, 6 on Dashlane and 6 on 1Password. Press reports give other counts for 1Password, and I did not use them.

## Sources

- [1P-WP] 1Password Security Design white paper (PDF): https://1passwordstatic.com/files/security/1password-white-paper.pdf (HTML edition: https://agilebits.github.io/security-design/)
- [1P-REC] 1Password, Recover accounts for team members: https://support.1password.com/recovery/
- [1P-DPA] AgileBits DPA v4.8, last revised 2026-07-30, §8.1: https://1password.com/files/legal/agilebits-dpa-012026.pdf
- [1P-OKTA] 1Password, Okta Support System incident (2023-10-23, updated 2023-10-25): https://1password.com/blog/okta-incident
- [1P-ETH] 1Password, response to the ETH Zurich paper (2026-02-16): https://1password.com/blog/eth-zurich-zero-knowledge-malicious-server-review
- [BW-WP] Bitwarden Security Whitepaper: https://bitwarden.com/help/bitwarden-security-white-paper/
- [BW-SMDEC] Bitwarden, Secret Decryption: https://bitwarden.com/help/secret-decryption/
- [BW-TOK] Bitwarden, Access Tokens: https://bitwarden.com/help/access-tokens/
- [BW-FORGOT] Bitwarden, Forgot Master Password: https://bitwarden.com/help/forgot-master-password/
- [BW-AUD] Bitwarden, Compliance, Audits and Certifications: https://bitwarden.com/help/is-bitwarden-audited/
- [BW-NPM] Bitwarden statement on the Checkmarx supply chain incident (2026-04-23): https://community.bitwarden.com/t/bitwarden-statement-on-checkmarx-supply-chain-incident/96127
- [BW-ETH] Bitwarden, response to the ETH Zurich audit (2026-02-16): https://bitwarden.com/blog/security-through-transparency-eth-zurich-audits-bitwarden-cryptography/
- [ETH] Scarlata, Torrisi, Backendal, Paterson, "Zero Knowledge (About) Encryption", IACR ePrint 2026/058, revision 2026-08-12: https://eprint.iacr.org/2026/058
- [DOP-FS] Doppler Security Fact Sheet: https://docs.doppler.com/docs/security-fact-sheet
- [DOP-EKM] Doppler Enterprise Key Management: https://docs.doppler.com/docs/enterprise-key-management
- [DOP-DPA] Doppler DPA, §2.6 and §4.3: https://www.doppler.com/legal/dpa
- [DOP-PSA] Doppler Public Security Alerts (rendered in a browser on 2026-11-12): https://dopplerteam.notion.site/Public-Security-Alerts-284ad8274b5948f180faf20986fe947f
- [INF-SEC] Infisical, Security internals: https://infisical.com/docs/internals/security
- [INF-ORGADM] Infisical, Organization Admin Console: https://infisical.com/docs/documentation/platform/admin-panel/org-admin-console
- [INF-DPA] Infisical DPA, §3 and §8: https://infisical.com/terms/dpa
- [INF-VDP] Infisical Vulnerability Disclosure Policy: https://infisical.com/vulnerability-disclosure
- [INF-GHSA] Infisical GitHub security advisories: https://github.com/Infisical/infisical/security/advisories
- [INF-EMU] Infisical, response to the eMudhra CA compromise: https://infisical.com/blog/emudhra-certificate-authority-compromise
- [INF-2023] Infisical Update, June 2023: https://infisical.com/blog/infisical-update-june-2023
- [INF-PR] Infisical PR #7974, "docs(security): remove e2e references", merged 2026-09-03: https://github.com/Infisical/infisical/pull/7974
- [HCP-SEC] HCP Vault Dedicated security overview: https://developer.hashicorp.com/hcp/docs/vault/what-is-hcp-vault/security-overview
- [HCP-OV] HCP Vault Dedicated overview: https://developer.hashicorp.com/hcp/docs/vault/what-is-hcp-vault
- [HC-2021] HCSEC-2021-12, Codecov and the HashiCorp GPG key exposure: https://discuss.hashicorp.com/t/hcsec-2021-12-codecov-security-event-and-hashicorp-gpg-key-exposure/23512
- [HC-2026] HCSEC-2026-33, Linux signing GPG key update: https://discuss.hashicorp.com/t/hcsec-2026-33-hashicorp-linux-signing-gpg-key-update-ca026560/77734
- [AWS-SM] AWS Secrets Manager, secret encryption and decryption: https://docs.aws.amazon.com/secretsmanager/latest/userguide/security-encryption.html
- [AWS-KMS] AWS KMS cryptographic details, design goals: https://docs.aws.amazon.com/kms/latest/cryptographic-details/design-goals.html
- [AWS-NITRO] Security design of the AWS Nitro System, "No AWS operator access": https://docs.aws.amazon.com/whitepapers/latest/security-design-of-aws-nitro-system/no-aws-operator-access.html
- [AWS-SUP] AWS Support, service-linked roles: https://docs.aws.amazon.com/awssupport/latest/user/using-service-linked-roles-sup.html
- [AWS-SUPPOL] AWSSupportServiceRolePolicy, v60, edited 2026-10-26: https://docs.aws.amazon.com/aws-managed-policy/latest/reference/AWSSupportServiceRolePolicy.html
- [AWS-PRIV] AWS Data Privacy FAQ: https://aws.amazon.com/compliance/data-privacy-faq/
- [AWS-DPA] AWS GDPR DPA, §4 and §9: https://d1.awsstatic.com/legal/aws-dpa/aws-dpa.pdf
- [AWS-BULL] AWS Security Bulletins: https://aws.amazon.com/security/security-bulletins/
