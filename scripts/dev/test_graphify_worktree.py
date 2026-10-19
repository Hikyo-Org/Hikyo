"""Cache publication and isolation contracts for the Graphify worktree helper."""

import json
import os
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import patch

import graphify_worktree as helper

RUNTIME = {
    "graphify_version": "0.9.92",
    "ast_schema": 17,
    "grammars": "fixture",
    "graph_schema": 1,
}
COMMIT = "a" * 40


def write_graph(directory, commit=COMMIT):
    directory.mkdir(parents=True, exist_ok=True)
    (directory / "graph.json").write_text(
        json.dumps(
            {
                "graph": {"graphify_version": "0.9.92", "schema_version": 1},
                "built_at_commit": commit,
                "nodes": [],
                "links": [],
            }
        )
    )


class GraphifyWorktreeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="graphify helper test ")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.seed = self.root / "seed"
        write_graph(self.seed)
        (self.seed / "cache/ast").mkdir(parents=True)
        (self.seed / "cache/ast/fragment.json").write_text('{"source": "base"}')
        (self.seed / "manifest.json").write_text("{}")

    def test_seed_identity_tracks_commit_runtime_and_helper_schema(self):
        identity = helper.seed_identity(COMMIT, RUNTIME)
        key = helper.identity_key(identity)
        self.assertEqual(
            key, helper.identity_key(helper.seed_identity(COMMIT, dict(RUNTIME)))
        )
        self.assertNotEqual(
            key, helper.identity_key(helper.seed_identity("b" * 40, RUNTIME))
        )
        for field, changed in (
            ("graphify_version", "0.9.93"),
            ("ast_schema", 18),
            ("grammars", "changed"),
            ("graph_schema", 2),
        ):
            with self.subTest(field=field):
                runtime = {**RUNTIME, field: changed}
                self.assertNotEqual(
                    key, helper.identity_key(helper.seed_identity(COMMIT, runtime))
                )
        with patch.object(helper, "SEED_SCHEMA", helper.SEED_SCHEMA + 1):
            self.assertNotEqual(
                key, helper.identity_key(helper.seed_identity(COMMIT, RUNTIME))
            )

    def test_main_bootstrap_refreshes_shared_seed_without_checkout_graph(self):
        def fake_git(_repo, *arguments):
            responses = {
                ("rev-parse", "--show-toplevel"): str(self.root),
                ("rev-parse", "--path-format=absolute", "--git-common-dir"): str(
                    self.root / "common"
                ),
                ("branch", "--show-current"): "main",
                (
                    "rev-parse",
                    "--verify",
                    "refs/remotes/hikyo-upstream/main^{commit}",
                ): COMMIT,
            }
            return responses[arguments]

        with (
            patch.object(sys, "argv", ["graphify_worktree.py", "bootstrap"]),
            patch.dict(os.environ, {"AGENT_HEAVY_ACTIVE": "unit-test"}),
            patch.object(helper, "git", side_effect=fake_git),
            patch.object(helper.shutil, "which", return_value="graphify"),
            patch.object(helper, "runtime_fingerprint", return_value=RUNTIME),
            patch.object(helper, "ensure_seed", return_value=self.seed) as seed,
            patch.object(helper, "bootstrap") as bootstrap,
        ):
            self.assertEqual(helper.main(), 0)
        seed.assert_called_once_with(
            self.root, self.root / "common/hikyo-graphify", COMMIT, RUNTIME, "graphify"
        )
        bootstrap.assert_not_called()
        self.assertFalse((self.root / "graphify-out").exists())

    def test_copies_only_reusable_state_with_independent_files(self):
        for name in (
            ".rebuild.lock",
            ".pending_changes",
            ".pending_full",
            ".graphify_root",
            "README.md",
        ):
            (self.seed / name).write_text("do not copy")
        (self.seed / "memory").mkdir()
        (self.seed / "memory/private.json").write_text("private")
        (self.seed / "cache/last_query_stamp").write_text("session marker")
        (self.seed / "cache/worker.lock").write_text("process marker")
        destination = self.root / "destination"
        helper.copy_outputs(self.seed, destination)
        self.assertEqual(
            {p.name for p in destination.iterdir()},
            {"graph.json", "cache", "manifest.json"},
        )
        source = self.seed / "cache/ast/fragment.json"
        copied = destination / "cache/ast/fragment.json"
        self.assertFalse((destination / "cache/last_query_stamp").exists())
        self.assertFalse((destination / "cache/worker.lock").exists())
        self.assertNotEqual(source.stat().st_ino, copied.stat().st_ino)
        copied.write_text("worktree edit")
        self.assertEqual(source.read_text(), '{"source": "base"}')

    def test_rejects_nested_cache_symlink(self):
        (self.seed / "cache/ast/link.json").symlink_to(self.seed / "graph.json")
        with self.assertRaisesRegex(RuntimeError, "symlink"):
            helper.copy_outputs(self.seed, self.root / "destination")
        with self.assertRaisesRegex(RuntimeError, "symlink"):
            helper.validate_private_output(self.seed)

    def test_rejects_shared_output_directory(self):
        output = self.root / "shared"
        output.symlink_to(self.seed, target_is_directory=True)
        with self.assertRaisesRegex(RuntimeError, "symlink"):
            helper.validate_private_output(output)

    def test_bootstrap_preserves_readme_and_resets_root(self):
        repo = self.root / "worktree"
        output = repo / "graphify-out"
        output.mkdir(parents=True)
        (output / "README.md").write_text("tracked documentation")
        before = (self.seed / "graph.json").read_bytes()
        with patch.object(helper, "update") as update:
            helper.bootstrap(repo, self.seed, "graphify", RUNTIME)
        update.assert_called_once_with(repo, "graphify")
        self.assertEqual((output / "README.md").read_text(), "tracked documentation")
        saved_root = Path((output / ".graphify_root").read_text().strip())
        self.assertEqual((repo / saved_root).resolve(), repo.resolve())
        self.assertEqual((self.seed / "graph.json").read_bytes(), before)
        self.assertNotEqual(
            (output / "graph.json").stat().st_ino,
            (self.seed / "graph.json").stat().st_ino,
        )

    def test_existing_graph_is_not_replaced(self):
        repo = self.root / "worktree"
        output = repo / "graphify-out"
        write_graph(output, commit="b" * 40)
        before = (output / "graph.json").read_bytes()
        with (
            patch.object(helper, "copy_outputs") as copy,
            patch.object(helper, "update") as update,
        ):
            helper.bootstrap(repo, self.seed, "graphify", RUNTIME)
        copy.assert_not_called()
        update.assert_called_once_with(repo, "graphify")
        self.assertEqual((output / "graph.json").read_bytes(), before)

    def test_uninitialized_cache_conflict_preserves_existing_state(self):
        repo = self.root / "worktree"
        cache = repo / "graphify-out/cache"
        cache.mkdir(parents=True)
        (cache / "semantic.json").write_text("local semantic extraction")
        with (
            patch.object(helper, "update") as update,
            self.assertRaisesRegex(RuntimeError, "existing graph state"),
        ):
            helper.bootstrap(repo, self.seed, "graphify", RUNTIME)
        update.assert_not_called()
        self.assertEqual(
            (cache / "semantic.json").read_text(), "local semantic extraction"
        )
        self.assertFalse((repo / "graphify-out/graph.json").exists())

    def test_update_owns_output_and_disables_force_and_llm(self):
        with (
            patch.dict(
                os.environ,
                {
                    "GRAPHIFY_OUT": "/unrelated",
                    "GRAPHIFY_FORCE": "1",
                    "GRAPHIFY_BACKEND": "openai",
                },
            ),
            patch.object(helper.subprocess, "Popen") as run,
        ):
            run.return_value.wait.return_value = 0
            helper.update(self.root, "graphify")
        environment = run.call_args.kwargs["env"]
        self.assertEqual(environment["GRAPHIFY_OUT"], "graphify-out")
        self.assertEqual(environment["GRAPHIFY_BACKEND"], "none")
        self.assertEqual(environment["GRAPHIFY_MAX_WORKERS"], "2")
        self.assertEqual(environment["GRAPHIFY_NO_AUTO_REFRESH"], "1")
        self.assertNotIn("GRAPHIFY_FORCE", environment)
        self.assertEqual(run.call_args.kwargs["cwd"], self.root)

    def test_interrupted_update_waits_for_child_cleanup(self):
        with patch.object(helper.subprocess, "Popen") as process:
            child = process.return_value
            child.wait.side_effect = [SystemExit(143), 0]
            child.poll.return_value = None
            with self.assertRaises(SystemExit):
                helper.update(self.root, "graphify")
        child.send_signal.assert_called_once_with(signal.SIGINT)
        self.assertEqual(child.wait.call_count, 2)

    def test_seed_retention_keeps_current_and_newest_predecessor(self):
        seeds = self.root / "seeds"
        seeds.mkdir()
        history = []
        for index in range(4):
            identity = helper.seed_identity(str(index) * 40, RUNTIME)
            seed = seeds / helper.identity_key(identity)
            write_graph(seed, identity["commit"])
            (seed / "seed.json").write_text(json.dumps(identity))
            os.utime(seed, (index + 1, index + 1))
            history.append(seed)
            helper.prune_seeds(seeds, seed)
            self.assertEqual(set(seeds.iterdir()), set(history[max(0, index - 1) :]))
        foreign = seeds / "unowned"
        foreign.mkdir()
        helper.prune_seeds(seeds, history[-1])
        self.assertTrue(foreign.is_dir())

    def test_partial_bootstrap_rolls_back_published_sidecars(self):
        repo = self.root / "worktree"
        output = repo / "graphify-out"
        output.mkdir(parents=True)
        (output / "README.md").write_text("tracked documentation")
        original = Path.rename

        def interrupted_rename(path, target):
            if path.name == "graph.json":
                raise SystemExit(143)
            return original(path, target)

        with (
            patch.object(Path, "rename", interrupted_rename),
            self.assertRaises(SystemExit),
        ):
            helper.bootstrap(repo, self.seed, "graphify", RUNTIME)
        self.assertEqual({path.name for path in output.iterdir()}, {"README.md"})

    def test_sigterm_unwinds_owned_build_checkout(self):
        probe = self.root / "term_probe.py"
        probe.write_text(
            "import sys, time\n"
            "from pathlib import Path\n"
            f"sys.path.insert(0, {str(Path(helper.__file__).parent)!r})\n"
            "import graphify_worktree as h\n"
            "root = Path(sys.argv[1]); state = root/'state'; state.mkdir()\n"
            "def run(args, cwd, **kwargs):\n"
            "    if args[1:3] == ['worktree', 'add']:\n"
            "        Path(args[-2]).mkdir(parents=True)\n"
            "        (root/'registered').write_text(args[-2])\n"
            "    if args[1:3] == ['worktree', 'list']:\n"
            "        return 'worktree ' + (root/'registered').read_text() + '\\0'\n"
            "    if args[1:3] == ['worktree', 'remove']:\n"
            "        (root/'removed').touch()\n"
            "    return ''\n"
            "def update(*args):\n"
            "    (root/'ready').touch()\n"
            "    time.sleep(30)\n"
            "h.run = run; h.update = update; h.install_signal_handlers()\n"
            f"h.ensure_seed(root, state, {COMMIT!r}, {RUNTIME!r}, 'graphify')\n"
        )
        with subprocess.Popen([sys.executable, str(probe), str(self.root)]) as child:
            try:
                deadline = time.monotonic() + 5
                while not (self.root / "ready").exists():
                    if child.poll() is not None or time.monotonic() > deadline:
                        self.fail("Cancellation fixture did not reach the owned build")
                    time.sleep(0.01)
                child.terminate()
                self.assertEqual(child.wait(timeout=5), 143)
            finally:
                if child.poll() is None:
                    child.kill()
                    child.wait()
        self.assertTrue((self.root / "removed").exists())
        self.assertEqual(list((self.root / "state").glob("build-*")), [])

    def test_bad_version_or_commit_is_rejected(self):
        with self.assertRaisesRegex(RuntimeError, "expected commit"):
            helper.validate_graph(self.seed, RUNTIME, "b" * 40)
        with self.assertRaisesRegex(RuntimeError, "Incompatible"):
            helper.validate_graph(self.seed, {**RUNTIME, "graphify_version": "0.9.93"})

    def test_graph_header_without_topology_is_rejected(self):
        path = self.seed / "graph.json"
        data = json.loads(path.read_text())
        del data["nodes"]
        path.write_text(json.dumps(data))
        with self.assertRaisesRegex(TypeError, "topology"):
            helper.validate_graph(self.seed, RUNTIME)

    def test_base_failure_does_not_publish_and_removes_owned_checkout(self):
        state = self.root / "state"
        state.mkdir()
        prior = (
            state
            / "seeds"
            / helper.identity_key(helper.seed_identity("b" * 40, RUNTIME))
        )
        write_graph(prior, "b" * 40)
        (prior / "seed.json").write_text(
            json.dumps(helper.seed_identity("b" * 40, RUNTIME))
        )
        calls = []
        registered = []

        def fake_run(arguments, cwd, **kwargs):
            calls.append(arguments)
            if arguments[1:3] == ["worktree", "add"]:
                (Path(arguments[-2]) / "graphify-out").mkdir(parents=True)
                registered.append(arguments[-2])
            if arguments[1:3] == ["worktree", "list"]:
                return f"worktree {registered[-1]}\0"
            return ""

        error = subprocess.CalledProcessError(1, ["graphify", "update", "."])
        with (
            patch.object(helper, "run", side_effect=fake_run),
            patch.object(helper, "update", side_effect=error),
            self.assertRaises(subprocess.CalledProcessError),
        ):
            helper.ensure_seed(self.root, state, COMMIT, RUNTIME, "graphify")
        self.assertTrue(any(call[1:3] == ["worktree", "remove"] for call in calls))
        self.assertEqual(list((state / "seeds").iterdir()), [prior])

    def test_failed_worktree_add_preserves_original_failure(self):
        state = self.root / "state"
        state.mkdir()
        error = subprocess.CalledProcessError(128, ["git", "worktree", "add"])

        def fake_run(arguments, _cwd, **_kwargs):
            if arguments[1:3] == ["worktree", "add"]:
                raise error
            return ""

        with (
            patch.object(helper, "run", side_effect=fake_run),
            self.assertRaises(subprocess.CalledProcessError) as caught,
        ):
            helper.ensure_seed(self.root, state, COMMIT, RUNTIME, "graphify")
        self.assertIs(caught.exception, error)
        self.assertEqual(list((state / "seeds").iterdir()), [])

    def test_sigterm_during_worktree_add_removes_late_registration(self):
        fake_git = self.root / "fake_git.py"
        fake_git.write_text(
            "import sys, time\nfrom pathlib import Path\n"
            "root=Path(__file__).parent; args=sys.argv[1:]\n"
            "if args[:2] == ['worktree', 'add']:\n"
            "    Path(args[-2]).mkdir(parents=True)\n"
            "    (root/'registered').write_text(args[-2])\n"
            "    (root/'ready').touch()\n"
            "    time.sleep(0.3)\n"
            "elif args[:2] == ['worktree', 'list']:\n"
            "    print('worktree ' + (root/'registered').read_text() + '\\0')\n"
            "elif args[:2] == ['worktree', 'remove']:\n"
            "    (root/'registered').unlink(); (root/'removed').touch()\n"
        )
        probe = self.root / "add_probe.py"
        probe.write_text(
            "import sys\nfrom pathlib import Path\n"
            f"sys.path.insert(0, {str(Path(helper.__file__).parent)!r})\n"
            "import graphify_worktree as h\n"
            "root=Path(__file__).parent; state=root/'state'; state.mkdir()\n"
            "original=h.run\n"
            "def run(args, cwd, **kwargs):\n"
            "    return original([sys.executable,str(root/'fake_git.py'),*args[1:]],cwd,**kwargs)\n"
            "h.run=run; h.install_signal_handlers()\n"
            f"h.ensure_seed(root,state,{COMMIT!r},{RUNTIME!r},'graphify')\n"
        )
        with subprocess.Popen([sys.executable, str(probe)]) as child:
            try:
                deadline = time.monotonic() + 5
                while not (self.root / "ready").exists():
                    if child.poll() is not None or time.monotonic() > deadline:
                        self.fail("Worktree-add fixture did not register its checkout")
                    time.sleep(0.01)
                child.terminate()
                self.assertEqual(child.wait(timeout=5), 143)
            finally:
                if child.poll() is None:
                    child.kill()
                    child.wait()
        self.assertTrue((self.root / "removed").exists())
        self.assertFalse((self.root / "registered").exists())
        self.assertEqual(list((self.root / "state").glob("build-*")), [])


if __name__ == "__main__":
    unittest.main()
