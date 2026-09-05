# #79 release-floor recovery lane

Worktree `one-zero-floor-hardware`, base `f4175a5dbffc63a5d1e34bf450a7a52956a54668`; uncommitted Codex-authored scope. Parent owns review, signed DCO commit, PR, exact-head CI and merge. No production infrastructure changed.

## Decision and result

The live [Marc amendment, 2026-08-20](https://github.com/Hikyo-Org/Hikyo/issues/79#issuecomment-5354009024) explicitly accepts native arm64 4-CPU/4-GB cgroup CI as the floor. Hardware-only instructions in the older brief and spec index were stale. The spec index now links the operative decision. No locked ADR history was rewritten.

Added a manual workflow and isolated native Docker harness for full K2/K3 plus the actual binary's export, recurring drill, destructive restore, status and per-principal reconciliation. Every run requires two independently configured disposable TEST GitHub secrets and separate readonly mode-0600 mounts. See [release runbook](../release/floor-acceptance.md) for exact secret formats and evidence requirements; [HTML report](../reports/1.0/floor-acceptance.html) records choices and why.

Files: `.github/workflows/floor-acceptance.yml`, `scripts/ci/floor-acceptance.sh`, `internal/isolation/floor_acceptance_test.go` (opt-in `flooracceptance` tag), release runbook, HTML report, this handoff and spec index sentence.

## Validation

Native Linux arm64 Docker cgroup v2 run passed: `/tmp/hikyo-floor-evidence-local2`, log `/tmp/hikyo-floor-local2.log`. Effective quota `400000 100000`, RAM `4294967296`, swap `0`, peak RAM `237510656` bytes, no OOM, total runtime 1696 ms. The CLI drill reported success/readable values/minted-revoked credential/RTO met at schema 43. Container exit zero. Image `sha256:1bbec755de07b4267a58eaa0f87d2343330aa7e026e9e40726c5f92a5d3a36c2`. Source-dirty marker true is deliberate: this is local harness validation on an Apple-hosted Linux ARM VM, not physical Pi evidence or final candidate acceptance.

Negative checks: each missing custody input refuses before creating evidence; executing the same image with a 1-CPU quota fails the effective-limit assertion before credential reads. Shellcheck, actionlint and `git diff --check` passed. Initial runtime caught root-owned binary permissions inherited from umask077; the image build now sets its two non-secret executables0555, while custody remains0600. Retest passed.

## Remaining parent actions

Configure secrets `FLOOR_BACKUP_CUSTODY` (JSON identity plus matching recipient) and `FLOOR_ROOT_CUSTODY` (independent64hex test root). Dispatch `floor-acceptance` at reviewed release candidate, verify clean exact-SHA provenance, all subtests, final JSON and no OOM, then link artifact in acceptance record. Existing release app secret is unrelated and was never used. Cleanup removes temporary secret files and the owned container; images and explicit sanitized evidence remain reviewable.

The controller-under-load128MiB gate is separate, coordinated with agent approval_acceptance; OBL-OPERATOR-PI-FIT stays open until that evidence passes. No CPU derating factor has been invented. The existing committed scanner Pi4 artifact only proves its own scanner measurements. This work does not close #79 or cut a release tag.

Parent Standards review and independent Spec/security review: CLEAN. Both verified separate dedicated TEST custody, native architecture plus effective cgroup checks, runtime backup/restore evidence and the honest boundary excluding operator/performance claims. Parent selects merge commits for reviewed stacks to preserve signed ancestry and avoid unnecessary exact-head CI reruns; every PR still requires its own green gate.

CI integration: the cache policy originally refused every runner except ubuntu-latest. The two explicit manual floor workflows now use ubuntu-24.04-arm without shared caches. Parent positive checks and independent negative cache, trigger, and runner mutations passed; this preserves the ordinary runner/cache restrictions.
