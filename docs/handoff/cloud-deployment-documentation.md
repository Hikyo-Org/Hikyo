# Cloud deployment documentation

## Delivered scope

- Cloud deployment hub and AWS, Azure, Google Cloud, DigitalOcean, and Hetzner
  guides in `docs/site/src/content/docs/docs/`, linked from navigation, index,
  installation, and self-hosting. Search indexes content automatically.
- `install/cloud/aws/cloudformation.json`: EC2/SSM host with no inbound rules,
  encrypted boot disk and retained separate data volume. Existing VPC/subnet
  required. Upload through CloudFormation; a quick-create URL needs S3 template
  hosting, which is not configured by this change.
- `install/cloud/azure/azuredeploy.json`: Ubuntu VM, VNet, static public IP,
  explicit inbound deny, SSH public-key authentication, encrypted managed data
  disk. Azure launch link references the file on `main` and works after
  publication there. Deleting a resource group still deletes its disks.
- Both templates provision infrastructure only. Verified release installation,
  persistent custody, root key, DNS/TLS, administrator creation, and restore
  verification remain explicit operator steps. No cloud account was modified.
- Added `cloud-nightlies.mdx` with a complete fresh Docker/native-TLS nightly
  installation, persistent root/custody inputs, first-admin steps, exact digest
  and moving-channel upgrades. `cloud-upgrades.mdx` distinguishes supported
  systemd nightlies from manual/stable/Kubernetes procedures and track changes.
- `install/cloud/compose/nightly.yaml` runs the application after explicit setup;
  this is distinct from the AWS/Azure infrastructure templates. The root-key
  environment path also supports local operator commands. Runtime binary path
  was verified against `Dockerfile.release`.
- DigitalOcean and Hetzner now link their published Docker-host launch options.
  These do not install Hikyo. DigitalOcean requires a reviewed Marketplace image
  for a Hikyo-specific listing; its App Platform has no persistent volumes.
  Hetzner native deploy links select existing Apps, not arbitrary repo templates.

## Validation

- `corepack pnpm --dir docs/site run verify`: passed, including Astro check
  (zero diagnostics), build, policy/link checks, CSP, and offline browser test.
- AWS: `cfn-lint` 1.57.0 passed.
- Azure: upstream ARM deployment schema plus all six resource schemas passed
  local JSON Schema validation against their declared API versions.
- HTTP checks: hub, two lifecycle guides, and five provider pages return 200 and appear in
  `/api/search.json`.
- Rendered internal guide anchors and both native Docker-host launch links were
  checked over the LAN preview. Compose configuration rendered using the exact
  guide environment block; required key/state inputs fail closed when missing.
  Native TLS, loopback operational port, non-root/read-only container settings,
  and persistent mounts were inspected. No actual container deployment was run.
- Playwright: hub/provider links and Azure launch URI checked; desktop and
  390px mobile viewport show no horizontal overflow. Mobile screenshot inspected.
- Local source links and `git diff --check` passed.
- Cross-provider review skipped at the user's explicit request.

## Delivery boundaries

The user approved GitHub Pages publication after reviewing the LAN preview.
Publish through the signed PR, passing required checks, merge, and existing
`docs` workflow; verify all eight cloud routes and repository-hosted templates
on the public site afterward. This handoff records pre-publication validation;
the PR and workflow runs carry the final delivery state. No live cloud resource
deployment was performed.
Regional image/SKU availability, IAM permissions, network reachability, provider
console deployment, and complete Hikyo boot still need account-level validation.
Cloud provider fees are not measured or quoted. The docs explicitly distinguish
local checks from live deployment evidence and prereleases from stable support.
