# PR #686: Hikyo self-configuration handoff

Implementation and local verification complete, 2026-09-06. All 53 server inputs have managed consumers. The full fresh Kubernetes proof passed at generation 19. Exact-head CI and review are tracked in draft PR https://github.com/Hikyo-Org/Hikyo/pull/686. User authorized autonomous implementation and controlled bootstrap rollouts. Do not reopen accepted decisions. No merge or production deployment is authorized or performed.

## Authority and behavior

Each independent instance owns its protected Hikyo organization/project/Production environment, secrets and applied generation. Only HA replicas share an owner. The root management view groups references without copying secrets or granting remote authority. Remote operations use the target instance's existing authenticated interface. Protected access requires instance admin and MFA; Apply/Restore consumes fresh revision/owner/generation/plan-bound passkey or TOTP proof.

Setup imports the effective server environment and configured file contents once. The applied revision then owns configuration. The catalogue has 27 top-level keys: nine mail/channel values, 16 owner settings, secret `HIKYO_NODE_OVERRIDES` with 15 per-node fields, and `HIKYO_BOOTSTRAP_SOURCES`. The generated inventory contains 65 inputs: 53 server, ten client, one command-only and one retired. `server-variable-coverage.json` maps every server input to its actual consumer; the report build enforces exact inventory equality.

Ordinary changes replace the affected mail/application/network/TLS/auth/backup/egress consumers live while preserving counters and owner resources. Development controls require an existing development deployment. The candidate-root alias reloads live without rotating the primary root or retiring wrappers; clear it by removing the optional field, not setting an empty string.

Bootstrap support requires explicit enrollment of a singleton Recreate Kubernetes 1.36 Deployment. It switches same-PostgreSQL database aliases, externally held root keys, complete seven-input upgrade profiles, or separately authorized HA/node identity. An HA-enabled singleton provides coordination, not redundancy. Multi-replica bootstrap, host/systemd/GitOps providers, data migration, image installation and moving operator custody are separate capabilities.

## Runtime invariants

Exact source and old/new topology correspondence are signed through the existing journal. Final MFA rechecks authority, source versions, membership and plan generation. Source changes and actual topology changes require separate plans. Ordinary Apply retains current identity and template fences. First source rollout on an already HA-enabled singleton signs unchanged correspondence.

A partial deployment remains business-fenced. Restore has its own fresh exact MFA and restores external inputs without changing the desired revision. A separate fresh repair must restore runtime authority. Source Restore retains a fenced HA coordinator so ordinary repair recovers actual heartbeat and scheduler leases before another reboot. Actual topology repair requires a controlled replacement. Root finalization refuses while unresolved deployment recovery needs its wrapper; retirement is never automatic.

Upgrade profiles bind all seven enrolled values and immutable public artifact digests. Pure Prepare performs no admission, migration, ciphertext copy or operator rotation. Mutable build/control/custody state is rechecked before Submit. Replacement resolves the selected profile before the existing authoritative signed gate, which retains evidence expiry/nonce and migration authority. Alternative state paths must reference the same actual persistent custody directory.

## Signed checkpoints

Branch: `t3code/dogfood-hikyo-environment`.

- `2a070efb575602f881d67f5ac49d35ce9703fa6c`: expanded baseline and historical kind evidence.
- `0cb6de707439fcbc56e0c0844fbd505d1a05f60b`: development controls and next-root selector.
- `8f93fb5016fce8b2fa01d00cc42f0d82a9c1293a`: SCRAM/readiness/browser fixture repairs.
- `b974c39092e8f4f13182e9e9650349c5f894deae`: seven-input upgrade profiles; `08129e051dfb05976af6aac220040309d6c60819`: singleton topology and chart admission fix.
- `2450a34a999b11525a2d4edd5d09b9a98f534da4`: isolated service fixture scheduling; `c40833b958ae34030e41814c53eebaa3629610aa`: strict config leaf preserved with local wire type and explicit app conversion; `29e76d3cecc08ee3f3b4a062c414fb2b34e389ae`: owned HTTP fixture cleanup.

The final evidence/docs commit follows these checkpoints. Check Git for its exact head; do not assume a runtime build's embedded commit names later test/docs-only commits. Archived D31/D32/D33/D34 patches are already integrated. Do not reapply them. No schema migration or development manifest 137 was introduced.

## Final proof and verification

`docs/reports/self-configuration/validation/controlled-rollout-expanded-kind.json` and `controlled-rollout-expanded-source-manifest.json` record the uninterrupted final run on signed runtime `c40833b9`. Candidate directory: `/tmp/hikyo-rollout-final-candidate-818e`.

- All 795 native/Linux compiled/embed inputs remain byte-identical. Manifest SHA-256: `e64b464a9b375afbae05f6a4ea342c91023a116bdeafc5a6f0b3ae2733f3f766`.
- Linux candidate and final running ELF: `eafed43e5162715e1e435a27cc5f3dfa1e0ecc92705fb86374eb61810675f281`.
- Native operator: `e783b14102e7b87f7d95bdaea6f508ebd1647e4aac5244946cde5cedf2a0c9bf`. Both executables carry the same seven verified signed linker strings.
- Fresh-start/completion harness: `8bdce7cc0aa8c47ad05d41fa860f3eca63e18fa1871b0413497fa2eb73aeeb8d`; every chart input also matches. Evidence JSON SHA-256: `e2e4fecf6b75971f08f7bebd9fe9a9672b1fe6e3fd280c454c7bad6e85bb4ad5`.
- Final generation 19 completed. Command expiry renewed sequence 6 to 7 with MFA count 6 unchanged. Real encrypted drill recovered wrappers/secret and reconciled/minted/revoked a credential. All seven upgrade inputs changed, same custody device/inode `65025:3200808`. Independent session A received 403 before its positive window expired after session B applied policy zero.

Ordinary reload, DB/root replacement, root guard, Restore/fresh repair/later Apply, next-root select/clear and HA identity/source-repair checks all passed in that fresh run. Native scratch transport used eight opaque `verify-full` connections and left zero children. Both exported JSON files passed a recursive 3224-string check against private values and secret markers. No private custody is published. The signed fixture uses temporary test release authority, not production release authority. It does not prove a new release/schema upgrade, data migration, executor process takeover or multi-replica failover.

All eleven combined backend package suites passed with PostgreSQL configured. Current web typecheck, 934 tests and build passed. Focused races, isolation, authority invariants, SQLC regeneration, live chart admission and vet passed. Source and final harness reviews are CLEAN. Detailed commands, timings and source boundaries are in `docs/reports/self-configuration/validation.md`.

CI `34051414948` on `2450a34a` passed browser/closure/isolation checks and full service race in 1192.522 seconds under the unchanged 1200-second limit. Its boundary and HTTP cleanup failures are fixed in c40833b9 and 29e76d3c. The exact HTTP teardown error reproduced locally; final three-fixture race count5 passed 63.398 seconds with original server deadlines and assertions. Public metadata: `validation/config-leaf-boundary.json`, `ha-http-cleanup.json`, and `service-race-scheduling.json`. Read current PR checks for final-head CI; earlier run status is historical.

## Resources and delivery discipline

The report must stay available at http://192.168.0.30:8769/. Report sources and final static HTML live in `docs/reports/self-configuration`; public operator docs are linked from the docs navigation. The report is standalone and embeds fonts/styles/scripts. A host-origin LAN HTTP check does not imply a separate-device network test.

Final owned kind context `kind-hikyo-rollout-818e`, namespace `hikyo-rollout-a06347c9`: server/PostgreSQL/executor replicas verified zero after proof, owned registry on port 56509 stopped. All three container restart counts were zero before cleanup. Private custody `/var/folders/vd/czrqg5h95pz6gyrt813t33_w0000gn/T/hikyo-rollout-kind-frn57thm` and log `/tmp/hikyo-rollout-final-expanded-proof.log` are retained. Historical fixtures are stopped; their interrupted/resumed evidence is not the final proof. Do not expose private logs, passwords, TOTP seeds, operator keys or root escrow.

Unrelated local fixtures `hikyo-self-config-818e` on 56488 and `hikyo-self-config-scram-818e` on 56489 were preserved. The SCRAM DSN is in private `/tmp/hikyo-self-config-scram-818e.json`; never print it. Keep report servers running. Always check explicit context and ownership before touching retained resources.

Every commit requires DCO and configured crypto signing. Verify the entire PR range before push and GitHub Verified afterward. Keep PR draft. User has not authorized merge or production deployment.
