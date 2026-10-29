"""Verify an operator-controlled read-only LXC mount receipt, not remote attestation."""

import json
import os
from pathlib import Path
import socket
import stat
import time
import uuid


PROOF = Path("/run/hikyo-evidence-host/encryption.json")


def object_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate host receipt field")
        result[key] = value
    return result


def validate(value, *, guest, source, device, now, expected_luks_uuid, expected_filesystem_uuid):
    fields = {"version", "guest", "container_id", "node", "mount_source", "luks_uuid",
              "filesystem_uuid", "device_major", "device_minor", "verified_ns"}
    if not isinstance(value, dict) or set(value) != fields:
        raise ValueError("invalid host mount receipt")
    if (type(value["version"]) is not int or value["version"] != 1
            or value["guest"] != guest or value["mount_source"] != source
            or not source.startswith("/dev/mapper/")
            or type(value["container_id"]) is not int or value["container_id"] <= 0
            or not isinstance(value["node"], str) or not value["node"]
            or type(value["device_major"]) is not int or type(value["device_minor"]) is not int
            or (value["device_major"], value["device_minor"]) != device
            or type(value["verified_ns"]) is not int
            or not 0 <= now - value["verified_ns"] <= 300_000_000_000):
        raise ValueError("host mount receipt does not bind this guest/mount/window")
    for field in ("luks_uuid", "filesystem_uuid"):
        if not isinstance(value[field], str) or str(uuid.UUID(value[field])) != value[field]:
            raise ValueError("invalid host mount UUID")
    if value["luks_uuid"] != expected_luks_uuid or value["filesystem_uuid"] != expected_filesystem_uuid:
        raise ValueError("host mount UUID differs from approved storage binding")
    return value


def verify(base, source, expected_luks_uuid, expected_filesystem_uuid):
    # The host owns this metadata file. It is bound read-only into the guest;
    # the unprivileged guest receives neither block device nor unlock key.
    if PROOF.resolve() != PROOF or not os.statvfs(PROOF).f_flag & os.ST_RDONLY:
        raise ValueError("host receipt must be a fixed read-only bind mount")
    fd = os.open(PROOF, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid not in (0, 65534)
                or info.st_mode & 0o222 or not 0 < info.st_size <= 4096):
            raise ValueError("host receipt owner/type/mode/size refused")
        raw = os.read(fd, 4097)
        if len(raw) != info.st_size:
            raise ValueError("incomplete host mount receipt")
        value = json.loads(raw, object_pairs_hook=object_pairs)
    finally:
        os.close(fd)
    device = Path(base).stat().st_dev
    return validate(value, guest=socket.gethostname(), source=source,
                    device=(os.major(device), os.minor(device)), now=time.time_ns(),
                    expected_luks_uuid=expected_luks_uuid, expected_filesystem_uuid=expected_filesystem_uuid)
