#!/usr/bin/env bash
# Local CI pre-flight: the cheap, deterministic checks that account for most
# pre-merge CI reds, run fail-fast in cost order so you hit a tsc/vet/dco error
# in ~2 min locally instead of ~8 min into a container job.
#
# It deliberately does NOT run the expensive or flaky tails (Playwright browser
# suite, `go test -race`, k8s-e2e, the postgres-backed test_core suite). Those
# are slow and/or load-flaky; run them on demand. See scripts/dev/README for the
# rationale and the CI-failure data behind this list.
#
#   scripts/dev/preflight.sh          fast gate (Go build/vet/fmt + typechecks)
#   scripts/dev/preflight.sh --full   also regenerate-freshness + supply-chain
#
# Re-run in a loop for continuous feedback while editing, e.g.:
#   while true; do scripts/dev/preflight.sh; read -rp 'enter to re-run'; done
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 1

full=false
case "${1:-}" in
	'') ;;
	--full) full=true ;;
	*) printf 'usage: %s [--full]\n' "$0" >&2; exit 2 ;;
esac

# Fail loud on missing prerequisites rather than silently skipping a check.
need() { command -v "$1" >/dev/null 2>&1 || { printf 'preflight: missing required tool: %s\n' "$1" >&2; exit 2; }; }
need go
need git
need fnm

# Pin Node to .nvmrc via fnm, then make pnpm available through Corepack — so the
# typechecks below run on the same toolchain CI uses, not whatever's on PATH.
eval "$(fnm env)"
fnm use --install-if-missing || { printf 'preflight: fnm could not select Node from .nvmrc\n' >&2; exit 2; }
corepack enable pnpm >/dev/null 2>&1 || true
need pnpm

stage() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
fail() { printf '\n\033[31mpreflight FAILED at: %s\033[0m\n' "$1" >&2; exit 1; }

start=$(date +%s)

# 1. DCO sign-off on this branch's commits (CI job: dco). One of the most
#    common reds and a one-line fix (`git commit -s --amend` / rebase).
stage 'DCO sign-off (vs origin/main)'
if git rev-parse --verify --quiet origin/main >/dev/null; then
	./scripts/ci/check-dco.sh origin/main HEAD || fail 'dco'
else
	# shellcheck disable=SC2016  # backticked command is literal advice text
	printf 'preflight: origin/main missing; run `git fetch origin main`. Skipping DCO.\n' >&2
fi

# 2. Go formatting + imports (CI job: generated / lint).
stage 'gofmt'
unformatted=$("$(go env GOROOT)/bin/gofmt" -l .)
[ -z "$unformatted" ] || { printf '%s\n' "$unformatted"; fail 'gofmt'; }

stage 'go import formatting'
./scripts/ci/check-go-imports.sh || fail 'go-imports'

# 3. Compile + vet the whole module (gates test_core, race, everything Go).
stage 'go build ./...'
go build ./... || fail 'go build'

stage 'go vet ./...'
go vet ./... || fail 'go vet'

# 4. Frozen API surface (CI job: freeze-guard).
stage 'API freeze guard'
./scripts/ci/check-api-freeze.sh || fail 'api-freeze'

# 5. TypeScript typechecks — the #1 pre-merge web/client failure. clients/ts
#    generates the sources web imports, so install/generate it first (mirrors
#    build-spa.sh and the client CI job).
stage 'clients/ts: install + generate + typecheck + test'
pnpm --dir clients/ts install --frozen-lockfile || fail 'clients/ts install'
pnpm --dir clients/ts run generate || fail 'clients/ts generate'
pnpm --dir clients/ts run typecheck || fail 'clients/ts typecheck'
pnpm --dir clients/ts run test || fail 'clients/ts test'

stage 'web: install + typecheck'
pnpm --dir web install --frozen-lockfile || fail 'web install'
pnpm --dir web run typecheck || fail 'web typecheck'

# --full: heavier deterministic checks worth running before a final push.
if [ "$full" = true ]; then
	# Generated code is fresh (CI job: generated). Needs the pinned generators.
	stage 'generated code is fresh'
	go tool sqlc generate || fail 'sqlc generate'
	go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml || fail 'oapi-codegen'
	go run ./internal/scanning/gen || fail 'scanning gen'
	./scripts/gen-crds.sh || fail 'gen-crds'
	git diff --exit-code -- internal/store/sqlitegen internal/store/pggen api/apigen \
		internal/scanning/rules_gen.go chart/hikyo/crds internal/operator/api || fail 'generated drift'
	test -z "$(git status --porcelain --untracked-files=all -- \
		internal/store/sqlitegen internal/store/pggen api/apigen internal/scanning/rules_gen.go \
		chart/hikyo/crds internal/operator/api)" || fail 'generated untracked drift'

	# Supply-chain / release fixtures (CI job: supply-chain-checks).
	stage 'supply-chain: chart + release fixtures'
	./scripts/ci/check-chart.sh || fail 'check-chart'
	./scripts/release/test-fixtures.sh || fail 'release fixtures'
fi

printf '\n\033[32mpreflight OK (%ss)\033[0m\n' "$(( $(date +%s) - start ))"
