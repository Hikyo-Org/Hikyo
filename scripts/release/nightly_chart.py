"""Prepare source-bound nightly charts and compare packages without extracting them."""

import argparse
import re
import shutil
import tarfile
from pathlib import Path, PurePosixPath

import yaml


def prepare(source: Path, destination: Path, repository: str, version: str,
            commit: str, image_digest: str) -> None:
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9][A-Za-z0-9_.-]*", repository):
        raise ValueError("invalid source repository")
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("invalid source commit")
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+-nightly\.[0-9]{8}\.[1-9][0-9]*\.g[0-9a-f]{8}", version):
        raise ValueError("invalid nightly version")
    if not version.endswith(".g" + commit[:8]):
        raise ValueError("nightly version does not identify the source commit")
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", image_digest):
        raise ValueError("invalid image digest")
    if any(path.is_symlink() for path in source.rglob("*")):
        raise ValueError("source chart must not follow symbolic links")
    shutil.copytree(source, destination)
    metadata_path = destination / "Chart.yaml"
    metadata = yaml.safe_load(metadata_path.read_text())
    if metadata["name"] != "hikyo" or metadata["type"] != "application":
        raise ValueError("unexpected chart identity")
    metadata["version"] = version
    metadata["appVersion"] = version
    metadata.setdefault("annotations", {}).update({
        "org.opencontainers.image.source": "https://github.com/" + repository,
        "org.opencontainers.image.revision": commit,
        "hikyo.dev/release-profile": "nightly/v1",
    })
    metadata_path.write_text(yaml.safe_dump(metadata, sort_keys=False))
    values_path = destination / "values.yaml"
    values = yaml.safe_load(values_path.read_text())
    values["image"] = {
        "repository": "ghcr.io/" + repository.lower(),
        "digest": image_digest,
    }
    values["updates"]["channel"] = "nightly"
    values["releaseTrust"]["admissionPolicy"]["mode"] = "keyless"
    values_path.write_text(yaml.safe_dump(values, sort_keys=False))


def package_files(package: Path) -> dict[str, bytes]:
    files = {}
    size = 0
    with tarfile.open(package, "r:gz") as archive:
        for member in archive:
            path = PurePosixPath(member.name)
            if (path.is_absolute() or ".." in path.parts or not path.parts
                    or path.parts[0] != "hikyo"):
                raise ValueError("unexpected chart archive path")
            if member.isdir():
                continue
            if not member.isfile() or member.name in files:
                raise ValueError("chart contains a link, special file or duplicate")
            size += member.size
            if size > 20 * 1024 * 1024:
                raise ValueError("chart exceeds the package inspection bound")
            stream = archive.extractfile(member)
            if stream is None:
                raise ValueError("unreadable chart member")
            with stream:
                files[member.name] = stream.read()
    if not {"hikyo/Chart.yaml", "hikyo/values.yaml"} <= files.keys():
        raise ValueError("chart metadata or values are missing")
    return files


def compare(expected: Path, actual: Path) -> None:
    # Helm records packaging time. Authenticate the OCI digest separately, then
    # compare every uncompressed source byte rather than gzip/tar timestamps.
    if package_files(expected) != package_files(actual):
        raise ValueError("published chart differs from the source-bound package")


def check_rendered(path: Path, repository: str, image_digest: str) -> None:
    image = "ghcr.io/" + repository.lower() + "@" + image_digest
    servers = 0
    for document in yaml.safe_load_all(path.read_text()):
        if not document or document.get("kind") not in ("Deployment", "StatefulSet"):
            continue
        pod = document["spec"]["template"]["spec"]
        for container in pod.get("initContainers", []) + pod["containers"]:
            if container["image"] != image:
                raise ValueError("rendered container does not use the verified image")
            if container["name"] == "server":
                servers += 1
                env = {item["name"]: item.get("value") for item in container["env"]}
                if env.get("HIKYO_ROOT_KEY_FILE") != "/run/hikyo-root-key/root-key":
                    raise ValueError("host-only CLI bootstrap cannot resolve the staged root key")
    if servers != 1:
        raise ValueError("expected one rendered server container")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    build = commands.add_parser("prepare")
    for name in ("source", "destination", "repository", "version", "commit", "image-digest"):
        build.add_argument("--" + name, required=True)
    check = commands.add_parser("compare")
    check.add_argument("expected", type=Path)
    check.add_argument("actual", type=Path)
    rendered = commands.add_parser("check-rendered")
    rendered.add_argument("path", type=Path)
    rendered.add_argument("--repository", required=True)
    rendered.add_argument("--image-digest", required=True)
    args = parser.parse_args()
    if args.command == "prepare":
        prepare(Path(args.source), Path(args.destination), args.repository,
                args.version, args.commit, args.image_digest)
    elif args.command == "compare":
        compare(args.expected, args.actual)
    else:
        check_rendered(args.path, args.repository, args.image_digest)


if __name__ == "__main__":
    main()
