#!/usr/bin/env python3
"""Bound Actions checkpoints for nightly notarization, never release authority.

Only the original trusted workflow can produce a checkpoint. The recovery
worker has Actions permission only, and reruns failed jobs of that same run.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import zipfile


WORKFLOW = ".github/workflows/nightly.yml"
SCHEMA = "hikyo.dev/apple-notary-checkpoint/v1"
STAGES = {"prepared": 0, "submitted-arm64": 1, "submitted-x86_64": 2, "submitted": 3, "finalized": 4}
FILES = {f"{arch}-{suffix}" for arch in ("arm64", "x86_64")
         for suffix in ("receipt.json", "notary-log.json", "submitted.dmg")}
MAX_BYTES = 512 * 1024 * 1024


def require(condition, message):
    if not condition:
        raise ValueError(message)


def parse_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, "duplicate JSON key")
            result[key] = value
        return result

    def invalid(_value):
        raise ValueError("nonfinite JSON value")

    return json.loads(raw, object_pairs_hook=pairs, parse_constant=invalid)


def api(endpoint, *, method="GET", binary=False, paginate=False):
    command = ["gh", "api", "--method", method, endpoint]
    if paginate:
        command.extend(["--paginate", "--slurp"])
    result = subprocess.run(command, capture_output=True, check=False, timeout=120)
    require(result.returncode == 0, "GitHub API operation failed")
    return result.stdout if binary else parse_json(result.stdout)


def identity(environ=os.environ):
    result = {"repository": environ["GITHUB_REPOSITORY"],
              "run_id": environ["GITHUB_RUN_ID"],
              "attempt": environ["GITHUB_RUN_ATTEMPT"],
              "source_commit": environ["GITHUB_SHA"],
              "channel": environ.get("APPLE_RELEASE_CHANNEL", "nightly"),
              "source_ref": environ["GITHUB_REF"]}
    require(re.fullmatch(r"[\w.-]+/[\w.-]+", result["repository"], re.ASCII), "invalid repository")
    for key in ("run_id", "attempt"):
        require(re.fullmatch(r"[1-9][0-9]{0,17}", result[key]), "invalid run identity")
    require(re.fullmatch(r"[0-9a-f]{40}", result["source_commit"]), "invalid source")
    require(result["channel"] in ("nightly", "stable"), "invalid channel")
    return result


def trusted_run(run, repository, channel="nightly"):
    source_ok = (run["path"] == WORKFLOW and run["head_branch"] == "main" and
                 run["event"] in ("schedule", "repository_dispatch")) if channel == "nightly" else (
                     run["path"] == ".github/workflows/release.yml" and run["event"] == "push" and
                     re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+.*", run["head_branch"]) and
                     "-nightly." not in run["head_branch"])
    require(source_ok and
            run["repository"]["full_name"] == repository and
            run["head_repository"]["id"] == run["repository"]["id"], "untrusted workflow run")


def artifacts(repository, run_id):
    pages = api(f"repos/{repository}/actions/runs/{run_id}/artifacts?per_page=100", paginate=True)
    return [item for page in pages for item in page["artifacts"]]


def select_checkpoint(items, run_id, before_attempt=None):
    candidates = []
    pattern = rf"apple-notary-state-{run_id}-([1-9][0-9]*)-({'|'.join(STAGES)})"
    for item in items:
        match = re.fullmatch(pattern, item["name"])
        if match and (before_attempt is None or int(match[1]) < before_attempt):
            candidates.append((int(match[1]), STAGES[match[2]], item))
    if not candidates:
        return None
    candidates.sort(key=lambda item: (item[0], item[1]))
    require(len({(item[0], item[1]) for item in candidates}) == len(candidates), "duplicate checkpoint stage")
    selected = candidates[-1][2]
    require(not selected["expired"], "latest checkpoint expired; refuse older state")
    return selected


def read_checkpoint(artifact, repository, run_id):
    require(0 < artifact["size_in_bytes"] <= MAX_BYTES, "checkpoint size exceeds bound")
    raw = api(f"repos/{repository}/actions/artifacts/{artifact['id']}/zip", binary=True)
    require(len(raw) <= MAX_BYTES and artifact["digest"] == "sha256:" + hashlib.sha256(raw).hexdigest(),
            "Actions checkpoint digest mismatch")
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        require(archive.namelist() == ["checkpoint.tar"], "unexpected checkpoint ZIP entries")
        entry = archive.getinfo("checkpoint.tar")
        require(entry.file_size <= MAX_BYTES, "checkpoint expansion exceeds bound")
        raw_tar = archive.read(entry)
    result = {}
    with tarfile.open(fileobj=io.BytesIO(raw_tar), mode="r:") as archive:
        for entry in archive:
            require(entry.name in FILES | {"checkpoint.json"} and entry.name not in result and
                    entry.isfile() and 0 <= entry.size <= (256 * 1024 * 1024 if entry.name.endswith(".dmg") else 2_000_000),
                    "unsafe checkpoint TAR entry")
            result[entry.name] = archive.extractfile(entry).read()
    for arch in ("arm64", "x86_64"):
        require({f"{arch}-receipt.json", f"{arch}-submitted.dmg"} <= result.keys(), "incomplete checkpoint")
    record = parse_json(result.pop("checkpoint.json"))
    require(record["schema"] == SCHEMA and record["repository"] == repository and
            record["run_id"] == str(run_id) and type(record["recovery_pending"]) is bool and
            record["stage"] in STAGES and
            artifact["name"] == f"apple-notary-state-{run_id}-{record['attempt']}-{record['stage']}",
            "checkpoint identity mismatch")
    require(record["files"] == {name: hashlib.sha256(data).hexdigest() for name, data in result.items()},
            "checkpoint inventory mismatch")
    for arch in ("arm64", "x86_64"):
        receipt = parse_json(result[f"{arch}-receipt.json"])
        require(receipt["schema"] == "hikyo.dev/apple-notarization-receipt/v2" and
                receipt["repository"] == repository and receipt["original_run_id"] == str(run_id) and
                receipt["source_commit"] == record["source_commit"] and
                receipt["architecture"] == arch and receipt["channel"] == record["channel"] and
                receipt["source_ref"] == record["source_ref"] and
                receipt.get("input_binding", "actions-handoff") == "actions-handoff" and
                receipt["submitted_sha256"] == hashlib.sha256(result[f"{arch}-submitted.dmg"]).hexdigest(),
                "receipt checkpoint binding mismatch")
    return record, result


def snapshot(directory, destination, stage, pending, current):
    files = {}
    receipts = []
    for path in directory.iterdir():
        require(path.name in FILES and path.is_file() and not path.is_symlink(), "unexpected checkpoint file")
        files[path.name] = path.read_bytes()
    for arch in ("arm64", "x86_64"):
        require({f"{arch}-receipt.json", f"{arch}-submitted.dmg"} <= files.keys(), "missing stage intent")
        receipt = parse_json(files[f"{arch}-receipt.json"])
        receipts.append(receipt)
        require(receipt["schema"] == "hikyo.dev/apple-notarization-receipt/v2" and
                receipt["source_commit"] == current["source_commit"] and receipt["repository"] == current["repository"] and
                receipt["original_run_id"] == current["run_id"] and receipt["channel"] == current["channel"] and
                receipt["source_ref"] == current["source_ref"] and receipt["architecture"] == arch and
                receipt.get("input_binding", "actions-handoff") == "actions-handoff" and
                receipt["submitted_sha256"] == hashlib.sha256(files[f"{arch}-submitted.dmg"]).hexdigest(),
                "unbound checkpoint receipt")
        # Step outputs can disappear on interruption. A retained UUID with an
        # unresolved provider status still requires safe read-only continuation.
        if receipt.get("phase") in ("Prepared", "Submitting") and receipt.get("submission_id") is None:
            pending = True  # Continue safely; never infer whether Apple saw it.
        if receipt.get("phase") == "Submitted":
            require(re.fullmatch(r"[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}", receipt.get("submission_id", "")),
                    "invalid retained submission UUID")
            if receipt.get("status") in ("Unknown", "In Progress"):
                pending = True
        if (receipt.get("phase") == "Submitted" and receipt.get("status") == "Accepted" and
                (receipt.get("diagnostics", {}).get("availability") == "unavailable" or
                 (receipt.get("diagnostics", {}).get("availability") == "available" and
                  receipt.get("finalization_state", "NotStarted") != "Complete"))):
            pending = True
    if any(receipt.get("finalization_state") == "Blocked" for receipt in receipts):
        pending = False
    record = dict(current, schema=SCHEMA, stage=stage, recovery_pending=pending,
                  files={name: hashlib.sha256(data).hexdigest() for name, data in files.items()})
    files["checkpoint.json"] = json.dumps(record, sort_keys=True).encode()
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=destination.parent, delete=False) as temporary:
        temporary_name = Path(temporary.name)
    try:
        with tarfile.open(temporary_name, mode="w:") as archive:
            for name, raw in sorted(files.items()):
                entry = tarfile.TarInfo(name)
                entry.size, entry.mode = len(raw), 0o600
                archive.addfile(entry, io.BytesIO(raw))
        temporary_name.replace(destination)
    finally:
        temporary_name.unlink(missing_ok=True)


def unstarted_architectures(files, current):
    prepared = {arch: parse_json(files[f"{arch}-receipt.json"]) for arch in ("arm64", "x86_64")}
    prepared = {arch: receipt for arch, receipt in prepared.items()
                if receipt.get("phase") == "Prepared" and receipt.get("submission_id") is None}
    if not prepared:
        return []
    earliest = min(int(receipt["original_run_attempt"]) for receipt in prepared.values())
    latest = int(current["attempt"])
    require(1 <= earliest < latest, "invalid original preparation attempt")
    if latest - earliest > 12:
        return []  # Bounded proof lookup; UUID reconciliation remains available.
    for attempt in range(earliest, latest):
        pages = api(f"repos/{current['repository']}/actions/runs/{current['run_id']}/attempts/{attempt}/jobs?per_page=100", paginate=True)
        jobs = [job for page in pages for job in page["jobs"] if job["name"] == "apple-notarize"]
        if len(jobs) != 1:
            return []
        job = jobs[0]
        require(job["run_id"] == int(current["run_id"]) and job["run_attempt"] == attempt and
                job["head_sha"] == current["source_commit"], "previous job identity mismatch")
        for arch, receipt in list(prepared.items()):
            if attempt < int(receipt["original_run_attempt"]):
                continue
            steps = [step for step in job["steps"] if step["name"] == f"Submit {arch} once or reconcile its UUID"]
            if not (len(steps) == 1 and steps[0]["status"] == "completed" and steps[0]["conclusion"] == "skipped"):
                prepared.pop(arch)
    return sorted(prepared)


def no_previous_upload(current):
    # Creating initial intents on a later attempt is safe only with positive
    # skipped-job/step proof for every prior attempt. Missing checkpoint data or
    # empty Apple history never grants this permission.
    latest = int(current["attempt"])
    if latest > 13:
        return False
    for attempt in range(1, latest):
        pages = api(f"repos/{current['repository']}/actions/runs/{current['run_id']}/attempts/{attempt}/jobs?per_page=100", paginate=True)
        jobs = [job for page in pages for job in page["jobs"] if job["name"] == "apple-notarize"]
        if len(jobs) != 1:
            return False
        job = jobs[0]
        require(job["run_id"] == int(current["run_id"]) and job["run_attempt"] == attempt and
                job["head_sha"] == current["source_commit"], "previous job identity mismatch")
        if job["status"] == "completed" and job["conclusion"] == "skipped":
            continue
        for arch in ("arm64", "x86_64"):
            steps = [step for step in job["steps"] if step["name"] == f"Submit {arch} once or reconcile its UUID"]
            if not (len(steps) == 1 and steps[0]["status"] == "completed" and steps[0]["conclusion"] == "skipped"):
                return False
    return True


def restore(directory, current):
    require(not directory.exists(), "restore destination already exists")
    run = api(f"repos/{current['repository']}/actions/runs/{current['run_id']}")
    trusted_run(run, current["repository"], current["channel"])
    require(run["head_sha"] == current["source_commit"], "run source changed")
    selected = select_checkpoint(artifacts(current["repository"], current["run_id"]),
                                 current["run_id"], int(current["attempt"]))
    directory.mkdir(parents=True)
    if selected is None:
        require(current["attempt"] == "1" or no_previous_upload(current),
                "retry has no retained intent or proof of skipped uploads; do not resubmit")
        return
    record, files = read_checkpoint(selected, current["repository"], current["run_id"])
    require(all(record[key] == current[key] for key in ("source_commit", "channel", "source_ref")), "restored source mismatch")
    for name, raw in files.items():
        path = directory / name
        with path.open("xb") as stream:
            stream.write(raw)
        path.chmod(0o600)
    # Every attempt since preparation must explicitly show the upload skipped.
    # A later skipped attempt cannot erase an earlier ambiguous upload.
    unstarted = unstarted_architectures(files, current)
    if os.environ.get("GITHUB_OUTPUT"):
        with Path(os.environ["GITHUB_OUTPUT"]).open("a") as output:
            output.write("unstarted_architectures=" + " ".join(unstarted) + "\n")
    print(f"restored checkpoint {selected['id']} from attempt {record['attempt']} ({record['stage']})")


def recover_one(repository, run):
    if run["conclusion"] not in ("failure", "timed_out"):
        return False
    age = datetime.now(timezone.utc) - datetime.fromisoformat(run["created_at"].replace("Z", "+00:00"))
    if run["run_attempt"] >= 12 or age.total_seconds() > 7 * 86400:
        return False
    trusted_run(run, repository)
    prefix = f"apple-notary-state-{run['id']}-{run['run_attempt']}-"
    current_items = [item for item in artifacts(repository, run["id"]) if item["name"].startswith(prefix)]
    selected = select_checkpoint(current_items, run["id"])
    if selected is None:
        return False
    record, _files = read_checkpoint(selected, repository, run["id"])
    if not record["recovery_pending"] or int(record["attempt"]) != run["run_attempt"]:
        return False
    require(record["source_commit"] == run["head_sha"] and record["channel"] == "nightly" and
            record["source_ref"] == "refs/heads/main", "recovery source mismatch")
    ancestry = api(f"repos/{repository}/compare/{run['head_sha']}...main")
    require(ancestry["status"] in ("ahead", "identical"), "recovery source left main history")
    # Recheck live state immediately before mutation, under worker concurrency.
    latest = api(f"repos/{repository}/actions/runs/{run['id']}")
    if latest["status"] != "completed" or latest["conclusion"] not in ("failure", "timed_out") or latest["run_attempt"] != run["run_attempt"]:
        return False
    api(f"repos/{repository}/actions/runs/{run['id']}/rerun-failed-jobs", method="POST", binary=True)
    print(f"continued original run {run['id']} attempt {run['run_attempt']}; no rebuild or resign requested")
    return True


def recover(repository):
    # No Apple key, signing identity or publication token is available here.
    pages = api(f"repos/{repository}/actions/workflows/nightly.yml/runs?branch=main&status=completed&per_page=100", paginate=True)
    failures = 0
    for run in (item for page in pages for item in page["workflow_runs"]):
        try:
            if recover_one(repository, run):
                break
        except (ValueError, KeyError, TypeError, OSError, subprocess.TimeoutExpired, zipfile.BadZipFile, tarfile.TarError) as error:
            failures += 1
            # Retain a bounded refusal witness without emitting provider text as
            # workflow annotations. Another run's state remains independent.
            print(json.dumps(dict(run_id=run.get("id"), refused=str(error)), ensure_ascii=True))
    require(failures == 0, "one or more recovery checkpoints refused; see per-run diagnostics")


def append_evidence(dist, installations, notes, current, version, final_artifact):
    """Keep safe Apple proof in the existing signed, immutable notes asset.

    This adds no artifact kind or trust authority. Native receipts are checked
    against the exact finalized DMG and original signed executable bytes.
    """
    evidence = parse_json((dist / "apple-notarization-evidence.json").read_bytes())
    require(evidence["schema"] == "hikyo.dev/apple-release-evidence/v1" and
            len(evidence["receipts"]) == len(evidence["logs"]) == 2, "incomplete Apple release evidence")
    natives = {}
    for path in installations.rglob("*.json"):
        receipt = parse_json(path.read_bytes())
        arch = receipt["architecture"]
        require(arch in ("arm64", "x86_64") and receipt["schema"] == "hikyo.dev/apple-install-receipt/v1" and
                receipt["source_commit"] == current["source_commit"] and receipt["run_id"] == current["run_id"] and
                receipt["version"] == version and receipt["input_artifact_id"] == final_artifact and
                all(receipt[key] is True for key in ("gatekeeper", "executable_notarization", "quarantine_retained", "native_version", "native_help")),
                "native installation receipt mismatch")
        require(arch not in natives or natives[arch] == receipt, "conflicting native installation receipts")
        natives[arch] = receipt
    require(set(natives) == {"arm64", "x86_64"}, "both native installations are required")
    for arch in ("arm64", "x86_64"):
        name = f"hikyo_{version}_Darwin_{arch}.dmg"
        image_hash = hashlib.sha256((dist / name).read_bytes()).hexdigest()
        with tarfile.open(dist / f"hikyo_{version}_Darwin_{arch}.tar.gz") as archive:
            executable = archive.getmember("hikyo")
            require(executable.isfile(), "nonregular signed executable")
            binary_hash = hashlib.sha256(archive.extractfile(executable).read()).hexdigest()
        require(natives[arch]["final_dmg_sha256"] == image_hash and
                natives[arch]["installed_binary_sha256"] == binary_hash, "installed bytes differ from publication candidate")
        receipts = [item for item in evidence["receipts"] if item["architecture"] == arch]
        require(len(receipts) == 1, "duplicate Apple architecture")
        receipt = receipts[0]
        require(receipt["status"] == "Accepted" and receipt["diagnostics"]["availability"] == "available" and
                receipt["source_commit"] == current["source_commit"] and receipt["original_run_id"] == current["run_id"] and
                receipt["version"] == version and receipt["archive_filename"] == name and
                receipt["final_sha256"] == image_hash, "Accepted receipt differs from candidate")
        logs = [item for item in evidence["logs"] if item["jobId"].lower() == receipt["submission_id"]]
        require(len(logs) == 1 and logs[0]["status"].lower() == "accepted", "matching Accepted diagnostic required")
        require(logs[0].get("archiveFilename", name) == name and
                logs[0].get("sha256", receipt["submitted_sha256"]).lower() == receipt["submitted_sha256"],
                "diagnostic filename or submitted hash mismatch")
    with notes.open("a") as stream:
        stream.write("\nApple notarization and native installation evidence (bound by this signed inventory):\n")
        json.dump(dict(notarization=evidence, native_installations=natives), stream, sort_keys=True, indent=2)
        stream.write("\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    command = commands.add_parser("restore")
    command.add_argument("directory", type=Path)
    command = commands.add_parser("snapshot")
    command.add_argument("directory", type=Path)
    command.add_argument("destination", type=Path)
    command.add_argument("stage", choices=STAGES)
    commands.add_parser("recover")
    command = commands.add_parser("append-evidence")
    command.add_argument("dist", type=Path)
    command.add_argument("installations", type=Path)
    command.add_argument("notes", type=Path)
    args = parser.parse_args()
    try:
        if args.command == "restore":
            restore(args.directory, identity())
        elif args.command == "snapshot":
            pending = os.environ.get("APPLE_RECOVERY_PENDING", "false")
            require(pending in ("true", "false"), "invalid continuation decision")
            snapshot(args.directory, args.destination, args.stage, pending == "true", identity())
        elif args.command == "append-evidence":
            append_evidence(args.dist, args.installations, args.notes, identity(),
                            os.environ["VERSION"], os.environ["APPLE_FINAL_ARTIFACT_ID"])
        else:
            recover(identity()["repository"])
    except (ValueError, KeyError, TypeError, OSError, subprocess.TimeoutExpired, zipfile.BadZipFile, tarfile.TarError) as error:
        raise SystemExit(f"apple checkpoint: {error}") from error


if __name__ == "__main__":
    main()
