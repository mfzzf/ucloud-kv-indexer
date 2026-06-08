{{- define "ucloud-kv-mongodb.chart" -}}
{{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "ucloud-kv-mongodb.name" -}}
{{- default "mongodb" .Values.statefulset.name -}}
{{- end -}}

{{- define "ucloud-kv-mongodb.labels" -}}
helm.sh/chart: {{ include "ucloud-kv-mongodb.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: ucloud-kv-indexer
app.kubernetes.io/name: mongodb
app.kubernetes.io/instance: {{ .Values.region | quote }}
{{- end -}}

{{- define "ucloud-kv-mongodb.selectorLabels" -}}
app.kubernetes.io/name: mongodb
app.kubernetes.io/instance: {{ .Values.region | quote }}
{{- end -}}
