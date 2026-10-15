"""Python tooling imports must not dirty the exact CI candidate checkout."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class SourceHygieneTests(unittest.TestCase):
    def test_generated_bytecode_is_ignored_but_new_source_is_visible(self):
        ignore = Path(__file__).parents[3] / ".gitignore"
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".gitignore").write_text(ignore.read_text())
            module = root / "scripts/ci/runtime_fixture.py"
            module.parent.mkdir(parents=True)
            module.write_text("value = 1\n")

            def git(*args):
                return subprocess.check_output(
                    ["git", "-C", directory, *args], text=True
                ).strip()

            git("init", "-q")
            git("config", "core.excludesFile", os.devnull)
            git("add", ".gitignore", "scripts/ci/runtime_fixture.py")
            environment = dict(os.environ)
            environment.pop("PYTHONDONTWRITEBYTECODE", None)
            environment.pop("PYTHONPYCACHEPREFIX", None)
            subprocess.run(
                [
                    sys.executable,
                    "-c",
                    "import importlib.util, sys; "
                    "spec = importlib.util.spec_from_file_location('fixture', sys.argv[1]); "
                    "module = importlib.util.module_from_spec(spec); "
                    "spec.loader.exec_module(module)",
                    str(module),
                ],
                check=True,
                env=environment,
            )
            self.assertTrue(list(module.parent.glob("__pycache__/*.pyc")))
            self.assertEqual(git("ls-files", "--others", "--exclude-standard"), "")
            (module.parent / "new_source.py").write_text("value = 2\n")
            self.assertEqual(
                git("ls-files", "--others", "--exclude-standard"),
                "scripts/ci/new_source.py",
            )
