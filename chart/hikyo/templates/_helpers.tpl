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

{{- define "hikyo.storage.name" -}}
{{- printf "%s-storage" (include "hikyo.fullname" . | trunc 55 | trimSuffix "-") -}}
{{- end -}}

{{- define "hikyo.server.validate" -}}
{{- $storage := .Values.database.storageMonitoring -}}
{{- if $storage.enabled -}}
  {{- $url := required "database.storageMonitoring.kubeletURL is required" $storage.kubeletURL -}}
  {{- if not (regexMatch `^https://([a-z0-9]([a-z0-9.-]*[a-z0-9])?|\[[0-9a-f:]+\])(:[1-9][0-9]{0,4})?$` $url) -}}
    {{- fail "database.storageMonitoring.kubeletURL must be an HTTPS origin without credentials, path, query or fragment" -}}
  {{- end -}}
  {{- $port := regexFind `:[0-9]+$` $url -}}
  {{- if and $port (gt (atoi (trimPrefix ":" $port)) 65535) -}}
    {{- fail "database.storageMonitoring.kubeletURL port must be in 1..65535" -}}
  {{- end -}}
  {{- range $field := list "node" "pvc" -}}
    {{- $value := required (printf "database.storageMonitoring.%s is required" $field) (index $storage $field) -}}
    {{- if or (gt (len $value) 253) (not (regexMatch `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` $value)) -}}
      {{- fail (printf "database.storageMonitoring.%s must be a Kubernetes DNS subdomain name" $field) -}}
    {{- end -}}
  {{- end -}}
  {{- if and $storage.namespace (or (gt (len $storage.namespace) 63) (not (regexMatch `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` $storage.namespace))) -}}
    {{- fail "database.storageMonitoring.namespace must be a Kubernetes namespace name" -}}
  {{- end -}}
{{- else if or $storage.kubeletURL $storage.node $storage.namespace $storage.pvc -}}
  {{- fail "database.storageMonitoring inputs require enabled=true" -}}
{{- end -}}
{{- $databaseSecret := required "database.existingSecret is required" .Values.database.existingSecret -}}
{{- $rootKeySecret := required "rootKey.existingSecret is required" .Values.rootKey.existingSecret -}}
{{- $upgradeStateClaim := required "upgrade.stateExistingClaim is required" .Values.upgrade.stateExistingClaim -}}
{{- if .Values.upgrade.unattended.enabled -}}
  {{- if or .Values.ha.enabled (ne (int .Values.replicaCount) 1) -}}
    {{- fail "upgrade.unattended requires ha.enabled=false and replicaCount=1" -}}
  {{- end -}}
  {{- if .Values.rollout.enabled -}}
    {{- fail "upgrade.unattended cannot share authority with rollout.enabled" -}}
  {{- end -}}
  {{- if or .Values.upgrade.existingClaim .Values.upgrade.evidence .Values.upgrade.targetManifestSHA256 .Values.upgrade.legacyWritersStopped -}}
    {{- fail "upgrade.unattended cannot combine manual bundle, evidence, target or legacy-writer overrides" -}}
  {{- end -}}
  {{- $scratchSecret := required "upgrade.unattended.scratchDatabaseExistingSecret is required" .Values.upgrade.unattended.scratchDatabaseExistingSecret -}}
  {{- $scratchKey := required "upgrade.unattended.scratchDatabaseKey is required" .Values.upgrade.unattended.scratchDatabaseKey -}}
  {{- if lt (int .Values.upgrade.unattended.startupFailureThreshold) 1 -}}
    {{- fail "upgrade.unattended.startupFailureThreshold must be positive" -}}
  {{- end -}}
  {{- if or (eq $scratchKey ".") (eq $scratchKey "..") (not (regexMatch "^[A-Za-z0-9._-]+$" $scratchKey)) -}}
    {{- fail "upgrade.unattended.scratchDatabaseKey must be one Secret key name" -}}
  {{- end -}}
{{- else -}}
{{- $upgradeClaim := required "upgrade.existingClaim is required" .Values.upgrade.existingClaim -}}
{{- if eq $upgradeClaim $upgradeStateClaim -}}
  {{- fail "upgrade public artifacts and writable installation state require separate claims" -}}
{{- end -}}
{{- end -}}
{{- if and .Values.upgrade.targetManifestSHA256 (not (regexMatch "^[0-9a-f]{64}$" .Values.upgrade.targetManifestSHA256)) -}}
  {{- fail "upgrade.targetManifestSHA256 must be an exact lowercase SHA-256" -}}
{{- end -}}
{{- if and .Values.upgrade.legacyWritersStopped (not .Values.upgrade.evidence) -}}
  {{- fail "upgrade.legacyWritersStopped requires public upgrade evidence" -}}
{{- end -}}
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
{{- if and .Values.mcp.enabled (not (hasPrefix "https://" $origin)) -}}
  {{- fail "mcp.enabled requires an https:// externalOrigin" -}}
{{- end -}}
{{- $portSuffix := regexFind `:[0-9]+$` $origin -}}
{{- if $portSuffix -}}
  {{- $port := atoi (trimPrefix ":" $portSuffix) -}}
  {{- if or (gt $port 65535) (and (hasPrefix "https://" $origin) (eq $port 443)) (and (hasPrefix "http://" $origin) (eq $port 80)) -}}
    {{- fail "externalOrigin port must be in 1..65535 and must omit the scheme default" -}}
  {{- end -}}
{{- end -}}
{{- if and (not .Values.mcp.enabled) (not (empty .Values.mcp.allowedOrigins)) -}}
  {{- fail "mcp.allowedOrigins requires mcp.enabled=true" -}}
{{- end -}}
{{- if and (not .Values.mcp.enabled) .Values.mcp.writeEnabled -}}
  {{- fail "mcp.writeEnabled requires mcp.enabled=true" -}}
{{- end -}}
{{- if ne (len .Values.mcp.allowedOrigins) (len (uniq .Values.mcp.allowedOrigins)) -}}
  {{- fail "mcp.allowedOrigins must not contain duplicates" -}}
{{- end -}}
{{- range $mcpOrigin := .Values.mcp.allowedOrigins -}}
  {{- if or (contains "\\" $mcpOrigin) (not (regexMatch `^https?://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(:[1-9][0-9]{0,4})?$` $mcpOrigin)) -}}
    {{- fail (printf "mcp.allowedOrigins entry %q must be an exact canonical lowercase HTTP(S) origin" $mcpOrigin) -}}
  {{- end -}}
  {{- $mcpPortSuffix := regexFind `:[0-9]+$` $mcpOrigin -}}
  {{- if $mcpPortSuffix -}}
    {{- $mcpPort := atoi (trimPrefix ":" $mcpPortSuffix) -}}
    {{- if or (gt $mcpPort 65535) (and (hasPrefix "https://" $mcpOrigin) (eq $mcpPort 443)) (and (hasPrefix "http://" $mcpOrigin) (eq $mcpPort 80)) -}}
      {{- fail (printf "mcp.allowedOrigins entry %q port must be in 1..65535 and must omit the scheme default" $mcpOrigin) -}}
    {{- end -}}
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
  {{- if and (lt (int .Values.ha.replicaCount) 2) (not (and .Values.rollout.enabled .Values.rollout.topologyNodeIDs (eq (int .Values.ha.replicaCount) 1))) -}}
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
  {{- if not (hasKey .Values.operator "statusReporting") -}}
    {{- fail "operator.statusReporting is required" -}}
  {{- end -}}
  {{- if not (kindIs "bool" .Values.operator.statusReporting) -}}
    {{- fail "operator.statusReporting must be a boolean" -}}
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
hikyo.operator.clusterReads is the cluster-scoped read rule set of the operator
ClusterRole in both authority modes. Status reporting adds `get` on exactly the
kube-system Namespace, whose UID is the reported cluster id.
*/}}
{{- define "hikyo.operator.clusterReads" -}}
- apiGroups: ["hikyo.dev"]
  resources: ["hikyoinstances"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["apiextensions.k8s.io"]
  resources: ["customresourcedefinitions"]
  resourceNames: ["hikyoinstances.hikyo.dev", "hikyosecrets.hikyo.dev"]
  verbs: ["get"]
{{- if .Values.operator.statusReporting }}
- apiGroups: [""]
  resources: ["namespaces"]
  resourceNames: ["kube-system"]
  verbs: ["get"]
{{- end }}
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
# Secrets: get/create/update/patch; no list/watch. Opt-in delete withdraws
# owned typed Secrets under authoritative refusal, using UID/version preconditions.
# The operator reads
# every Secret through the uncached API reader (no Secret informer), so
# list/watch would only cache Secret values and enlarge the compromise blast
# radius.
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "create", "update", "patch"{{ if .Values.operator.nativeSecretTypes }}, "delete"{{ end }}]
{{- if .Values.operator.triggerRollouts }}
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["get", "list", "watch", "patch"]
{{- end }}
- apiGroups: [""]
  resources: ["serviceaccounts"]
  verbs: ["get"]
{{- end -}}
