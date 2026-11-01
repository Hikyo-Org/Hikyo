"""Trust-boundary and controller-policy checks, with real Helm rendering."""

import copy
import io
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import yaml

import render_gitops as gitops
from nightly_chart import prepare


COMMIT = "a" * 40
VERSION = "0.0.1-nightly.20261101.1.gaaaaaaaa"
DIGEST = "sha256:" + "b" * 64
IMAGE = {"repository": gitops.IMAGE, "digest": "sha256:" + "c" * 64}


class GitOpsTests(unittest.TestCase):
    def setUp(self):
        self.work = tempfile.TemporaryDirectory()
        self.addCleanup(self.work.cleanup)
        self.directory = Path(self.work.name)
        self.values = yaml.safe_load((gitops.ROOT / "chart/hikyo/examples/singleton-manual.yaml").read_text())
        self.chart = self.directory / "hikyo"
        prepare(gitops.ROOT / "chart/hikyo", self.chart, "Hikyo-Org/Hikyo", VERSION, COMMIT, IMAGE["digest"])
        subprocess.run(["helm", "package", str(self.chart), "--destination", str(self.directory)],
                       check=True, capture_output=True)
        self.package = next(self.directory.glob("*.tgz"))

    def rendered(self, values=None):
        path = self.directory / "values.yaml"
        path.write_text(yaml.safe_dump(values or self.values))
        return gitops.run("helm", "template", "hikyo", str(self.package),
                          "--namespace", "hikyo", "--values", str(path))

    def test_real_package_and_render(self):
        self.assertEqual(gitops.inspect_chart(self.package, COMMIT, VERSION), IMAGE)
        gitops.validate_values(self.values)
        gitops.check_rendered(self.rendered(), "hikyo", IMAGE, 1)
        stopped = copy.deepcopy(self.values)
        stopped["replicaCount"] = 0
        gitops.check_rendered(self.rendered(stopped), "hikyo", IMAGE, 0)

    def test_flux_oci_version_metadata_is_a_valid_kubernetes_label(self):
        path = self.chart / "Chart.yaml"
        chart = yaml.safe_load(path.read_text())
        chart["version"] += "+123456abcdef"
        path.write_text(yaml.safe_dump(chart))
        values = self.directory / "values.yaml"
        values.write_text(yaml.safe_dump(self.values))
        rendered = gitops.run("helm", "template", "hikyo", str(self.chart), "--values", str(values))
        for item in yaml.safe_load_all(rendered):
            if item:
                value = item["metadata"]["labels"]["helm.sh/chart"]
                self.assertLessEqual(len(value), 63)
                self.assertRegex(value, r"^[A-Za-z0-9](?:[-A-Za-z0-9_.]*[A-Za-z0-9])?$")
                self.assertIn("_123456abcdef", value)

    def test_refuse_different_source_version_profile_and_image(self):
        original = self.package.read_bytes()
        for member, key, value in [
            ("Chart.yaml", "version", "1.0.0"),
            ("Chart.yaml", "appVersion", "wrong"),
            ("Chart.yaml", "annotations", {}),
            ("values.yaml", "image", {"repository": "untrusted.example/server", "digest": IMAGE["digest"]}),
            ("values.yaml", "image", {"repository": gitops.IMAGE, "digest": "latest"}),
        ]:
            with self.subTest(member=member, key=key, value=value):
                self.package.write_bytes(original)
                files = gitops.package_files(self.package)
                document = yaml.safe_load(files["hikyo/" + member])
                document[key] = value
                files["hikyo/" + member] = yaml.safe_dump(document).encode()
                with tarfile.open(self.package, "w:gz") as archive:
                    for name, content in files.items():
                        info = tarfile.TarInfo(name)
                        info.size = len(content)
                        archive.addfile(info, io.BytesIO(content))
                with self.assertRaises(ValueError):
                    gitops.inspect_chart(self.package, COMMIT, VERSION)

    def test_refuse_unsafe_topology_and_authority(self):
        for path, value in [
            (("replicaCount",), 2), (("replicaCount",), True),
            (("ha", "enabled"), True), (("operator", "enabled"), True),
            (("rollout", "enabled"), True),
            (("upgrade", "unattended", "enabled"), True),
            (("upgrade", "stateExistingClaim"), "hikyo-upgrade-public"),
            (("rootKey", "existingSecret"), ""), (("tls", "existingSecret"), ""),
            (("database", "existingSecret"), ""),
            (("externalOrigin",), "http://hikyo.example.com"),
            (("network", "allowPlaintextOrigin"), True),
            (("releaseTrust", "admissionPolicy", "enabled"), True),
            (("image",), IMAGE),
            (("rootKey", "value"), "must-not-enter-Git"),
            (("rootKey", "existingSecret"), {"data": "must-not-enter-Git"}),
            (("network", "trustedProxyCIDRs"), ["not-a-CIDR"]),
            (("externalOrigin",), "https://user:password@hikyo.example.com"),
        ]:
            with self.subTest(path=path):
                values = copy.deepcopy(self.values)
                current = values
                for key in path[:-1]:
                    current = current[key]
                current[path[-1]] = value
                with self.assertRaises(ValueError):
                    gitops.validate_values(values)

    def test_refuse_rendered_escape_and_credentials(self):
        documents = list(yaml.safe_load_all(self.rendered()))
        server = next(item for item in documents if item["kind"] == "Deployment")
        for path, value in [
            (("spec", "strategy"), {"type": "RollingUpdate"}),
            (("spec", "template", "spec", "automountServiceAccountToken"), True),
            (("metadata", "namespace"), "other"),
        ]:
            with self.subTest(path=path):
                changed = copy.deepcopy(server)
                current = changed
                for key in path[:-1]:
                    current = current[key]
                current[path[-1]] = value
                with self.assertRaises(ValueError):
                    gitops.check_rendered(yaml.safe_dump(changed), "hikyo", IMAGE, 1)
        with self.assertRaises(ValueError):
            gitops.check_rendered(yaml.safe_dump_all(documents + [{"apiVersion": "v1", "kind": "Secret"}]),
                                  "hikyo", IMAGE, 1)

    def test_flux_pinning_scoped_identity_and_no_remediation(self):
        documents = gitops.manifests("flux", DIGEST, COMMIT, VERSION, IMAGE, self.values, "target", "flux-system")
        kinds = {item["kind"]: item for item in documents}
        self.assertEqual(set(kinds), {"ServiceAccount", "Role", "RoleBinding", "OCIRepository", "HelmRelease"})
        self.assertTrue(all(item["metadata"]["namespace"] == "target" for item in documents))
        self.assertEqual(kinds["RoleBinding"]["subjects"][0]["namespace"], "target")
        source = kinds["OCIRepository"]["spec"]
        self.assertEqual(source["url"], gitops.CHART)
        self.assertEqual(source["ref"], {"digest": DIGEST})
        self.assertEqual(source["verify"]["provider"], "cosign")
        matcher = source["verify"]["matchOIDCIdentity"]
        self.assertEqual(len(matcher), 1)
        self.assertEqual(matcher[0], {
            "issuer": r"^https://token\.actions\.githubusercontent\.com$",
            "subject": r"^https://github\.com/Hikyo-Org/Hikyo/\.github/workflows/nightly\.yml@refs/heads/main$",
        })
        release = kinds["HelmRelease"]["spec"]
        self.assertTrue(release["suspend"])
        self.assertEqual(release["targetNamespace"], "target")
        self.assertEqual(release["serviceAccountName"], "hikyo-deployer")
        self.assertEqual(release["chartRef"], {"kind": "OCIRepository", "name": "hikyo"})
        for action in ("install", "upgrade"):
            self.assertEqual(release[action]["strategy"], {"name": "RemediateOnFailure"})
            self.assertEqual(release[action]["remediation"], {"retries": 0, "remediateLastFailure": False})
            self.assertEqual(release[action]["crds"], "Skip")
            self.assertTrue(release[action]["disableTakeOwnership"])
        self.assertFalse(release["upgrade"]["cleanupOnFail"])
        self.assertEqual(release["driftDetection"]["mode"], "disabled")
        self.assertEqual(release["values"], self.values)

    def test_argo_digest_manual_sync_no_delete_finalizer(self):
        project, app = gitops.manifests("argo-cd", DIGEST, COMMIT, VERSION, IMAGE, self.values, "target", "gitops")
        self.assertEqual(project["metadata"]["namespace"], "gitops")
        self.assertEqual(project["spec"]["destinations"][0]["namespace"], "target")
        self.assertEqual(project["spec"]["clusterResourceWhitelist"], [])
        self.assertEqual(project["spec"]["sourceRepos"], [gitops.CHART])
        self.assertNotIn("finalizers", app["metadata"])
        spec = app["spec"]
        self.assertEqual(spec["source"]["targetRevision"], DIGEST)
        self.assertEqual(spec["source"]["path"], ".")
        self.assertEqual(spec["source"]["helm"], {"releaseName": "hikyo", "skipCrds": True, "valuesObject": self.values})
        self.assertEqual(spec["syncPolicy"]["automated"], {
            "enabled": False, "prune": False, "selfHeal": False, "allowEmpty": False})
        self.assertEqual(spec["syncPolicy"]["retry"], {"limit": 0})
        self.assertIn("Prune=false", spec["syncPolicy"]["syncOptions"])
        self.assertIn("FailOnSharedResource=true", spec["syncPolicy"]["syncOptions"])

    def test_cli_verifies_chart_then_image_before_output_and_never_applies(self):
        values = self.directory / "input.yaml"
        values.write_text(yaml.safe_dump(self.values))
        output = self.directory / "generated.yaml"
        calls = []
        real_run = gitops.run

        def boundary(*args):
            calls.append(args)
            if args[:2] == ("cosign", "verify"):
                return "[]"
            if args[:2] == ("helm", "pull"):
                (Path(args[-1]) / self.package.name).write_bytes(self.package.read_bytes())
                return ""
            return real_run(*args)

        argv = ["render_gitops.py", "--provider", "flux", "--chart-digest", DIGEST,
                "--source-commit", COMMIT, "--chart-version", VERSION,
                "--values", str(values), "--output", str(output)]
        with patch("sys.argv", argv), patch.object(gitops, "run", side_effect=boundary):
            gitops.main()
        self.assertTrue(output.is_file())
        self.assertEqual([args[0:2] for args in calls], [
            ("cosign", "verify"), ("helm", "pull"), ("cosign", "verify"), ("helm", "template")])
        self.assertEqual(calls[0][2], gitops.CHART.removeprefix("oci://") + "@" + DIGEST)
        self.assertEqual(calls[2][2], gitops.IMAGE + "@" + IMAGE["digest"])
        for args in (calls[0], calls[2]):
            self.assertEqual(args[-2:], ("--certificate-github-workflow-sha", COMMIT))
        before = output.read_bytes()
        with patch("sys.argv", argv), self.assertRaises(SystemExit), patch.object(gitops, "run") as blocked:
            gitops.main()
        blocked.assert_not_called()
        self.assertEqual(output.read_bytes(), before)
        output.unlink()
        with patch("sys.argv", argv), patch.object(gitops, "run", side_effect=subprocess.CalledProcessError(1, "cosign")):
            with self.assertRaises(subprocess.CalledProcessError):
                gitops.main()
        self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
