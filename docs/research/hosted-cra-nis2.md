# Research: Cyber Resilience Act and NIS2 duties for the hosted service and its open-source binaries

Date: 2026-11-17. Ticket: [Hikyo-Org/Hikyo#1339](https://github.com/Hikyo-Org/Hikyo/issues/1339).

This is a fact-finding note for the maintainer. It is not legal advice. It sets out what the law says, what the Commission's guidance says, and what each option would mean for Hikyo. It makes no decisions. Where I apply a rule to Hikyo, the text says it is my interpretation. A lawyer should confirm the points marked open before Hikyo relies on them.

**Sources.** Only primary sources are used: the Official Journal texts, the Commission's guidance, the Commission proposal, ENISA, the Dutch statute book and the Dutch regulators. EUR-Lex blocks automated clients, so I read each text from the Publications Office's Cellar copy of the same CELEX number. Citations link to the canonical EUR-Lex address. Quotes are from the English versions, or from the Dutch versions for Dutch law.

**Relation to earlier work.** This note builds on sections 8 and 9 of `docs/research/hosted-gdpr.md` on branch `research/hosted-gdpr`. It does not repeat them. Section 10 lists where the Commission's guidance changes what that note said.

**Hikyo facts used** (given by the maintainer, not re-derived): one MPL-2.0 Go binary that is both server and CLI, a web UI, a Kubernetes operator, a Helm chart, container images, a TypeScript client, an MCP server and install scripts. A desktop app is planned. Version 1.0.0 was published on 2026-11-01. The maintainer is a natural person in the Netherlands. There is no company or foundation yet. A paid hosted service run from the Netherlands is planned on the same code. It has not launched.

---

## Summary

1. **Today, on the guidance's reading, nothing Hikyo publishes is "placed on the market".** The Commission says that when a natural person supplies a free version, "the version provided for free ... is not within the scope of the CRA" (guidance para 53). Regular releases alone do not make it commercial (CRA recital 18). Interpretation: the 1.0.0 release did not by itself trigger CRA obligations.
2. **The paid hosted service is the likely trigger.** The service itself is not a CRA product, because software "that executes remotely and is merely accessed by the user is not, on that basis alone, a product with digital elements" (guidance para 21). But free software "through which it monetises other ... services" is placed on the market (guidance para 54, Example 15). Interpretation: the CLI, TypeScript client, MCP server, operator or desktop app become CRA products if they are how customers use the paid service.
3. **Once a client is a product, the hosted backend becomes its remote data processing solution.** An "authentication portal that issues credentials or tokens required for the product ... to operate would be considered RDPS" (guidance para 194). Who operates it is "not a decisive factor" (para 196). The backend then carries the CRA's security requirements and its reporting duty.
4. **Art. 14 reporting applies from the first day a product is placed on the market.** It has applied since 11 September 2026 (CRA Art. 71(2)). Reports go to the NCSC through ENISA's single reporting platform. The deadlines are 24 hours, 72 hours, and 14 days or one month. The clock runs on awareness of active exploitation or a severe incident, not on receiving a vulnerability report.
5. **The full rules apply from 11 December 2027, version by version.** Versions placed before that date are exempt unless substantially modified (CRA Art. 69(2)). Interpretation: a feature-bearing minor release after that date may be a substantial modification, so the exemption wears off quickly at Hikyo's release cadence.
6. **The current support policy can mostly survive.** "Latest minor only" is allowed under CRA Art. 13(10) if upgrades are free and impose no "additional costs" (guidance paras 130 and 131). The gaps are listed in section 5: a declared end date, ten-year availability of updates, likely a security-only patch path within the latest minor, the single point of contact, the reporting runbook, and the conformity paperwork.
7. **Classification is open.** Hikyo could fall under "identity management systems", "password managers" or "PKI" in CRA Annex III class I. Only one core functionality counts (guidance para 144). If any class applies, the FOSS route in CRA Art. 32(5) still allows self-assessment, but only if the technical documentation is public and the placed product is itself FOSS. I found no OJ citation of a harmonised standard; this is not verified against the primary list.
8. **Penalties scale with size, but there is no SME exemption from the duties.** The top tier is EUR 15 million or 2.5% of turnover (CRA Art. 64(2)). Micro and small enterprises are spared fines only for missing the 24-hour early warning (Art. 64(10)(a), as corrected in 2025). Stewards cannot be fined at all (Art. 64(10)(b)).
9. **NIS2 probably does not apply while Hikyo is small, but the size test looks through to the owner.** The hosted service is a cloud computing service. NIS2 applies only from medium size. Enterprises linked "through a natural person" count together if they work in the same or adjacent markets (Recommendation 2003/361/EC, Annex Art. 3(3)). The Dutch Cyberbeveiligingswet has applied since 15 August 2026 and follows the EU size rule.
10. **NIS2 customers will push their supplier duties onto Hikyo by contract.** Implementing Regulation 2024/2690 lists the clauses: incident notice, a right to audit, vulnerability handling and exit terms. Financial-sector customers under DORA must have similar written terms (DORA Art. 30). These do not bind Hikyo directly. They shape what sales will be asked to sign.

---

## 1. Who is the manufacturer, and when is anything "placed on the market"?

### The legal tests

| Point | What the source says |
| --- | --- |
| Manufacturer | A person "who develops or manufactures products with digital elements ... and markets them under its name or trademark, whether for payment, monetisation or free of charge" ([CRA Art. 3(13)](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng)). A natural person can be one. |
| Placing on the market | "the first making available" in the Union (Art. 3(21)). Making available means supply "in the course of a commercial activity, whether in return for payment or free of charge" (Art. 3(22)). |
| Commercial activity | It can include "an intention to monetise, for instance by providing a software platform through which the manufacturer monetises other services" (recital 15). FOSS "not monetised by their manufacturers should not be considered to be a commercial activity". "The mere presence of regular releases should not in itself lead to the conclusion" that it is commercial (recital 18). |
| Status of the guidance | Commission guidance C(2026) 5252, published in the OJ today as [C/2026/6013](https://eur-lex.europa.eu/eli/C/2026/6013/oj/eng). It is not binding. Only the Court of Justice gives an authoritative reading (para 8). |
| Free and paid versions | Community and paid versions "are different products". The paid one is placed on the market. The free one "is not considered to be placed on the market", including in open-core models (para 52). |
| Natural person | "If the entity is a natural person, the version provided for free (the community version) is not within the scope of the CRA." A legal person supplying the free version is a steward for it (para 53). |
| Monetising other services | Publishing FOSS can be placing it on the market "where it provides the software through which it monetises other products with digital elements or services" (para 54). Example 15: a free VPN app that "enables users to pay to access additional servers or dedicated IP addresses" is "considered placed on the market". |
| Cost recovery | A natural person charging only "actual costs", "including the person's reasonable living expenses", for bundled technical help is not, on that basis alone, placing on the market (para 58). Paid support or consultancy alone does not place the FOSS on the market (paras 55 to 57, Example 18). |
| Donations | Donations without a profit motive are not commercial (recital 15). Giving binaries or security fixes only to donors is (paras 60 to 62). |
| Not-for-profit publisher | A not-for-profit whose earnings after costs go to its objectives does not place the FOSS on the market (para 66, recital 18). |
| Responsibility | FOSS is under the responsibility of whoever exercises "primary control" over development, releases and distribution (para 49). |
| When a version is placed | Standalone software is placed when "its manufacturing phase is complete and that software is first supplied ... in the course of a commercial activity" (paras 13 and 14). Later releases that are not substantial modifications keep the first placing date (para 15, Examples 1 and 2). |
| Steward | "a legal person, other than a manufacturer", that supports FOSS "intended for commercial activities" on a sustained basis ([CRA Art. 3(14)](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng)). Recital 19 names "foundations" and entities publishing FOSS "in a business context, including not-for-profit entities". |
| Steward and manufacturer at once | The same legal person can be steward for the community version and manufacturer for the monetised one (guidance paras 72 to 74). A steward that starts monetising becomes the manufacturer from that date, "but not in respect of earlier versions for which it acted as a steward" (para 84). |

### Applying the tests to the structures on the table

All of this is interpretation. The guidance gives examples, not a ruling on Hikyo.

| Scenario | Who is the manufacturer of what | Main consequence |
| --- | --- | --- |
| **A. Today.** Marc as a natural person publishes free binaries. No paid offer. | Nobody. The free version of a natural person is out of scope (para 53). | No CRA duties. Art. 14 does not apply because nothing is placed on the market. |
| **B. Marc as a natural person (or sole trader) runs the paid service.** | Marc becomes manufacturer of any client through which the service is monetised (para 54, Example 15). The hosted backend is that client's RDPS (section 2). The self-hosted server binary is arguably a separate free product (para 52). | Art. 14 applies from launch. Annex I applies to versions placed after 11 December 2027. Personal liability and a published contact address for a natural person (section 5, item 11). |
| **C. A company runs the paid service. Marc keeps publishing the code personally.** | The company is likely the manufacturer of whatever client it offers its customers under its name. Marc's free versions remain out of scope as a natural person (para 53). | Open: if the company points customers to Marc's own releases, it is unclear who "markets [them] under its name or trademark". The trademarks are Marc's personally today. Other businesses Marc controls in the same or an adjacent market may be counted with the company for size (section 8). |
| **D. One company publishes the code and runs the service.** | The company is manufacturer for the monetised clients and steward for the free community version (paras 52, 53 and 72 to 74). | Two regimes in one entity. Steward duties are light (section 7). |
| **E. A not-for-profit foundation holds the project. A separate company runs the service.** | The foundation does not place the FOSS on the market (para 66) and can be its steward (recital 19, Art. 24). The company is manufacturer of what it monetises. | Separates the community project from commercial liability. Does not remove the company's duties for its monetised clients and RDPS. |

**Open question: which binaries are "the software through which it monetises".** The CLI, TypeScript client, MCP server and operator can each talk to a self-hosted server or to the paid service. Example 15's VPN app is free but unlocks paid servers. Interpretation: a client that is the normal way to use the paid service fits that example. A client that is identical for self-hosted users may still be "the" monetising software, because Example 15 does not require the app to be useless without payment. The server binary is harder. When run self-hosted it monetises nothing. But if the hosted service runs the same build, that build is part of the RDPS (section 2). Distinct builds count as distinct products (para 14), so a separate hosted build would make this easier to reason about. I found no authority closer than Example 15.

**What this implies for the product:**

- The legal trigger is the commercial launch, not the 1.0.0 release. Any launch plan needs a CRA workstream that starts before the first paid customer.
- The entity decision changes who carries the duties and whether a steward regime is available. Only a legal person can be a steward.
- Keeping the free distribution and the paid offering clearly separate (separate builds, names or channels) makes the "which product is placed on the market" question answerable. This is a design choice, not a legal requirement.

---

## 2. The hosted service and remote data processing (RDPS)

| Point | What the source says |
| --- | --- |
| Definition | Remote data processing is "data processing at a distance for which the software is designed and developed by the manufacturer, or under the responsibility of the manufacturer, and the absence of which would prevent the product with digital elements from performing one of its functions" ([CRA Art. 3(2)](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng)). |
| A product includes its RDPS | A product with digital elements is "a software or hardware product and its remote data processing solutions" (Art. 3(1)). |
| SaaS alone | Cloud services "designed and developed outside the responsibility of a manufacturer" are out of scope. NIS2 covers SaaS (recital 12). Web apps "accessed exclusively through a web browser" are not products on that basis alone (guidance para 21). |
| Scope of the duty | RDPS requirements do not cover "the security of a manufacturer's network and information systems as a whole" (recital 11). |
| Which functions count | Not only core functions (guidance para 189). Examples include synchronising, onboarding, configuration, automated distribution of updates and "identity and access management" (para 190). A function that can also be done manually still counts (para 191). Telemetry is not RDPS (para 192). |
| Tokens | "an authentication portal that issues credentials or tokens required for the product with digital elements to operate would be considered RDPS" (para 194). |
| Operator | "the notion of 'who operates the solution' is not a decisive factor". A manufacturer remains responsible if a third party operates the solution (para 196). |
| Hosting layer | An app built on IaaS or PaaS can be RDPS. Third-party SaaS used by the manufacturer is not RDPS and is treated as a component (paras 197 to 202). |
| Boundary | Only the interface the product directly interacts with is RDPS, not the back office behind it (section 8.3.1, mobile banking example). CI/CD and update distribution to edge locations are not RDPS (para 182). |
| Reporting | RDPS is covered by Art. 14 reporting (para 178). |

**Interpretation for Hikyo.** If a client is placed on the market (section 1, scenarios B to E), the hosted Hikyo API that stores and serves secrets, issues tokens and certificates, and holds configuration is the client's RDPS. The client cannot do its job without it. The hosted web UI on its own is not a product (para 21). It would only matter as part of the RDPS if it supports a function of a placed client. Under the banking example, the RDPS boundary is the API and services that clients talk to directly. Hikyo's internal billing, admin tooling and provider infrastructure sit outside it, and NIS2 is the regime for them if it applies (section 8).

**What this implies for the product:**

- The CRA's Annex I Part I properties (secure defaults, access control, confidentiality, integrity, logging, resilience and so on) would apply to the hosted API as part of the client product, from the dates in section 4.
- A severe incident on the hosted service that affects a placed client's security would be an Art. 14(3) report (section 3), in addition to any GDPR or contractual notice.
- The breach-notification SLA ticket owns the customer-facing design. This note only fixes the CRA trigger and deadlines.

---

## 3. Art. 14 reporting, in force since 11 September 2026

Art. 14 applies from 11 September 2026 (CRA Art. 71(2)). It covers every in-scope product, including those placed before 11 December 2027 (Art. 69(3)). It keeps applying after support ends (guidance para 210). It only bites once something is placed on the market. On the reading in section 1, that is the commercial launch, not today.

### What must be reported, when and to whom

| Event | Early warning | Notification | Final report | Source |
| --- | --- | --- | --- | --- |
| Actively exploited vulnerability | "within 24 hours of the manufacturer becoming aware of it" | within 72 hours, with "general information ... about the product ... the general nature of the exploit" and mitigations | "no later than 14 days after a corrective or mitigating measure is available" | [CRA Art. 14(1) and (2)](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng) |
| Severe incident having an impact on the security of the product | within 24 hours, "including at least whether the incident is suspected of being caused by unlawful or malicious acts" | within 72 hours, with an initial assessment | "within one month after the submission of the incident notification" | Art. 14(3) and (4) |

- **Actively exploited** means "reliable evidence that a malicious actor has exploited it in a system without permission of the system owner" (Art. 3(42)).
- **Severe** means the incident affects or can affect the product's ability "to protect the availability, authenticity, integrity or confidentiality of sensitive or important data or functions", or can lead to "the introduction or execution of malicious code in a product ... or in the network and information systems of a user" (Art. 14(5)). Recital 68 gives malicious code in the release channel as an example.
- **Not reportable:** vulnerabilities found and reported in good faith that are not exploited (recital 68). Exploitation the manufacturer knew of before 11 September 2026 (guidance para 217). A third-party component's vulnerability unless it is exploited in your product (para 218).
- **Awareness** starts when, after an immediate initial assessment, the manufacturer "has a reasonable degree of certainty" (guidance para 213).
- **Users.** The manufacturer must inform "the impacted users ... and where appropriate all users", with mitigations, "where appropriate in a structured, machine-readable format" (Art. 14(8)). The guidance treats this as risk-based. It need not be public (paras 219 to 221). If the manufacturer does not inform users in time, the CSIRT may do it (Art. 14(8)).
- **Intermediate report** on request of the CSIRT (Art. 14(6)).
- **Where.** Reports go through ENISA's single reporting platform to the endpoint of the coordinating CSIRT of the main establishment. That is the Member State "where the decisions related to the cybersecurity of its products ... are predominantly taken" (Art. 14(7)).

### The Dutch channel

- ENISA "has deployed the initial operating capability of the Single Reporting Platform (SRP)" on 11 September 2026. The portal is at `portal.cra-srp.enisa.europa.eu` ([ENISA](https://www.enisa.europa.eu/news/the-cra-single-reporting-platform-is-launched)). The Commission calls it "operational as of 11 September 2026" ([Commission](https://digital-strategy.ec.europa.eu/en/policies/cra-reporting)).
- The NCSC says manufacturers "doen die melding bij het Nationaal Cyber Security Centrum (NCSC)". "Je registreert je op MijnNCSC en doet je melding via het portaal, of je meldt via het meldformulier op ncsc.nl." "Alle termijnen starten op het moment dat jij kennis krijgt" ([NCSC, 11 September 2026](https://www.ncsc.nl/nieuws/meldplicht-cyber-resilience-act-gaat-vandaag-in)).
- Under the Cyberbeveiligingsbesluit, the Minister is the CSIRT and "de coördinator met het oog op een gecoördineerde bekendmaking van kwetsbaarheden" ([Cbb Art. 2(1) and (3)](https://wetten.overheid.nl/BWBR0052875)). Interpretation: in practice this is the NCSC, under the Minister of Justice and Security.
- The RDI is the Dutch CRA market surveillance authority. It also points reporters to the NCSC ([RDI](https://www.rdi.nl/onderwerpen/draadloze-apparatuur/handel-en-apparatuur/cra)).

### How this sits next to SECURITY.md

- SECURITY.md promises acknowledgement "within 7 days", fix targets of 14 and 30 days from confirmation, and a 90-day embargo. Those are coordinated-disclosure terms with a reporter. They do not conflict with Art. 14, because a private report of an unexploited bug is not reportable (recital 68).
- The conflict point is narrower. SECURITY.md says "Active exploitation accelerates disclosure and remediation". Once a product is placed on the market, active exploitation also starts a 24-hour clock to the NCSC and ENISA. Nothing in SECURITY.md covers that today. Interpretation: the reporting duty needs a runbook, an MijnNCSC registration and someone reachable within 24 hours, including when the solo maintainer is away.
- The hosted service adds incidents as triggers. A compromise of the hosted API that affects the security of a placed client is a "severe incident" candidate under Art. 14(5).

**What this implies for the product:**

- Before the commercial launch: register on MijnNCSC, write the Art. 14 runbook, and decide who can file within 24 hours. The breach-notification SLA ticket owns the customer-facing side. The machine-readable user notice in Art. 14(8) can reuse the GHSA advisory and any in-product notice channel.
- Micro and small enterprises are spared fines for a late 24-hour early warning, but not for the 72-hour notification or the final report (section 7).

---

## 4. Dates and the placing-date mechanics

| Date | What applies | Source |
| --- | --- | --- |
| 11 June 2026 | Rules on notified bodies (Chapter IV) | [CRA Art. 71(2)](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng) |
| 11 September 2026 | Art. 14 reporting, for all in-scope products including those already on the market | Art. 71(2), Art. 69(3) |
| 11 December 2027 | Everything else: Annex I, Arts. 13, 28 to 32, CE marking, steward duties | Art. 71(2); steward date confirmed by [ENISA](https://www.enisa.europa.eu/news/the-cra-single-reporting-platform-is-launched) |

**The exemption for earlier versions.** "Products with digital elements that have been placed on the market before 11 December 2027 shall be subject to the requirements set out in this Regulation only if, from that date, those products are subject to a substantial modification" (Art. 69(2)). The guidance adds that for such products "the manufacturer is not required to comply with the vulnerability handling obligations set out in Part II of Annex I" (para 210).

**What counts as a substantial modification.** A software update that only reduces risk is not one. A feature update that changes the intended purpose or broadens the attack surface may be (recital 39, Art. 3(30)). Non-substantial releases keep the first placing date (guidance para 15). Each substantially modified version needs its own declared support period (para 128). A substantial modification does not automatically reset the support period of earlier versions (paras 132 to 135).

**Interpretation for Hikyo.**

- If the paid service launches before 11 December 2027, the client version placed at launch carries only the Art. 14 duty until a substantial modification.
- Hikyo ships milestone-driven, feature-bearing minors. The first such minor after 11 December 2027 is likely a substantial modification. From then on, each new minor is a newly placed product that must meet Annex I in full, with its own support period.
- So the pre-2027 exemption buys months, not years, at the current cadence.
- If the steward route is used first and monetisation comes later, the steward becomes the manufacturer only from the date of monetisation, "but not in respect of earlier versions for which it acted as a steward" (para 84).

**What this implies for the product:** plan conformity work (section 5) for the first minor released after 11 December 2027, not for some later date. A launch date before or after that day changes little beyond Art. 14.

---

## 5. Obligations from 11 December 2027 and the gap list

These apply only to versions placed on the market (section 1) after 11 December 2027, or substantially modified after it (section 4). They do not apply to Marc's free versions as a natural person.

### The support-period question

- The support period must reflect expected use time and "shall be at least five years", unless the product is expected to be used for less ([CRA Art. 13(8)](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng)). Five years is "not to be considered as the default" (guidance para 126). Recital 60 points to longer periods for some software.
- The end date, "including at least the month and the year", must be stated "at the time of purchase" (Art. 13(19)).
- **The latest-version allowance.** "Where a manufacturer has placed subsequent substantially modified versions of a software product on the market, that manufacturer may ensure compliance with the essential cybersecurity requirement set out in Part II, point (2), of Annex I only for the version that it has last placed on the market, provided that the users of the versions that were previously placed on the market have access to the version last placed on the market free of charge and do not incur additional costs to adjust the hardware and software environment" (Art. 13(10)).
- "Additional costs" do not include "personnel time, routine testing, configuration adjustments or upgrades of underlying software dependencies". They do include "mandatory purchases of new hardware, infrastructure replacement or fundamental changes to the operating environment" (guidance para 130).
- For "continuously evolving" software, a manufacturer "may rely on Article 13(10) to discontinue addressing and remediating vulnerabilities for earlier versions once users are able to upgrade" free of charge (guidance para 131, Example 53). The other Part II duties and Art. 14 remain. Users who do not upgrade should be told where technically feasible (footnote 18). Paid support for older versions is allowed (footnote 19).

Interpretation: SUPPORT.md's "exactly one version" model can stand, provided each minor is a free upgrade with no "additional costs". Roll-forward migrations and a single binary fit para 130. A minor that forces a database engine change or new infrastructure would not. Each minor would still need a declared support period of at least five years on paper, even if Art. 13(10) lets fixes go only to the newest minor.

### Gap list against SECURITY.md and SUPPORT.md

| # | Requirement | Source | Current state | Gap (interpretation) |
| --- | --- | --- | --- | --- |
| 1 | Declared support period of at least five years, with month and year, per substantially modified version | Art. 13(8) and (19); guidance para 128 | SUPPORT.md supports "exactly one version". No end date is stated. | **Gap.** State an end date per minor. Rely on Art. 13(10) for "latest only" fixes. |
| 2 | Security updates "separately from functionality updates", "where technically feasible" | Annex I Part II(2); recital 57 | SUPPORT.md: "An urgent security fix may therefore require a feature-bearing minor upgrade." | **Likely gap.** Recital 57 says users should not have to install new features to get security fixes. Counter-reading: if each feature minor is a substantially modified version (section 4), Art. 13(10) limits Part II(2) to the version last placed, so shipping a fix only in a new free minor may be lawful. Neither the Act nor the guidance addresses a feature minor released to carry a fix. Either change the quoted sentence or document why separation is not feasible. |
| 3 | Each security update stays available "for a minimum of 10 years or for the remainder of the support period, whichever is longer" | Art. 13(9) | Releases are published. No retention commitment. | **Gap.** A retention commitment for release artefacts. Art. 13(11) also asks for risk warnings on public archives of old versions. |
| 4 | Single point of contact that lets "users ... choose their preferred means of communication" and does not limit them "to automated tools" | Art. 13(17); recital 63 | GitHub Private Vulnerability Reporting, with email described as "fallback-only". | **Possible gap.** Two human-read channels likely meet the "not only automated" rule. Calling email "fallback-only" sits awkwardly with "choose their preferred means". This contact must also be in the Annex II user information. |
| 5 | Coordinated vulnerability disclosure policy and a contact address | Annex I Part II(5) and (6); Art. 13(8) | SECURITY.md has both. | Largely met. |
| 6 | Publicly disclose fixed vulnerabilities, with description, affected product, impact, severity and remediation help. Delay allowed "until after users have been given the possibility to apply the relevant patch" | Annex I Part II(4) | GHSA plus CVE, published after the patch. | Largely met. Check that each advisory carries severity and remediation text. |
| 7 | SBOM "covering at the very least the top-level dependencies" | Annex I Part II(1); Annex VII | Releases ship SBOMs. | Met in substance. Annex II(9) asks where the SBOM can be found, if it is made available. |
| 8 | Secure distribution and free security updates with advisory messages | Annex I Part II(7) and (8) | Signed releases. Software is free. | Met in substance. Supply-chain ticket owns the details. |
| 9 | Regular security testing and review | Annex I Part II(3) | Not assessed here. | Not assessed. |
| 10 | Art. 14 reporting runbook and user notice | Art. 14 | Not in SECURITY.md. | **Gap** once placed on the market (section 3). |
| 11 | Name or trademark, "the postal address, email address or other digital contact details" on the product or its documentation | Art. 13(16) | Not stated. | **Gap.** Interpretation: a postal address is expected. For a natural person this means publishing a home or business address. |
| 12 | Report vulnerabilities found in components to their maintainers | Art. 13(6) | Not stated. | **Gap** in policy text. |
| 13 | End-of-support notice to users "where technically feasible" | Art. 13(19); guidance para 131 fn. 18 | Release notes state the previous minor's EOL. | Partly met. An in-product notice may be expected where feasible. |
| 14 | Cybersecurity risk assessment, documented, covering Annex I Part I | Art. 13(2) to (4) | Not assessed here. | **Gap** in paperwork. |
| 15 | Technical documentation (Annex VII), conformity assessment (Art. 32), EU declaration of conformity (Annex V) or simplified form (Annex VI), CE marking | Arts. 13(12), 28, 30, 31, 32 | None. | **Gap.** For software, CE marking goes "either to the EU declaration of conformity ... or on the website accompanying the software product" (Art. 30(1)). |
| 16 | User information and instructions (Annex II), kept online for 10 years or the support period | Art. 13(18); Annex II | Docs site exists. Not mapped to Annex II. | **Gap.** Map the docs to Annex II items, including the support end date (Annex II(7)). |

**What this implies for the product:**

- Most of the vulnerability-handling posture already matches. The work is a declared support period per minor, likely a security-only patch path within the latest minor, a ten-year artefact retention promise, the Art. 14 runbook, and the conformity documents.
- Documents must be kept for ten years after placing or for the support period, whichever is longer (Art. 13(13)). This outlives any single version.

---

## 6. Classification and conformity assessment

### The categories that could fit

Annex III class I includes "Identity management systems and privileged access management software and hardware", "Password managers", "Network management systems" and "Public key infrastructure and digital certificate issuance software" ([CRA Annex III](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng)). [Implementing Regulation (EU) 2025/2392](https://eur-lex.europa.eu/eli/reg_impl/2025/2392/oj/eng) gives the technical descriptions:

| Category | Description (Annex I of IR 2025/2392) | Fit with Hikyo (interpretation) |
| --- | --- | --- |
| Identity management (point 1) | Products "that provide mechanisms for authentication or authorisation and that may also provide mechanisms for the lifecycle management of identity credentials of natural persons, legal persons, devices or systems". Includes "access management systems that control access ... to digital resources". | Plausible. Service accounts, workload identity federation and access control over secrets are authorisation mechanisms for systems. OIDC, SAML and SCIM are integrations, not the product's purpose. |
| Password managers (point 3) | Products "that store passwords, locally on a device or on a remote server, including activities such as generation of passwords as well as password sharing and integration with local or third-party applications". Includes "enterprise password managers". | Plausible. A secrets store that hands credentials to applications resembles an enterprise password manager. |
| Network management (point 6) | Products that "manage connected network elements ... by monitoring them and controlling their network operations and configuration", including "controllers for software-defined networking". | Weak. Hikyo holds configuration but does not manage network elements. |
| PKI (point 9) | Products "used as part of a public key infrastructure (PKI) that manage the validation, creation, issuance, distribution, status publication, renewal or revocation of digital certificates", including "key management systems". | Plausible for the PKI feature. Docs/adr/pki.md keeps it narrow: no ACME, OCSP, public trust or HSM. |
| Container runtime (class II point 1) | Container runtime systems. | No. The Kubernetes operator is not a runtime. |

### The core-functionality test

- A product is important when it "has the core functionality of a product category". Integrating such a function "shall not in itself render the product in which it is integrated subject to" the stricter assessment (CRA Art. 7(1)).
- A product that can perform a category's functions "but whose core functionality itself is different" does not meet the description. The examples are a SOAR tool that is not a SIEM and a smartphone with a password manager (IR 2025/2392 recital 5). Extra functions do not remove a core functionality (recital 4).
- Core functionality means "main features and technical capabilities, without which it would not be able to meet its intended purpose", read in light of documentation and marketing (guidance para 139). "A product with digital elements may not have more than one core functionality" (para 144). Separately sold modules are classified separately (para 145).

Interpretation: Hikyo markets itself as a secrets manager. That is not a named category. The closest fits are "password managers" and "identity management". The PKI is a feature within the product, not its core. I found no authority that places a secrets manager in any category. How Hikyo describes its purpose in documentation and marketing is part of the test (para 139). If the PKI or identity features were ever sold or shipped as separate products, each would be classified on its own (para 145).

### Conformity assessment routes

| Classification | Routes | Source |
| --- | --- | --- |
| Default (not in Annex III) | Module A self-assessment (internal control). Always available for the default category (guidance para 137). | [CRA Art. 32(1)](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng) |
| Class I | Module A only if a harmonised standard, common specification or certification scheme at "substantial" level is applied in full. Otherwise Module B+C or H, which need a notified body. | Art. 32(2); guidance para 149 |
| Class II | B+C, H or a certification scheme. | Art. 32(3) |
| FOSS in Annex III | "shall be able to demonstrate conformity ... by using one of the procedures referred to in paragraph 1 ... provided that the technical documentation referred to in Article 31 is made available to the public at the time of the placing on the market". | Art. 32(5) |

- **Harmonised standards.** I found no CRA harmonised standard cited in the Official Journal under Art. 27. I could not verify this against a primary list (see the Unverified table). Without one, a non-FOSS class I product needs a notified body.
- **The FOSS route depends on the placed product being FOSS.** Art. 32(5) only helps for a product "qualifying as free and open-source software". Interpretation: an MPL-2.0 client qualifies. A proprietary desktop app, or a closed module added to the paid offering, would not, and would lose the Module A route if it falls in Annex III.

**What this implies for the product:**

- If the owner treats Hikyo as default category, Module A applies. That stance should be written down with reasons, because misclassification is a compliance risk (guidance para 143).
- If the owner treats it as class I, publishing the technical documentation at placing keeps self-assessment open, as long as the placed products stay FOSS.
- The licence of the planned desktop app and of any paid-only module is therefore a CRA decision as well as a product one.

---

## 7. Penalties, SME support and the steward regime

### Fines

| Infringement | Maximum | Source |
| --- | --- | --- |
| Annex I requirements and Arts. 13 and 14 | EUR 15 million or 2.5% of worldwide annual turnover, whichever is higher | [CRA Art. 64(2)](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng) |
| Other obligations | EUR 10 million or 2% | Art. 64(3) |
| Incorrect, incomplete or misleading information to authorities | EUR 5 million or 1% | Art. 64(4) |

- Size counts when setting the amount: "the size, in particular with regard to microenterprises and small and medium sized-enterprises, including start-ups" (Art. 64(5)). Recital 121 asks authorities to consider income where the offender is not an undertaking.
- **Relief for micro and small enterprises.** No fines for "any failure to meet the deadline referred to in Article 14(2), point (a), or Article 14(4), point (a)" (Art. 64(10)(a)). Those are the two 24-hour early warnings. The original text referred to "paragraphs 3 to 9". A [2025 corrigendum](http://data.europa.eu/eli/reg/2024/2847/corrigendum/2025-07-02/oj) changed it to "paragraphs 2 to 9", so the relief now covers the top-tier fines in para 2. The 72-hour notification and final reports are not covered.
- **Stewards.** No fines for "any infringement of this Regulation by open-source software stewards" (Art. 64(10)(b)).
- Member States set the penalty rules (Art. 64(1)). The Dutch implementing act, the Uitvoeringswet verordening cyberweerbaarheid (36.875), passed the Tweede Kamer on 10 September 2026. It is still before the Eerste Kamer, which issued its committee report on 3 November 2026 ([Eerste Kamer](https://www.eerstekamer.nl/wetsvoorstel/36875_uitvoeringswet_verordening)). I did not read its penalty clauses.

### SME support

- Member States must offer awareness measures, a dedicated communication channel and testing support for micro and small enterprises (CRA Art. 33(1)). Regulatory sandboxes are optional (Art. 33(2)). The Commission provides guidance (Art. 33(3)).
- Micro and small enterprises may use a simplified technical documentation form, to be set by implementing act (Art. 33(5)). Notified bodies must take SME interests into account in their fees (Art. 32(6)).
- SME status uses the whole of Recommendation 2003/361/EC, including partner and linked enterprises (CRA recital 5).

### The steward regime

- Only a legal person can be a steward (Art. 3(14)).
- Duties: a documented and verifiable cybersecurity policy that fosters secure development and vulnerability handling (Art. 24(1)); cooperation with market surveillance authorities on request (Art. 24(2)); Art. 14(1) reporting "to the extent that they are involved in the development", and Art. 14(3) and (8) for severe incidents affecting systems the steward provides for development (Art. 24(3)).
- No CE marking (recital 19). No fines (Art. 64(10)(b)). Steward duties apply from 11 December 2027 (Art. 71(2)).
- Reporting depends on the kind of support the steward gives: non-technical, infrastructure or engineering (guidance paras 79 to 82).

**What this implies for the product:** the fine ceilings are the same for a natural person as for a company. Size only affects the amount and the early-warning relief. A steward entity is the only structure with no fine exposure for the free distribution. It does not shield whoever monetises the clients.

---

## 8. NIS2 scope, aggregation and the Dutch Cyberbeveiligingswet

### What kind of entity the hosted service would be

- A "cloud computing service" is "a digital service that enables on-demand administration and broad remote access to a scalable and elastic pool of shareable computing resources" ([NIS2 Art. 6(30)](https://eur-lex.europa.eu/eli/dir/2022/2555/oj/eng)). SaaS is one of its service models (recital 33). Cloud computing service providers are in Annex I point 8.
- A "managed service provider" provides services "related to the installation, management, operation or maintenance of ICT products ... via assistance or active administration" (Art. 6(39)). Interpretation: a self-service SaaS is a cloud service, not managed services. Hands-on operation of customers' own Hikyo installs could move it towards MSP.

### The size test, with the parts the earlier note left open

- NIS2 covers Annex I and II types "which qualify as medium-sized enterprises ... or exceed the ceilings" (NIS2 Art. 2(1)). Medium: fewer than 250 staff and turnover up to EUR 50 million or balance sheet up to EUR 43 million ([Recommendation 2003/361/EC, Annex Art. 2](https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32003H0361)). Below that, a cloud provider is out of scope unless designated (NIS2 Art. 2(2)(b) to (e)).
- **Linked enterprises count in full; partners pro rata** (Recommendation Annex Art. 6(2)). Partner means a holding of 25% or more (Art. 3(2)). Linked means control (Art. 3(3)).
- **The natural-person clause.** "Enterprises which have one or other of such relationships through a natural person or group of natural persons acting jointly are also considered linked enterprises if they engage in their activity or in part of their activity in the same relevant market or in adjacent markets." An adjacent market is "directly upstream or downstream of the relevant market" (Annex Art. 3(3)). Interpretation: if Marc controls other businesses in the same or an adjacent IT market, their staff and turnover may be added to the Hikyo entity's.
- **Owner-managers count** as staff (Annex Art. 5(c)). A new enterprise without approved accounts uses "a bona fide estimate made in the course of the financial year" (Art. 4(3)). Status changes only after two consecutive years over or under a ceiling (Art. 4(2)).
- **Independence.** Member States "are able to take into account the degree of independence an entity enjoys in relation to its partner or linked enterprises", in particular separate network and information systems (NIS2 recital 16). I found no Dutch provision using this option in the Cbw or the Cbb.

### The Dutch statute

| Point | What the source says |
| --- | --- |
| In force | "De Cyberbeveiligingswet en dit besluit treden in werking met ingang van 15 augustus 2026" ([Cbb Art. 35, Stb. 2026, 189](https://zoek.officielebekendmakingen.nl/stb-2026-189.html)). |
| Size rule | Annex 1 entities above the medium ceilings are essential (Cbw Art. 8(1)(f)). Those that "in aanmerking komen als middelgrote onderneming" are important (Art. 12(1)(a)). Small entities are not listed, apart from size-independent types ([Cbw](https://wetten.overheid.nl/BWBR0052872)). Interpretation: this follows NIS2. |
| Annex 1 | Lists "Aanbieders van cloudcomputingdiensten" under digital infrastructure and "Aanbieders van beheerde diensten" under B2B ICT management (Cbw bijlage 1). |
| Designation | The minister can designate an Annex 1 or 2 entity as essential on criteria such as being "in Nederland de enige aanbieder" of an essential service (Cbw Art. 9). |
| Jurisdiction | Cloud and managed service providers fall under the Cbw if they have "hun hoofdvestiging of vertegenwoordiger in Nederland". The main establishment is where cybersecurity decisions are "hoofdzakelijk" taken (Cbw Art. 4(3) and (5); NIS2 Art. 26(1)(b)). |
| Registration | In-scope entities give their name, address, contact details "met inbegrip van e-mailadressen, IP-bereiken en telefoonnummers", sectors and Member States served (Cbw Art. 44(1)). Changes go in "onverwijld of in elk geval binnen twee weken" (Art. 44(2)). This mirrors the two-week rule in NIS2 Art. 3(4). The Cbb adds items such as the Handelsregister number and domain names (Cbb Art. 27). |
| Which rulebook | For cloud providers and the other entity types in IR 2024/2690, Cbb Arts. 6 to 18 do not apply (Cbb Art. 4). The explanatory note limits this to cases "wanneer een entiteit uitsluitend van een soort is" in that regulation. Interpretation: a Hikyo entity in scope as a cloud provider would follow [IR 2024/2690](https://eur-lex.europa.eu/eli/reg_impl/2024/2690/oj/eng) directly. |
| Fines | NIS2 sets maximums of at least EUR 10 million or 2% for essential entities and EUR 7 million or 1.4% for important entities (NIS2 Art. 34(4) and (5)). I did not read the Dutch amounts. |

### If Hikyo grows into scope as a cloud provider

- **Significant incident thresholds** under [IR 2024/2690](https://eur-lex.europa.eu/eli/reg_impl/2024/2690/oj/eng): any loss above EUR 500,000 or 5% of turnover, whichever is lower (Art. 3(1)(a)); "successful, suspectedly malicious and unauthorised access ... capable of causing severe operational disruption" (Art. 3(1)(e)); and, for cloud specifically, full unavailability for more than 30 minutes, or data integrity, confidentiality or authenticity "compromised as a result of a suspectedly malicious action" (Art. 7).
- **Reporting.** 24-hour early warning, 72-hour notification and a final report within one month, plus notice to service recipients (NIS2 Art. 23). This runs alongside the CRA report in section 3 if the incident also affects a placed client.

**What this implies for the product:**

- While the Hikyo entity and anything linked to it stay below 50 staff and EUR 10 million, NIS2 does not apply directly.
- The entity choice matters here too. A company that Marc controls, sitting alongside other IT businesses he controls in an adjacent market, may be aggregated with them.
- If NIS2 ever applies, IR 2024/2690 is the detailed rulebook. Its incident thresholds are concrete enough to build monitoring against.

---

## 9. What NIS2 customers will ask of Hikyo as a supplier, and a note on DORA

The breach-notification SLA and supply-chain tickets own the design. This section only sets out what the law asks of Hikyo's customers.

### NIS2 customers

- In-scope entities must address "supply chain security, including security-related aspects concerning the relationships between each entity and its direct suppliers or service providers" ([NIS2 Art. 21(2)(d)](https://eur-lex.europa.eu/eli/dir/2022/2555/oj/eng)). They must consider "the vulnerabilities specific to each direct supplier and service provider and the overall quality of products and cybersecurity practices of their suppliers and service providers, including their secure development procedures" (Art. 21(3)). Recital 85 mentions contractual measures with suppliers, including "software editors".
- **The detailed clause list** in [IR 2024/2690 Annex point 5.1.4](https://eur-lex.europa.eu/eli/reg_impl/2024/2690/oj/eng) binds only the entity types it covers (Art. 1), such as cloud and managed service providers. Interpretation: it is still the most concrete template that any NIS2 customer is likely to use. It asks for contracts, "where appropriate through service level agreements", with:
  - (a) cybersecurity requirements, including those on acquisition in point 6.1;
  - (d) "an obligation on suppliers and service providers to notify, without undue delay, the relevant entities of incidents that present a risk";
  - (e) "the right to audit or right to receive audit reports";
  - (f) "an obligation ... to handle vulnerabilities that present a risk";
  - (g) rules on subcontracting;
  - (h) obligations on termination, including retrieval and disposal of information.
- Supplier selection should weigh suppliers' secure development and the "ability to diversify sources of supply and limit vendor lock-in" (point 5.1.2).
- Acquisition requirements include "requirements regarding security updates throughout the entire lifetime ... or replacement after the end of the support period" and information on components (point 6.1.2(b) and (c)).
- **Dutch customers.** The Cbb requires an entity to check "of haar rechtstreekse leveranciers en rechtstreekse dienstverleners ... voldoen aan de beveiligingseisen", and to check this periodically (Cbb Art. 10). The explanatory note says the supply-chain articles apply only to suppliers "die van invloed zijn op de netwerk- en informatiesystemen van een organisatie". Interpretation: a secrets manager clearly is.

### DORA customers (banks, insurers, investment firms and similar)

- Financial entities "remain fully responsible" when they use ICT third-party services, and keep a register of all such arrangements ([DORA Art. 28(1)(a) and (3)](https://eur-lex.europa.eu/eli/reg/2022/2554/oj/eng)).
- Every ICT contract must include at least: descriptions of all services and subcontracting conditions; the "locations ... where data is to be processed, including the storage location", with advance notice of changes; data protection provisions; access, recovery and return of data on insolvency or termination; service levels; incident assistance "at no additional cost, or at a cost that is determined ex-ante"; cooperation with authorities; termination rights; and participation in security training (Art. 30(2)(a) to (i)).
- For critical or important functions, the contract adds full quantitative service levels, business continuity testing, participation in threat-led penetration tests, "unrestricted rights of access, inspection and audit", and exit strategies with "a mandatory adequate transition period" (Art. 30(3)). Microenterprise customers may let an independent third party exercise the audit right (Art. 30(3), last subparagraph).
- These duties are on the financial entity, not on Hikyo. Hikyo would meet them only through its contract terms. Interpretation: a secrets manager is likely to support critical or important functions for such a customer.

**What this implies for the product:** a standard security addendum covering incident notice, audit reports, vulnerability handling, data location, exit and data return would answer most NIS2 and DORA questionnaires. Its wording belongs to the breach-SLA and supply-chain tickets.

---

## 10. Where this changes `hosted-gdpr.md` sections 8 and 9

The earlier note did not use the Commission's guidance. The guidance changes four of its points.

| Earlier statement | What the guidance adds | Source |
| --- | --- | --- |
| The steward route "probably does not fit a maintainer that also sells the product". | One legal person can be steward for the free community version and manufacturer for the monetised version at the same time. In Example 29, a company that publishes and maintains FOSS without monetising it is "not its manufacturer, but is its steward". In Example 30, a company that offers a paid version is the manufacturer. | Guidance paras 52, 53, 72 to 74; Examples 29 and 30 |
| Recital 15 "plausibly" makes the binaries commercial if the maintainer sells a hosted service. | Narrower. A natural person's free version is out of scope (para 53). What is placed on the market is the software "through which it monetises" the service (para 54, Example 15). The self-hosted server binary is not automatically included. | Guidance paras 52 to 54 |
| Art. 14 reporting "has applied since 11 September 2026" to in-scope products. | True, but only once a product is placed on the market. On the reading in section 1, nothing Hikyo publishes is placed today. The trigger is the commercial launch. | Guidance para 210; section 1 above |
| Group-level aggregation for NIS2 was not researched. | Linked enterprises count in full, including those linked "through a natural person" in the same or an adjacent market. | Recommendation 2003/361/EC, Annex Art. 3(3) and 6(2); section 8 above |

---

## 11. Pending changes that could alter these answers

| Item | Status on 2026-11-17 | Effect if adopted |
| --- | --- | --- |
| **Digital Omnibus**, [COM(2025) 837](https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:52025PC0837), procedure [2025/0360(COD)](https://oeil.secure.europarl.europa.eu/oeil/en/procedure-file?reference=2025/0360(COD)) | Commission proposal. Parliament: "Awaiting committee decision". ITRE draft report on 22 June 2026, amendments on 27 July 2026. No trilogue. The Council agreed its position on 21 October 2026 (see Unverified). | Adds NIS2 Art. 23a: an ENISA single-entry point that "may ... build on the single reporting platform" of the CRA. A CRA Art. 14(3) severe-incident report "shall constitute reporting of information under paragraph 4" of NIS2 Art. 23 (new Art. 23(12)). The memorandum says it "leaves unchanged the underlying legal requirements". It does not amend the CRA's own duties. |
| **Council mandate on the Omnibus** | Press release of 21 October 2026. I could not load the page. Search summaries say the Council prefers national entry points plus an EU information point over one EU entry point. | Would change where a joint CRA and NIS2 report is filed, not the deadlines. |
| **Dutch CRA implementing act** (36.875) | Passed the Tweede Kamer on 10 September 2026. Eerste Kamer committee report on 3 November 2026, awaiting the government's reply. Enters into force by royal decree. | Sets the Dutch penalty rules and the RDI's powers. Not read for this note. |
| **Commission guidance** | Published in the OJ today as C/2026/6013. The cover note said it applies only once all language versions are available. | Now formally adopted. Not binding on courts (para 8). |
| **Implementing acts under the CRA** | SBOM format (Art. 13(24)), simplified technical documentation for micro and small enterprises (Art. 33(5)), notification format (Art. 14(10)). Not checked for adoption. | Would fix the format of documents in the section 5 gap list. |
| **Harmonised standards** | Not found in the OJ (see Unverified). | A cited standard would open Module A for class I products that are not FOSS. |
| **SRP review** | The Commission reviews the platform by 11 September 2028 (CRA Art. 70(2)). | Possible changes to the reporting channel. |

---

## Unverified or not researched

| Item | Status |
| --- | --- |
| Whether any CRA harmonised standard is cited in the OJ under Art. 27 | **Not verified.** I found no citation. Only secondary commentary says none exists. The primary check is the Commission's list of OJ references for Regulation (EU) 2024/2847 and the implementing decision on standardisation request M/606. Consequence if none: a class I product that is not FOSS needs a notified body (Art. 32(2)). FOSS keeps Module A only through Art. 32(5). |
| Council position on the Digital Omnibus | **Partly verified.** The press release page returned HTTP 403. The content comes from search summaries. |
| SRP version 1.4 and lack of voluntary (Art. 15) reports at launch | **Not verified.** Seen only in a law-firm summary. Not relied on. |
| Penalty clauses and RDI powers in the Dutch CRA implementing act (36.875) | **Not researched.** The act is not adopted yet. This note does not claim that CRA fines are unenforceable in the meantime. |
| Dutch NIS2 fine amounts and supervisor designation decrees (BWBR0052823, BWBR0052879) | **Not researched.** |
| Whether the Netherlands uses the NIS2 recital 16 independence option for linked enterprises | **Not found** in the Cbw or the Cbb text. A ministerial regulation was not checked. |
| Commission CRA FAQ of 3 December 2025 | **Not read.** The guidance replaces much of it. |
| Any CRA amendment proposal, or the 2026 Cybersecurity Act revision with NIS2 changes | **Not researched.** |
| Whether a secrets manager falls in an Annex III category | **Open.** No authority found. Section 6 sets out the arguments. |
| Which Hikyo clients count as "the software through which it monetises" the hosted service | **Open.** No authority closer than guidance Example 15. |
| Who is the manufacturer if a company sells the service but Marc publishes the clients and owns the trademarks | **Open.** The guidance does not address split publisher and monetiser. |
| Marc's other business interests, for NIS2 and SME aggregation | **Unknown.** Not in the facts given. |
| Whether Annex I Part I is met by the current product | **Not assessed.** Out of scope for a legal note. |

---

## Sources

**EU legislation** (read from the Cellar copy of the same CELEX)

- Regulation (EU) 2024/2847 (Cyber Resilience Act): https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng
- CRA corrigendum of 2 July 2025 (Art. 64(10)): http://data.europa.eu/eli/reg/2024/2847/corrigendum/2025-07-02/oj
- Commission Implementing Regulation (EU) 2025/2392 (technical descriptions of Annex III and IV categories): https://eur-lex.europa.eu/eli/reg_impl/2025/2392/oj/eng
- Directive (EU) 2022/2555 (NIS2): https://eur-lex.europa.eu/eli/dir/2022/2555/oj/eng
- Commission Implementing Regulation (EU) 2024/2690 (NIS2 technical and methodological requirements): https://eur-lex.europa.eu/eli/reg_impl/2024/2690/oj/eng
- Commission Recommendation 2003/361/EC (SME definition): https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32003H0361
- Regulation (EU) 2022/2554 (DORA): https://eur-lex.europa.eu/eli/reg/2022/2554/oj/eng
- Proposal COM(2025) 837 (Digital Omnibus): https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:52025PC0837

**Commission and EU bodies**

- Commission guidance on the CRA, C(2026) 5252, OJ C/2026/6013 of 17 November 2026: https://eur-lex.europa.eu/eli/C/2026/6013/oj/eng
- The same guidance as the annex PDF, used for paragraph numbers: https://transition-pathways.europa.eu/system/files/media/document/2026-09/C_2026_5252_1_EN_annexe_acte_autonome_cp_part1_v3_kXuCPwXXxAuG8Uj44Ar0o2Vros_131456.pdf
- Commission announcement of the guidance: https://digital-strategy.ec.europa.eu/en/library/commission-publishes-new-guidance-support-timely-cyber-resilience-act-implementation
- Commission page on CRA reporting: https://digital-strategy.ec.europa.eu/en/policies/cra-reporting
- ENISA, single reporting platform launch, 11 September 2026: https://www.enisa.europa.eu/news/the-cra-single-reporting-platform-is-launched
- European Parliament Legislative Observatory, 2025/0360(COD): https://oeil.secure.europarl.europa.eu/oeil/en/procedure-file?reference=2025/0360(COD)
- Council press release, 21 October 2026 (not loaded, see Unverified): https://www.consilium.europa.eu/en/press/press-releases/2026/10/21/digital-rulebook-council-agrees-position-to-simplify-data-protection-privacy-and-cyber-reporting-rules/

**Netherlands**

- Cyberbeveiligingswet (BWBR0052872): https://wetten.overheid.nl/BWBR0052872
- Cyberbeveiligingsbesluit (BWBR0052875): https://wetten.overheid.nl/BWBR0052875
- Staatsblad 2026, 189 (Cyberbeveiligingsbesluit with explanatory note): https://zoek.officielebekendmakingen.nl/stb-2026-189.html
- NCSC, "Meldplicht Cyber Resilience Act gaat vandaag in", 11 September 2026: https://www.ncsc.nl/nieuws/meldplicht-cyber-resilience-act-gaat-vandaag-in
- RDI, Cyber Resilience Act: https://www.rdi.nl/onderwerpen/draadloze-apparatuur/handel-en-apparatuur/cra
- Eerste Kamer, Uitvoeringswet verordening cyberweerbaarheid (36.875): https://www.eerstekamer.nl/wetsvoorstel/36875_uitvoeringswet_verordening

**Repository**

- `SECURITY.md` and `SUPPORT.md` on `origin/dev`, read on 2026-11-17.
- `docs/research/hosted-gdpr.md` on branch `research/hosted-gdpr`.
