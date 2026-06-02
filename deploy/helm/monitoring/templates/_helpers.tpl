{{/* Naming + label helpers for the monitoring chart. */}}

{{- define "mon.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "mon.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "mon.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/part-of: opengate-monitoring
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/* Per-component name + selector labels. Pass a list: (list $ "component"). */}}
{{- define "mon.componentName" -}}
{{- $root := index . 0 -}}{{- $c := index . 1 -}}
{{- printf "%s-%s" (include "mon.fullname" $root) $c | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "mon.selectorLabels" -}}
{{- $root := index . 0 -}}{{- $c := index . 1 -}}
app.kubernetes.io/name: {{ include "mon.name" $root }}
app.kubernetes.io/instance: {{ $root.Release.Name }}
app.kubernetes.io/component: {{ $c }}
{{- end -}}
