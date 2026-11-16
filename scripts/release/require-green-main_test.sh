#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-green-main.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
commit=176e6e67379d0675e6211f0491dc965cee4f1c5c
# The API proof itself lives in the stdlib verifier's adversarial tests. This
# fixture checks the portable wrapper's exact inputs and error propagation.
cat >"$fixture_dir/go" <<'GO'
#!/bin/sh
set -eu
[ "$#" -eq 16 ]
[ "$1" = -C ] && [ "$2" = "$EXPECTED_ROOT" ]
[ "$3" = run ] && [ "$4" = -p ] && [ "$5" = 2 ]
[ "$6" = ./scripts/ci/source-proof ]
[ "$7" = --repository ] && [ "$8" = Hikyo-Org/hikyo ]
[ "$9" = --mode ] && [ "${10}" = main ]
[ "${11}" = --commit ] && [ "${12}" = "$EXPECTED_COMMIT" ]
[ "${13}" = --authority-commit ] && [ "${14}" = "$EXPECTED_COMMIT" ]
[ "${15}" = --failure-class ]
printf 'called\n' >>"$CALLS"
# Status 1 is the verifier's definite "no" and says so through the class file; status 4 a failed
# read (it also writes its class). Any other status is a crash or a build failure that never
# reached a verdict, so the wrapper's own default class stays in place.
[ "$PROOF_STATUS" != 1 ] || printf 'not-green\n' >"${16}"
[ "$PROOF_STATUS" != 4 ] || printf 'lookup\n' >"${16}"
[ "$PROOF_STATUS" = 0 ] || exit 1
exit 0
GO
chmod +x "$fixture_dir/go"
export EXPECTED_ROOT="$repo_root" EXPECTED_COMMIT="$commit"
export CALLS="$fixture_dir/calls" PROOF_STATUS=0
GO_BIN="$fixture_dir/go" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >/dev/null
[ "$(wc -l <"$CALLS" | tr -d ' ')" = 1 ]
PROOF_STATUS=1
export PROOF_STATUS
if GO_BIN="$fixture_dir/go" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >"$fixture_dir/out" 2>&1; then
 printf 'green main fixture: a definite not-green verdict was ignored\n' >&2
 exit 1
fi
grep -F 'needs complete authenticated source proof' "$fixture_dir/out" >/dev/null
# A verifier that never reached a verdict (a build failure, a crash) is "could not look", not "not green".
for status in 2 17; do
 PROOF_STATUS=$status
 export PROOF_STATUS
 rc=0
 GO_BIN="$fixture_dir/go" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >"$fixture_dir/out" 2>&1 || rc=$?
 [ "$rc" = 4 ] || { printf 'green main fixture: verifier status %s without a verdict exited %s, want 4\n' "$status" "$rc" >&2; exit 1; }
 grep -F 'nothing is decided' "$fixture_dir/out" >/dev/null
done
# A failed GitHub read is its own exit code, so a caller never reads "could not look" as "not green".
PROOF_STATUS=4
export PROOF_STATUS
rc=0
GO_BIN="$fixture_dir/go" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >"$fixture_dir/out" 2>&1 || rc=$?
[ "$rc" = 4 ] || { printf 'green main fixture: a failed read exited %s, want 4\n' "$rc" >&2; exit 1; }
PROOF_STATUS=1
export PROOF_STATUS
rc=0
GO_BIN="$fixture_dir/go" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >"$fixture_dir/out" 2>&1 || rc=$?
[ "$rc" = 1 ] || { printf 'green main fixture: a missing proof exited %s, want 1\n' "$rc" >&2; exit 1; }
before=$(wc -l <"$CALLS" | tr -d ' ')
# Setup failures are "could not look" (exit 4), never the shell's own status 1, which reads as "not green".
PROOF_STATUS=0
export PROOF_STATUS
rc=0
TMPDIR="$fixture_dir/missing" GO_BIN="$fixture_dir/go" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >"$fixture_dir/out" 2>&1 || rc=$?
[ "$rc" = 4 ] || { cat "$fixture_dir/out" >&2; printf 'green main fixture: a class file that cannot be created exited %s, want 4\n' "$rc" >&2; exit 1; }
mkdir "$fixture_dir/fakebin"
# shellcheck disable=SC2016
printf '#!/bin/sh\nprintf "%%s\\n" "$FAKE_CLASS"\n' >"$fixture_dir/fakebin/mktemp"
chmod +x "$fixture_dir/fakebin/mktemp"
rc=0
PATH="$fixture_dir/fakebin:$PATH" FAKE_CLASS="$fixture_dir/nodir/class" GO_BIN="$fixture_dir/go" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >"$fixture_dir/out" 2>&1 || rc=$?
[ "$rc" = 4 ] || { cat "$fixture_dir/out" >&2; printf 'green main fixture: a class file that cannot be written exited %s, want 4\n' "$rc" >&2; exit 1; }
rc=0
GO_BIN="$fixture_dir/go" sh -c "$(cat "$script_dir/require-green-main.sh")" /nonexistent/a/b/require-green-main.sh Hikyo-Org/hikyo "$commit" >"$fixture_dir/out" 2>&1 || rc=$?
[ "$rc" = 4 ] || { cat "$fixture_dir/out" >&2; printf 'green main fixture: an unresolvable script directory exited %s, want 4\n' "$rc" >&2; exit 1; }
[ "$(wc -l <"$CALLS" | tr -d ' ')" = "$before" ] || { printf 'green main fixture: the verifier ran although setup failed\n' >&2; exit 1; }
for bad in main abc ABCDEF0000000000000000000000000000000000; do
 if GO_BIN="$fixture_dir/go" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$bad" >/dev/null 2>&1; then
  printf 'green main fixture: mutable/invalid source accepted\n' >&2
  exit 1
 fi
done
if GO_BIN="$fixture_dir/go" "$script_dir/require-green-main.sh" '../foreign' "$commit" >/dev/null 2>&1; then
 printf 'green main fixture: invalid repository accepted\n' >&2
 exit 1
fi
if GO_BIN="$fixture_dir/go" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" extra >/dev/null 2>&1; then
 printf 'green main fixture: extra arguments accepted\n' >&2
 exit 1
fi
[ "$(wc -l <"$CALLS" | tr -d ' ')" = "$before" ]
printf 'green main fixture: exact source forwarding, bounded compile workers and verifier failure propagation passed\n'
