{{- define "dns-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dns-operator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "dns-operator.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "dns-operator.labels" -}}
app.kubernetes.io/name: {{ include "dns-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "dns-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dns-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "dns-operator.managerServiceAccountName" -}}
{{- if .Values.serviceAccount.name -}}
{{- .Values.serviceAccount.name -}}
{{- else -}}
{{- printf "%s-controller-manager" (include "dns-operator.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "dns-operator.corednsServiceAccountName" -}}
{{- printf "%s-coredns" (include "dns-operator.fullname" .) -}}
{{- end -}}

{{- define "dns-operator.caddyServiceAccountName" -}}
{{- printf "%s-caddy" (include "dns-operator.fullname" .) -}}
{{- end -}}

{{- define "dns-operator.zoneConfigMapName" -}}
{{- printf "zone-%s" (replace "." "-" .Values.operator.authoritativeZone) -}}
{{- end -}}

{{- define "dns-operator.zoneConfigMapKey" -}}
{{- printf "db.%s" .Values.operator.authoritativeZone -}}
{{- end -}}

{{- define "dns-operator.publishZonesArg" -}}
{{- join "," .Values.operator.publishZones -}}
{{- end -}}

{{- define "dns-operator.authoritativeServiceName" -}}
{{- printf "%s-authoritative-dns" (include "dns-operator.fullname" .) -}}
{{- end -}}

{{- define "dns-operator.corednsCorefileConfigMapName" -}}
{{- printf "%s-coredns-corefile" (include "dns-operator.fullname" .) -}}
{{- end -}}

{{- define "dns-operator.corednsEmptyZoneConfigMapName" -}}
{{- printf "%s-coredns-empty-zone" (include "dns-operator.fullname" .) -}}
{{- end -}}

{{- define "dns-operator.caddyBootstrapConfigMapName" -}}
{{- printf "%s-caddy-bootstrap" (include "dns-operator.fullname" .) -}}
{{- end -}}

{{- define "dns-operator.caddyServiceName" -}}
{{- printf "%s-caddy" (include "dns-operator.fullname" .) -}}
{{- end -}}

{{- define "dns-operator.bootstrapEndpointServiceName" -}}
{{- if .Values.bootstrap.tailnetDNSEndpoint.serviceName -}}
{{- .Values.bootstrap.tailnetDNSEndpoint.serviceName -}}
{{- else -}}
{{- include "dns-operator.authoritativeServiceName" . -}}
{{- end -}}
{{- end -}}

{{- define "dns-operator.tailscaleAuth" -}}
{{- $auth := .Values.secrets.tailscaleAdmin -}}
{{- $mode := default "apiToken" $auth.mode -}}
{{- $name := required "secrets.tailscaleAdmin.name is required" $auth.name -}}
{{- if eq $mode "apiToken" -}}
secretRef:
  name: {{ $name | quote }}
  key: {{ required "secrets.tailscaleAdmin.key is required when mode is apiToken" $auth.key | quote }}
{{- else if eq $mode "oauthClientCredentials" -}}
oauthClientCredentials:
  clientIDSecretRef:
    name: {{ $name | quote }}
    key: {{ required "secrets.tailscaleAdmin.clientIDKey is required when mode is oauthClientCredentials" $auth.clientIDKey | quote }}
  clientSecretSecretRef:
    name: {{ $name | quote }}
    key: {{ required "secrets.tailscaleAdmin.clientSecretKey is required when mode is oauthClientCredentials" $auth.clientSecretKey | quote }}
{{- with $auth.scopes }}
  scopes:
{{- toYaml . | nindent 4 }}
{{- end }}
{{- else }}
{{- fail "secrets.tailscaleAdmin.mode must be one of apiToken or oauthClientCredentials" }}
{{- end -}}
{{- end -}}
