{{- define "pv-migrate.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "pv-migrate.fullname" -}}
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

{{- define "pv-migrate.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "pv-migrate.labels" -}}
helm.sh/chart: {{ include "pv-migrate.chart" . }}
{{ include "pv-migrate.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "pv-migrate.selectorLabels" -}}
app.kubernetes.io/name: {{ include "pv-migrate.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "pv-migrate.sshd.serviceAccountName" -}}
{{- if .Values.sshd.serviceAccount.create }}
{{- default (printf "%s-%s" (include "pv-migrate.fullname" .) "sshd") .Values.sshd.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.sshd.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "pv-migrate.rsync.serviceAccountName" -}}
{{- if .Values.rsync.serviceAccount.create }}
{{- default (printf "%s-%s" (include "pv-migrate.fullname" .) "rsync") .Values.rsync.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.rsync.serviceAccount.name }}
{{- end }}
{{- end }}
