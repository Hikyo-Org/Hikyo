# GitHub adapter live acceptance

5 September 2026. Full `TestGitHubDotComContract`: **PASS**, 574.203 seconds, no skipped live cases. All 18 real GitHub Actions workflows succeeded and their downloaded secret/variable hashes matched.

## Fixed behavior

- Environment secrets and variables use `/environments/{name}/secrets` and `/variables`. Repository and organization artifacts retain `/actions`. The old environment paths returned 404 with a token that succeeded on canonical paths. Seven operations now share the correct namespace; encoded names and GHES prefixes have regression coverage.
- A token missing Variables write can pass the deliberately read-only connection test. Actual first-sync sentinel writes now prove that permission and return named guidance while preserving the underlying 403 and journal outcome.
- GitHub refuses empty variables. Sync rejects empty configuration values by key name before provider or journal effects. Plan remains value-blind; empty secrets have separate create, presence and workflow-consumption proof.
- Measured effective maxima are 48,000 UTF-8 bytes per variable and 47,952 plaintext bytes per secret. Sync rejects overflow before effects. Exact maxima are consumed by real workflows; the next byte is explicitly refused by the provider with 422. No provider internal size-accounting mechanism is inferred.

## Decisions and proof

The fixture remains a small, public, synthetic repository for maintainer acceptance. Enterprise installations do not need this fixture repository. Seven distinct fine-grained PATs separated delivery, denied-permission controls and administrative harness access. All seven were revoked after acceptance and cleanup. Organization permissions are organization-wide, while synthetic artifacts used selected visibility limited to the fixture repository.

Each of repository, organization and protected-environment delivery passed six workflow pairs: empty secret with a nonempty variable, CRLF, lone carriage return, trailing whitespace, Unicode, and distinct exact secret/variable maxima. The complete gate also covered connection identities, permission refusals, provider mutation status contracts, selected recipients, protected-environment preservation and opt-in settings-free environment creation.

Regression tests were red before their fixes. Final local validation: 159 package PASS events, 197 adapter race PASS events, 15 targeted SQLite service PASS events, adapter vet, docs check and docs build. Package/race counts include subtests and each has one intentionally disabled external-contract skip; the separate live run has no skips. Standards and specification reviews both returned CLEAN in round 2 of 3.

The live run used base `a7c084200e5b61049114ce5b85d2cbd1a6061d47` plus the recorded six Go file hashes. All 72 baseline contract dependency inputs were unchanged on then-current main `ee8924e6d1a1d2361343e14744f88f71ecbe58a1`; final modified inputs are pinned separately. This supports carrying scoped acceptance across the base update, not whole-program equivalence. CI and merge status belong to the delivery PR.

Provider cleanup verified no remaining HIKYO-prefixed secrets or variables across the three destinations, zero workflow artifacts, and only the retained `contract` environment. Raw redacted logs and credential custody records remain in the local acceptance artifact directory; no credential values are committed.

Evidence: [portable acceptance record](../reports/1.0/github-live-acceptance.json), [HTML decision report](../reports/1.0/github-environment-api-paths.html), [locked ADR](../adr/github-adapter.md), [fixture repository](https://github.com/Hikyo-Org/hikyo-adapter-acceptance).

Delivery prerequisite PR #678 merged as `0e1ff3c4b1536b5d27f14c5a86da2c3025ede86b`. After rebasing onto it, all 72 baseline GitHub contract dependency inputs and all six modified Go file hashes still match the live-tested inputs. The updated exact-head PR must pass CI before merge; this base comparison does not relabel unrelated whole-program tests.
