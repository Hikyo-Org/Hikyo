#!/usr/bin/env python3
"""Seed private worktree graphs from a compatible, committed-main AST cache."""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import shlex
import shutil
import signal
import subprocess
import sys
import tempfile
from pathlib import Path

SEED_SCHEMA = 1
OUTPUT = "graphify-out"


def run(arguments: list[str], cwd: Path, *, capture: bool = False) -> str:
    result = subprocess.run(
        arguments,
        cwd=cwd,
        check=True,
        text=True,
        stdout=subprocess.PIPE if capture else None,
    )
    return result.stdout.strip() if capture else ""


def git(repo: Path, *arguments: str) -> str:
    return run(["git", *arguments], repo, capture=True)


def runtime_fingerprint(executable: str) -> dict:
    """Ask the CLI's own interpreter, rather than the caller's Python install."""
    launcher = Path(executable).resolve()
    with launcher.open(encoding="utf-8") as source:
        shebang = source.readline().strip()
    if not shebang.startswith("#!"):
        raise RuntimeError("graphify must be a Python console script with a shebang")
    interpreter = shlex.split(shebang[2:])
    if not interpreter:
        raise RuntimeError("graphify has an empty interpreter shebang")
    probe = (
        "import json; import graphify.cache as c; import graphify.export as e; "
        "print(json.dumps({'graphify_version': c._EXTRACTOR_VERSION, "
        "'ast_schema': c._AST_CACHE_SCHEMA, 'grammars': c._grammar_fingerprint(), "
        "'graph_schema': e.GRAPH_SCHEMA_VERSION}))"
    )
    data = json.loads(run([*interpreter, "-c", probe], launcher.parent, capture=True))
    if (
        not isinstance(data, dict)
        or not isinstance(data.get("graphify_version"), str)
        or data["graphify_version"] == "unknown"
        or not isinstance(data.get("ast_schema"), int)
        or not isinstance(data.get("graph_schema"), int)
        or data.get("grammars") in (None, "unknown", "none")
    ):
        raise RuntimeError("Cannot establish Graphify runtime/parser compatibility")
    return data


def seed_identity(commit: str, runtime: dict) -> dict:
    return {"helper_schema": SEED_SCHEMA, "commit": commit, "runtime": runtime}


def identity_key(identity: dict) -> str:
    return hashlib.sha256(json.dumps(identity, sort_keys=True).encode()).hexdigest()


def validate_graph(directory: Path, runtime: dict, commit: str | None = None) -> None:
    graph = directory / "graph.json"
    if graph.is_symlink():
        raise RuntimeError(f"Refusing symlinked graph: {graph}")
    with graph.open(encoding="utf-8") as source:
        data = json.load(source)
    if not isinstance(data, dict) or not isinstance(data.get("graph"), dict):
        raise TypeError(f"Invalid graph metadata: {graph}")
    if not isinstance(data.get("nodes"), list) or not isinstance(
        data.get("links"), list
    ):
        raise TypeError(f"Invalid graph topology: {graph}")
    if (
        data["graph"].get("graphify_version") != runtime["graphify_version"]
        or data["graph"].get("schema_version") != runtime["graph_schema"]
    ):
        raise RuntimeError(f"Incompatible graph metadata: {graph}")
    if commit is not None and data.get("built_at_commit") != commit:
        raise RuntimeError(f"Graph was not built at expected commit {commit}: {graph}")


def selected_artifact(name: str) -> bool:
    """Copy extraction state only, excluding process locks and personal memory."""
    return name in {
        "graph.json",
        "manifest.json",
        "cache",
        ".graphify_build.json",
        ".graphify_labels.json",
        ".graphify_labels.json.sig",
        ".graphify_labels.pending.json",
    }


def copy_file(source: str, destination: str) -> str:
    # APFS clones save space while preserving independent, mutable inodes.
    if sys.platform == "darwin":
        clone = subprocess.run(
            [
                "/bin/cp",
                "-cp",
                str(Path(source).absolute()),
                str(Path(destination).absolute()),
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )
        if clone.returncode == 0:
            return destination
    # Other filesystems still receive independent copies, never hardlinks.
    return shutil.copy2(source, destination)


def validate_private_output(directory: Path) -> None:
    if directory.is_symlink():
        raise RuntimeError(f"Refusing symlinked graph output directory: {directory}")
    for current, directories, files in os.walk(directory, followlinks=False):
        for name in [*directories, *files]:
            path = Path(current) / name
            if path.is_symlink():
                raise RuntimeError(f"Refusing symlinked graph artifact: {path}")


def cache_ignored(_directory: str, names: list[str]) -> set[str]:
    return {
        name
        for name in names
        if name.endswith(".lock")
        or name.startswith(".lock")
        or name == "needs_update"
        or ("stamp" in name and ("query" in name or "session" in name))
    }


def copy_outputs(source: Path, destination: Path) -> None:
    """Copy the allowlisted state and reject links even inside cache trees."""
    if source.is_symlink() or destination.is_symlink():
        raise RuntimeError("Graph output directories must not be symlinks")
    destination.mkdir(parents=True, exist_ok=True)
    for entry in source.iterdir():
        if not selected_artifact(entry.name):
            continue
        if entry.is_symlink():
            raise RuntimeError(f"Refusing symlinked graph artifact: {entry}")
        target = destination / entry.name
        if target.exists() or target.is_symlink():
            raise RuntimeError(f"Refusing to replace graph artifact: {target}")
        if entry.is_dir():
            for current, directories, files in os.walk(entry, followlinks=False):
                for name in [*directories, *files]:
                    path = Path(current) / name
                    if path.is_symlink():
                        raise RuntimeError(f"Refusing symlinked graph artifact: {path}")
            shutil.copytree(
                entry, target, copy_function=copy_file, ignore=cache_ignored
            )
        else:
            copy_file(str(entry), str(target))


def update(repo: Path, executable: str) -> None:
    validate_private_output(repo / OUTPUT)
    environment = os.environ.copy()
    environment.update(
        {
            "GRAPHIFY_BACKEND": "none",
            "GRAPHIFY_NO_AUTO_REFRESH": "1",
            "GRAPHIFY_MAX_WORKERS": "2",
            "GRAPHIFY_OUT": OUTPUT,
            "GRAPHIFY_REPO_ROOT": str(repo),
        }
    )
    # A caller's force flag must not disable Graphify's deletion/shrink guard.
    environment.pop("GRAPHIFY_FORCE", None)
    command = [executable, "update", "."]
    child = subprocess.Popen(command, cwd=repo, env=environment)
    try:
        code = child.wait()
    except BaseException:
        # Let Graphify unwind its own extraction workers before removing the
        # owned temporary checkout. Keep children in the queue's process group.
        if child.poll() is None:
            child.send_signal(signal.SIGINT)
        child.wait()
        raise
    if code:
        raise subprocess.CalledProcessError(code, command)


def install_signal_handlers() -> None:
    def terminate(signum, _frame):
        raise SystemExit(128 + signum)

    signal.signal(signal.SIGTERM, terminate)
    signal.signal(signal.SIGHUP, terminate)


def compatible_seed(seed: Path, identity: dict) -> bool:
    metadata = seed / "seed.json"
    if seed.is_symlink() or not metadata.is_file() or metadata.is_symlink():
        return False
    with metadata.open(encoding="utf-8") as source:
        return json.load(source) == identity


def prune_seeds(seeds: Path, current: Path) -> None:
    """Retain the current seed and newest predecessor under the shared lock."""
    owned = []
    for candidate in seeds.iterdir():
        metadata = candidate / "seed.json"
        if candidate.is_symlink() or not candidate.is_dir() or metadata.is_symlink():
            continue
        try:
            identity = json.loads(metadata.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            continue
        if isinstance(identity, dict) and identity_key(identity) == candidate.name:
            owned.append(candidate)
    predecessors = sorted(
        (seed for seed in owned if seed != current),
        key=lambda seed: seed.stat().st_mtime,
        reverse=True,
    )
    for seed in predecessors[1:]:
        shutil.rmtree(seed)


def ensure_seed(
    repo: Path, state: Path, commit: str, runtime: dict, executable: str
) -> Path:
    identity = seed_identity(commit, runtime)
    seeds = state / "seeds"
    seeds.mkdir(parents=True, exist_ok=True)
    seed = seeds / identity_key(identity)
    if seed.exists():
        if not compatible_seed(seed, identity):
            raise RuntimeError(f"Invalid existing seed: {seed}")
        validate_graph(seed, runtime, commit)
        prune_seeds(seeds, seed)
        print(f"Graphify: reusing committed base {commit[:12]}", flush=True)
        return seed

    prior = None
    for candidate in sorted(
        seeds.iterdir(), key=lambda item: item.stat().st_mtime, reverse=True
    ):
        if candidate.is_symlink() or not (candidate / "seed.json").is_file():
            continue
        with (candidate / "seed.json").open(encoding="utf-8") as source:
            metadata = json.load(source)
        if (
            isinstance(metadata, dict)
            and metadata.get("helper_schema") == SEED_SCHEMA
            and metadata.get("runtime") == runtime
        ):
            validate_graph(candidate, runtime, metadata.get("commit"))
            prior = candidate
            break

    print(f"Graphify: building committed base {commit[:12]}", flush=True)
    with tempfile.TemporaryDirectory(prefix="build-", dir=state) as temporary:
        root = Path(temporary)
        checkout = root / "checkout"
        try:
            run(["git", "worktree", "add", "--detach", str(checkout), commit], repo)
            output = checkout / OUTPUT
            if prior is not None:
                copy_outputs(prior, output)
                (output / ".graphify_root").write_text(".", encoding="utf-8")
            update(checkout, executable)
            validate_graph(output, runtime, commit)
            staged = root / "seed"
            copy_outputs(output, staged)
            (staged / "seed.json").write_text(
                json.dumps(identity, indent=2) + "\n", encoding="utf-8"
            )
            staged.rename(seed)
        finally:
            # Git may finish registering the checkout while a signal unwinds
            # run(). Probe the uniquely owned path even if add never returned.
            registered = git(repo, "worktree", "list", "--porcelain", "-z")
            if f"worktree {checkout}" in registered.split("\0"):
                run(["git", "worktree", "remove", "--force", str(checkout)], repo)
    prune_seeds(seeds, seed)
    return seed


def bootstrap(repo: Path, seed: Path | None, executable: str, runtime: dict) -> None:
    output = repo / OUTPUT
    if output.is_symlink() or (output / "graph.json").is_symlink():
        raise RuntimeError("Graphify requires private, non-symlinked worktree output")
    if not (output / "graph.json").exists():
        if seed is None:
            raise RuntimeError("A base seed is required for a new worktree graph")
        # Preserve the tracked README and any other pre-existing local files.
        with tempfile.TemporaryDirectory(
            prefix=".graphify-seed-", dir=repo
        ) as temporary:
            staged = Path(temporary) / "output"
            copy_outputs(seed, staged)
            (staged / ".graphify_root").write_text(".", encoding="utf-8")
            output.mkdir(exist_ok=True)
            for artifact in staged.iterdir():
                target = output / artifact.name
                if target.exists() or target.is_symlink():
                    raise RuntimeError(
                        f"Refusing to replace existing graph state: {target}"
                    )
            # Install graph.json atomically and last, preserving the tracked
            # README in place. Interrupted publication fails closed on retry.
            artifacts = sorted(
                staged.iterdir(), key=lambda path: path.name == "graph.json"
            )
            published = []
            try:
                for artifact in artifacts:
                    target = output / artifact.name
                    artifact.rename(target)
                    published.append(target)
            except BaseException:
                for artifact in reversed(published):
                    artifact.rename(staged / artifact.name)
                raise
        print(
            "Graphify: seeded private worktree cache; updating changed files",
            flush=True,
        )
    else:
        print("Graphify: updating existing private worktree graph", flush=True)
    update(repo, executable)
    validate_graph(output, runtime)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "command",
        choices=("bootstrap", "refresh-base", "install"),
        nargs="?",
        default="bootstrap",
    )
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--base-ref", default="refs/remotes/hikyo-upstream/main")
    args = parser.parse_args()
    repo = Path(git(args.repo, "rev-parse", "--show-toplevel"))
    state = (
        Path(git(repo, "rev-parse", "--path-format=absolute", "--git-common-dir"))
        / "hikyo-graphify"
    )
    state.mkdir(parents=True, exist_ok=True)
    if args.command == "bootstrap" and git(repo, "branch", "--show-current") == "main":
        print(
            "Graphify: main refreshes the shared seed only; no checkout graph is created",
            flush=True,
        )
        args.command = "refresh-base"
    if args.command == "install":
        with tempfile.NamedTemporaryFile(
            dir=state, prefix="bootstrap-", delete=False
        ) as temporary:
            staged = Path(temporary.name)
            try:
                temporary.write(Path(__file__).read_bytes())
                temporary.flush()
                staged.chmod(0o755)
                staged.replace(state / "bootstrap.py")
            finally:
                staged.unlink(missing_ok=True)
        print(
            f"Installed Graphify bootstrap in Git common directory: {state / 'bootstrap.py'}"
        )
        return 0

    if not os.environ.get("AGENT_HEAVY_ACTIVE"):
        queue = shutil.which("agent-heavy")
        if queue:
            label = "Hikyo Graphify " + args.command
            os.execv(
                queue,
                [
                    queue,
                    "run",
                    "--label",
                    label,
                    "--",
                    sys.executable,
                    str(Path(__file__).resolve()),
                    *sys.argv[1:],
                ],
            )
        print(
            "Graphify: agent-heavy is unavailable; running locally without shared scheduling",
            file=sys.stderr,
        )
    executable = shutil.which("graphify")
    if executable is None:
        raise RuntimeError("graphify is not installed or is missing from PATH")
    runtime = runtime_fingerprint(executable)
    with (state / "bootstrap.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        output = repo / OUTPUT
        if output.is_symlink() or (output / "graph.json").is_symlink():
            raise RuntimeError(
                "Graphify requires private, non-symlinked worktree output"
            )
        seed = None
        if args.command == "refresh-base" or not (output / "graph.json").exists():
            try:
                commit = git(
                    repo, "rev-parse", "--verify", f"{args.base_ref}^{{commit}}"
                )
            except subprocess.CalledProcessError as error:
                raise RuntimeError(
                    f"Base ref {args.base_ref} is unavailable. Fetch hikyo-upstream main "
                    "or select an existing committed base with --base-ref."
                ) from error
            seed = ensure_seed(repo, state, commit, runtime, executable)
        if args.command == "bootstrap":
            bootstrap(repo, seed, executable, runtime)
    return 0


if __name__ == "__main__":
    install_signal_handlers()
    try:
        raise SystemExit(main())
    except (
        OSError,
        RuntimeError,
        TypeError,
        ValueError,
        subprocess.CalledProcessError,
    ) as error:
        print(f"Graphify bootstrap failed: {error}", file=sys.stderr)
        raise SystemExit(1) from error
