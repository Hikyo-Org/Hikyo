# Azure host infrastructure

[![Deploy to Azure](https://aka.ms/deploytoazurebutton)](https://portal.azure.com/#create/Microsoft.Template/uri/https%3A%2F%2Fraw.githubusercontent.com%2FHikyo-Org%2FHikyo%2Fmain%2Finstall%2Fcloud%2Fazure%2Fazuredeploy.json)

This button opens Azure's native template review and deployment form. It creates **billable host infrastructure, not a running Hikyo installation**. Review the subscription, region, VM size, disk capacity, and pricing before submitting. The public button becomes available once this template is published on `main`.

The template creates an Ubuntu 24.04 LTS amd64 VM (`Standard_B2s` by default), a Standard static public IPv4 address, a dedicated virtual network, and a separate encrypted managed data disk. It accepts your existing **public** SSH key only. Password login is disabled. There are no bootstrap scripts, downloaded installers, generated application secrets, or inbound allow rules. A custom inbound deny rule also overrides Azure's default virtual-network and load-balancer allowances. Outbound access uses the VM's public IP.

## Deploy and configure

1. Open the button, select a **new dedicated resource group**, and enter your public SSH key. Never paste a private key. Use an amd64-compatible size available in your region. Resource names are fixed; use a separate resource group for each installation.
2. After deployment, open `hikyo-firewall` in the Azure portal. Under **Inbound security rules**, add TCP port `22`, source **IP Addresses** set to your operator public IPv4 address with `/32`, destination **Any**, action **Allow**, priority `100`. Use your VPN's egress address when appropriate. Do not use `0.0.0.0/0` for SSH. Connect with `ssh azureuser@<publicIpAddress>` using the matching private key. Azure VM **Run command** is an alternative management path for authorized operators; the VM agent is enabled and does not need inbound SSH.
3. Identify the attached LUN `0` disk using `lsblk -f`. Deliberately partition, format, and mount it before putting persistent application data there. It starts empty and unmounted. Use a filesystem UUID for persistent mounts. **Never format an existing or restored disk.** Configure the application to use the mounted volume and verify the mount after a reboot.
4. Follow the [Azure deployment guide](https://hikyo.app/docs/deploy-azure/) and [shared host installation](https://hikyo.app/docs/cloud-deployment/#complete-the-host-installation) to upload verified release artifacts, configure Hikyo, production storage, root keys, service startup, DNS, TLS, and backups. Allow TCP `8443` from intended client CIDRs only after TLS and authentication are configured. A separately configured reverse proxy may use `443`; open TCP `80` only if its selected certificate challenge or redirect needs it. Keep operational port `8081`, database, and metrics ports private.
5. Verify HTTPS, authentication, readiness, and backup restoration before using this installation. Remove temporary SSH access when finished. Deployment success means that Azure created infrastructure; it does not mean Hikyo is installed or ready.

## Data lifecycle and costs

The separate `hikyo-data` managed disk uses platform-managed encryption at rest. It has `deleteOption: Detach`, so deleting the VM through the normal VM API retains the disk unless you explicitly choose to delete it too. **Deleting the resource group deletes the data disk and its contents.** ARM does not provide CloudFormation-style deletion retention for a resource group. Export and test backups outside this resource group before destructive operations; detachment is not a backup. The OS disk is configured for deletion with the VM.

VMs, disks, the public IPv4 address, snapshots, backups, and network transfer can incur charges. Deallocating the VM does not remove disk or public-IP charges. Review retained resources after removal. The default size is a small pilot starting point, not a production sizing guarantee or high-availability configuration.

## Validation

The template is declarative ARM JSON. Validate it against Azure's deployment and resource schemas before publication. Local schema checks cannot prove subscription quotas, regional SKU/image availability, or successful deployment; those require an authenticated Azure validation/deployment in your subscription. No cloud resources are created by repository checks.
