"""Run with uv run --with pyotp python -m unittest discover ... ."""

import importlib.util
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "fixture", Path(__file__).with_name("rehearsal-fixture.py")
)
fixture_module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture_module)


class FixtureChecks(unittest.TestCase):
    def test_mcp_verification_rejects_missing_or_enabled_write_catalog(self):
        for tools in (
            None,
            [],
            [{"name": "hikyo_stage_change"}],
            [{"name": "hikyo_validate_change"}],
        ):
            with self.subTest(tools=tools):
                fixture = fixture_module.Fixture.__new__(fixture_module.Fixture)
                fixture.origin = "https://127.0.0.1:1234"
                with patch.object(
                    fixture_module.urllib.request.OpenerDirector,
                    "open",
                    return_value=io.BytesIO(
                        json.dumps({"result": {"tools": tools}}).encode()
                    ),
                ):
                    fixture.http = fixture_module.urllib.request.OpenerDirector()
                    with self.assertRaises(fixture_module.Failure):
                        fixture.verify_mcp_disabled()

    def test_mcp_verification_records_read_only_catalog(self):
        fixture = fixture_module.Fixture.__new__(fixture_module.Fixture)
        fixture.origin = "https://127.0.0.1:1234"
        response = {"result": {"tools": [{"name": "hikyo_list_definitions"}]}}
        with (
            patch.object(
                fixture_module.urllib.request.OpenerDirector,
                "open",
                return_value=io.BytesIO(json.dumps(response).encode()),
            ) as opened,
            patch.object(fixture, "write") as write,
            patch.object(fixture_module.sys, "argv", ["fixture", "verify"]),
        ):
            fixture.http = fixture_module.urllib.request.OpenerDirector()
            fixture.verify_mcp_disabled()
        self.assertEqual(opened.call_args.args[0].full_url, fixture.origin + "/mcp")
        self.assertTrue(json.loads(write.call_args.args[1])["write_tools_absent"])

    def test_remote_origin_refused_before_credentials_or_commands(self):
        with (
            patch.dict(
                "os.environ",
                {
                    "HIKYO_REHEARSAL_CONTAINER": "anything",
                    "HIKYO_REHEARSAL_URL": "https://hikyo.dbugit.io",
                },
                clear=True,
            ),
            self.assertRaisesRegex(fixture_module.Failure, "loopback"),
        ):
            fixture_module.Fixture()

    def test_verify_rejects_unowned_container_before_read_or_mutation(self):
        fixture = fixture_module.Fixture.__new__(fixture_module.Fixture)
        fixture.container = "unrelated"
        with (
            patch.dict("os.environ", {"HIKYO_REHEARSAL_RUN_ID": "expected"}),
            patch.object(
                fixture,
                "command",
                return_value=b'[{"Config":{"Labels":{"io.hikyo.dbugit.rehearsal":"other"}}}]',
            ) as command,
            patch.object(fixture, "cli") as cli,
            self.assertRaisesRegex(fixture_module.Failure, "not owned"),
        ):
            fixture.verify()
        command.assert_called_once()
        cli.assert_not_called()

    def test_cli_uses_container_visible_directory_owner(self):
        fixture = fixture_module.Fixture.__new__(fixture_module.Fixture)
        fixture.directory = Path("/private/synthetic-fixture")
        fixture.container = "owned-rehearsal-source"
        helper = "postgres:18@sha256:" + "a" * 64
        with (
            patch.dict("os.environ", {"HIKYO_REHEARSAL_HELPER_IMAGE": helper}),
            patch.object(fixture, "command", side_effect=[b"0:0\n", b"ok"]) as command,
        ):
            fixture.cli("values", "export", machine=False)
        self.assertTrue(
            any(
                "dst=/fixture,readonly" in arg
                for arg in command.call_args_list[0].args[0]
            )
        )
        args = command.call_args_list[1].args[0]
        self.assertEqual(args[args.index("--user") + 1], "0:0")
        self.assertIn("--read-only", args)

    def test_http_redirect_cannot_forward_authentication(self):
        with self.assertRaisesRegex(fixture_module.Failure, "redirect"):
            fixture_module.NoRedirect().redirect_request(
                None, None, 302, "", {}, "https://elsewhere.invalid"
            )

    def test_export_mismatch_stops_before_preview_mutation(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = fixture_module.Fixture.__new__(fixture_module.Fixture)
            fixture.directory = Path(directory)
            fixture.owned_container = dict
            (fixture.directory / "fixture.json").write_text(
                json.dumps(
                    {
                        "environment": "env_test",
                        "values": {"REHEARSAL_SECRET": "test-only-secret"},
                    }
                )
            )
            calls = []

            def export(*args, **kwargs):
                calls.append(args)
                output = args[args.index("--output-file") + 1]
                (fixture.directory / Path(output).name).write_text(
                    json.dumps(
                        {"items": [{"name": "REHEARSAL_SECRET", "value": "changed"}]}
                    )
                )

            fixture.cli = export
            with self.assertRaises(fixture_module.Failure) as result:
                fixture.verify()
            self.assertEqual(len(calls), 1)
            self.assertEqual(calls[0][:2], ("values", "export"))
            self.assertNotIn("test-only-secret", str(result.exception))
            self.assertNotIn(
                "changed", str(result.exception).replace("changed across", "")
            )

    def test_approval_request_is_not_successful_publication(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = fixture_module.Fixture.__new__(fixture_module.Fixture)
            fixture.directory = Path(directory)
            fixture.owned_container = dict
            values = {"REHEARSAL_CONFIG": "test"}
            (fixture.directory / "fixture.json").write_text(
                json.dumps({"environment": "env_test", "values": values})
            )

            def cli(*args, **kwargs):
                if args[:2] == ("values", "export"):
                    output = args[args.index("--output-file") + 1]
                    (fixture.directory / Path(output).name).write_text(
                        json.dumps(
                            {"items": [{"name": "REHEARSAL_CONFIG", "value": "test"}]}
                        )
                    )
                    return b""
                if args[:2] == ("env", "create"):
                    return b'{"environment":{"id":"env_new"},"uncopied_secrets":[]}'
                if args[:2] == ("values", "set"):
                    return b'{"version_id":"version_test"}'
                if args[:2] == ("values", "publish"):
                    return b'{"approval_request":{"id":"request_test"}}'
                self.fail("publication refusal must prevent later operations")

            fixture.cli = cli
            with self.assertRaisesRegex(fixture_module.Failure, "did not publish"):
                fixture.verify()


if __name__ == "__main__":
    unittest.main()
