#!/usr/bin/env python3
"""Install only on a newly approved dedicated Debian guest. See deployment packet."""

import argparse
import json
import os
from pathlib import Path
import pwd
import re
import shutil
import subprocess


BASE = Path("/srv/hikyo-evidence")
CODE = Path("/opt/hikyo-evidence")
CONFIG = Path("/etc/hikyo-evidence")
NAMES = {"pg-tenant", "pg-instance", "sqlite-tenant", "sqlite-instance"}


def run(*arguments):
    subprocess.run(arguments, check=True, stdout=subprocess.DEVNULL)


def write(path, body, mode=0o644):
    with path.open("x") as target:
        target.write(body)
    path.chmod(mode)


def install(manifest, keys, rehearsal):
    if os.geteuid() != 0:
        raise ValueError("root installation required on the approved guest")
    streams = json.loads(Path(manifest).read_text())
    if set(streams) != NAMES:
        raise ValueError("all four fixed streams required")
    # Check encryption before creating anything. The rehearsal exception is
    # limited to explicitly synthetic sources inside a disposable container.
    if rehearsal:
        if not Path("/.dockerenv").exists() or any(
                not value["source_id"].startswith("local-1037-") for value in streams.values()):
            raise ValueError("rehearsal requires a disposable synthetic container")
    else:
        mounted = subprocess.check_output(["findmnt", "-n", "-o", "SOURCE", "--mountpoint", str(BASE)], text=True).strip()
        if not mounted.startswith("/dev/mapper/"):
            raise ValueError("dedicated unlocked encrypted evidence mount required")
        run("cryptsetup", "status", mounted)
    if CODE.exists() or CONFIG.exists() or any(BASE.iterdir()):
        raise ValueError("fresh installation only; reconcile partial/unknown state before retry")
    for name, value in streams.items():
        if set(value) != {"source_id", "trail", "scope", "database", "max_bytes", "max_rows"}:
            raise ValueError("invalid fixed receiver config")
        if value["trail"] != name.split("-")[1] or value["max_bytes"] != 16777216 or value["max_rows"] != 20000:
            raise ValueError("stream/limit mismatch")
        key = (Path(keys) / (name + ".pub")).read_text().strip()
        if not re.fullmatch(r"ssh-ed25519 [A-Za-z0-9+/]+={0,2}(?: [^\r\n]*)?", key):
            raise ValueError("one Ed25519 submit public key required per stream")
        for prefix in ("hk-s-", "hk-e-", "hk-p-"):
            try:
                pwd.getpwnam(prefix + name)
            except KeyError:
                continue
            raise ValueError("identity already exists; reconcile before retry")
    for user in ("hk-reader", "hk-monitor"):
        try:
            pwd.getpwnam(user)
        except KeyError:
            continue
        raise ValueError("identity already exists; reconcile before retry")
    for command in ("python3", "sshd", "sudo", "visudo", "runuser", "setfacl", "timeout", "ssh-keygen", "useradd"):
        if shutil.which(command) is None:
            raise ValueError("missing prerequisite: " + command)
    CODE.mkdir(mode=0o755)
    CONFIG.mkdir(mode=0o755)
    (CONFIG / "keys").mkdir(mode=0o755)
    for directory in (CODE, CONFIG, CONFIG / "keys"):
        directory.chmod(0o755)
    for script in ("receiver.py", "monitor.py", "health.py", "poll-once.py"):
        shutil.copyfile(Path(__file__).with_name(script), CODE / script)
        (CODE / script).chmod(0o644)
    for user in ("hk-reader", "hk-monitor"):
        run("useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", user)
    BASE.chmod(0o711)
    monitor_streams = {}
    for name in sorted(NAMES):
        submit, evidence, poller = (prefix + name for prefix in ("hk-s-", "hk-e-", "hk-p-"))
        for user in (submit, evidence, poller):
            # Submit requires a real login shell for sshd's forced command.
            # Its password is unusable; the isolated sshd admits publickey only.
            run("useradd", "--system", "--no-create-home", "--shell", "/bin/sh" if user == submit else "/usr/sbin/nologin", user)
        # An unusable, unlocked password field allows publickey with UsePAM=no.
        # No password authentication is permitted by this dedicated sshd.
        run("usermod", "--password", "*", submit)
        directory = BASE / name
        directory.mkdir(mode=0o700)
        shutil.chown(directory, user=evidence, group=evidence)
        cfg = dict(streams[name], database=str(directory / "retained.sqlite"))
        config = CONFIG / (name + ".json")
        write(config, json.dumps(cfg, sort_keys=True) + "\n")
        run("runuser", "-u", evidence, "--", "python3", "-I", "-B", str(CODE / "receiver.py"), "init", str(config))
        run("setfacl", "-m", "u:hk-reader:rx,u:hk-monitor:rx", str(directory))
        run("setfacl", "-m", "u:hk-reader:r,u:hk-monitor:r", str(directory / "retained.sqlite"))
        key = (Path(keys) / (name + ".pub")).read_text().strip()
        receive = f"/usr/bin/timeout --kill-after=5s 180s /usr/bin/python3 -I -B {CODE}/receiver.py receive {config}"
        wrapper = CODE / ("submit-" + name)
        write(wrapper, '#!/bin/sh\nset -eu\n[ -z "${SSH_ORIGINAL_COMMAND:-}" ] || exit 64\nexec /usr/bin/sudo -n -u ' + evidence + ' -- ' + receive + '\n', 0o755)
        write(CONFIG / "keys" / submit, f'restrict,command="{wrapper}" {key}\n')
        write(Path("/etc/sudoers.d") / ("hikyo-" + name), f"{submit} ALL=({evidence}) NOPASSWD: {receive}\n", 0o440)
        spool = BASE / ("spool-" + name)
        spool.mkdir(mode=0o700)
        shutil.chown(spool, user=poller, group=poller)
        # A fixed precreated status inode gives the monitor read-only access.
        write(spool / "poll-status.json", '{"attempt_ns":0,"outcome":"NOTRUN"}\n', 0o600)
        shutil.chown(spool / "poll-status.json", user=poller, group=poller)
        run("setfacl", "-m", "u:hk-monitor:rx", str(spool))
        run("setfacl", "-m", "u:hk-monitor:r", str(spool / "poll-status.json"))
        run("setfacl", "-d", "-m", "u:hk-monitor:r", str(spool))
        monitor_streams[name] = {"config": str(config), "poll_status": str(spool / "poll-status.json")}
    state = BASE / "monitor"
    state.mkdir(mode=0o700)
    shutil.chown(state, user="hk-monitor", group="hk-monitor")
    write(CONFIG / "monitor.json", json.dumps({"streams": monitor_streams, "max_age": 900, "min_free_bytes": 2147483648}, sort_keys=True) + "\n")
    run("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(CONFIG / "ssh_host_ed25519_key"))
    write(CONFIG / "sshd_config", f"""Port 2222
ListenAddress 127.0.0.1
HostKey {CONFIG}/ssh_host_ed25519_key
PidFile /run/hikyo-evidence-sshd.pid
AuthorizedKeysFile {CONFIG}/keys/%u
AllowUsers {' '.join('hk-s-' + name for name in sorted(NAMES))}
AuthenticationMethods publickey
PasswordAuthentication no
KbdInteractiveAuthentication no
UsePAM no
PermitRootLogin no
PermitTTY no
PermitUserRC no
DisableForwarding yes
MaxSessions 1
MaxStartups 2:30:4
LoginGraceTime 15
ClientAliveInterval 15
ClientAliveCountMax 2
StrictModes yes
""")
    run("visudo", "-c")
    Path("/run/sshd").mkdir(exist_ok=True)
    run("sshd", "-t", "-f", str(CONFIG / "sshd_config"))
    for unit in Path(__file__).with_name("systemd").iterdir():
        destination = Path("/etc/systemd/system") / unit.name
        if destination.exists():
            raise ValueError("existing systemd unit requires reconciliation")
        shutil.copyfile(unit, destination)
        destination.chmod(0o644)
    print("installed; services are NOT started, collection is NOT authorized")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest")
    parser.add_argument("keys")
    parser.add_argument("--rehearsal", action="store_true")
    args = parser.parse_args()
    os.umask(0o077)
    install(args.manifest, args.keys, args.rehearsal)


if __name__ == "__main__":
    main()
