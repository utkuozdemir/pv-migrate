{{- /*
The tool looks its resources up by name, and the names it uses are derived from
the release name, which it always sets. So the chart names everything from the
release name and nothing else: no name overrides, since an override would rename
the objects without the tool knowing.
*/ -}}
{{- define "pv-migrate.fullname" -}}
{{- .Release.Name }}
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
app.kubernetes.io/name: {{ .Chart.Name }}
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

{{- define "pv-migrate.rclone.serviceAccountName" -}}
{{- if .Values.rclone.serviceAccount.create }}
{{- default (printf "%s-%s" (include "pv-migrate.fullname" .) "rclone") .Values.rclone.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.rclone.serviceAccount.name }}
{{- end }}
{{- end }}
