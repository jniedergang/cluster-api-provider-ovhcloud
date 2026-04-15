{{/*
Chart name (sanitized, max 63 chars)
*/}}
{{- define "capiovh.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully qualified app name (release-chart)
*/}}
{{- define "capiovh.fullname" -}}
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
Chart label
*/}}
{{- define "capiovh.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "capiovh.labels" -}}
helm.sh/chart: {{ include "capiovh.chart" . }}
{{ include "capiovh.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
cluster.x-k8s.io/provider: infrastructure-ovhcloud
cluster.x-k8s.io/v1beta1: v1alpha2
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "capiovh.selectorLabels" -}}
app.kubernetes.io/name: {{ include "capiovh.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Service account name
*/}}
{{- define "capiovh.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "capiovh.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Image (computes repository:tag with appVersion default)
*/}}
{{- define "capiovh.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
