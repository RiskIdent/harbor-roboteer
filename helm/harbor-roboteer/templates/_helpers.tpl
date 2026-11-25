{{/*
Expand the name of the chart.
*/}}
{{- define "harbor-roboteer.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "harbor-roboteer.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "harbor-roboteer.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "harbor-roboteer.labels" -}}
helm.sh/chart: {{ include "harbor-roboteer.chart" . }}
{{ include "harbor-roboteer.selectorLabels" . }}
{{- with .Chart.AppVersion }}
app.kubernetes.io/version: {{ regexReplaceAll "@.*$" . "" | trunc 63 | trimSuffix "-" | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "harbor-roboteer.selectorLabels" -}}
app.kubernetes.io/name: {{ include "harbor-roboteer.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "harbor-roboteer.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "harbor-roboteer.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the cleanup CronJob and its NetworkPolicy. CronJob names are limited to 52 chars.
*/}}
{{- define "harbor-roboteer.cleanupFullname" -}}
{{- include "harbor-roboteer.fullname" . | trunc 44 | trimSuffix "-" }}-cleanup
{{- end }}

{{/*
Cleanup pod selector labels. Deliberately not matching "harbor-roboteer.selectorLabels",
so the server Service and NetworkPolicy don't select cleanup pods.
*/}}
{{- define "harbor-roboteer.cleanupSelectorLabels" -}}
app.kubernetes.io/name: {{ include "harbor-roboteer.name" . }}-cleanup
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: cleanup
{{- end }}

{{/*
Container env, shared by the server and the cleanup CronJob.
*/}}
{{- define "harbor-roboteer.env" -}}
{{- if and .Values.tracing.enabled (eq .Values.tracing.exporter "otlp") }}
- name: OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
  value: "{{ .Values.tracing.otlp.endpoint }}"
- name: OTEL_EXPORTER_OTLP_TRACES_PROTOCOL
  value: "{{ .Values.tracing.otlp.protocol }}"
- name: OTEL_EXPORTER_OTLP_TRACES_COMPRESSION
  value: "{{ .Values.tracing.otlp.compression | default "none" }}"
{{- end }}
{{- with .Values.env }}
{{ toYaml . }}
{{- end }}
{{- end }}
