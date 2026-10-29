#!/usr/bin/env bash
# Feedback policy, shared by local execution and hosted setup. Source only from
# the feedback tooling owner, never from the candidate being inspected.
# Indexed arrays and shell builtins keep planning usable with Bash 3.2 before
# Go, Node, pnpm or jq setup. Paths stay in arrays, never executable plan data.

feedback_scope_contains() {
	case "$1:$2" in
		all:* | tooling:scripts/*) return 0 ;;
		application:scripts/*) return 1 ;;
		application:*) return 0 ;;
		*) return 1 ;;
	esac
}

feedback_go_inputs() {
	local scope=$1 path
	go_files=()
	go_packages=()
	for path in ${feedback_go_files[@]+"${feedback_go_files[@]}"}; do
		if feedback_scope_contains "$scope" "${path#./}"; then go_files+=("$path"); fi
	done
	for path in ${feedback_go_packages[@]+"${feedback_go_packages[@]}"}; do
		if feedback_scope_contains "$scope" "${path#./}/"; then go_packages+=("$path"); fi
	done
}

feedback_plan_build() {
	local path package existing duplicate
	local remaining_go=()
	web=false client=false workflows=false docs=false all_go=false
	release_admission=false audit_collector=false
	feedback_go_files=() feedback_go_packages=() shell_files=()
	while IFS= read -r -d '' path; do
		case "$path" in
			scripts/ci/release-admission/* | scripts/ci/aws-acceptance/* | .github/workflows/aws-acceptance.yml) release_admission=true ;;
		esac
		case "$path" in
			scripts/ops/audit-collector/* | docs/operations/audit-collector.md) audit_collector=true ;;
		esac
		case "$path" in
			web/*) web=true ;;
			clients/ts/* | api/openapi.yaml) client=true; web=true ;;
			.nvmrc | package.json | pnpm-lock.yaml | pnpm-workspace.yaml) client=true; web=true ;;
		esac
		case "$path" in
			SECURITY.md | SUPPORT.md | GOVERNANCE.md | TRADEMARK.md | CONTRIBUTING.md | LICENSE | docs/status/* | docs/release/* | docs/site/* | install/upgrade-nightly.sh | .nvmrc | package.json | pnpm-lock.yaml | pnpm-workspace.yaml) docs=true ;;
		esac
		case "$path" in
			.github/workflows/*.yml | .github/workflows/*.yaml) workflows=true ;;
			go.mod | go.sum) all_go=true ;;
		esac
		case "$path" in
			*.go)
				case "$path" in
					*/*) package="./${path%/*}" ;;
					*) package=./. ;;
				esac
				remaining_go=("$package"/*.go)
				if [ -f "${remaining_go[0]}" ]; then
					duplicate=false
					for existing in ${feedback_go_packages[@]+"${feedback_go_packages[@]}"}; do
						if [ "$existing" = "$package" ]; then duplicate=true; break; fi
					done
					if [ "$duplicate" = false ]; then feedback_go_packages+=("$package"); fi
				fi
				;;
		esac
		[ -f "$path" ] || continue
		case "$path" in
			*.go) feedback_go_files+=("./$path") ;;
			*.sh | .githooks/* | install/install.sh.in) shell_files+=("./$path") ;;
		esac
	done <"$1"
}

# Stable check IDs are data. Both adapters consume this ordering; only the
# execution adapter knows commands. Local unscoped Go keeps its combined graph.
feedback_plan_select() {
	local lane=$1 scope=$2 check prerequisite
	feedback_checks=()
	feedback_prerequisites=()
	feedback_go_inputs "$scope"
	case "$lane" in
		go)
			if [ "$scope" != application ]; then
				if [ "$audit_collector" = true ]; then feedback_checks+=(audit-collector); fi
				if [ "$release_admission" = true ]; then feedback_checks+=(release-admission); fi
			fi
			if [ "${#go_files[@]}" -gt 0 ]; then feedback_checks+=(go-format); fi
			if [ "$all_go" = true ] && [ "$scope" != tooling ]; then feedback_checks+=(go-mod-verify); fi
			if [ "${#go_packages[@]}" -gt 0 ]; then feedback_checks+=(go-compile); fi
			if [ "$scope" != application ]; then
				if [ "${#shell_files[@]}" -gt 0 ]; then feedback_checks+=(shellcheck); fi
				if [ "$workflows" = true ]; then feedback_checks+=(actionlint); fi
				if [ "$docs" = true ]; then feedback_checks+=(docs); fi
			fi
			;;
		frontend)
			if [ "$client" = true ]; then feedback_checks+=(client-typecheck client-test); fi
			if [ "$web" = true ]; then feedback_checks+=(web-typecheck web-lint web-test); fi
			;;
		*) fail "unknown feedback adapter lane: $lane" ;;
	esac
	for check in ${feedback_checks[@]+"${feedback_checks[@]}"}; do
		feedback_check_prerequisites "$check"
		for prerequisite in ${feedback_check_needs[@]+"${feedback_check_needs[@]}"}; do
			if ! feedback_has_prerequisite "$prerequisite"; then feedback_prerequisites+=("$prerequisite"); fi
		done
	done
}

feedback_check_prerequisites() {
	case "$1" in
		audit-collector) feedback_check_needs=(python) ;;
		release-admission) feedback_check_needs=(python releaseDependencies) ;;
		go-format | go-mod-verify | go-compile) feedback_check_needs=(go) ;;
		shellcheck) feedback_check_needs=(shellcheck) ;;
		actionlint) feedback_check_needs=(go shellcheck) ;;
		docs) feedback_check_needs=(node) ;;
		client-typecheck | client-test) feedback_check_needs=(clientDependencies node pnpm jq) ;;
		web-typecheck | web-lint | web-test) feedback_check_needs=(clientDependencies webDependencies node pnpm jq) ;;
		*) fail "unknown feedback check: $1" ;;
	esac
}

feedback_has_prerequisite() {
	local prerequisite
	for prerequisite in ${feedback_prerequisites[@]+"${feedback_prerequisites[@]}"}; do
		[ "$prerequisite" != "$1" ] || return 0
	done
	return 1
}

feedback_plan_hosted_lane() {
	# This is the only hosted-to-local mapping. Workflow consumers pass these
	# enum values as quoted arguments instead of interpreting domain booleans.
	case "$1" in
		go) feedback_lane=go feedback_scope=application ;;
		tooling) feedback_lane=go feedback_scope=tooling ;;
		frontend) feedback_lane=frontend feedback_scope=all ;;
		*) fail "unknown hosted feedback lane: $1" ;;
	esac
	feedback_plan_select "$feedback_lane" "$feedback_scope"
}

feedback_plan_lane_json() {
	local check prerequisite separator='' need_separator
	printf '{"lane":"%s","goScope":"%s","checks":[' "$feedback_lane" "$feedback_scope"
	for check in ${feedback_checks[@]+"${feedback_checks[@]}"}; do
		printf '%s{"id":"%s","prerequisites":[' "$separator" "$check"
		feedback_check_prerequisites "$check"
		need_separator=''
		for prerequisite in ${feedback_check_needs[@]+"${feedback_check_needs[@]}"}; do
			printf '%s"%s"' "$need_separator" "$prerequisite"
			need_separator=,
		done
		printf ']}'
		separator=,
	done
	printf '],"prerequisites":{'
	separator=''
	for prerequisite in go node pnpm jq shellcheck python releaseDependencies clientDependencies webDependencies; do
		printf '%s"%s":' "$separator" "$prerequisite"
		if feedback_has_prerequisite "$prerequisite"; then printf true; else printf false; fi
		separator=,
	done
	printf '}}'
}

feedback_plan_json() {
	local scope=$1 hosted_lane selected_go=false selected_shell=false separator=''
	feedback_go_inputs "$scope"
	# Preserve the original domain contract, including manifest-only tooling
	# scope reporting go=true even though that scope does not run mod verify.
	if [ "${#go_packages[@]}" -gt 0 ] || [ "$all_go" = true ]; then selected_go=true; fi
	if [ "${#shell_files[@]}" -gt 0 ]; then selected_shell=true; fi
	printf '{"web":%s,"client":%s,"workflows":%s,"go":%s,"shell":%s,"docs":%s,"releaseAdmission":%s,"auditCollector":%s,"lanes":{' \
		"$web" "$client" "$workflows" "$selected_go" "$selected_shell" "$docs" "$release_admission" "$audit_collector"
	for hosted_lane in go tooling frontend; do
		feedback_plan_hosted_lane "$hosted_lane"
		printf '%s"%s":' "$separator" "$hosted_lane"
		feedback_plan_lane_json
		separator=,
	done
	printf '}}\n'
}
