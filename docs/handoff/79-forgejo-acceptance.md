# #79 real Forgejo acceptance

> Historical local artifact names below identify the original run only and are not included in this checkout. Recorded results were not rerun for this path cleanup.

Base: `54c58bbdbf7abb90228482718a8bf4f0844f6ea4`. Branch `fix/one-zero-forgejo-acceptance`. Parent owns review, signed commit, CI and merge. Production cleanup changes are limited to each provider client's existing Forget boundary.

A second resource leak was found at the provider module-release boundary: both clients create private transports per outbox attempt but Forget left their idle connections alive. New real TLS regressions failed in both packages before the fix; each Forget now closes idle connections while preserving credential cleanup and GitHub release-once state cleanup. Full provider packages pass normal and race tests. The fixture cleanup now uses a bounded live context and reports scrub failures, then forgets its client.

The external lifecycle was SQLite-only even with a PostgreSQL DSN. It now uses named SQLite/PostgreSQL subtests and existing isolation openers. All real-provider lifecycle assertions remain intact; numbered placeholders work on both drivers and boolean TRUE preserves dialect correctness.

Actual Forgejo 16.0.3+gitea-1.22.0 over verified private TLS passed SQLite (0.75s) and PostgreSQL 18.6 (1.13s), zero skips. A Go source overlay restoring the exact base fixture passed its original SQLite-only lifecycle (0.57s). Both runs execute native Linux arm64 test binaries cross-compiled with Go 1.27.0. The adapter receives only write:repository on a non-admin user; separate fixture provisioning authority never reaches the adapter. After both runs the isolated provider has zero secret/variable rows.

[HTML decision report](../reports/1.0/forgejo-acceptance.html) and [machine-readable hashes/results](../reports/1.0/forgejo-acceptance/result.json) describe the exact tested source. These are pre-commit test results, not a final release verdict. GitHub's seven-token dedicated harness remains an external gate.

Validation: canonical real-provider lifecycle passes both engines; exact-base SQLite replay passes; `GOMAXPROCS=2 go vet -p 1 ./internal/adapter/forgejo ./internal/adapter/githubactions ./internal/isolation`, both full provider package suites with and without `-race`, five structural-lint checks, and `git diff --check` pass. Artifact redaction check confirms no delivery token or PostgreSQL password appears in report, result, handoff or logs.

The original isolated fixture used Docker names `hikyo-forgejo-acceptance-5405-server`, `hikyo-forgejo-acceptance-5405-postgres`, volume `hikyo-forgejo-acceptance-5405-data`, and network `hikyo-forgejo-acceptance-5405`. Only host loopback port 47831 was exposed. Private credentials and 2-day certificate keys were stored outside the repository in owner-only custody; never copy private fixture files into public artifacts. Public result metadata/logs were copied safely.

The untracked local replay script read private environment material without printing it, ran the compiled isolation test in the owned network, and redacted token/password values from errors. Neither that script nor the private fixture directory is included in this checkout. Recreate a dedicated fixture and rebuild its binary from the final candidate before a new replay, then update candidate/patch/binary hashes. Remove only fixture resources you own after verification; this historical note does not establish the current presence or ownership of Docker resources.
