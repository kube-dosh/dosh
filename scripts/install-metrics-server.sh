#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/root/.local/bin:$PATH

NAMESPACE=${DOSH_METRICS_NAMESPACE:-dosh-metrics}
IMAGE=${DOSH_METRICS_IMAGE:-192.168.122.10:5000/metrics-server/metrics-server:v0.9.1}
INSECURE_TLS=${DOSH_METRICS_INSECURE_TLS:-true}

if [[ ${1:-} == "uninstall" ]]; then
  kubectl delete apiservice v1beta1.metrics.k8s.io --ignore-not-found
  kubectl delete namespace "$NAMESPACE" --ignore-not-found
  exit 0
fi

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

for command in kubectl; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done

log "Installing Metrics Server ${IMAGE} in ${NAMESPACE}"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: metrics-server
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: metrics-server
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: dosh-metrics-server
  labels:
    app.kubernetes.io/name: metrics-server
rules:
  - apiGroups: [""]
    resources: ["nodes/metrics"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["pods", "nodes"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["metrics.k8s.io"]
    resources: ["pods", "nodes"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["authentication.k8s.io"]
    resources: ["tokenreviews"]
    verbs: ["create"]
  - apiGroups: ["authorization.k8s.io"]
    resources: ["subjectaccessreviews"]
    verbs: ["create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: dosh-metrics-server
  labels:
    app.kubernetes.io/name: metrics-server
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: dosh-metrics-server
subjects:
  - kind: ServiceAccount
    name: metrics-server
    namespace: ${NAMESPACE}
---
apiVersion: v1
kind: Service
metadata:
  name: metrics-server
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: metrics-server
spec:
  selector:
    app.kubernetes.io/name: metrics-server
  ports:
    - name: https
      port: 443
      targetPort: https
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: metrics-server
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: metrics-server
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: metrics-server
  template:
    metadata:
      labels:
        app.kubernetes.io/name: metrics-server
    spec:
      serviceAccountName: metrics-server
      containers:
        - name: metrics-server
          image: ${IMAGE}
          imagePullPolicy: IfNotPresent
          args:
            - --cert-dir=/tmp
            - --secure-port=4443
            - --kubelet-preferred-address-types=InternalIP,Hostname
            - --kubelet-use-node-status-port
            - --metric-resolution=15s
            - --kubelet-insecure-tls=${INSECURE_TLS}
          ports:
            - name: https
              containerPort: 4443
              protocol: TCP
          livenessProbe:
            httpGet:
              path: /livez
              port: https
              scheme: HTTPS
            initialDelaySeconds: 10
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /readyz
              port: https
              scheme: HTTPS
            initialDelaySeconds: 5
            periodSeconds: 5
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 250m
              memory: 256Mi
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop: ["ALL"]
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 1000
          volumeMounts:
            - name: tmp
              mountPath: /tmp
      volumes:
        - name: tmp
          emptyDir:
            sizeLimit: 32Mi
---
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1beta1.metrics.k8s.io
  labels:
    app.kubernetes.io/name: metrics-server
spec:
  service:
    name: metrics-server
    namespace: ${NAMESPACE}
    port: 443
  group: metrics.k8s.io
  version: v1beta1
  insecureSkipTLSVerify: true
  groupPriorityMinimum: 100
  versionPriority: 100
EOF
 kubectl -n "$NAMESPACE" rollout restart deployment/metrics-server >/dev/null
 kubectl -n "$NAMESPACE" rollout status deployment/metrics-server --timeout=120s >/dev/null

log "Waiting for Metrics API"
for _ in $(seq 1 120); do
  if kubectl get --raw /apis/metrics.k8s.io/v1beta1 >/dev/null 2>&1; then
    if kubectl top nodes >/dev/null 2>&1; then
      kubectl top nodes
      kubectl top pods -n "$NAMESPACE" 2>/dev/null || true
      printf '\nMetrics Server installation: pass\n'
      exit 0
    fi
  fi
  sleep 2
done

kubectl -n "$NAMESPACE" get deploy,pod,svc -o wide >&2 || true
kubectl -n "$NAMESPACE" logs deployment/metrics-server --tail=100 >&2 || true
die "Metrics Server did not become available"
