# Issue #458 — local binding identity validation

Issue: https://github.com/Hikyo-Org/Hikyo/issues/458. Base:
`5543b70e`.

## Contract

- The federated-binding form refuses blank or whitespace-only `issuer` and
  `subject` values before claim validation or any create request.
- Each refusal names the missing byte-exact identity field and states that
  nothing was bound.
- Preset-filled happy paths, request payloads, routes, persistence, and
  generated outputs remain unchanged.

## Coverage

- The machine-access Playwright flow blanks each field through the public form,
  submits it, checks the local refusal, and proves no binding-create POST fired.
- Local test execution was deferred before the first push because host memory
  pressure remained high; exact-head CI is required before merge.
