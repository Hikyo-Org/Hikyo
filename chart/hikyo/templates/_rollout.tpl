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
{{- if or .Values.ha.enabled (ne (int .Values.replicaCount) 1) -}}{{- fail "enrolled config rollout requires one server replica and stable deployment-owned node identity; HA bootstrap rollout is not supported" -}}{{- end -}}
{{- if .Values.rollout.enrolled -}}
{{- if not (semverCompare ">=1.36.0-0 <1.37.0-0" .Capabilities.KubeVersion.Version) -}}
{{- fail "enrolled config rollout requires Kubernetes 1.36 for its closed field-shape admission contract" -}}
{{- end -}}
{{- range $key := list "enrollmentID" "ownerInstanceID" "incarnation" "deploymentUID" "commandSecretUID" "responseSecretUID" "journalSecretUID" "leaseUID" "authorityPublicKey" "authorityExistingSecret" "authorityKey" -}}
{{- if empty (index $.Values.rollout $key) -}}{{- fail (printf "rollout.%s is required for enrolled custody" $key) -}}{{- end -}}
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
{{- $target := dict "namespace" .Release.Namespace "deployment" (include "hikyo.fullname" .) "deployment_uid" .Values.rollout.deploymentUID "container" "server" "stable_node_id" (printf "%s-server" (include "hikyo.fullname" .)) "config_secret" (printf "%s-config" $name) "rollback_secret" (printf "%s-rollback" $name) "request_secret" (printf "%s-plan" $name) "receipt_secret" (printf "%s-receipt" $name) "sources" .Values.rollout.fileSources "database_sources" .Values.rollout.databaseSources "root_sources" .Values.rollout.rootSources -}}
{{- dict "id" .Values.rollout.enrollmentID "owner_instance_id" .Values.rollout.ownerInstanceID "incarnation" .Values.rollout.incarnation "target" $target "command_secret" (printf "%s-command" $name) "command_secret_uid" .Values.rollout.commandSecretUID "response_secret" (printf "%s-response" $name) "response_secret_uid" .Values.rollout.responseSecretUID "journal_secret" (printf "%s-journal" $name) "journal_secret_uid" .Values.rollout.journalSecretUID "lease_name" $name "lease_uid" .Values.rollout.leaseUID "executor_pod" (printf "%s-0" $name) | toJson -}}
{{- end -}}
