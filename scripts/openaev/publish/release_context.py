#!/usr/bin/env python3
"""Resolve one published release, never a moving latest/nightly reference."""
import json
import os
import re
import subprocess
import threading
from pathlib import Path
from urllib.parse import quote

TAG = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$")
NIGHTLY = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+-nightly\.[0-9]{8}\.([1-9][0-9]*)\.g[0-9a-f]{8}$")
SHA = re.compile(r"^[0-9a-f]{40}$")
RELEASE_PAGE_SIZE = 25
MAX_RELEASE_PAGES = 40
MAX_RELEASE_PAGE_BYTES = 4 * 1024 * 1024
RELEASE_REQUEST_SECONDS = 30
CHECK_NAME = "OpenAEV release validation"
GITHUB_ACTIONS_APP_ID = 15368
CHECK_PAGE_SIZE = 100
MAX_CHECK_PAGES = 5


def api(path, payload=None):
    """Read bounded GitHub metadata or create one success-only check run."""
    if not re.match(r"^repos/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/", path):
        raise ValueError("unexpected GitHub API resource")
    command = ["gh", "api", path]
    encoded = None
    if payload is not None:
        if not re.fullmatch(r"repos/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/check-runs", path):
            raise ValueError("publisher may only create check runs")
        if payload.get("status") != "completed" or payload.get("conclusion") != "success":
            raise ValueError("publisher may only create completed success checks")
        encoded = json.dumps(payload).encode()
        if len(encoded) > 64 * 1024:
            raise ValueError("check payload exceeds public proof limit")
        command += ["--method", "POST", "--input", "-"]
    process = subprocess.Popen(command, stdin=subprocess.PIPE if encoded else subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    timer = threading.Timer(RELEASE_REQUEST_SECONDS, process.kill)
    timer.daemon = True
    timer.start()
    try:
        if encoded:
            if process.stdin is None:
                raise ValueError("missing check API request stream")
            process.stdin.write(encoded)
            process.stdin.close()
        if process.stdout is None:
            raise ValueError("missing GitHub API response stream")
        raw = process.stdout.read(MAX_RELEASE_PAGE_BYTES + 1)
        if len(raw) > MAX_RELEASE_PAGE_BYTES:
            raise ValueError("GitHub API response exceeds bounded response limit; replay exact tag manually")
        if process.wait(timeout=2) != 0:
            raise ValueError("GitHub API request failed or timed out")
        return json.loads(raw)
    finally:
        timer.cancel()
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=2)
        if process.stdin is not None and not process.stdin.closed:
            process.stdin.close()
        if process.stdout is not None:
            process.stdout.close()


def release_page(repository, page):
    data = api(f"repos/{repository}/releases?per_page={RELEASE_PAGE_SIZE}&page={page}")
    if not isinstance(data, list) or len(data) > RELEASE_PAGE_SIZE:
        raise ValueError("release API response is not a bounded page")
    return data


def check_identity(release_id, commit):
    return f"openaev-release:{release_id}:{commit}"


def existing_pass(repository, release_id, commit):
    """Only the GitHub Actions app's exact completed success is idempotent."""
    external_id = check_identity(release_id, commit)
    name = quote(CHECK_NAME, safe="")
    for page in range(1, MAX_CHECK_PAGES + 1):
        data = api(f"repos/{repository}/commits/{commit}/check-runs?check_name={name}&filter=all&per_page={CHECK_PAGE_SIZE}&page={page}")
        checks = data["check_runs"]
        if not isinstance(checks, list) or len(checks) > CHECK_PAGE_SIZE:
            raise ValueError("check API response is not a bounded page")
        for check in checks:
            app = check.get("app") or {}
            if (check.get("name") == CHECK_NAME and check.get("external_id") == external_id
                    and check.get("head_sha") == commit and check.get("status") == "completed"
                    and check.get("conclusion") == "success" and app.get("id") == GITHUB_ACTIONS_APP_ID
                    and app.get("slug") == "github-actions"):
                return True
        if len(checks) < CHECK_PAGE_SIZE:
            return False
    raise ValueError("check history exceeds bounded lookup window")


def nightly_release(repository, run_number):
    matches = []
    for number in range(1, MAX_RELEASE_PAGES + 1):
        page = release_page(repository, number)
        for item in page:
            match = NIGHTLY.fullmatch(item["tag_name"])
            if match and int(match[1]) == run_number and not item["draft"] and item["prerelease"]:
                matches.append(item)
                if len(matches) > 1:
                    raise ValueError("nightly run number does not identify one release")
        if len(page) < RELEASE_PAGE_SIZE:
            return matches[0] if matches else None
    # Absence or uniqueness is not proven beyond the bounded window. Explicit
    # dispatch with a known tag bypasses history enumeration for old releases.
    raise ValueError("release history exceeds bounded lookup window; replay exact tag manually")


def source_sha(repository, tag):
    obj = api(f"repos/{repository}/git/ref/tags/{quote(tag, safe='')}")["object"]
    for _ in range(8):
        if obj["type"] == "commit" and SHA.fullmatch(obj["sha"]):
            return obj["sha"]
        if obj["type"] != "tag":
            break
        obj = api(f"repos/{repository}/git/tags/{obj['sha']}")["object"]
    raise ValueError("release tag must resolve to one commit")


def resolve(event_name, event, repository, dispatch_tag=""):
    if event_name == "release":
        release = event["release"]
        if NIGHTLY.fullmatch(release["tag_name"]):
            return None  # Its published event precedes image signing.
    elif event_name == "workflow_run":
        run = event["workflow_run"]
        if (run["conclusion"] != "success" or run["head_branch"] != "main"
                or run["head_repository"]["full_name"] != repository
                or run["path"] != ".github/workflows/nightly.yml"):
            return None
        release = nightly_release(repository, run["run_number"])
        if release is None:
            return None  # No new release when the nightly source was unchanged.
    elif event_name == "workflow_dispatch":
        if not TAG.fullmatch(dispatch_tag):
            raise ValueError("an exact published version tag is required")
        release = api(f"repos/{repository}/releases/tags/{quote(dispatch_tag, safe='')}")
    else:
        raise ValueError("unsupported OpenAEV event")
    # A rerun retains its original event payload; verify current release identity
    # and the owned success check before repeating the expensive lab.
    original_tag = release["tag_name"]
    release = api(f"repos/{repository}/releases/{release['id']}")
    if release["tag_name"] != original_tag:
        raise ValueError("release tag changed since the triggering event")
    tag = release["tag_name"]
    if not TAG.fullmatch(tag) or release["draft"] or not release["published_at"]:
        raise ValueError("candidate must be a published version release")
    commit = source_sha(repository, tag)
    if event_name == "workflow_run" and commit != event["workflow_run"]["head_sha"]:
        raise ValueError("nightly source differs from the completed producer")
    if event_name != "workflow_dispatch" and existing_pass(repository, release["id"], commit):
        return None
    return {"release_id": release["id"], "tag": tag, "source_sha": commit,
            "published_at": release["published_at"], "repository": repository}


def main():
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
    context = resolve(os.environ["GITHUB_EVENT_NAME"], event,
                      os.environ["GITHUB_REPOSITORY"], os.environ.get("RELEASE_TAG", ""))
    output = Path(os.environ["GITHUB_OUTPUT"])
    with output.open("a") as stream:
        stream.write(f"run={'true' if context else 'false'}\n")
        if context:
            for key, value in context.items():
                stream.write(f"{key}={value}\n")
    if context:
        destination = Path(os.environ["OPENAEV_RESULT_DIR"])
        destination.mkdir(parents=True, exist_ok=True)
        (destination / "release-identity.json").write_text(json.dumps(context))
        release = api(f"repos/{context['repository']}/releases/{context['release_id']}")
        (destination / "release.json").write_text(json.dumps(release))


if __name__ == "__main__":
    main()
