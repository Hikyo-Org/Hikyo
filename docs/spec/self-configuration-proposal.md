# Hikyo self-configuration design

Status: D11 expanded by explicit user instruction, 2026-09-06. The earlier nine-key implementation is in PR #686 and passed its delivery checks. All-variable activation is not yet implemented or verified. The [validation record](../reports/self-configuration/validation.md) distinguishes earlier evidence from the expanded work.

## Report and authority

Open the [standalone HTML report](../reports/self-configuration/index.html), or serve its directory as described in the [report README](../reports/self-configuration/README.md). The [structured decision record](../reports/self-configuration/report-data.json) is the source for the generated report. Every question includes alternatives, recommendation, decision, rationale, repository evidence and acceptance conditions.

The user explicitly approved UI application without restarting the container, instance-admin control of the system organization, and separate configuration projects for independent remotes with sharing confined to HA replicas of one logical instance. The user then delegated the remaining recommendations: "I trust your instincts, assume I agree with the coming recommendations." The user subsequently replaced D11 with every Hikyo variable and remote Apply, restricted to instance administrators using passkey or TOTP. D09 and D11 now record that explicit instruction; the other choices retain their recorded authority. These are not individual user answers and do not imply merge or production-deployment approval.

## Selected behavior

1. During fresh setup on each logical instance, provision a real protected organization `Hikyo`, an instance-named project and environment `Production`. A unified root management view groups references to these owner-local projects. Bind owner instance, org, project and environment IDs. Create normal encrypted values, schema, explicit scoped grants, initial snapshot and runtime binding atomically after the first administrator exists. Existing installations use an explicit, idempotent adoption operation with a preview.
2. Use the existing matrix, drafts, publication, history and rollback. Instance settings links to the project. Saving a draft and publishing a revision do not activate it. An instance administrator with an MFA-authenticated session applies an exact published revision through the UI after a fresh, single-use passkey or TOTP ceremony. Password-only or recovery-only authentication and machine tokens do not substitute; the target owner checks authority and exact revision binding.
3. Cover every supported Hikyo server variable, with remote Apply by the target instance administrator. Record each key's source, default/absence semantics, secrecy, owner or node scope, validator and real activation mechanism. The prior nine-key restriction is superseded. Application settings require component or application-generation replacement; deployment/bootstrap settings require dedicated durable transitions. A client-only variable is not server configuration, and a retired variable remains refused. The independent unlock source cannot live only inside the database it unlocks. See the [full-catalogue implementation design](self-configuration-full-catalogue.md).
4. The applied revision owns managed values after adoption. External variables/files seed once; stale inputs cannot override or invalidate a valid managed revision. Prepare components, record a durable target generation and atomically swap an immutable bundle per process. Pre-commit failure leaves the previous target intact; post-commit failure stays pending/partial and reconciles to the committed target.
5. HA tracks only replicas of the selected logical instance and fences stale consumers. Independent remotes never join its apply, share its variables or inherit root values. Bounded runtime retention roots protect required payloads. Normal rollback restores into drafts and publishes a new revision. Explicit host-local recovery handles invalid stored config; restored backups fence outbound self-config use until reconciliation and explicit resumption.

## Remote-instance clarification

The user proposed a root organization with one project per remote and explicitly distinguished remote instances from HA. The selected interpretation is one root **management view** with separate project references, rather than a physical organization storing other instances' secrets centrally. This interpretation is a delegated recommendation; the remote/HA separation is an explicit user requirement.

Each independent instance keeps its physical protected organization, project, schema, values, history and active generation locally. A browser accesses a remote through that remote's existing workspace API and owner-side authorization. Local administrator rights and directory credentials do not grant remote configuration access. Connecting or removing a remote changes the management view, not the remote's stored configuration.

HA replicas share one logical instance, one PostgreSQL database and one root-key authority, so they share that instance's configuration project. An independent remote is never an environment or replica of another project's configuration. Root membership creates no defaults, inheritance, copying or coordinated apply. Node-specific configuration requires a per-node activation path; a shared project must never distribute the same node identity to every HA replica. Bootstrap source custody remains independently available even when its transition is initiated from Hikyo.

The [multi-instance ADR](../adr/multi-instance.md) explicitly preserves owner-local secrets, browser-direct access and symmetric instances without a central main server. A physical central root organization would change that contract and is not silently introduced here.

## Decisions

| ID | Question | Selected recommendation | Authority |
| --- | --- | --- | --- |
| [D01](../reports/self-configuration/index.html#D01) | How should configuration changes take effect? | Apply from Hikyo. | Explicit user approval |
| [D02](../reports/self-configuration/index.html#D02) | Should Hikyo use its normal interface or a separate settings store? | A real project, using the normal matrix. | Delegated approval |
| [D03](../reports/self-configuration/index.html#D03) | When are the system resources created? | Idempotent provisioning during initial setup. | Delegated approval |
| [D04](../reports/self-configuration/index.html#D04) | How do existing installations adopt managed configuration? | One-time adoption from instance settings. | Delegated approval |
| [D05](../reports/self-configuration/index.html#D05) | Can display names or normal lifecycle operations break the system binding? | A durable instance binding to immutable resource IDs. | Delegated approval |
| [D06](../reports/self-configuration/index.html#D06) | Who can change Hikyo's own configuration? | Instance-admin access for all system configuration changes. | Explicit user approval |
| [D07](../reports/self-configuration/index.html#D07) | Where should system-resource protection be enforced? | Enforce it centrally, below every transport. | Delegated approval |
| [D08](../reports/self-configuration/index.html#D08) | How does the server consume its own secrets? | An internal, operation-bound runtime consumer. | Delegated approval |
| [D09](../reports/self-configuration/index.html#D09) | Should applying settings require reauthentication or another reviewer? | Target-instance administrator plus fresh passkey or TOTP for Apply. | Explicit user approval |
| [D10](../reports/self-configuration/index.html#D10) | What can leave the system project? | Normal encryption and secret ceremonies, with a closed consumer boundary. | Delegated approval |
| [D11](../reports/self-configuration/index.html#D11) | Which configuration belongs inside Hikyo? | Every supported Hikyo server variable, with remote Apply and explicit activation rules. | Explicit user approval |
| [D12](../reports/self-configuration/index.html#D12) | Which source wins after adoption? | One authority per key: the applied revision. | Delegated approval |
| [D13](../reports/self-configuration/index.html#D13) | How should file-based mail secrets and custom trust be imported? | Import effective contents, never make remote apply a file-reading interface. | Delegated approval |
| [D14](../reports/self-configuration/index.html#D14) | Does Apply need to contact the SMTP server? | Local validation plus an explicit test of an exact revision. | Delegated approval |
| [D15](../reports/self-configuration/index.html#D15) | Can users loosen validation or change the system schema? | A versioned runtime-owned schema with server-side validation. | Delegated approval |
| [D16](../reports/self-configuration/index.html#D16) | What is the unit of activation? | One revision and one immutable runtime bundle per process. | Delegated approval |
| [D17](../reports/self-configuration/index.html#D17) | How do concurrent applies and crash recovery behave? | A durable apply job and compare-and-swap generation. | Delegated approval |
| [D18](../reports/self-configuration/index.html#D18) | What does Apply mean for HA replicas versus independent remotes? | A fenced generation with visible node convergence. | Delegated approval |
| [D19](../reports/self-configuration/index.html#D19) | What status and bounds should operators see? | An asynchronous job with honest status. | Delegated approval |
| [D20](../reports/self-configuration/index.html#D20) | Does a failed relay connection undo an applied revision? | Separate configuration activation from delivery health. | Delegated approval |
| [D21](../reports/self-configuration/index.html#D21) | How does rollback work? | Use normal restore and publication, then explicit apply. | Delegated approval |
| [D22](../reports/self-configuration/index.html#D22) | What prevents garbage collection of the running config? | A first-class runtime retention reference with a narrow lifecycle. | Delegated approval |
| [D23](../reports/self-configuration/index.html#D23) | How can an operator recover when the UI cannot start? | A documented host-local recovery command. | Delegated approval |
| [D24](../reports/self-configuration/index.html#D24) | What happens after backup restore or key rotation? | Integrate with the existing encryption and recovery lifecycle. | Delegated approval |
| [D25](../reports/self-configuration/index.html#D25) | How should this design become shipped behavior? | One implementation path, with explicit design-to-code gates. | Delegated approval |
| [D26](../reports/self-configuration/index.html#D26) | Do independent remote instances share configuration? | Separate projects for independent instances; shared project only within an HA instance. | Explicit user approval |
| [D27](../reports/self-configuration/index.html#D27) | Should the root organization physically store every remote’s configuration? | One Hikyo root management view; a protected physical organization remains on each owner. | Delegated approval |

## Implementation sequence

1. **Contracts and authority.** Amend the owning permission, tenant-isolation, revision, audit, encryption, operations and mail contracts. Register the bound system-resource profile and runtime system site; enumerate affected operations and audit events. Preserve the existing multi-instance no-proxy/no-central-state contract and distinguish root navigation from a physical organization. Required proof: Formula, isolation, import-boundary and audit completeness checks pass. Scoped authority cannot target another tenant. Mail #608 is checked for drift before edits.
2. **Provisioning and durable storage.** Both-engine migrations for the immutable binding, generation/apply jobs, node acknowledgements and bounded retention references. Shared fresh-setup/adoption service seeds normal keys, secret cells and an initial snapshot from effective server config. Bind every record to its owning logical instance; HA setup deduplicates only replicas, never independent remotes. Required proof: SQLite/PostgreSQL tests cover concurrent bootstrap, HA adoption disagreement, collisions, transaction rollback, one-time import, key rotation and GC/apply races.
3. **Runtime activation and mail integration.** Coordinate the mail library seam from #608; shared immutable config manager; exact-revision apply/status API and human CLI; client regeneration and parity records; node preflight, fencing and recovery commands. Route remote human operations directly to the owner through the existing workspace session. Scope HA coordination to that owner. Required proof: Race tests and crash injection cover before/after commit, swap, idempotency, per-node convergence and restart. SMTP conformance and zero unsolicited egress remain true.
4. **Browser workflow and operations docs.** System project marker in the existing matrix, settings shortcut, pending/active state, candidate/active test email, publish/apply, per-node status and normal history rollback. Add setup/adoption/recovery instructions to docs navigation, index/search and relevant operational pages. Add the unified root view of owner-labelled project references; remote schemas, values and status stay independent, with explicit unsupported/offline/access states. Required proof: Desktop/mobile and both themes: administrator completes the full mail change using only the browser; process start time stays unchanged; unauthorized controls and requests are refused.
5. **Failure, restore and delivery verification.** Verify all acceptance cases together, including two-node HA, failed SMTP, revocation, stale direct traffic, retained snapshots, restore fencing and backup/key rotation. Review final changes; deliver a signed, DCO-signed PR when implementation is requested. Required proof: Relevant checks and both-engine lifecycle suites pass; exact-head CI, adversarial review as required by authorship, and preview evidence precede merge. Production deployment requires its own actual delivery evidence. A two-remote-plus-HA scenario must prove distinct variables, schemas and revision identities; no cross-instance effects, secret proxying or viewer-outage dependency.

This sequence records the earlier nine-key implementation. Its verification does not prove the expanded D11 scope. The [full-catalogue design](self-configuration-full-catalogue.md) supplies the additional application-lifecycle and bootstrap-transition requirements. Consult the [handoff](../handoff/self-configuration-design.md) for the current delivery state.

## Contract amendments required

The permission, tenant-isolation, revision, audit, encryption and operations ADRs remain canonical until visibly amended. Preserve the existing multi-instance no-proxy/no-central-state contract; the root view introduces no global tenant or authority. Required changes include the bound system-resource operation profile and its instance-config conjunction; a narrow registered runtime system site; bounded runtime retention roots; activation/recovery events; HA convergence/fencing; and restored managed credentials.

The mailer contract in [social-signin.md](./social-signin.md) section 7 and [#608](https://github.com/Hikyo-Org/Hikyo/issues/608) currently uses startup-loaded process configuration. Reuse its library, TLS/egress posture, test transport and send semantics while extending its source to managed activation. Unrelated local sign-up flows are outside this work, but the mail seam must be coordinated with that ticket. Runtime configuration apply does not enable the retired software-upgrade mechanism.

## Evidence baseline

Inspected checkout: `90b4ca6a5d22438e751cf9af83aa4fd077a6a61c`. Issue #608 was open when checked on 2026-09-06.

- `internal/service/bootstrap.go`: first-admin creation currently provisions no organization/project.
- `internal/config/config.go` and `internal/app/app.go`: flags/environment configure components at startup; no managed activation path exists.
- `internal/app/tls.go` and `cmd/hikyo/tls_signal_unix.go`: TLS certificate reload exists; SIGHUP is not general config reload.
- `internal/app/ha.go` and `internal/authz/authorize.go`: HA coordination and transaction-bound system authority provide extension points, not permission to bypass scope.
- `internal/service/pins.go`, `internal/store/retention.go` and the revision ADR: ordinary pins expire, so preserving a long-lived applied snapshot needs an explicit runtime retention contract.

## Required end-to-end result

An administrator authorized on the owning instance completes setup/adoption, edits that instance’s project in the matrix, publishes, tests the selected revision and applies it. Subsequent mail uses the new values on every admitted node with process start times unchanged. A restart loads the committed target, not the newest draft or stale environment. Invalid apply, failed SMTP, partial HA and rollback each display distinct outcomes without secret leakage. Two independent remotes with different variables and an HA pair on one remote must prove that apply affects only the selected owner’s replicas; disconnecting the viewer leaves both remotes operational.

The HTML report is a working document artifact. The original nine-key runtime behavior is implemented and verified. The expanded D11 behavior requires additional implementation and verification before the feature can be described as managing every server variable.
