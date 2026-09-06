FROM debian:trixie-slim@sha256:d7e12182ce18b85b93007c1dedf31f2d29e01ccf3182cc4017c709b6259bc132
RUN apt-get update && apt-get install -y --no-install-recommends systemd systemd-sysv dbus && rm -rf /var/lib/apt/lists/* && useradd --system --user-group hikyo
COPY hostupgrade.test /hostupgrade.test
COPY host-upgrade-helper /host-upgrade-helper
ENV container=docker
STOPSIGNAL SIGRTMIN+3
# Credential mounts must propagate between systemd's child mount namespaces.
# /run is the container's own tmpfs, never a host bind mount.
# https://github.com/systemd/systemd/issues/38103
CMD ["/bin/sh", "-c", "mount --make-rshared /run && exec /sbin/init"]
