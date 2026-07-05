{{- define "baasparse.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "baasparse.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "baasparse.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "baasparse.labels" -}}
app.kubernetes.io/name: {{ include "baasparse.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "baasparse.selectorLabels" -}}
app.kubernetes.io/name: {{ include "baasparse.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "baasparse.secretName" -}}
{{- if .Values.database.existingSecret -}}{{ .Values.database.existingSecret }}{{- else -}}{{ include "baasparse.fullname" . }}{{- end -}}
{{- end -}}

{{/* Resolved S3 endpoint: explicit value, else the bundled MinIO service. */}}
{{- define "baasparse.s3endpoint" -}}
{{- if .Values.storage.s3.endpoint -}}{{ .Values.storage.s3.endpoint }}{{- else -}}{{ include "baasparse.fullname" . }}-minio:9000{{- end -}}
{{- end -}}

{{/* Resolved OTLP endpoint: explicit value, else the bundled collector. */}}
{{- define "baasparse.otelEndpoint" -}}
{{- if .Values.otel.endpoint -}}{{ .Values.otel.endpoint }}{{- else if .Values.otelCollector.enabled -}}http://{{ include "baasparse.fullname" . }}-otel-collector:4318{{- else -}}{{- end -}}
{{- end -}}
