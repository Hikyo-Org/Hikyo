# Deployment artifacts and operating boundaries

This is the deployment entry-point inventory. Read [installation](https://hikyo.app/docs/installation/)
for verification and platform commands, [the singleton Kubernetes runbook](kubernetes-singleton.md)
for a fresh PostgreSQL installation, and [manual upgrades](manual-upgrades.md) for
existing installations. Artifact availability, a valid configuration, and a
healthy installation are separate facts.

Hikyo remains in `0.x` development. Signed nightlies are prereleases; no stable
release was observed in the 31 October 2026 inventory. Do not describe a source
checkout, nightly, template or installation example as a supported stable release.

## Artifact matrix

| Artifact | Source and distribution | What it provides | What remains separate |
| --- | --- | --- | --- |
| Native archives | GitHub signed nightly releases; Linux, macOS and Windows, each amd64/arm64 | One multicall executable with the browser UI embedded | Server configuration, keys, installation custody, service operation and backups |
| Linux packages | Nightly release assets: DEB, RPM, APK and Arch packages for amd64/arm64 | Binary and license installation | No daemon service or first-admin setup; no hosted apt/RPM package repository |
| macOS distribution | Signed nightly archives and DMGs; publication depends on Apple acceptance | CLI executable; the DMG is not a desktop application bundle | Follow that release's notarization/verification evidence |
| OCI server image | `ghcr.io/hikyo-org/hikyo`; exact nightly version and moving `nightly` tag | Distroless, non-root Linux amd64/arm64 image consuming the verified release binaries | Pin and authenticate the digest; a pull is not installation or upgrade completion |
| Helm chart | `chart/hikyo` in source; nightly publisher targets `ghcr.io/hikyo-org/charts/hikyo` | Chart version/appVersion equal the nightly version; exact matching image digest and source revision | Confirm that exact chart's publication, signature and anonymous retrieval before installing |
| Compose | `install/compose/server.yaml` and `server-unattended.yaml` | Manual and fresh unattended deployment definitions | Explicit data/key/custody storage, TLS boundary, operator-owned replacement and recovery |
| Cloud infrastructure | AWS CloudFormation, Azure template, GCP/DigitalOcean/Hetzner guides | Host/infrastructure preparation in the operator's account | Application activation and provider runtime acceptance; these are not managed services |

The nightly chart publisher added by this deployment slice is a publication
mechanism, not evidence that an existing nightly has a chart. The original
anonymous chart-registry inspection returned HTTP 403. Treat public retrieval
as **HOLD** until the exact publisher run and a clean anonymous pull succeed.
Older prereleases do not acquire charts retroactively. A successful GitHub
archive release can coexist with failed image or chart publication; consult the
individual job and artifact digest rather than assuming the whole release exists.

The chart and image have their own digest signatures under the nightly workflow's
GitHub OIDC identity. They do not extend the closed `nightly/v1` archive inventory.
The runtime's release/route verification still owns admission. Never substitute
a chart signature for runtime recovery custody or a signed migration route.

## Runtime matrix

| Deployment | Datastore and topology | Upgrade contract | Evidence boundary |
| --- | --- | --- | --- |
| Native/systemd nightly | Existing documented Linux/systemd/SQLite path | Verified local coordinator under its existing consent/custody contract | Do not infer general PostgreSQL, non-systemd or provider support |
| Manual native / Compose | Explicit SQLite or PostgreSQL configuration | Independently prepared signed bundle/custody, stopped writers and verified migration | The operator owns service orchestration and recovery |
| Fresh unattended Docker | One container, SQLite, persistent data/root/custody | Initial opt-in on an empty installation; verified backup and scratch restore before migration | External image replacement is separately owned; no Docker socket in the app |
| Fresh manual Kubernetes | One non-HA PostgreSQL server; public-bundle and private-state claims | Explicit signed bundle/operator pin and shared installation authority | The complete singleton example follows this path; future upgrades remain manual |
| Fresh unattended Kubernetes | One non-HA server, PostgreSQL plus a separate empty scratch database | `Recreate`, initial opt-in and persistent local recovery custody | Runtime upgrade path exists; separate operator-command bootstrap gap below; not an enrollment path for existing manual data |
| Multi-node application HA | Shared root authority and externally HA PostgreSQL | Full stop of all writers, one verified migrator, exact-target boot | App-node availability does not provide database HA, zero-downtime schema upgrades or multi-region DR |
| Kubernetes configuration executor | Narrowly enrolled singleton bootstrap-source changes | Existing administrator-approved configuration protocol | Not a general server release updater; separate from secrets delivery |
| Secrets delivery operator | `HikyoInstance` / `HikyoSecret` to native Secrets | Reconciles delivery and permitted consumer rollouts | Does not install or upgrade the Hikyo server |
| Flux / Argo CD server lifecycle | No complete shipped controller recipe in this slice | Follow the signed-upgrade ADR before automating release changes | Helm compatibility alone is not migration-safe GitOps acceptance |

Kubernetes 1.36 is the existing configuration-rollout target. The repository's
Kind tests pin their Kubernetes image and chart tooling in source; that does not
establish a tested EKS/AKS/GKE or every-distribution matrix. Record the actual
Kubernetes, Helm, CSI and CNI versions used by an installation and retain its
source/artifact/runtime evidence separately.

## Unattended operator-command gap

Unattended server startup resolves its authenticated bundle and operator public
key into its in-memory configuration. Separately executed `admin`, `backup export`
and `restore drill` processes do not inherit those resolved paths. These commands
require the actual public bundle, operator pin and installed
custody; `HIKYO_DB` and the root key alone are insufficient. The chart now exposes
the staged root-key path for operator commands, but that does not resolve the
other unattended inputs automatically.

The complete singleton example therefore uses the manual-custody path. A safe
admitted-custody resolver and end-to-end operator-command checks are still needed
before advertising a complete fresh unattended Kubernetes operating experience.
Do not bypass release admission, invent installation pins, relaunch an upgrade
coordinator merely to run an admin command, or copy a custody journal into a
different authority directory. Runtime unattended upgrade coverage remains
separate from this operator-command gap. The repair and required runtime
acceptance are tracked in [#1112](https://github.com/Hikyo-Org/Hikyo/issues/1112).

## Installation acceptance record

For each installation, retain the exact source commit, chart version/digest,
image index and selected platform digest, signature-verifier result, reviewed
nonsecret values, Kubernetes/Helm/CSI/CNI versions, storage qualification, initial
readiness/admin result and encrypted restore-drill receipt. Keep credentials,
root keys and private recovery material outside this record and outside Git.

Use [public deployment acceptance](public-deployment-acceptance.md) when checking
a real installation through ingress. Source tests and local templates do not
replace those observations. Existing live installations need their own
operational authorization; this inventory is not permission to upgrade them.

The next deployment slices are controller-tested Flux and Argo CD installation
recipes, their separately proven migration lifecycle, and packaged monitoring,
off-host backup, placement and private-registry integration. The broader update
initiative remains [#1068](https://github.com/Hikyo-Org/Hikyo/issues/1068), under
the [signed-upgrade contract](../adr/signed-upgrade-compatibility.md).
