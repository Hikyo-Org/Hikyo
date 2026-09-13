# Nightly action pin repair

Nightly run [34778531005](https://github.com/Hikyo-Org/Hikyo/actions/runs/34778531005), dispatched from main after the container publication change, failed workflow validation before any jobs started. No container publication ran.

GitHub's run annotation reported:

> The action docker/login-action@dbcb813823bdd20940b903addbd779551569679fd is not allowed in Hikyo-Org/Hikyo because all actions must be from a repository owned by Hikyo-Org, created by GitHub, or match one of the patterns: anchore/sbom-action@*, azure/setup-helm@*, docker/build-push-action@*, docker/login-action@*, docker/setup-buildx-action@*, goreleaser/goreleaser-action@*, sigstore/cosign-installer@*. All actions must also be pinned to a full-length commit SHA.

The login action was already allowed by repository policy. Its copied pin contained 41 hexadecimal characters. GitHub's `repos/docker/login-action/git/ref/tags/v4.6.0` endpoint confirms the correct 40-character commit is `dbcb813823bdd20940b903addbd779551569679f`.

Both nightly and stable release workflows now use that verified commit. Repository Actionlint validates action-reference syntax but does not enforce the repository's full-length SHA policy; it accepted the malformed pin. The lint job now also runs `go test ./scripts/ci -run '^TestWorkflowActionPins' -count=1`.

The additional check decodes real workflow YAML using the existing yaml.v3 dependency, checks remote step actions and reusable workflows, allows local references, and requires digest pins for container actions. Its fixtures cover the exact 41-character incident, truncated/nonhex refs, mutable tags and branches, quoted/folded scalars, aliases, local references and container digests. The live test scans every `.yml` and `.yaml` workflow.

Validation: the full `scripts/ci` Go suite, Actionlint, and `git diff --check` passed locally. The live scan found no other malformed action pins. No repository policy was relaxed.

Delivery owner: merge the signed repair PR after required checks pass, require green main CI, then dispatch nightly from repaired main. The failed run used the old workflow and should not be rerun to test this fix. Confirm the new run starts jobs and completes container publication; capture its image digest and verify the anonymous pull before reporting nightlies available.
