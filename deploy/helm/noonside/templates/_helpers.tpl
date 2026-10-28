{{- define "noonside.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 50 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 50 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "noonside.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "noonside.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Images; the tag defaults to the chart's appVersion (the commit CI built). */}}
{{- define "noonside.apiImage" -}}
{{- printf "%s:%s" .Values.api.image.repository (default .Chart.AppVersion .Values.api.image.tag) -}}
{{- end -}}
{{- define "noonside.webImage" -}}
{{- printf "%s:%s" .Values.web.image.repository (default .Chart.AppVersion .Values.web.image.tag) -}}
{{- end -}}

{{/* The public origin: publicUrl, else https://<host>. */}}
{{- define "noonside.origin" -}}
{{- default (printf "https://%s" (required "host is required" .Values.host)) .Values.publicUrl | trimSuffix "/" -}}
{{- end -}}

{{/* Service names. */}}
{{- define "noonside.apiName" -}}{{ include "noonside.fullname" . }}-api{{- end -}}
{{- define "noonside.webName" -}}{{ include "noonside.fullname" . }}-web{{- end -}}
{{- define "noonside.authName" -}}{{ include "noonside.fullname" . }}-auth{{- end -}}
{{- define "noonside.sftpgoName" -}}{{ include "noonside.fullname" . }}-sftpgo{{- end -}}
{{- define "noonside.pgName" -}}{{ include "noonside.fullname" . }}-pg{{- end -}}

{{/* Database connection: host, port, database, sslmode. */}}
{{- define "noonside.dbHost" -}}
{{- if .Values.postgres.enabled -}}{{ include "noonside.pgName" . }}-rw{{- else -}}{{ required "externalDatabase.host is required" .Values.externalDatabase.host }}{{- end -}}
{{- end -}}
{{- define "noonside.dbPort" -}}
{{- if .Values.postgres.enabled -}}5432{{- else -}}{{ .Values.externalDatabase.port }}{{- end -}}
{{- end -}}
{{- define "noonside.dbName" -}}
{{- if .Values.postgres.enabled -}}{{ .Values.postgres.database }}{{- else -}}{{ .Values.externalDatabase.database }}{{- end -}}
{{- end -}}
{{- define "noonside.dbSSLMode" -}}
{{- if .Values.postgres.enabled -}}require{{- else -}}{{ .Values.externalDatabase.sslmode }}{{- end -}}
{{- end -}}
{{/* Secret (username/password) the app connects with: CNPG's superuser secret. */}}
{{- define "noonside.dbSecret" -}}
{{- if .Values.postgres.enabled -}}{{ include "noonside.pgName" . }}-superuser{{- else -}}{{ required "externalDatabase.secretName is required" .Values.externalDatabase.secretName }}{{- end -}}
{{- end -}}
{{- define "noonside.authDbSecret" -}}
{{- if .Values.postgres.enabled -}}{{ .Values.postgres.authRoleSecret }}{{- else -}}{{ required "externalDatabase.authSecretName is required" .Values.externalDatabase.authSecretName }}{{- end -}}
{{- end -}}

{{/* The app's database env: libpq variables fill in what DATABASE_URL leaves out. */}}
{{- define "noonside.dbEnv" -}}
- name: DATABASE_URL
  value: {{ printf "host=%s port=%s dbname=%s sslmode=%s" (include "noonside.dbHost" .) (include "noonside.dbPort" .) (include "noonside.dbName" .) (include "noonside.dbSSLMode" .) | quote }}
- name: PGUSER
  valueFrom:
    secretKeyRef:
      name: {{ include "noonside.dbSecret" . }}
      key: username
- name: PGPASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "noonside.dbSecret" . }}
      key: password
{{- end -}}

{{- define "noonside.securityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: ["ALL"]
{{- end -}}

{{/* The API's hosted FTP settings (SFTPGo, templates/sftpgo.yaml). */}}
{{- define "noonside.hostedFtpEnv" -}}
{{- $ftp := .Values.hostedFtp -}}
{{- $name := include "noonside.sftpgoName" . -}}
- name: HOSTED_FTP_API_URL
  value: {{ printf "http://%s:8080" $name | quote }}
- name: HOSTED_FTP_ADMIN_USER
  value: {{ $ftp.admin.username | quote }}
- name: SFTPGO_ADMIN_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ required "hostedFtp.admin.existingSecret is required" $ftp.admin.existingSecret }}
      key: {{ $ftp.admin.passwordKey }}
- name: HOSTED_FTP_SFTP_ADDR
  value: {{ printf "%s:2022" $name | quote }}
- name: HOSTED_FTP_PUBLIC_HOST
  value: {{ required "hostedFtp.publicHost is required" $ftp.publicHost | quote }}
- name: HOSTED_FTP_PUBLIC_PORT
  value: {{ $ftp.ftp.port | quote }}
- name: HOSTED_FTP_HOOK_URL
  value: {{ printf "http://%s:8080/api/v1/hooks/hosted-ftp" (include "noonside.apiName" .) | quote }}
- name: HOSTED_FTP_QUOTA_MB
  value: {{ $ftp.quotaMB | quote }}
- name: HOSTED_FTP_RETENTION_DAYS
  value: {{ $ftp.retentionDays | quote }}
- name: HOSTED_FTP_MAX_ACCOUNTS
  value: {{ $ftp.maxAccounts | quote }}
{{- end -}}
