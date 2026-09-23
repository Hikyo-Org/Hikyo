# Handoff: Dependabot alerts 13, 22, 23, 28, 30 (and image-size)

Branch: `fix/dependabot-alerts`.

## Scope

| Alert | Severity | Package | Workspace | Advisory |
|---|---|---|---|---|
| 30 | critical | expr-eval 2.0.2 | web (dev) | GHSA-q9v2-7m5w-4693 code execution |
| 23 | high | expr-eval 2.0.2 | web (dev) | GHSA-8gw3-rxh4-v6jx prototype pollution |
| 22 | high | expr-eval 2.0.2 | web (dev) | GHSA-jc85-fpwf-qm7x unrestricted evaluate functions |
| 13 | high | js-yaml 4.3.1 | clients/ts (dev) | GHSA-2883-xcg3-v3hh merge-key CPU use |
| 28 | medium | devalue 5.9.0 | docs/site (runtime) | GHSA-9rgm-9g3h-6x36 malformed-input DoS |
| none yet | high x2 | image-size 1.2.1 | web (dev) | GHSA-w3rx-r6r6-pgpr and a JXL/HEIF sibling, found by `pnpm audit` |

## Decisions

- **expr-eval**: unmaintained, no patched release. It arrives through
  `@open-pencil/cli` -> `@open-pencil/core` (latest 0.15.1 still depends on
  it), whose `calc` tool (the design CLI/MCP) passes caller text to
  `parser.evaluate`, so the evaluate-path advisories are reachable. Aliased to
  `expr-eval-fork@3.0.3`, which carries the fixes and keeps the same default
  export (`{ Parser, Expression }`). The calc tool gives identical results on
  six sample expressions before and after, and refuses
  `constructor.constructor("return process")()`. (`**` fails on both; the
  upstream tool description is wrong, not a regression.)
- **image-size**: pptxgenjs 4.0.1 (via `@open-pencil/core`) declares
  `image-size ^1.2.1` but never loads it: both its builds `require('sizeof')`.
  Every 1.x release is vulnerable, so overriding to 2.0.4 is a major bump on a
  module nothing imports.
- **js-yaml**: raised the existing override from 4.3.1 to 4.3.2 (see
  `dependabot-5-js-yaml.md`).
- **devalue**: new override to 5.9.2; astro 7.2.8 and @astrojs/react 6.0.4
  accept it.

The changed-path classifier already treats all three `pnpm-workspace.yaml`
files as full-suite config.

## Verification

- `pnpm audit`: no known vulnerabilities in web, clients/ts, docs/site.
- web: typecheck, 1114 unit tests, production build.
- clients/ts: `pnpm run verify`.
- docs/site: `pnpm run build`.

## Follow-up

Drop the expr-eval alias once `@open-pencil/core` moves off `expr-eval`, and
the image-size override once pptxgenjs requires a patched range.
