# Handoff: #454 display-once secret copy

Issue: https://github.com/Hikyo-Org/Hikyo/issues/454. Base:
`5543b70eae3f3851247ce34f842e976c60ad02cf`.

## Contract

- The TOTP enrolment URI and regenerated recovery codes have local `Copy`
  controls while their display-once values are visible.
- TOTP copies the exact `otpauth` URI. Recovery codes copy as one code per line.
- Both controls use `app/clipboard.ts`; missing or blocked browser clipboard
  access becomes an operator-visible refusal instead of an uncaught error.
- Copy success does not acknowledge storage. Recovery-code dismissal remains
  blocked until the operator explicitly checks the existing storage gate.

## Coverage

- Account-security component coverage checks the exact recovery-code and TOTP
  clipboard payloads, success feedback, refusal feedback, and unchanged storage
  gate.
- Local execution was deferred before the first push because host memory
  pressure was high. Run the focused web test, web typecheck, lint, and the full
  repository suite before merge when pressure subsides; exact-head CI remains
  authoritative.
- Generated outputs: none.
