#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/root/.local/bin:$PATH

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
STATE_DIR=${DOSH_E2E_ARTIFACT_DIR:-$(mktemp -d)}
STATE_FILE="$STATE_DIR/fixture.json"
INFRA_LOG="$STATE_DIR/infra.log"
EVIDENCE="$STATE_DIR/retry-evidence.log"
DOSH_IMAGE_TAG=${DOSH_E2E_IMAGE_TAG:-0.1.3-retry-$(date +%s)}
DOSH_IMAGE_DIGEST=${DOSH_E2E_IMAGE_DIGEST:-}
PROXY_IMAGE_TAG=${DOSH_FAULT_PROXY_IMAGE_TAG:-kube-fault-proxy-$(date +%s)}
PROXY_IMAGE_REPOSITORY=${DOSH_FAULT_PROXY_IMAGE_REPOSITORY:-192.168.122.10:5000/dosh/kube-fault-proxy}
ADMIN_PORT=${DOSH_FAULT_ADMIN_PORT:-18081}
SKIP_BUILD=${DOSH_E2E_SKIP_BUILD:-1}
PROXY_SKIP_BUILD=${DOSH_FAULT_PROXY_SKIP_BUILD:-0}
INFRA_PID=""
FIXTURE_WORKDIR=""
PORT_FORWARD_PID=""
APP_NAMESPACE=""
TARGET_NAMESPACE=""
BASE=""
ENV_PATCHED=0
NP_PATCHED=0
CSRF_TOKEN=""

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

remove_env_override() {
  [[ -n "$APP_NAMESPACE" && "$ENV_PATCHED" == 1 ]] || return 0
  kubectl -n "$APP_NAMESPACE" set env deployment/dosh \
    DOSH_KUBE_API_SERVER- DOSH_INSECURE_SKIP_VERIFY- >/dev/null 2>&1 || true
  kubectl -n "$APP_NAMESPACE" rollout status deployment/dosh --timeout=180s >/dev/null 2>&1 || true
  ENV_PATCHED=0
}

restore_network_policies() {
  [[ -n "$APP_NAMESPACE" && "$NP_PATCHED" == 1 ]] || return 0
  if [[ -s "$STATE_DIR/networkpolicies.yaml" ]]; then
    kubectl apply -f "$STATE_DIR/networkpolicies.yaml" >/dev/null 2>&1 || true
  fi
  NP_PATCHED=0
}

cleanup() {
  if [[ -n "$PORT_FORWARD_PID" ]]; then
    kill "$PORT_FORWARD_PID" 2>/dev/null || true
    wait "$PORT_FORWARD_PID" 2>/dev/null || true
    PORT_FORWARD_PID=""
  fi
  remove_env_override
  restore_network_policies
  if [[ -n "$APP_NAMESPACE" ]]; then
    kubectl -n "$APP_NAMESPACE" delete service kube-fault-proxy --ignore-not-found >/dev/null 2>&1 || true
    kubectl -n "$APP_NAMESPACE" delete deployment kube-fault-proxy --ignore-not-found >/dev/null 2>&1 || true
  fi
  if [[ -n "$INFRA_PID" ]] && kill -0 "$INFRA_PID" 2>/dev/null; then
    kill -TERM "$INFRA_PID" 2>/dev/null || true
    wait "$INFRA_PID" 2>/dev/null || true
  fi
  if [[ -n "$FIXTURE_WORKDIR" && ${DOSH_E2E_KEEP_ARTIFACTS:-0} != 1 ]]; then
    rm -rf "$FIXTURE_WORKDIR"
  fi
  if [[ ${DOSH_E2E_KEEP_ARTIFACTS:-0} != 1 ]]; then
    rm -rf "$STATE_DIR"
  else
    printf 'Kubernetes retry artifacts retained: %s\n' "$STATE_DIR"
  fi
}
trap cleanup EXIT

for command in kubectl helm curl jq go make podman bash; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done
mkdir -p "$STATE_DIR"

assert_isolated() {
  local node_port release_namespace production_image
  node_port=$(kubectl -n dosh get service dosh -o jsonpath='{.spec.ports[0].nodePort}')
  production_image=$(kubectl -n dosh get deployment dosh -o jsonpath='{.spec.template.spec.containers[0].image}')
  release_namespace=$(helm list -A --filter '^dosh$' -o json | jq -r '.[0].namespace // ""')
  [[ "$node_port" == "32626" ]] || die "production dosh NodePort changed: $node_port"
  [[ "$release_namespace" == "dosh" ]] || die "production dosh Release namespace changed: $release_namespace"
  printf 'production\tnamespace=dosh\tnode_port=%s\timage=%s\n' "$node_port" "$production_image" >>"$EVIDENCE"
}

wait_for_ready() {
  local code=000
  for _ in $(seq 1 120); do
    code=$(curl -sk -o /dev/null -w '%{http_code}' "$BASE/readyz" || true)
    [[ "$code" == 200 ]] && return 0
    sleep 0.5
  done
  kubectl -n "$APP_NAMESPACE" get pods -o wide >&2 || true
  kubectl -n "$APP_NAMESPACE" logs deployment/dosh --tail=120 >&2 || true
  die "dosh readiness through fault proxy stayed at HTTP ${code}"
}

assert_isolated
log "Starting isolated OIDC fixture for Kubernetes API fault injection"
export DOSH_E2E_HOLD=1
export DOSH_E2E_STATE_FILE="$STATE_FILE"
export DOSH_E2E_KEEP_WORKDIR=1
export DOSH_E2E_SKIP_BUILD="$SKIP_BUILD"
export DOSH_E2E_IMAGE_TAG="$DOSH_IMAGE_TAG"
export DOSH_E2E_IMAGE_DIGEST="$DOSH_IMAGE_DIGEST"
bash "$ROOT/scripts/e2e-oidc.sh" >"$INFRA_LOG" 2>&1 &
INFRA_PID=$!
for _ in $(seq 1 300); do
  [[ -s "$STATE_FILE" ]] && break
  if ! kill -0 "$INFRA_PID" 2>/dev/null; then
    tail -n 200 "$INFRA_LOG" >&2 || true
    die "OIDC fixture exited before becoming ready"
  fi
  sleep 1
done
[[ -s "$STATE_FILE" ]] || { tail -n 200 "$INFRA_LOG" >&2 || true; die "timed out waiting for OIDC fixture"; }

BASE=$(jq -r '.base' "$STATE_FILE")
APP_NAMESPACE=$(jq -r '.namespace' "$STATE_FILE")
TARGET_NAMESPACE=$(jq -r '.targetNamespace' "$STATE_FILE")
FIXTURE_WORKDIR=$(jq -r '.workdir' "$STATE_FILE")
[[ "$BASE" == https://* && "$BASE" != *:32626 ]] || die "fixture endpoint is not isolated: $BASE"

login_admin() {
  local jar="$FIXTURE_WORKDIR/admin.cookie" headers="$FIXTURE_WORKDIR/admin.headers" location status
  rm -f "$jar"
  curl -sk -D "$headers" -o /dev/null -c "$jar" "$BASE/api/v1/auth/oidc/login?returnTo=%2Foverview"
  location=$(awk 'tolower($1)=="location:" {sub(/\r$/,"",$2); print $2}' "$headers" | tail -1)
  [[ -n "$location" ]] || die "admin login did not return an IdP redirect"
  if [[ "$location" == *\?* ]]; then location="${location}&user=admin"; else location="${location}?user=admin"; fi
  status=$(curl -skL -b "$jar" -c "$jar" -o /dev/null -w '%{http_code}' "$location")
  [[ "$status" == 200 ]] || die "admin login failed with HTTP $status"
  curl -sk -b "$jar" "$BASE/api/v1/session" >"$FIXTURE_WORKDIR/admin.session.json"
  CSRF_TOKEN=$(jq -r '.csrfToken' "$FIXTURE_WORKDIR/admin.session.json")
  [[ -n "$CSRF_TOKEN" && "$CSRF_TOKEN" != null ]] || die "admin session did not include a CSRF token"
}

dosh_request() {
  local method=$1 path=$2 body=${3:-} output=${4:-/dev/null}
  local args=(-sk -b "$FIXTURE_WORKDIR/admin.cookie" -X "$method" -o "$output" -w '%{http_code}')
  if [[ "$method" != GET ]]; then
    args+=(-H "X-CSRF-Token: ${CSRF_TOKEN}")
  fi
  if [[ -n "$body" ]]; then
    args+=(-H 'Content-Type: application/json' -d "$body")
  fi
  curl "${args[@]}" "$BASE$path"
}

admin_reset() {
  curl -fsS -X POST "http://127.0.0.1:${ADMIN_PORT}/reset" >/dev/null
}

admin_config() {
  curl -fsS -H 'Content-Type: application/json' -d "$1" \
    "http://127.0.0.1:${ADMIN_PORT}/config" >/dev/null
}

admin_stats() {
  curl -fsS "http://127.0.0.1:${ADMIN_PORT}/stats"
}

stat_count() {
  local method=$1 path_part=$2 status=$3
  admin_stats | jq \
    --arg method "$method" \
    --arg path "$path_part" \
    --argjson status "$status" \
    '[.requests[] | select(.method == $method and (.path | contains($path)) and .status == $status) | .count] | add // 0'
}

assert_count() {
  local method=$1 path_part=$2 status=$3 want=$4 got
  got=$(stat_count "$method" "$path_part" "$status")
  printf 'attempt\tmethod=%s\tpath_contains=%s\tstatus=%s\tcount=%s\twant=%s\n' \
    "$method" "$path_part" "$status" "$got" "$want" >>"$EVIDENCE"
  [[ "$got" == "$want" ]] || die "request count for ${method} ${path_part} status ${status}: got ${got}, want ${want}"
}

log "Building and deploying the test-only Kubernetes API fault proxy"
if [[ "$PROXY_SKIP_BUILD" != 1 ]]; then
  mkdir -p "$ROOT/tools/kubefaultproxy/bin"
  (cd "$ROOT" && CGO_ENABLED=0 go build -trimpath \
    -o "$ROOT/tools/kubefaultproxy/bin/kube-fault-proxy" ./tools/kubefaultproxy)
  podman build -t "${PROXY_IMAGE_REPOSITORY}:${PROXY_IMAGE_TAG}" \
    -f "$ROOT/tools/kubefaultproxy/Dockerfile" "$ROOT/tools/kubefaultproxy"
  podman push --tls-verify=false "${PROXY_IMAGE_REPOSITORY}:${PROXY_IMAGE_TAG}"
fi

kubectl -n "$APP_NAMESPACE" apply -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-fault-proxy
  labels: { app: kube-fault-proxy, dosh-e2e: "true" }
spec:
  replicas: 1
  selector:
    matchLabels: { app: kube-fault-proxy }
  template:
    metadata:
      labels: { app: kube-fault-proxy, dosh-e2e: "true" }
    spec:
      automountServiceAccountToken: false
      securityContext:
        runAsNonRoot: true
        seccompProfile: { type: RuntimeDefault }
      containers:
        - name: proxy
          image: ${PROXY_IMAGE_REPOSITORY}:${PROXY_IMAGE_TAG}
          imagePullPolicy: IfNotPresent
          env:
            - name: UPSTREAM_URL
              value: https://kubernetes.default.svc:443
            - name: PROXY_ADDR
              value: :8443
            - name: ADMIN_ADDR
              value: :8081
          ports:
            - { name: https, containerPort: 8443, protocol: TCP }
            - { name: admin, containerPort: 8081, protocol: TCP }
          readinessProbe:
            httpGet: { path: /healthz, port: admin }
            periodSeconds: 2
            failureThreshold: 30
          livenessProbe:
            httpGet: { path: /healthz, port: admin }
            periodSeconds: 10
          resources:
            requests: { cpu: 10m, memory: 16Mi }
            limits: { cpu: 100m, memory: 64Mi }
          securityContext:
            allowPrivilegeEscalation: false
            capabilities: { drop: ["ALL"] }
            readOnlyRootFilesystem: true
---
apiVersion: v1
kind: Service
metadata:
  name: kube-fault-proxy
  labels: { app: kube-fault-proxy, dosh-e2e: "true" }
spec:
  selector: { app: kube-fault-proxy }
  ports:
    - { name: https, port: 443, targetPort: https, protocol: TCP }
    - { name: admin, port: 8081, targetPort: admin, protocol: TCP }
EOF
kubectl -n "$APP_NAMESPACE" rollout status deployment/kube-fault-proxy --timeout=180s >/dev/null

log "Temporarily routing the isolated dosh release through the fault proxy"
if kubectl -n "$APP_NAMESPACE" get networkpolicy,ciliumnetworkpolicy >/dev/null 2>&1; then
  kubectl -n "$APP_NAMESPACE" get networkpolicy,ciliumnetworkpolicy -o yaml >"$STATE_DIR/networkpolicies.yaml"
  kubectl -n "$APP_NAMESPACE" delete networkpolicy,ciliumnetworkpolicy --all >/dev/null
  NP_PATCHED=1
fi
kubectl -n "$APP_NAMESPACE" set env deployment/dosh \
  "DOSH_KUBE_API_SERVER=https://kube-fault-proxy.${APP_NAMESPACE}.svc:443" \
  DOSH_INSECURE_SKIP_VERIFY=true >/dev/null
ENV_PATCHED=1
kubectl -n "$APP_NAMESPACE" rollout status deployment/dosh --timeout=180s >/dev/null
wait_for_ready

login_admin
kubectl -n "$APP_NAMESPACE" port-forward service/kube-fault-proxy "${ADMIN_PORT}:8081" \
  >"$STATE_DIR/port-forward.log" 2>&1 &
PORT_FORWARD_PID=$!
for _ in $(seq 1 100); do
  curl -fsS "http://127.0.0.1:${ADMIN_PORT}/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS "http://127.0.0.1:${ADMIN_PORT}/healthz" >/dev/null || die "fault proxy admin endpoint is unavailable"

printf 'retry-test\tstarted_at=%s\tdosh_image=%s%s\tproxy_image=%s\tbase=%s\n' \
  "$(date -Is)" "$DOSH_IMAGE_TAG" "${DOSH_IMAGE_DIGEST:+@${DOSH_IMAGE_DIGEST}}" "${PROXY_IMAGE_REPOSITORY}:${PROXY_IMAGE_TAG}" "$BASE" >>"$EVIDENCE"

log "Testing transient 429/5xx retries on GET"
for status in 429 500 502 503 504; do
  admin_reset
  admin_config "{\"method\":\"GET\",\"pathContains\":\"/api/v1/nodes\",\"status\":${status},\"remaining\":2}"
  code=$(dosh_request GET "/api/v1/nodes" "" "$STATE_DIR/transient-${status}.json")
  printf 'transient\tstatus=%s\tresponse=%s\n' "$status" "$code" >>"$EVIDENCE"
  [[ "$code" == 200 ]] || die "transient ${status} did not recover to 200: ${code}"
  assert_count GET "/api/v1/nodes" "$status" 2
  assert_count GET "/api/v1/nodes" 200 1
done

log "Testing Retry-After and bounded persistent failure"
admin_reset
admin_config '{"method":"GET","pathContains":"/api/v1/nodes","status":429,"remaining":2,"retryAfter":"1"}'
started=$(date +%s%3N)
code=$(dosh_request GET "/api/v1/nodes" "" "$STATE_DIR/retry-after.json")
elapsed=$(( $(date +%s%3N) - started ))
printf 'retry-after\tresponse=%s\telapsed_ms=%s\n' "$code" "$elapsed" >>"$EVIDENCE"
[[ "$code" == 200 ]] || die "Retry-After case did not recover: ${code}"
(( elapsed >= 1800 )) || die "Retry-After was not honored: ${elapsed}ms"
assert_count GET "/api/v1/nodes" 429 2
assert_count GET "/api/v1/nodes" 200 1

admin_reset
admin_config '{"method":"GET","pathContains":"/pods","status":429,"remaining":100}'
code=$(dosh_request GET "/api/v1/pods?namespace=${TARGET_NAMESPACE}" "" "$STATE_DIR/persistent-429.json")
printf 'persistent\tstatus=429\tresponse=%s\tbody=%s\n' "$code" \
  "$(jq -c '{code,message}' "$STATE_DIR/persistent-429.json" 2>/dev/null || true)" >>"$EVIDENCE"
[[ "$code" == 429 ]] || die "persistent 429 mapped to ${code}, want 429"
jq -e '.code == "rate_limited"' "$STATE_DIR/persistent-429.json" >/dev/null || die "persistent 429 response code is not rate_limited"
assert_count GET "/pods" 429 3

log "Testing that writes are never automatically replayed"
admin_reset
admin_config '{"method":"PUT","pathContains":"/scale","status":503,"remaining":100}'
before=$(kubectl -n "$TARGET_NAMESPACE" get deployment e2e-demo -o jsonpath='{.spec.replicas}')
code=$(dosh_request POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-demo/scale" \
  "{\"replicas\":$((before + 1))}" "$STATE_DIR/write-503.json")
after=$(kubectl -n "$TARGET_NAMESPACE" get deployment e2e-demo -o jsonpath='{.spec.replicas}')
printf 'write\tresponse=%s\treplicas_before=%s\treplicas_after=%s\tbody=%s\n' "$code" "$before" "$after" \
  "$(jq -c '{code,message}' "$STATE_DIR/write-503.json" 2>/dev/null || true)" >>"$EVIDENCE"
[[ "$code" == 503 ]] || die "write fault mapped to ${code}, want 503"
[[ "$before" == "$after" ]] || die "replicas changed despite injected write failure: ${before} -> ${after}"
assert_count PUT "/scale" 503 1

log "Testing connection reset retry and client cancellation"
admin_reset
admin_config '{"method":"GET","pathContains":"/api/v1/nodes","abort":true,"remaining":2}'
code=$(dosh_request GET "/api/v1/nodes" "" "$STATE_DIR/abort.json")
printf 'abort\tresponse=%s\n' "$code" >>"$EVIDENCE"
[[ "$code" == 200 ]] || die "connection reset did not recover: ${code}"
assert_count GET "/api/v1/nodes" 0 2
assert_count GET "/api/v1/nodes" 200 1

admin_reset
admin_config '{"method":"GET","pathContains":"/api/v1/nodes","status":503,"remaining":1,"delayMs":5000}'
set +e
curl -sk --max-time 1 -b "$FIXTURE_WORKDIR/admin.cookie" -o /dev/null "$BASE/api/v1/nodes"
cancel_exit=$?
set -e
sleep 1
printf 'cancellation\tcurl_exit=%s\n' "$cancel_exit" >>"$EVIDENCE"
[[ "$cancel_exit" == 28 ]] || die "cancellation request did not time out locally: exit ${cancel_exit}"
assert_count GET "/api/v1/nodes" 0 1

admin_reset
code=$(dosh_request GET "/api/v1/nodes" "" "$STATE_DIR/recovered.json")
printf 'recovered\tresponse=%s\n' "$code" >>"$EVIDENCE"
[[ "$code" == 200 ]] || die "dosh did not recover after fault proxy reset: ${code}"

kubectl -n "$APP_NAMESPACE" logs deployment/kube-fault-proxy --tail=-1 >>"$EVIDENCE" 2>&1 || true
assert_isolated
log "Kubernetes 429/5xx fault injection matrix passed"
cat "$EVIDENCE"
