# Pinned tooling and a working local app

## Inspect pins and choose the scope

Derive Go from `go.mod`, Node from `.nvmrc`, pnpm from each manifest's
`packageManager`, and Corepack from `scripts/ci/install-corepack.sh`. Compare
effective `go version`, `node --version`, `corepack --version`, and
`pnpm --version` after selection. Select Node before installing Corepack because
Node selection can change npm's global tool location.

`fnm` is mandatory for the current `scripts/dev/preflight.sh`, even with the
correct Node already installed. Install it using the platform's supported
installer when that preflight is needed. With fnm available, from the root:

```sh
eval "$(fnm env)"
fnm use --install-if-missing
scripts/ci/install-corepack.sh
go mod download
```

The Corepack script installs the pinned launcher globally under the active Node
and checks package-manager consistency. Inspect it before running; handle
permission errors with a writable user toolchain instead of silently using sudo
or changing system configuration. Go may auto-download a toolchain according
to `GOTOOLCHAIN`; verify the effective version. Use official documentation and
check current instructions when giving external installation commands.

## Dependencies and embedded UI

The root `package.json` only pins tools. This is not a single pnpm workspace:
`clients/ts`, `web`, `docs/site`, and `scripts/mcp-conformance` have their own
manifests/lockfiles. Install relevant packages with frozen lockfiles. The web
bundle resolves generated client source and Zod from `clients/ts`, so both
packages need installation even for frontend-only work.

For the full app, run from the root:

```sh
pnpm --dir clients/ts install --frozen-lockfile
pnpm --dir web install --frozen-lockfile
pnpm --dir web run build
mkdir -p bin
go build -tags ui -o ./bin/hikyo ./cmd/hikyo
```

`scripts/ci/build-spa.sh --verify` wraps these frontend installs, web
typecheck/lint/unit tests, and bundle build. Plain Go without `-tags ui` is
API-only. Build the SPA before a ui-tagged binary: `internal/webui/dist` is
ignored and absent on a fresh checkout. Do not commit the bundle. Generate
client/Go source only when required by the changes or stale/missing artifacts;
inspect generated diffs and retain legitimate changes.

## Isolated development runtime

Use this section and first-admin setup only for a selected source-built
playground or an already-requested running app. Build and test work can finish
without leaving a persistent instance running.

Use a dedicated mode-0700 directory outside tracked source for runtime, database,
root key, socket, and authority. Record its location. Retain the user's existing
database/key pair. Inspect inherited Hikyo configuration names for production
overrides without printing secret values. Use explicit development configuration
in a clean child environment if necessary; never smoke-test an existing
production DB.

After assigning absolute paths to the checkout and dedicated runtime directory:

```sh
cd "$hikyo_runtime_dir"
"$hikyo_repo_root/bin/hikyo" server --dev --cli-socket "$hikyo_runtime_dir/cli.sock"
```

Track the process/session handle. From another terminal, verify health,
readiness, and the embedded page:

```sh
curl --fail http://127.0.0.1:8081/healthz
curl --fail http://127.0.0.1:8081/readyz
curl --fail --output /dev/null http://127.0.0.1:8080/login
```

Browser/app defaults to 8080; operational health to 8081. Recheck config/help.
If ports are occupied, do not kill unknown listeners; select unused loopback
ports with `--listen` and `--operational-listen` and update checks/login URLs.

Development is intentionally loopback-only. Do not weaken that boundary for LAN
previews. Passkey ceremonies may require a loopback hostname such as `localhost`
as the relying-party origin; inspect source/config instead of assuming an IP
supports WebAuthn. A real LAN-accessible authenticated app needs the supported
secure TLS/origin configuration, a separate setup decision.

## First administrator and CLI

Read `docs/site/src/content/docs/docs/getting-started.mdx` and current CLI help.
Create the first admin only in the disposable development instance and from
the same runtime directory as the server:

```sh
"$hikyo_repo_root/bin/hikyo" admin --dev create \
  --username admin --output-file "$hikyo_runtime_dir/admin-authority"
"$hikyo_repo_root/bin/hikyo" account establish-credential \
  --instance http://127.0.0.1:8080 --as admin
```

Credential establishment requires the contributor's controlling terminal,
instance confirmation, single-use authority, and chosen password. Tell them
where to read the mode-0600 authority locally; never print it through agent
tools, put it in argv/environment, or paste it in chat. Delete only that
bootstrap authority after successful establishment. Continue independent tests
while the contributor handles this step. CLI login uses the same-user socket:

```sh
"$hikyo_repo_root/bin/hikyo" login http://127.0.0.1:8080 --local --as admin \
  --socket "$hikyo_runtime_dir/cli.sock"
```

Successful startup is not proof of usable admin credentials. This evaluation
app requires no cloud secrets or production `.env`.

## Frontend iteration and docs previews

Normal web Vite currently has no backend proxy and does not prove integrated
authenticated behavior. Use the embedded Go app for real flows. For mocks or
components deliberately choose and label prototype or Storybook:

```sh
pnpm --dir web run prototype --host 0.0.0.0
pnpm --dir web run storybook --host 0.0.0.0
```

Check supported forwarded host/port flags. Documentation-site work uses:

```sh
pnpm --dir docs/site install --frozen-lockfile
pnpm --dir docs/site run dev --host 0.0.0.0
```

Use the actual port/current LAN address for a permitted LAN preview, verify
that exact URL, and keep its process live through the requested review window.
Stop only servers created by the bootstrap when cleanup is requested.
