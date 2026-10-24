{{/* The key tool's account, role, binding and job; the keys never leave the cluster. */}}
{{/* Takes root, name, args and legacy (adopt reads the shared secret). */}}
{{- define "opengate.authority.job" -}}
{{- $root := .root -}}
{{- $name := printf "%s-authority-%s" (include "opengate.fullname" $root) .name | trunc 63 | trimSuffix "-" -}}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ $name }}
  labels:
    {{- include "opengate.labels" $root | nindent 4 }}
    app.kubernetes.io/component: authority
automountServiceAccountToken: false
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ $name }}
  labels:
    {{- include "opengate.labels" $root | nindent 4 }}
    app.kubernetes.io/component: authority
rules:
  # Kubernetes cannot hold create to one name, so create stands alone and reads nothing.
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["create"]
  - apiGroups: [""]
    resources: ["secrets"]
    resourceNames: [{{ $root.Values.authority.secretName | quote }}]
    verbs: ["get", "update"]
  {{- if .legacy }}
  - apiGroups: [""]
    resources: ["secrets"]
    resourceNames: [{{ $root.Values.server.existingSecret | quote }}]
    verbs: ["patch"]
  {{- end }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ $name }}
  labels:
    {{- include "opengate.labels" $root | nindent 4 }}
    app.kubernetes.io/component: authority
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: {{ $name }}
subjects:
  - kind: ServiceAccount
    name: {{ $name }}
    namespace: {{ $root.Release.Namespace }}
---
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ $name }}
  labels:
    {{- include "opengate.labels" $root | nindent 4 }}
    app.kubernetes.io/component: authority
spec:
  backoffLimit: 0
  activeDeadlineSeconds: 120
  template:
    metadata:
      labels:
        {{- include "opengate.selectorLabels" $root | nindent 8 }}
        app.kubernetes.io/component: authority
    spec:
      restartPolicy: Never
      serviceAccountName: {{ $name }}
      # The tool reads and writes its secret through the API, as this account alone.
      automountServiceAccountToken: true
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
        fsGroup: 65532
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: authority
          image: "{{ $root.Values.image.repository }}:{{ $root.Values.image.tag }}"
          imagePullPolicy: {{ $root.Values.image.pullPolicy }}
          command: ["opengate-authority"]
          args:
            {{- toYaml .args | nindent 12 }}
          resources:
            {{- toYaml $root.Values.authority.resources | nindent 12 }}
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          {{- if .legacy }}
          volumeMounts:
            - name: legacy
              mountPath: /legacy
              readOnly: true
          {{- end }}
      {{- if .legacy }}
      volumes:
        # The shared secret's master key, present until the adoption removes it from there. Its
        # files belong to root, so the tool's user reads them through the pod's file group.
        - name: legacy
          secret:
            secretName: {{ $root.Values.server.existingSecret }}
            optional: true
            defaultMode: 0440
            items:
              - key: ca.crt
                path: ca.crt
              - key: ca.key
                path: ca.key
      {{- end }}
{{- end -}}
