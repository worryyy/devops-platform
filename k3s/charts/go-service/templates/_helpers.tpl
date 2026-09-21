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
{{- printf "%s-%s" .Release.Name $name -}}
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

{{- define "goService.ingressName" -}}
{{- include "goService.fullname" . -}}
{{- end -}}

{{- define "goService.catalogServiceName" -}}
{{- default (include "goService.fullname" .) .Values.platform.serviceName -}}
{{- end -}}
