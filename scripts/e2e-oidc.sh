#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/root/.local/bin:$PATH

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
NAMESPACE=${DOSH_E2E_NAMESPACE:-dosh-e2e-oidc}
TARGET_NAMESPACE=${DOSH_E2E_TARGET_NAMESPACE:-dosh-e2e-target}
RELEASE=${DOSH_E2E_RELEASE:-dosh-oidc-e2e}
NODE_IP=${DOSH_E2E_NODE_IP:-192.168.122.100}
NODE_PORT=${DOSH_E2E_NODE_PORT:-32627}
IDP_HOST=${DOSH_E2E_IDP_HOST:-192.168.122.1}
IDP_PORT=${DOSH_E2E_IDP_PORT:-9443}
AUDIT_ENABLED=${DOSH_E2E_AUDIT_ENABLED:-0}
AUDIT_HOST=${DOSH_E2E_AUDIT_HOST:-192.168.122.1}
AUDIT_PORT=${DOSH_E2E_AUDIT_PORT:-9444}
AUDIT_CA_FILE=${DOSH_E2E_AUDIT_CA_FILE:-}
AUDIT_TOKEN=${DOSH_E2E_AUDIT_TOKEN:-dosh-e2e-audit-token}
AUDIT_HMAC_KEY=${DOSH_E2E_AUDIT_HMAC_KEY:-dosh-e2e-audit-hmac}
AUDIT_TIMEOUT=${DOSH_E2E_AUDIT_TIMEOUT:-1s}
AUDIT_QUEUE_SIZE=${DOSH_E2E_AUDIT_QUEUE_SIZE:-1024}
OIDC_STATE_TTL_MS=${DOSH_E2E_OIDC_STATE_TTL_MS:-5000}
OIDC_STATE_TTL="${OIDC_STATE_TTL_MS}ms"
RBAC_SUFFIX=${DOSH_E2E_RBAC_SUFFIX:-}
RBAC_NAME=${DOSH_E2E_RBAC_NAME:-dosh-oidc-e2e}
CORDON_NODE=${DOSH_E2E_CORDON_NODE:-worker-0005}
IMAGE_REPOSITORY=${DOSH_E2E_IMAGE_REPOSITORY:-192.168.122.10:5000/dosh/dosh}
IMAGE_TAG=${DOSH_E2E_IMAGE_TAG:-0.1.4-oidc-e2e-$(date +%s)}
IMAGE_DIGEST=${DOSH_E2E_IMAGE_DIGEST:-}
SKIP_BUILD=${DOSH_E2E_SKIP_BUILD:-0}
HOLD=${DOSH_E2E_HOLD:-0}
STATE_FILE=${DOSH_E2E_STATE_FILE:-}
WORKDIR=$(mktemp -d)
IDP_PID=""
CORDON_NODE_ORIGINAL=""

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
cleanup() {
  if [[ -n "$CORDON_NODE_ORIGINAL" ]] && kubectl get node "$CORDON_NODE" >/dev/null 2>&1; then
    current=$(kubectl get node "$CORDON_NODE" -o jsonpath='{.spec.unschedulable}' 2>/dev/null || true)
    current=${current:-false}
    if [[ "$current" != "$CORDON_NODE_ORIGINAL" ]]; then
      original=${CORDON_NODE_ORIGINAL:-false}
      kubectl patch node "$CORDON_NODE" --type=merge -p "{\"spec\":{\"unschedulable\":${original}}}" >/dev/null 2>&1 || true
    fi
  fi
  if [[ -n "$IDP_PID" ]]; then
    kill "$IDP_PID" 2>/dev/null || true
    wait "$IDP_PID" 2>/dev/null || true
  fi
  if [[ ${DOSH_E2E_KEEP_WORKDIR:-0} != 1 ]]; then
    rm -rf "$WORKDIR"
  else
    printf 'E2E workdir retained: %s\n' "$WORKDIR"
  fi
}
trap cleanup EXIT

require() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

for command in kubectl helm podman curl openssl jq go make ss; do
  require "$command"
done

if [[ -n "$STATE_FILE" ]]; then
  mkdir -p "$(dirname "$STATE_FILE")"
fi

log "Checking cluster and port isolation"
kubectl version >/dev/null
if kubectl -n "$NAMESPACE" get service dosh >/dev/null 2>&1; then
  existing_port=$(kubectl -n "$NAMESPACE" get service dosh -o jsonpath='{.spec.ports[0].nodePort}')
  [[ "$existing_port" == "$NODE_PORT" ]] || die "existing test service uses unexpected NodePort $existing_port"
fi
current_dosh_port=$(kubectl -n dosh get service dosh -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null || true)
[[ "$current_dosh_port" != "$NODE_PORT" ]] || die "test NodePort collides with production dosh service"
kubectl get node "$CORDON_NODE" >/dev/null || die "Cordon test node ${CORDON_NODE} does not exist"
CORDON_NODE_ORIGINAL=$(kubectl get node "$CORDON_NODE" -o jsonpath='{.spec.unschedulable}' 2>/dev/null || true)
CORDON_NODE_ORIGINAL=${CORDON_NODE_ORIGINAL:-false}

log "Generating short-lived test certificates"
openssl req -x509 -newkey rsa:2048 -nodes -days 2 \
  -keyout "$WORKDIR/ca.key" -out "$WORKDIR/ca.crt" -subj '/CN=dosh-e2e-ca' >/dev/null 2>&1
cat >"$WORKDIR/idp.ext" <<EOF
subjectAltName=IP:${IDP_HOST}
extendedKeyUsage=serverAuth
EOF
openssl req -newkey rsa:2048 -nodes -keyout "$WORKDIR/idp.key" -out "$WORKDIR/idp.csr" \
  -subj "/CN=${IDP_HOST}" >/dev/null 2>&1
openssl x509 -req -days 2 -in "$WORKDIR/idp.csr" -CA "$WORKDIR/ca.crt" -CAkey "$WORKDIR/ca.key" \
  -CAcreateserial -out "$WORKDIR/idp.crt" -extfile "$WORKDIR/idp.ext" >/dev/null 2>&1
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -keyout "$WORKDIR/dosh.key" -out "$WORKDIR/dosh.crt" \
  -subj "/CN=${NODE_IP}" -addext "subjectAltName=IP:${NODE_IP}" >/dev/null 2>&1

if [[ "$SKIP_BUILD" == 1 ]]; then
  log "Reusing candidate image ${IMAGE_REPOSITORY}:${IMAGE_TAG}"
  podman image exists "${IMAGE_REPOSITORY}:${IMAGE_TAG}" || die "candidate image is not present locally"
else
  log "Building candidate image ${IMAGE_REPOSITORY}:${IMAGE_TAG}"
  make -C "$ROOT" build VERSION="$IMAGE_TAG" COMMIT=oidc-e2e
  podman build -t "${IMAGE_REPOSITORY}:${IMAGE_TAG}" -f "$ROOT/Dockerfile" "$ROOT"
  podman push --tls-verify=false "${IMAGE_REPOSITORY}:${IMAGE_TAG}" >/dev/null
fi

log "Starting signed test IdP at https://${IDP_HOST}:${IDP_PORT}"
if ss -H -ltn "sport = :${IDP_PORT}" | grep -q .; then
  die "test IdP port ${IDP_PORT} is already in use; stop the stale process before rerunning"
fi
go build -o "$WORKDIR/oidctestidp" "$ROOT/tools/oidctestidp" 2>/dev/null || \
  (cd "$ROOT" && go build -o "$WORKDIR/oidctestidp" ./tools/oidctestidp)
"$WORKDIR/oidctestidp" \
  -addr "0.0.0.0:${IDP_PORT}" -cert "$WORKDIR/idp.crt" -key "$WORKDIR/idp.key" \
  -issuer "https://${IDP_HOST}:${IDP_PORT}" -client-id dosh-e2e -client-secret dosh-e2e-secret \
  >"$WORKDIR/idp.log" 2>&1 &
IDP_PID=$!
idp_ready=0
for _ in $(seq 1 50); do
  if ! kill -0 "$IDP_PID" 2>/dev/null; then
    cat "$WORKDIR/idp.log" >&2
    die "test IdP exited before becoming healthy"
  fi
  if curl -sf --cacert "$WORKDIR/ca.crt" "https://${IDP_HOST}:${IDP_PORT}/healthz" >/dev/null 2>&1; then
    idp_ready=1
    break
  fi
  sleep 0.2
done
(( idp_ready == 1 )) || {
  cat "$WORKDIR/idp.log" >&2
  die "test IdP did not become healthy"
}
curl -sf --cacert "$WORKDIR/ca.crt" "https://${IDP_HOST}:${IDP_PORT}/.well-known/openid-configuration" >/dev/null || {
  cat "$WORKDIR/idp.log" >&2
  die "test IdP did not become healthy"
}

log "Creating isolated secrets, namespaces and RBAC"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl create namespace "$TARGET_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n "$TARGET_NAMESPACE" create secret generic e2e-secret \
  --from-literal=token=e2e-secret-value --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n "$NAMESPACE" create secret tls dosh-e2e-tls \
  --cert="$WORKDIR/dosh.crt" --key="$WORKDIR/dosh.key" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n "$NAMESPACE" create configmap dosh-e2e-oidc-ca \
  --from-file=ca.crt="$WORKDIR/ca.crt" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
session_key=$(openssl rand -base64 32)
kubectl -n "$NAMESPACE" create secret generic dosh-e2e-auth \
  --from-literal=session-key="$session_key" \
  --from-literal=client-secret=dosh-e2e-secret \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
if [[ "$AUDIT_ENABLED" == 1 ]]; then
  [[ -s "$AUDIT_CA_FILE" ]] || die "DOSH_E2E_AUDIT_CA_FILE is required when audit E2E is enabled"
  kubectl -n "$NAMESPACE" create secret generic dosh-e2e-audit \
    --from-literal=token="$AUDIT_TOKEN" \
    --from-literal=hmac-key="$AUDIT_HMAC_KEY" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "$NAMESPACE" create configmap dosh-e2e-audit-ca \
    --from-file=ca.crt="$AUDIT_CA_FILE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
fi

kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: dosh-e2e-viewer${RBAC_SUFFIX}
rules:
  - apiGroups: [""]
    resources: ["nodes", "namespaces", "pods", "events", "services", "configmaps", "persistentvolumes", "persistentvolumeclaims"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["list"]
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["batch"]
    resources: ["jobs", "cronjobs"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses", "ingressclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["discovery.k8s.io"]
    resources: ["endpointslices"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses"]
    verbs: ["get", "list", "watch"]
  - nonResourceURLs: ["/api", "/api/*", "/apis", "/apis/*", "/version", "/openapi/v2", "/openapi/v3", "/openapi/v3/*"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: dosh-e2e-operator-write${RBAC_SUFFIX}
  namespace: ${TARGET_NAMESPACE}
rules:
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets", "deployments/scale", "statefulsets/scale"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: dosh-e2e-viewers${RBAC_SUFFIX}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: dosh-e2e-viewer${RBAC_SUFFIX}
subjects:
  - kind: Group
    name: dosh-e2e-viewers
    apiGroup: rbac.authorization.k8s.io
  - kind: Group
    name: dosh-e2e-operators
    apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: dosh-e2e-operators${RBAC_SUFFIX}
  namespace: ${TARGET_NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: dosh-e2e-operator-write${RBAC_SUFFIX}
subjects:
  - kind: Group
    name: dosh-e2e-operators
    apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: dosh-e2e-admins${RBAC_SUFFIX}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: Group
    name: dosh-e2e-admins
    apiGroup: rbac.authorization.k8s.io
EOF

kubectl -n "$TARGET_NAMESPACE" apply -f - >/dev/null <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: e2e-demo
  labels: { app: e2e-demo }
spec:
  replicas: 0
  selector:
    matchLabels: { app: e2e-demo }
  template:
    metadata:
      labels: { app: e2e-demo }
    spec:
      containers:
        - name: demo
          image: 192.168.122.10:5000/dosh/dosh:0.1.2
          command: ["/usr/local/bin/dosh"]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: e2e-admin
  labels: { app: e2e-admin }
spec:
  replicas: 1
  selector:
    matchLabels: { app: e2e-admin }
  template:
    metadata:
      labels: { app: e2e-admin }
    spec:
      containers:
        - name: demo
          image: 192.168.122.10:5000/dosh/dosh:0.1.2
          command: ["/usr/local/bin/dosh"]
EOF

log "Installing OIDC candidate Helm release ${RELEASE}"
audit_args=()
if [[ "$AUDIT_ENABLED" == 1 ]]; then
  audit_args+=(
    --set audit.webhook.enabled=true
    --set-string audit.webhook.url="https://${AUDIT_HOST}:${AUDIT_PORT}/audit"
    --set audit.webhook.existingSecret=dosh-e2e-audit
    --set audit.webhook.tokenKey=token
    --set audit.webhook.hmacKeyKey=hmac-key
    --set audit.webhook.ca.existingConfigMap=dosh-e2e-audit-ca
    --set-string audit.webhook.timeout="$AUDIT_TIMEOUT"
    --set audit.webhook.queueSize="$AUDIT_QUEUE_SIZE"
    --set networkPolicy.webhook.ipBlocks[0]="${AUDIT_HOST}/32"
    --set networkPolicy.webhook.ports[0]="$AUDIT_PORT"
  )
fi
helm upgrade --install "$RELEASE" "$ROOT/deploy/helm/dosh" -n "$NAMESPACE" --create-namespace \
  --set image.repository="$IMAGE_REPOSITORY" \
  --set-string image.tag="$IMAGE_TAG" \
  ${IMAGE_DIGEST:+--set-string image.digest="$IMAGE_DIGEST"} \
  --set replicaCount=1 \
  --set antiAffinity.required=false \
  --set service.nodePort="$NODE_PORT" \
  --set service.port=32626 \
  --set containerPort=32626 \
  --set tls.existingSecret=dosh-e2e-tls \
  --set auth.mode=oidc \
  --set auth.existingSecret=dosh-e2e-auth \
  --set auth.sessionKeyKey=session-key \
  --set auth.oidc.existingSecret=dosh-e2e-auth \
  --set auth.oidc.clientSecretKey=client-secret \
  --set auth.oidc.clientID=dosh-e2e \
  --set-string auth.oidc.issuer="https://${IDP_HOST}:${IDP_PORT}" \
  --set-string auth.oidc.redirectURL="https://${NODE_IP}:${NODE_PORT}/api/v1/auth/oidc/callback" \
  --set auth.oidc.ca.existingConfigMap=dosh-e2e-oidc-ca \
  --set auth.oidc.adminGroups[0]=dosh-e2e-admins \
  --set-string auth.oidc.stateTTL="$OIDC_STATE_TTL" \
  --set networkPolicy.oidc.ipBlocks[0]="${IDP_HOST}/32" \
  --set networkPolicy.oidc.ports[0]="$IDP_PORT" \
  --set rbac.clusterRoleName="$RBAC_NAME" \
  --set serviceAccount.name="$RBAC_NAME" \
  "${audit_args[@]}" \
  --wait --timeout 3m >/dev/null

BASE="https://${NODE_IP}:${NODE_PORT}"
release_ready=0
for _ in $(seq 1 60); do
  if curl -skf "$BASE/healthz" >/dev/null 2>&1 && curl -skf "$BASE/readyz" >/dev/null 2>&1; then
    release_ready=1
    break
  fi
  sleep 0.5
done
(( release_ready == 1 )) || die "dosh OIDC test release is not ready"

login_as() {
  local user=$1
  local jar="$WORKDIR/${user}.cookie"
  local headers="$WORKDIR/${user}.login.headers"
  local authz status
  curl -sk -D "$headers" -o /dev/null -c "$jar" \
    "$BASE/api/v1/auth/oidc/login?returnTo=%2Fworkloads%3Fnamespace%3D${TARGET_NAMESPACE}"
  authz=$(awk 'tolower($1)=="location:" {sub(/\r$/,"",$2); print $2}' "$headers" | tail -1)
  [[ -n "$authz" ]] || die "$user login did not return an IdP redirect"
  if [[ "$authz" == *\?* ]]; then
    authz="${authz}&user=${user}"
  else
    authz="${authz}?user=${user}"
  fi
  status=$(curl -skL -b "$jar" -c "$jar" -o "$WORKDIR/${user}.login.body" -w '%{http_code}' "$authz")
  [[ "$status" == 200 ]] || die "$user OIDC login failed with HTTP $status"
  curl -sk -b "$jar" "$BASE/api/v1/session" >"$WORKDIR/${user}.session.json"
  jq -e --arg username "${user}@example.test" \
    '.authMode == "oidc" and .user.username == $username and (.csrfToken | length > 0)' \
    "$WORKDIR/${user}.session.json" >/dev/null || die "$user session response is invalid"
}

csrf() { jq -r '.csrfToken' "$WORKDIR/$1.session.json"; }
status_of() {
  local jar=$1 method=$2 path=$3 data=${4:-}
  local args=(-sk -o "$WORKDIR/request.body" -w '%{http_code}' -b "$jar" -X "$method" "$BASE$path")
  if [[ "$method" != GET ]]; then
    args+=(-H "X-CSRF-Token: $(csrf "$(basename "$jar" .cookie)")" -H 'Content-Type: application/json')
  fi
  if [[ -n "$data" ]]; then
    args+=(--data "$data")
  fi
  curl "${args[@]}"
}

log "Logging in viewer, operator and admin through the signed IdP"
login_as viewer
login_as operator
login_as admin

jq -e '.capabilities.read == true and .capabilities.revealSecrets == false and .capabilities.scale == false and .capabilities.nodeCordon == false' \
  "$WORKDIR/viewer.session.json" >/dev/null || die "viewer capabilities are incorrect"
jq -e '.capabilities.read == true and .capabilities.revealSecrets == false and .capabilities.nodeCordon == false' \
  "$WORKDIR/operator.session.json" >/dev/null || die "operator capabilities are incorrect"
jq -e '.capabilities.read == true and .capabilities.scale == true and .capabilities.delete == true and .capabilities.revealSecrets == true and .capabilities.nodeCordon == true' \
  "$WORKDIR/admin.session.json" >/dev/null || die "admin capabilities are incorrect"

log "Running RBAC matrix"
[[ "$(status_of "$WORKDIR/viewer.cookie" GET "/api/v1/workloads?namespace=${TARGET_NAMESPACE}")" == 200 ]] || die "viewer cannot read target workloads"
[[ "$(status_of "$WORKDIR/viewer.cookie" POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-demo/scale" '{"replicas":2}')" == 403 ]] || die "viewer scale was not denied"
[[ "$(status_of "$WORKDIR/operator.cookie" POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-demo/scale" '{"replicas":2}')" == 200 ]] || die "operator cannot scale in target namespace"
[[ "$(status_of "$WORKDIR/operator.cookie" POST "/api/v1/workloads/deployment/default/e2e-demo/scale" '{"replicas":2}')" == 403 ]] || die "operator exceeded target namespace"
[[ "$(status_of "$WORKDIR/admin.cookie" POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-admin/scale" '{"replicas":0}')" == 200 ]] || die "admin cannot scale in target namespace"
[[ "$(status_of "$WORKDIR/admin.cookie" GET '/api/v1/nodes')" == 200 ]] || die "admin cannot read nodes"

log "Verifying node Cordon/Uncordon RBAC, state and audit"
[[ "$CORDON_NODE_ORIGINAL" == false ]] || die "Cordon test node ${CORDON_NODE} must start schedulable"
[[ "$(status_of "$WORKDIR/viewer.cookie" POST "/api/v1/nodes/${CORDON_NODE}/cordon")" == 403 ]] || die "viewer node cordon was not denied"
[[ "$(kubectl get node "$CORDON_NODE" -o jsonpath='{.spec.unschedulable}' 2>/dev/null || true)" != true ]] || die "denied node cordon changed cluster state"
[[ "$(status_of "$WORKDIR/operator.cookie" POST "/api/v1/nodes/${CORDON_NODE}/cordon")" == 403 ]] || die "operator node cordon was not denied"
[[ "$(status_of "$WORKDIR/admin.cookie" POST "/api/v1/nodes/${CORDON_NODE}/cordon")" == 200 ]] || die "admin cannot cordon node"
jq -e --arg node "$CORDON_NODE" '.name == $node and .unschedulable == true' "$WORKDIR/request.body" >/dev/null || die "cordon response did not report unschedulable=true"
[[ "$(kubectl get node "$CORDON_NODE" -o jsonpath='{.spec.unschedulable}' 2>/dev/null || true)" == true ]] || die "cordon did not update the node"
[[ "$(status_of "$WORKDIR/admin.cookie" POST "/api/v1/nodes/${CORDON_NODE}/uncordon")" == 200 ]] || die "admin cannot uncordon node"
jq -e --arg node "$CORDON_NODE" '.name == $node and .unschedulable == false' "$WORKDIR/request.body" >/dev/null || die "uncordon response did not report unschedulable=false"
[[ "$(kubectl get node "$CORDON_NODE" -o jsonpath='{.spec.unschedulable}' 2>/dev/null || true)" != true ]] || die "uncordon did not restore scheduling"

create_payload() {
  printf '{"name":"%s","namespace":"%s","replicas":1,"containerName":"app","image":"nginx:alpine","containerPort":80,"cpuRequestMilli":100,"cpuLimitMilli":500,"memoryRequestBytes":134217728,"memoryLimitBytes":536870912,"env":[{"name":"MODE","value":"e2e"}],"labels":{"dosh-e2e":"created"}}' "$1" "$TARGET_NAMESPACE"
}

log "Verifying Deployment create, preview, conflict and RBAC"
[[ "$(status_of "$WORKDIR/operator.cookie" GET "/api/v1/capabilities?namespace=${TARGET_NAMESPACE}")" == 200 ]] || die "operator scoped capabilities request failed"
jq -e '.create == true and .scale == true' "$WORKDIR/request.body" >/dev/null || die "operator cannot create/scale in the target namespace"
kubectl -n "$TARGET_NAMESPACE" delete deployment e2e-operator-created --ignore-not-found >/dev/null
[[ "$(status_of "$WORKDIR/admin.cookie" POST /api/v1/workloads/deployments/preview "$(create_payload e2e-preview)")" == 200 ]] || die "admin Deployment preview failed"
grep -Fq 'kind: Deployment' "$WORKDIR/request.body" || die "Deployment preview did not return YAML"
grep -Fq 'name: e2e-preview' "$WORKDIR/request.body" || die "Deployment preview omitted the requested name"
[[ "$(status_of "$WORKDIR/viewer.cookie" POST /api/v1/workloads/deployments "$(create_payload e2e-viewer-created)")" == 403 ]] || die "viewer Deployment create was not denied"
invalid_payload=$(create_payload e2e-invalid | sed 's/"replicas":1/"replicas":1001/')
[[ "$(status_of "$WORKDIR/admin.cookie" POST /api/v1/workloads/deployments "$invalid_payload")" == 400 ]] || die "invalid Deployment create was not rejected"
if kubectl -n "$TARGET_NAMESPACE" get deployment e2e-invalid >/dev/null 2>&1; then die "invalid Deployment request created a resource"; fi
[[ "$(status_of "$WORKDIR/operator.cookie" POST /api/v1/workloads/deployments "$(create_payload e2e-operator-created)")" == 201 ]] || die "operator cannot create a Deployment in the target namespace"
kubectl -n "$TARGET_NAMESPACE" get deployment e2e-operator-created -o json | jq -e \
  '.spec.replicas == 1 and .spec.template.spec.containers[0].image == "nginx:alpine" and .spec.template.spec.containers[0].ports[0].containerPort == 80 and .metadata.labels["dosh-e2e"] == "created"' \
  >/dev/null || die "created Deployment does not match the requested spec"
[[ "$(status_of "$WORKDIR/admin.cookie" POST /api/v1/workloads/deployments "$(create_payload e2e-operator-created)")" == 409 ]] || die "duplicate Deployment create did not return 409"
[[ "$(kubectl -n "$TARGET_NAMESPACE" get deployment e2e-operator-created --no-headers 2>/dev/null | wc -l)" == 1 ]] || die "conflicting create produced more than one Deployment"
kubectl -n "$TARGET_NAMESPACE" delete deployment e2e-operator-created --ignore-not-found >/dev/null

log "Verifying audit identity attribution"
audit_log=$(kubectl -n "$NAMESPACE" logs deployment/dosh --tail=-1 2>/dev/null || true)
for username in viewer@example.test operator@example.test admin@example.test; do
  grep -Fq "\"user\":\"${username}\"" <<<"$audit_log" || die "audit log is missing ${username}"
done
grep -Fq '"result":"denied"' <<<"$audit_log" || die "audit log is missing denied result"
grep -Fq '"action":"create"' <<<"$audit_log" || die "audit log is missing Deployment create"
node_audit=$(kubectl -n "$NAMESPACE" logs deployment/dosh --since=5m 2>/dev/null | jq -Rr \
  'fromjson? | select(.msg == "audit_event" and .resource == "node" and .name == "'"$CORDON_NODE"'") |
   [.user, .action, .result, .request_id] | @tsv' || true)
grep -Fq $'viewer@example.test\tcordon\tdenied\t' <<<"$node_audit" || die "node cordon denied audit is missing"
grep -Fq $'admin@example.test\tcordon\tsuccess\t' <<<"$node_audit" || die "node cordon success audit is missing"
grep -Fq $'admin@example.test\tuncordon\tsuccess\t' <<<"$node_audit" || die "node uncordon success audit is missing"
[[ "$(awk -F '\t' 'NF == 4 && $4 != "" { count++ } END { print count+0 }' <<<"$node_audit")" -ge 3 ]] || die "node scheduling audit is missing request IDs"

log "Verifying in-memory audit history API authorization and query semantics"
[[ "$(curl -sk -o "$WORKDIR/request.body" -w '%{http_code}' "$BASE/api/v1/audit-events")" == 401 ]] ||
  die "unauthenticated audit history request was not rejected"
[[ "$(status_of "$WORKDIR/viewer.cookie" GET '/api/v1/audit-events')" == 403 ]] ||
  die "viewer audit history request was not denied"
[[ "$(status_of "$WORKDIR/operator.cookie" GET '/api/v1/audit-events')" == 403 ]] ||
  die "operator audit history request was not denied"
[[ "$(status_of "$WORKDIR/admin.cookie" GET '/api/v1/audit-events?page=1&pageSize=5&sort=id&order=desc')" == 200 ]] ||
  die "admin audit history request failed"
jq -e '
  (.meta.total >= 5) and (.meta.page == 1) and (.meta.pageSize == 5) and
  (.meta.sort == "id") and (.meta.order == "desc") and
  ((.items | length) <= 5) and
  ([.items[].id] == ([.items[].id] | sort | reverse))
' "$WORKDIR/request.body" >/dev/null || die "admin audit history pagination/sort response is invalid"

[[ "$(status_of "$WORKDIR/admin.cookie" GET "/api/v1/audit-events?namespace=${TARGET_NAMESPACE}&action=scale&result=success&q=operator&sort=id&order=desc")" == 200 ]] ||
  die "admin filtered audit history request failed"
jq -e --arg namespace "$TARGET_NAMESPACE" '
  (.meta.total >= 1) and
  ([.items[] | select(
    .namespace != $namespace or .action != "scale" or .result != "success" or
    ((.user | ascii_downcase) | contains("operator") | not)
  )] | length == 0)
' "$WORKDIR/request.body" >/dev/null || die "audit history filters did not constrain the result set"

[[ "$(status_of "$WORKDIR/admin.cookie" GET '/api/v1/audit-events?result=denied&pageSize=200&sort=id&order=asc')" == 200 ]] ||
  die "admin denied-result audit history request failed"
jq -e '(.meta.total >= 1) and ([.items[] | select(.result != "denied")] | length == 0)' \
  "$WORKDIR/request.body" >/dev/null || die "audit history denied filter returned non-denied records"
[[ "$(status_of "$WORKDIR/admin.cookie" GET '/api/v1/audit-events?result=unknown')" == 400 ]] ||
  die "invalid audit history result filter was not rejected"

log "Verifying RP-initiated logout"
logout_result=$(curl -skL -b "$WORKDIR/admin.cookie" -c "$WORKDIR/admin.cookie" \
  -o /dev/null -w '%{url_effective}' "$BASE/api/v1/auth/oidc/logout")
[[ "$logout_result" == "$BASE/login" ]] || die "RP logout returned to $logout_result instead of $BASE/login"
[[ "$(curl -sk -o /dev/null -w '%{http_code}' -b "$WORKDIR/admin.cookie" "$BASE/api/v1/session")" == 401 ]] || die "logout did not clear the dosh session"

if [[ "$HOLD" == 1 ]]; then
  [[ -n "$STATE_FILE" ]] || die "DOSH_E2E_HOLD=1 requires DOSH_E2E_STATE_FILE"
  state_tmp="${STATE_FILE}.tmp"
  jq -n \
    --arg base "$BASE" \
    --arg idp "https://${IDP_HOST}:${IDP_PORT}" \
    --arg namespace "$NAMESPACE" \
    --arg targetNamespace "$TARGET_NAMESPACE" \
    --arg release "$RELEASE" \
    --arg image "${IMAGE_REPOSITORY}:${IMAGE_TAG}${IMAGE_DIGEST:+@${IMAGE_DIGEST}}" \
    --arg workdir "$WORKDIR" \
    --arg idpPid "$IDP_PID" \
    --arg rbacSuffix "$RBAC_SUFFIX" \
    --arg rbacName "$RBAC_NAME" \
    --arg auditURL "https://${AUDIT_HOST}:${AUDIT_PORT}/audit" \
    --argjson auditEnabled "$([[ "$AUDIT_ENABLED" == 1 ]] && echo true || echo false)" \
    --argjson stateTTLMs "$OIDC_STATE_TTL_MS" \
    '{base:$base,idpURL:$idp,namespace:$namespace,targetNamespace:$targetNamespace,release:$release,image:$image,workdir:$workdir,idpPID:$idpPid,auditURL:$auditURL,auditEnabled:$auditEnabled,rbacSuffix:$rbacSuffix,rbacName:$rbacName,stateTTLMs:$stateTTLMs}' \
    >"$state_tmp"
  mv "$state_tmp" "$STATE_FILE"
  log "Fixture ready; holding test IdP and release for external browser tests"
  printf 'fixture state: %s\n' "$STATE_FILE"
fi

cat <<EOF

OIDC cluster matrix: pass
  release:          ${RELEASE}/${NAMESPACE}
  image:            ${IMAGE_REPOSITORY}:${IMAGE_TAG}${IMAGE_DIGEST:+@${IMAGE_DIGEST}}
  dosh endpoint:    ${BASE}
  test IdP:         https://${IDP_HOST}:${IDP_PORT}
  target namespace: ${TARGET_NAMESPACE}
  viewer:           read=200 scale=403 revealSecrets=false
  operator:         target scale=200 other namespace=403
  admin:            target scale=200 nodes=200 capabilities=all
  node scheduling:  viewer/operator cordon=403 admin cordon/uncordon=200 restored
  audit:            all three users and denied result present
  logout:           ${BASE}/login and session=401
EOF

if [[ "$HOLD" == 1 ]]; then
  printf 'Fixture held for external tests. Send SIGTERM to this process to stop the IdP.\n'
  while :; do sleep 3600; done
else
  printf 'The test IdP was stopped with this script. Re-run this script to recreate the\n'
  printf 'complete fixture; add DOSH_E2E_KEEP_WORKDIR=1 to retain generated credentials.\n'
fi
