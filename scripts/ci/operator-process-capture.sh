#!/bin/sh
# Executed in the kind NODE, outside the measured operator cgroup.
set -eu
cgroup=$1
container_id=$2
case "$cgroup" in
	/sys/fs/cgroup/*/cri-containerd-"$container_id".scope) ;;
	*) exit 1 ;;
esac
case "$cgroup" in *..*) exit 1 ;; esac
pids=$(cat "$cgroup/cgroup.procs")
# cgroup.procs lists process IDs, not the Go process's individual threads.
case "$pids" in ''|*[!0-9]*) exit 1 ;; esac
pid=$pids
start_time() {
	# The command name in /proc/PID/stat can contain spaces and parentheses.
	# Start time is field 20 after its final closing parenthesis (field 3).
	sed 's/.*) //' "/proc/$pid/stat" | awk '{print $20}'
}
printf 'container_id %s\ncgroup %s\npids_before %s\nstart_time_before %s\n' "$container_id" "$cgroup" "$pids" "$(start_time)"
printf 'executable %s\n' "$(readlink "/proc/$pid/exe")"
printf 'executable_sha256 %s\n' "$(sha256sum "/proc/$pid/exe" | awk '{print $1}')"
printf 'cmdline_base64 %s\n' "$(base64 "/proc/$pid/cmdline" | tr -d '\n')"
cat "/proc/$pid/status"
pids_after=$(cat "$cgroup/cgroup.procs")
[ "$pids_after" = "$pids" ] || exit 1
printf 'start_time_after %s\npids_after %s\n' "$(start_time)" "$pids_after"
