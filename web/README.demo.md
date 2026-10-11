# Browser-only Hikyo demo

The real Hikyo frontend with disposable sample data and three account presets.
It builds to static files, needs no backend, and is hosted under `/demo/` by the
existing GitHub Pages workflow. Refresh, Reset demo, or changing accounts starts
a fresh demo. Each tab has independent data.

## Try it

Start with Developer. Click LOG_LEVEL in development, enter a value, and save a
draft. Open Publish drafts to see the permission limit. Switch to Demo admin,
create a new draft, and publish it to see the new revision and history. Click a
secret cell and Reveal to display deliberately fake material. Switch to Viewer
to inspect the same UI with editing and secret reveal unavailable.

| Account | Browse | Edit and stage | Publish | Reveal sample secrets |
| --- | --- | --- | --- | --- |
| Viewer | All environments | No | No | No |
| Developer | All environments | Development and staging | No | No |
| Demo admin | All environments | All environments | Yes | Yes |

Settings, members, declarations, and revision history are read-only. Machine
access, approvals, temporary access, deployment adapters, audit, remotes, SCIM
provisioning, and account security also open as read-only screens with sample
records. Browsing and supported detail controls remain usable; actions that
change data or connect to another instance are disabled.

Real login/MFA, imports, key/folder management, revision restore/pinning/diff,
and copying across environments remain unavailable. The app's usual dialogs,
layout, themes, matrix navigation, masking, and form controls remain in use.

## Run and build

From the repository root, using the Node version in `.nvmrc` and pinned pnpm:

```sh
pnpm --dir clients/ts install --frozen-lockfile
pnpm --dir web install --frozen-lockfile
pnpm --dir web run demo
```

Vite prints a LAN-accessible `/demo/` URL. For the release-shaped static build:

```sh
pnpm --dir web run build:demo
pnpm --dir web run preview:demo --port 5195 --strictPort
```

The output is `web/demo-dist/`. Copy its contents into the `demo/` directory of
any static website. No API routes, redirects, service worker, or history fallback
are required. Navigation uses URLs such as `/demo/?account=admin#/orgs/...`.
The account parameter comes before the hash. Serve over HTTP(S), not `file://`.

For a project Pages site or another mount point, rebuild with that public base:

```sh
HIKYO_DEMO_BASE=/my-repository/demo/ pnpm --dir web run build:demo
```

The existing docs workflow builds and tests the demo, then copies it into the
Pages tree after the docs service worker has been generated. Once merged and
deployed, it will be served at `https://hikyo.app/demo/`. This change itself does
not publish anything.

## Implementation boundary

`src/demo/api.ts` installs an in-memory fetch adapter before React and the auth
provider mount. It handles only the supported fixture reads and value
mutations. Unsupported API calls and remote fetches return local refusals.
Generated SDK response parsers still validate the payloads. There is no database,
session storage, or local-storage persistence of demo values. Existing theme and
UI preferences can still use their normal browser storage.

Account changes reload the document, which drops drafts, published edits,
revealed values, and query caches together. Role restrictions exist in both
the UI and adapter. This is a public simulation, not an authentication or
authorization boundary: all bundled sample material is public, and reveal/MFA
assurance is simulated. Use only sample values.

Keep dynamic imports behind literal `import.meta.env.MODE === 'demo'` checks.
An imported boolean alone does not remove dynamic chunks with this Vite build.
The normal `build` command checks its emitted assets for demo implementation
markers before compression. The demo adds no dependencies.

## Keeping the demo in sync

The demo mounts the same route components, layouts, styles, and generated API
parsers as the normal application. UI changes therefore flow into the demo on
the next build. The Pages workflow rebuilds it from the same source revision.

Every route in `src/app/navigation.ts` must have a policy in
`src/demo/surfaces.ts`: interactive, read-only, or excluded with a reason.
The route wrapper and sidebar use that policy. TypeScript rejects missing or
stale entries. Interactive means the route supports normal demo interactions;
it does not promise that every action works. History and declaration drawers
share the interactive matrix, with their own mutation actions disabled.

`e2e/demo/demo.spec.ts` also requires a browser expectation for every route,
with a mode that matches its policy. Tests derive URLs from the real route
registry and visit every route with all three accounts on desktop and mobile.
They check real content, read-only notices, exclusions, and redirects. New
screens cannot silently disappear from coverage. Existing interaction tests
verify editing, publishing, role restrictions, and browsable read-only details.

When adding a screen, choose its policy, provide any fixture responses using
the generated schemas, and add its expected heading/sample or excluded outcome
to the browser record. Run the validation below. Changes inside an existing
screen still need appropriate interaction coverage; route coverage alone cannot
prove every control remains correct.

## Validation

```sh
pnpm --dir web run typecheck
pnpm --dir web run lint
pnpm --dir web run design:check
pnpm --dir web run test
pnpm --dir web run build
pnpm --dir web run build:demo
pnpm --dir web run test:demo
```

The browser suite needs the pinned Playwright Chromium installation, available
in the existing CI image. Locally, `pnpm --dir web exec playwright install chromium`
installs it. The suite serves the built `/demo/` artifact without Hikyo, exercises
all three accounts on desktop and mobile, and rejects real API or external
network requests. Unit tests cover schema validity, role refusals, isolated
state, selective publishing, stale draft rejection, unset values, and history.
