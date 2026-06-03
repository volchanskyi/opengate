{{/* Common naming + label helpers. */}}

{{- define "opengate.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opengate.fullname" -}}
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

{{- define "opengate.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "opengate.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end -}}

{{- define "opengate.selectorLabels" -}}
app.kubernetes.io/name: {{ include "opengate.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "opengate.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "opengate.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "opengate.postgres.fullname" -}}
{{- printf "%s-postgres" (include "opengate.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opengate.redis.fullname" -}}
{{- printf "%s-redis" (include "opengate.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "opengate.redisSentinel.fullname" -}}
{{- printf "%s-redis-sentinel" (include "opengate.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
opengate.redis.masterFQDN — stable in-cluster DNS of the bootstrap master
(redis pod-0) via the headless Redis service. Sentinel monitors this name and
nodes fall back to it before Sentinel knows a master.
*/}}
{{- define "opengate.redis.masterFQDN" -}}
{{- printf "%s-0.%s.%s.svc.cluster.local" (include "opengate.redis.fullname" .) (include "opengate.redis.fullname" .) .Release.Namespace -}}
{{- end -}}
