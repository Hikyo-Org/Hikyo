{{- define "hikyo.rollout.name" -}}
{{- printf "%s-rollout" (include "hikyo.fullname" .) | trunc 55 | trimSuffix "-" -}}
{{- end -}}

{{/* Admission policies are cluster-scoped. Bind their names to the complete
namespace and rollout identity so equal release names can coexist. */}}
{{- define "hikyo.rollout.policyName" -}}
{{- $identity := printf "%s/%s" .Release.Namespace (include "hikyo.rollout.name" .) -}}
{{- printf "%s-%s" (.Release.Namespace | trunc 20 | trimSuffix "-") ($identity | sha256sum | trunc 24) -}}
{{- end -}}

{{- define "hikyo.rollout.validate" -}}
{{- if .Values.rollout.enabled -}}
{{- if or (ne (int .Values.replicaCount) 1) (and .Values.ha.enabled (or (empty .Values.rollout.topologyNodeIDs) (ne (int .Values.ha.replicaCount) 1))) -}}{{- fail "enrolled config rollout requires one server replica; singleton HA requires explicit topology identity enrollment" -}}{{- end -}}
{{- if .Values.rollout.topologyNodeIDs -}}
{{- $stable := printf "%s-server" (include "hikyo.fullname" .) -}}
{{- if not (has $stable .Values.rollout.topologyNodeIDs) -}}{{- fail "rollout.topologyNodeIDs must include the installed stable server identity" -}}{{- end -}}
{{- end -}}
{{- if .Values.rollout.enrolled -}}
{{- if not (semverCompare ">=1.36.0-0 <1.37.0-0" .Capabilities.KubeVersion.Version) -}}
{{- fail "enrolled config rollout requires Kubernetes 1.36 for its closed field-shape admission contract" -}}
{{- end -}}
{{- range $key := list "enrollmentID" "ownerInstanceID" "incarnation" "deploymentUID" "commandSecretUID" "responseSecretUID" "journalSecretUID" "leaseUID" "authorityPublicKey" "authorityExistingSecret" "authorityKey" -}}
{{- if empty (index $.Values.rollout $key) -}}{{- fail (printf "rollout.%s is required for enrolled custody" $key) -}}{{- end -}}
{{- end -}}
{{- if .Values.rollout.upgradeSources -}}
{{- range $alias := .Values.rollout.upgradeStateAliases -}}
{{- $source := index $.Values.rollout.upgradeSources $alias -}}
{{- if or (not $source) (ne $source.state_directory (printf "/var/lib/hikyo-upgrade/aliases/%s" $alias)) -}}
{{- fail "rollout.upgradeStateAliases must name a source using its fixed /var/lib/hikyo-upgrade/aliases/<alias> custody mount" -}}
{{- end -}}
{{- end -}}
{{- range $alias, $source := .Values.rollout.upgradeSources -}}
{{- if and (ne $source.state_directory "/var/lib/hikyo-upgrade/operator-custody") (not (has $alias $.Values.rollout.upgradeStateAliases)) -}}
{{- fail "alternate upgrade state directories require an immutable same-PVC rollout.upgradeStateAliases mount" -}}
{{- end -}}
{{- end -}}
{{- $initial := index .Values.rollout.upgradeSources .Values.rollout.initialUpgradeSource -}}
{{- $installed := include "hikyo.rollout.installedUpgradeSource" . | fromJson -}}
{{- if or (not $initial) (not (deepEqual $initial $installed)) -}}{{- fail "rollout.initialUpgradeSource must name an enrolled tuple matching all seven installed upgrade inputs" -}}{{- end -}}
{{- else if or .Values.rollout.initialUpgradeSource .Values.rollout.upgradeStateAliases -}}
{{- fail "rollout.initialUpgradeSource requires an enrolled upgradeSources descriptor" -}}
{{- end -}}
{{- if .Values.rollout.databaseSources -}}
{{- $initial := index .Values.rollout.databaseSources .Values.rollout.initialDatabaseSource -}}
{{- if or (not $initial) (ne $initial.name .Values.database.existingSecret) (ne $initial.key "HIKYO_DB") -}}{{- fail "rollout.initialDatabaseSource must name the installed database.existingSecret HIKYO_DB alias" -}}{{- end -}}
{{- end -}}
{{- if .Values.rollout.rootSources -}}
{{- $initial := index .Values.rollout.rootSources .Values.rollout.initialRootSource -}}
{{- if or (not $initial) (ne $initial.name .Values.rootKey.existingSecret) (ne $initial.key .Values.rootKey.key) -}}{{- fail "rollout.initialRootSource must name the installed rootKey.existingSecret/key alias" -}}{{- end -}}
{{- end -}}
{{- end -}}
{{- range $sources := list .Values.rollout.databaseSources .Values.rollout.rootSources -}}
{{- range $alias, $source := $sources -}}
{{- if or (not (regexMatch "^[a-z0-9]([a-z0-9-]{0,29}[a-z0-9])?$" $alias)) (empty $source.name) (empty $source.key) -}}
{{- fail "rollout source aliases must be DNS labels of at most 31 characters with an installed Secret name/key" -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "hikyo.rollout.enrollment" -}}
{{- $name := include "hikyo.rollout.name" . -}}
{{- $target := dict "namespace" .Release.Namespace "deployment" (include "hikyo.fullname" .) "deployment_uid" .Values.rollout.deploymentUID "container" "server" "stable_node_id" (printf "%s-server" (include "hikyo.fullname" .)) "config_secret" (printf "%s-config" $name) "rollback_secret" (printf "%s-rollback" $name) "request_secret" (printf "%s-plan" $name) "receipt_secret" (printf "%s-receipt" $name) "sources" .Values.rollout.fileSources "database_sources" .Values.rollout.databaseSources "root_sources" .Values.rollout.rootSources "upgrade_sources" .Values.rollout.upgradeSources "initial_upgrade_source" .Values.rollout.initialUpgradeSource -}}
{{- if .Values.rollout.topologyNodeIDs -}}{{- $_ := set $target "topology_node_ids" .Values.rollout.topologyNodeIDs -}}{{- end -}}
{{- dict "id" .Values.rollout.enrollmentID "owner_instance_id" .Values.rollout.ownerInstanceID "incarnation" .Values.rollout.incarnation "target" $target "command_secret" (printf "%s-command" $name) "command_secret_uid" .Values.rollout.commandSecretUID "response_secret" (printf "%s-response" $name) "response_secret_uid" .Values.rollout.responseSecretUID "journal_secret" (printf "%s-journal" $name) "journal_secret_uid" .Values.rollout.journalSecretUID "lease_name" $name "lease_uid" .Values.rollout.leaseUID "executor_pod" (printf "%s-0" $name) | toJson -}}
{{- end -}}

{{/* The initial enrolled alias describes startup exactly; it does not silently
change the release's installed upgrade evidence or persistent state location. */}}
{{- define "hikyo.rollout.installedUpgradeSource" -}}
{{- dict "bundle_directory" "/run/hikyo-upgrade/bundle" "state_directory" "/var/lib/hikyo-upgrade/operator-custody" "evidence_directory" (ternary "/run/hikyo-upgrade/evidence" "" .Values.upgrade.evidence) "ciphertext_path" (ternary "/run/hikyo-upgrade/backup.age" "" .Values.upgrade.evidence) "operator_public_key_file" "/run/hikyo-upgrade/operator.pub" "target_manifest_sha256" .Values.upgrade.targetManifestSHA256 "legacy_writers_stopped" .Values.upgrade.legacyWritersStopped | toJson -}}
{{- end -}}

{{- define "hikyo.rollout.upgradeEnvironment" -}}
{{- dict "HIKYO_UPGRADE_BUNDLE" .bundle_directory "HIKYO_UPGRADE_STATE_DIR" .state_directory "HIKYO_UPGRADE_EVIDENCE" .evidence_directory "HIKYO_UPGRADE_BACKUP" .ciphertext_path "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY" .operator_public_key_file "HIKYO_UPGRADE_TARGET_MANIFEST" .target_manifest_sha256 "HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED" (toString .legacy_writers_stopped) | toJson -}}
{{- end -}}
