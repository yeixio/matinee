{{- define "matinee.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "matinee.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "matinee.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: server
{{- end -}}

{{- define "matinee.postgresql.fullname" -}}
{{- printf "%s-postgresql" (include "matinee.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* The secret holding database-url (and the bundled database's password). */}}
{{- define "matinee.databaseSecret" -}}
{{- if .Values.database.existingSecret -}}
{{- .Values.database.existingSecret -}}
{{- else -}}
{{- printf "%s-db" (include "matinee.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/* True when an install hook generates the bundled database's credentials. */}}
{{- define "matinee.generateDBSecret" -}}
{{- if and .Values.postgresql.enabled (not .Values.database.existingSecret) (not .Values.postgresql.auth.password) -}}
true
{{- end -}}
{{- end -}}

{{/* The address the apps connect to: discovery.url, or the chart's own ingress. */}}
{{- define "matinee.publicURL" -}}
{{- if .Values.discovery.url -}}
{{- .Values.discovery.url -}}
{{- else if and .Values.ingress.enabled .Values.ingress.hosts -}}
{{- printf "%s://%s" (ternary "https" "http" (not (empty .Values.ingress.tls))) (first .Values.ingress.hosts).host -}}
{{- end -}}
{{- end -}}

{{/* Seconds to keep serving on shutdown before stopping (see shutdown in values.yaml). */}}
{{- define "matinee.drainDelay" -}}
{{- if ne (toString .Values.shutdown.drainDelaySeconds) "" -}}
{{- .Values.shutdown.drainDelaySeconds -}}
{{- else if eq .Values.strategy.type "RollingUpdate" -}}
5
{{- else -}}
0
{{- end -}}
{{- end -}}
