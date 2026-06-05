{{- define "ucloud-kv-gateway.chart" -}}
{{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "ucloud-kv-gateway.namespace" -}}
{{- default .Release.Namespace .Values.namespace.name -}}
{{- end -}}

{{- define "ucloud-kv-gateway.name" -}}
{{- default "kvgateway" .Values.gateway.name -}}
{{- end -}}

{{- define "ucloud-kv-gateway.secretName" -}}
{{- default "kvgateway-secrets" .Values.secrets.name -}}
{{- end -}}

{{- define "ucloud-kv-gateway.bootstrapName" -}}
{{- default "kvgateway-bootstrap" .Values.bootstrap.configMapName -}}
{{- end -}}

{{- define "ucloud-kv-gateway.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag }}
{{- end -}}

{{- define "ucloud-kv-gateway.labels" -}}
helm.sh/chart: {{ include "ucloud-kv-gateway.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: ucloud-kv-indexer
app.kubernetes.io/name: kvgateway
{{- end -}}
