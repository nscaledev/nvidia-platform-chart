{{/*
Chart name.
*/}}
{{- define "nvidia-platform.name" -}}
{{- .Chart.Name }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "nvidia-platform.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: {{ .Chart.Name }}
{{- end }}
