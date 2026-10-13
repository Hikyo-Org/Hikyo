"""Identity of the complete trusted hosted harness and native verifier sources."""

import hashlib
from pathlib import Path


def source_fingerprint() -> str:
    here = Path(__file__).resolve().parent
    root = here.parents[1]
    runtime_extensions = {".py", ".json", ".go", ".sh", ".lock", ".yaml", ".yml"}
    paths = {path for path in here.rglob("*") if path.is_file()
             and (path.suffix in runtime_extensions or path.name == "Dockerfile")
             and not any(part.startswith(".") or part == "__pycache__"
                         for part in path.relative_to(here).parts)}
    paths.update((root / "internal").rglob("*.go"))
    paths.update(path for path in (root / "release/trust").rglob("*") if path.is_file())
    paths.update((root / "go.mod", root / "go.sum"))
    digest = hashlib.sha256()
    for path in sorted(paths):
        if path.is_symlink():
            raise ValueError("suite_source_symlink_forbidden")
        digest.update(path.relative_to(root).as_posix().encode() + b"\0" + path.read_bytes())
    return digest.hexdigest()
