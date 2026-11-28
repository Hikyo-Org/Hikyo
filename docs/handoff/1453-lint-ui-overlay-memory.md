# PR #1453: lint ui context memory

## Result

`internal/lint` passed the ui context's `internal/webui/dist` placeholder
through `packages.Config.Overlay`. With any `Config.Overlay` set, go/packages
ignores all export data and type-checks the full dependency closure from source,
so each ui repository load took about 2.5 GB of live heap against 1.1 GB for the
default context. `LoadIn` now passes the overlay to cmd/go as an `-overlay=`
build flag, so dependencies stay on export data.

## Evidence

- Four-context load (`TestContextsSelectTaggedAndPlatformFiles`, macOS
  `/usr/bin/time -l`): peak footprint 5.70 GB to 3.88 GB, max RSS 2.77 GB to
  2.22 GB.
- `TestContextOverlayKeepsDependenciesOnExportData` fails on the old loader
  (45 dependencies from source) and passes on the new one.
- Full `internal/lint` suite: PASS, 1272 s.

## Remaining work

- The suite still caches all four repository loads (about 2 GB live) for the
  whole process. Holding one context at a time would need the repository
  tests to run context-major, which changes top-level test names and the CI
  analysis shard filters. It is not started.
- Full-suite peak footprint is 5.9 GB; that run had no before-fix baseline, and
  transient loads in individual tests were not profiled.
