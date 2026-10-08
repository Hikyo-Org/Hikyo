# Optional local Docker playground

Use only after the contributor selects a Docker playground. Inspect the current
checkout's container build and installation docs before generating commands.
Keep contribution builds/tests separate: a published image does not validate
the user's edited source.

## Select a supported container path

Check `docker version`, `docker compose version`, the active Docker context,
and daemon reachability. Verify the context targets the contributor's local
machine; do not accidentally launch on a remote/shared daemon. A missing daemon
is a concrete prerequisite. Offer the supported source-built path if the user
does not want to install/start Docker. Avoid silently installing a system daemon.

Inspect `Dockerfile.release`, `install/cloud/compose/README.md`, and the current
image/Compose requirements. The existing cloud nightly example enables
unattended upgrades and uses production host paths; do not start it unchanged
for a disposable contributor playground. Do not enroll unattended upgrades,
create cloud resources, or use production credentials as part of this choice.

`install/compose/demo` and `scripts/compose-demo.sh` exercise secret delivery to
an application container. They are not a persistent containerized Hikyo server
and the script tears its instance down. Do not present them as the requested
playground.

Native `server --dev` binds to container loopback; publishing a host port does
not make that listener reachable. Do not change development-mode validation,
use broad trusted-proxy exceptions, or assume host networking works on every
platform. Use a supported container server configuration with native TLS,
explicit external origin, local test root key, and persistent disposable storage.
If no lightweight supported path is available, explain the actual additional
TLS/storage setup and recommend source-built rather than inventing a one-line
Docker command. Obtain the user's revised choice before switching methods.

## Isolate and prove the playground

Use a unique Compose project/container name, task-owned data/key paths, and
unused ports published only to host loopback by default. Do not mount the
Docker socket or unrelated home directories. Preserve the database/root-key
pair across restarts. Select a verified published image for product exploration,
or a supported source-image build when requested; describe which source/version
it runs. Do not treat an unsigned source build as a signed release.

Validate Compose configuration, start the selected project, inspect container
state, and verify health/readiness plus the UI at the exact advertised URL.
Handle first-admin creation using the documented container/CLI path and human
credential-establishment steps; keep authorities/passwords out of agent output.
Keep it running for exploration, and provide project-scoped stop/restart
commands plus storage location. Removing volumes or keys is a separate
destructive choice, not ordinary stop/cleanup.
