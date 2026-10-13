"""Prepare signed ARM64 release evidence with the trusted workflow native verifier.

The released image stays inert until its bytes match the authenticated binary.
Candidate source is an identity to check, never executable preparation code.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import stat
import subprocess
import tempfile
import uuid
from pathlib import Path

from hikyo_nightly_release import Candidate, GhClient, is_nightly_tag, validate_candidate

SOURCE_REPO = Path(__file__).resolve().parents[2]
ISSUER = "https://token.actions.githubusercontent.com"
IDENTITY = (
    "https://github.com/Hikyo-Org/Hikyo/.github/workflows/nightly.yml@refs/heads/main"
)
MAX_BUNDLE_BYTES = 128 << 20
MAX_FILES = 2048

# This adapter calls the same preparation API as unattended container startup.
# It never invokes Apply, an upgrade coordinator, database code or the candidate.



class CandidateError(RuntimeError):
    """Fixed diagnostics keep command output and local custody out of chat."""


def _object(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or any(not isinstance(k, str) for k in value):
        raise CandidateError("invalid_candidate_object")
    return dict(value)


def _text(value: dict[str, object], name: str) -> str:
    item = value.get(name)
    if not isinstance(item, str) or not item:
        raise CandidateError("invalid_candidate_field")
    return item


def command(
    arguments: list[str],
    *,
    cwd: Path | None = None,
    env: dict[str, str] | None = None,
    timeout: float = 30,
    limit: int = 4 << 20,
) -> bytes:
    with tempfile.TemporaryFile() as output:
        try:
            result = subprocess.run(
                arguments,
                cwd=cwd,
                env=env,
                stdin=subprocess.DEVNULL,
                stdout=output,
                stderr=subprocess.DEVNULL,
                timeout=timeout,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired):
            raise CandidateError("candidate_command_unavailable") from None
        if result.returncode:
            raise CandidateError("candidate_command_refused")
        if output.tell() > limit:
            raise CandidateError("candidate_command_output_bound")
        output.seek(0)
        return output.read(limit + 1)


def tree_inventory(directory: Path) -> list[dict[str, object]]:
    """Closed regular-file tree, bounded bytes and stable no-follow reads."""
    root = directory.lstat()
    if not stat.S_ISDIR(root.st_mode) or root.st_uid != os.getuid():
        raise CandidateError("invalid_bundle_directory")
    rows: list[dict[str, object]] = []
    total = 0
    for path in sorted(directory.rglob("*")):
        info = path.lstat()
        if stat.S_ISDIR(info.st_mode):
            continue
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid():
            raise CandidateError("invalid_bundle_member")
        if len(rows) >= MAX_FILES or info.st_size <= 0:
            raise CandidateError("bundle_inventory_bound")
        total += info.st_size
        if total > MAX_BUNDLE_BYTES:
            raise CandidateError("bundle_size_bound")
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(descriptor, "rb") as stream:
            opened = os.fstat(stream.fileno())
            if (opened.st_dev, opened.st_ino, opened.st_size) != (
                info.st_dev,
                info.st_ino,
                info.st_size,
            ):
                raise CandidateError("bundle_member_changed")
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
            after = os.fstat(stream.fileno())
            if (after.st_size, after.st_mtime_ns, after.st_ctime_ns) != (
                opened.st_size,
                opened.st_mtime_ns,
                opened.st_ctime_ns,
            ):
                raise CandidateError("bundle_member_changed")
        rows.append(
            {
                "path": path.relative_to(directory).as_posix(),
                "size": info.st_size,
                "sha256": digest,
            }
        )
    if not rows or not any(row["path"] == "index.json" for row in rows):
        raise CandidateError("bundle_index_missing")
    return rows


def tree_digest(rows: list[dict[str, object]]) -> str:
    return hashlib.sha256(
        json.dumps(rows, sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()


def _host_path(value: object, cache: Path) -> Path:
    if not isinstance(value, str):
        raise CandidateError("native_output_path_boundary")
    try:
        relative = Path(value).relative_to(cache)
    except ValueError:
        raise CandidateError("native_output_path_boundary") from None
    if ".." in relative.parts or not relative.parts:
        raise CandidateError("native_output_path_boundary")
    return cache / relative


def _verified_output(
    value: object, candidate: Candidate, cache: Path
) -> dict[str, object]:
    result = _object(value)
    identity = _object(result.get("identity"))
    if (
        identity.get("profile") != ("nightly/v1" if is_nightly_tag(candidate.tag) else "stable/v1")
        or identity.get("version") != candidate.tag[1:]
        or identity.get("commit") != candidate.source_sha
    ):
        raise CandidateError("native_candidate_identity_mismatch")
    artifacts = result.get("artifacts")
    if not isinstance(artifacts, list):
        raise CandidateError("native_artifact_inventory_missing")
    images = [_object(row) for row in artifacts if _object(row).get("kind") == "image"]
    if len(images) > 1 or (not is_nightly_tag(candidate.tag) and len(images) != 1):
        raise CandidateError("signed_candidate_image_inventory_missing_or_ambiguous")
    signed_image_ref: str | None = None
    if images:
        image, image_tag, digest = (images[0].get(key) for key in ("image", "tag", "digest"))
        if (image != "ghcr.io/hikyo-org/hikyo" or image_tag != candidate.tag[1:]
                or not isinstance(digest, str) or re.fullmatch(r"sha256:[a-f0-9]{64}", digest) is None):
            raise CandidateError("signed_candidate_image_identity_mismatch")
        signed_image_ref = str(image) + "@" + digest
    manifest = _text(identity, "manifest_sha256")
    binary_hash = _text(result, "binary_sha256")
    if any(
        re.fullmatch(r"[a-f0-9]{64}", item) is None for item in [manifest, binary_hash]
    ):
        raise CandidateError("invalid_native_digest")
    bundle = _host_path(result.get("bundle_directory"), cache)
    binary = _host_path(result.get("binary_path"), cache)
    binary_info = binary.lstat()
    if not stat.S_ISREG(binary_info.st_mode) or binary_info.st_size > 256 << 20:
        raise CandidateError("invalid_native_binary")
    with binary.open("rb") as stream:
        if hashlib.file_digest(stream, "sha256").hexdigest() != binary_hash:
            raise CandidateError("native_binary_changed")
    rows = tree_inventory(bundle)
    return {
        "expected_version": candidate.tag[1:],
        "source_commit": candidate.source_sha,
        "bundle_directory": str(bundle),
        "bundle_tree_sha256": tree_digest(rows),
        "bundle_inventory": rows,
        "native_manifest_sha256": manifest,
        "native_binary_sha256": binary_hash,
        "trusted_source_sha": command(["git", "rev-parse", "HEAD"], cwd=SOURCE_REPO).decode().strip(),
        "native_verification": identity["profile"],
        "signed_image_ref": signed_image_ref,
    }


def resolve_image(candidate: Candidate) -> str:
    reference = "ghcr.io/hikyo-org/hikyo:" + candidate.tag[1:]
    raw = command(
        [
            "docker",
            "buildx",
            "imagetools",
            "inspect",
            reference,
            "--format",
            "{{json .Manifest}}",
        ],
        timeout=120,
    )
    digest = _text(_object(json.loads(raw)), "digest")
    if re.fullmatch(r"sha256:[a-f0-9]{64}", digest) is None:
        raise CandidateError("invalid_native_image_digest")
    return "ghcr.io/hikyo-org/hikyo@" + digest


def verify_image_binary(reference: str, expected_sha256: str) -> str:
    """Copy, never start, the signed image and bind its bytes to native release."""
    container = "hikyo-lab-native-payload-" + uuid.uuid4().hex
    with tempfile.TemporaryDirectory(prefix="hikyo-image-payload-") as temporary:
        destination = Path(temporary) / "hikyo"
        try:
            command(
                [
                    "docker",
                    "create",
                    "--name",
                    container,
                    "--platform",
                    "linux/arm64",
                    "--network",
                    "none",
                    "--read-only",
                    "--cap-drop",
                    "ALL",
                    reference,
                ]
            )
            command(
                ["docker", "cp", container + ":/usr/local/bin/hikyo", str(destination)],
                timeout=120,
            )
            info = destination.lstat()
            if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= 256 << 20:
                raise CandidateError("invalid_image_binary")
            descriptor = os.open(destination, os.O_RDONLY | os.O_NOFOLLOW)
            with os.fdopen(descriptor, "rb") as stream:
                digest = hashlib.file_digest(stream, "sha256").hexdigest()
            if digest != expected_sha256:
                raise CandidateError("image_native_binary_mismatch")
            return digest
        finally:
            command(["docker", "rm", "-f", container])


def verify_image(reference: str, state: Path, name: str, source_sha: str, *, identity: str = IDENTITY) -> None:
    signature = command(
        [
            "cosign",
            "verify",
            "--certificate-identity",
            identity,
            "--certificate-oidc-issuer",
            ISSUER,
            "--certificate-github-workflow-sha",
            source_sha,
            reference,
        ],
        timeout=120,
    )
    raw = command(
        ["docker", "buildx", "imagetools", "inspect", "--raw", reference], timeout=120
    )
    index = _object(json.loads(raw))
    manifests = index.get("manifests")
    if not isinstance(manifests, list) or not 2 <= len(manifests) <= 16:
        raise CandidateError("signed_multiarch_index_required")
    platforms = {
        _text(_object(_object(row).get("platform")), "os")
        + "/"
        + _text(_object(_object(row).get("platform")), "architecture")
        for row in manifests
    }
    if not {"linux/arm64", "linux/amd64"} <= platforms:
        raise CandidateError("signed_index_platforms_missing")
    (state / (name + "-signature.json")).write_bytes(signature)
    (state / (name + "-index.json")).write_bytes(raw)
    command(["docker", "pull", "--platform", "linux/arm64", reference], timeout=180)


def prepare_candidate(
    candidate: Candidate,
    state: Path,
    source_repo: Path = SOURCE_REPO,
) -> dict[str, object]:
    if source_repo.resolve() != SOURCE_REPO.resolve():
        raise CandidateError("trusted_workflow_checkout_required")
    state = state.absolute()
    if state.exists() or state.is_symlink():
        raise CandidateError("fresh_candidate_state_required")
    state.mkdir(mode=0o700, parents=True)
    client = GhClient()
    release = validate_candidate(client, candidate)
    if release.get("immutable") is not True:
        raise CandidateError("immutable_release_required")
    endpoint = command(
        ["docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"]
    )
    if os.environ.get("DOCKER_HOST") or not endpoint.strip().startswith(b"unix:///"):
        raise CandidateError("local_docker_required")
    cache = state / "cache"
    adapter = state / "native-prepare"
    command(["go", "build", "-mod=readonly", "-trimpath", "-o", str(adapter),
             "./scripts/openaev/native-prepare"], cwd=source_repo, timeout=300)
    release_path = state / "release-input.json"
    release_path.write_text(json.dumps(release))
    release_path.chmod(0o600)
    raw = command([str(adapter), "--version", candidate.tag[1:], "--source-sha", candidate.source_sha,
                   "--published-at", candidate.published_at, "--trust", str(source_repo / "release/trust"),
                   "--state", str(cache), "--release-json", str(release_path)], timeout=510)
    native_output = state / "native-preparation.json"
    native_output.write_bytes(raw)
    native_output.chmod(0o600)
    result = _verified_output(json.loads(raw), candidate, cache)
    image_ref = resolve_image(candidate)
    if result.get("signed_image_ref") is not None and result["signed_image_ref"] != image_ref:
        raise CandidateError("image_signed_manifest_digest_mismatch")
    verify_image(image_ref, state, "candidate", candidate.source_sha, identity=(IDENTITY if is_nightly_tag(candidate.tag) else "https://github.com/Hikyo-Org/Hikyo/.github/workflows/release.yml@refs/tags/" + candidate.tag))
    result["image_ref"] = image_ref
    result["image_binary_sha256"] = verify_image_binary(
        image_ref, _text(result, "native_binary_sha256")
    )
    result["image_native_binary_verified"] = True
    validate_candidate(client, candidate)
    proof = {
        "format": "hikyo-hosted-release-candidate/v1",
        "release_id": candidate.release_id,
        "tag": candidate.tag,
        **result,
    }
    path = state / "candidate.json"
    path.write_text(json.dumps(proof, sort_keys=True, indent=2) + "\n")
    path.chmod(0o600)
    return proof
