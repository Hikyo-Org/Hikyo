# PR #1086: readable datastore storage

The datastore-volume warning now formats available bytes using binary units
(bytes, KiB, MiB, GiB, TiB, PiB or EiB). The reported example changes from
`38777499648 bytes available` to `36.11 GiB available`.

Formatting belongs to `web/src/routes/OpsDiagnosticBanners.tsx` and applies only
to the `data-volume` finding. The API and CLI retain their exact byte counts.
Utilization, severity, critical thresholds and unknown-capacity messages remain
unchanged. No dependency, schema or storage-policy change is required.

The component regression covers the reported value, unchanged unrelated messages,
escaping and persistent diagnostic semantics. Shared local fast feedback passed
on runtime commit `e77922de348c71c037eb4532b00cf6fd158920f7`: typecheck, lint,
217 test files and 2,035 tests. Repeat with
`scripts/ci/fast-feedback.sh --working-tree` after further edits.

Browser verification used the existing prototype and synthetic critical-capacity
data on `/instance`, at desktop 1440 x 1000 and mobile 390 x 844. Matching
before/after screenshots were captured. The baseline was reconstructed by
restoring the original message in the same rendered DOM. This is display
verification, not measurement of a running datastore. Hosted checks and the
protected merge queue remain separate from local validation.
