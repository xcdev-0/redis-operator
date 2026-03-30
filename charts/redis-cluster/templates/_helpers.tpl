{{/*
Expand the chart name.
*/}}
{{- define "redis-cluster.chartName" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
RedisCluster resource name.
*/}}
{{- define "redis-cluster.resourceName" -}}
{{- default .Release.Name .Values.redisCluster.name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Namespace for rendered resources.
*/}}
{{- define "redis-cluster.namespace" -}}
{{- default .Release.Namespace .Values.redisCluster.namespace -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "redis-cluster.labels" -}}
app.kubernetes.io/name: {{ include "redis-cluster.chartName" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}
