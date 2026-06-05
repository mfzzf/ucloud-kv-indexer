{{- define "ucloud-kv-web.chart" -}}
{{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "ucloud-kv-web.namespace" -}}
{{- default .Release.Namespace .Values.namespace.name -}}
{{- end -}}

{{- define "ucloud-kv-web.name" -}}
{{- default "ucloud-kv-web" .Values.web.name -}}
{{- end -}}

{{- define "ucloud-kv-web.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag }}
{{- end -}}

{{- define "ucloud-kv-web.labels" -}}
helm.sh/chart: {{ include "ucloud-kv-web.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: ucloud-kv-indexer
app.kubernetes.io/name: ucloud-kv-web
{{- end -}}
