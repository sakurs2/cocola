{{- define "cocola-sandbox.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "cocola-sandbox.imageRef" -}}
{{- if .digest -}}
{{- printf "%s@%s" .image .digest -}}
{{- else -}}
{{- .image -}}
{{- end -}}
{{- end -}}
