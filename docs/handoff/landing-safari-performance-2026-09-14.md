# Landing page Safari performance investigation

Investigated against `55ba25b985e8f42f4026571fb10db31783798bc7` on 14 September
2026. The user confirmed the landing page, required animations to remain, approved
local comparison experiments, and approved the precache cleanup recommendation.

## Implemented change

The service worker previously precached every emitted asset, even CSS, scripts,
and fonts used only by design prototypes. Excluding the prototype HTML directories
did not exclude their dependencies emitted into the shared `_astro/` directory.

The Astro integration in [pwa-asset-graph.mjs](../site/scripts/pwa-asset-graph.mjs)
captures the build's static and dynamic dependency edges. It also connects emitted
CSS to its font and image references: Vite puts that metadata on server chunks,
which production HTML does not reference directly. Every production HTML document
is a root; public files and assets shared with production stay cached. Worker
generation consumes the graph in the same build, avoiding stale on-disk metadata.
Filename matching is deliberately conservative: incidental mentions can retain an
extra asset rather than remove a required dependency.

| Local production build | Before | After |
| --- | ---: | ---: |
| Precached files | 222 | 108 |
| Uncompressed precache bytes | 11,377,310 | 9,004,384 |
| Font files | 134 | 32 |
| Stylesheets | 13 | 2 |
| JavaScript files | 19 | 18 |
| HTML pages | 49 | 49 |

This removes 114 precache entries and 2,372,926 uncompressed bytes, approximately
21% of the install payload. These are build sizes, not measured transfer savings
under HTTP compression. Complete offline documentation and the search index remain
available. No explicit offline-download UI was introduced because the existing
contract includes unvisited docs pages offline.

## Animation experiments, local only

No production animation or visual source changed. The primary candidate is the
full-document `body::before` repeating radial gradient in
[landing.css](../site/src/styles/landing.css). Its registered `--rake-gap` property
animates continuously over 300 seconds. Changing gradient stops requires repaint
work; a longer duration does not reduce updates to one per animation cycle.
The sticky header and comparison table also use backdrop blur.

A snapshot of the baseline production build was served over local HTTP. Separate
Playwright Chromium and WebKit instances used 1280 by 800 viewports at DPR 2, with
service workers blocked to isolate rendering. After fonts and entry motion settled,
three rounds alternated variant order. Each round measured 2.5 seconds idle and
2.5 seconds scrolling at 2 CSS pixels per millisecond using `requestAnimationFrame`.
Chromium main-thread work is the CDP `Performance.TaskDuration` delta; frame gaps
are animation-frame callback intervals, not direct display-presentation timings.

| Variant | Chromium idle main-thread ms | Chromium scrolling main-thread ms |
| --- | ---: | ---: |
| Original gradient animation | 1,156.4 | 198.3 |
| Frozen background, diagnostic control | 33.7 | 39.2 |
| Animated opacity, fixed groove spacing | 30.6 | 45.0 |
| Original animation, backdrop blur removed | 1,257.7 | 204.2 |

Values are medians of three samples. The opacity experiment fixes `--rake-gap` at
32px and animates opacity from 0.30 to 0.45, retaining the original 300-second
duration, negative 75-second delay, easing, and alternating repetition. The groove
strokes retain their thickness, but the visual effect changes from spacing motion
to a slow change in intensity. This is a candidate for user selection, not an
approved replacement. All hero and section entry animations remain unchanged.

WebKit stayed near 60 callbacks per second for all variants, with median p95 frame
gaps of 19ms. The reported Safari lag was not reproduced. Native Safari automation
was unavailable because Allow remote automation was disabled. These results show
a substantial Chromium main-thread cost reduction, not a proven Safari frame-rate
or battery improvement. Removing blur alone did not reduce idle main-thread work.

A temporary local comparison server offers Original, Frozen control, and Animated
opacity, plus an optional 15-second visual demonstration. Its experiment controls
and overrides are outside the repository and are not part of production builds.

The user subsequently reported no visible background animation in Safari, no
sluggishness on their own machine, and no visible effect from the opacity demo
even in Chrome. The benchmark therefore does not establish a useful visual
replacement or explain the original Safari report. The user selected only the
precache optimization for delivery; the original animations are retained.

## Validation and delivery

- Full `scripts/ci/verify-docs.sh` passed under the repository's Node 26.7.0 pin:
  Astro check reported zero errors, warnings, or hints; build, dependency checks,
  graph regression tests, CSP, OSS, offline docs, live-doc fixtures, and fallback
  channel fixtures passed.
- Offline coverage checks retained CSS font/image references, unvisited page
  hydration, fonts, and actual search-dialog results with the HTTP server stopped.
  Generated prototype CSS is checked against CacheStorage, including retention of
  styles shared with production.
- The enhanced browser regression fails against the saved baseline with
  `prototype-only emitted CSS leaked into the precache` and passes against the
  changed build. The parent reran the final browser test successfully.
- Comparing SHA-256 hashes of all baseline and changed build outputs found only
  `sw.js` changed. All HTML, styles, scripts, fonts, and images remained identical.
- Parent source review checked shared and dynamic dependencies, CSS/font edges,
  fresh graph lifetime, and existing offline/routing contracts.
- Cross-provider review was skipped: no current Claude quota reply arrived within
  the policy's three-minute window. This is not a cross-provider CLEAN verdict.
- Delivery scope: signed commit and branch push of the precache cleanup and its
  regression coverage. Merge and deployment are not authorized by this request.

No animation replacement is selected for this change. A future Safari diagnosis
needs reproduction on the device that experienced the original report.
