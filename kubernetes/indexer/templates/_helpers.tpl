{{- define "ucloud-kv-indexer.chart" -}}
{{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "ucloud-kv-indexer.namespace" -}}
ucloud-kv-indexers
{{- end -}}

{{- define "ucloud-kv-indexer.secretName" -}}
{{- default "kvindexer-secrets" .Values.secrets.name -}}
{{- end -}}

{{- define "ucloud-kv-indexer.bootstrapName" -}}
{{- default "kvindexer-bootstrap" .Values.bootstrap.configMapName -}}
{{- end -}}

{{- define "ucloud-kv-indexer.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag }}
{{- end -}}

{{- define "ucloud-kv-indexer.commonLabels" -}}
helm.sh/chart: {{ include "ucloud-kv-indexer.chart" .root }}
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
app.kubernetes.io/part-of: ucloud-kv-indexer
app.kubernetes.io/name: kvindexer
app.kubernetes.io/instance: {{ .indexer.clusterID | quote }}
app.kubernetes.io/region: {{ .indexer.region | quote }}
{{- end -}}
