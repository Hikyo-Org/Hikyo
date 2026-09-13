#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
planner=$repo_root/scripts/ci/analysis-shards
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-analysis-shards.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM

mkdir -p \
	"$fixture_dir/extra" \
	"$fixture_dir/internal/app" \
	"$fixture_dir/internal/crypto" \
	"$fixture_dir/internal/isolation" \
	"$fixture_dir/internal/service" \
	"$fixture_dir/internal/store" \
	"$fixture_dir/internal/lint" \
	"$fixture_dir/internal/upgradegate" \
	"$fixture_dir/internal/store/upgrade"

printf '%s\n' 'module example.com/shards' 'go 1.27.0' >"$fixture_dir/go.mod"

for package in extra internal/app internal/crypto internal/isolation internal/service internal/store internal/lint internal/upgradegate internal/store/upgrade; do
	package_name=${package##*/}
	printf 'package %s\n' "$package_name" >"$fixture_dir/$package/$package_name.go"
done

cat >"$fixture_dir/extra/extra_test.go" <<'EOF'
package extra

import "testing"

func FuzzAuto(f *testing.F) { f.Fuzz(func(*testing.T, []byte) {}) }
EOF

cat >"$fixture_dir/internal/app/app_test.go" <<'EOF'
package app

import "testing"

func TestBoot(t *testing.T)           {}
func TestRestore(t *testing.T)        {}
func TestUpgrade(t *testing.T)        {}
func TestMaintenance(t *testing.T)    {}
func TestScheduler(t *testing.T)      {}
func TestDiagnostics(t *testing.T)    {}
func TestOwnerRuntime(t *testing.T)   {}
func TestAutomaticDrill(t *testing.T) {}
func helperNotATest(t *testing.T)     {}
func FuzzApp(f *testing.F) { f.Fuzz(func(*testing.T, []byte) {}) }
func Example() {
	// Output:
}
func Example_documentation() {}
EOF

cat >"$fixture_dir/internal/crypto/crypto_test.go" <<'EOF'
package crypto

import "testing"

func FuzzOpen(f *testing.F)  { f.Fuzz(func(*testing.T, []byte) {}) }
func FuzzParse(f *testing.F) { f.Fuzz(func(*testing.T, []byte) {}) }
EOF

cat >"$fixture_dir/internal/isolation/isolation_test.go" <<'EOF'
package isolation

import "testing"

func FuzzIsolation(f *testing.F) { f.Fuzz(func(*testing.T, []byte) {}) }
func TestAlpha(t *testing.T)      {}
func TestBravo(t *testing.T)      {}
func TestCharlie(t *testing.T)    {}
func TestDelta(t *testing.T)      {}
func TestEcho(t *testing.T)       {}
func TestFoxtrot(t *testing.T)    {}
func TestGolf(t *testing.T)       {}
func TestHotel(t *testing.T)      {}
func TestIndia(t *testing.T)      {}

// Go does not discover a lowercase rune after the Test prefix.
func Testlower(t *testing.T) {}
EOF

cat >"$fixture_dir/internal/service/service_test.go" <<'EOF'
package service

import "testing"

func FuzzService(f *testing.F) { f.Fuzz(func(*testing.T, []byte) {}) }
func TestOne(t *testing.T) {}
func TestTwo(t *testing.T) {}
func TestThree(t *testing.T) {}
func TestFour(t *testing.T) {}
func TestFive(t *testing.T) {}
func TestSix(t *testing.T) {}
func TestSeven(t *testing.T) {}
func TestEight(t *testing.T) {}
EOF
cat >"$fixture_dir/internal/service/external_test.go" <<'EOF'
package service_test

import "testing"

func TestExternal(t *testing.T) {}
func FuzzExternal(f *testing.F) { f.Fuzz(func(*testing.T, []byte) {}) }
func Example_external() {
	// Output:
}
EOF

race_actual=$fixture_dir/race-actual
fuzz_actual=$fixture_dir/fuzz-actual
isolation_actual=$fixture_dir/isolation-actual
: >"$race_actual"
: >"$fuzz_actual"
: >"$isolation_actual"

shard=0
while [ "$shard" -lt 3 ]; do
	"$planner" fuzz --root "$fixture_dir" --shard "$shard" --shards 3 |
		awk -v shard="$shard" '{ print shard "\t" $0 }' >>"$fuzz_actual"
	"$planner" isolation --root "$fixture_dir" --shard "$shard" --shards 3 |
		awk -v shard="$shard" '{ print shard "\t" $0 }' >>"$isolation_actual"
	shard=$((shard + 1))
done

# Across supported layouts, every normal package and every split-suite target
# must run exactly once, including external tests, fuzz seeds and examples.
for shard_count in 1 3 6; do
	: >"$race_actual"
	shard=0
	while [ "$shard" -lt "$shard_count" ]; do
		"$planner" race --root "$fixture_dir" --shard "$shard" --shards "$shard_count" |
			awk -v shard="$shard" '{ print shard "\t" $0 }' >>"$race_actual"
		shard=$((shard + 1))
	done
	awk -F '\t' '$2 !~ /internal\/(app|service)$/ { print $2 }' "$race_actual" | sort >"$fixture_dir/whole-actual"
	printf '%s\n' extra internal/crypto internal/lint internal/store internal/store/upgrade internal/upgradegate |
		sed 's|^|example.com/shards/|' | sort >"$fixture_dir/whole-expected"
	cmp "$fixture_dir/whole-expected" "$fixture_dir/whole-actual"
	if awk -F '\t' '$2 !~ /internal\/(app|service)$/ && NF != 2 { found=1 } END { exit !found }' "$race_actual"; then
		printf 'analysis shard fixture failed: unexpected filter on whole package\n' >&2
		exit 1
	fi
	for package in app service; do
		awk -F '\t' -v package="example.com/shards/internal/$package" '$2 == package { print $3 }' "$race_actual" |
			sed 's/^\^(//; s/)\$$//' | tr '|' '\n' | sort >"$fixture_dir/targets-actual"
		if [ "$package" = app ]; then
			printf '%s\n' Example FuzzApp TestAutomaticDrill TestBoot TestDiagnostics TestMaintenance TestOwnerRuntime TestRestore TestScheduler TestUpgrade
		else
			printf '%s\n' Example_external FuzzExternal FuzzService TestEight TestExternal TestFive TestFour TestOne TestSeven TestSix TestThree TestTwo
		fi | sort >"$fixture_dir/targets-expected"
		cmp "$fixture_dir/targets-expected" "$fixture_dir/targets-actual"
		split_count=$(awk -F '\t' -v package="example.com/shards/internal/$package" '$2 == package { print $1 }' "$race_actual" | sort -u | wc -l | tr -d ' ')
		expected_count=$shard_count
		[ "$expected_count" -ne 3 ] || expected_count=2
		[ "$expected_count" -le 4 ] || expected_count=4
		[ "$split_count" -ge "$expected_count" ] || {
			printf 'analysis shard fixture failed: %s not spread over %s runners\n' "$package" "$expected_count" >&2
			exit 1
		}
	done
	if [ "$shard_count" -eq 6 ]; then
		awk -F '\t' '
			$2 ~ /internal\/(app|service)$/ && $1 >= 4 { exit 1 }
			$2 ~ /internal\/(store|lint)$/ && $1 != 4 { exit 1 }
			$2 ~ /internal\/(upgradegate|store\/upgrade)$/ && $1 != 5 { exit 1 }
		' "$race_actual"
	fi
done
if [ -n "$(cut -f2- "$fuzz_actual" | sort | uniq -d)" ]; then
	printf 'analysis shard fixture failed: fuzz target assigned more than once\n' >&2
	exit 1
fi
if [ -n "$(cut -f2 "$isolation_actual" | sort | uniq -d)" ]; then
	printf 'analysis shard fixture failed: isolation test assigned more than once\n' >&2
	exit 1
fi

cut -f2- "$fuzz_actual" | sort >"$fixture_dir/fuzz-targets"
cat >"$fixture_dir/fuzz-expected" <<'EOF'
example.com/shards/extra	FuzzAuto
example.com/shards/internal/app	FuzzApp
example.com/shards/internal/crypto	FuzzOpen
example.com/shards/internal/crypto	FuzzParse
example.com/shards/internal/isolation	FuzzIsolation
example.com/shards/internal/service	FuzzExternal
example.com/shards/internal/service	FuzzService
EOF
cmp "$fixture_dir/fuzz-expected" "$fixture_dir/fuzz-targets"

cut -f2 "$isolation_actual" | sort >"$fixture_dir/isolation-tests"
cat >"$fixture_dir/isolation-expected" <<'EOF'
TestAlpha
TestBravo
TestCharlie
TestDelta
TestEcho
TestFoxtrot
TestGolf
TestHotel
TestIndia
EOF
cmp "$fixture_dir/isolation-expected" "$fixture_dir/isolation-tests"

crypto_shards=$(awk -F '\t' '$2 == "example.com/shards/internal/crypto" { print $1 }' \
	"$fuzz_actual" | sort -u | wc -l | tr -d ' ')
[ "$crypto_shards" -eq 1 ] || {
	printf 'analysis shard fixture failed: one package was split across fuzz shards\n' >&2
	exit 1
}

if "$planner" race --root "$fixture_dir" --shard 3 --shards 3 >/dev/null 2>&1; then
	printf 'analysis shard fixture failed: out-of-range shard accepted\n' >&2
	exit 1
fi

printf 'analysis shard fixture: complete, disjoint race packages, fuzz targets, and isolation tests passed\n'
