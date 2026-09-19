# AWS host template

[`cloudformation.json`](cloudformation.json) provisions one Ubuntu 24.04 amd64
EC2 host, an encrypted 16 GiB gp3 boot disk, a separate encrypted 30 GiB gp3
data volume by default, and an instance role for AWS Systems Manager.
It does **not** install Hikyo, create a database, configure DNS or TLS, or open
inbound ports. Stack success means infrastructure was provisioned, not that
Hikyo is ready.

## Create the host

1. Select an AWS region with the chosen instance type and Canonical public AMI
   parameter available. Choose an existing VPC and a subnet within that VPC.
   Enable VPC DNS support. With `AssociatePublicIp=true`, the subnet needs a
   route to an internet gateway. With `false`, provide NAT or the required
   Systems Manager VPC endpoints. Network ACLs and endpoint security groups
   must allow Systems Manager HTTPS connectivity. VPC endpoints alone do not
   provide internet access for OS updates or downloading Hikyo.
2. Download `cloudformation.json`. In the region's CloudFormation console,
   choose **Create stack**, **With new resources**, then **Upload a template
   file**. Select the file, enter the VPC/subnet parameters, and review the
   resources and ongoing EC2, EBS, IPv4, NAT, and endpoint costs that apply.
3. Acknowledge IAM resource creation and create the stack. The role only has
   `AmazonSSMManagedInstanceCore`; it grants no application Secrets Manager or
   database access. The deployer needs permission to create the listed EC2 and
   IAM resources, pass the instance role, and resolve the public SSM parameter.
4. After stack completion, open the `SessionManagerUrl` output. Allow time for
   the preinstalled Ubuntu SSM Agent to register. Your AWS identity separately
   needs Session Manager permissions. If the host stays offline, check the
   outbound route, DNS, endpoint rules, and SSM Agent status. There is no SSH
   key pair or inbound SSH rule.
5. Follow the [AWS deployment guide](https://hikyo.app/docs/deploy-aws/) to
   prepare storage, install Hikyo, and configure production services. Keep all
   persistent application data, database files, configuration, and root-key
   material on retained storage or an independently backed-up service.

CloudFormation launch links with `templateURL` require a published S3 template
URL. A GitHub raw URL is not a supported substitute. This repository does not
claim to provide a working AWS quick-create link until that hosting exists.

## Storage and lifecycle

The data volume is attached as `/dev/sdf` at the EC2 API, but Nitro instances
expose an NVMe device with a different name. Match the `DataVolumeId` output
against the device's EBS serial using `lsblk -o NAME,SIZE,FSTYPE,MOUNTPOINTS,SERIAL`.
Inspect the selected device with `sudo blkid <device>` before any formatting.
Only create a filesystem on a confirmed new, empty volume. Mount by filesystem
UUID and arrange persistent mounting before writing Hikyo data. No user data
script formats disks or downloads software.

Deleting the stack terminates the host and deletes its boot disk. The data
volume is retained and remains billable. Retention is not a backup: configure
application-consistent database backups, root-key/configuration backups, and
EBS snapshots with tested restores. Save the volume ID outside the stack before
deletion. A new stack creates a new volume; it does not automatically adopt an
old one. Recovery requires an operator to attach the retained volume to a host
in the same Availability Zone, mount its existing filesystem, and restore
configuration and services.

Review change sets before updates. Resolving a new `UbuntuAmi` or changing the
subnet can replace the instance; changing Availability Zone replaces the data
volume and retains the old one without copying its contents. Even same-zone
instance replacement can fail to reattach an in-use data volume. Before host
replacement, stop services, back up, and unmount the volume; plan a controlled
detach/reattach or restore. Never assume a stack update migrates data. Normal
OS patching inside the host avoids an infrastructure replacement.

The public IPv4 address is ephemeral. Use deliberate DNS/address management or
a load balancer for production. Configure HTTPS ingress through managed
CloudFormation changes once TLS and authentication are ready. The instance
requires IMDSv2, with a hop limit of one. Container workloads cannot assume
they can access its metadata credentials. Add application AWS permissions only
through a separately reviewed, least-privilege design.

## Local validation

Run `cfn-lint install/cloud/aws/cloudformation.json` from the repository root.
This checks template syntax and CloudFormation resource schemas; it does not
prove region capacity, account permissions, connectivity, or a live deployment.
