# CLI log messaging handoff

## Delivered

- Interactive `hikyo server` shows a labeled ready summary on stdout, including
  version, app URL, bound address, operational URL, and mode. The summary emits
  only after both HTTP serving goroutines start. Redirected stdout remains free
  of diagnostics.
- Timestamped text logs in development and structured JSON logs in production
  remain authoritative operational output on stderr, so production JSON logs
  do not mix with human prose.
- Interactive `hikyo version` shows readable build metadata. Redirected
  `hikyo version` retains its legacy one-line release identity while consumers
  migrate; `hikyo --version` is the exact machine-friendly version value.
- `hikyo about` and `hikyo welcome` show the supplied full 44-by-80 artwork;
  the embedded bytes are pinned by SHA-256 in tests.
- Explicit and passive update checks show labeled installed/latest/channel and
  release information.

## Verification

- Focused console, command, and update tests pass.
- Redaction lint and isolation invariants pass.
- `go test ./... -count=1`: 4,029 tests passed across 69 packages.
- Live development-server startup showed both timestamped logs and the readable
  server summary with resolved ephemeral listener ports.
