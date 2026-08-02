{{/*
Expand the name of the chart.
*/}}
{{- define "goService.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "goService.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "goService.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "goService.labels" -}}
helm.sh/chart: {{ include "goService.chart" . }}
{{ include "goService.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels.
*/}}
{{- define "goService.selectorLabels" -}}
app.kubernetes.io/name: {{ include "goService.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Sanitize values that are written into Kubernetes labels.
*/}}
{{- define "goService.labelValue" -}}
{{- $value := default "" . | toString -}}
{{- regexReplaceAll "[^A-Za-z0-9_.-]" $value "-" | trunc 63 | trimSuffix "-" | quote -}}
{{- end -}}

{{/*
Release identity labels injected by CI/CD.
*/}}
{{- define "goService.releaseLabels" -}}
delivery.platform/deploy-id: {{ include "goService.labelValue" .Values.release.deployId }}
delivery.platform/release-batch: {{ include "goService.labelValue" .Values.release.releaseBatch }}
delivery.platform/git-sha: {{ include "goService.labelValue" .Values.release.gitSha }}
delivery.platform/environment: {{ include "goService.labelValue" .Values.release.environment }}
delivery.platform/rollout-profile: {{ include "goService.profileName" . | quote }}
{{- end -}}

{{/*
Release identity annotations for long or revision-oriented metadata.
*/}}
{{- define "goService.releaseAnnotations" -}}
delivery.platform/image-digest: {{ default "" .Values.image.digest | quote }}
delivery.platform/gitops-revision: {{ default "" .Values.release.gitopsRevision | quote }}
{{- if .Values.observability.releaseInfo.enabled }}
delivery.platform/release-info-scrape: "true"
delivery.platform/release-info-path: {{ .Values.observability.releaseInfo.path | quote }}
delivery.platform/release-info-port: {{ default .Values.app.port .Values.observability.releaseInfo.port | quote }}
{{- end }}
{{- end -}}

{{/*
Runtime metadata exposed through the Kubernetes downward API.
*/}}
{{- define "goService.releaseEnv" -}}
- name: SERVICE_NAME
  value: {{ include "goService.catalogServiceName" . | quote }}
- name: POD_NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
- name: DEPLOY_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.labels['delivery.platform/deploy-id']
- name: RELEASE_BATCH
  valueFrom:
    fieldRef:
      fieldPath: metadata.labels['delivery.platform/release-batch']
- name: GIT_SHA
  valueFrom:
    fieldRef:
      fieldPath: metadata.labels['delivery.platform/git-sha']
- name: IMAGE_DIGEST
  valueFrom:
    fieldRef:
      fieldPath: metadata.annotations['delivery.platform/image-digest']
- name: GITOPS_REVISION
  valueFrom:
    fieldRef:
      fieldPath: metadata.annotations['delivery.platform/gitops-revision']
{{- end -}}

{{/*
Container image reference. Digest is preferred when GitOps has a resolved image.
*/}}
{{- define "goService.image" -}}
{{- $repository := .Values.image.repository -}}
{{- if .Values.image.registry -}}
{{- $repository = printf "%s/%s" .Values.image.registry .Values.image.repository -}}
{{- end -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" $repository .Values.image.digest -}}
{{- else -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s:%s" $repository $tag -}}
{{- end -}}
{{- end -}}

{{/* Rollout is enabled explicitly for deployable services. */}}
{{- define "goService.rolloutEnabled" -}}
{{- if .Values.rollout.enabled -}}true{{- else -}}false{{- end -}}
{{- end -}}

{{- define "goService.stableServiceName" -}}
{{- default (include "goService.fullname" .) .Values.rollout.stableService | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "goService.candidateServiceName" -}}
{{- default (printf "%s-candidate" (include "goService.fullname" .)) .Values.rollout.candidateService | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "goService.ingressName" -}}
{{- include "goService.fullname" . -}}
{{- end -}}

{{- define "goService.catalogServiceName" -}}
{{- default (include "goService.fullname" .) .Values.platform.serviceName -}}
{{- end -}}

{{- define "goService.profileName" -}}
{{- default "standard-canary" .Values.rollout.profile -}}
{{- end -}}

{{/*
Delivery strategy derived from the release profile. The profile is the single
source of truth; strategy can be overridden explicitly but values.schema.json
rejects profiles/strategy combinations that do not match.
*/}}
{{- define "goService.strategy" -}}
{{- if .Values.rollout.strategy -}}
{{- .Values.rollout.strategy -}}
{{- else -}}
{{- if eq (include "goService.profileName" .) "controlled-bluegreen" -}}bluegreen{{- else if eq (include "goService.profileName" .) "fast-rolling" -}}rolling{{- else -}}canary{{- end -}}
{{- end -}}
{{- end -}}

{{- define "goService.analysisTemplateName" -}}
{{- printf "%s-analysis-canary" (include "goService.fullname" .) -}}
{{- end -}}

{{- define "goService.bluegreenPreviewTemplateName" -}}
{{- printf "%s-analysis-bluegreen-preview-job" (include "goService.fullname" .) -}}
{{- end -}}

{{- define "goService.bluegreenPostTemplateName" -}}
{{- printf "%s-analysis-bluegreen-post" (include "goService.fullname" .) -}}
{{- end -}}

{{- define "goService.previewReplicaCount" -}}
{{- min (.Values.rollout.previewReplicaCount | int) (.Values.replicaCount | int) -}}
{{- end -}}

{{/*
Prometheus query arguments shared by every analysis step. The Rollout
controller injects stable-hash and latest-hash automatically, so stable and
candidate revisions are always compared exactly.
*/}}
{{- define "goService.analysisArgs" -}}
- name: service
  value: {{ include "goService.catalogServiceName" . | quote }}
- name: namespace
  value: {{ .Release.Namespace | quote }}
- name: request-route-regex
  value: {{ .Values.rollout.analysis.requestRouteRegex | quote }}
- name: operation-route-regex
  value: {{ .Values.rollout.analysis.operationRouteRegex | quote }}
- name: min-samples
  value: {{ .Values.rollout.analysis.minSamples | quote }}
- name: stable-min-samples
  value: {{ .Values.rollout.analysis.stableMinSamples | quote }}
- name: max-error-rate
  value: {{ .Values.rollout.analysis.maxErrorRate | quote }}
- name: max-error-rate-increase
  value: {{ .Values.rollout.analysis.maxErrorRateIncrease | quote }}
- name: max-p95-ratio
  value: {{ .Values.rollout.analysis.maxP95Ratio | quote }}
- name: max-p95-seconds
  value: {{ .Values.rollout.analysis.maxP95Seconds | quote }}
- name: min-operation-success-rate
  value: {{ .Values.rollout.analysis.minOperationSuccessRate | quote }}
{{- end -}}

{{- define "goService.canaryDryRunMetrics" -}}
- metricName: canary-samples
- metricName: canary-error-rate
- metricName: canary-error-rate-increase
- metricName: canary-p95
- metricName: canary-p95-ratio
- metricName: stable-samples
- metricName: stable-error-rate
- metricName: stable-p95
{{- if ne .Values.rollout.analysis.operationRouteRegex "" }}
- metricName: canary-operation-success-rate
{{- end }}
{{- end -}}

{{- define "goService.bluegreenDryRunMetrics" -}}
- metricName: preview-probe
- metricName: bluegreen-error-rate
- metricName: bluegreen-error-rate-increase
- metricName: bluegreen-p95
- metricName: bluegreen-p95-ratio
- metricName: stable-samples
- metricName: stable-error-rate
- metricName: stable-p95
{{- if ne .Values.rollout.analysis.operationRouteRegex "" }}
- metricName: bluegreen-operation-success-rate
{{- end }}
{{- end -}}

{{/*
Canary step analysis block. dryRun keeps every metric non-blocking while the
HTTP metrics and scrape job are being validated.
*/}}
{{- define "goService.stepAnalysis" -}}
- analysis:
    templates:
      - templateName: {{ include "goService.analysisTemplateName" . }}
    args:
      {{- include "goService.analysisArgs" . | nindent 6 }}
{{- if .Values.rollout.analysis.dryRun }}
    dryRun:
      {{- include "goService.canaryDryRunMetrics" . | nindent 6 }}
{{- end }}
{{- end -}}

{{/*
Canary steps derived from the release profile: critical-canary advances
1 -> 5 -> 20 -> 50 -> 100 with a 5 minute initial wait per stage,
standard-canary advances 20 -> 50 -> 100 with a 3 minute wait.
*/}}
{{- define "goService.rolloutSteps" -}}
{{- $profile := include "goService.profileName" . -}}
{{- $steps := index .Values.rollout.profiles $profile | default dict -}}
{{- if not $steps.steps -}}
{{- $steps = index .Values.rollout.profiles "standard-canary" -}}
{{- end -}}
{{- range $step := $steps.steps }}
- setWeight: {{ $step.setWeight }}
{{- if ne $step.pause "0s" }}
- pause:
    duration: {{ $step.pause | quote }}
{{ include "goService.stepAnalysis" $ }}
{{- end }}
{{- end -}}
{{- end -}}

{{/*
BlueGreen pre-promotion analysis: a Job provider curls the Preview Service
health, dependency and read-only business endpoints. Authenticated probes read
their test-account token from the rollout-probe-auth Secret.
*/}}
{{- define "goService.bluegreenPrePromotionAnalysis" -}}
prePromotionAnalysis:
  templates:
    - templateName: {{ include "goService.bluegreenPreviewTemplateName" . }}
{{- if .Values.rollout.analysis.dryRun }}
  dryRun:
    {{- include "goService.bluegreenDryRunMetrics" . | nindent 4 }}
{{- end }}
{{- end -}}

{{/*
BlueGreen post-promotion analysis runs for the configured duration after the
active service switches, using the same application SLI queries.
*/}}
{{- define "goService.bluegreenPostPromotionAnalysis" -}}
postPromotionAnalysis:
  templates:
    - templateName: {{ include "goService.bluegreenPostTemplateName" . }}
  args:
    {{- include "goService.analysisArgs" . | nindent 4 }}
{{- if .Values.rollout.analysis.dryRun }}
  dryRun:
    {{- include "goService.bluegreenDryRunMetrics" . | nindent 4 }}
{{- end }}
{{- end -}}
