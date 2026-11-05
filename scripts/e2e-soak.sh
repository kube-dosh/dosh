#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/root/.local/bin:$PATH

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
STATE_DIR=${DOSH_E2E_ARTIFACT_DIR:-$(mktemp -d)}
STATE_FILE="$STATE_DIR/fixture.json"
INFRA_LOG="$STATE_DIR/infra.log"
EVIDENCE="$STATE_DIR/soak-evidence.log"
APP_LOG_RAW="$STATE_DIR/app.log.raw"
EVENT_STREAM_DIR="$STATE_DIR/event-streams"
LOG_STREAM_DIR="$STATE_DIR/log-streams"
IMAGE_TAG=${DOSH_E2E_IMAGE_TAG:-0.1.3-oidc-e2e-$(date +%s)}
IMAGE_DIGEST=${DOSH_E2E_IMAGE_DIGEST:-}
DEMO_IMAGE=${DOSH_SOAK_DEMO_IMAGE:-192.168.122.10:5000/dosh/nginx-logger:alpine}
DURATION=${DOSH_SOAK_DURATION:-1800}
SAMPLE_INTERVAL=${DOSH_SOAK_SAMPLE_INTERVAL:-30}
EVENT_STREAMS=${DOSH_SOAK_EVENT_STREAMS:-30}
LOG_STREAMS=${DOSH_SOAK_LOG_STREAMS:-30}
STREAM_ROUND=0
INFRA_PID=""
FIXTURE_WORKDIR=""
EVENT_PIDS=()
LOG_PIDS=()
TARGET_NAMESPACE=""
APP_NAMESPACE=""

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

on_error() {
  local rc=$1 line=$2 command=$3
  printf 'error: line=%s rc=%s command=%s\n' "$line" "$rc" "$command" >&2
  if [[ -d "${STATE_DIR:-}" ]]; then
    printf 'error\tline=%s\trc=%s\tcommand=%s\n' "$line" "$rc" "$command" >>"$EVIDENCE"
  fi
}
trap 'on_error "$?" "$LINENO" "$BASH_COMMAND"' ERR

stop_streams() {
  local pid
  for pid in "${EVENT_PIDS[@]}" "${LOG_PIDS[@]}"; do
    [[ -n "$pid" ]] || continue
    kill "$pid" 2>/dev/null || true
  done
  for pid in "${EVENT_PIDS[@]}" "${LOG_PIDS[@]}"; do
    [[ -n "$pid" ]] || continue
    wait "$pid" 2>/dev/null || true
  done
  EVENT_PIDS=()
  LOG_PIDS=()
}

delete_soak_events() {
  [[ -n "$TARGET_NAMESPACE" ]] || return 0
  kubectl -n "$TARGET_NAMESPACE" delete configmap -l dosh.soak/event=true \
    --ignore-not-found >/dev/null 2>&1 || true
}

cleanup() {
  stop_streams
  delete_soak_events
  if [[ -n "$TARGET_NAMESPACE" ]]; then
    kubectl -n "$TARGET_NAMESPACE" delete deployment metrics-demo --ignore-not-found >/dev/null 2>&1 || true
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
    printf 'Soak artifacts retained: %s\n' "$STATE_DIR"
  fi
}
trap cleanup EXIT

for command in kubectl helm curl jq go make bash; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done
mkdir -p "$STATE_DIR" "$EVENT_STREAM_DIR" "$LOG_STREAM_DIR"
if (( DURATION < 1 )); then die "DOSH_SOAK_DURATION must be positive"; fi
if (( SAMPLE_INTERVAL < 1 )); then die "DOSH_SOAK_SAMPLE_INTERVAL must be positive"; fi
if (( EVENT_STREAMS < 1 || LOG_STREAMS < 1 )); then die "stream counts must be positive"; fi

log "Starting isolated OIDC fixture for ${DURATION}s soak"
export DOSH_E2E_HOLD=1
export DOSH_E2E_STATE_FILE="$STATE_FILE"
export DOSH_E2E_KEEP_WORKDIR=1
export DOSH_E2E_SKIP_BUILD="${DOSH_E2E_SKIP_BUILD:-1}"
export DOSH_E2E_IMAGE_TAG="$IMAGE_TAG"
export DOSH_E2E_IMAGE_DIGEST="$IMAGE_DIGEST"
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

wait_for_dosh_ready() {
  local _ status
  for _ in $(seq 1 120); do
    status=$(curl -sk -o /dev/null -w '%{http_code}' "$BASE/readyz" 2>/dev/null || true)
    [[ "$status" == 200 ]] && return 0
    sleep 1
  done
  return 1
}

login_admin() {
  local jar="$FIXTURE_WORKDIR/admin.cookie" headers="$FIXTURE_WORKDIR/admin.headers" location status
  local _ 
  for _ in $(seq 1 60); do
    rm -f "$jar"
    if ! curl -sk -D "$headers" -o /dev/null -c "$jar" "$BASE/api/v1/auth/oidc/login?returnTo=%2Foverview"; then
      sleep 1
      continue
    fi
    location=$(awk 'tolower($1)=="location:" {sub(/\r$/,"",$2); print $2}' "$headers" | tail -1)
    if [[ -z "$location" ]]; then
      sleep 1
      continue
    fi
    if [[ "$location" == *\?* ]]; then location="${location}&user=admin"; else location="${location}?user=admin"; fi
    status=$(curl -skL -b "$jar" -c "$jar" -o /dev/null -w '%{http_code}' "$location" 2>/dev/null || true)
    [[ "$status" == 200 ]] && return 0
    sleep 1
  done
  die "admin login did not recover after the application rollout"
}

start_streams() {
  stop_streams
  local pod round_dir i pid
  if (( STREAM_ROUND > 0 )); then
    printf 'session-refresh\t%s\tnext_round=%s\n' "$(date -Is)" "$((STREAM_ROUND + 1))" >>"$EVIDENCE"
  fi
  login_admin
  pod=$(kubectl -n "$TARGET_NAMESPACE" get pod -l app=metrics-demo \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  [[ -n "$pod" ]] || return 1
  STREAM_ROUND=$((STREAM_ROUND + 1))
  round_dir="$EVENT_STREAM_DIR/round-${STREAM_ROUND}"
  mkdir -p "$round_dir" "$LOG_STREAM_DIR/round-${STREAM_ROUND}"
  for i in $(seq 1 "$EVENT_STREAMS"); do
    curl -skN -b "$FIXTURE_WORKDIR/admin.cookie" \
      "$BASE/api/v1/events/stream?namespace=${TARGET_NAMESPACE}" \
      >"$round_dir/stream-${i}.log" 2>&1 &
    EVENT_PIDS+=("$!")
  done
  for i in $(seq 1 "$LOG_STREAMS"); do
    curl -skN -b "$FIXTURE_WORKDIR/admin.cookie" \
      "$BASE/api/v1/pods/${TARGET_NAMESPACE}/${pod}/logs?follow=true&tailLines=10" \
      >"$LOG_STREAM_DIR/round-${STREAM_ROUND}/stream-${i}.log" 2>&1 &
    LOG_PIDS+=("$!")
  done
  sleep 2
  for pid in "${EVENT_PIDS[@]}" "${LOG_PIDS[@]}"; do
    if ! kill -0 "$pid" 2>/dev/null; then
      die "a soak stream exited during startup"
    fi
  done
  printf 'streams\t%s\tround=%s\tpod=%s\tevent_streams=%s\tlog_streams=%s\n' \
    "$(date -Is)" "$STREAM_ROUND" "$pod" "$EVENT_STREAMS" "$LOG_STREAMS" >>"$EVIDENCE"
}

check_streams() {
  local elapsed=$1 pid dead=0
  for pid in "${EVENT_PIDS[@]}" "${LOG_PIDS[@]}"; do
    if ! kill -0 "$pid" 2>/dev/null; then dead=$((dead + 1)); fi
  done
  if (( dead > 0 )); then
    STREAM_FAILURES=$((STREAM_FAILURES + dead))
    printf 'stream-dead\telapsed=%s\tcount=%s\n' "$elapsed" "$dead" >>"$EVIDENCE"
    start_streams || true
  fi
}

to_mi() {
  local value=$1
  case "$value" in
    *Gi) printf '%s' $(( ${value%Gi} * 1024 )) ;;
    *Mi) printf '%s' "${value%Mi}" ;;
    *Ki) printf '%s' $(( ${value%Ki} / 1024 )) ;;
    *) printf '0' ;;
  esac
}

sample_memory() {
  local output line name cpu mem _ mi total=0 single=0
  output=$(kubectl -n "$APP_NAMESPACE" top pod -l app.kubernetes.io/name=dosh \
    --no-headers 2>/dev/null || true)
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    read -r name cpu mem _ <<<"$line"
    mi=$(to_mi "$mem")
    total=$((total + mi))
    if (( mi > single )); then single=$mi; fi
  done <<<"$output"
  if (( total > 0 )); then
    MEM_SAMPLES=$((MEM_SAMPLES + 1))
    if [[ -z "$FIRST_MEM_MI" ]]; then FIRST_MEM_MI=$total; fi
    LAST_MEM_MI=$total
    if (( total > MAX_TOTAL_MEM_MI )); then MAX_TOTAL_MEM_MI=$total; fi
    if (( single > MAX_POD_MEM_MI )); then MAX_POD_MEM_MI=$single; fi
  fi
  printf 'sample\telapsed=%s\tat=%s\ttotal_memory_mi=%s\tmax_pod_memory_mi=%s\tevent_configmaps=%s\n' \
    "$1" "$(date -Is)" "$total" "$single" "$EVENT_COUNT" >>"$EVIDENCE"
}

collect_app_logs() {
  kubectl -n "$APP_NAMESPACE" logs -l app.kubernetes.io/name=dosh \
    --since=2m --prefix --timestamps 2>&1 >>"$APP_LOG_RAW" || true
}

log "Creating stable soak pod and opening ${LOG_STREAMS} log / ${EVENT_STREAMS} SSE streams"
kubectl -n "$TARGET_NAMESPACE" apply -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: metrics-demo
  labels: { app: metrics-demo, dosh-soak: "true" }
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
          command: ["/bin/sh", "-c"]
          args: ["i=0; while true; do i=\$((i+1)); echo \"soak-log-line-\$i\"; sleep 2; done"]
          resources:
            requests: { cpu: 10m, memory: 16Mi }
            limits: { cpu: 100m, memory: 64Mi }
EOF
kubectl -n "$TARGET_NAMESPACE" rollout status deployment/metrics-demo --timeout=120s >/dev/null
sleep 2
start_streams

printf 'mode=soak\tstarted_at=%s\tduration_seconds=%s\timage=%s%s\tdemo_image=%s\tevent_streams=%s\tlog_streams=%s\n' \
  "$(date -Is)" "$DURATION" "$IMAGE_TAG" "${IMAGE_DIGEST:+@${IMAGE_DIGEST}}" "$DEMO_IMAGE" "$EVENT_STREAMS" "$LOG_STREAMS" >>"$EVIDENCE"
START_EPOCH=$(date +%s)
NEXT_SAMPLE=$START_EPOCH
NEXT_EVENT=$START_EPOCH
NEXT_RECONNECT=$((START_EPOCH + 300))
DEMO_DELETED=0
APP_RESTARTED=0
MAX_TOTAL_MEM_MI=0
MAX_POD_MEM_MI=0
FIRST_MEM_MI=""
LAST_MEM_MI=""
MEM_SAMPLES=0
EVENT_COUNT=0
EVENT_CREATE_FAILURES=0
RECONNECT_COUNT=0
STREAM_FAILURES=0

while (( $(date +%s) - START_EPOCH < DURATION )); do
  now=$(date +%s)
  elapsed=$((now - START_EPOCH))
  if (( now >= NEXT_SAMPLE )); then
    sample_memory "$elapsed"
    collect_app_logs
    check_streams "$elapsed"
    NEXT_SAMPLE=$((now + SAMPLE_INTERVAL))
  fi
  if (( now >= NEXT_EVENT )); then
    EVENT_COUNT=$((EVENT_COUNT + 1))
    if kubectl -n "$TARGET_NAMESPACE" create configmap "soak-event-${EVENT_COUNT}" \
      --from-literal=sequence="${EVENT_COUNT}" --dry-run=client -o yaml \
      | kubectl label --local -f - dosh.soak/event=true -o yaml \
      | kubectl apply -f - >/dev/null 2>&1; then
      printf 'event-created\telapsed=%s\tsequence=%s\n' "$elapsed" "$EVENT_COUNT" >>"$EVIDENCE"
    else
      EVENT_CREATE_FAILURES=$((EVENT_CREATE_FAILURES + 1))
      printf 'event-create-failed\telapsed=%s\tsequence=%s\n' "$elapsed" "$EVENT_COUNT" >>"$EVIDENCE"
    fi
    NEXT_EVENT=$((now + 60))
  fi
  if (( now >= NEXT_RECONNECT )); then
    RECONNECT_COUNT=$((RECONNECT_COUNT + 1))
    printf 'reconnect\telapsed=%s\tat=%s\n' "$elapsed" "$(date -Is)" >>"$EVIDENCE"
    start_streams
    NEXT_RECONNECT=$((now + 300))
  fi
  if (( elapsed >= DURATION / 2 && DEMO_DELETED == 0 )); then
    DEMO_DELETED=1
    printf 'pod-delete\telapsed=%s\n' "$elapsed" >>"$EVIDENCE"
    stop_streams
    kubectl -n "$TARGET_NAMESPACE" delete pod -l app=metrics-demo --wait=true >/dev/null
    kubectl -n "$TARGET_NAMESPACE" rollout status deployment/metrics-demo --timeout=120s >/dev/null
    start_streams
  fi
  if (( elapsed >= DURATION * 2 / 3 && APP_RESTARTED == 0 )); then
    APP_RESTARTED=1
    printf 'app-rollout\telapsed=%s\n' "$elapsed" >>"$EVIDENCE"
    stop_streams
    collect_app_logs
    printf 'app-rollout-restart\telapsed=%s\tat=%s\n' "$elapsed" "$(date -Is)" >>"$EVIDENCE"
    kubectl -n "$APP_NAMESPACE" rollout restart deployment/dosh >/dev/null
    printf 'app-rollout-status\telapsed=%s\tat=%s\n' "$elapsed" "$(date -Is)" >>"$EVIDENCE"
    kubectl -n "$APP_NAMESPACE" rollout status deployment/dosh --timeout=180s >/dev/null
    printf 'app-rollout-ready\telapsed=%s\tat=%s\n' "$elapsed" "$(date -Is)" >>"$EVIDENCE"
    wait_for_dosh_ready || die "dosh readiness did not recover after rollout"
    login_admin
    start_streams
  fi
  sleep 5
done

stop_streams
collect_app_logs
sort -u "$APP_LOG_RAW" -o "$STATE_DIR/app.log"
APP_LOG=$(cat "$STATE_DIR/app.log")
ERROR_COUNT=$(grep -c '"level":"ERROR"' <<<"$APP_LOG" || true)
EVENT_CONFIGMAP_COUNT=$(kubectl -n "$TARGET_NAMESPACE" get configmap -l dosh.soak/event=true \
  --no-headers 2>/dev/null | wc -l)
EVENT_BYTES=$(find "$EVENT_STREAM_DIR" -type f -name '*.log' -exec cat {} + 2>/dev/null | wc -c)
LOG_BYTES=$(find "$LOG_STREAM_DIR" -type f -name '*.log' -exec cat {} + 2>/dev/null | wc -c)
EVENT_FRAMES=$(find "$EVENT_STREAM_DIR" -type f -name '*.log' -exec cat {} + 2>/dev/null \
  | grep -c '^event: event' || true)
LOG_LINES=$(find "$LOG_STREAM_DIR" -type f -name '*.log' -exec cat {} + 2>/dev/null \
  | grep -c 'soak-log-line-' || true)
printf 'final\tfinished_at=%s\tfirst_memory_mi=%s\tlast_memory_mi=%s\tmax_total_memory_mi=%s\tmax_pod_memory_mi=%s\tmemory_samples=%s\tevents_generated=%s\tevent_create_failures=%s\tevent_configmaps=%s\tevent_frames=%s\tevent_stream_bytes=%s\tlog_lines=%s\tlog_stream_bytes=%s\tapp_errors=%s\treconnects=%s\tstream_failures=%s\n' \
  "$(date -Is)" "$FIRST_MEM_MI" "$LAST_MEM_MI" "$MAX_TOTAL_MEM_MI" "$MAX_POD_MEM_MI" \
  "$MEM_SAMPLES" "$EVENT_COUNT" "$EVENT_CREATE_FAILURES" "$EVENT_CONFIGMAP_COUNT" \
  "$EVENT_FRAMES" "$EVENT_BYTES" "$LOG_LINES" "$LOG_BYTES" "$ERROR_COUNT" \
  "$RECONNECT_COUNT" "$STREAM_FAILURES" >>"$EVIDENCE"
delete_soak_events

(( MEM_SAMPLES > 0 )) || die "no application memory samples were collected"
(( MAX_POD_MEM_MI <= 256 )) || die "pod memory exceeded 256Mi: ${MAX_POD_MEM_MI}Mi"
(( ERROR_COUNT == 0 )) || die "app emitted ${ERROR_COUNT} ERROR log lines during soak"
(( EVENT_CREATE_FAILURES == 0 )) || die "event creation failed ${EVENT_CREATE_FAILURES} times"
(( EVENT_CONFIGMAP_COUNT == EVENT_COUNT )) || die "event ConfigMap count ${EVENT_CONFIGMAP_COUNT} != ${EVENT_COUNT}"
(( EVENT_FRAMES > 0 )) || die "events stream produced no observable event frame"
(( LOG_LINES > 0 )) || die "log streams produced no observable log line"
(( RECONNECT_COUNT >= 1 )) || die "soak did not exercise stream reconnects"
(( STREAM_FAILURES == 0 )) || die "soak observed ${STREAM_FAILURES} unexpected stream exits"

log "soak passed"
cat "$EVIDENCE"
if [[ ${DOSH_E2E_KEEP_ARTIFACTS:-0} == 1 ]]; then
  printf '\nEvidence retained at %s\n' "$EVIDENCE"
fi
