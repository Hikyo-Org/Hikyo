# Trusted-source release admission

This package implements the source qualification path for
[#1046](https://github.com/Hikyo-Org/Hikyo/issues/1046). Its scope is the existing
issue 158 acceptance series. Source qualification, owner release approval,
Root implementation/configuration adoption, resource authorization, protected
deployment approval and protected merge are separate gates. No default live
transport, original authenticator, approval, release or execution grant exists.

The [policy contract](../../../docs/development/release-admission.md) describes
the authority and lifecycle rules. The pinned specification is in `spec/`.
The original R2 artifacts and frozen historical package remain untouched.

## Check the source

From the repository root, create a separate Python environment and install the
explicit dependency. PyYAML parses the workflow topology; duplicate YAML keys,
anchors and aliases refuse.

```sh
RELEASE_ADMISSION_VENV="$(mktemp -d)"
python3 -m venv "$RELEASE_ADMISSION_VENV"
"$RELEASE_ADMISSION_VENV/bin/python" -m pip install -r scripts/ci/release-admission/requirements.txt
"$RELEASE_ADMISSION_VENV/bin/python" -B scripts/ci/release-admission/run_checks.py --unit
```

The unit suite checks record/lifecycle contracts, complete collector replay,
durable decision storage, workflow topology and source loading. CI runs this
suite in the existing lint job, preserving the exact
job inventory. Local `fast-feedback.sh --working-tree --plan` reports
`releaseAdmission: true` for package changes and runs the same finite suite.

Full configured qualification, including adapter contexts, additionally requires the separately retained
historical originals and their exact source fixtures. Set
`HIKYO_158_ARCHIVE_ROOT` to the explicitly selected archive root and
`HIKYO_158_NODE_PATH` to the reviewed Node 26.7.0 executable, then run:

```sh
"$RELEASE_ADMISSION_VENV/bin/python" -B scripts/ci/release-admission/run_checks.py \
  --qualify --archive-root "${HIKYO_158_ARCHIVE_ROOT:?Select the exact retained historical archive}"
```

`HISTORICAL-ARCHIVE-PINS.json` records the confined relative names, sizes and
raw hashes. Missing, changed, aliased or symlinked originals refuse. The index
is integrity evidence; it does not authenticate an owner decision. Qualification
never substitutes redacted bytes, updates historical timestamps or treats a
missing archive as a passing skipped test. Raw historical owner/native payloads
are not repository fixtures.
The adapter checks the exact Node executable digest. An arbitrary executable
or an unreviewed runtime does not qualify the native transport compiler.
The process qualification also needs a Python/SQLite build that supports the
holder's forked read-only SQLite access. On macOS, qualification exposed a
post-fork crash in a Python 3.14.4 build linked to Apple's system SQLite.
The checked environment uses Python 3.14.9 linked to SQLite 3.54.1 and the pinned
PyYAML dependency. Select a validated Python executable when creating the
environment; a crashing child is a failed qualification.

## Static source and runtime data

`IMPLEMENTATION-MANIFEST.json` and `EXECUTION-MAP.json` describe static source.
The loader verifies independently reviewed raw map/manifest pins, every source
file and fixed module roles before imports. Runtime release/configuration data
cannot select arbitrary imports. Every affected consumer pins the actual
successor source. Model instances remain separate, and every consumer within
an attempt receives the same configured instance.

In a fresh interpreter, put this package on the import path and call
`load_successor.bootstrap_helpers(reviewed_map_sha256=...,
reviewed_manifest_raw_sha256=...)` before importing `bounded`, `release_policy`,
`release_records`, `release_collector` or `workflow_contract` and their consumers.
Supply pins from independent source review through the trusted Root bootstrap;
do not compute operational review pins from the package being loaded.
`load_unconfigured` performs the same bootstrap when no public helper has yet
been imported. See [the supported import sequence](RECORDS-API.md#imports-and-bounded-helpers).

The loader compiles verified helper snapshots and retains their shared module,
class and function identities across repeat loads. It refuses ordinary pre-imports,
including same-path bytecode cache objects, removed/replaced registered modules,
another package root or changed helper snapshots. On refusal, start a fresh
interpreter with the correct externally reviewed pins. Do not clear `sys.modules`,
reload helpers or replace objects while callers retain their identities.
The unit runner and pipeline fixtures bootstrap before dependent imports using
locally computed integrity pins only; passing these checks grants no source
review, adoption or operational authority. Direct focused pipeline qualification
through `python -B -m unittest test_two_release_pipeline...` from this directory
uses the same fixture bootstrap and still requires the explicit archive/runtime.

Maintainers regenerate static pins only after source changes, before independent
exact-source review. Sealing computes integrity metadata; it grants no adoption.
Ordinary release updates supply new evidence/configuration data under unchanged
implementation/map bytes. They do not regenerate code pins. The record graph is
policy/code -> release -> owner approval -> Root adoption -> genesis -> composite
configuration -> manifest -> attempt binding -> preparation -> claim.

The original eight-field resource configuration remains separately bound. Each
claim, forward context, holder/driver packet, bind, result and cleanup context
retains `attemptTrustBindingSHA256`. A different release or pair cannot replace
a reserved attempt, refund its charges or enable a retry.

## Operator sequence

1. Independently review exact implementation bytes and adopt their static map
   through the existing Root custody mechanism. Use the explicit loader and
   configuration APIs; their default original-authority boundary refuses.
2. Explicitly select one already merged release and its queue run/attempt.
   Collect complete REST/GraphQL, source/range/job and change-review originals.
   Authenticate the whole captures separately. Review the complete trusted diff
   from the prior approved release or literal historical anchor.
3. Obtain the owner's genuinely later explicit approval of the exact release,
   policy and unchanged resource scope. Root authenticates it and adopts the
   exact configuration, creating a status genesis under the authoritative
   decision stream. Hashes and successful CI do not supply this consent.
4. Select the exact candidate/main pair and both complete CI runs. Later main
   needs its own queue proof, even when all fifteen trusted source hashes match.
   Bind the composite configuration, source manifest and immutable attempt
   before preparation. Keep whole capture originals outside nested model
   records, with exact replay correspondence and the original bounds.
5. At preparation, reserve and each forward boundary, authenticate current
   authoritative status and stream watermark. The existing resource, billing,
   identity/MFA, lease, once-marker, physical-lock and deployment gates still
   apply. This package does not perform those operational approvals.

Root safety holds append their reason and evidence. Owner withdrawal can append
after either eligible or held. Neither old state returns to eligibility.
Recovery creates a new adoption/genesis. Root-only recovery may revalidate an
unchanged unexpired standing grant only with a new single-use recovery decision,
remediation review and complete proof of no withdrawal. After withdrawal, a
genuinely later owner reauthorization must name the real withdrawal, affected
adoption and prior approval. Reserved attempts never revive.

`DecisionJournal` supplies durable CAS mechanics for the existing authority
adapter. It persists head, global watermark and decision consumption together.
The adapter must authenticate the complete native stream and exact checkpoint
under the lock. A cache or local journal alone cannot assert authority. An
interrupted write refuses until exact pending-event recovery is independently
reauthenticated; the old decision is neither discarded nor spent twice.
Recovery retains a no-follow descriptor, rechecks the exact pending bytes after
authentication, and syncs file data before publication. The journal directory
must remain owned by the custody adapter with exclusive write access. Advisory
locks and byte rechecks cannot make a hostile filesystem immutable.

Cleanup explicitly reconstructs valid original action-time source/authority,
then requires current exact-resource owner/MFA/quiescence/survivor safety proof.
Main advancement or present withdrawal cannot strand that cleanup. Reconstruction
grants no new forward work. Unsafe pinned cleanup tooling requires a separately
reviewed remedy.

The literal 176+32=208 charges, one key/one dispatch, four old key schedules,
all stricter budget/reserve rules, horizon, cutoffs, 120s/60s/55s/2s windows and
existing byte/request/time/lineage bounds remain unchanged. No release approval
renews an expired bank. Genuine real-release authentication/adoption and AWS
acceptance remain separately authorized work.
