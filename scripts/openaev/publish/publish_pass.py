#!/usr/bin/env python3
"""Validate public PASS proof and create its success-only commit check."""
import argparse
import json
import re
import sys
from pathlib import Path
from datetime import UTC, datetime
from release_context import CHECK_NAME, api, check_identity, existing_pass, source_sha

FIELDS = {"schema_version", "status", "release_id", "tag", "source_sha", "published_at",
          "image_ref", "suite_sha256", "expected_assertions", "passed_assertions",
          "failed_assertions", "skipped_assertions", "cleanup_verified", "isolation_verified",
          "image_native_binary_verified", "native_verification"}


def suite_fingerprint():
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
    from suite_identity import source_fingerprint
    return source_fingerprint()


def validate(result, identity):
    if set(result) != FIELDS:
        raise ValueError("PASS attestation contains missing or unexpected fields")
    for key in ("release_id", "tag", "source_sha", "published_at"):
        if result[key] != identity[key]:
            raise ValueError(f"PASS attestation differs from release identity: {key}")
    for key, expected in {"schema_version": 1, "status": "PASS", "expected_assertions": 313,
                          "passed_assertions": 313, "failed_assertions": 0,
                          "skipped_assertions": 0, "cleanup_verified": True,
                          "isolation_verified": True, "image_native_binary_verified": True}.items():
        if type(result[key]) is not type(expected) or result[key] != expected:
            raise ValueError(f"incomplete PASS attestation: {key}")
    image = f"ghcr.io/{identity['repository'].lower()}@sha256:"
    if not re.fullmatch(re.escape(image) + r"[0-9a-f]{64}", result["image_ref"]):
        raise ValueError("candidate image is not the repository's pinned digest")
    if result["suite_sha256"] != suite_fingerprint():
        raise ValueError("suite differs from the trusted harness checkout")
    prerelease = identity["tag"].removeprefix("v").partition("-")[2]
    profile = "nightly/v1" if prerelease.startswith("nightly.") else "stable/v1"
    if result["native_verification"] != profile:
        raise ValueError("candidate native verification profile differs")
    return result


def publish(result, identity, run_url):
    validate(result, identity)
    repository = identity["repository"]
    if not re.fullmatch(r"https://github\.com/" + re.escape(repository)
                        + r"/actions/runs/[1-9][0-9]*", run_url):
        raise ValueError("unexpected run URL")
    release = api(f"repos/{repository}/releases/{identity['release_id']}")
    if (release.get("immutable") is not True or release["draft"] or release["tag_name"] != identity["tag"]
            or release["published_at"] != identity["published_at"]
            or source_sha(repository, identity["tag"]) != identity["source_sha"]):
        raise ValueError("published release identity changed during validation")
    if existing_pass(repository, identity["release_id"], identity["source_sha"]):
        return False
    summary = (f"PASS: all 313 assertions passed on a fresh GitHub-hosted ARM64 runner. "
               f"Native binary binding, isolation and cleanup verified.\n\n"
               f"Release: `{identity['tag']}` (ID `{identity['release_id']}`)\n\n"
               f"Source: `{identity['source_sha']}`\n\n"
               f"Candidate: `{result['image_ref']}`\n\n"
               f"Trusted harness SHA-256: `{result['suite_sha256']}`")
    api(f"repos/{repository}/check-runs", payload={
        "name": CHECK_NAME, "head_sha": identity["source_sha"],
        "external_id": check_identity(identity["release_id"], identity["source_sha"]),
        "details_url": run_url, "status": "completed", "conclusion": "success",
        "completed_at": datetime.now(UTC).isoformat(timespec="seconds").replace("+00:00", "Z"),
        "output": {"title": "OpenAEV PASS: 313/313 assertions", "summary": summary},
    })
    return True


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("result", type=Path)
    parser.add_argument("identity", type=Path)
    parser.add_argument("--run-url")
    args = parser.parse_args()
    result = json.loads(args.result.read_text())
    identity = json.loads(args.identity.read_text())
    validate(result, identity)
    if args.run_url:
        publish(result, identity, args.run_url)


if __name__ == "__main__":
    main()
