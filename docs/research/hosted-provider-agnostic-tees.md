# Research: TEEs for a provider-agnostic attested unsealer

Date: 2026-11-12. Ticket: [Hikyo-Org/Hikyo#1343](https://github.com/Hikyo-Org/Hikyo/issues/1343). Status: research only. This note maps which TEEs the attested unsealer can target, where they are offered, and what a client must verify on each. It proposes one shared attestation abstraction and recommends the TEE families GA needs. It makes no decision.

Sibling note: [`hosted-client-side-and-confidential-computing.md`](https://github.com/Hikyo-Org/Hikyo/blob/research/hosted-client-side-and-confidential-computing/docs/research/hosted-client-side-and-confidential-computing.md) (ticket #1307, on its research branch), referred to below as "the sibling note". It already covers Nitro Enclaves, Nitro Sandboxes, NitroTPM, AWS SEV-SNP basics and surcharge, Google Cloud Confidential VM, GKE and Confidential Space with surcharge prices, Azure CVM SKUs and Secure Key Release, and TEE.fail and RMPocalypse. This note does not repeat those facts. It extends them where this ticket needs more.

All external sources were fetched on 2026-11-12.

## The question

"Which TEEs can a provider-agnostic attested unsealer target, and where are they offered? Cover AMD SEV-SNP, Intel TDX, AWS Nitro and Confidential Containers (CNCF) on Kubernetes. Cover AWS, Google Cloud, Azure and EU providers such as OVHcloud, Scaleway, Hetzner and IONOS, plus a self-hoster's own hardware. For each provider, give: availability by region; whether attestation is rooted at the CPU vendor or the cloud; what a client must verify; the cost; what the host still controls. Then propose one attestation abstraction that the CLI, the Kubernetes operator and the unsealer can share, and say which TEE families GA needs."

The locked design (owner, 2026-11-12) is the starting point. The unsealer is a small separate process in a TEE. Clients verify its attestation themselves. They accept only measurements listed in a public transparency log after a waiting period, and at least two at once during rolling upgrades. The holder secret is sealed to a public key bound into the evidence. The image has no shell. Hosting is undecided. TEEs rooted at the CPU vendor (SEV-SNP, TDX) are the base. Nitro is one option. Self-hosters get the same set.

## Short answer

- **Every CPU-signed report proves less than it seems in a public cloud.** The question that decides fit is what the AMD- or Intel-signed measurement actually covers. Four groups emerge:
  1. **Customer-supplied boot** (self-host, IONOS, bare metal at Scaleway and others, CoCo on bare metal). The measurement covers firmware, kernel, initrd and command line. Hikyo builds them, except on IONOS, which supplies a published OVMF build. Hikyo can compute it and put it in the transparency log. The log pins the whole launch measurement, and the root is the CPU vendor alone.
  2. **Provider firmware, OS from disk** (AWS SEV-SNP, Google Cloud SEV-SNP). The SNP measurement covers only the provider's UEFI firmware. The unsealer's identity has to come from a TPM that the cloud runs (NitroTPM on AWS, a hypervisor-emulated vTPM on Google Cloud). The application measurement is then rooted at the cloud, not the CPU vendor.
  3. **Paravisor** (Azure SEV-SNP and TDX). Microsoft's paravisor is in the launch measurement and hosts the vTPM inside the TEE. Guest measurements chain to AMD or Intel, but only through a Microsoft binary that is accepted by signer, not by a hash Hikyo can reproduce.
  4. **TDX with runtime registers** (Google Cloud TDX). MRTD covers Google's firmware. Google publishes MRTD endorsement objects, so the platform layer can accept MRTD by endorsement. The RTMRs cover the boot chain, are signed by Intel, and are pinned by the log. *Unverified: the endorsement signature chain and current C3 and C4 coverage.*
- **Where SEV-SNP and TDX are offered.**
  - **SEV-SNP** is the widest: self-host on EPYC Milan or later, IONOS (Frankfurt), Scaleway Elastic Metal (Paris and Amsterdam confirmed, Warsaw unverified), Google Cloud (2 EU regions), Azure (many EU regions) and AWS (Ireland and Ohio on shared tenancy, any commercial region on Dedicated Hosts).
  - **TDX** is offered by Google Cloud (4 EU regions across C3 and C4) and Azure (West Europe, North Europe in the price list), and on self-host 5th Gen Xeon or Xeon 6.
  - **No EU provider** was found offering TDX. None of OVHcloud, Hetzner, STACKIT, Open Telekom Cloud, Exoscale or UpCloud documents a managed SEV-SNP confidential VM.
- **Kubernetes.** A node-level confidential VM (Confidential GKE Nodes, AKS CVM node pools) does not separate the unsealer from the cluster admin. The unsealer needs its own TEE boundary: a Confidential Containers pod with an agent policy that denies exec, a Nitro Sandbox, or a standalone confidential VM. CoCo is a CNCF incubating project with releases every six weeks. Microsoft announced the retirement of AKS's managed CoCo preview for March 2026.
- **Cost** is small next to engineering. Shared-tenancy SEV-SNP on AWS adds about $14 a month to an `m6a.xlarge` in Ireland. Azure `DC4as_v6` costs 10 percent more than `D4as_v6` in West Europe. Google Cloud figures are in the sibling note. IONOS and Scaleway publish no confidential-specific price.
- **Abstraction.** One Go package with a `Verifier` per evidence format (SNP report, TDX quote, Nitro COSE_Sign1, TPM quote chained to a hardware report). Each returns the same `Claims`. Policy has two layers: a platform layer (firmware or paravisor, accepted by reproducible hash or vendor endorsement, plus minimum TCB) and a workload layer (values Hikyo logs per release). The unsealer ships its own collateral, so clients verify offline against pinned roots. Reuse `google/go-sev-guest` and `google/go-tdx-guest` (Apache-2.0).
- **Recommendation.** GA needs **AMD SEV-SNP** and **Intel TDX**, both through raw hardware evidence with a workload measurement Hikyo computes. The two families serve different targets. SEV-SNP is the floor for self-hosters and IONOS. TDX is the raw path for a hosted multi-region service, on Google Cloud. Which family weighs more for the hosted service depends on the hosting choice, which is the owner's. Nitro and the TPM-chained cloud profiles (Azure paravisor, AWS NitroTPM, Google vTPM) are add-ons on demand once a hosting provider is chosen. The reasoning is in [Which TEE families GA needs](#which-tee-families-ga-needs).
- **Hosted multi-region placements are asymmetric.** Hyperscaler SEV-SNP does not count: AWS and Google Cloud SNP are group 2, and Azure is group 3. With only raw SNP and TDX, each family has one multi-region hosted placement:
  - SEV-SNP: bare metal with CoCo at Scaleway Elastic Metal. SNP-capable Milan models are in Paris and Amsterdam. Warsaw has only EPYC 8004 models, where SNP is unverified. Hosted multi-region SNP rests on this one provider.
  - TDX: Google Cloud C3 and C4 in four EU regions. Google publishes MRTD endorsements. Their signature chain and current coverage were not checked.

  IONOS has one region, which suits self-hosters but not a multi-region hosted service. AWS shared-tenancy SNP has one EU region. If the owner picks AWS or Azure for hosting, the matching add-on (Nitro, or the Azure paravisor profile) becomes a GA requirement rather than an option.

## The discriminator: what the signed measurement covers

| Group | Platforms | Launch measurement covers | Who supplies it | Where the unsealer's identity comes from | Root for the application measurement |
|---|---|---|---|---|---|
| 1 Customer-supplied boot | Self-host QEMU/KVM, CoCo `kata-qemu-snp` and `kata-qemu-tdx`, IONOS, bare metal at Scaleway; OVHcloud and Hetzner bare metal if SNP can be enabled (unverified) | SNP: firmware, kernel, initrd, command line, vCPU layout. TDX: MRTD for firmware, RTMRs for the boot chain | Hikyo or the self-hoster; on IONOS, IONOS supplies the OVMF | Launch measurement plus HOST_DATA (SNP) or MRCONFIGID (TDX) | CPU vendor |
| 2 Provider firmware, OS from disk | AWS SEV-SNP, Google Cloud SEV-SNP | Provider UEFI firmware only | AWS ([reproducible](https://github.com/aws/uefi)) or Google ([signed endorsements](https://github.com/google/gce-tcb-verifier)) | NitroTPM PCRs (AWS) or hypervisor vTPM PCRs (Google) | Cloud |
| 3 Paravisor | Azure DCas/ECas (SNP), DCes/ECes (TDX) | Microsoft paravisor (HCL), which hosts the vTPM | Microsoft | vTPM PCRs, with the vTPM key vouched for by the hardware report | CPU vendor, through a Microsoft binary |
| 4 TDX with RTMRs | Google Cloud C3, C4 | MRTD: Google firmware. RTMRs: boot chain | Google (firmware), Hikyo (boot chain) | RTMR values | CPU vendor for the RTMRs. MRTD accepted by Google's endorsement; endorsement objects are published. *Unverified: endorsement signature chain and current C3/C4 coverage* |

What this means for the transparency log. In group 1 the log pins the whole launch measurement. In group 4 the log pins the RTMRs, and the platform layer accepts MRTD by Google's endorsement. In groups 2 and 3 the log cannot pin the launch measurement, because the provider changes its firmware or paravisor without a Hikyo release. There the client must accept the provider's part by endorsement or by a provider-published hash, and pin only Hikyo's part through the log. Groups 1 and 4 keep the unsealer's own measurements in CPU-signed evidence. Group 2 puts them under a cloud TPM, and group 3 under a vTPM inside a Microsoft binary accepted by signer, whose reproducibility has not been shown.

## Comparison by provider

Regions name EU regions first. "Price list" means the region has a confidential SKU in the provider's own price API; that is not a capacity guarantee.

| Provider, offering | TEE | Regions (EU first) | Attestation root | Client verifies | Cost (4 vCPU, 16 GiB unless stated) | Host still controls |
|---|---|---|---|---|---|---|
| AWS EC2, shared tenancy | SEV-SNP | Europe (Ireland), US East (Ohio) only | AMD ARK, but the report is signed by VLEK, "issued by AMD for AWS" | SNP report, VLEK chain to AMD ARK, AWS UEFI hash (reproducible), plus NitroTPM quote for the OS (AWS-rooted) | `m6a.xlarge` Ireland $0.1926/h + 10% = **+$14.06 a month** | Firmware build, boot disk, host maintenance (14 days' notice, manual restart), I/O |
| AWS EC2, Dedicated Hosts | SEV-SNP | "any commercial AWS Region" | AMD ARK, VCEK (per chip) | As above, with VCEK from AMD KDS | No SNP surcharge. *Dedicated Host price not fetched* | As above. Firmware updates only when a host is reallocated |
| AWS Nitro Enclaves, Nitro Sandboxes | Nitro | Sandboxes: all commercial regions (sibling note) | AWS Nitro root (hypervisor) | COSE_Sign1, chain to Nitro root, PCR0 to 2 | None (sibling note) | Parent instance: image choice, I/O, scheduling |
| Google Cloud Confidential VM | SEV-SNP (N2D) | europe-west3, europe-west4, asia-southeast1, us-central1 | AMD ARK, VCEK; firmware endorsed by Google | SNP report, VCEK chain, Google launch endorsement, plus vTPM quote for the OS (hypervisor-emulated) | Sibling note: $12.34 a month surcharge | Firmware, vTPM, boot disk, maintenance (no live migration) |
| Google Cloud Confidential VM | TDX (C3, C4) | europe-west1 (C4), europe-west3, europe-west4, europe-west9 (C3); 8 non-EU regions | Intel SGX root via PCK chain | TDX quote, PCK chain and Intel collateral, MRTD (Google firmware), RTMRs | Sibling note: $15.24 a month surcharge | Firmware, boot disk, maintenance (no live migration) |
| Azure confidential VMs | SEV-SNP (DCas, ECas) | v6 price list: West Europe, North Europe, Germany West Central, France Central, Italy North, Poland Central, Spain Central, Sweden Central, Switzerland North, Norway East, Belgium Central and others | AMD ARK, VCEK; paravisor accepted by Microsoft signer | HCL report from vTPM, SNP signature, IDKeyDigest, vTPM quote | `DC4as_v6` $0.242/h vs `D4as_v6` $0.220/h in West Europe: **+10%, $16.06 a month** | Paravisor binary, firmware, maintenance, I/O |
| Azure confidential VMs | TDX (DCes, ECes) | v5 price list: West Europe, North Europe, Central US, East US 2. v6: West Europe, East US, West US, West US 3 | Intel, through the paravisor | TDX quote via Microsoft tooling. *Unverified: raw quote path* | `DC4es_v5` $0.230/h vs `D4s_v5` $0.230/h in West Europe: **no difference**. v6: +9.9% (sibling note) | As above |
| IONOS Cloud Confidential VM | SEV-SNP | Frankfurt `de/fra/2` only; "currently available only in EU data centers" | AMD ARK (VCEK or VLEK, not stated which) | SNP report and launch measurement over OVMF (IONOS-built; customer-built unverified), kernel, initrd and command line | Not public: "Contact your account manager" | Scheduling, maintenance downtime, API lifecycle, I/O |
| Scaleway Elastic Metal | SEV-SNP on bare metal | Milan models EM-I210E and EM-I215E: fr-par-1, fr-par-2, nl-ams-1. Warsaw has EPYC 8004 models only (SNP unverified) | AMD ARK, VCEK | Same as self-host. The customer is the hypervisor | *Not fetched (price page blocked)* | BIOS settings and firmware updates (customer may not change either), physical access |
| OVHcloud bare metal | SEV named, SNP not documented | Scale 2026 and Advance 2026 in Europe, Canada, US, APAC | Would be AMD ARK if SNP is enabled | Same as self-host | Advance-5 "from $467/month" (USD, `/en/` page) | BIOS, firmware, physical access |
| Hetzner dedicated | None documented | n/a | n/a | n/a | n/a | n/a |
| STACKIT Confidential Server | "AMD SEV" (SEV-ES per Edgeless) | "limited extent and on request" | No CPU attestation documented | n/a | *Not fetched* | n/a |
| Self-hosted hardware | SEV-SNP (EPYC 7003 and later), TDX (5th Gen Xeon, Xeon 6) | Owner's sites | AMD ARK or Intel SGX root | As group 1 | Hardware only | The self-hoster is the host: firmware, microcode, BIOS, physical access |
| CoCo on Kubernetes | SEV-SNP, TDX (bare metal); cloud TEEs (peer pods) | Wherever the nodes run | As the underlying TEE | As the underlying TEE, plus init-data digest | As the nodes | Kubelet, control plane, image pull, scheduling; agent policy limits exec |

Sources for the table are in the per-provider sections below.

## Per provider

### AWS

The sibling note covers Nitro Enclaves, Nitro Sandboxes, NitroTPM, the SEV-SNP surcharge and the KMS model. Additions for this ticket:

- **Regions and tenancy.** Shared tenancy SEV-SNP runs only in "US East (Ohio) or Europe (Ireland)". Dedicated Hosts support SEV-SNP in "any commercial AWS Region". Only `m6a`, `c6a` and `r6a` up to fixed sizes qualify ([AMD SEV-SNP for EC2](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/sev-snp.html)). AWS documents no Intel TDX offering in EC2. *Unverified: absence of TDX is inferred from the EC2 user guide, not stated.*
- **Attestation root.** The report "is signed with a VCEK signature (on Dedicated Hosts) or a VLEK signature (on shared tenancy) that chains back to an AMD root of trust". The VLEK "is issued by AMD for AWS" ([Attest with SEV-SNP](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/snp-attestation.html)). On shared tenancy the guest requests the certificates "from host memory". The VLEK chain is fetched from `kdsintf.amd.com/vlek/v1/Milan/cert_chain` (same page).
  - So a VLEK-signed report proves "AMD hardware operated by AWS", not a specific chip. The client still verifies to AMD's root.
- **What the measurement covers.** The launch measurement is "a cryptographic hash of the initial boot state of an instance". AWS says it validates "the initial boot code" ([AMD SEV-SNP for EC2](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/sev-snp.html)). That boot code is the OVMF firmware, which "finds and runs the boot loader provided in the AMI" (same page).
  - AWS publishes the firmware source with Nix builds for "reproducible builds of the UEFI binaries", so customers "can match their running UEFI firmware with the binaries released here". Measurements come from the third-party `sev-snp-measure` tool ([aws/uefi](https://github.com/aws/uefi), BSD-2-Clause-Patent).
  - The AMI's boot loader, kernel and the unsealer are therefore not in the SNP measurement. *Inferred from the two pages: a modified AMI produces the same SNP measurement.*
  - Edgeless Constellation uses NitroTPM PCRs for the OS on AWS SEV-SNP, and lists the firmware PCRs as measured by AWS and not reproducible ([Constellation attestation](https://docs.edgeless.systems/constellation/architecture/attestation)). The Constellation repository was archived in January 2026, so this may be stale. NitroTPM lists M6a, C6a and R6a as supported instance families and names no SEV-SNP exclusion ([NitroTPM requirements](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/enable-nitrotpm-prerequisites.html)). *Unverified: that NitroTPM and SEV-SNP run together on one instance today.*
- **Consequence.** On AWS SEV-SNP, the unsealer's identity is rooted at AWS (NitroTPM), with SNP adding memory protection against the host. That still stops the operator. Cloud-provider cooperation is accepted residual risk under the owner's resolution in [Grilling: operator trust boundary](https://github.com/Hikyo-Org/Hikyo/issues/1312) ("Still exposed" list). It is not a CPU-vendor root for the application. On AWS, Nitro Sandboxes give a cleaner Kubernetes fit with the same AWS root (sibling note).
- **Constraints.** "Hibernation and Nitro Enclaves aren't supported" with SEV-SNP. On shared tenancy, a maintenance event comes with 14 days' notice and "You must manually stop or restart your instance". On Dedicated Hosts, "To get the latest firmware, release the existing host and allocate a new one" ([AMD SEV-SNP for EC2](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/sev-snp.html)).
- **Cost.** The EC2 on-demand price map gives `m6a.xlarge` (4 vCPU, 16 GiB) at $0.1926/h in Ireland and $0.1728/h in Ohio ([EC2 on-demand price map, Ireland](https://b0.p.awsstatic.com/pricing/2.0/meteredUnitMaps/ec2/USD/current/ec2-ondemand-without-sec-sel/EU%20(Ireland)/Linux/index.json)). The 10 percent SEV-SNP fee adds $0.01926/h, **$14.06 a month** in Ireland and $12.61 in Ohio, at 730 hours.

### Google Cloud

The sibling note covers machine families, Confidential GKE Nodes, Confidential Space and surcharge prices. Additions:

- **Regions.** SEV-SNP (N2D, Milan) runs in asia-southeast1, europe-west3, europe-west4 and us-central1. TDX on `c3-standard` runs in 11 regions, including europe-west3, europe-west4 and europe-west9. TDX on `c4-standard` adds europe-west1. Live migration is supported only for SEV on N2D and C3D ([Supported configurations](https://docs.cloud.google.com/confidential-computing/confidential-vm/docs/supported-configurations)). The same page warns that SEV-SNP instances "may see longer boot times and performance changes between August and November 2026" during a guest kernel migration.
- **Raw evidence is available.** For SEV-SNP and TDX the guest can request a report from the AMD Secure Processor or the Intel TDX module with go-tpm-tools v0.4.4 or later, or with `go-sev-guest` and `go-tdx-guest`. "For SEV-SNP, VCEK certificates are cached on the VM" ([Confidential VM attestation](https://docs.cloud.google.com/confidential-computing/confidential-vm/docs/attestation)).
- **SNP measurement and endorsements.** Google signs "launch endorsements", golden measurements for its OVMF builds. It "publishes its production virtual firmware binaries for transparency", and verifiers "may download the 2MiB image to inspect and reproduce the golden measurement". The root is `https://pki.goog/cloud_integrity/GCE-cc-tcb-root_1.crt`. The endorsement "is provided from the host certificate cache" and composes with `go-sev-guest` through `verify.SNPValidateFunc`. Google "does not populate and authenticate an IDBLOCK" ([gce-tcb-verifier](https://github.com/google/gce-tcb-verifier), Apache-2.0, "not an officially supported Google product").
- **OS measurement on SNP.** The vTPM is Google-managed ([Confidential VM attestation](https://docs.cloud.google.com/confidential-computing/confidential-vm/docs/attestation)). Edgeless states that on Google Cloud "this vTPM doesn't run inside the hardware-protected CVM context, but is emulated by the hypervisor" ([Constellation attestation](https://docs.edgeless.systems/constellation/architecture/attestation)). The Constellation repository was archived in January 2026, so this may be stale, and Google's own page does not say where the vTPM runs. On that basis, on SNP the unsealer's identity is rooted at Google.
- **TDX measurement.** "The initial launch component (firmware) is measured into MRTD". "The remaining boot chain is measured into RTMR registers", and users can extend RTMRs ([Confidential VM attestation](https://docs.cloud.google.com/confidential-computing/confidential-vm/docs/attestation)). On TDX the boot chain is in Intel-signed evidence.
- **TDX endorsements.** gce-tcb-verifier contains TDX endorsement code (`tdx/endorsement.go`, `tdx/mrtd_from_ovmf.go`, `extract/extracttdx`, `gcetcbendorsement/tdxpolicy.go`) ([gce-tcb-verifier `tdx`](https://github.com/google/gce-tcb-verifier/tree/main/tdx)). Its endorsement proto has a `VMTdx` message with "Expected MRTD report field values given legal configurations", one per legal configuration, keyed by `ram_gib` and `early_accept` ([endorsement.proto](https://github.com/google/gce-tcb-verifier/blob/main/proto/endorsement.proto)). `extracttdx` maps an MRTD to the object `ovmf_x64_csm/tdx/<mrtd>.binarypb`, and the public [`gce_tcb_integrity` bucket, `ovmf_x64_csm/tdx/`](https://storage.googleapis.com/storage/v1/b/gce_tcb_integrity/o?prefix=ovmf_x64_csm/tdx/) lists such objects. So MRTD depends on the VM's RAM size and early-accept setting, as the SNP measurement depends on vCPU count. The README still documents only SNP. *Unverified: the TDX endorsement signature chain, and whether current C3 and C4 firmware is covered.*
- **Google Cloud Attestation** (the token service) supports SEV and TDX on C3, not SEV-SNP, per the same page.
- **GKE.** Confidential GKE Nodes support SNP and TDX from GKE 1.32.2-gke.1297000 (Standard, COS) and 1.35.2-gke.1485000 (Autopilot). The page offers no node attestation procedure, only a configuration check ([Confidential GKE Nodes](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/confidential-gke-nodes)). The protection is per node, so the cluster admin still controls every pod on it.

### Azure

The sibling note covers SKUs, the SEV-SNP and TDX price deltas and Secure Key Release. Additions:

- **Regions.** The [Azure Retail Prices API](https://prices.azure.com/api/retail/prices) lists `Standard_DC4as_v6` (SEV-SNP) in 58 regions, including West Europe, North Europe, Germany West Central, Germany North, France Central, Italy North, Poland Central, Spain Central, Sweden Central, Switzerland North, Norway East and Belgium Central. `Standard_DC4as_v5` is listed in 18 regions, including West Europe, North Europe, Germany West Central, Italy North, Switzerland North and UK South. TDX `Standard_DC4es_v5` is listed in West Europe, North Europe, Central US and East US 2. `Standard_DC4es_v6` is listed in West Europe, East US, West US and West US 3. A price row is not proof of capacity.
- **Paravisor and vTPM.** "The vTPM runs inside the VM in a hardware-based protected memory region." "Since the vTPM runs within the Confidential VM, it's measured by the AMD SEV-SNP hardware" ([vTPMs in Azure CVMs](https://learn.microsoft.com/en-us/azure/confidential-computing/virtual-tpms-in-azure-confidential-vm)). Microsoft's open-source paravisor, OpenHCL, "runs within the guest partition" and "hosts a virtual TPM (vTPM)" ([OpenHCL architecture](https://openvmm.dev/guide/reference/architecture/openhcl.html)). *Unverified: whether the paravisor in production CVM SKUs is OpenHCL, and whether its production builds are reproducible from that source.*
- **Raw SNP report.** At boot the CVM generates a SEV-SNP report "stored in a predefined nonvolatile index (NVIndex) of the vTPM". It is read with `tpm2_nvread -C o 0x01400001`, and the 1,184-byte SNP report starts at offset 32. "The report data JSON in the guest attestation report is measured and the hash is reflected in the SEV-SNP report". The JSON includes "HCLAkPub", the vTPM attestation key. VCEK certificates come from IMDS at `169.254.169.254/metadata/THIM/amd/certification` ([Leverage vTPMs in Azure CVMs](https://learn.microsoft.com/en-us/azure/confidential-computing/how-to-leverage-virtual-tpms-in-azure-confidential-vms)).
- **Key binding.** The paravisor fills `report_data` with the hash of its own claims. A guest binds its own key through a TPM quote signed by that attestation key. Microsoft recommends PCR23 for user-mode measurements (same page). *Unverified: whether a guest can request a fresh SNP report with its own `report_data`. This note assumes binding on Azure is TPM-chained only.*
- **Measurement.** The SNP launch measurement covers the Microsoft paravisor. Edgeless lists Azure firmware PCRs 0 to 3 as "measured by Azure" and "not reproducible". It verifies the paravisor through the report's IDKeyDigest against a list of accepted Microsoft signing-key digests ([Constellation attestation](https://docs.edgeless.systems/constellation/architecture/attestation)).
- **TDX.** Microsoft points to a separate GitHub guide for DCesv5 and ECesv5. It uses Intel Trust Authority, which "requires registration with Intel" ([Guest attestation](https://learn.microsoft.com/en-us/azure/confidential-computing/guest-attestation-confidential-vms)). The guide's tool accepts "Intel Trust Authority" or "maa" as verifier ([TDX attestation app](https://github.com/Azure/confidential-computing-cvm-guest-attestation/blob/tdx-preview/tdx-attestation-app/ATTEST.md)). *Unverified: how a guest obtains a raw TD quote on Azure, and what MRTD and RTMRs cover there.*
- **Price additions.** West Europe, Linux, on demand: `DC4as_v6` $0.242/h vs `D4as_v6` $0.220/h, +10%. North Europe: $0.223 vs $0.202, +10.4%. `DC4es_v5` $0.230/h vs `D4s_v5` $0.230/h, no difference ([Azure Retail Prices API](https://prices.azure.com/api/retail/prices)). *Not verified as like-for-like CPU generations.*
- **AKS Confidential Containers** was a preview with a `kata-cc` runtime. Microsoft announced its retirement: "Existing node pools using KataCcIsolation will continue to operate until March 2, 2026" ([Azure update 502044](https://azure.microsoft.com/updates?id=502044)). *Unverified: that the shutdown happened on schedule.* Do not plan on it.

### IONOS Cloud

IONOS is the one EU provider found with a managed SEV-SNP confidential VM.

- **Availability.** "Confidential VM is currently available only in EU data centers provisioned with AMD SEV-SNP-capable hardware". The documented region is Frankfurt `de/fra/2` ([IONOS Confidential VM FAQ](https://docs.ionos.com/cloud/compute-services/compute-engine/confidential-computing-vm/confidential-vm-faq)). No GA or preview status is stated. The CPU generation is given only as "AMD EPYC server processors".
- **Measurement.** It is a SHA-384 digest over OVMF, kernel, initrd and kernel parameters. "The computation is deterministic." "The VM starts with user-provided artifacts, including the kernel, `initrd`, OVMF firmware, and kernel parameters." The page says AMD holds the signing key, through VCEK or VLEK and AMD's Key Distribution Service. It lists "OVMF firmware build transparency and provenance" under what IONOS controls ([Security and trust model](https://docs.ionos.com/cloud/compute-services/compute-engine/confidential-computing-vm/overview/security-and-trust-model)), and the [FAQ](https://docs.ionos.com/cloud/compute-services/compute-engine/confidential-computing-vm/confidential-vm-faq) points to IONOS's OVMF builds. The "user-provided" wording and these two statements conflict. This note reads them as: the customer passes the firmware file at launch, and IONOS supplies the build to use. *Unverified: whether a customer-built OVMF is accepted.* This is group 1: Hikyo can compute the full measurement over IONOS's published OVMF and its own kernel, initrd and command line.
- **Tooling.** IONOS publishes SNPGuard, an "attestation service and key broker", in Rust under Apache-2.0. It requires OVMF built with `SNP_KERNEL_HASHES` and produces ID and Auth Blocks ([ionos-cloud/snp-guard](https://github.com/ionos-cloud/snp-guard)). It has releases v1 to v4, published 2026-07-29 to 2026-08-04 ([releases](https://github.com/ionos-cloud/snp-guard/releases), [tutorial](https://docs.ionos.com/cloud/tutorials/compute-services/confidential-computing-vm/deploy-confidential-vm-with-snpguard)).
- **Host still controls.** Host hardware and SEV-SNP enablement, the hypervisor, and API lifecycle. The host sees the VM's existence, resources and API actions. Limitations: no live migration ("Every host-level maintenance event causes downtime"), fixed vCPU count, no hot-plug, no cloud-init, SMT always on ([Limitations](https://docs.ionos.com/cloud/compute-services/compute-engine/confidential-computing-vm/overview/limitations)).
- **Kubernetes.** A Confidential VM is a whole VM, not a Kubernetes node type. *Not researched: whether IONOS Managed Kubernetes can use Confidential VM nodes.*
- **Cost.** "Priced based on your hardware configuration. Contact your account manager" ([FAQ](https://docs.ionos.com/cloud/compute-services/compute-engine/confidential-computing-vm/confidential-vm-faq)).

### Scaleway

- **Offer.** Scaleway sells no confidential VM. Its Elastic Metal bare metal supports SEV-SNP: "The AMD Security Stack and SEV-SNP protocol are supported on the following Elastic Metal server offers: EM-IX20E, All new AMD-based Elastic Metal offers". The page lists a default configuration with `CpuMinSevAsid: 100`. That suggests SNP is on by default, but the page does not state it. The page was validated on 2026-09-11 ([Elastic Metal security features](https://github.com/scaleway/docs-content/blob/main/pages/elastic-metal/reference-content/elastic-metal-security-features.mdx); the scaleway.com copy was behind a bot challenge).
- **Regions.** The page links EM-IX20E to the Iridium range, but EM-IX20E is not in the datasheet. The Milan models EM-I210E and EM-I215E (EPYC 7313P) are offered only in fr-par-1, fr-par-2 and nl-ams-1. Warsaw (pl-waw-2, pl-waw-3) and nl-ams-2 have no Milan models: their other parts are Rome (no SNP) and EPYC 4004/4005, and their SNP candidates are EPYC 8004 models ([Elastic Metal datasheet](https://github.com/scaleway/docs-content/blob/main/pages/elastic-metal/reference-content/elastic-metal-datasheet.mdx)). So two metros are confirmed for SNP. *Unverified: SNP on EPYC 8004, which would add Warsaw.* Some Elastic Metal models use EPYC 4004 and 4005 parts. *Unverified: SNP support on those; see self-host below.*
- **Host still controls.** The customer runs the hypervisor, so attestation is as for self-host (group 1, VCEK). But "Users are not allowed to change the BIOS settings", and "Firmware updates are managed by Scaleway" and installed "automatically upon reboot" ([Elastic Metal FAQ](https://github.com/scaleway/docs-content/blob/main/pages/elastic-metal/faq.mdx)). Scaleway therefore controls the SNP firmware and microcode level. The reported TCB in each report exposes that level, and the client enforces a minimum.
- **Cost.** *Not fetched.*

### OVHcloud

- **Offer.** Confidential computing is bare metal only. The page names Intel SGX and "AMD Infinity Guard" with AMD Secure Encrypted Virtualization. It does not name SEV-SNP or TDX, and lists no managed confidential VM ([OVHcloud confidential computing](https://www.ovhcloud.com/en/bare-metal/uc-confidential-computing/)). Features are enabled "via the BIOS or the OVHcloud Control Panel" (same page).
- **Hardware.** Scale 2026 "supports AMD SEV technology" on EPYC 9005. Advance 2026 uses EPYC 4005. Both are offered in Europe, Canada, the US and APAC ([OVHcloud Bare Metal 2026](https://corporate.ovhcloud.com/en/newsroom/news/ovhcloud-bare-metal-2026-amd/)). OVHcloud's only SEV guide targets Naples and Rome CPUs on Ubuntu 20.04 ([Enable AMD SME/SEV](https://docs.ovhcloud.com/en/guides/bare-metal-cloud/dedicated-servers/sme-sev-enable-and-use)). Neither CPU generation supports SNP.
- **Cost.** Advance-5 (EPYC 8224P) "from $467/month", marked "Soon available", in USD on the `/en/` page ([OVHcloud confidential computing](https://www.ovhcloud.com/en/bare-metal/uc-confidential-computing/)).
- *Unverified: whether SNP can be enabled on Scale 2026 servers. If it can, OVHcloud is a group 1 self-host target.*

### Hetzner

No Hetzner page on SEV, SEV-SNP or confidential computing was found. Searches restricted to hetzner.com returned only other providers. *Unverified: whether Hetzner AX servers with EPYC CPUs allow SNP in the BIOS.* Treat Hetzner as "self-host on rented hardware, SNP unconfirmed".

### Other EU and sovereign clouds

- **STACKIT Confidential Server.** "Currently only available to a limited extent and on request", with "modern (hardware-based) security technologies (e.g. AMD SEV)" ([STACKIT Confidential Server](https://stackit.com/en/products/confidential/stackit-confidential-server)). Edgeless documents STACKIT as "AMD SEV-ES" with a hypervisor-based vTPM ([Constellation attestation](https://docs.edgeless.systems/constellation/architecture/attestation)). SEV-ES has no attestation report signed per VM. *Unverified: SNP support.*
- **Open Telekom Cloud** offers confidential computing as a preconfigured ECS "c7t" flavor on physical servers, and its blog describes Intel SGX ([OTC confidential computing](https://www.open-telekom-cloud.com/en/security/confidential-computing)). SGX is a process enclave, not a VM TEE, so it is out of scope for a provider-agnostic unsealer.
- **Exoscale, UpCloud.** No first-party page on SEV-SNP or TDX was found.
- **Oracle Cloud** is not in scope. A search summary reported SEV-SNP on its E5 and E6 shapes. *Unverified: no first-party page was read.*

### Self-hosted hardware

- **SEV-SNP CPUs.** AMD publishes ASK and ARK certificates for EPYC 7xx3 (Milan), 9xx4 (Genoa) and 9xx5 (Turin) ([AMD SEV developer page](https://www.amd.com/en/developer/sev.html)). AMD states that "Prior generations of AMD EPYC™ do not support SEV-SNP" ([AMD-SB-1004](https://www.amd.com/en/resources/product-security/bulletin/amd-sb-1004.html)). *Unverified: SNP on EPYC 8004 (Siena) is implied by Scaleway's Iridium offers. A search summary claimed that AMD documents SEV as unavailable on EPYC 4004 and 4005. The AMD page was not read.*
- **TDX CPUs.** Intel requires "5th Gen Intel® Xeon® Scalable Processor" or "Intel® Xeon® 6 Processors", with symmetric DIMM population across memory controllers ([Intel TDX enabling guide, hardware](https://cc-enabling.trustedservices.intel.com/intel-tdx-enabling-guide/03/hardware_selection/)). Canonical also lists "select SKUs" of 4th Gen Sapphire Rapids ([canonical/tdx](https://github.com/canonical/tdx)).
- **Host software, upstream.**
  - Linux 6.11 added "AMD Secure Nested Paging (SEV-SNP) Hypervisor Support" ([Linux 6.11](https://kernelnewbies.org/Linux_6.11)). Linux 6.16 added "KVM: Tdx initialization + vcpu/vm creation" ([Linux 6.16](https://kernelnewbies.org/Linux_6.16)).
  - QEMU's `sev-snp-guest` object exists "Since: 9.1". It accepts `id-block`, `id-auth`, `author-key-enabled` and `host-data`, and `kernel-hashes` adds "hashes of kernel/initrd/cmdline ... for measured boot". QEMU's `tdx-guest` object exists "Since: 10.1". It accepts `mrconfigid`, `mrowner`, `mrownerconfig` and a `quote-generation-socket` for the host's Quote Generation Service ([QEMU qapi/qom.json](https://github.com/qemu/qemu/blob/master/qapi/qom.json)). QEMU's TDX page still calls TD attestation "future work" ([QEMU TDX](https://www.qemu.org/docs/master/system/i386/tdx.html)). That contradicts the schema, so the page is likely stale.
  - The guest gets reports through configfs-tsm at `/sys/kernel/config/tsm/report`, since Linux 6.7. The `inblob` takes "Up to 64 bytes of user specified binary data", and for SNP the `auxblob` carries the host-supplied certificate table ([configfs-tsm ABI](https://github.com/torvalds/linux/blob/master/Documentation/ABI/testing/configfs-tsm-report)).
- **Distributions.** Canonical: "Starting with Ubuntu 25.10, base TDX host support has been fully enabled directly in Ubuntu". The project calls itself a "tech preview", and host attestation needs Intel's DCAP stack, an Intel PCS subscription key and platform registration ([canonical/tdx](https://github.com/canonical/tdx)). Confidential Containers v0.23.0 was tested on SNP with an EPYC 7713, kernel 7.0 and Ubuntu 26.04. It was tested on TDX with kernel 6.17 and Ubuntu 24.04.5 ([CoCo v0.23.0 release notes](https://github.com/confidential-containers/confidential-containers/blob/main/releases/v0.23.0.md)). *Not researched: RHEL and SUSE host support levels.*
- **Attestation root.** SNP reports are signed by VCEK, which is fetched from AMD KDS by chip ID and TCB version ([AWS SNP attestation](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/snp-attestation.html) shows the KDS flow). TDX quotes carry a PCK certificate chain to Intel's SGX root. Collateral (TCB info, QE identity, CRLs) comes from the Intel PCS API ([go-tdx-guest](https://github.com/google/go-tdx-guest)).
- **What the host owner controls.** Everything outside the guest: BIOS settings, SNP firmware and microcode level (including rollback to a lower TCB), the TDX module version, scheduling, I/O, and physical access. The reported TCB exposes the firmware level. TEE.fail-class interposer attacks need physical access (sibling note), which a self-hoster has by definition. For a self-hoster this protects against their own cluster admins, not against the hardware owner.

### Confidential Containers (CNCF) on Kubernetes

- **Status.** "Confidential Containers is a CNCF incubating project" ([CoCo docs](https://confidentialcontainers.org/docs/)) with "a new release every 6 weeks". The latest release is v0.23.0 (2026-09-24), based on Kata Containers 4.2.0 and Trustee v0.22.0. Attestation "is supported and tested on three platforms: Intel TDX, AMD SEV-SNP, and IBM SEL" ([v0.23.0 release notes](https://github.com/confidential-containers/confidential-containers/blob/main/releases/v0.23.0.md)).
- **Runtime classes.** The installation docs name `kata-qemu-snp`, `kata-qemu-tdx`, `kata-qemu-nvidia-gpu-snp`, `kata-qemu-nvidia-gpu-tdx`, `kata-qemu-se`, `kata-qemu-coco-dev` (no TEE) and `kata-remote` for peer pods ([CoCo installation](https://confidentialcontainers.org/docs/getting-started/installation/)).
- **Peer pods.** The Cloud API Adaptor gives each pod "its own dedicated confidential VM". Guides exist for AWS (EKS), AKS, Google Cloud and Alibaba Cloud. The TEE is "such as Intel® TDX, AMD SEV-SNP, or IBM Secure Execution, where available" ([CoCo cloud providers](https://confidentialcontainers.org/docs/cloud-providers/)). A peer pod inherits the cloud's measurement group, so on Azure it is group 3 and on AWS SNP group 2. *Unverified: which TEE each peer-pod provider guide uses.*
- **Trust model.** "The Kubernetes control plane is untrusted." "Everything on the host outside of the enclave is untrusted." The host-to-guest API "is protected with an OPA policy running inside the guest that can block malicious requests by the host." Images "should almost always be deployed with signed and/or encrypted images", or the image is "part of the unmeasured API" ([CoCo trust model](https://confidentialcontainers.org/docs/architecture/trust-model/trust-model/)).
- **Init-data and agent policy.** Init-data is a TOML document holding `policy.rego` (the agent policy), the Attestation Agent config and the Confidential Data Hub config. Its hash is "Written to a configuration register at pod launch (TDX's MR_CONFIG_ID, SNP's HOSTDATA)", or extended into PCR 8 on vTPM platforms. A policy can set `default ExecProcessRequest := false` ([CoCo init-data](https://confidentialcontainers.org/docs/features/initdata/)).
  - The get-attestation page says init-data is "currently only supported with peer pods". Kata's Go runtime, however, passes the init-data digest to QEMU's SNP and TDX objects for local VMs (`appendProtectionDevice(..., initdataDigest)` in [`qemu_amd64.go`](https://github.com/kata-containers/kata-containers/blob/main/src/runtime/virtcontainers/qemu_amd64.go)). Kata's init-data code truncates the digest to 32 bytes for SNP and 48 for TDX ([`initdata.rs`](https://github.com/kata-containers/kata-containers/blob/main/src/libs/kata-types/src/initdata.rs)). *Inferred from source: bare-metal init-data binding exists. Not tested end to end.*
- **Own attestation for a workload.** A pod can request evidence at `http://127.0.0.1:8006/aa/evidence?runtime_data=...`, and "the runtime data will be included in the report". The API is off by default. The page warns that "two workloads could produce interchangeable attestation reports" unless the guest TCB is differentiated, for example through init-data ([CoCo get-attestation](https://confidentialcontainers.org/docs/features/get-attestation/)). This lets a Hikyo client verify the unsealer pod directly, without Trustee.
- **What a cluster admin still controls.** The admin picks the runtime class, the pod spec, the image reference and the init-data. Every one of those changes the measurement (image digests through the policy, policy through HOSTDATA or MRCONFIGID), so clients reject anything not in the log. The admin also controls scheduling, deletion, networking and image-pull availability. v0.23.0's limitations: "Credentials for authenticated registries are exposed to the host", and a secure agent policy "may not support some Kubernetes commands, such as exec" ([v0.23.0 release notes](https://github.com/confidential-containers/confidential-containers/blob/main/releases/v0.23.0.md)).
- **Fit with "client verifies, no shell".** Good, with one condition. The workload measurement must cover the guest kernel and initrd, the init-data (and so the policy pinning the unsealer image digest and denying exec), and `report_data` must bind the unsealer's key. On bare metal SNP and TDX that is all CPU-rooted. Edgeless Contrast builds this model for AKS and bare metal, but is licensed BSL 1.1 (change license AGPL-3.0) with a use grant limited to its SDK ([Contrast LICENSE](https://github.com/edgelesssys/contrast/blob/main/LICENSE)). That makes it a reference design, not a dependency.

## Where a KMS can gate release on attestation

The owner treats KMS key release as a separate topic. This is the per-platform status only.

| Platform | Provider KMS gates on that TEE's attestation? | Source |
|---|---|---|
| AWS Nitro Enclaves, Nitro Sandboxes | Yes, through KMS condition keys on PCRs | Sibling note |
| AWS NitroTPM (attestable AMIs) | Yes, verifiable by KMS | Sibling note |
| AWS SEV-SNP report | *None found* | [AMD SEV-SNP for EC2](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/sev-snp.html) names no KMS integration |
| Google Cloud SNP, TDX | Confidential Space through workload identity federation (sibling note). Google Cloud Attestation tokens cover SEV and TDX on C3, not SNP | [Confidential VM attestation](https://docs.cloud.google.com/confidential-computing/confidential-vm/docs/attestation) |
| Azure SNP | Secure Key Release on Microsoft Azure Attestation claims | Sibling note |
| Azure TDX | *Unverified* | |
| IONOS, Scaleway, OVHcloud, Hetzner, self-host | No provider KMS. Self-run broker (SNPGuard, CoCo Trustee KBS) | [snp-guard](https://github.com/ionos-cloud/snp-guard), [Trustee](https://github.com/confidential-containers/trustee) |

## What the host still controls

Common to every platform:

- **Availability.** Scheduling, start, stop, deletion, maintenance. A TEE never prevents denial of service.
- **I/O.** Disk and network are outside the TEE. The unsealer must treat both as hostile: it seals responses to the client, keeps no plaintext state, and authenticates everything it reads.
- **Replay of old evidence.** The host can replay a captured report. Freshness needs a client nonce or a short validity window (see the abstraction below).
- **Firmware and microcode level.** The host can run an older SNP firmware, microcode or TDX module. The report carries the TCB version (SNP reported TCB; TDX TEE_TCB_SVN plus Intel's TCB status), so the client must enforce a minimum. Revocation of a vulnerable TCB depends on the client refreshing that minimum.
- **Launch configuration.** vCPU count and type, guest policy, `HOST_DATA`, `MRCONFIGID` and the ID block are host inputs. All appear in the evidence or change the measurement. The client checks them.
- **Debug.** Debug guests are possible. The SNP guest policy, the TDX `ATTRIBUTES.DEBUG` bit and Nitro's all-zero PCRs in debug mode reveal it. The client rejects them.
- **Migration.** SNP live migration is unsupported on AWS, IONOS and Google Cloud SNP. Google supports it only for SEV ([Supported configurations](https://docs.cloud.google.com/confidential-computing/confidential-vm/docs/supported-configurations)). The SNP guest policy carries a migration-agent permission, and Azure's token carries `x-ms-sevsnpvm-migration-allowed` ([Guest attestation](https://learn.microsoft.com/en-us/azure/confidential-computing/guest-attestation-confidential-vms)). The client should require it to be off.
- **Collateral delivery.** On AWS shared tenancy and Google Cloud the host supplies the certificates. It cannot forge them, because they chain to AMD or Intel. It can serve stale but still valid collateral, such as an older TCB info or CRL (see [Freshness and caching](#freshness-and-caching)).

Per platform:

| Platform | Firmware | Image choice | Notable host powers |
|---|---|---|---|
| AWS SEV-SNP | AWS UEFI (reproducible) | Operator's AMI, outside SNP measurement | NitroTPM is AWS-run; restart on maintenance |
| Google Cloud SNP | Google OVMF (endorsed) | Disk image, outside SNP measurement | vTPM emulated by hypervisor |
| Google Cloud TDX | Google firmware in MRTD | Disk image, in RTMRs | Firmware changes alter MRTD |
| Azure SNP, TDX | Microsoft paravisor | Disk image, in vTPM PCRs | Paravisor updates; accepted by signer |
| IONOS | IONOS-built OVMF, measured (customer-built unverified) | Customer kernel and initrd, measured | Maintenance restarts; no live migration |
| Scaleway bare metal | Scaleway BIOS and SNP firmware; customer guest firmware | Customer, measured | Scaleway updates firmware on reboot; customer may not change BIOS |
| Self-host | Owner | Owner | Physical access |
| CoCo pod | As underlying platform | Pod spec and init-data, measured | Control plane, kubelet, image pull |
| Nitro Sandbox | AWS | Parent chooses image, measured in PCR0 | "carries the device traffic, chooses the image, and controls when the Nitro Sandbox runs" (sibling note) |

## A shared attestation abstraction

### Roles in RATS terms

[RFC 9334](https://www.rfc-editor.org/rfc/rfc9334) (RATS architecture) gives the vocabulary.

- **Attester:** the unsealer.
- **Verifier:** a Go library embedded in each relying party, not a remote service. This is RATS's background-check model with the Verifier co-located.
- **Relying parties:** the CLI, the Kubernetes operator and the CLI's local MCP server.
- **Endorsements:** AMD and Intel certificate chains, the AWS Nitro root, Google launch endorsements, and Microsoft paravisor signer digests.
- **Reference values:** Hikyo's public transparency log.

Hikyo does not need to issue tokens, so an [EAT (RFC 9711)](https://www.rfc-editor.org/rfc/rfc9711) is optional. The normalized `Claims` below can map to EAT claims if a self-hoster wants a remote verifier such as Veraison.

### Evidence formats

| Format | Produced by | Contents to ship with it |
|---|---|---|
| `snp-report` | configfs-tsm, `go-sev-guest` client | 1,184-byte report, VCEK or VLEK certificate, ASK and ARK (or a product name for the pinned ones), optional Google launch endorsement |
| `tdx-quote-v4` | configfs-tsm, `go-tdx-guest` client | Quote with embedded PCK chain, Intel collateral (TCB info, QE identity, CRLs), RTMR event log, optional Google MRTD endorsement |
| `nitro-cose` | `/dev/nsm` | COSE_Sign1 document with its certificate bundle (sibling note) |
| `tpm-quote+hw` | vTPM | TPM quote and event log, plus the hardware report that vouches for the AK (Azure HCL report), or a cloud AK certificate (NitroTPM, Google vTPM) |

### Interface sketch

```go
package attest

// Claims is what every verifier returns after checking signatures and chains.
type Claims struct {
	Platform       string            // "sev-snp", "tdx", "nitro", "tpm+sev-snp", "tpm+tdx", "tpm+cloud", "none"
	Root           string            // pinned root: "amd-ark-milan", "intel-sgx-root", "aws-nitro", "aws-nitrotpm", "gce-vtpm"
	PlatformValues map[string][]byte // provider-controlled: SNP MEASUREMENT, IDKeyDigest, MRTD, firmware PCRs
	WorkloadValues map[string][]byte // Hikyo-controlled: HOST_DATA, MRCONFIGID, RTMR1-3, PCRs, Nitro PCR0-2
	TCB            TCB               // SNP reported TCB, or TDX TEE_TCB_SVN plus Intel TCB status
	Debug          bool
	Migratable     bool
	Bound          [64]byte          // report_data or user_data; TPM qualifying data holds SHA-256(Bound)
}

type Verifier interface {
	// Verify checks signatures, chains and collateral offline against pinned roots.
	Verify(ev Evidence, now time.Time) (Claims, error)
}

// Policy comes from the transparency log, not from the server.
type Policy struct {
	Releases  []Release                 // accepted, past their waiting period; >= 2 during rollout
	Platforms map[string]PlatformPolicy // accepted firmware or paravisor values, minimum TCB
}

func Check(c Claims, p Policy, nonce [32]byte, unsealerKey []byte) error
```

`Check` passes only if all of these hold:

1. The root is pinned for that platform.
2. The platform values match the platform policy, either by hash or by a verified provider endorsement.
3. The TCB is at or above the minimum.
4. Debug and migration are off.
5. The workload values equal one `Release` entry.
6. `Bound` equals `SHA-512("hikyo-unsealer-v1" || nonce || len(unsealerKey) || unsealerKey)`, with a fixed 32-byte nonce and a 4-byte big-endian length. Fixed and length-prefixed fields keep two different (nonce, key) pairs from encoding to the same input. On TPM-chained platforms the quote's qualifying data is `SHA-256(Bound)`, and the check compares that.

`Bound` fills the full 64 bytes that SNP `report_data`, TDX `REPORTDATA` and configfs-tsm `inblob` allow. A TPM quote is different. Its qualifying data is a `TPM2B_DATA`, which the TPM 2.0 specification sizes to `sizeof(TPMT_HA)`, so a TPM that implements only SHA-256 accepts at most 34 bytes ([TPM 2.0 Part 2, Structures](https://trustedcomputinggroup.org/resource/tpm-library-specification/)). A 64-byte `Bound` may be rejected, so the quote carries the 32-byte `SHA-256(Bound)`. *Unverified: the qualifying-data limit of each cloud vTPM (NitroTPM, Google vTPM, Azure paravisor vTPM).*

Inside a CoCo pod, the unsealer can get the same binding through the Attestation Agent. The REST server decodes `runtime_data` as raw bytes or, with `encoding=base64`, as URL-safe base64 without padding ([`utils.rs`](https://github.com/confidential-containers/guest-components/blob/main/api-server-rest/src/utils.rs)). The SNP and TDX attesters reject more than 64 bytes and zero-pad shorter input into `report_data` ([SNP attester](https://github.com/confidential-containers/guest-components/blob/main/attestation-agent/attester/src/snp/mod.rs), [TDX attester](https://github.com/confidential-containers/guest-components/blob/main/attestation-agent/attester/src/tdx/mod.rs)). So the 64-byte `Bound` value lands unchanged. Use `encoding=base64`: without it, the hex or text form of a 64-byte digest exceeds the limit.

### Two policy layers

- **Platform layer.** It differs by group:
  - Group 1: empty or Hikyo's own OVMF hash. On IONOS, the hash of IONOS's published OVMF build.
  - Group 2: AWS UEFI hashes that Hikyo rebuilds from `aws/uefi`, or a Google endorsement verified to `pki.goog`.
  - Group 3: a list of Microsoft IDKeyDigests.
  - Group 4: Google's MRTD endorsement, verified to Google's root. The policy carries the VM's RAM size and early-accept setting, because the endorsed MRTD depends on both, as the SNP measurement depends on vCPU count.

  Hikyo can either log accepted provider values (strict, but a provider firmware update breaks clients until logged) or accept them by the provider's signature (always current, but the provider can change them). That choice is per platform and should be explicit.
- **Workload layer.** One entry per release per platform. For example:
  - The SNP measurement of Hikyo's OVMF, kernel, initrd and command line for a given vCPU count, computed at build time.
  - The init-data digest for a CoCo pod.
  - RTMR values for TDX.
  - PCR0 to 2 for Nitro.

  Each entry also carries the release version, the image digest and a not-before time that ends the waiting period.

### Freshness and caching

- **First contact.** The client sends a nonce. The unsealer returns fresh evidence binding the nonce and its per-boot public key.
- **Afterwards.** The client caches the verified `(key, claims)` for a short TTL and seals requests to that key. That avoids a hardware report per request. Reports are slow on some platforms, and Google warns of boot and performance changes on SNP.
- **Collateral.** The unsealer fetches and serves collateral: VCEK from AMD KDS, VLEK from the host, Intel PCS TCB info and CRLs, Google SNP endorsements from the host cache, and Google TDX MRTD endorsements from the public `gce_tcb_integrity` bucket. *Unverified: how a TDX guest obtains its endorsement other than from that bucket.* Clients verify all signatures against roots compiled into the binary.
  - The CLI then needs no network access to AMD or Intel, and works offline as long as the collateral is within its validity period.
  - The only online input is the transparency log, which the CLI already needs for the measurement set.
  - The host can serve stale but still valid collateral: an older Intel TCB info or an older CRL that does not yet list a revoked key or TCB. Signatures cannot catch that. The minimum TCB that the client reads from the transparency log is the only mitigation, so a TCB recovery must raise that minimum in the log.
  - `go-sev-guest` and `go-tdx-guest` take an injectable `Getter`, so a cache-backed getter fits ([go-sev-guest](https://github.com/google/go-sev-guest), [go-tdx-guest](https://github.com/google/go-tdx-guest)).
- **The same package on both sides.** The unsealer imports the attester half (configfs-tsm, `/dev/nsm`, TPM). The CLI and operator import the verifier half. The non-TEE mode returns `Platform: "none"`, which a policy accepts only when the installation is marked operator-trusted.

### Libraries to reuse

| Library | Use | License | Maintainer and activity |
|---|---|---|---|
| [google/go-sev-guest](https://github.com/google/go-sev-guest) | SNP report retrieval, `verify` (signature, VCEK and VLEK chains, CRL) and `validate` (measurement, HOST_DATA, report_data, minimum TCB, policy, ID block) | Apache-2.0 | Google; "not an officially supported Google product"; pushed 2026-11-10. VLEK handling present in `verify/verify.go` |
| [google/go-tdx-guest](https://github.com/google/go-tdx-guest) | Quote retrieval through configfs-tsm, `verify` with optional Intel PCS collateral and CRL, RTMR extend | Apache-2.0 | Google; pushed 2026-11-09 |
| [google/go-configfs-tsm](https://github.com/google/go-configfs-tsm) | Generic configfs-tsm access | Apache-2.0 | Google; last push 2025-06 |
| [google/go-tpm-tools](https://github.com/google/go-tpm-tools), [google/go-eventlog](https://github.com/google/go-eventlog) | TPM quotes and event log replay for TPM-chained platforms | Apache-2.0 | Google; active |
| [google/gce-tcb-verifier](https://github.com/google/gce-tcb-verifier) | Google launch endorsements for SNP and TDX (MRTD) | Apache-2.0 | Google; active |
| [anjuna-security/go-nitro-attestation](https://github.com/anjuna-security/go-nitro-attestation), [hf/nitrite](https://github.com/hf/nitrite) | Nitro COSE_Sign1 verification | Apache-2.0; MIT | Third party; last pushes 2025-05 and 2024-12. No AWS-maintained Go verifier exists (sibling note). A small own verifier on `veraison/go-cose` is an option |
| [veraison/go-cose](https://github.com/veraison/go-cose), [veraison/eat](https://github.com/veraison/eat) | COSE and EAT building blocks | MPL-2.0; Apache-2.0 | Veraison project; active |
| [confidential-containers/trustee](https://github.com/confidential-containers/trustee) | Reference KBS and verifier; not embeddable in Go clients (Rust) | Apache-2.0 | CoCo; active |
| [edgelesssys/contrast](https://github.com/edgelesssys/contrast) | Reference design for CoCo verification | BSL 1.1 | Edgeless; active. License blocks reuse |
| [edgelesssys/constellation](https://github.com/edgelesssys/constellation) | Reference for per-cloud measurement handling | n/a | Archived 2026-01 |

*Not found: a Go library that parses Azure's HCL report and runtime-claims JSON.* Microsoft's tooling is C++ and Python. A Go parser for group 3 would be Hikyo's own code.

## Which TEE families GA needs

**Recommendation: AMD SEV-SNP and Intel TDX, both through raw hardware evidence with a workload measurement Hikyo computes. SEV-SNP is the floor for self-hosters and IONOS. TDX is the raw path for a hosted multi-region service, on Google Cloud; raw hosted multi-region SNP rests on Scaleway alone. How to weigh the two for the hosted service follows from the hosting choice, which is the owner's. Nitro and the TPM-chained cloud profiles are added on demand.**

Reasons, from the evidence above:

1. **Reach.**
   - SEV-SNP is the only family available at every class of target: self-host (EPYC Milan and later), the one EU managed confidential VM (IONOS), and EU bare metal (Scaleway, where SNP is supported and on-by-default is inferred, not stated). AWS, Google Cloud and Azure also offer SNP in EU regions, but as group 2 or 3, so their SNP evidence does not cover the unsealer's code under the CPU-vendor root alone.
   - TDX reaches four Google Cloud EU regions (group 4) and Azure (group 3). No EU provider documents TDX, and AWS offers none.
   - So the families split. SEV-SNP is the floor for self-hosters and IONOS. For a hosted multi-region service, raw SNP rests on Scaleway alone, and raw TDX on Google Cloud alone.
   - Nitro reaches AWS only.
2. **Self-host parity.** A self-hoster with an EPYC Milan or later server can run the same SNP unsealer as the hosted service. TDX covers self-hosters on Intel from 5th Gen Xeon. Without TDX, Intel self-hosters fall back to operator-trusted mode.
3. **Root at the CPU vendor.**
   - The owner's addendum asks for it. Groups 1 and 4 keep the unsealer's own measurements in CPU-signed evidence. In group 1 the log pins the whole measurement. In group 4 the log pins the RTMRs, and MRTD is accepted by Google's endorsement.
   - On AWS SNP and Google Cloud SNP, the unsealer's identity comes from a cloud TPM. On Azure it passes through Microsoft's paravisor.
   - So the GA verifiers are the raw SNP and TDX paths. If a CPU-vendor root is required, only group 1 or group 4 placements qualify.
4. **Several regions at GA.** The map's destination sets the GA bar as "several regions you choose between" ([Wayfinder map: Hikyo hosted service at general availability](https://github.com/Hikyo-Org/Hikyo/issues/1301)); which regions exist is open in [Grilling: regions](https://github.com/Hikyo-Org/Hikyo/issues/1326). That bar rules out single-region placements for the hosted service:
   - IONOS (`de/fra/2` only) fails on regions. AWS SNP fails on measurement group, on shared tenancy (Ireland only in the EU) and on Dedicated Hosts (any region) alike: both are group 2.
   - Scaleway Elastic Metal with CoCo (self-managed Kubernetes) passes with two confirmed EU metros, Paris and Amsterdam. Warsaw depends on unverified SNP support on EPYC 8004.
   - Google Cloud TDX (four EU regions) passes. Google publishes MRTD endorsements. Their signature chain and current C3 and C4 coverage were not checked.
   - If the owner instead picks AWS or Azure, the matching add-on below (Nitro, or the Azure paravisor profile) becomes a GA requirement.
5. **Kubernetes fit.**
   - CoCo with `kata-qemu-snp` or `kata-qemu-tdx` gives a per-pod TEE, an agent policy that denies exec, and init-data measured into HOST_DATA or MRCONFIGID. That is the "no shell, client verifies" shape on Kubernetes, the same for hosted and self-hosted.
   - It needs bare-metal or nested-virtualization-capable nodes. Scaleway Elastic Metal and self-host qualify. *Unverified: CoCo on IONOS, OVHcloud or Hetzner.*
6. **Cost of supporting both families is small.** Both verifiers reuse Google's Apache-2.0 libraries with the same `Getter` and `validate` shape. The larger cost is the build pipeline: computing a reproducible SNP launch measurement and TDX MRTD and RTMR values per release.

**Add on demand, once a hosting provider is chosen:**

- **Nitro** (Sandboxes on EKS), if the hosted service lands on AWS. It is the best Kubernetes fit there, and AWS SEV-SNP does not cover the OS in the CPU-signed measurement.
- **TPM chained to SNP or TDX on Azure (group 3)**, if hosting lands on Azure. It needs a Go parser for the HCL report and a policy on Microsoft signer digests.
- **Cloud vTPM profiles (AWS NitroTPM, Google SNP vTPM)**, only if a customer or region requires those platforms. They give a cloud root for the application, which the threat model tolerates but the addendum does not prefer.

**Not needed:** SEV and SEV-ES (no per-VM attestation suitable for clients), Intel SGX (process enclaves, a different programming model), and IBM Secure Execution (no target provider in scope).

## Unverified and not researched

These could change the recommendation:

1. **Google Cloud TDX endorsements.** Endorsement objects are published; the signature chain and current C3 and C4 coverage were not checked. If they do not hold, group 4 needs Hikyo to log Google firmware hashes, and every Google firmware update breaks clients until logged.
2. **Azure binding.** Whether an Azure guest can request a fresh SNP report or TD quote with its own `report_data`. If so, group 3 simplifies. Also unverified: how a raw TD quote is obtained on Azure, and whether production paravisor builds match the OpenHCL source.
3. **NitroTPM and SEV-SNP together.** NitroTPM lists the same instance families and names no exclusion. A combined launch was not tested. If they cannot run together, AWS SNP cannot attest the unsealer's code at all, and AWS hosting needs Nitro.
4. **CoCo init-data on bare metal.** The docs say peer pods only. The Kata source passes the digest to QEMU for SNP and TDX. Not tested end to end. The `report_data` path through the Attestation Agent was checked in source only.
5. **EU bare metal.** Whether Scaleway's EPYC 8004 models support SNP, which decides Warsaw. Whether SNP can be enabled on OVHcloud Scale 2026 or Hetzner AX servers. Whether CoCo runs on IONOS Confidential VMs or IONOS Kubernetes. Scaleway, IONOS and STACKIT prices.

Gaps that matter less:

6. **CPU support.** EPYC 4004 and 4005 SNP support was not confirmed from AMD. If it is absent, some Scaleway and OVHcloud models do not qualify.
7. **AWS.** Dedicated Host prices for SNP were not fetched. AWS's lack of TDX is inferred, not stated.
8. **STACKIT.** Whether Confidential Server offers SNP. Edgeless says SEV-ES.
9. **Distributions.** RHEL and SUSE host support for SNP and TDX was not researched.
10. **VLEK provisioning.** How AMD issues the VLEK to AWS, and what AWS can do with it. *Unverified: whether the cloud can see the VLEK private key.*
11. **Hardware attacks.** TEE.fail-class attacks and AMD-SB-3020 remain as in the sibling note. For self-hosters, the hardware owner is by definition able to mount them.
