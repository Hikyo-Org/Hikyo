# PR #740: Sidebar viewport height

Long settings content must scroll independently of the application navigation.
`web/src/styles/app.css` now sizes the shell to `100vh`, with `100dvh` for
dynamic viewports, and uses a shrinkable grid row. The sidebar has the same
viewport cap, including the mobile drawer.

Validation: web typecheck and all 977 unit tests passed. A Chromium layout
reproduction using the actual stylesheet and long content measured 2509px
before and 800px after in an 800px viewport. Content became independently
scrollable. The mobile drawer measured 844px in an 844px viewport.
The collaborative preview was unavailable, so this was an isolated browser
layout check, not an authenticated application preview.

Cross-provider review was skipped under the trivial CSS diff exception.
Delivery is authorized to merge once required PR checks pass.
