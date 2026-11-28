# Deployment artifacts and operating boundaries

This is the deployment entry-point inventory. Read [installation](https://hikyo.app/docs/installation/)
for verification and platform commands, [the singleton Kubernetes runbook](kubernetes-singleton.md)
for a fresh PostgreSQL installation from a nightly chart, [Flux/Argo CD recipes](kubernetes-gitops.md)
for controller-owned installation, and [manual upgrades](manual-upgrades.md)
for existing installations. Artifact availability, a valid configuration, and a
healthy installation are separate facts.

The [latest stable release](https://github.com/Hikyo-Org/Hikyo/releases/latest) is the
supported release; [Hikyo 1.0.0](https://github.com/Hikyo-Org/Hikyo/releases/tag/v1.0.0)
established the API/CLI baseline. Signed nightlies remain unsupported prereleases. Do not
describe a source checkout, nightly, template or installation example as
stable-release acceptance, and do not treat a published artifact as acceptance of
your deployment.

## Artifact matrix

| Artifact | Source and distribution | What it provides | What remains separate |
| --- | --- | --- | --- |
| Native archives | GitHub stable and signed nightly releases; Linux, macOS and Windows, each amd64/arm64 | One multicall executable with the browser UI embedded | Server configuration, keys, installation custody, service operation and backups |
| Linux packages | Stable and nightly release assets: DEB, RPM, APK and Arch packages for amd64/arm64 | Binary and license installation | No daemon service or first-admin setup; no hosted apt/RPM package repository |
| macOS distribution | Stable and signed nightly archives and DMGs; nightly publication depends on Apple acceptance | CLI executable; the DMG is not a desktop application bundle | Follow that release's notarization/verification evidence |
| OCI server image | `ghcr.io/hikyo-org/hikyo`; exact stable versions, exact nightly versions and moving `nightly` tag | Distroless, non-root Linux amd64/arm64 image consuming the verified release binaries | Pin and authenticate the digest; a pull is not installation or upgrade completion |
| Helm chart | `chart/hikyo` in source; `ghcr.io/hikyo-org/charts/hikyo` holds exact stable versions, and the nightly publisher adds exact prerelease versions | Chart version/appVersion equal the release version; exact matching image digest. Nightly charts also record their source revision | Confirm that exact chart's publication, signature and anonymous retrieval before installing |
| Compose | `install/compose/server.yaml` and `server-unattended.yaml` | Manual and fresh unattended deployment definitions | Explicit data/key/custody storage, TLS boundary, operator-owned replacement and recovery |
| Cloud infrastructure | AWS CloudFormation, Azure template, GCP/DigitalOcean/Hetzner guides | Host/infrastructure preparation in the operator's account | Application activation and provider runtime acceptance; these are not managed services |

The nightly chart publisher is a publication mechanism, not evidence that a
given nightly has a chart. Nightlies published before it landed have none, and a
nightly whose release job failed or was skipped (for example after a failed Apple
notarization) has none either. Treat a nightly chart as **HOLD** until its exact
publisher run and a clean anonymous pull succeed. Older prereleases do not
acquire charts retroactively. On 15 November 2026 the registry listed nightly
charts published from 4 November, among them
`1.1.0-nightly.20261111.117.ga8f3cf47` (see the
[published-artifact run](kubernetes-gitops.md#published-artifact-run) for what was verified).
Nightly versions are prereleases, so Helm 3.16.1 kept resolving an unpinned chart
reference to the newest stable version (then `1.0.0`) when stable and nightly charts were both present. Always pin the exact digest anyway. A successful GitHub
archive release can coexist with failed image or chart publication; consult the
individual job and artifact digest rather than assuming the whole release exists.

Nightly chart and image digests are signed under the nightly workflow's GitHub
OIDC identity and do not extend the closed `nightly/v1` archive inventory. Stable
digests are signed under `release.yml@refs/tags/vX.Y.Z` for their own release tag.
The runtime's release/route verification still owns admission. Never substitute
a chart signature for runtime recovery custody or a signed migration route.

## Memory sizing

Pre-authentication admission bounds concurrent Argon2id work to
`HIKYO_ADMISSION_BUDGET_MIB` (272 MiB by default), but the process also needs
its own working set, about 76 MiB idle and 148 MiB after one derivation. Give
the server at least 512 MiB under a container memory limit, and 1 GiB for
headroom.

When `GOMEMLIMIT` is unset, the server reads the container's cgroup memory
limit at boot and sets its Go memory limit to 80% of it, so garbage from
repeated derivations is collected before the kernel kills the process. It logs
a warning when that limit cannot hold the admission budget plus its working
set. A host service limited by a nested cgroup, such as systemd `MemoryMax=`,
is not detected: set `GOMEMLIMIT` explicitly, for example `GOMEMLIMIT=400MiB`
under a 512 MiB limit. Rate-limiting `/api/v1/auth/*` per source address at
the reverse proxy further reduces exposure.

## Runtime matrix

| Deployment | Datastore and topology | Upgrade contract | Evidence boundary |
| --- | --- | --- | --- |
| Native/systemd nightly | Existing documented Linux/systemd/SQLite path | Verified local coordinator under its existing consent/custody contract | Do not infer general PostgreSQL, non-systemd or provider support |
| Manual native / Compose | Explicit SQLite or PostgreSQL configuration | Independently prepared signed bundle/custody, stopped writers and verified migration | The operator owns service orchestration and recovery |
| Fresh unattended Docker | One container, SQLite, persistent data/root/custody | Initial opt-in on an empty installation; verified backup and scratch restore before migration | External image replacement is separately owned; no Docker socket in the app |
| Fresh manual Kubernetes | One non-HA PostgreSQL server; public-bundle and private-state claims | Explicit signed bundle/operator pin and shared installation authority | The complete singleton example follows this path with a nightly chart; future upgrades remain manual |
| Fresh unattended Kubernetes | One non-HA server, PostgreSQL plus a separate empty scratch database | `Recreate`, initial opt-in and persistent local recovery custody | Runtime upgrade path exists; separate operator-command bootstrap gap below; not an enrollment path for existing manual data |
| Multi-node application HA | Shared root authority and externally HA PostgreSQL | Full stop of all writers, one verified migrator, exact-target boot | App-node availability does not provide database HA, zero-downtime schema upgrades or multi-region DR |
| Kubernetes configuration executor | Narrowly enrolled singleton bootstrap-source changes | Existing administrator-approved configuration protocol | Not a general server release updater; separate from secrets delivery |
| Secrets delivery operator | `HikyoInstance` / `HikyoSecret` to native Secrets | Reconciles delivery and permitted consumer rollouts | Does not install or upgrade the Hikyo server |
| Flux server installation | Exact-digest OCIRepository and namespaced HelmRelease/RBAC; generated after chart/image verification; the stable 1.0.0 chart is refused | Suspended by default; explicit activation, no rollback/uninstall remediation; manual full-stop migration | Controller mechanics and actual application/migration acceptance are separate |
| Argo CD server installation | Exact-digest generic OCI Application and restricted AppProject; generated after chart/image verification; the stable 1.0.0 chart is refused | Manual sync, no automatic self-heal/pruning; manual full-stop migration | Chart rendering is Argo-owned, not a Helm release; no built-in Cosign plugin is added |

The GitOps generator accepts stable and nightly charts, and the singleton guide's
commands name the nightly publisher. The generator refuses stable 1.0.0 because that
chart predates two changes in this source:

- It does not set `HIKYO_ROOT_KEY_FILE`, and `admin` has no root-key flag. Inside a
  running 1.0.0 pod, `admin create` refuses with `no root key configured`. The chart
  has no value that could add the setting, so the unmodified chart offers no
  first-administrator route.
- It does not normalize the `helm.sh/chart` label. Flux 2.10.0 verified its signature,
  then failed the install on the invalid label `hikyo-1.0.0+<digest>`.

Stable 1.1.0 was cut from this source. On 15 November 2026 the generator, with real
Cosign and `gh` checks, authenticated its chart and image for Flux and Argo CD, and
Flux 2.10.0 and Argo CD 3.5.6 each completed a zero-replica install of that exact digest in a
disposable Kind cluster (see the [GitOps runbook](kubernetes-gitops.md#published-artifact-run)). That is
controller-mechanics evidence only. A 1.0.0 test is also recorded: an unsigned chart built from
this source and running the released 1.0.0 image installed under Flux and created its
first administrator inside the pod. That run used controllers older than the qualified
Flux 2.10.0, with signature verification removed, and a database and custody already
initialized by the 1.0.0 chart, so it was not a fresh install. No complete stable Kubernetes first-install
runbook exists. The [GitOps prerequisites](kubernetes-gitops.md#prerequisites-and-artifact-selection)
list what changes for a stable chart. To use a stable chart without controllers,
complete [stable bundle verification](https://hikyo.app/docs/installation/#verify-a-stable-bundle),
confirm the chart digest against the release notes, then install that digest with the
[packaged chart setup](https://hikyo.app/docs/kubernetes-operator/#install-the-operator)
with the [manual Kubernetes inputs](manual-upgrades.md#compose-and-kubernetes).

Kubernetes 1.36 is the existing configuration-rollout target. The repository's
Kind tests pin their Kubernetes image and chart tooling in source; that does not
establish a tested EKS/AKS/GKE or every-distribution matrix. Record the actual
Kubernetes, Helm, CSI and CNI versions used by an installation and retain its
source/artifact/runtime evidence separately.

## Unattended operator commands

Unattended server startup resolves its authenticated bundle and operator public
key into its in-memory configuration, and records the admitted image and the
enrolled operator pin in the unattended state directory. Separately executed
`admin`, `backup` and `restore` processes (`docker exec`, `kubectl exec`) read
those records back and pass the same signed-release and custody gate, so they
need only the environment the server already has: `HIKYO_DB`, the root key path
(set by the chart and the Compose example) and the unattended state directory.
They never enroll, migrate, take the coordinator lock or start an updater, and
refuse a missing, malformed, stale or replaced record, another release, or an
unfinished upgrade. See [local operator
commands](unattended-container-upgrades.md#local-operator-commands).

Real-container acceptance (`scripts/ci/unattended-operator.sh`) covers the first
administrator, credential establishment, diagnostics, encrypted backup and the
readable restore drill in a distroless SQLite container and a singleton
PostgreSQL container through Docker. The kind singleton run (`unattended-kind.sh`)
repeats `admin create`, `backup keygen`, `backup export`, `restore status` and
`admin config status` through `kubectl exec` in the admitted pod; credential
establishment and the restore drill are covered only through Docker. The complete
singleton example still documents manual custody; choosing unattended custody
for a fresh Kubernetes install remains an operator decision. Do not bypass
release admission, invent installation pins, relaunch an upgrade coordinator
merely to run an admin command, or copy a custody journal into a different
authority directory.

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

The [Flux and Argo CD recipes](kubernetes-gitops.md) now provide controller
installation configurations and an explicit manual migration procedure. A
general automated migration coordinator, broader populated upgrade/recovery
qualification, and packaged monitoring, off-host backup, placement and
private-registry integration remain separate work. The broader update
initiative remains [#1068](https://github.com/Hikyo-Org/Hikyo/issues/1068), under
the [signed-upgrade contract](../adr/signed-upgrade-compatibility.md).
