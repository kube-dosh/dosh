#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/root/.local/bin:$PATH

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
STATE_DIR=${DOSH_E2E_ARTIFACT_DIR:-$(mktemp -d)}
STATE_FILE="$STATE_DIR/fixture.json"
INFRA_LOG="$STATE_DIR/infra.log"
EVIDENCE="$STATE_DIR/metrics-evidence.log"
IMAGE_TAG=${DOSH_E2E_IMAGE_TAG:-0.1.3-oidc-e2e-$(date +%s)}
METRICS_NAMESPACE=${DOSH_METRICS_NAMESPACE:-dosh-metrics}
DEMO_IMAGE=${DOSH_METRICS_DEMO_IMAGE:-192.168.122.10:5000/kubedo/pause:3.10.2}
SKIP_BUILD=${DOSH_E2E_SKIP_BUILD:-1}
INFRA_PID=""
FIXTURE_WORKDIR=""
METRICS_SCALED_DOWN=0
DEMO_CREATED=0

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
restore_metrics() {
  if [[ "$METRICS_SCALED_DOWN" == 1 ]]; then
    kubectl -n "$METRICS_NAMESPACE" scale deployment/metrics-server --replicas=1 >/dev/null 2>&1 || true
    kubectl -n "$METRICS_NAMESPACE" rollout status deployment/metrics-server --timeout=120s >/dev/null 2>&1 || true
    METRICS_SCALED_DOWN=0
  fi
}
cleanup() {
  restore_metrics
  if [[ -n "$INFRA_PID" ]] && kill -0 "$INFRA_PID" 2>/dev/null; then
    kill -TERM "$INFRA_PID" 2>/dev/null || true
    wait "$INFRA_PID" 2>/dev/null || true
  fi
  if [[ -n "$FIXTURE_WORKDIR" && ${DOSH_E2E_KEEP_ARTIFACTS:-0} != 1 ]]; then
    rm -rf "$FIXTURE_WORKDIR"
  fi
  if [[ "$DEMO_CREATED" == 1 ]]; then
    kubectl -n "${TARGET_NAMESPACE:-dosh-e2e-target}" delete deployment metrics-demo --ignore-not-found >/dev/null 2>&1 || true
  fi
  if [[ ${DOSH_E2E_KEEP_ARTIFACTS:-0} != 1 ]]; then
    rm -rf "$STATE_DIR"
  else
    printf 'Metrics E2E artifacts retained: %s\n' "$STATE_DIR"
  fi
}
trap cleanup EXIT

for command in kubectl helm curl jq go make bash; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done
mkdir -p "$STATE_DIR"

log "Checking Metrics API baseline"
kubectl get apiservice v1beta1.metrics.k8s.io >/dev/null
kubectl top nodes >/dev/null

log "Starting isolated OIDC fixture for Metrics API verification"
export DOSH_E2E_HOLD=1
export DOSH_E2E_STATE_FILE="$STATE_FILE"
export DOSH_E2E_KEEP_WORKDIR=1
export DOSH_E2E_SKIP_BUILD="$SKIP_BUILD"
export DOSH_E2E_IMAGE_TAG="$IMAGE_TAG"
export DOSH_E2E_IMAGE_DIGEST="${DOSH_E2E_IMAGE_DIGEST:-}"
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
[[ -s "$STATE_FILE" ]] || {
  tail -n 200 "$INFRA_LOG" >&2 || true
  die "timed out waiting for OIDC fixture state"
}

BASE=$(jq -r '.base' "$STATE_FILE")
NAMESPACE=$(jq -r '.namespace' "$STATE_FILE")
TARGET_NAMESPACE=$(jq -r '.targetNamespace' "$STATE_FILE")
FIXTURE_WORKDIR=$(jq -r '.workdir' "$STATE_FILE")
[[ "$BASE" == https://* && "$BASE" != *:32626 ]] || die "fixture endpoint is not isolated: $BASE"

login() {
  local user=$1
  local jar="$FIXTURE_WORKDIR/${user}.cookie"
  local headers="$FIXTURE_WORKDIR/${user}.headers"
  local location status
  curl -sk -D "$headers" -o /dev/null -c "$jar" \
    "$BASE/api/v1/auth/oidc/login?returnTo=%2Foverview"
  location=$(awk 'tolower($1)=="location:" {sub(/\r$/,"",$2); print $2}' "$headers" | tail -1)
  [[ -n "$location" ]] || die "$user login did not return an IdP redirect"
  if [[ "$location" == *\?* ]]; then location="${location}&user=${user}"; else location="${location}?user=${user}"; fi
  status=$(curl -skL -b "$jar" -c "$jar" -o /dev/null -w '%{http_code}' "$location")
  [[ "$status" == 200 ]] || die "$user login failed with HTTP $status"
}

api_get() {
  local jar=$1 path=$2
  curl -skf -b "$jar" "$BASE$path"
}

wait_metrics_state() {
  local expected=$1
  for _ in $(seq 1 45); do
    local body
    body=$(api_get "$FIXTURE_WORKDIR/admin.cookie" /api/v1/overview 2>/dev/null || true)
    if [[ -n "$body" ]] && [[ "$(jq -r '.cluster.metricsAvailable' <<<"$body")" == "$expected" ]]; then
      printf '%s' "$body"
      return 0
    fi
    sleep 2
  done
  return 1
}

log "Logging in as admin and checking real usage"
login admin

log "Creating a stable metrics demo pod"
DEMO_CREATED=1
kubectl -n "$TARGET_NAMESPACE" apply -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: metrics-demo
  labels: { app: metrics-demo }
spec:
  replicas: 1
  selector:
    matchLabels: { app: metrics-demo }
  template:
    metadata:
      labels: { app: metrics-demo }
    spec:
      containers:
        - name: demo
          image: ${DEMO_IMAGE}
          imagePullPolicy: IfNotPresent
          resources:
            requests: { cpu: 10m, memory: 16Mi }
            limits: { cpu: 100m, memory: 64Mi }
EOF
kubectl -n "$TARGET_NAMESPACE" rollout status deployment/metrics-demo --timeout=120s >/dev/null
for _ in $(seq 1 60); do
  if kubectl top pods -n "$TARGET_NAMESPACE" 2>/dev/null | grep -q 'metrics-demo'; then break; fi
  sleep 2
done

overview=$(api_get "$FIXTURE_WORKDIR/admin.cookie" /api/v1/overview)
jq -e '.cluster.metricsAvailable == true and .capacity.cpuUsageMilli > 0 and .capacity.memoryUsageBytes > 0 and .counts.nodes > 0' \
  <<<"$overview" >/dev/null || die "overview does not contain real metrics"
nodes=$(api_get "$FIXTURE_WORKDIR/admin.cookie" /api/v1/nodes)
jq -e '[.items[] | select(.hasMetrics == true and .cpuUsageMilli > 0 and .memoryUsageBytes > 0)] | length > 0' \
  <<<"$nodes" >/dev/null || die "node list does not contain real metrics"
pods=$(api_get "$FIXTURE_WORKDIR/admin.cookie" "/api/v1/pods?namespace=${TARGET_NAMESPACE}")
jq -e '[.items[] | select(.name | startswith("metrics-demo-")) | select(.hasMetrics == true) | select(.memoryUsageBytes > 0)] | length > 0' \
  <<<"$pods" >/dev/null || die "pod list does not contain real metrics"
printf 'available\tmetricsAvailable=true\tcpuUsageMilli=%s\tmemoryUsageBytes=%s\tnodes>0\tpods.hasMetrics>0\n' \
  "$(jq -r '.capacity.cpuUsageMilli' <<<"$overview")" "$(jq -r '.capacity.memoryUsageBytes' <<<"$overview")" >>"$EVIDENCE"

log "Temporarily scaling Metrics Server to zero to verify graceful fallback"
METRICS_SCALED_DOWN=1
kubectl -n "$METRICS_NAMESPACE" scale deployment/metrics-server --replicas=0 >/dev/null
for _ in $(seq 1 90); do
  if ! kubectl get --raw /apis/metrics.k8s.io/v1beta1 >/dev/null 2>&1; then break; fi
  sleep 1
done
overview_down=$(wait_metrics_state false) || die "overview did not degrade when Metrics API was unavailable"
jq -e '.capacity.cpuUsageMilli == 0 and .capacity.memoryUsageBytes == 0' <<<"$overview_down" >/dev/null || \
  die "overview retained stale metrics instead of falling back"
nodes_down=$(api_get "$FIXTURE_WORKDIR/admin.cookie" /api/v1/nodes)
jq -e '(.items | length) > 0 and ([.items[] | select(.hasMetrics == false)] | length) == (.items | length)' <<<"$nodes_down" >/dev/null || \
  die "node list did not report hasMetrics=false during fallback"
printf 'unavailable\tmetricsAvailable=false\tusage=0\tnode.hasMetrics=false\n' >>"$EVIDENCE"

log "Restoring Metrics Server and verifying recovery"
restore_metrics
overview_up=""
for _ in $(seq 1 60); do
  overview_up=$(api_get "$FIXTURE_WORKDIR/admin.cookie" /api/v1/overview 2>/dev/null || true)
  if [[ -n "$overview_up" ]] && jq -e '.cluster.metricsAvailable == true and .capacity.cpuUsageMilli > 0 and .capacity.memoryUsageBytes > 0' \
    <<<"$overview_up" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
jq -e '.cluster.metricsAvailable == true and .capacity.cpuUsageMilli > 0 and .capacity.memoryUsageBytes > 0' \
  <<<"$overview_up" >/dev/null || die "overview did not return real usage after recovery"
printf 'recovered\tmetricsAvailable=true\tcpuUsageMilli=%s\tmemoryUsageBytes=%s\n' \
  "$(jq -r '.capacity.cpuUsageMilli' <<<"$overview_up")" "$(jq -r '.capacity.memoryUsageBytes' <<<"$overview_up")" >>"$EVIDENCE"

log "Metrics API two-state verification passed"
cat "$EVIDENCE"
if [[ ${DOSH_E2E_KEEP_ARTIFACTS:-0} == 1 ]]; then
  printf '\nEvidence retained at %s\n' "$EVIDENCE"
fi
