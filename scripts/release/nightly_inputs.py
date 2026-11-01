#!/usr/bin/env python3
"""Restore dev-built payloads without replacing trusted publisher tooling."""

import os
from pathlib import Path, PurePosixPath
import shutil
import sys
import tarfile
import tempfile


MAX_ENTRIES = 4096
MAX_BYTES = 4 * 1024 * 1024 * 1024
ROOTS = frozenset({"dist", "nightly-context"})
REQUIRED = frozenset({"dist/metadata.json", "nightly-context/source-validation.json"})


def restore(archive_path: Path, destination: Path) -> None:
    """Validate the complete archive before installing either payload root."""
    for name in ROOTS:
        target = destination / name
        if target.exists() or target.is_symlink():
            raise ValueError(f"nightly input destination already exists: {name}")

    with tarfile.open(archive_path, "r:gz") as archive:
        entries = []
        names = set()
        total = 0
        for entry in archive:
            name = entry.name.rstrip("/")
            path = PurePosixPath(name)
            if (
                not name
                or path.is_absolute()
                or "\\" in name
                or any(part in {"", ".", ".."} for part in name.split("/"))
                or path.parts[0] not in ROOTS
                or name in names
                or not (entry.isdir() or entry.isreg())
                or (len(path.parts) == 1 and not entry.isdir())
            ):
                raise ValueError("unsafe or duplicate nightly input entry")
            names.add(name)
            entries.append(entry)
            total += entry.size
            if len(entries) > MAX_ENTRIES or entry.size < 0 or total > MAX_BYTES:
                raise ValueError("nightly input archive exceeds its resource bounds")
        if not REQUIRED.issubset(names):
            raise ValueError("nightly input archive lacks source or payload metadata")
        for entry in entries:
            if entry.name.rstrip("/") in REQUIRED and (not entry.isreg() or entry.size == 0):
                raise ValueError("nightly input metadata must be a nonempty regular file")

        with tempfile.TemporaryDirectory(prefix=".nightly-inputs-", dir=destination) as scratch:
            staged = Path(scratch)
            for entry in entries:
                target = staged / entry.name.rstrip("/")
                if entry.isdir():
                    target.mkdir(parents=True, exist_ok=True)
                    continue
                target.parent.mkdir(parents=True, exist_ok=True)
                source = archive.extractfile(entry)
                if source is None:
                    raise ValueError("nightly input file cannot be read")
                with source, target.open("xb") as output:
                    shutil.copyfileobj(source, output, length=128 * 1024)
                os.chmod(target, entry.mode & 0o777)
            for name in sorted(ROOTS):
                os.replace(staged / name, destination / name)


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: nightly_inputs.py ARCHIVE", file=sys.stderr)
        return 2
    try:
        restore(Path(sys.argv[1]), Path.cwd())
    except (OSError, ValueError, EOFError, tarfile.TarError) as error:
        print(f"nightly inputs: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
