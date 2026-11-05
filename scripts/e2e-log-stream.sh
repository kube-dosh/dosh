#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/root/.local/bin:$PATH

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
NAMESPACE=${DOSH_E2E_LOG_NAMESPACE:-dosh-e2e-logs}
TARGET_NAMESPACE=${DOSH_E2E_LOG_TARGET_NAMESPACE:-dosh-e2e-logs-target}
RELEASE=${DOSH_E2E_LOG_RELEASE:-dosh-logs-e2e}
NODE_IP=${DOSH_E2E_NODE_IP:-192.168.122.100}
NODE_PORT=${DOSH_E2E_LOG_NODE_PORT:-32631}
IMAGE_REPOSITORY=${DOSH_E2E_IMAGE_REPOSITORY:-192.168.122.10:5000/dosh/dosh}
REGISTRY_HOST=${IMAGE_REPOSITORY%%/*}
REGISTRY_REPOSITORY=${IMAGE_REPOSITORY#*/}
IMAGE_TAG=${DOSH_E2E_IMAGE_TAG:-0.1.4-rc-logframes-$(date -u +%Y%m%dT%H%M%SZ)}
IMAGE_DIGEST=${DOSH_E2E_IMAGE_DIGEST:-}
SKIP_BUILD=${DOSH_E2E_SKIP_BUILD:-0}
LOG_IMAGE=${DOSH_E2E_LOG_IMAGE:-192.168.122.10:5000/dosh/nginx-logger:alpine}
ARTIFACT_DIR=${DOSH_E2E_ARTIFACT_DIR:-/var/lib/dosh/e2e-runs/$(date -u +%Y%m%dT%H%M%SZ)-v244-log-stream}
WORKDIR=$(mktemp -d)
BASE="https://${NODE_IP}:${NODE_PORT}"
PASSWORD=$(openssl rand -hex 24)
PRODUCTION_BEFORE=""

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

cleanup() {
  kubectl delete namespace "$TARGET_NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
  kubectl delete namespace "$NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

for command in kubectl helm curl openssl jq podman timeout; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done
mkdir -p "$ARTIFACT_DIR" "$WORKDIR"

production_fingerprint() {
  local revision image port ready
  revision=$(helm -n dosh list -o json | jq -r '.[] | select(.name=="dosh") | .revision')
  image=$(kubectl -n dosh get deployment dosh -o jsonpath='{.spec.template.spec.containers[0].image}')
  port=$(kubectl -n dosh get service dosh -o jsonpath='{.spec.ports[0].nodePort}')
  ready=$(kubectl -n dosh get deployment dosh -o jsonpath='{.status.readyReplicas}/{.status.replicas}')
  printf 'revision=%s\timage=%s\tnodePort=%s\tready=%s' "$revision" "$image" "$port" "$ready"
}

PRODUCTION_BEFORE=$(production_fingerprint)
[[ $(kubectl get service -A -o jsonpath='{range .items[*]}{.spec.ports[*].nodePort}{"\n"}{end}' | tr ' ' '\n' | grep -cx "$NODE_PORT" || true) -eq 0 ]] ||
  die "test NodePort ${NODE_PORT} is already in use"

if [[ "$SKIP_BUILD" != 1 ]]; then
  log "Building candidate ${IMAGE_REPOSITORY}:${IMAGE_TAG}"
  make -C "$ROOT" build VERSION="$IMAGE_TAG" COMMIT=log-e2e >"$ARTIFACT_DIR/build.log" 2>&1
  podman build -t "${IMAGE_REPOSITORY}:${IMAGE_TAG}" -f "$ROOT/Dockerfile" "$ROOT" >>"$ARTIFACT_DIR/build.log" 2>&1
  podman push --tls-verify=false "${IMAGE_REPOSITORY}:${IMAGE_TAG}" >>"$ARTIFACT_DIR/build.log" 2>&1
fi
if [[ -z "$IMAGE_DIGEST" ]]; then
  IMAGE_DIGEST=$(curl -skI -H 'Accept: application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json' \
    "https://${REGISTRY_HOST}/v2/${REGISTRY_REPOSITORY}/manifests/${IMAGE_TAG}" 2>/dev/null | awk 'tolower($1)=="docker-content-digest:" {sub(/\r$/,"",$2); print $2}' | tail -1)
fi
[[ "$IMAGE_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || die "failed to resolve candidate digest"

log "Creating isolated TLS and local-auth fixture"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -keyout "$WORKDIR/tls.key" -out "$WORKDIR/tls.crt" \
  -subj "/CN=${NODE_IP}" -addext "subjectAltName=IP:${NODE_IP}" >/dev/null 2>&1
printf '%s' "$PASSWORD" | "$ROOT/bin/dosh" hash-password >"$WORKDIR/admin-password-hash"
openssl rand -base64 32 >"$WORKDIR/session-key"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n "$NAMESPACE" create secret tls dosh-tls --cert="$WORKDIR/tls.crt" --key="$WORKDIR/tls.key" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n "$NAMESPACE" create secret generic dosh-auth \
  --from-file=admin-password-hash="$WORKDIR/admin-password-hash" \
  --from-file=session-key="$WORKDIR/session-key" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

log "Installing isolated Release ${RELEASE}/${NAMESPACE}"
helm upgrade --install "$RELEASE" "$ROOT/deploy/helm/dosh" \
  --namespace "$NAMESPACE" \
  --set image.repository="$IMAGE_REPOSITORY" \
  --set-string image.tag="$IMAGE_TAG" \
  --set-string image.digest="$IMAGE_DIGEST" \
  --set-string service.nodePort="$NODE_PORT" \
  --set-string rbac.clusterRoleName="$RELEASE" \
  --set-string runtime.clusterName=dosh-log-stream-e2e \
  --set auth.existingSecret=dosh-auth \
  --set tls.existingSecret=dosh-tls \
  --wait --timeout 3m >"$ARTIFACT_DIR/helm-install.log" 2>&1
kubectl -n "$NAMESPACE" rollout status deployment/dosh --timeout=120s >"$ARTIFACT_DIR/rollout.log" 2>&1
[[ "$(kubectl -n "$NAMESPACE" get deployment dosh -o jsonpath='{.spec.template.spec.containers[0].image}')" == "${IMAGE_REPOSITORY}@${IMAGE_DIGEST}" ]] ||
  die "isolated Release is not pinned to the candidate digest"
[[ "$(kubectl -n "$NAMESPACE" get deployment dosh -o jsonpath='{.status.readyReplicas}/{.status.replicas}')" == "2/2" ]] ||
  die "isolated Release is not 2/2 Ready"
for _ in $(seq 1 60); do
  [[ "$(curl -sk -o /dev/null -w '%{http_code}' "$BASE/healthz" || true)" == 200 ]] && break
  sleep 1
done
[[ "$(curl -sk -o /dev/null -w '%{http_code}' "$BASE/healthz")" == 200 ]] || die "isolated Release health check failed"

log "Authenticating isolated local admin"
jq -n --arg username admin --arg password "$PASSWORD" '{username:$username,password:$password}' >"$WORKDIR/login.json"
[[ "$(curl -sk -c "$WORKDIR/admin.cookie" -H 'Content-Type: application/json' --data-binary @"$WORKDIR/login.json" \
  -o "$WORKDIR/session.json" -w '%{http_code}' "$BASE/api/v1/session")" == 200 ]] || die "local admin login failed"
rm -f "$WORKDIR/login.json"

log "Creating log emitter fixture"
kubectl create namespace "$TARGET_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: apps/v1
kind: Deployment
metadata:
  name: log-emitter
  namespace: ${TARGET_NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels: { app: log-emitter }
  template:
    metadata:
      labels: { app: log-emitter }
    spec:
      containers:
        - name: logger
          image: ${LOG_IMAGE}
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "-c"]
          args:
            - |
              i=0
              while true; do
                i=\$((i+1))
                printf 'dosh-log-%s\\n' "\$i"
                printf 'DOSH_HTML_MARKER <img src=x onerror=alert(1)>\\n'
                printf '\\033[31mDOSH_ANSI_RED\\033[0m\\n'
                sleep 1
              done
          resources:
            requests: { cpu: 10m, memory: 16Mi }
            limits: { cpu: 100m, memory: 64Mi }
EOF
kubectl -n "$TARGET_NAMESPACE" rollout status deployment/log-emitter --timeout=120s >"$ARTIFACT_DIR/log-emitter-rollout.log" 2>&1
POD=$(kubectl -n "$TARGET_NAMESPACE" get pod -l app=log-emitter -o jsonpath='{.items[0].metadata.name}')
[[ -n "$POD" ]] || die "log emitter Pod was not created"

log "Verifying bounded NDJSON completion frames"
HTTP_STATUS=$(curl -skN -D "$ARTIFACT_DIR/logs.headers" -b "$WORKDIR/admin.cookie" \
  "$BASE/api/v1/pods/${TARGET_NAMESPACE}/${POD}/logs?container=logger&tailLines=12&follow=false" \
  -o "$ARTIFACT_DIR/logs-frames.ndjson" -w '%{http_code}')
[[ "$HTTP_STATUS" == 200 ]] || die "non-follow logs status was ${HTTP_STATUS}"
grep -Fqi 'content-type: application/x-ndjson; charset=utf-8' "$ARTIFACT_DIR/logs.headers" || die "logs response is not NDJSON"
jq -e -s 'length > 1 and (.[-1].type == "end") and ([.[] | select(.type == "log")] | length > 0) and ([.[] | select(.type != "log" and .type != "end")] | length == 0)' \
  "$ARTIFACT_DIR/logs-frames.ndjson" >/dev/null || die "log completion frames are invalid"
jq -rs 'map(select(.type == "log") | .data) | join("")' "$ARTIFACT_DIR/logs-frames.ndjson" >"$ARTIFACT_DIR/logs.txt"
grep -Fq 'dosh-log-' "$ARTIFACT_DIR/logs.txt" || die "expected log content is missing"
grep -Fq 'DOSH_HTML_MARKER <img src=x onerror=alert(1)>' "$ARTIFACT_DIR/logs.txt" || die "HTML marker was not preserved as inert text bytes"
if grep -Fq '[log stream ended]' "$ARTIFACT_DIR/logs.txt"; then die "legacy plain-text sentinel leaked into log frames"; fi

log "Verifying pre-stream structured errors"
[[ "$(curl -sk -b "$WORKDIR/admin.cookie" -o "$ARTIFACT_DIR/missing-container.json" -w '%{http_code}' \
  "$BASE/api/v1/pods/${TARGET_NAMESPACE}/${POD}/logs?container=does-not-exist")" =~ ^(400|404)$ ]] || die "missing container error status is not 4xx"
jq -e '.code != "" and .message != "" and .requestId != ""' "$ARTIFACT_DIR/missing-container.json" >/dev/null || die "missing container error is not structured"
[[ "$(curl -sk -o "$ARTIFACT_DIR/unauthenticated.json" -w '%{http_code}' \
  "$BASE/api/v1/pods/${TARGET_NAMESPACE}/${POD}/logs?container=logger")" == 401 ]] || die "unauthenticated logs request was not rejected"
jq -e '.code == "unauthorized" and .requestId != ""' "$ARTIFACT_DIR/unauthenticated.json" >/dev/null || die "unauthenticated log error is not structured"

log "Verifying follow stream termination when the Pod disappears"
(timeout 60 curl -skN -b "$WORKDIR/admin.cookie" \
  "$BASE/api/v1/pods/${TARGET_NAMESPACE}/${POD}/logs?container=logger&follow=true&tailLines=3" \
  -o "$ARTIFACT_DIR/follow-frames.ndjson" 2>"$ARTIFACT_DIR/follow-curl.err"; printf '%s' "$?" >"$ARTIFACT_DIR/follow.exit") &
FOLLOW_PID=$!
for _ in $(seq 1 30); do
  [[ -s "$ARTIFACT_DIR/follow-frames.ndjson" ]] && break
  sleep 1
done
[[ -s "$ARTIFACT_DIR/follow-frames.ndjson" ]] || die "follow stream produced no frames"
kubectl -n "$TARGET_NAMESPACE" delete deployment log-emitter --wait=true --timeout=60s >/dev/null
wait "$FOLLOW_PID" || true
jq -e -s 'length > 0 and (.[-1].type == "end" or .[-1].type == "error")' "$ARTIFACT_DIR/follow-frames.ndjson" >/dev/null ||
  die "follow stream did not terminate with an end or structured error frame"

[[ "$(production_fingerprint)" == "$PRODUCTION_BEFORE" ]] || die "production fingerprint changed during isolated log test"
{
  printf 'image=%s@%s\n' "$IMAGE_REPOSITORY" "$IMAGE_DIGEST"
  printf 'release=%s/%s revision=%s\n' "$RELEASE" "$NAMESPACE" "$(helm -n "$NAMESPACE" list -o json | jq -r '.[0].revision')"
  printf 'log_stream=pass: application/x-ndjson, structured log/end frames, manual test log content preserved\n'
  printf 'structured_errors=pass: missing container 4xx Problem, unauthenticated 401 Problem, requestId present\n'
  printf 'follow_termination=pass: end or structured error frame after Pod deletion\n'
  printf 'production_unchanged=pass: %s\n' "$PRODUCTION_BEFORE"
  printf 'playwright_isolation=unchanged: workers=1, retries=0, serial per-browser containers, host flock\n'
} >"$ARTIFACT_DIR/summary.txt"

printf 'log stream E2E: pass\n'
printf 'artifact: %s\n' "$ARTIFACT_DIR"
printf 'candidate: %s@%s\n' "$IMAGE_REPOSITORY" "$IMAGE_DIGEST"
