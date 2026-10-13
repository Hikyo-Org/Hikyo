"""Fresh target lifecycle, interrupted creates and native ownership regressions."""

from __future__ import annotations

import copy
import hashlib
import tempfile
import time
import unittest
import uuid
from pathlib import Path
from unittest.mock import patch

import hikyo_nightly_lifecycle as lifecycle

RUN = "f5c301bd-b75e-4343-a44c-1b7e827fbd1a"
IMAGE = "ghcr.io/hikyo-org/hikyo@sha256:" + "1" * 64
VERSION = "0.0.1-nightly.20261003.66.g5c3f969f"
COMMIT = "5c3f969f692268e5c0bc7f5b9acc860f3bff0f48"


def bundle(root: Path) -> tuple[Path, str]:
    directory = root / "bundle"
    directory.mkdir()
    (directory / "index.json").write_text('{"synthetic":true}\n')
    (directory / "proof").mkdir()
    (directory / "proof/signature").write_bytes(b"public-signature")
    entries = [
        {
            "path": path.relative_to(directory).as_posix(),
            "size": path.stat().st_size,
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
        }
        for path in sorted(
            directory.rglob("*"),
            key=lambda item: item.relative_to(directory).as_posix(),
        )
        if path.is_file()
    ]
    return directory, hashlib.sha256(lifecycle._canonical(entries)).hexdigest()


def template() -> dict[str, object]:
    return lifecycle._json(
        Path(__file__).with_name("hikyo-nightly-deployment.json").read_bytes()
    )


class Native:
    """Native objects with controller ownership and UID-enforced cascade deletes."""

    def __init__(self) -> None:
        self.objects: dict[tuple[str, str], dict[str, object]] = {}
        self.commands: list[tuple[tuple[str, ...], bytes | None, int]] = []
        self.archive_digest = ""
        self.fail_create_response = False
        self.fail_transfer_digest = False
        self.hold_pv = False
        self.fail_pvc_delete = False

    def __call__(
        self, arguments: tuple[str, ...], payload: bytes | None, timeout: int
    ) -> bytes:
        self.commands.append((arguments, payload, timeout))
        if arguments[0] == "get":
            kinds = {
                "deployments": "Deployment",
                "pods": "Pod",
                "replicasets": "ReplicaSet",
                "persistentvolumeclaims": "PersistentVolumeClaim",
            }
            if arguments[1] in kinds:
                return lifecycle._canonical(
                    {
                        "items": [
                            item
                            for (kind, _), item in self.objects.items()
                            if kind == kinds[arguments[1]]
                        ]
                    }
                )
            item = self.objects.get((arguments[1], arguments[2]))
            return b"" if item is None else lifecycle._canonical(item)
        if arguments[0] == "create":
            if payload is None:
                raise AssertionError("create body required")
            item = lifecycle._json(payload)
            kind, meta = (
                lifecycle._text(item, "kind"),
                lifecycle._object(item["metadata"]),
            )
            name = lifecycle._text(meta, "name")
            if (kind, name) in self.objects:
                raise lifecycle.LifecycleError("native_already_exists")
            meta["uid"], meta["resourceVersion"], meta["generation"] = (
                str(uuid.uuid4()),
                "1",
                1,
            )
            item["metadata"] = meta
            self.objects[(kind, name)] = item
            if kind == "Pod":
                item["status"] = {"phase": "Running"}
                pvc = self.objects[("PersistentVolumeClaim", "hikyo-lab-data")]
                pvc_meta = lifecycle._object(pvc["metadata"])
                name_pv = "pvc-" + lifecycle._text(pvc_meta, "uid")
                lifecycle._object(pvc["spec"])
                pvc_spec = lifecycle._object(pvc["spec"])
                pvc_spec["volumeName"] = name_pv
                pvc["spec"] = pvc_spec
                self.objects[("PersistentVolume", name_pv)] = {
                    "kind": "PersistentVolume",
                    "metadata": {"name": name_pv, "uid": str(uuid.uuid4())},
                    "spec": {
                        "storageClassName": "local-path",
                        "persistentVolumeReclaimPolicy": "Delete",
                        "claimRef": {
                            "namespace": "hikyo-lab",
                            "name": "hikyo-lab-data",
                            "uid": pvc_meta["uid"],
                        },
                    },
                }
            if kind == "Deployment":
                item["status"] = {"availableReplicas": 1, "observedGeneration": 1}
                pod_meta = copy.deepcopy(
                    lifecycle._object(
                        lifecycle._object(lifecycle._object(item["spec"])["template"])[
                            "metadata"
                        ]
                    )
                )
                rs_uid = str(uuid.uuid4())
                for (
                    child_kind,
                    child_name,
                    parent_kind,
                    parent_name,
                    parent_uid,
                    child_uid,
                ) in (
                    (
                        "ReplicaSet",
                        "hikyo-abc123",
                        "Deployment",
                        "hikyo",
                        meta["uid"],
                        rs_uid,
                    ),
                    (
                        "Pod",
                        "hikyo-abc123-xyz12",
                        "ReplicaSet",
                        "hikyo-abc123",
                        rs_uid,
                        str(uuid.uuid4()),
                    ),
                ):
                    child_meta = copy.deepcopy(pod_meta)
                    child_meta.update(
                        {
                            "name": child_name,
                            "namespace": "hikyo-lab",
                            "uid": child_uid,
                            "ownerReferences": [
                                {
                                    "kind": parent_kind,
                                    "name": parent_name,
                                    "uid": parent_uid,
                                    "controller": True,
                                }
                            ],
                        }
                    )
                    self.objects[(child_kind, child_name)] = {
                        "kind": child_kind,
                        "metadata": child_meta,
                    }
            if self.fail_create_response:
                self.fail_create_response = False
                raise lifecycle.LifecycleError("transport_lost_after_create")
            return lifecycle._canonical(item)
        if arguments[0] == "exec":
            if "cat >" in " ".join(arguments):
                if payload is None:
                    raise AssertionError("archive required")
                self.archive_digest = hashlib.sha256(payload).hexdigest()
            if "sha256sum" in arguments:
                digest = "0" * 64 if self.fail_transfer_digest else self.archive_digest
                return (digest + "  /var/lib/hikyo/bundle.tar\n").encode()
            return b""
        if arguments[0] == "delete":
            if payload is None:
                raise AssertionError("delete options required")
            route = arguments[2].split("/")
            kind = {
                "deployments": "Deployment",
                "pods": "Pod",
                "persistentvolumeclaims": "PersistentVolumeClaim",
            }[route[-2]]
            key = kind, route[-1]
            item = self.objects[key]
            preconditions = lifecycle._object(lifecycle._json(payload)["preconditions"])
            meta = lifecycle._object(item["metadata"])
            if preconditions != {
                "uid": meta["uid"],
                "resourceVersion": meta["resourceVersion"],
            }:
                raise lifecycle.LifecycleError("native_uid_precondition_failed")
            if kind == "PersistentVolumeClaim" and self.fail_pvc_delete:
                raise lifecycle.LifecycleError("temporary_pvc_delete_failure")
            del self.objects[key]
            if kind == "Deployment":
                for child_key in list(self.objects):
                    if child_key[0] == "ReplicaSet" or child_key[1].startswith(
                        "hikyo-abc123-"
                    ):
                        del self.objects[child_key]
            if kind == "PersistentVolumeClaim" and not self.hold_pv:
                volume = lifecycle._object(item["spec"]).get("volumeName")
                if isinstance(volume, str):
                    self.objects.pop(("PersistentVolume", volume), None)
            return b"{}"
        raise AssertionError("unsupported native command")


class LifecycleTests(unittest.TestCase):
    def test_full_lifecycle_fresh_pvc_helper_then_server_then_reclaim(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            directory, digest = bundle(path)
            native = Native()
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            prepared = target.prepare(directory, digest)
            self.assertTrue(prepared["preparer_removed"])
            self.assertNotIn(("Pod", target.preparer), native.objects)
            target.start(
                template(), IMAGE, expected_version=VERSION, source_commit=COMMIT
            )
            result = target.cleanup()
            self.assertTrue(result["complete"])
            self.assertIs(result["verified"], True)
            self.assertEqual(len(lifecycle._object(result["owned_child_uids"])), 2)
            self.assertEqual(native.objects, {})
            deletes = [
                arguments
                for arguments, _, _ in native.commands
                if arguments[0] == "delete"
            ]
            self.assertTrue(
                all("/namespaces/hikyo-lab/" in arguments[2] for arguments in deletes)
            )
            self.assertFalse(
                any("persistentvolumes/" in arguments[2] for arguments in deletes)
            )
            self.assertLess(
                next(
                    i
                    for i, (args, _, _) in enumerate(native.commands)
                    if args[0] == "delete"
                ),
                next(
                    i
                    for i, (args, payload, _) in enumerate(native.commands)
                    if args[0] == "create"
                    and payload is not None
                    and b'"kind":"Deployment"' in payload
                ),
            )
            self.assertEqual(target.journal.stat().st_mode & 0o777, 0o600)

    def test_verified_bundle_change_and_symlinks_rejected_before_cluster_calls(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            directory, digest = bundle(path)
            native = Native()
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            (directory / "index.json").write_bytes(b"changed")
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "verified_bundle_tree_changed"
            ):
                target.prepare(directory, digest)
            self.assertEqual(native.commands, [])
            (directory / "link").symlink_to(directory / "index.json")
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "bundle_symlink_forbidden"
            ):
                lifecycle.bundle_archive(directory, digest)

    def test_preparer_reserves_future_server_resources_without_pinning_or_overlap(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            directory, digest = bundle(path)
            native = Native()
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            target.prepare(directory, digest)
            prep_payload = next(
                payload
                for args, payload, _ in native.commands
                if args[0] == "create"
                and payload is not None
                and b'"kind":"Pod"' in payload
            )
            prep = lifecycle._json(prep_payload)
            prep_spec = lifecycle._object(prep["spec"])
            prep_containers = prep_spec["containers"]
            if not isinstance(prep_containers, list) or len(prep_containers) != 1:
                self.fail("one bounded preparation container required")
            resources = lifecycle._object(
                lifecycle._object(prep_containers[0])["resources"]
            )
            future = template()
            future_pod = lifecycle._object(
                lifecycle._object(lifecycle._object(future["spec"])["template"])["spec"]
            )
            containers, initializers = (
                future_pod["containers"],
                future_pod["initContainers"],
            )
            if not isinstance(containers, list) or not isinstance(initializers, list):
                self.fail("future server resource declarations required")
            requests = lifecycle._object(resources["requests"])
            limits = lifecycle._object(resources["limits"])
            for name, suffix in (("cpu", "m"), ("memory", "Mi")):
                native_requests = [
                    lifecycle._object(
                        lifecycle._object(lifecycle._object(row)["resources"])[
                            "requests"
                        ]
                    )[name]
                    for row in containers + initializers
                ]
                if not all(
                    isinstance(value, str) and value.endswith(suffix)
                    for value in native_requests
                ):
                    self.fail("bounded matching units required")
                maximum = max(
                    int(str(value).removesuffix(suffix)) for value in native_requests
                )
                self.assertEqual(requests[name], str(maximum) + suffix)
            server_resources = lifecycle._object(
                lifecycle._object(containers[0])["resources"]
            )
            self.assertEqual(limits, server_resources["limits"])
            self.assertEqual(requests, {"cpu": "100m", "memory": "384Mi"})
            self.assertEqual(limits, {"cpu": "500m", "memory": "384Mi"})
            self.assertEqual(prep_spec["nodeSelector"], future_pod["nodeSelector"])
            self.assertNotIn("nodeName", prep_spec)
            self.assertNotIn(("Pod", target.preparer), native.objects)
            self.assertNotIn(("Deployment", "hikyo"), native.objects)
            target.start(future, IMAGE, expected_version=VERSION, source_commit=COMMIT)
            self.assertTrue(target.cleanup()["verified"])

    def test_archive_deterministic_and_has_no_symlink_or_absolute_members(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            directory, digest = bundle(Path(root))
            first, sha = lifecycle.bundle_archive(directory, digest)
            self.assertEqual(lifecycle.bundle_archive(directory, digest), (first, sha))
            import io
            import tarfile

            with tarfile.open(fileobj=io.BytesIO(first)) as archive:
                self.assertTrue(
                    all(
                        item.isfile()
                        and not item.name.startswith("/")
                        and item.uid == 65532
                        for item in archive
                    )
                )

    def test_existing_target_or_orphan_controller_not_adopted(self) -> None:
        for kind in ("Deployment", "Pod", "PersistentVolumeClaim", "ReplicaSet"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as root:
                path = Path(root)
                directory, digest = bundle(path)
                native = Native()
                native.objects[(kind, "foreign")] = {
                    "kind": kind,
                    "metadata": {"name": "foreign"},
                }
                target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
                with self.assertRaisesRegex(
                    lifecycle.LifecycleError, "lab_target_not_idle"
                ):
                    target.prepare(directory, digest)
                self.assertFalse(
                    any(
                        args[0] in ("create", "delete")
                        for args, _, _ in native.commands
                    )
                )

    def test_interrupted_create_recovers_from_durable_local_intent(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            directory, digest = bundle(path)
            native = Native()
            native.fail_create_response = True
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "transport_lost_after_create"
            ):
                target.prepare(directory, digest)
            recovered = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            self.assertTrue(recovered.cleanup()["complete"])
            self.assertEqual(native.objects, {})

    def test_uid_replacement_refused_before_any_cleanup_deletion(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            directory, digest = bundle(path)
            native = Native()
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            target.prepare(directory, digest)
            lifecycle._object(
                native.objects[("PersistentVolumeClaim", "hikyo-lab-data")]["metadata"]
            )
            meta = lifecycle._object(
                native.objects[("PersistentVolumeClaim", "hikyo-lab-data")]["metadata"]
            )
            meta["uid"] = str(uuid.uuid4())
            native.objects[("PersistentVolumeClaim", "hikyo-lab-data")]["metadata"] = (
                meta
            )
            before = len(native.commands)
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "native_resource_uid_changed"
            ):
                target.cleanup()
            self.assertFalse(
                any(args[0] == "delete" for args, _, _ in native.commands[before:])
            )
            self.assertFalse(
                lifecycle._json((path / "state/cleanup.json").read_bytes())["complete"]
            )
            self.assertNotIn(
                "verified", lifecycle._json((path / "state/cleanup.json").read_bytes())
            )

    def test_response_lost_after_deployment_create_recovers_controller_children(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            directory, digest = bundle(path)
            native = Native()
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            target.prepare(directory, digest)
            native.fail_create_response = True
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "transport_lost_after_create"
            ):
                target.start(
                    template(), IMAGE, expected_version=VERSION, source_commit=COMMIT
                )
            recovered = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            self.assertTrue(recovered.cleanup()["complete"])
            self.assertEqual(native.objects, {})

    def test_expired_run_can_cleanup_but_cannot_start(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            directory, digest = bundle(path)
            native = Native()
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            target.prepare(directory, digest)
            with patch.object(lifecycle.time, "time", return_value=time.time() + 7201):
                with self.assertRaisesRegex(
                    lifecycle.LifecycleError, "prepared_run_required"
                ):
                    target.start(
                        template(),
                        IMAGE,
                        expected_version=VERSION,
                        source_commit=COMMIT,
                    )
                self.assertTrue(target.cleanup()["complete"])

    def test_unsafe_pod_template_rejected_without_server_creation(self) -> None:
        for field, value in (
            ("hostNetwork", True),
            ("automountServiceAccountToken", True),
            ("nodeName", "kyoto"),
        ):
            with self.subTest(field=field), tempfile.TemporaryDirectory() as root:
                path = Path(root)
                directory, digest = bundle(path)
                native = Native()
                target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
                target.prepare(directory, digest)
                manifest = template()
                spec = lifecycle._object(manifest["spec"])
                pod = lifecycle._object(spec["template"])
                podspec = lifecycle._object(pod["spec"])
                podspec[field] = value
                pod["spec"], spec["template"], manifest["spec"] = podspec, pod, spec
                with self.assertRaises(lifecycle.LifecycleError):
                    target.start(
                        manifest, IMAGE, expected_version=VERSION, source_commit=COMMIT
                    )
                self.assertNotIn(("Deployment", "hikyo"), native.objects)
                self.assertTrue(target.cleanup()["complete"])

    def test_failed_transfer_cannot_start_but_can_cleanup_owned_helper(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            directory, digest = bundle(path)
            native = Native()
            native.fail_transfer_digest = True
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "native_bundle_transfer_digest_mismatch"
            ):
                target.prepare(directory, digest)
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "prepared_run_required"
            ):
                target.start(
                    template(), IMAGE, expected_version=VERSION, source_commit=COMMIT
                )
            self.assertTrue(target.cleanup()["complete"])

    def test_foreign_child_owner_refuses_cleanup_before_deployment_delete(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            directory, digest = bundle(path)
            native = Native()
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            target.prepare(directory, digest)
            target.start(
                template(), IMAGE, expected_version=VERSION, source_commit=COMMIT
            )
            child = native.objects[("ReplicaSet", "hikyo-abc123")]
            meta = lifecycle._object(child["metadata"])
            meta["ownerReferences"] = [
                {
                    "kind": "Deployment",
                    "name": "production",
                    "uid": str(uuid.uuid4()),
                    "controller": True,
                }
            ]
            child["metadata"] = meta
            before = len(native.commands)
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "foreign_lab_child_owner"
            ):
                target.cleanup()
            self.assertFalse(
                any(args[0] == "delete" for args, _, _ in native.commands[before:])
            )

    def test_foreign_volume_binding_prevents_any_cleanup_deletion(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            directory, digest = bundle(path)
            native = Native()
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            target.prepare(directory, digest)
            for (kind, _), item in native.objects.items():
                if kind == "PersistentVolume":
                    spec = lifecycle._object(item["spec"])
                    spec["claimRef"] = {
                        "namespace": "production",
                        "name": "hikyo-lab-data",
                        "uid": str(uuid.uuid4()),
                    }
                    item["spec"] = spec
            before = len(native.commands)
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "bound_volume_ownership_mismatch"
            ):
                target.cleanup()
            self.assertFalse(
                any(args[0] == "delete" for args, _, _ in native.commands[before:])
            )

    def test_reclaim_recovery_retains_pv_uid_after_pvc_disappeared(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            directory, digest = bundle(path)
            native = Native()
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            target.prepare(directory, digest)
            native.hold_pv = True
            with (
                patch.object(
                    target,
                    "_wait",
                    side_effect=lambda check, seconds=180: (
                        check()
                        or (_ for _ in ()).throw(
                            lifecycle.LifecycleError("test_wait_failed")
                        )
                    ),
                ),
                self.assertRaisesRegex(lifecycle.LifecycleError, "test_wait_failed"),
            ):
                target.cleanup()
            self.assertNotIn(
                ("PersistentVolumeClaim", "hikyo-lab-data"), native.objects
            )
            recovered = lifecycle.EphemeralHikyo(RUN, path / "state", command=native)
            self.assertIsNotNone(recovered.bound_volume)
            pv = next(key for key in native.objects if key[0] == "PersistentVolume")
            meta = lifecycle._object(native.objects[pv]["metadata"])
            meta["uid"] = str(uuid.uuid4())
            native.objects[pv]["metadata"] = meta
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "bound_volume_uid_changed"
            ):
                recovered.cleanup()
            native.objects.clear()
            self.assertTrue(recovered.cleanup()["complete"])

    def test_candidate_images_must_be_exact_official_digest(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            target = lifecycle.EphemeralHikyo(
                RUN, Path(root) / "state", command=Native()
            )
            for image in (
                "ghcr.io/hikyo-org/hikyo:nightly",
                "production/hikyo@sha256:" + "1" * 64,
            ):
                with self.assertRaisesRegex(
                    lifecycle.LifecycleError, "invalid_verified_candidate_identity"
                ):
                    target.start(
                        template(),
                        image,
                        expected_version=VERSION,
                        source_commit=COMMIT,
                    )

    def test_invalid_run_state_lease_and_allowlist_fail_closed(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)
            with self.assertRaisesRegex(lifecycle.LifecycleError, "invalid_run_id"):
                lifecycle.EphemeralHikyo("foreign", path / "state")
            target = lifecycle.EphemeralHikyo(RUN, path / "state", command=Native())
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "resource_outside_fixed_lab_allowlist"
            ):
                target._create({"kind": "Secret", "metadata": {"name": "production"}})
            target.expires_at = int(time.time()) + 30
            target._save()
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "lifecycle_journal_owner_mismatch"
            ):
                lifecycle.EphemeralHikyo(str(uuid.uuid4()), path / "state")
            link = path / "linked"
            link.symlink_to(path / "state")
            with self.assertRaisesRegex(
                lifecycle.LifecycleError, "unsafe_lifecycle_state"
            ):
                lifecycle.EphemeralHikyo(RUN, link)


if __name__ == "__main__":
    unittest.main()
