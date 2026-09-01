{{- define "hikyo.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "hikyo.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "hikyo.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "hikyo.labels" -}}
app.kubernetes.io/name: {{ include "hikyo.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end -}}

{{- define "hikyo.operator.fullname" -}}
{{- printf "%s-operator" (include "hikyo.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "hikyo.server.validate" -}}
{{- $databaseSecret := required "database.existingSecret is required" .Values.database.existingSecret -}}
{{- $rootKeySecret := required "rootKey.existingSecret is required" .Values.rootKey.existingSecret -}}
{{- $rootKeyName := required "rootKey.key is required" .Values.rootKey.key -}}
{{- if or (eq $rootKeyName ".") (eq $rootKeyName "..") (not (regexMatch "^[A-Za-z0-9._-]+$" $rootKeyName)) -}}
  {{- fail "rootKey.key must be one Secret key name using only letters, digits, dot, underscore, or hyphen" -}}
{{- end -}}
{{- $origin := required "externalOrigin is required" .Values.externalOrigin -}}
{{- if contains "\\" $origin -}}
  {{- fail "externalOrigin must not contain a backslash" -}}
{{- end -}}
{{- if not (regexMatch `^https?://[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*(:[1-9][0-9]{0,4})?$` $origin) -}}
  {{- fail "externalOrigin must be a canonical lowercase DNS origin without userinfo, path, query, fragment, trailing slash, or default port" -}}
{{- end -}}
{{- if and (not (hasPrefix "https://" $origin)) (not .Values.network.allowPlaintextOrigin) -}}
  {{- fail "externalOrigin must use https:// unless network.allowPlaintextOrigin is true" -}}
{{- end -}}
{{- $portSuffix := regexFind `:[0-9]+$` $origin -}}
{{- if $portSuffix -}}
  {{- $port := atoi (trimPrefix ":" $portSuffix) -}}
  {{- if or (gt $port 65535) (and (hasPrefix "https://" $origin) (eq $port 443)) (and (hasPrefix "http://" $origin) (eq $port 80)) -}}
    {{- fail "externalOrigin port must be in 1..65535 and must omit the scheme default" -}}
  {{- end -}}
{{- end -}}
{{- if .Values.database.tls.existingSecret -}}
  {{- $databaseCAKey := required "database.tls.key is required when database.tls.existingSecret is set" .Values.database.tls.key -}}
  {{- if or (eq $databaseCAKey ".") (eq $databaseCAKey "..") (not (regexMatch "^[A-Za-z0-9._-]+$" $databaseCAKey)) -}}
    {{- fail "database.tls.key must be one Secret key name using only letters, digits, dot, underscore, or hyphen" -}}
  {{- end -}}
{{- end -}}
{{- $imageDigest := required "image.digest is required" .Values.image.digest -}}
{{- if .Values.ha.enabled -}}
  {{- if lt (int .Values.ha.replicaCount) 2 -}}
    {{- fail "ha.replicaCount must be at least 2 when ha.enabled: multi-node HA needs more than one replica" -}}
  {{- end -}}
  {{- if gt (int .Values.ha.minAvailable) (int .Values.ha.replicaCount) -}}
    {{- fail "ha.minAvailable must not exceed ha.replicaCount, or the PodDisruptionBudget blocks every voluntary disruption" -}}
  {{- end -}}
{{- end -}}
{{- end -}}

{{- define "hikyo.operator.validate" -}}
{{- if not (hasKey .Values "operator") -}}
  {{- fail "operator values are required" -}}
{{- end -}}
{{- if not (hasKey .Values.operator "enabled") -}}
  {{- fail "operator.enabled is required" -}}
{{- end -}}
{{- if .Values.operator.enabled -}}
  {{- if not (hasKey .Values.operator "namespaces") -}}
    {{- fail "operator.namespaces is required; use [] explicitly for cluster-wide authority" -}}
  {{- end -}}
  {{- if not (kindIs "slice" .Values.operator.namespaces) -}}
    {{- fail "operator.namespaces must be a list" -}}
  {{- end -}}
  {{- if ne (len .Values.operator.namespaces) (len (uniq .Values.operator.namespaces)) -}}
    {{- fail "operator.namespaces must not contain duplicates" -}}
  {{- end -}}
  {{- range .Values.operator.namespaces -}}
    {{- if empty . -}}
      {{- fail "operator.namespaces entries must not be empty" -}}
    {{- end -}}
  {{- end -}}
  {{- if not (hasKey .Values.operator "triggerRollouts") -}}
    {{- fail "operator.triggerRollouts is required" -}}
  {{- end -}}
  {{- if not (hasKey .Values.operator "designatedServiceAccounts") -}}
    {{- fail "operator.designatedServiceAccounts is required" -}}
  {{- end -}}
  {{- if not (kindIs "map" .Values.operator.designatedServiceAccounts) -}}
    {{- fail "operator.designatedServiceAccounts must be a map" -}}
  {{- end -}}
  {{- range $namespace, $serviceAccounts := .Values.operator.designatedServiceAccounts -}}
    {{- if not (kindIs "slice" $serviceAccounts) -}}
      {{- fail (printf "operator.designatedServiceAccounts[%s] must be a list" $namespace) -}}
    {{- end -}}
    {{- range $serviceAccounts -}}
      {{- if empty . -}}
        {{- fail (printf "operator.designatedServiceAccounts[%s] entries must not be empty" $namespace) -}}
      {{- end -}}
    {{- end -}}
    {{- if not (empty $.Values.operator.namespaces) -}}
      {{- if not (has $namespace $.Values.operator.namespaces) -}}
        {{- fail (printf "operator.designatedServiceAccounts[%s]: namespace %q is not in operator.namespaces; a TokenRequest grant for an unwatched namespace grants nothing" $namespace $namespace) -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
  {{- $resources := required "operator.resources is required" .Values.operator.resources -}}
  {{- $requests := required "operator.resources.requests is required" .Values.operator.resources.requests -}}
  {{- $limits := required "operator.resources.limits is required" .Values.operator.resources.limits -}}
  {{- $requestCPU := required "operator.resources.requests.cpu is required" .Values.operator.resources.requests.cpu -}}
  {{- $requestMemory := required "operator.resources.requests.memory is required" .Values.operator.resources.requests.memory -}}
  {{- $limitCPU := required "operator.resources.limits.cpu is required" .Values.operator.resources.limits.cpu -}}
  {{- $limitMemory := required "operator.resources.limits.memory is required" .Values.operator.resources.limits.memory -}}
  {{- $replicaCount := required "operator.replicaCount is required" .Values.operator.replicaCount -}}
  {{- if not (hasKey .Values.operator "leaderElection") -}}
    {{- fail "operator.leaderElection is required" -}}
  {{- end -}}
  {{- if not .Values.operator.leaderElection -}}
    {{- fail "operator.leaderElection must be true" -}}
  {{- end -}}
{{- end -}}
{{- end -}}

{{/*
hikyo.operator.namespaceRules is the per-namespace access rule set, rendered
into the cluster-wide ClusterRole OR into each watched namespace's Role so both
modes grant IDENTICAL namespace-scoped authority. It deliberately carries NO
serviceaccounts/token rule — TokenRequest grants are per-namespace Roles in both
modes (ADR § Identity: mandatory per-namespace, resourceNames-restricted).
*/}}
{{- define "hikyo.operator.namespaceRules" -}}
- apiGroups: ["hikyo.dev"]
  resources: ["hikyosecrets"]
  # `patch` is used ONLY for JSON-merge finalizer bookkeeping (a merge patch on
  # metadata.finalizers), never a whole-object update.
  verbs: ["get", "list", "watch", "patch"]
- apiGroups: ["hikyo.dev"]
  resources: ["hikyosecrets/status"]
  verbs: ["update", "patch"]
# finalizers/update is required when the OwnerReferencesPermissionEnforcement
# admission plugin is enabled, because controller ownerRefs carry
# blockOwnerDeletion.
- apiGroups: ["hikyo.dev"]
  resources: ["hikyosecrets/finalizers"]
  verbs: ["update"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
# Secrets: EXACTLY get/create/update/patch — no list/watch. The operator reads
# every Secret through the uncached API reader (no Secret informer), so
# list/watch would only cache Secret values and enlarge the compromise blast
# radius.
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "create", "update", "patch"]
{{- if .Values.operator.triggerRollouts }}
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["get", "list", "watch", "patch"]
{{- end }}
- apiGroups: [""]
  resources: ["serviceaccounts"]
  verbs: ["get"]
{{- end -}}
