# Deployment landscape and Hikyo gaps

Research date: 31 October 2026. Hikyo source: `929ce90ff70986ef3482f68acaa51185e0ec6ce7`.

## Assessment

Hikyo already has the main deployment building blocks: a native server, Linux packages, multi-architecture containers, Compose definitions, a first-party Helm chart, application-tier HA, encrypted backups, and guarded migrations. The larger gap is turning those components into an easily discoverable, reproducible installation and operating experience. Our chart exists in source; our public stable distribution and a tested GitOps lifecycle are separate, unfinished propositions. [H01], [H02], [H03], [H04], [H05]

The recommended order is to establish installable artifacts and complete reference installations, add tested Flux and Argo CD recipes within the existing migration contract, then improve chart integrations and operating examples. A general server lifecycle operator or additional cloud provisioning frameworks should follow demonstrated demand. These are research recommendations, not accepted scope or authorization to implement them.

## Scope and reading the comparison

The sample covers secrets-management peers, identity infrastructure, developer platforms, automation, observability, and a common home-server application. It is a representative selection of widely used projects, not a measured popularity ranking. Argo CD is an additional reference for declarative Kubernetes packaging.

- **First-party** means the project's documentation or source identifies the offering as its own. It does not imply a commercial support entitlement.
- **Community** preserves the ownership stated by the official documentation. A chart being linked from a project does not make it vendor-maintained.
- **Not established** means no such offering was established in the reviewed sources. It is not a claim that no community solution exists.
- A **server lifecycle operator** deploys or manages the application itself. A **secrets delivery operator** distributes values to workloads. They are different capabilities.
- A Terraform provider for application resources is not an infrastructure module that installs the server. A Docker-based install script is not a native server package.
- Chart presence does not prove GitOps compatibility, a safe migration path, zero downtime, or disaster recovery.

This is a source and documentation investigation. No peer software was installed or benchmarked, no cluster or cloud account was changed, and no current CI, runtime, recovery, or release acceptance was inferred from older receipts.

## Who offers which installation routes

Every linked source in a row supports the corresponding qualifications. Detailed lifecycle differences follow the route comparison.

| Application | Native server / packages | Container / Compose | Kubernetes packaging and ownership | Important deployment qualification |
| --- | --- | --- | --- | --- |
| **Hikyo** | Six OS/architecture archive targets; Linux local-install packages; nightly assets observed; signing documented | Official amd64/arm64 image pipeline and manual/unattended Compose definitions | First-party `chart/hikyo`; documented OCI address; public package access **unverified** | Only prereleases observed. PostgreSQL required by chart; Secrets and persistent upgrade custody supplied separately. [H01], [H02], [H03], [H06] |
| **Vault** | Official native executable / OS packages | Official container route; reviewed full TLS/HA Compose recipe is **Enterprise-specific** | First-party HashiCorp server Helm chart | Integrated Storage HA needs quorum, persistent storage and initialization/unsealing. Advanced replication/automated snapshots have Enterprise caveats. [Native lifecycle][V1], [Docker][V2], [Kubernetes][V3], [architecture][V5] |
| **OpenBao** | Official executable and package-manager installs | Official images; maintained first-party Compose deployment not established | Project-owned OpenBao server Helm chart | HA example uses Integrated Storage. Version-specific migration docs take precedence over older File-storage examples: OpenBao 2.7 removes File storage. [Install][O1], [Helm][O2], [migration][O4] |
| **Infisical** | Official **AMD64-only DEB/RPM server package**, including service/control tooling | Official Compose with PostgreSQL/Redis for evaluation or small installations | Official server Helm chart; production guidance uses external PostgreSQL/Redis | Bundled chart databases are testing-only, not HA. Native server packages are separate from the client CLI. [Linux package][I1], [Compose][I2], [Helm][I3] |
| **Bitwarden** | Standard Linux installer runs Docker/Compose, not a standalone native server | Official multi-container server distribution | Official `bitwarden/self-host` Helm chart; provider-specific guides | SQL, attachments/data-protection state and shared storage need explicit ownership. Basic self-hosting is free; paid features need licenses. [Standard deployment][B1], [Helm][B2], [licensing][B3] |
| **Grafana** | Official OS-specific packages and downloads | Official Docker and Compose instructions | Official docs direct users to a **community-maintained** Grafana chart | HA requires a shared MySQL/PostgreSQL database and multiple servers; the database's availability remains the operator's responsibility. [Install][g-install], [Docker][g-docker], [chart][g-chart], [HA][g-ha] |
| **GitLab** | Official Linux package route | Official Docker and Compose route | First-party Helm chart and a server lifecycle operator | Production Kubernetes guidance uses a Cloud Native Hybrid architecture with external stateful services. The operator has explicit production limitations. [Methods][gl-methods], [Docker][gl-docker], [chart][gl-chart], [operator][gl-operator] |
| **Keycloak** | Java server distribution | Official container route; a full production Compose distribution was not established | First-party server operator, OLM and Kustomize resources; **experimental first-party Helm chart installs the operator** | Operator does not provision the database. An operator chart is not a direct chart for a standalone server deployment. [Guides][kc-guides], [operator install][kc-install], [deployment][kc-deploy] |
| **Immich** | No endorsed native production server route established | **Compose is explicitly recommended for production** | Official documentation identifies an official Helm chart | NAS/all-in-one alternatives are labeled community. A documented Kubernetes install is not evidence of a full HA or automated recovery service. [Compose][im-compose], [Kubernetes][im-k8s], [requirements][im-requirements] |
| **n8n** | npm installation documented | First-party Compose configurations, including PostgreSQL and queue workers | Official OCI Helm chart; raw manifests also supplied | Queue mode needs PostgreSQL and Redis. Multi-main HA is Enterprise-only. Chart `main` also contains next-major release-candidate work; select a released chart explicitly. [Hosting][n-host], [hosting repository][n-repo], [chart][n-chart], [queue][n-queue] |
| **Argo CD**, packaging reference | CLI binary is not a standalone native server deployment | Production Compose route not established in reviewed installation docs | First-party non-HA/HA manifests and Kustomize; documented Helm chart is **community-maintained** | Official documentation includes self-management through an Argo CD Application. This is a concrete GitOps recipe, beyond generic Helm compatibility. [Install][a-install], [self-management][a-declarative] |

## Who handles the operating lifecycle

| Application | Server lifecycle / HA | Upgrade and recovery model | What Hikyo can learn |
| --- | --- | --- | --- |
| Vault / OpenBao | Raft-based HA and operational runbooks; their reviewed first-party secrets operators/injectors do not establish a server lifecycle operator. [Vault][V3], [OpenBao][O2] | Vault calls for isolated restore/upgrade rehearsal. OpenBao documents snapshots and external scheduling; its File-to-PebbleDB migration explicitly suspends GitOps reconciliation. [Vault upgrade][V1], [OpenBao backups][O3], [migration][O4] | Separate storage, quorum, unsealing and application rollout responsibilities in the installation guide. |
| Infisical | Production Kubernetes architecture includes external stateful services; documented on-premises design uses CloudNativePG. [Helm][I3], [HA][I4] | Preserve encryption keys separately from database backups. Cross-data-center recovery in the HA guide is human-initiated, not automatic. [Compose recovery][I2], [HA architecture][I4] | Make supported production topologies and dependency ownership apparent before installation. |
| Bitwarden | Installer/control workflow and Helm configuration; Secrets Manager operator delivers secrets, not server releases. [Standard install][B1], [operator][B5] | Standard SQL container performs nightly DB backups. Full recovery also needs configuration, attachments and data-protection material; Helm backup scheduling remains operator-owned. [Backups][B4] | Distinguish installation flavors and their support boundaries instead of treating every container deployment identically. |
| Grafana | HA is documented separately from installation. Grafana Operator can manage deployments and application resources. [HA][g-ha], [operator][g-operator] | A chart is one packaging option; the operator is another project. Neither removes the need to operate the underlying database. | Offer clear examples that combine existing primitives without promising to provision every dependency. |
| GitLab | Lifecycle operator with limitations; capacity-based reference architectures and infrastructure tooling. [Operator][gl-operator], [architectures][gl-architectures], [IaC][gl-chart] | Selected architectures support documented zero-downtime upgrade procedures, with stated limits. That claim depends on the architecture and setup, not just replica count. [Chart installation][gl-chart] | Publish a small number of complete, bounded reference topologies and show their evidence. |
| Keycloak | First-party operator manages server deployments. [Deployment][kc-deploy] | Compatible patch updates can roll; other changes require a stop. Database rollback requires restoring the old installation and database backup. OLM guidance recommends manual approval. [Compatibility][kc-compat], [upgrade guide][kc-upgrade], [operator install][kc-install] | Keep maintenance and restore rules explicit. Hikyo's full-stop migrations are a defensible contract; zero downtime would require a new compatibility design. |
| Immich | Compose gives a deliberately approachable production path; full app HA not established here. [Compose][im-compose] | Scheduled database dumps exist, but database backups exclude photos/videos. Separate file backups and a recovery procedure remain necessary. [Backup][im-backup] | Show exactly which state each backup protects and what must be recovered separately. Hikyo already separates the archive, root key and installation custody. [H05] |
| n8n | Queue workers, webhook processors and licensed multi-main topology are separate scaling roles. [Queue][n-queue] | The official hosting repo provides deployment variants; its chart documents infrastructure controls including routing, HPA/KEDA and disruption budgets. These are not an end-to-end recovery guarantee. [Repository][n-repo], [chart][n-chart] | Supply named examples and explain when scaling needs extra dependencies or a different operating contract. |
| Hikyo | PostgreSQL-backed application HA, PDB and soft node spreading; a separate delivery operator and a narrowly enrolled configuration executor. [H04], [H07], [H08] | Full-stop HA upgrade, one verified migrator, exact-target boot. Unattended initial opt-in supports Docker/SQLite or singleton Kubernetes/PostgreSQL, with encrypted local custody and scratch restore. No automatic old-image rollback after SQL writes. [H04], [H09] | Preserve these safeguards while improving installation and integrations. The configuration executor is not a general server release controller. |

### Actual GitOps evidence, beyond chart availability

| Offering | What the reviewed official sources establish | Limit |
| --- | --- | --- |
| Vault | Enterprise/OpenShift validated design includes Argo CD Applications/ApplicationSets, ordering and PVC protection; it also discusses applicability to Flux. [Validated design][V4] | This is a named Enterprise/OpenShift design, not a universal Flux HelmRelease recipe. |
| OpenBao | Official Flux integration article covers workload identity, Transit/SOPS and signature verification. Its storage migration guide gives Flux/Argo suspend/resume instructions. [Integration][O5], [migration][O4] | A secrets integration and a migration interlock are not a turnkey production server-install recipe. |
| Infisical / Bitwarden | Reviewed operator and GitOps material concerns delivering secrets to consumers. [Infisical][I5], [Bitwarden][B5] | Do not count secret-delivery resources as server installation or server lifecycle management. |
| Argo CD | First-party self-management recipe uses an Application and Kustomize resources. [Self-management][a-declarative] | It establishes this application's packaging pattern, not Hikyo upgrade compatibility. |
| Hikyo | A Helm chart and a separate future Flux-controller contract exist. [H02], [H11] | No shipped Flux/Argo server-deployment recipe was found; legacy Flux apply is retired. [H12] |

For Grafana, GitLab, Keycloak, Immich and n8n, the reviewed sources establish the deployment ingredients in the main table. This research did not establish a project-owned, complete Flux/Argo server-install recipe for those applications. Generic Helm/Kustomize compatibility remains different from a tested application-specific recipe.

## What proper deployment techniques mean

There is no single correct orchestrator for every installation. A well-operated native service or Compose deployment can be appropriate for a single host. Kubernetes adds value when the operator already has a platform and needs scheduling, policy and reconciliation. Docker explicitly documents Compose as a production option. GitLab also directs users toward simpler architectures when Kubernetes is unnecessary. [Docker production guidance][p-compose], [GitLab chart guidance][gl-chart]

| Technique | Appropriate practice | Hikyo today |
| --- | --- | --- |
| Reproducible artifacts | Pin chart versions and immutable image digests; authenticate artifacts against a documented trust policy. Tags alone can move. [Kubernetes images][p-images] | Digest-based chart templates, signed release/nightly pipelines, release admission and optional Kyverno policy exist. Stable publication and public chart retrieval are separate evidence gaps. [H01], [H02], [H03], [H10] |
| Declarative desired state | Keep reviewed environment settings in Git. Give exactly one system ownership of each mutable deployment field. Separate bootstrap credentials from the application being bootstrapped. Flux reconciles Helm actions; Argo CD renders Helm templates and owns application lifecycle itself. [Flux][p-flux], [Argo Helm][p-argo] | No shipped Flux `HelmRelease`/source recipe, Argo CD `Application`, or Kustomize deployment overlay found. Do not assume the two controllers execute identical lifecycle semantics. [H11], [H12] |
| Secure bootstrap | Reference existing Secrets; use narrow RBAC, encrypted Secret storage and restricted container privileges. Do not place root material or database credentials in plaintext Git. [Kubernetes Secret practices][p-secrets] | Existing-Secret references, non-root/read-only containers, dropped capabilities and disabled default token mounting are present. The installer must still establish external root-key and upgrade custody. [H02], [H07] |
| Deliberate networking | Configure an explicit TLS/proxy boundary and only necessary network access. NetworkPolicy needs an enforcing network plugin and policies have additive semantics. [NetworkPolicy][p-network] | Native TLS or trusted-proxy validation and configurable NetworkPolicy exist. Default application ingress is broad and egress unrestricted; the restricted example is opt-in. No chart-managed Ingress or HTTPRoute is shipped. [H02], [H06], [H13] |
| Correct health signaling | Readiness controls traffic; liveness detects when a restart is needed; startup protects lengthy initialization. A dependency outage should not cause a needless restart loop. [Kubernetes probes][p-probes] | Separate `/readyz` and `/healthz`, plus chart startup/readiness/liveness probes. Unattended preflight has a longer startup budget. Source inspected, not exercised in this research. [H07], [H09] |
| Safe schema changes | Follow the application's compatibility contract. Fence old writers, run the admitted migration path and retain tested recovery. Rolling upgrade is conditional; replacing an image is not undoing SQL. [Keycloak upgrades][kc-upgrade] | Strong existing contract. Deployment uses `Recreate`; multi-node HA still requires an explicit full stop. Generic GitOps remediation must not bypass the migration fence. [H04], [H07], [H11] |
| Recoverability | Back up all required state into another failure domain, protect recovery keys, and prove a readable restore. Define and monitor RPO/RTO. Immich's file/database split illustrates why one backup does not cover every asset. [Immich recovery][im-backup] | Encrypted exports, schedules/retention, health and restore drills exist. Off-host transport/mounts, independent root-key recovery and installation custody remain operator-owned. No native object-store uploader. [H05] |
| Availability and capacity | Treat app replicas, database HA, storage and failure domains separately. PDBs constrain eviction, not arbitrary rollout actions or involuntary failure. Configure measured resource requests and placement for the intended topology. [Disruptions][p-disruptions], [reference architectures][gl-architectures] | Application HA exists; database HA is external. Server `resources` defaults to `{}`; chart placement is a soft hostname spread under HA, not guaranteed zone separation. No built-in HPA or scheduling knobs such as nodeSelector/tolerations found. [H02], [H04], [H07] |
| Observable operation | Monitor readiness, errors, resource saturation, backup freshness and upgrade failures. Protect operational endpoints. A production example should include collector configuration and alert ownership. | Loopback-only metrics and `hikyo doctor` already exist. The chart documents a same-pod collector approach but does not package the collector, scrape objects or dashboards. External evidence-monitor tooling is a separate facility, not that chart integration. [H02], [H05], [H14] |
| Verifiable support | Publish tested versions, deployment modes, prerequisites and evidence limits. Repeat install/update/recovery checks for the exact artifact. A template being valid is not proof of a healthy installation. [Argo tested Kubernetes matrix][a-install] | Kind/chart CI scripts and an explicit public-deployment acceptance procedure exist. They were not run here; source fixtures do not prove a live cloud installation. Cloud guides explicitly disclose their local-validation boundary. [H17], [H15], [H16] |

### GitOps needs two separate deliverables

**An installation recipe** describes the pinned source/chart, namespace, values, Secrets/PVC ordering, CRD ownership, ingress, health checks and teardown rules. For Hikyo, a manually maintained exact release with controlled reconciliations is a reasonable first target, once demonstrated on the chosen controller.

**An automated release lifecycle** additionally proves writer fencing, migration, failure recovery, controller suspension/resume and ownership under drift correction. Hikyo's accepted upgrade ADR calls for a separate narrowly authorized pull-based Flux controller. Its legacy updater exits with a refusal. Neither the secrets delivery operator nor the configuration rollout executor implements that release controller. [H08], [H11], [H12]

Flux has retry/remediation strategies, including rollback or uninstall when configured; these must be reviewed for schema-changing releases. Argo CD uses Helm as a renderer, so a Helm CLI installation test alone does not validate Argo behavior. This is why adding a few YAML files would address only part of the GitOps gap. [Flux HelmRelease API][p-flux], [Argo Helm behavior][p-argo]

## What Hikyo does not offer, and what that costs users

The following absence findings apply to the pinned source and reviewed offering, not to arbitrary private deployments or unreviewed community repositories.

| Gap | Classification | User consequence | Recommended response |
| --- | --- | --- | --- |
| Published stable release | **Not available in inspected GitHub release inventory** | No stable artifact/version can anchor the supported production instructions today. | Preserve the prerelease label; complete the existing release acceptance process rather than treating nightly publication as stable support. [H01] |
| Confirmed public Helm package retrieval | **HOLD, not proven absent** | The OCI address is documented, but this investigation could not establish that a user can pull a chart version anonymously. | Verify the intended package visibility and version inventory. Publish and smoke-test a signed nightly chart if nightly Helm installs are intended. [H03], [H06] |
| Flux and Argo CD examples | **Missing packaging and validation** | Platform teams must invent sources, values ordering, ownership and migration-safe reconciliation behavior. | Deliver pinned, tested recipes in phases: installation first, then supported upgrades and failure cases. [H11], [H12] |
| General server release lifecycle operator | **Not implemented** | No controller owns the complete Hikyo server release upgrade. Existing operators solve delivery/configuration, not general installation and upgrades. | Use the accepted separate-controller boundary if automated HA/Flux upgrades become a delivery goal. Do not expand server privileges to shortcut it. [H08], [H11] |
| Complete Kubernetes reference bundle | **Pieces exist, integration missing** | Users assemble database, scratch database, TLS, Secrets, PVCs, release evidence and app chart from multiple guides. | Provide one singleton example and one manual-HA example with explicit prerequisites and observed install/recovery checks. [H02], [H04], [H06], [H09] |
| Chart-managed Ingress / Gateway API route | **Missing convenience** | Users author front-door resources and align origin, TLS, proxy trust and headers themselves. | Add optional resources or complete external-resource examples. Own a clear proxy boundary; do not install an ingress controller into every app release. [H06], [H13] |
| Packaged monitoring integration | **Missing integration, metrics exist** | No supplied same-pod collector plus protected export path, monitoring objects, dashboards or ordinary deployment alert examples were found. | Package a documented collector example and alerts without exposing the server's loopback metrics publicly. [H02], [H14] |
| Built-in database provisioning / HA | **External dependency by design** | Kubernetes users bring PostgreSQL; HA users also bring a highly available database. | Keep database ownership external, as Keycloak/Grafana/GitLab also do, but provide concrete managed-DB or database-operator reference wiring. [H04], [kc-deploy][g-ha][gl-chart] |
| Off-host backup transport and a complete K8s recovery example | **Runtime backup exists; integration incomplete** | Users must arrange remote storage, locking semantics, retention of copies and separate recovery custody. | Add an end-to-end recovery example with off-host export delivery and independent root/custody handling. [H05] |
| Broad chart placement, private-registry and scaling knobs | **Missing chart API** | No built-in nodeSelector/tolerations/configurable affinity, imagePullSecrets or HPA controls were found. Server resource requests need operator input. | Add the placement and registry controls users need; provide measured resource profiles. Evaluate HPA only for supported HA and maintenance semantics. [H02], [H07] |
| Turnkey cloud application deployment / deployment IaC modules | **Infrastructure templates and guides exist** | AWS/Azure templates provision infrastructure, not an initialized Hikyo service. GCP/DO/Hetzner are guides or host-image choices. No Hikyo Terraform/Ansible/Pulumi deployment module was found. | Finish one reproducible cloud installation before multiplying provisioning tools. Provider tests remain a separate acceptance task. [H17] |
| Turnkey native service packaging and Linux package repositories | **Binary packaging exists; service setup remains manual** | Linux packages install the binary/license only, with no daemon configuration or startup. Hosted apt/RPM repositories are explicitly not published. | Provide an intentional service/bootstrap flow if a native-server audience is a priority. Keep credential/custody generation explicit. [H01] |
| Zero-downtime schema upgrades / multi-region DR | **Not offered by current contract** | HA covers app-node loss, not maintenance without downtime or cross-region failover. | State this clearly. Treat either as separate architecture work, not a Helm setting. [H04], [H11] |

### Existing strengths to retain

Do not report these as missing: multi-node application HA, encrypted backup scheduling and restore drills, native Linux packages, multi-architecture images, NetworkPolicy, probes, a PDB, release trust checks, or a Kubernetes configuration executor. They are present in source/docs, with the boundaries above. Equally, their presence does not establish public stable distribution or a working installation in a particular cluster. [H01], [H02], [H04], [H05], [H07], [H08], [H09], [H10]

## Proposed priorities and acceptance evidence

| Priority | Deliverable | Evidence needed to call it done |
| --- | --- | --- |
| **1** | Clear artifact and deployment support matrix | State available nightly/stable artifacts, chart versions, supported install modes and tested platform versions. Prove a clean external client can retrieve the documented exact chart and matching image. |
| **1** | One complete small-production installation | Fresh singleton Kubernetes/PostgreSQL example with root/TLS Secret references, real upgrade state storage, scratch database where applicable, ingress and resource sizing. Verify first admin setup, traffic, restart and encrypted restore. |
| **1** | Flux installation recipe, then Argo CD equivalent | Test the actual controller against the exact chart/artifact. Verify bootstrap ordering, secrets, CRDs, drift, restart, uninstall retention and an intentionally refused unsafe update. Do not label it automated upgrade support yet. |
| **2** | Safe GitOps upgrades for explicitly supported topologies | Demonstrate maintenance, writer stop, backup/scratch restore, admitted migration, readiness and durable failure fencing. Prove reconcile/remediation cannot restore an incompatible old image. Preserve the existing Flux authority design. |
| **2** | Chart and operations integration | Optional ingress/HTTPRoute examples, placement/private registry controls, resource profiles, protected metrics collector wiring, dashboards/alerts and an off-host backup/recovery recipe. |
| **3** | Additional cloud IaC, package repositories, broader lifecycle operator | Choose based on actual operator demand and maintenance capacity. Each new route needs its own install, update and recovery proof. |

These priorities are an assessment of adoption friction, not a comparison score. Hikyo should support fewer deployment paths completely before adding more entry points whose operating lifecycle users must discover themselves.

## Evidence and limitations

**Repository inspection:** chart metadata/values/schema/templates/CRDs, Compose files, cloud templates/guides, release and nightly workflows, backup/HA/upgrade/configuration documentation, upgrade ADR, retired Flux adapter and chart CI scripts. Searches included Flux/Argo/Kustomize resources, ingress/HTTPRoute, monitoring objects, HPA, Terraform/Ansible/Pulumi, registry controls and placement settings. No implementation was changed and no tests were needed to establish these source inventory facts.

**Live distribution observations, 31 October 2026 around 07:37 UTC:** paginated GitHub release inventory contained no non-draft stable release. The latest visible listed prerelease was `v0.0.1-nightly.20261029.99.gd0224221`, published `2026-10-29T08:56:29Z`; its asset metadata included six platform archives, Linux packages, macOS DMGs and release/provenance documents. Asset signatures were not verified in this task. Source capabilities at the pinned checkout may postdate that nightly; this inventory is not proof that the observed artifact contains every inspected feature. [Release inventory](https://github.com/Hikyo-Org/Hikyo/releases), [observed prerelease](https://github.com/Hikyo-Org/Hikyo/releases/tag/v0.0.1-nightly.20261029.99.gd0224221)

An anonymous registry metadata attempt for `ghcr.io/hikyo-org/charts/hikyo` returned HTTP 403 during the token/list flow. No chart was downloaded. This establishes only that this access attempt failed; it does not prove a package is absent, private, unpublished or inaccessible to every client. The release workflow contains chart packaging/publishing; the inspected nightly workflow does not contain a corresponding Helm packaging step. [H03], [H18]

**NOTRUN:** competitor deployment tests, clean-client chart install, current hosted CI, Kubernetes/runtime acceptance, provider-account deployment, backup restore exercises, signature verification and performance/availability measurements. Third-party support tiers and moving documentation can change; check the selected release before acting.

## Sources

Hikyo links below pin the inspected source rather than moving `main`. External sources were inspected on the research date; their ownership and maturity labels are retained in the comparison. References to unimplemented design describe intended constraints, not shipped capability.

- **[H01]:** installation, packages and artifact availability.
- **[H02]:** chart values, dependency and operating surface.
- **[H03]:** stable release chart packaging; compare nightly workflow for publication scope.
- **[H04]:** application-tier HA, full-stop upgrades and boundaries.
- **[H05]:** encrypted backups, schedules, off-host storage and separate recovery material.
- **[H06]:** Kubernetes installation guidance.
- **[H07]:** chart runtime, security, probes, placement and update strategy.
- **[H08]:** enrolled Kubernetes configuration rollout executor.
- **[H09]:** unattended container upgrade scope and custody.
- **[H10]:** optional image admission policy.
- **[H11]:** signed-upgrade governance and separate Flux-controller design.
- **[H12]:** retired Flux adapter.
- **[H13]:** restricted-network example.
- **[H14]:** observability and self-hosting baseline.
- **[H15]:** chart/Kind acceptance script.
- **[H16]:** public deployment acceptance evidence classes.
- **[H17]:** cloud templates, infrastructure-only activation and provider evidence boundaries.
- **[H18]:** nightly publication workflow.

[H17]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/docs/site/src/content/docs/docs/cloud-deployment.mdx
[H18]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/.github/workflows/nightly.yml
[H01]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/docs/site/src/content/docs/docs/installation.mdx
[H02]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/chart/hikyo/values.yaml
[H03]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/.github/workflows/release.yml#L595
[H04]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/docs/site/src/content/docs/docs/high-availability.mdx
[H05]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/docs/site/src/content/docs/docs/backup-and-restore.mdx
[H06]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/docs/site/src/content/docs/docs/kubernetes-operator.mdx
[H07]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/chart/hikyo/templates/deployment.yaml
[H08]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/docs/site/src/content/docs/docs/configuration-rollouts.mdx
[H09]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/docs/site/src/content/docs/docs/container-upgrades.mdx
[H10]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/chart/hikyo/templates/kyverno-verify-images.yaml
[H11]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/docs/adr/signed-upgrade-compatibility.md#L344
[H12]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/install/updater/hikyo-update-flux
[H13]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/chart/hikyo/examples/restricted-network.yaml
[H14]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/docs/site/src/content/docs/docs/self-hosting.mdx
[H15]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/scripts/ci/chart-kind.sh
[H16]: https://github.com/Hikyo-Org/Hikyo/blob/929ce90ff70986ef3482f68acaa51185e0ec6ce7/docs/operations/public-deployment-acceptance.md
[g-install]: https://grafana.com/docs/grafana/latest/setup-grafana/installation/
[g-docker]: https://grafana.com/docs/grafana/latest/setup-grafana/installation/docker/
[g-chart]: https://grafana.com/docs/grafana/latest/setup-grafana/installation/helm/
[g-ha]: https://grafana.com/docs/grafana/latest/setup-grafana/set-up-for-high-availability/
[g-operator]: https://grafana.github.io/grafana-operator/docs/
[gl-methods]: https://docs.gitlab.com/install/
[gl-docker]: https://docs.gitlab.com/install/docker/installation/
[gl-chart]: https://docs.gitlab.com/charts/installation/
[gl-operator]: https://docs.gitlab.com/operator/
[gl-architectures]: https://docs.gitlab.com/administration/reference_architectures/
[kc-guides]: https://www.keycloak.org/guides
[kc-install]: https://www.keycloak.org/operator/installation
[kc-deploy]: https://www.keycloak.org/operator/basic-deployment
[kc-compat]: https://www.keycloak.org/server/update-compatibility
[kc-upgrade]: https://www.keycloak.org/docs/26.8.0/upgrading/
[im-compose]: https://docs.immich.app/install/docker-compose/
[im-k8s]: https://docs.immich.app/install/kubernetes/
[im-requirements]: https://docs.immich.app/install/requirements/
[im-backup]: https://docs.immich.app/administration/backup-and-restore/
[n-host]: https://docs.n8n.io/deploy/host-n8n.md
[n-repo]: https://github.com/n8n-io/n8n-hosting
[n-chart]: https://github.com/n8n-io/n8n-hosting/blob/main/charts/n8n/README.md
[n-queue]: https://docs.n8n.io/deploy/host-n8n/configure-n8n/scaling/enable-queue-mode.md
[a-install]: https://argo-cd.readthedocs.io/en/stable/operator-manual/installation/
[a-declarative]: https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/#manage-argo-cd-using-argo-cd
[p-compose]: https://docs.docker.com/compose/how-tos/production/
[p-images]: https://kubernetes.io/docs/concepts/containers/images/
[p-flux]: https://fluxcd.io/flux/components/helm/helmreleases/
[p-argo]: https://argo-cd.readthedocs.io/en/stable/user-guide/helm/
[p-secrets]: https://kubernetes.io/docs/concepts/security/secrets-good-practices/
[p-network]: https://kubernetes.io/docs/concepts/services-networking/network-policies/
[p-probes]: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/
[p-disruptions]: https://kubernetes.io/docs/concepts/workloads/pods/disruptions/

[V1]: https://developer.hashicorp.com/vault/docs/upgrade
[V2]: https://developer.hashicorp.com/vault/docs/deploy/run-on-docker
[V3]: https://developer.hashicorp.com/vault/docs/deploy/kubernetes
[V4]: https://developer.hashicorp.com/validated-designs/vault/installation-guide/openshift/platform-integration
[V5]: https://developer.hashicorp.com/vault/tutorials/day-one-raft/raft-reference-architecture
[O1]: https://openbao.org/docs/install/
[O2]: https://openbao.org/docs/platform/k8s/helm/run/
[O3]: https://openbao.org/docs/concepts/storage/
[O4]: https://openbao.org/docs/guides/migrations/k8s-pebbledb/
[O5]: https://openbao.org/blog/flux-openbao-secrets-signatures/
[I1]: https://infisical.com/docs/self-hosting/deployment-options/native/linux-package/installation
[I2]: https://infisical.com/docs/self-hosting/deployment-options/docker-compose
[I3]: https://infisical.com/docs/self-hosting/deployment-options/kubernetes-helm
[I4]: https://infisical.com/docs/self-hosting/reference-architectures/on-prem-k8s-ha
[I5]: https://infisical.com/blog/gitops-secrets-management
[B1]: https://bitwarden.com/help/install-on-premise-linux/
[B2]: https://bitwarden.com/help/self-host-with-helm/
[B3]: https://bitwarden.com/help/licensing-on-premise/
[B4]: https://bitwarden.com/help/backup-on-premise/
[B5]: https://bitwarden.com/help/secrets-manager-kubernetes-operator/
