# Research: GDPR obligations for a hosted secrets processor

Date: 2026-11-12. Ticket: [Hikyo-Org/Hikyo#1305](https://github.com/Hikyo-Org/Hikyo/issues/1305).

This document lists what the law requires and what each requirement implies for product design. It makes no decisions. It is not legal advice. A Dutch privacy lawyer should review the DPA and the transfer analysis before launch.

**Sources.** Only primary sources: legislation, EDPB documents, Autoriteit Persoonsgegevens (AP) publications, CJEU and General Court material, and Dutch government pages. EUR-Lex blocks automated access with a bot challenge, so the legislative texts were read from the Publications Office Cellar copy of the same CELEX documents (for example `publications.europa.eu/resource/celex/32016R0679`). The citations give the canonical EUR-Lex URLs. CJEU judgments are cited through the Court's own press releases where the full text could not be fetched. The press releases say they do not bind the Court.

**Context taken from the ADR** (`docs/adr/hosted-service.md` in the Hikyo repository, read for context only). The hosted service runs `sealed-only` (decision 2). Without unattended access, a datastore dump plus the root key yields no Sealed values. Names, schemas and audit metadata stay unencrypted (decision 5). The operator's reach into a Sealed org is limited to lifecycle (decision 6). Support cannot recover a Sealed org (decision 8). Anything terminating TLS sees plaintext values in responses, and holder secrets travel in an HPKE envelope with no forward secrecy (decision 12).

---

## Summary

1. **Roles.** For customer content, including secret values, org metadata and the customer's audit trail, Hikyo is a **processor**. For its own account, billing and service-operation data, Hikyo is a **controller**. Encryption does not remove processor status: the EDPB's own example of a host storing encrypted data is a processor.
2. **The DPA.** Art. 28(3) fixes the minimum content. Two duties need real product work. Sub-processor changes must be actively notified, so a list page is not enough. At the end of service, data must be deleted or returned, including existing copies, and deletion must be confirmed.
3. **Breaches.** Hikyo, as processor, must notify **each affected customer** "without undue delay". Hikyo does not assess the risk. The 72-hour clock and the decision to tell data subjects belong to the customer. Because Hikyo cannot see inside a Sealed org, the useful notice is the key facts: which keys were exposed, which environments had unattended access, and the time window. The encryption exemption the AP and EDPB describe holds only if the key was never at risk.
4. **Erasure and backups.** The GDPR has no backup carve-out. The EDPB's 2025 enforcement report accepts that backups may not be edited, but expects documented backup retention and a way to re-apply erasure after a restore. No primary source endorses crypto-shredding as erasure.
5. **Residency.** The GDPR requires no localisation. Chapter V governs transfers, and remote access from outside the EEA counts as a transfer. A TLS-terminating CDN is very likely a sub-processor. If it is US-linked, its lawfulness rests on the EU-US Data Privacy Framework (DPF), which is under appeal (C-703/25 P). If the DPF falls, EDPB Use Case 6 says no technical measure helps when the importer sees plaintext.
6. **NIS2 and the CRA.** NIS2 counts SaaS as a cloud computing service, but its size cap probably excludes Hikyo for now. It still reaches Hikyo through customers' supply-chain duties. The Dutch Cyberbeveiligingswet has been in force since 15 August 2026. The CRA plausibly applies to Hikyo's distributed binaries if the hosted service monetises them. Its vulnerability-reporting duty has applied since 11 September 2026.

---

## 1. Roles: processor for customer content, controller for its own data

| Finding | Source |
|---|---|
| A processor "processes personal data on behalf of the controller" (Art. 4(8)). Processing includes "storage", "retrieval", "consultation" and "disclosure by transmission" (Art. 4(2)). | [GDPR Art. 4(2), 4(8)](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng) |
| A host that "does not determine whether the data it hosts are personal data" and only stores **encrypted** data is still a processor. An Art. 28 agreement is required, and the host must notify breaches. | [EDPB Guidelines 07/2020 v2.1](https://www.edpb.europa.eu/system/files/documents/2023-10/EDPB_guidelines_202007_controllerprocessor_final_en.pdf), "Example: Hosting services", p. 16 |
| A communications provider is normally the controller for data "necessary for the operation of the service as such (e.g., traffic and billing data)", but not for the content it carries. | Same, "Example: Telecom operators", pp. 12-13 |
| A provider that "inevitably" and "systematically" has access to personal data while delivering the service is a processor, even if that access is not the main purpose of the service. | Same, para. 83 and "Example: General IT support", pp. 27-28 |
| A processor that decides purposes and means itself "shall be considered to be a controller in respect of that processing". | [GDPR Art. 28(10)](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng) |
| Pseudonymised data are not personal data "in all cases and for every person". Whether the data subject is identifiable depends on the circumstances. For the controller's duty to inform, identifiability is assessed "at the time of collection of the data and from the point of view of the controller". Decided under Regulation 2018/1725, which uses the same concept. | [CJEU C-413/23 P, EDPS v SRB, press release 107/25](https://curia.europa.eu/site/upload/docs/application/pdf/2025-09/cp250107en.pdf) |

**What this implies for the product:**

- **Secret values are in scope even when Hikyo cannot read them.** SRB opens an argument that ciphertext Hikyo cannot decrypt is not personal data *for Hikyo*. It does not undo the hosting example, which treats storing encrypted data for a customer as processing for that customer. The safe design assumption is that Sealed values are processed on the customer's behalf. Whether a secret contains personal data is the customer's question. Some do, for example a person's own credentials, or a value that names a person. Hikyo cannot know, so the DPA has to cover all values.
- **Plaintext metadata is plainly personal data.** Member emails, display names, IP addresses in the audit trail and session records all qualify.
- **Hikyo needs a controller-side privacy notice and lawful basis** for account, billing, signup, abuse prevention and service logs. These are separate from the DPA.
- **Art. 28(10) risk.** Any use of customer-org data for Hikyo's own purposes, such as product analytics, usage-based upselling beyond billing, or training, could make Hikyo a controller for that processing. Telemetry design should stay within documented instructions, or be disclosed and limited to what billing and security need.

## 2. Article 28: the DPA and sub-processors

Art. 28(3) requires a written contract (Art. 28(9) allows electronic form) setting out the subject-matter, duration, nature and purpose, types of data and categories of data subjects. Points (a) to (h) must be in it. The Commission's Art. 28(7) standard clauses can be used as the base ([Implementing Decision (EU) 2021/915](https://eur-lex.europa.eu/eli/dec_impl/2021/915/oj/eng)).

| Art. 28(3) clause ([GDPR](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)) | What it implies for Hikyo |
|---|---|
| (a) Process only on documented instructions, **including with regard to transfers**. Inform the controller of any legal requirement to process otherwise, unless that is prohibited. | The product's documented behavior is the instruction set. Any feature that moves data, such as adapter sync to a third-country destination, needs to be the customer's own instruction. That fits decision 11, where a holder enables it. EDPB Opinion 22/2024 says the words "unless required to do so by Union or Member State law" are "highly recommended but not mandatory". A broader "law or binding order of a governmental body" clause stays within contractual freedom, but does not release the processor from its duties ([Opinion 22/2024](https://www.edpb.europa.eu/system/files/2024-10/edpb_opinion_202422_relianceonprocessors-sub-processors_en.pdf), executive summary, pp. 2-3). |
| (b) Staff are bound to confidentiality. | Employment and contractor terms. Fits decision 6, where support access is approved by the org and audited. |
| (c) Take all measures required by Art. 32. | See section 3. |
| (d) Sub-processor conditions (Art. 28(2) and (4)). | See the sub-processor list below. |
| (e) Assist with data-subject requests "insofar as this is possible". | Hikyo cannot search Sealed values, so the assistance must be **self-service**. The customer needs to find, export and delete values and member data itself. Values, `export` and per-org export exist. Member and audit data need the same. |
| (f) Assist with Arts. 32 to 36 "taking into account the nature of processing and the information available to the processor". | This covers breach notice (section 4) and DPIA support: a published description of the Sealed and Managed boundaries, which the ADR's honest statement already provides. |
| (g) At the controller's choice, delete or return all personal data at the end of the service, "and deletes existing copies". | Per-org export (decision 10) is the "return" path. Per-org deletion must cover backups (section 5). The EDPB says deletion must be secure, and the processor "should confirm to the controller that the deletion has been completed within an agreed timescale and in an agreed manner". The contract should let the controller change its choice before the end ([07/2020](https://www.edpb.europa.eu/system/files/documents/2023-10/EDPB_guidelines_202007_controllerprocessor_final_en.pdf) paras. 139-142, p. 40). |
| (h) Provide information to demonstrate compliance, and allow audits and inspections. | An audit pack: security description, test results, certifications if any, sub-processor list. The open-source server helps, because customers can inspect it. |
| Final subparagraph: immediately tell the controller if an instruction infringes data-protection law. | A support process, not a product feature. |

**Sub-processors (Art. 28(2) and (4)):**

- The processor needs "prior specific or general written authorisation". Under a general authorisation, it must tell the controller of "any intended changes concerning the addition or replacement of other processors", so the controller can object ([GDPR Art. 28(2)](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)).
- The controller must receive a list of intended sub-processors with "their locations, what they will be doing and proof of what safeguards have been implemented". The contract must set a timeframe for objection and the steps after an objection ([07/2020](https://www.edpb.europa.eu/system/files/documents/2023-10/EDPB_guidelines_202007_controllerprocessor_final_en.pdf) paras. 152-158, pp. 42-43).
- It is "not sufficient for the processor to merely provide the controller with a generalized access to a list of the sub-processors which might be updated from time to time". The processor "must actively inform the controller of any change" (07/2020 footnote 54, p. 38).
- Controllers should have the identity (name, address, contact person) of **all** processors and sub-processors "readily available at all times", whatever the risk. The processor "should proactively provide" it and keep it up to date ([Opinion 22/2024](https://www.edpb.europa.eu/system/files/2024-10/edpb_opinion_202422_relianceonprocessors-sub-processors_en.pdf), executive summary).
- The same obligations must be passed down to each sub-processor. Hikyo "shall remain fully liable to the controller" for them ([GDPR Art. 28(4)](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)). The AP says the same in Dutch: written consent is needed, and the sub-processor must offer "minimaal hetzelfde niveau van gegevensbescherming" ([AP, Verwerkers](https://autoriteitpersoonsgegevens.nl/nl/onderwerpen/algemene-informatie-avg/verwerkers)).
- If a transfer outside the EEA happens between processors, "the processor/exporter should prepare the relevant documentation", meaning the transfer ground, the transfer impact assessment and supplementary measures, and the controller must be able to show it to the supervisory authority ([Opinion 22/2024](https://www.edpb.europa.eu/system/files/2024-10/edpb_opinion_202422_relianceonprocessors-sub-processors_en.pdf), p. 3).

**What this implies for the product:** a per-installation sub-processor register (hosting, database, backup storage, CDN or ingress, email, error tracking, payments), with location and purpose for each. Changes go out as an active notice to a named contact in each org, with an objection window, and the notice is recorded. Because the hosted control plane must use public interfaces only (decision 1), the register and notice can sit outside the core. Every customer still needs a reachable contact point.

**Other processor duties outside Art. 28:**

- **Art. 30(2) record of processing.** It must list each controller, the categories of processing, transfers, and a description of security measures. The under-250-staff exemption in Art. 30(5) does not apply when processing "is not occasional", and a hosted service processes continuously ([GDPR Art. 30](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)).
- **Art. 37 DPO.** A DPO is mandatory only for large-scale systematic monitoring, or large-scale special-category or criminal data, as a core activity. Neither appears to describe a secrets manager ([GDPR Art. 37(1)](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)). This is my reading. It is not confirmed by any authority.
- **Lead supervisory authority.** For cross-border processing, the authority of the main establishment leads. For a Netherlands operator, that is the AP ([GDPR Art. 56(1)](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)).
- **Liability.** A processor is liable for damage where it breached processor-specific duties or acted outside lawful instructions ([GDPR Art. 82(2)](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)).

## 3. Article 32: security

- Controller **and processor** must implement measures "appropriate to the risk", "taking into account the state of the art, the costs of implementation". The list includes encryption, ongoing confidentiality, integrity, availability and resilience, timely restore after an incident, and "a process for regularly testing, assessing and evaluating" the measures ([GDPR Art. 32(1)](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)). Recital 83 names encryption as a mitigation ([GDPR recital 83](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)).
- A breach alone does not prove the measures were inadequate: courts "must assess the appropriateness of those measures in a concrete manner". But "it is for the controller to prove that the protective measures implemented were appropriate", and fear of misuse can itself be non-material damage ([CJEU C-340/21, press release 191/23](https://curia.europa.eu/site/upload/docs/application/pdf/2023-12/cp230191en.pdf)). The case concerned a controller. Because Art. 32 binds processors too, customers will push this burden of proof onto Hikyo through the audit clause.

**What this implies for the product:**

- **Keep evidence of appropriateness.** That means the ADR's acceptance gates as test results, the independent cryptographic review (gate 9), restore drills (decision 5's Sealed restore proof), and the redaction invariants (gate 5). The CJEU burden of proof makes these the defence, not the design alone.
- **Art. 32(1)(c), restore in a timely manner.** `sealedSlotKey` loss "destroys every slot in the installation" (ADR decision 3). That is a single point of failure for availability, and it needs custody and escrow evidence.
- **Art. 32(4).** Anyone acting under the processor's authority may process data only on instructions. That fits decision 6: support access is approved by the org and mirrored to its trail.

## 4. Articles 33 and 34: breach notice when Hikyo cannot see inside a Sealed org

**Who does what:**

| Duty | Holder | Source |
|---|---|---|
| Notify the controller "without undue delay after becoming aware" | Processor (Hikyo) | [GDPR Art. 33(2)](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng) |
| Notify the supervisory authority within 72 hours where feasible, unless "unlikely to result in a risk" | Controller (customer) | GDPR Art. 33(1) |
| Tell data subjects if there is "high risk", unless an exemption applies, for example encryption that makes the data "unintelligible to any person who is not authorised" | Controller | GDPR Art. 34(1), 34(3)(a) |
| Document every breach | Controller | GDPR Art. 33(5) |

The EDPB adds the following ([Guidelines 9/2022 v2.0](https://www.edpb.europa.eu/system/files/2023-04/edpb_guidelines_202209_personal_data_breach_notification_v2.0_en.pdf), paras. 44-48, pp. 13-14):

- "The processor does not need to first assess the likelihood of risk arising from a breach before notifying the controller; it is the controller that must make this assessment." The processor "just needs to establish whether a breach has occurred".
- The controller is "in principle" aware once the processor has told it. The EDPB "recommends the processor promptly notifies the controller, with further information about the breach provided in phases".
- "Where the processor provides services to multiple controllers that are all affected by the same incident, the processor will have to report details of the incident to each controller."
- A processor may notify the authority on the controller's behalf if authorised by contract, but "the legal responsibility to notify remains with the controller". The AP agrees: "Een verwerker mag alleen datalekken bij de AP melden als u de verwerker daarvoor heeft gemachtigd". It also advises agreeing a dedicated, reachable breach contact point in the DPA ([AP, Verwerkers](https://autoriteitpersoonsgegevens.nl/nl/onderwerpen/algemene-informatie-avg/verwerkers)).

**The encryption exemption, and when it fails:**

- EDPB: a confidentiality breach of encrypted data is still a breach. But "if the confidentiality of the key is intact", the data are "in principle unintelligible" and notification may not be needed. If the key is later found compromised, "notification may still be required". An availability breach with no backup may still be notifiable ([9/2022](https://www.edpb.europa.eu/system/files/2023-04/edpb_guidelines_202209_personal_data_breach_notification_v2.0_en.pdf) paras. 76-80, pp. 18-20). Loss of a decryption key with no way to restore access is "a permanent loss of availability" (same, section on availability breaches, p. 8).
- The AP puts three conditions on the exemption: "The data are still fully intact", "You still have full control over the data (you have a recent backup)", and "The key used for the encryption ... was not at risk at any time during the data breach" ([AP, Reporting or not reporting a data breach](https://www.autoriteitpersoonsgegevens.nl/en/themes/security/data-breaches/reporting-or-not-reporting-a-data-breach)). The AP also says to register every breach internally, even one that need not be reported.

**How the ADR's cases map to these conditions.** This mapping is my analysis. No authority has ruled on it.

| Incident | Is the key plausibly intact? | What Hikyo can and should tell each affected org |
|---|---|---|
| Datastore or backup stolen. Root key safe. Environments without unattended access. | Yes, for Sealed values. **No**, for plaintext metadata (member emails, names, schemas, audit actors and IPs), which has no encryption at all. | The time window, the stolen data classes, that values were sealed, and which metadata was exposed. |
| Datastore or backup stolen **with** the root key | Sealed values without unattended access: yes (gate 1). Environments with unattended access, and adapter credentials: **no** (decision 11). | A list of the org's environments that had unattended access in the window, and the adapter credentials held. |
| Root key or `holderTransitKey` compromised, and traffic captures exist | **No** for holder secrets in captured envelopes: decision 12 has no forward secrecy, so recovery keys may be exposed. | Which holders presented secrets in the window, and a prompt to rotate holder keys. |
| Modified server or capture in production | **No**: holder secrets and response plaintext are exposed (the honest statement). | The time window of the hostile release, and the affected environments and holders. |
| `sealedSlotKey` lost | Not a confidentiality issue. It is an **availability breach** for every Sealed org on the installation. | A notice to every org on the installation. |
| CDN or ingress logs captured responses | **No**: values in responses are plaintext at TLS termination. | The routes and window logged. |

**What this implies for the product:**

- An incident tool that lists affected orgs, and for each org: the environments touched, whether each had unattended access in the window, the key classes exposed, and the time window.
- A per-org security contact that the customer must keep current. The AP stresses a reachable contact point.
- A DPA clause with a concrete early-notice target (the EDPB endorses contract-level early notice in para. 46) and phased updates.
- Optional authority to notify the AP on the customer's behalf. The AP offers a form for this, "Machtiging datalek melden door verwerker" ([AP, Meldplicht datalekken](https://autoriteitpersoonsgegevens.nl/nl/onderwerpen/beveiliging/meldplicht-datalekken)).
- Hikyo's own breach register as a controller for account data. Hikyo is also the controller-side notifier for its own account data.

## 5. Erasure versus retained backups

- **Art. 17 belongs to the controller.** The processor assists under Art. 28(3)(e), and at the end of service must "delete[] existing copies unless Union or Member State law requires storage" ([GDPR Art. 17, 28(3)(g)](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)). The GDPR text has no exception for backups.
- **EDPB Coordinated Enforcement 2025** ([report adopted 10 February 2026](https://www.edpb.europa.eu/system/files/2026-02/edpb_cef-report_2025_right-to-erasure_en.pdf), Issue 6, pp. 20-21):
  - "Depending on the technical settings and risks, it might not always be advisable to modify or delete information from back-ups. But, in that case, organisations should have appropriate procedures to keep track of erasure requests and comply with them on restored systems."
  - Relying on long, incremental deletion cycles made it hard for one authority to conclude that deletion happened "without undue delay".
  - Recommendations: "Follow established standards to erase and destroy data", and "Verify that erasure has been carried out and be able to demonstrate such erasure."
  - The EDPB "may consider" more guidance on backups. As of the report, none exists.
- **Crypto-shredding.** I found **no** EDPB or AP text that accepts destroying keys as erasure. Treat it as a risk-reduction measure, not as settled compliance.

**What this implies for the product:**

- **A documented backup retention period** per installation, stated in the DPA, so that erasure of backup copies happens on a known schedule.
- **An erasure ledger outside the backups** (deleted orgs, members and values), re-applied automatically after any restore. The ADR already notes that an org-level restore "brings back removed members' slots and retired key versions" (decision 8). The same restore would bring back data the customer erased.
- **Per-org deletion with a completion confirmation** to the customer (07/2020 para. 141). Per-org restore (decision 10) has to respect the ledger.
- **Sealed data in old backups** stays openable by the customer's holders together with `sealedSlotKey`. Hikyo cannot open it, but the customer can. So "deleted" must mean that Hikyo destroys the backups on schedule, not that they become unreadable to Hikyo.
- **Open design question: append-only audit trail versus erasure.** The tenant trail (audit-model ADR) keeps actor identities. For the customer's trail, retention is the customer's decision as controller, so expose a retention setting or documented behavior. For Hikyo's instance trail, Hikyo needs its own lawful retention basis. I did not research which Art. 17(3) exceptions would apply.

## 6. EU data residency and transfers

- **No localisation duty.** Chapter V permits transfers only under its conditions, "including for onward transfers" ([GDPR Art. 44](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)). The routes are adequacy (Art. 45), appropriate safeguards such as the Commission's SCCs (Art. 46(2)(c), [Implementing Decision (EU) 2021/914](https://eur-lex.europa.eu/eli/dec_impl/2021/914/oj/eng)), or derogations.
- **What counts as a transfer:** three cumulative criteria: an exporter subject to the GDPR, which makes data available to a separate importer, located in a third country. "Remote access from a third country (even if it takes place only by means of displaying personal data on a screen, for example in support situations ...)" is a transfer, and so is storage in a non-EEA cloud ([EDPB Guidelines 05/2021 v2.0](https://www.edpb.europa.eu/system/files/2023-02/edpb_guidelines_05-2021_interplay_between_the_application_of_art3-chapter_v_of_the_gdpr_v2_en_0.pdf), p. 8, Example 11 p. 12).
- **An EU processor subject to third-country law** (Example 12, p. 13) is not itself a transfer. The controller must still assess "sufficient guarantees" under Art. 28(1), because "reliability ... may be in doubt". If the processor answers a foreign authority's request against instructions, it becomes a controller under Art. 28(10).
- **Encryption as a supplementary measure** ([EDPB Recommendations 01/2020 v2.0](https://www.edpb.europa.eu/system/files/2021-06/edpb_recommendations_202001vo.2.0_supplementarymeasurestransferstools_en.pdf)):
  - Use Case 1 (p. 30): storage that does not need data in the clear can be protected if "the keys are retained solely under the control of the data exporter, or by an entity trusted by the exporter in the EEA".
  - Use Case 6 (pp. 34-35): where the processor "needs access to the data in the clear", the EDPB is "incapable of envisioning an effective technical measure". Transport and at-rest encryption "do not constitute a supplementary measure ... if the data importer is in possession of the cryptographic keys".
- **The US route.** [Implementing Decision (EU) 2023/1795](https://eur-lex.europa.eu/eli/dec_impl/2023/1795/oj/eng) Art. 1 finds adequacy only for "organisations in the United States that are included in the 'Data Privacy Framework List'". The General Court upheld it on 3 September 2025 ([T-553/23, press release 106/25](https://curia.europa.eu/site/upload/docs/application/pdf/2025-09/cp250106en.pdf)). An appeal is pending ([C-703/25 P, OJ C/2025/6610](https://eur-lex.europa.eu/eli/C/2025/6610/oj/eng)).
- **Schrems II** shows an adequacy decision can be annulled with immediate effect on transfers ([C-311/18, press release 91/20](https://curia.europa.eu/site/upload/docs/application/pdf/2020-07/cp200091en.pdf)).
- **AP practice (health sector).** The AP's March 2026 guide for health data in the cloud advises choosing "in beginsel een leverancier ... die niet onder de wet- en regelgeving van een derde land valt", and "beveelt ... aan dat de verwerking ... zich beperkt tot locaties binnen de EER". It asks for transparent data-centre locations, sub-processor plans and experience with foreign authority requests ([AP, Praktijkgids gezondheidsgegevens in de cloud](https://www.autoriteitpersoonsgegevens.nl/system/files?file=2026-03%2FPraktijkgids+gezondheidsgegevens+in+de+cloud.pdf), pp. 16-17). This is sector guidance. It binds no one generally, but healthcare customers will likely ask for it.

**What this implies for the product:**

- **Each non-EEA sub-processor adds a Chapter V transfer to document.** An EEA sub-processor whose parent is subject to non-EEA law adds a "sufficient guarantees" assessment. The register therefore needs each one's jurisdiction, including its parent company.
- **No support access from outside the EEA**, or document it as a transfer.
- **Use Case 1 fits only narrowly.** Server nodes decrypt during requests (decision 4), so a non-EEA party running or administering nodes falls under Use Case 6, not Use Case 1. Use Case 1 can fit a non-EEA backup or object-storage sub-processor that holds only ciphertext. Even then it covers Sealed values only. Plaintext metadata, environments with unattended access and adapter credentials in the same backup fail it, unless the whole backup is encrypted with keys kept in the EEA. Sealed does not by itself make non-EEA hosting acceptable.
- **Anything that sees plaintext** (CDN, ingress, error tracker, log pipeline) falls under Use Case 6 if it is non-EEA or subject to non-EEA law.
- **Show residency per installation.** Decision 1's installations make region a natural property to display.

## 7. Is a CDN that terminates TLS a sub-processor?

**I found no EDPB or AP text that addresses CDNs directly.** The conclusion below is inferred from the general tests.

1. **It processes personal data.** Client IP addresses are personal data: recital 30 names "internet protocol addresses" as online identifiers ([GDPR recital 30](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)), and the CJEU held a dynamic IP address can be personal data ([C-582/14 Breyer, press release 112/16](https://curia.europa.eu/site/upload/docs/application/pdf/2016-10/cp160112en.pdf), decided under Directive 95/46). Per the ADR, the terminator also sees session cookies, raw service-account bearer tokens, MCP traffic and **plaintext secret values in responses** (decision 12, "What it does not stop"). Art. 4(2) counts "disclosure by transmission" as processing.
2. **It does so on Hikyo's behalf, and access is inevitable.** That matches the EDPB's IT-support example of a provider that "inevitably" and "systematically" has access (07/2020 pp. 27-28), and the hosting example. The telecom example concerns **controller** status for message content. It does not exempt a transmission provider from processor status.
3. **So, very likely, it is a sub-processor under Art. 28(2) and (4).** It belongs on the list, the obligations must flow down, and changes must be actively notified.
4. **If the CDN is US-linked, it is a Chapter V question.** Under the DPF (Art. 45), no supplementary measures are needed while the CDN is on the DPF List. If the DPF falls, SCCs plus a transfer impact assessment would apply. Use Case 6 then very likely leaves **no effective measure**, because the CDN holds the TLS keys and sees plaintext. An EU-incorporated subsidiary of a US company is Example 12: not a transfer by itself, but a "sufficient guarantees" question.
5. **NIS2 also names "Content delivery network providers"** as digital infrastructure ([NIS2 Annex I point 8](https://eur-lex.europa.eu/eli/dir/2022/2555/oj/eng)). That is relevant to supply-chain assessment, not to the GDPR role.

**The design options this exposes** (no option is chosen here):

- **(a) TLS passthrough at the CDN, or no CDN.** The CDN sees only ciphertext and IP addresses. It is probably still a sub-processor for IP addresses, but out of Use Case 6.
- **(b) A TLS-terminating CDN that is EEA-only and not subject to third-country law.** Normal sub-processor handling applies.
- **(c) A TLS-terminating US-linked CDN.** This depends on the DPF and on the outcome of C-703/25 P. It also undercuts the honest statement's "sealed at rest" story for anything in responses.
- **The CDN's own controller processing** (for example security analytics on traffic) would need separate review of that vendor's terms. I did not research any vendor.

## 8. NIS2, only where it plausibly applies

- **SaaS is a cloud computing service.** "The service models of cloud computing include ... Software as a Service (SaaS)" ([NIS2 recital 33](https://eur-lex.europa.eu/eli/dir/2022/2555/oj/eng)). The definition is in Art. 6(30). Cloud computing service providers are listed in Annex I point 8.
- **Size cap.** NIS2 applies to Annex I types that are at least medium-sized enterprises (Art. 2(1)). A medium-sized enterprise has fewer than 250 staff and turnover up to EUR 50 million or a balance sheet up to EUR 43 million. A small enterprise has fewer than 50 staff and up to EUR 10 million ([Recommendation 2003/361/EC, Annex Art. 2](https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32003H0361)). Cloud providers are **not** among the size-independent types in Art. 2(2)(a). A Member State could still designate an entity under Art. 2(2)(b) to (e), for example as a sole provider or for systemic risk. **So Hikyo is probably outside NIS2 while it is small.** This depends on headcount and turnover I do not know, and on group-level aggregation rules I did not research.
- **It still reaches Hikyo indirectly.** In-scope customers must manage "supply chain security, including security-related aspects concerning the relationships between each entity and its direct suppliers or service providers" (Art. 21(2)(d)). Expect NIS2 customers to ask for incident-notice terms that match their own 24-hour early warning and 72-hour notification (Art. 23(4)).
- **If Hikyo grows into scope:** Art. 21 measures, Art. 23 reporting, jurisdiction of the main establishment for cloud providers (Art. 26(1)(b)), and the detailed requirements of [Implementing Regulation (EU) 2024/2690](https://eur-lex.europa.eu/eli/reg_impl/2024/2690/oj/eng), which explicitly covers cloud computing service providers.
- **The Netherlands.** The Cyberbeveiligingswet (Cbw) implements NIS2. It "is op 15 augustus 2026 in werking getreden" and replaces the Wbni ([NCSC](https://www.ncsc.nl/cyberbeveiligingswet-nis2); [Digitale Overheid](https://www.digitaleoverheid.nl/overzicht-van-alle-onderwerpen/cyberbeveiligingswet/)).

**What this implies for the product:** the design need not satisfy NIS2 directly yet. But a breach-notice SLA that customers can map onto their own NIS2 deadlines, plus the security documentation from section 3, will be asked for in sales.

## 9. EU Cyber Resilience Act (CRA), only where it plausibly applies

- **The hosted service itself is mostly outside the CRA.** Recital 12 says NIS2 covers SaaS, and that "cloud services designed and developed outside the responsibility of a manufacturer of a product with digital elements do not fall within the scope". The exception is "remote data processing", meaning processing "the absence of which would prevent the product with digital elements from performing one of its functions" ([CRA recital 12, Art. 3](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng)). **Interpretive point:** if Hikyo's CLI, Kubernetes operator or web client is a product placed on the market, the hosted backend they depend on could count as its remote data processing.
- **The open-source binaries may be in scope.** Free and open-source software is in scope only if "made available on the market ... in the course of a commercial activity". That includes "an intention to monetise, for instance by providing a software platform through which the manufacturer monetises other services" (recital 15). Software "not monetised by their manufacturers" is not commercial (recital 18). A maintainer who sells a hosted service built on the same software plausibly meets recital 15. That is my reading. No authority has confirmed it.
- **The "steward" alternative.** An open-source software steward is a legal person "other than a manufacturer" supporting FOSS intended for commercial activities, with a light regime (Art. 24). It probably does not fit a maintainer that also sells the product.
- **Dates.** Most obligations apply from 11 December 2027. **Art. 14 reporting has applied since 11 September 2026** (Art. 71(2)) to all in-scope products, including those already on the market (Art. 69(3)). It requires a 24-hour early warning and a 72-hour notification of actively exploited vulnerabilities, sent to the coordinating CSIRT and ENISA (Art. 14(1) and (2)).
- **Possible "important product" categories (Annex III, class I).** Three are candidates:
  - "Identity management systems and privileged access management software";
  - "Password managers": "products ... that store passwords, locally on a device or on a remote server, including ... password sharing and integration with ... third-party applications";
  - "Public key infrastructure and digital certificate issuance software", which includes "key management systems".

  The category depends on the product's **core functionality**. A product that can perform a category's functions but whose core functionality differs is not in it ([Implementing Regulation (EU) 2025/2392](https://eur-lex.europa.eu/eli/reg_impl/2025/2392/oj/eng), recitals 2, 4-5 and Annex points 1, 3, 9). Whether a secrets manager's core functionality is "password manager" is **undecided**. I found no authority on it.
- **If class I applies:** self-assessment is allowed only with harmonised standards (Art. 32(2)). Otherwise third-party assessment is required. However, FOSS in Annex III may use self-assessment "provided that the technical documentation ... is made available to the public" (Art. 32(5)).

**What this implies for the product:** a vulnerability intake and a 24-hour/72-hour reporting path for released binaries are relevant now if the binaries are in scope. Publishing the technical documentation keeps the self-assessment route open. The PKI module is the feature most likely to pull Hikyo into an Annex III category.

## 10. Pending changes that could alter these answers

- **Digital Omnibus proposal, [COM(2025) 837](https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:52025PC0837).** It would require notifying the authority only for breaches "likely to result in a high risk", extend the deadline "to 96 hours", and route notices through an ENISA single-entry point. It would also add to Art. 4 that information "is not to be considered personal data for a given entity where that entity does not have means reasonably likely to be used to identify" the person. That would codify SRB, and could change whether undecryptable Sealed ciphertext is personal data **for Hikyo**. The proposal's amending text replaces Art. 33(1) and adds paragraphs 1a, 6 and 7. It leaves Art. 33(2), the processor's duty to notify the controller, unchanged (proposal Article 3, point 8). **Adoption status is not verified.**
- **C-703/25 P.** The Court of Justice appeal against the DPF ruling. **Its outcome is not verified.**

## Unverified or not researched

| Item | Status |
|---|---|
| Whether the Court of Justice has decided C-703/25 P | Not verified. The OJ notice of the appeal was confirmed. A search result mentioned a June 2026 document in the case that I could not read. |
| Whether the Digital Omnibus has been adopted, and its final Art. 33 text | Not verified. The proposal text was read. A Council compromise of May 2026 appeared in search results only and was not read. |
| The AP's enforcement decisions or fines against processors | Not searched. |
| Any EDPB or AP guidance specifically on CDNs, or accepting crypto-shredding as erasure | Searched in the documents read. None found. |
| Hikyo's size against the NIS2 thresholds | Unknown. It needs the operator's headcount and finances. |
| Whether a secrets manager is a "password manager" under CRA Annex III | No authority found. Open. |
| Breyer and SRB are under predecessor or parallel regimes (Directive 95/46, Regulation 2018/1725) | Stated where cited. Their application to the GDPR is by analogy, though the definitions are materially the same. |
| A specific CDN vendor's DPF status, PoP locations or terms | Not researched. It depends on the vendor. |

## Sources

Legislation (read from the Publications Office Cellar copy of the same CELEX):

- GDPR, Regulation (EU) 2016/679: https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng
- NIS2, Directive (EU) 2022/2555: https://eur-lex.europa.eu/eli/dir/2022/2555/oj/eng
- Implementing Regulation (EU) 2024/2690: https://eur-lex.europa.eu/eli/reg_impl/2024/2690/oj/eng
- CRA, Regulation (EU) 2024/2847: https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng
- Implementing Regulation (EU) 2025/2392: https://eur-lex.europa.eu/eli/reg_impl/2025/2392/oj/eng
- SCCs for transfers, Implementing Decision (EU) 2021/914: https://eur-lex.europa.eu/eli/dec_impl/2021/914/oj/eng
- Art. 28(7) SCCs, Implementing Decision (EU) 2021/915: https://eur-lex.europa.eu/eli/dec_impl/2021/915/oj/eng
- DPF adequacy, Implementing Decision (EU) 2023/1795: https://eur-lex.europa.eu/eli/dec_impl/2023/1795/oj/eng
- SME definition, Recommendation 2003/361/EC: https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32003H0361
- Digital Omnibus proposal, COM(2025) 837: https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:52025PC0837

EDPB:

- Guidelines 07/2020 on controller and processor, v2.1: https://www.edpb.europa.eu/system/files/documents/2023-10/EDPB_guidelines_202007_controllerprocessor_final_en.pdf
- Guidelines 9/2022 on breach notification, v2.0: https://www.edpb.europa.eu/system/files/2023-04/edpb_guidelines_202209_personal_data_breach_notification_v2.0_en.pdf
- Opinion 22/2024 on processors and sub-processors: https://www.edpb.europa.eu/system/files/2024-10/edpb_opinion_202422_relianceonprocessors-sub-processors_en.pdf
- Guidelines 05/2021 on Art. 3 and Chapter V, v2.0: https://www.edpb.europa.eu/system/files/2023-02/edpb_guidelines_05-2021_interplay_between_the_application_of_art3-chapter_v_of_the_gdpr_v2_en_0.pdf
- Recommendations 01/2020 on supplementary measures, v2.0: https://www.edpb.europa.eu/system/files/2021-06/edpb_recommendations_202001vo.2.0_supplementarymeasurestransferstools_en.pdf
- CEF 2025 report on the right to erasure: https://www.edpb.europa.eu/system/files/2026-02/edpb_cef-report_2025_right-to-erasure_en.pdf

Autoriteit Persoonsgegevens:

- Reporting or not reporting a data breach: https://www.autoriteitpersoonsgegevens.nl/en/themes/security/data-breaches/reporting-or-not-reporting-a-data-breach
- Verwerkers: https://autoriteitpersoonsgegevens.nl/nl/onderwerpen/algemene-informatie-avg/verwerkers
- Meldplicht datalekken (links the processor mandate form): https://autoriteitpersoonsgegevens.nl/nl/onderwerpen/beveiliging/meldplicht-datalekken
- Praktijkgids gezondheidsgegevens in de cloud (March 2026): https://www.autoriteitpersoonsgegevens.nl/system/files?file=2026-03%2FPraktijkgids+gezondheidsgegevens+in+de+cloud.pdf

Courts:

- C-413/23 P EDPS v SRB, press release 107/25: https://curia.europa.eu/site/upload/docs/application/pdf/2025-09/cp250107en.pdf
- C-340/21 Natsionalna agentsia za prihodite, press release 191/23: https://curia.europa.eu/site/upload/docs/application/pdf/2023-12/cp230191en.pdf
- C-582/14 Breyer, press release 112/16: https://curia.europa.eu/site/upload/docs/application/pdf/2016-10/cp160112en.pdf
- C-311/18 Schrems II, press release 91/20: https://curia.europa.eu/site/upload/docs/application/pdf/2020-07/cp200091en.pdf
- T-553/23 Latombe v Commission, press release 106/25: https://curia.europa.eu/site/upload/docs/application/pdf/2025-09/cp250106en.pdf
- C-703/25 P appeal, OJ C/2025/6610: https://eur-lex.europa.eu/eli/C/2025/6610/oj/eng

Dutch government (NIS2 implementation):

- NCSC, Cyberbeveiligingswet: https://www.ncsc.nl/cyberbeveiligingswet-nis2
- Digitale Overheid, Cyberbeveiligingswet: https://www.digitaleoverheid.nl/overzicht-van-alle-onderwerpen/cyberbeveiligingswet/
