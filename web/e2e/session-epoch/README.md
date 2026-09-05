# Browser session ownership regression

Run from `web/` with the repository's Node version:

```sh
node node_modules/@playwright/test/cli.js test --config e2e/session-epoch.config.ts
```

The standalone configuration starts Vite on port 4319 and uses installed
Playwright Chromium. It does not start the backend or the full flow suite.

The harness imports the real AuthProvider, root API transport, TanStack query
client and workspace store. Only API responses are mocked. Two Chromium tabs
share the real secure companion cookie. The tests hold replacement identity
responses until both tabs discard the old account's query data, mutation cache,
component disclosure and workspace bearer. Delayed old-account responses must
reject after the replacement identity settles.

Both BroadcastChannel and the storage-event fallback are exercised. Screenshots
of both replacement tabs are written under `web/test-results/session-epoch/`.
