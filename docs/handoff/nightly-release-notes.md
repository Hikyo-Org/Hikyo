# Nightly release notes handoff

Task branch: `dunky13/remove-notarization-json`.

The nightly release body copies the human-readable build summary before Apple
evidence is appended. Successful notarization adds a pointer to the
`NIGHTLY-BUILD.txt` release asset. That asset retains the full notarization and
native installation JSON and is still hashed into the signed release manifest.
Failure notes and the subsequent changelog remain intact.

Local validation passed: shared fast feedback (actionlint and ShellCheck),
`go test -p 2 ./scripts/ci -run '^TestNightlyApple' -count=1`, and all 21
`test_apple_notary_state.py` tests. A synthetic execution of the staging step
verified success and failure notes and preservation of evidence in the manifest
input. Synthetic checks do not establish Apple acceptance or publication.

Independent Standards and Spec source reviews reported no findings. The final
wording uses a plain asset filename to satisfy ShellCheck.

Hosted CI and human merge approval remain required. After merge, the next
published nightly should have concise release notes and retain full evidence in
the asset. Existing immutable releases were not edited, and no Apple submission,
release publication, or deployment was performed.
