# Security checks

Hikyo tests security behavior through its native application interfaces. The
full validation pipeline includes adversarial isolation tests, coverage-guided
Go fuzzing, race detection, encryption and recovery checks, browser tests,
repository security analyzers, and linked-binary vulnerability checks.

Security results describe a particular revision and test profile. A green
branch badge is not evidence that a different released binary passed those
checks, and a signature authenticates an artifact rather than its behavior.

## Release validation

Nightlies and stable/prerelease candidates require full source validation for
the immutable release revision. The stable tag path additionally checks the
tag's actual commit and its relationship to the protected main history. A
previous successful run for another revision cannot replace this validation.

After publication, a separate GitHub-hosted job downloads the release and
verifies its signed identity before executing its binary in disposable
fixtures. This job is nonblocking. It does not edit release notes, upload
release assets, or publish a passing report when validation fails.

Source validation and artifact validation are separate claims. Publication
does not wait for the post-release campaign. A missing report means that a
passing artifact result is unavailable, not that the version passed.

## Read the evidence

Open the exact version's validation run, rather than the latest successful run
or a branch badge. The passing security-checks artifact contains JSON evidence
and an HTML report that opens without a server. Compare the release tag,
source commit, harness revision, tested binary digest, profile, datastore,
executed cases and run attempt before reusing a result.

Reports have GitHub Actions artifact retention limits. Expired, missing,
cancelled and failed runs do not inherit a newer or older passing result.
Vulnerability results record a scan time; they are historical observations,
not an indefinitely current statement about dependencies.

The post-release profile is `hikyo-release-runtime-v1`. Its
`security-pass-TAG-SOURCE-RUN-ATTEMPT` artifact is retained for 90 days. It
requires the complete native artifact campaign on both datastores, the
authenticated Chromium campaign on the same binary, a vulnerability scan of
those bytes, and successful fixture cleanup. A skipped or incomplete mandatory
case prevents PASS publication. The JSON includes the producing harness commit
and the exact run attempt.

The native campaign has 14 mandatory scenarios per datastore and bounded
generated grant, revocation and restart sequences. Its `assertions` count
describes successful oracle groups, not individual comparisons, requests or
independent attacks. The server uses normal production admission, TLS and
native WebAuthn ceremonies with the default zero disclosure window. Requests
are paced within the normal authentication and MCP limits.

The browser campaign is a separate native-auth markup and CORS profile. It
tests the compiled SPA embedded in the same binary with default admission and
service budgets and no fake adapter provider. Its loopback HTTP `--dev`
transport and deliberately configured 300-second development/zero-second
protected disclosure windows are recorded. It does not claim full production
browser deployment or external identity-provider coverage.

The pinned binary scanner blocks symbol-reachable findings, matching the full
CI scanner policy. Module and package advisory records remain visible with
their identifiers and classification. They are not silently counted as zero
vulnerabilities or treated as fully triaged. The runtime receipt explicitly
does not infer source-fuzz execution counts or current CodeQL/Dependabot status.

## Coverage and its limits

| Layer | Assertion | Limit |
| --- | --- | --- |
| Authorization | Cross-tenant and capability refusals, real missing-object comparisons, current-grant and credential revocation checks | A bounded regression and generated-sequence profile cannot enumerate all possible application histories |
| State and disclosure | Synthetic canaries must not escape denied reads; denied writes preserve protected state | Operator/root compromise is outside the application's isolation boundary |
| Artifact boundary | Real binary, native bootstrap and issued credentials, disposable SQLite and PostgreSQL | A Linux binary campaign does not prove every operating system, CPU architecture, image or deployment configuration |
| Fuzzing | Existing crash targets plus semantic and metamorphic assertions for accepted data | Fuzz iterations are not distinct attacks or a security coverage percentage |
| Browser | Authenticated compiled SPA, hostile stored content, browser execution and controlled egress assertions | A browser corpus is bounded and does not prove every extension, browser engine or malicious client behavior |
| Encryption and recovery | Tenant-bound encryption, tamper/truncation refusal and cross-engine restore invariants | Recovery exercises do not establish the security of every operator procedure |
| Static and dependencies | Repository analyzers and linked-binary vulnerability checks | Tool findings depend on rules, supported code paths and database freshness |

Keep the existing CI badge. The [validation runs](https://github.com/Hikyo-Org/Hikyo/actions)
and their version-bound reports provide the security detail; a separate fuzz
badge would add no artifact-specific assurance.

## Maintaining the profile

Preserve deterministic regressions when fuzzing or a generated sequence finds
a failure. Retain the reproducing seed and operation sequence, minimize it,
and verify both the refused operation and a legitimate positive control.
Negative-control fixtures must demonstrate that the test oracle detects a
deliberately broken boundary without changing production behavior.

Changes to authorization, authentication, encryption, ingress, adapters or
delivery also require maintainer security review under
[CONTRIBUTING.md](../CONTRIBUTING.md#security-sensitive-contributions).
Automated tests supplement that review. This profile is not a penetration-test
certification or an ASVS certification.

## Review traceability

This limited map uses **OWASP ASVS 5.0.0**, rather than a moving standard.
It identifies useful review questions and executable evidence owners. It
does not establish conformance with an ASVS level or all requirements.

| Requirement | Evidence owner | Review still required |
| --- | --- | --- |
| 8.2.1, function permissions | Native denied-mutation control; isolation operation/formula matrices | New operations and capability combinations |
| 8.2.2, object permissions | Existing-object versus authorized-missing twins over native REST/MCP | New object types and scope relationships |
| 8.2.3, field permissions | Secret/config projection and disclosure tests | New classifications and disclosure paths |
| 8.3.2, authorization changes | Generated grant transitions, separate credential revocation and restart controls; native isolation replica tests | New caches, distributed schedules and provider-driven changes |
| 8.4.1, tenant boundaries | Native cross-tenant controls and unchanged protected state | New tenant-scoped operations and side effects |
| 16.2.5, sensitive log data | Audit canary and credential checks; native audit/crypto tests | New event payloads and downstream export destinations |
| 16.3.2, denial logging | Native durable denial audit control | Completeness and event correlation across new denial paths |
| 16.5.1, safe errors | Uniform refusal twins, body/header canaries and negative controls | Unexpected faults and new error serialization |

Requirement meanings come from the pinned
[ASVS authorization chapter](https://github.com/OWASP/ASVS/blob/v5.0.0/5.0/en/0x17-V8-Authorization.md)
and [logging/error chapter](https://github.com/OWASP/ASVS/blob/v5.0.0/5.0/en/0x25-V16-Security-Logging-and-Error-Handling.md).
The runtime campaign does not cover datastore fault injection, backup restore,
external providers, arbitrary concurrent schedules or detector/notification
systems. Existing source suites supply additional coverage where documented;
manual review remains necessary for the rest.

OpenAEV is not a CI dependency. Detection and response drills, when needed,
have a separate purpose from these application behavior assertions. Private
vulnerability reporting remains governed by [SECURITY.md](../SECURITY.md).
