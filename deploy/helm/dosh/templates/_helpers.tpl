{{- define "dosh.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "dosh.fullname" -}}
{{- if .Values.nameOverride -}}
{{- .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end }}

{{- define "dosh.labels" -}}
app.kubernetes.io/name: {{ include "dosh.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end }}

{{- define "dosh.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dosh.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "dosh.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "dosh.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end }}

{{- define "dosh.tlsScheme" -}}
{{- if .Values.tls.enabled -}}HTTPS{{- else -}}HTTP{{- end -}}
{{- end }}

{{- define "dosh.authMode" -}}
{{- $mode := default "local" .Values.auth.mode -}}
{{- if not (has $mode (list "local" "oidc" "token")) -}}
{{- fail (printf "auth.mode must be local, oidc or token, got %q" $mode) -}}
{{- end -}}
{{- $mode -}}
{{- end }}
