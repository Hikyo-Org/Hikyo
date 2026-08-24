# Handoff: #444 safe clipboard writes

Issue: https://github.com/Hikyo-Org/Hikyo/issues/444. Base:
`377a3f39b005077cb52c26f51ca431b08e2c401a`.

## Contract

- `app/clipboard.ts` owns browser clipboard writes and returns the closed
  outcomes `ok` or `refused`.
- Missing clipboard APIs, synchronous throws, and rejected writes all return
  `refused`; callers never need to build a promise chain from an unsafe browser
  property access.
- Machine-credential copy preserves its existing success and refusal messages.
  A refused copy leaves the display-once value visible and its stored
  confirmation unchecked.
- Values copy and best-effort clipboard clearing use the same safe boundary;
  existing disclosure notices and clear timing remain unchanged.

## Coverage

- Helper tests cover successful writes, rejected writes, synchronous throws,
  and an absent clipboard API.
- The mint-dialog regression covers the operator-visible refusal, retained
  display-once value, and unchecked stored confirmation.
- Local web validation was deferred before the first push because host memory
  pressure remained high. After pressure subsided, Vitest passed all 43 files
  and 337 tests; TypeScript typechecking and the production Vite build also
  passed.
- Generated outputs: none.
