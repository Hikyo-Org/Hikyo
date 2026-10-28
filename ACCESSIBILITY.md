# Accessibility

Hikyo aims to make its browser interface, documentation, and contributor
experience usable by people with disabilities. Accessibility barriers are bugs.
Our target is WCAG 2.2 Level AA; this is a development goal, not a claim of
complete conformance or an independent accessibility certification.

## Design and supported environments

The [design system](./DESIGN.md) and [UI specification](./docs/spec/ui-spec.md)
require keyboard interaction, visible focus, accessible names and descriptions,
text alongside status colors, sufficient contrast, reduced-motion support, and
usable touch targets. Changes should preserve these requirements in both light
and dark themes and at desktop and phone sizes.

The browser UI is intended for current Chrome, Edge, Firefox, and Safari with
JavaScript enabled. The automated [browser flow suite](./web/playwright.config.ts)
uses Chromium at desktop and emulated mobile sizes. That coverage does not
establish compatibility with every browser, physical mobile device, or
assistive technology. There is no verified screen-reader and browser support
matrix published yet.

The [CLI](https://hikyo.app/docs/cli-reference/) and
[HTTP API](https://hikyo.app/docs/http-api/) provide alternative ways to perform
many operations. Some account-security ceremonies, including
passkey enrollment, require the browser; alternatives do not establish full
feature parity or remove the need to fix browser barriers.

## Verification and known limitations

The repository contains automated checks for field labels, announcements,
keyboard and focus behavior, contrast, and serious or critical axe findings in
covered browser flows. Their requirements are described in the
[UI specification](./docs/spec/ui-spec.md). Automated checks cover only the
flows they exercise and cannot replace testing with assistive technologies.

Manual screen-reader coverage, browser combinations beyond the automated
Chromium suite, and behavior on physical mobile devices remain verification
gaps. This statement does not certify that every page or interaction is free
of barriers. Confirmed barriers should be tracked in
[accessibility issues](https://github.com/Hikyo-Org/Hikyo/issues?q=is%3Aissue%20is%3Aopen%20accessibility).

## Report an accessibility barrier

Open an [accessibility report](https://github.com/Hikyo-Org/Hikyo/issues/new?template=accessibility.md).
If the template or GitHub's form is itself a barrier, a
[plain issue](https://github.com/Hikyo-Org/Hikyo/issues/new) with whatever details
you can provide is welcome. You do not need to identify a WCAG criterion.

Helpful details include the affected page or command, Hikyo version, what you
were trying to do, expected and actual behavior, and any workaround. Include
your operating system, browser, and assistive technology and their versions
where relevant. Screenshots or recordings are optional.

Public reports must omit secrets, tokens, plaintext configuration values, and
personal information. If the barrier exposes a security vulnerability, use the
[private security channels](./SECURITY.md#reporting-a-vulnerability) instead.
Maintainers will triage reports by their impact on completing a task; response
and fix times depend on maintainer capacity.
