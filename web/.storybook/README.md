# Hikyo Storybook

Story files live beside their UI, composite, feature or page owner. Discovery is
recursive under `src/`. Use explicit slash-separated CSF titles:

- `Design system/…` for shared UI and visual foundations.
- `Shared/…` for cross-feature composites.
- `Features/<domain>/…` for feature-specific components.
- `Pages/…` for screens, while keeping files in their feature folders.

Existing modules carry explicit legacy `id` values so title changes preserve
Canvas, Docs and OpenPencil links. Do not remove an ID without documenting its
replacement. There is no custom `storySort`; Storybook retains its installed
default ordering and CSF export order.

Use native semantic HTML for structure, text, lists, tables and forms. Shared
components own meaningful styling, interaction, accessibility, sensitivity or
graphics. The markup checker targets actual shared-control bypasses rather
than banning native HTML. Compatible prop variants can share a captioned
gallery with a controls-driven example. Preserve distinct loading, empty,
error/retry, busy, permission, validation, selected, expanded, keyboard and
overlay states. Before removing behavior-only stories, prove equivalent owning
regressions and record the retained example.

Every module must retain generated Docs. Provide a source-backed description,
correct types and defaults, and controls that update the example. API screens
use `withApp`; install contract-valid, method/path/query-aware fixtures before
mounting. App modules and top-layer components use `topLayerDocs`, which keeps
examples in independent frames and disables Docs autoplay. Pending requests
end with the fixture lifetime; asynchronous handlers must check their request
signal after awaiting a body before changing fixture storage. Do not change
global fetch from inline Docs or share a live query cache across examples.

Real-route journeys render `AppRoutes` under the existing provider/router
decorator and use named interaction steps. Await observable module, render,
request, play and font completion. Keep production motion unchanged. Docs
remain manually explorable; human-only WebAuthn and popup authorization,
external federation, private-key downloads and production secret access are
explicit exceptions rather than simulated browser proof.

The installed Storybook 10.6 iframe Docs renderer ignores the autoplay flag.
The Overview journey and Menu keyboard examples therefore check
`isManualDocsFrame` before playing: same-origin Docs frames stay at the starting
screen, while Canvas and the default browser interaction pass still run every
step. Other stories may deliberately play to
reach their named overlay or interaction state; the flag alone is not proof
that a framed story stays idle.

The app theme toolbar owns both foreground and background through application tokens. The separate built-in background toolbar is disabled because its forced surface can disagree with the app theme. The central gate compares actual body paint with the resolved `--bg` token, as well as theme attributes.

The public Docs container subscribes to the same globals event as installed Docs Controls. It publishes the toolbar theme to same-origin examples through an owned attribute. Frame observers change CSS without replacing their URLs, React trees, providers or live forms, and disconnect on navigation. The browser test project prebundles this event dependency to avoid a dependency-optimizer reload during initial test imports.

The Docs isolation guard checks each exported story's effective metadata and
story parameters using the installed CSF parser. It follows static local aliases,
the canonical `topLayerDocs` import and simple fixture helper return objects;
JavaScript spreads remain shallow before Storybook merges parameter levels.
An unrelated sibling, comment or constant cannot supply frame evidence. Inline
overrides, invalid scalar `app` parameters and unresolved isolation expressions
fail visibly. A helper may use arguments for fixture data, but isolation fields
must be statically declared without depending on shadowed helper parameters.
Keep parameter aliases immutable. Top-level calls or mutations are not static
isolation declarations; direct CSF story annotation assignments are parsed by
Storybook. Asynchronous or cyclic parameter helpers fail visibly.
The runtime harness's inline-Docs refusal remains the independent backstop.

Storybook 10.6 also rerenders Docs for globals updates. Its exported renderer
rekeys the root error boundary on each call, replacing framed examples and
their unsaved state. The local `docs.renderer` facade retains the existing
render only when its context, parameters and mount element are identical.
Theme updates still reach the subscribed container and controls. Navigation,
HMR with a new context, changed parameters, failed renders and explicit unmount
delegate to the original exported renderer so they can refresh the page.

The application and preview share `src/styles/index.ts`. Component styling
belongs beside its owner; global tokens, reset, themes and true utilities stay
global. `Button.css` is imported by the global owner because semantic anchors,
summary controls and file-upload labels also consume its classes. Docs-only
table overflow styling stays here. Preserve declaration order and check both
themes and sibling consumers before moving a rule. Register new CSS owners in
the token adherence budget.

React prop extraction excludes only `clients/ts/src/generated/`, which owns
API transport, schemas and types. All frontend owners remain eligible. The
installed docgen hook is retained with a Vite filter; a changed hook contract
fails visibly for upgrade review. Keep generated Docs and live-control checks
when evaluating this optimization.

Use the repository's Node version and frozen client/web installs. From `web/`:

```sh
node --run typecheck
node --run lint
node --run test
node --run design:check
node --run build-storybook -- --output-dir .artifacts/review/storybook-static
node --run check:storybook-docs -- .artifacts/review/storybook-static
node --run test-storybook
node --run test-storybook:light
STORYBOOK_STATIC_DIR=.artifacts/review/storybook-static node --run test-storybook:theme
```

`build-storybook` exports design assets, builds once and checks the complete
index. The guard recursively inventories source modules and matches each
generated Docs entry by import path. Empty files, undiscovered nested modules,
malformed/empty indexes, stripped Docs, opt-outs and unrelated same-title Docs
fail. Existing CI runs the same guard after its build and keeps both browser
palettes and the design subpath checks. The frame source check enforces the
shared convention; `withApp` also refuses inline app Docs at runtime.

Theme switching is a single built-Storybook browser contract in
`e2e/storybook/`: Canvas, inline Docs, independent API frames, dialogs/popovers,
live controls, unsaved form state, focus reachability and navigation cleanup.
It consumes an immutable existing build and is also required by the existing
Storybook CI job. Shared observer/renderer regressions stay beside their owner.

Every story still renders and runs accessibility checks in dark and light.
The default pass executes every interaction. The light project alone sets the
test-only `hikyoInteractionPass: appearance` global. Only explicit
`interactionOnce(id, fullPlay, appearancePlay)` entries may use a shorter
appearance play. Manual light Canvas keeps full plays. Docs retains explicit
manual-exploration guards for route journeys and keyboard/top-layer examples;
the test-only global does not change their behavior.
The manifest in `interactionCoverage.ts` gives each opt-in a source-backed
reason and retained view IDs; the complete-index guard rejects unaccounted
tags, stale IDs, missing replacement views and test opt-outs.

Keep async readiness in both passes before accessibility runs. State-producing
plays, keyboard/focus views, errors, busy states and overlays remain in both
palettes unless an explicit retained view covers their appearance. The real
Overview creation journey runs fully once: its light play waits for the real
starting route and API response with the same project seeded before mounting,
then follows the real Projects route to retain the same final providers and
navigation shell. The named endpoint stories also retain both palettes.
This removes repeated behavior work, not stories
or accessibility checks. The authoring policy defaults to full coverage; do not
infer safety from a name or skip every play in light mode.

For a same-source full light reference measurement, run
`STORYBOOK_INTERACTION_PASS=full node --run test-storybook:light`.
Unknown pass values and an appearance-only dark pass fail visibly.

Keep review builds immutable while browser checks run. Run expensive suites
sequentially before measuring load or capturing screenshots. Preserve raw
logs, exit statuses and before artifacts. Measure app startup separately from
all app chunks and Storybook output:

```sh
node scripts/storybook/measure.mjs BEFORE_APP BEFORE_STORYBOOK
node scripts/storybook/measure.mjs AFTER_APP AFTER_STORYBOOK
```

The measurement script parses entry-point HTML with the already installed
Happy DOM parser. Script evaluation and resource loading are disabled, and the
detached window is closed after parsing. HTML case, comments and attribute
quoting cannot change which startup assets are counted. Static imports and
lazy chunks retain their separate accounting; gzip uses Node zlib at level 9.

The source-backed inventory and local verification handoff live under
`docs/handoff/storybook-improvement*`. Catalogue counts are snapshots, not a
cleanup target or proof that every composition branch was exercised.
