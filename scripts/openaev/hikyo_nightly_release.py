"""Read-only release identity binding and sanitized native exercise counts."""

from __future__ import annotations

import html
import json
import os
import re
import subprocess
import tempfile
import uuid
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Protocol
from urllib.parse import quote

REPOSITORY = "Hikyo-Org/Hikyo"
START = "<!-- hikyo-openaev-lab:begin -->"
END = "<!-- hikyo-openaev-lab:end -->"
MAX_BODY = 125_000
MAX_JSON = 4_000_000
TAG = re.compile(r"v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?")
NIGHTLY = re.compile(r"v\d+\.\d+\.\d+-nightly\.(\d{8})\.([1-9]\d*)\.g([a-f0-9]{8,40})")
SHA = re.compile(r"[a-f0-9]{40}")
LABEL = re.compile(r"[A-Za-z0-9][A-Za-z0-9_. -]{0,63}")
STATUSES = frozenset({"passed", "failed", "skipped", "infra_error"})


def is_nightly_tag(tag: str) -> bool:
    """Match the SemVer prerelease prefix, without treating embedded words as a channel."""
    return re.match(r"^v[0-9]+\.[0-9]+\.[0-9]+-nightly\.", tag) is not None


class ReleaseError(RuntimeError):
    """Sanitized literal diagnostics; never include gh output or API bodies."""


class ReleaseClient(Protocol):
    def request(self, path: str, body: dict[str, object] | None = None) -> object: ...


class GhClient:
    """Existing trusted gh authentication; bounded requests with private JSON input."""

    def __init__(self, executable: str = "gh", timeout: float = 30) -> None:
        if not 0 < timeout <= 60:
            raise ReleaseError("invalid_github_timeout")
        self.executable, self.timeout = executable, timeout

    def request(self, path: str, body: dict[str, object] | None = None) -> object:
        if not path.startswith(f"repos/{REPOSITORY}/") or "\n" in path:
            raise ReleaseError("github_repository_boundary")
        command = [self.executable, "api", "--hostname", "github.com", path]
        with tempfile.TemporaryFile() as output:
            if body is not None:
                raise ReleaseError("hosted_runner_github_read_only")
            # A file bounds Python memory independently of API response size.
            try:
                completed = subprocess.run(
                    command,
                    stdin=subprocess.DEVNULL,
                    stdout=output,
                    stderr=subprocess.DEVNULL,
                    timeout=self.timeout,
                    check=False,
                )
            except (OSError, subprocess.TimeoutExpired):
                raise ReleaseError("github_request_unavailable") from None
            if completed.returncode != 0:
                raise ReleaseError("github_request_refused")
            if output.tell() > MAX_JSON:
                raise ReleaseError("github_response_size_limit")
            output.seek(0)
            encoded = output.read(MAX_JSON + 1)
        try:
            return json.loads(encoded)
        except (ValueError, UnicodeError):
            raise ReleaseError("github_response_invalid_json") from None


def _object(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        raise ReleaseError("github_response_invalid_object")
    return dict(value)


def _text(value: dict[str, object], field: str) -> str:
    result = value.get(field)
    if not isinstance(result, str) or not result:
        raise ReleaseError("github_response_invalid_field")
    return result


def _body(release: dict[str, object]) -> str:
    body = release.get("body")
    if body is None:
        body = ""
    if not isinstance(body, str) or len(body.encode()) > MAX_BODY:
        raise ReleaseError("release_body_size_or_shape")
    return body


def _published(value: dict[str, object]) -> datetime:
    try:
        result = datetime.fromisoformat(
            _text(value, "published_at").replace("Z", "+00:00")
        )
        if result.tzinfo is None:
            raise ValueError()
        return result.astimezone(UTC)
    except (TypeError, ValueError):
        raise ReleaseError("release_publication_time_invalid") from None


def _revoked(value: dict[str, object]) -> bool:
    name = value.get("name")
    return (
        value.get("revoked") is True
        or (isinstance(name, str) and name.upper().startswith("[REVOKED]"))
        or "<!-- hikyo-release-revoked -->" in _body(value)
    )


def _eligible(value: dict[str, object]) -> bool:
    return (
        value.get("draft") is False
        and type(value.get("prerelease")) is bool
        and isinstance(value.get("tag_name"), str)
        and TAG.fullmatch(str(value["tag_name"])) is not None
        and value.get("published_at") is not None
        and not _revoked(value)
    )


@dataclass(frozen=True)
class Candidate:
    release_id: int
    tag: str
    source_sha: str
    published_at: str

    def __post_init__(self) -> None:
        match = NIGHTLY.fullmatch(self.tag)
        if (
            type(self.release_id) is not int
            or self.release_id <= 0
            or TAG.fullmatch(self.tag) is None
            or SHA.fullmatch(self.source_sha) is None
            or (match is not None and not self.source_sha.startswith(match[3]))
        ):
            raise ReleaseError("candidate_identity_invalid")
        _published({"published_at": self.published_at})


def resolve_source(client: ReleaseClient, tag: str) -> str:
    if TAG.fullmatch(tag) is None:
        raise ReleaseError("candidate_tag_invalid")
    ref = _object(
        client.request(f"repos/{REPOSITORY}/git/ref/tags/{quote(tag, safe='')}")
    )
    if ref.get("ref") != f"refs/tags/{tag}":
        raise ReleaseError("candidate_tag_ref_mismatch")
    target = _object(ref.get("object"))
    seen: set[str] = set()
    for _ in range(5):
        sha = _text(target, "sha")
        if SHA.fullmatch(sha) is None or sha in seen:
            raise ReleaseError("candidate_source_invalid")
        seen.add(sha)
        if target.get("type") == "commit":
            match = NIGHTLY.fullmatch(tag)
            if match is not None and not sha.startswith(match[3]):
                raise ReleaseError("candidate_source_tag_mismatch")
            return sha
        if target.get("type") != "tag":
            raise ReleaseError("candidate_source_not_commit")
        annotated = _object(client.request(f"repos/{REPOSITORY}/git/tags/{sha}"))
        if annotated.get("sha") != sha:
            raise ReleaseError("candidate_annotated_tag_mismatch")
        target = _object(annotated.get("object"))
    raise ReleaseError("candidate_tag_depth_limit")


def discover_candidate(client: ReleaseClient) -> Candidate:
    """Select newest publication, not response order or the mutable main branch."""
    releases: list[dict[str, object]] = []
    complete = False
    for page in range(1, 11):
        response = client.request(
            f"repos/{REPOSITORY}/releases?per_page=100&page={page}"
        )
        if not isinstance(response, list) or len(response) > 100:
            raise ReleaseError("release_listing_invalid")
        releases.extend(_object(row) for row in response)
        if len(response) < 100:
            complete = True
            break
    if not complete:
        raise ReleaseError("release_listing_limit")
    eligible = [row for row in releases if _eligible(row) and NIGHTLY.fullmatch(str(row["tag_name"]))]
    if not eligible:
        raise ReleaseError("published_nightly_unavailable")
    latest = max(eligible, key=_published)
    release_id = latest.get("id")
    if type(release_id) is not int:
        raise ReleaseError("release_identity_invalid")
    candidate = Candidate(
        release_id,
        _text(latest, "tag_name"),
        resolve_source(client, _text(latest, "tag_name")),
        _text(latest, "published_at"),
    )
    validate_candidate(client, candidate)
    return candidate


def validate_candidate(
    client: ReleaseClient, candidate: Candidate
) -> dict[str, object]:
    """Revalidate immediately before deploy/publication; signature checks are separate."""
    release = _object(
        client.request(f"repos/{REPOSITORY}/releases/{candidate.release_id}")
    )
    if (
        not _eligible(release)
        or release.get("id") != candidate.release_id
        or release.get("tag_name") != candidate.tag
        or release.get("published_at") != candidate.published_at
        or resolve_source(client, candidate.tag) != candidate.source_sha
    ):
        raise ReleaseError("release_candidate_binding_changed")
    _body(release)
    return release


@dataclass(frozen=True)
class TestResult:
    case_id: str
    status: str
    evidence_verified: bool

    def __post_init__(self) -> None:
        if LABEL.fullmatch(self.case_id) is None or self.status not in STATUSES:
            raise ReleaseError("native_test_result_invalid")
        if type(self.evidence_verified) is not bool:
            raise ReleaseError("native_evidence_invalid")
        if self.status in {"passed", "failed"} and not self.evidence_verified:
            raise ReleaseError("native_evidence_required")


@dataclass(frozen=True)
class SuiteResult:
    name: str
    tests: tuple[TestResult, ...]

    def __post_init__(self) -> None:
        if LABEL.fullmatch(self.name) is None or not isinstance(self.tests, tuple):
            raise ReleaseError("native_suite_invalid")
        if len(self.tests) > 10_000 or any(
            not isinstance(row, TestResult) for row in self.tests
        ):
            raise ReleaseError("native_suite_invalid")
        identifiers = [row.case_id for row in self.tests]
        if len(identifiers) != len(set(identifiers)):
            raise ReleaseError("native_test_identity_duplicate")


def summarise_results(suites: tuple[SuiteResult, ...]) -> dict[str, dict[str, int]]:
    if (
        not suites
        or len(suites) > 32
        or any(not isinstance(s, SuiteResult) for s in suites)
    ):
        raise ReleaseError("native_suites_required")
    if len({suite.name for suite in suites}) != len(suites):
        raise ReleaseError("native_suite_identity_duplicate")
    return {
        suite.name: {
            status: sum(result.status == status for result in suite.tests)
            for status in ("passed", "failed", "skipped", "infra_error")
        }
        for suite in suites
    }


def outcome(suites: tuple[SuiteResult, ...], infra_errors: tuple[str, ...]) -> str:
    counts = summarise_results(suites)
    if (
        any(LABEL.fullmatch(error) is None for error in infra_errors)
        or len(infra_errors) > 32
    ):
        raise ReleaseError("public_infrastructure_error_invalid")
    if infra_errors or any(row["infra_error"] for row in counts.values()):
        return "INFRA ERROR"
    if any(row["failed"] for row in counts.values()):
        return "FAIL"
    if any(row["skipped"] or sum(row.values()) == 0 for row in counts.values()):
        return "INCOMPLETE"
    return "PASS"


def _badge(label: str, message: str, color: str) -> str:
    url = "https://img.shields.io/static/v1?" + "&".join(
        f"{name}={quote(value, safe='')}"
        for name, value in (("label", label), ("message", message), ("color", color))
    )
    return f"![{label}: {message}]({url})"


def render_section(
    candidate: Candidate,
    run_id: str,
    suites: tuple[SuiteResult, ...],
    *,
    cleanup_verified: bool,
    infra_errors: tuple[str, ...] = (),
) -> str:
    try:
        if str(uuid.UUID(run_id)) != run_id:
            raise ValueError()
    except (ValueError, TypeError, AttributeError):
        raise ReleaseError("exercise_run_identity_invalid") from None
    if type(cleanup_verified) is not bool:
        raise ReleaseError("cleanup_status_invalid")
    if not cleanup_verified and "cleanup-unverified" not in infra_errors:
        infra_errors = (*infra_errors, "cleanup-unverified")
    counts = summarise_results(suites)
    verdict = outcome(suites, infra_errors)
    color = {
        "PASS": "brightgreen",
        "FAIL": "red",
        "INCOMPLETE": "yellow",
        "INFRA ERROR": "orange",
    }[verdict]
    total = {status: sum(row[status] for row in counts.values()) for status in STATUSES}
    badges = [_badge("OpenAEV lab", verdict, color)] + [
        _badge(status.replace("_", " "), str(total[status]), shade)
        for status, shade in (
            ("passed", "brightgreen"),
            ("failed", "red"),
            ("skipped", "yellow"),
            ("infra_error", "orange"),
        )
    ]
    lines = [
        START,
        "### Disposable Hikyo lab results",
        "",
        " ".join(badges),
        "",
        f"Exact candidate: `{candidate.tag}`; source `{candidate.source_sha}`.",
        f"Exercise run: `{run_id}`. Disposable Hikyo cleanup: {'verified' if cleanup_verified else 'UNVERIFIED, operator action required'}.",
        "",
        "| Native suite | Passed | Failed | Skipped | Infrastructure errors |",
        "| --- | ---: | ---: | ---: | ---: |",
    ]
    for name, row in counts.items():
        lines.append(
            f"| {name} | {row['passed']} | {row['failed']} | {row['skipped']} | {row['infra_error']} |"
        )
    lines += [
        "",
        "Counts describe individually verified deployed lab assertions. Unit tests are excluded.",
        "Skipped or unexecuted suites cannot produce PASS. Infrastructure errors are separate from failed assertions.",
        "This bounded authorization, isolation and audit-alert exercise is not a CVE scan, production certification or capacity benchmark.",
    ]
    if infra_errors:
        lines += [
            "",
            "Infrastructure diagnostics: "
            + ", ".join(f"`{error}`" for error in infra_errors)
            + ".",
        ]
    return "\n".join(lines + [END])


def replace_section(body: str, section: str) -> str:
    if (
        len(body.encode()) > MAX_BODY
        or not section.startswith(START)
        or not section.endswith(END)
    ):
        raise ReleaseError("release_section_invalid")
    starts, ends = body.count(START), body.count(END)
    if starts == ends == 0:
        result = body + ("\n\n" if body else "") + section
    elif starts == ends == 1 and body.index(START) < body.index(END):
        beginning, ending = body.index(START), body.index(END) + len(END)
        result = body[:beginning] + section + body[ending:]
    else:
        raise ReleaseError("release_owned_marker_ambiguous")
    if len(result.encode()) > MAX_BODY:
        raise ReleaseError("release_body_size_limit")
    return result


def write_report(
    directory: Path,
    candidate: Candidate,
    run_id: str,
    suites: tuple[SuiteResult, ...],
    *,
    cleanup_verified: bool,
    infra_errors: tuple[str, ...] = (),
) -> dict[str, Path]:
    """Exclusive immutable artifacts with only sanitized counts and public IDs."""
    section = render_section(
        candidate,
        run_id,
        suites,
        cleanup_verified=cleanup_verified,
        infra_errors=infra_errors,
    )
    if directory.is_symlink():
        raise ReleaseError("report_directory_symlink")
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    if not directory.is_dir() or list(directory.iterdir()):
        raise ReleaseError("report_directory_must_be_empty")
    directory.chmod(0o700)
    effective_errors = (
        infra_errors
        if cleanup_verified or "cleanup-unverified" in infra_errors
        else (*infra_errors, "cleanup-unverified")
    )
    document = {
        "schema_version": 1,
        "recorded_at": datetime.now(UTC).isoformat(),
        "repository": REPOSITORY,
        "release_id": candidate.release_id,
        "tag": candidate.tag,
        "source_sha": candidate.source_sha,
        "published_at": candidate.published_at,
        "run_id": run_id,
        "outcome": outcome(suites, effective_errors),
        "suite_counts": summarise_results(suites),
        "cleanup_verified": cleanup_verified,
        "infra_errors": list(effective_errors),
    }
    markdown = section.removeprefix(START).removesuffix(END).strip() + "\n"
    offline = (
        '<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Hikyo nightly lab results</title><style>body{font:16px system-ui;max-width:1000px;margin:2rem auto;padding:0 1rem;color:#182235;background:#fff}pre{font:inherit;white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.6}</style><body><pre>'
        + html.escape(markdown)
        + "</pre></body></html>\n"
    )
    artifacts = {
        "results.json": json.dumps(document, sort_keys=True, indent=2) + "\n",
        "results.md": markdown,
        "results.html": offline,
    }
    paths: dict[str, Path] = {}
    for name, contents in artifacts.items():
        target = directory / name
        descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "w") as handle:
            handle.write(contents)
            handle.flush()
            os.fsync(handle.fileno())
        paths[name] = target
    return paths
