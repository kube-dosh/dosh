#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/root/.local/bin:$PATH

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
NAMESPACE=${DOSH_E2E_AUDIT_NAMESPACE:-dosh-e2e-audit}
TARGET_NAMESPACE=${DOSH_E2E_AUDIT_TARGET_NAMESPACE:-dosh-e2e-audit-target}
RELEASE=${DOSH_E2E_AUDIT_RELEASE:-dosh-audit-e2e}
NODE_IP=${DOSH_E2E_NODE_IP:-192.168.122.100}
NODE_PORT=${DOSH_E2E_AUDIT_NODE_PORT:-32628}
AUDIT_HOST=${DOSH_E2E_AUDIT_HOST:-192.168.122.1}
AUDIT_PORT=${DOSH_E2E_AUDIT_PORT:-9444}
AUDIT_IDP_HOST=${DOSH_E2E_AUDIT_IDP_HOST:-192.168.122.1}
AUDIT_IDP_PORT=${DOSH_E2E_AUDIT_IDP_PORT:-9445}
AUDIT_TOKEN=${DOSH_E2E_AUDIT_TOKEN:-dosh-e2e-audit-token}
AUDIT_HMAC_KEY=${DOSH_E2E_AUDIT_HMAC_KEY:-dosh-e2e-audit-hmac}
AUDIT_BACKEND=${DOSH_E2E_AUDIT_BACKEND:-receiver}
LOKI_NAMESPACE=${DOSH_E2E_LOKI_NAMESPACE:-dosh-e2e-loki}
LOKI_IMAGE=${DOSH_E2E_LOKI_IMAGE:-192.168.122.10:5000/dosh/loki:3.5.4}
LOKI_IMAGE_DIGEST=${DOSH_E2E_LOKI_IMAGE_DIGEST:-sha256:98242d6a7f2403a1dc35ce256e906f7524a5d9aa166f22221c3aebb38d08643c}
LOKI_LOCAL_PORT=${DOSH_E2E_LOKI_LOCAL_PORT:-13100}
IMAGE_REPOSITORY=${DOSH_E2E_IMAGE_REPOSITORY:-192.168.122.10:5000/dosh/dosh}
IMAGE_TAG=${DOSH_E2E_IMAGE_TAG:-audit-e2e-$(date +%s)}
IMAGE_DIGEST=${DOSH_E2E_IMAGE_DIGEST:-}
SKIP_BUILD=${DOSH_E2E_SKIP_BUILD:-0}
KEEP_ARTIFACTS=${DOSH_E2E_KEEP_ARTIFACTS:-0}
KEEP_CLUSTER=${DOSH_E2E_KEEP_CLUSTER:-0}
ARTIFACT_DIR=${DOSH_E2E_ARTIFACT_DIR:-$(mktemp -d)}
EVIDENCE="$ARTIFACT_DIR/audit-webhook-evidence.log"
FIXTURE_STATE="$ARTIFACT_DIR/fixture.json"
FIXTURE_LOG="$ARTIFACT_DIR/oidc-fixture.log"
RECEIVER_LOG="$ARTIFACT_DIR/receiver.log"
LOKI_PORT_FORWARD_LOG="$ARTIFACT_DIR/loki-port-forward.log"
WORKDIR="$ARTIFACT_DIR/fixture-certs"
RECEIVER_PID=""
FIXTURE_PID=""
LOKI_PF_PID=""
DELIVERED_IDS="$ARTIFACT_DIR/audit-request-ids.txt"
LAST_STATUS=""
LAST_REQUEST_ID=""
LAST_BODY=""

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
record() { printf '%s\n' "$*" | tee -a "$EVIDENCE" >/dev/null; }

case "$AUDIT_BACKEND" in
  receiver|loki) ;;
  *) die "DOSH_E2E_AUDIT_BACKEND must be receiver or loki" ;;
esac

cleanup() {
  local rc=$?
  if [[ -n "$FIXTURE_PID" ]] && kill -0 "$FIXTURE_PID" 2>/dev/null; then
    kill -TERM "$FIXTURE_PID" 2>/dev/null || true
    wait "$FIXTURE_PID" 2>/dev/null || true
  fi
  if [[ -n "$RECEIVER_PID" ]] && kill -0 "$RECEIVER_PID" 2>/dev/null; then
    kill -TERM "$RECEIVER_PID" 2>/dev/null || true
    wait "$RECEIVER_PID" 2>/dev/null || true
  fi
  if [[ -n "$LOKI_PF_PID" ]] && kill -0 "$LOKI_PF_PID" 2>/dev/null; then
    kill -TERM "$LOKI_PF_PID" 2>/dev/null || true
    wait "$LOKI_PF_PID" 2>/dev/null || true
  fi
  if [[ "$KEEP_CLUSTER" != 1 ]]; then
    helm -n "$NAMESPACE" uninstall "$RELEASE" --wait --timeout 2m >/dev/null 2>&1 || true
    kubectl delete namespace "$NAMESPACE" "$TARGET_NAMESPACE" --ignore-not-found --wait=false >/dev/null 2>&1 || true
    kubectl delete clusterrole dosh-e2e-viewer-audit dosh-audit-e2e --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete clusterrolebinding dosh-e2e-viewers-audit dosh-e2e-admins-audit dosh-audit-e2e --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete namespace "$LOKI_NAMESPACE" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  fi
  if [[ $rc -eq 0 && "$KEEP_ARTIFACTS" != 1 ]]; then
    rm -rf "$ARTIFACT_DIR"
  else
    printf 'Audit Webhook E2E artifacts retained: %s\n' "$ARTIFACT_DIR"
  fi
}
trap cleanup EXIT

for command in kubectl helm curl openssl jq go make podman ss; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done
mkdir -p "$ARTIFACT_DIR" "$WORKDIR"
: >"$EVIDENCE"
: >"$DELIVERED_IDS"

log "Checking isolation and production invariants"
[[ "$(kubectl config current-context 2>/dev/null)" == "kubernetes-admin@kubedo-lab" ]] || \
  die "expected kubedo-lab context"
production_port=$(kubectl -n dosh get service dosh -o jsonpath='{.spec.ports[0].nodePort}')
[[ "$production_port" == 32626 ]] || die "production NodePort is $production_port, expected 32626"
[[ "$NODE_PORT" != "$production_port" ]] || die "audit test NodePort collides with production"
production_image=$(kubectl -n dosh get deployment dosh -o jsonpath='{.spec.template.spec.containers[0].image}')
production_revision=$(helm -n dosh history dosh -o json 2>/dev/null | jq -r 'max_by(.revision).revision')
cat >"$ARTIFACT_DIR/production-before.json" <<EOF
{"context":"kubernetes-admin@kubedo-lab","namespace":"dosh","image":"${production_image}","nodePort":${production_port},"revision":${production_revision}}
EOF
record "production_before image=${production_image} revision=${production_revision} nodePort=${production_port}"
record "candidate image=${IMAGE_REPOSITORY}:${IMAGE_TAG}${IMAGE_DIGEST:+@${IMAGE_DIGEST}} nodePort=${NODE_PORT}"
record "audit_backend=${AUDIT_BACKEND} loki_image=${LOKI_IMAGE}${LOKI_IMAGE_DIGEST:+@${LOKI_IMAGE_DIGEST}}"

if curl -sk --max-time 1 "https://${AUDIT_HOST}:${AUDIT_PORT}/healthz" >/dev/null 2>&1; then
  die "audit receiver port ${AUDIT_HOST}:${AUDIT_PORT} is already in use"
fi
[[ "$AUDIT_IDP_PORT" != "$AUDIT_PORT" ]] || die "audit IdP and receiver ports must differ"
if curl -sk --max-time 1 "https://${AUDIT_IDP_HOST}:${AUDIT_IDP_PORT}/healthz" >/dev/null 2>&1; then
  die "audit fixture IdP port ${AUDIT_IDP_HOST}:${AUDIT_IDP_PORT} is already in use"
fi
used_port=$(kubectl get service -A -o json | jq -r --argjson port "$NODE_PORT" '[.items[] | .spec.ports[]? | select(.nodePort == $port)] | length')
[[ "$used_port" == 0 ]] || die "NodePort ${NODE_PORT} is already in use"

log "Resetting stale dedicated audit fixture"
helm -n "$NAMESPACE" uninstall "$RELEASE" --wait --timeout 2m >/dev/null 2>&1 || true
kubectl delete namespace "$NAMESPACE" "$TARGET_NAMESPACE" --ignore-not-found --wait=true --timeout=120s >/dev/null 2>&1 || true
kubectl delete clusterrole dosh-e2e-viewer-audit dosh-audit-e2e --ignore-not-found >/dev/null 2>&1 || true
kubectl delete clusterrolebinding dosh-e2e-viewers-audit dosh-e2e-admins-audit dosh-audit-e2e --ignore-not-found >/dev/null 2>&1 || true

start_loki_backend() {
  [[ "$AUDIT_BACKEND" == "loki" ]] || return 0
  log "Starting real Loki backend with TLS and tenant authentication"
  if ss -H -ltn "sport = :${LOKI_LOCAL_PORT}" 2>/dev/null | grep -q .; then
    die "Loki port-forward port ${LOKI_LOCAL_PORT} is already in use"
  fi
  openssl req -x509 -newkey rsa:2048 -nodes -days 2 \
    -keyout "$WORKDIR/loki-ca.key" -out "$WORKDIR/loki-ca.crt" -subj '/CN=dosh-loki-e2e-ca' >/dev/null 2>&1
  cat >"$WORKDIR/loki.ext" <<EOF
subjectAltName=DNS:loki,DNS:loki.${LOKI_NAMESPACE}.svc,DNS:loki.${LOKI_NAMESPACE}.svc.cluster.local,DNS:localhost,IP:127.0.0.1
extendedKeyUsage=serverAuth
EOF
  openssl req -newkey rsa:2048 -nodes -keyout "$WORKDIR/loki.key" -out "$WORKDIR/loki.csr" \
    -subj '/CN=loki' >/dev/null 2>&1
  openssl x509 -req -days 2 -in "$WORKDIR/loki.csr" -CA "$WORKDIR/loki-ca.crt" -CAkey "$WORKDIR/loki-ca.key" \
    -CAcreateserial -out "$WORKDIR/loki.crt" -extfile "$WORKDIR/loki.ext" >/dev/null 2>&1
  cat >"$WORKDIR/loki-config.yml" <<'EOF'
auth_enabled: true

server:
  http_listen_port: 3100
  grpc_listen_port: 9096
  http_tls_config:
    cert_file: /etc/loki/tls/tls.crt
    key_file: /etc/loki/tls/tls.key

common:
  path_prefix: /var/lib/loki
  storage:
    filesystem:
      chunks_directory: /var/lib/loki/chunks
      rules_directory: /var/lib/loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory

schema_config:
  configs:
    - from: 2024-01-01
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

limits_config:
  retention_period: 24h
  allow_structured_metadata: true

compactor:
  working_directory: /var/lib/loki/compactor
  retention_enabled: true
  delete_request_store: filesystem

ruler:
  storage:
    type: local
    local:
      directory: /var/lib/loki/rules
EOF
  kubectl create namespace "$LOKI_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "$LOKI_NAMESPACE" create secret tls loki-tls \
    --cert="$WORKDIR/loki.crt" --key="$WORKDIR/loki.key" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "$LOKI_NAMESPACE" create configmap loki-config \
    --from-file=config.yml="$WORKDIR/loki-config.yml" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "$LOKI_NAMESPACE" apply -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: loki
  labels: { app: loki }
spec:
  replicas: 1
  selector:
    matchLabels: { app: loki }
  template:
    metadata:
      labels: { app: loki }
    spec:
      securityContext:
        fsGroup: 10001
        fsGroupChangePolicy: OnRootMismatch
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: loki
          image: ${LOKI_IMAGE}${LOKI_IMAGE_DIGEST:+@${LOKI_IMAGE_DIGEST}}
          imagePullPolicy: IfNotPresent
          args: ["-config.file=/etc/loki/config.yml"]
          ports:
            - name: http
              containerPort: 3100
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
          readinessProbe:
            httpGet:
              path: /ready
              port: http
              scheme: HTTPS
              httpHeaders:
                - name: X-Scope-OrgID
                  value: dosh
            periodSeconds: 2
            failureThreshold: 30
          livenessProbe:
            httpGet:
              path: /ready
              port: http
              scheme: HTTPS
              httpHeaders:
                - name: X-Scope-OrgID
                  value: dosh
            periodSeconds: 10
            failureThreshold: 6
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
            limits:
              cpu: "1"
              memory: 1Gi
          volumeMounts:
            - name: config
              mountPath: /etc/loki
              readOnly: true
            - name: tls
              mountPath: /etc/loki/tls
              readOnly: true
            - name: data
              mountPath: /var/lib/loki
            - name: tmp
              mountPath: /tmp
      volumes:
        - name: config
          configMap:
            name: loki-config
        - name: tls
          secret:
            secretName: loki-tls
        - name: data
          emptyDir:
            sizeLimit: 4Gi
        - name: tmp
          emptyDir:
            sizeLimit: 64Mi
---
apiVersion: v1
kind: Service
metadata:
  name: loki
  namespace: ${LOKI_NAMESPACE}
  labels: { app: loki }
spec:
  selector: { app: loki }
  ports:
    - name: http
      port: 3100
      targetPort: http
EOF
  kubectl -n "$LOKI_NAMESPACE" rollout status deployment/loki --timeout=180s >/dev/null
  kubectl -n "$LOKI_NAMESPACE" port-forward --address 127.0.0.1 svc/loki "${LOKI_LOCAL_PORT}:3100" >"$LOKI_PORT_FORWARD_LOG" 2>&1 &
  LOKI_PF_PID=$!
  for _ in $(seq 1 120); do
    curl -sSf --cacert "$WORKDIR/loki-ca.crt" -H 'X-Scope-OrgID: dosh' \
      "https://127.0.0.1:${LOKI_LOCAL_PORT}/ready" >/dev/null 2>&1 && break
    kill -0 "$LOKI_PF_PID" 2>/dev/null || { cat "$LOKI_PORT_FORWARD_LOG" >&2; die "Loki port-forward exited"; }
    sleep 0.25
  done
  curl -sSf --cacert "$WORKDIR/loki-ca.crt" -H 'X-Scope-OrgID: dosh' \
    "https://127.0.0.1:${LOKI_LOCAL_PORT}/ready" >/dev/null || die "Loki did not become ready"
  no_tenant=$(curl -sS -o /dev/null -w '%{http_code}' --cacert "$WORKDIR/loki-ca.crt" -G \
    --data-urlencode 'query={job="dosh-audit"}' \
    "https://127.0.0.1:${LOKI_LOCAL_PORT}/loki/api/v1/query_range")
  [[ "$no_tenant" == 401 ]] || die "Loki accepted a query without X-Scope-OrgID (status ${no_tenant})"
  record "loki_ready deployment=1/1 tls=private-ca tenant_auth=401 pod_security=restricted storage=emptyDir"
}

log "Generating private audit receiver CA and server certificate"
openssl req -x509 -newkey rsa:2048 -nodes -days 2 \
  -keyout "$WORKDIR/audit-ca.key" -out "$WORKDIR/audit-ca.crt" -subj '/CN=dosh-audit-e2e-ca' >/dev/null 2>&1
cat >"$WORKDIR/audit.ext" <<EOF
subjectAltName=IP:${AUDIT_HOST}
extendedKeyUsage=serverAuth
EOF
openssl req -newkey rsa:2048 -nodes -keyout "$WORKDIR/audit.key" -out "$WORKDIR/audit.csr" \
  -subj "/CN=${AUDIT_HOST}" >/dev/null 2>&1
openssl x509 -req -days 2 -in "$WORKDIR/audit.csr" -CA "$WORKDIR/audit-ca.crt" -CAkey "$WORKDIR/audit-ca.key" \
  -CAcreateserial -out "$WORKDIR/audit.crt" -extfile "$WORKDIR/audit.ext" >/dev/null 2>&1
AUDIT_CA_FILE="$WORKDIR/audit-ca.crt"

start_loki_backend

log "Building and starting the signed audit receiver"
(cd "$ROOT" && go build -o "$WORKDIR/audit-receiver" ./tools/auditreceiver)
receiver_env=(
  "AUDIT_BEARER_TOKEN=$AUDIT_TOKEN"
  "AUDIT_HMAC_KEY=$AUDIT_HMAC_KEY"
)
if [[ "$AUDIT_BACKEND" == "loki" ]]; then
  receiver_env+=(
    "AUDIT_LOKI_URL=https://127.0.0.1:${LOKI_LOCAL_PORT}"
    "AUDIT_LOKI_CA_FILE=$WORKDIR/loki-ca.crt"
    "AUDIT_LOKI_TENANT=dosh"
    "AUDIT_LOKI_TIMEOUT=2s"
  )
fi
env "${receiver_env[@]}" \
  "$WORKDIR/audit-receiver" -addr "${AUDIT_HOST}:${AUDIT_PORT}" \
  -cert "$WORKDIR/audit.crt" -key "$WORKDIR/audit.key" >"$RECEIVER_LOG" 2>&1 &
RECEIVER_PID=$!
for _ in $(seq 1 50); do
  curl -sSf --cacert "$AUDIT_CA_FILE" "https://${AUDIT_HOST}:${AUDIT_PORT}/healthz" >/dev/null 2>&1 && break
  kill -0 "$RECEIVER_PID" 2>/dev/null || { cat "$RECEIVER_LOG" >&2; die "audit receiver exited"; }
  sleep 0.2
done
curl -sSf --cacert "$AUDIT_CA_FILE" "https://${AUDIT_HOST}:${AUDIT_PORT}/healthz" >/dev/null || \
  die "audit receiver did not become healthy"
if [[ "$AUDIT_BACKEND" == "loki" ]]; then
  jq -e '.lokiConfigured == true' <(curl -sS --cacert "$AUDIT_CA_FILE" "https://${AUDIT_HOST}:${AUDIT_PORT}/stats") >/dev/null || \
    die "audit receiver is not forwarding to Loki"
  record "loki_gateway=ready endpoint=https://${AUDIT_HOST}:${AUDIT_PORT}/audit tenant=dosh"
fi

log "Starting isolated OIDC + audit fixture"
DOSH_E2E_NAMESPACE="$NAMESPACE" \
DOSH_E2E_TARGET_NAMESPACE="$TARGET_NAMESPACE" \
DOSH_E2E_RELEASE="$RELEASE" \
DOSH_E2E_NODE_IP="$NODE_IP" \
DOSH_E2E_NODE_PORT="$NODE_PORT" \
DOSH_E2E_IDP_HOST="$AUDIT_IDP_HOST" \
DOSH_E2E_IDP_PORT="$AUDIT_IDP_PORT" \
DOSH_E2E_IMAGE_REPOSITORY="$IMAGE_REPOSITORY" \
DOSH_E2E_IMAGE_TAG="$IMAGE_TAG" \
DOSH_E2E_IMAGE_DIGEST="$IMAGE_DIGEST" \
DOSH_E2E_SKIP_BUILD="$SKIP_BUILD" \
DOSH_E2E_HOLD=1 \
DOSH_E2E_STATE_FILE="$FIXTURE_STATE" \
DOSH_E2E_KEEP_WORKDIR=1 \
DOSH_E2E_RBAC_SUFFIX=-audit \
DOSH_E2E_RBAC_NAME=dosh-audit-e2e \
DOSH_E2E_AUDIT_ENABLED=1 \
DOSH_E2E_AUDIT_HOST="$AUDIT_HOST" \
DOSH_E2E_AUDIT_PORT="$AUDIT_PORT" \
DOSH_E2E_AUDIT_CA_FILE="$AUDIT_CA_FILE" \
DOSH_E2E_AUDIT_TOKEN="$AUDIT_TOKEN" \
DOSH_E2E_AUDIT_HMAC_KEY="$AUDIT_HMAC_KEY" \
DOSH_E2E_AUDIT_TIMEOUT=1s \
DOSH_E2E_AUDIT_QUEUE_SIZE=1024 \
  bash "$ROOT/scripts/e2e-oidc.sh" >"$FIXTURE_LOG" 2>&1 &
FIXTURE_PID=$!

for _ in $(seq 1 600); do
  [[ -s "$FIXTURE_STATE" ]] && break
  if ! kill -0 "$FIXTURE_PID" 2>/dev/null; then
    tail -n 240 "$FIXTURE_LOG" >&2 || true
    die "audit fixture exited before becoming ready"
  fi
  sleep 1
done
[[ -s "$FIXTURE_STATE" ]] || { tail -n 240 "$FIXTURE_LOG" >&2 || true; die "timed out waiting for fixture state"; }

BASE=$(jq -r '.base' "$FIXTURE_STATE")
FIXTURE_WORKDIR=$(jq -r '.workdir' "$FIXTURE_STATE")
[[ "$(jq -r '.auditEnabled' "$FIXTURE_STATE")" == true ]] || die "fixture did not enable audit"
[[ "$BASE" == "https://${NODE_IP}:${NODE_PORT}" ]] || die "unexpected fixture endpoint ${BASE}"
TEST_REVISION=$(helm -n "$NAMESPACE" history "$RELEASE" -o json | jq -r 'max_by(.revision).revision')
TEST_IMAGE=$(kubectl -n "$NAMESPACE" get deployment dosh -o jsonpath='{.spec.template.spec.containers[0].image}')
record "fixture_ready namespace=${NAMESPACE} release=${RELEASE} revision=${TEST_REVISION} image=${TEST_IMAGE} endpoint=${BASE}"

relogin_oidc_user() {
  local user=$1
  local jar="$FIXTURE_WORKDIR/${user}.cookie"
  local headers="$FIXTURE_WORKDIR/${user}.relogin.headers"
  local authz status
  curl -sk -D "$headers" -o /dev/null -c "$jar" \
    "$BASE/api/v1/auth/oidc/login?returnTo=%2Fworkloads%3Fnamespace%3D${TARGET_NAMESPACE}"
  authz=$(awk 'tolower($1)=="location:" {sub(/\r$/,"",$2); print $2}' "$headers" | tail -1)
  [[ -n "$authz" ]] || die "${user} relogin did not return an IdP redirect"
  if [[ "$authz" == *\?* ]]; then
    authz="${authz}&user=${user}"
  else
    authz="${authz}?user=${user}"
  fi
  status=$(curl -skL -b "$jar" -c "$jar" -o "$FIXTURE_WORKDIR/${user}.relogin.body" -w '%{http_code}' "$authz")
  [[ "$status" == 200 ]] || die "${user} relogin failed with HTTP ${status}"
  curl -sk -b "$jar" "$BASE/api/v1/session" >"$FIXTURE_WORKDIR/${user}.session.json"
  jq -e --arg username "${user}@example.test" \
    '.authMode == "oidc" and .user.username == $username and (.csrfToken | length > 0)' \
    "$FIXTURE_WORKDIR/${user}.session.json" >/dev/null || die "${user} relogin session is invalid"
}

# The OIDC fixture deliberately completes RP-initiated logout before HOLD. Restore
# all three sessions so audit scenarios exercise fresh authenticated requests.
for user in viewer operator admin; do
  relogin_oidc_user "$user"
done

receiver() {
  curl -sS --cacert "$AUDIT_CA_FILE" "$@"
}

receiver_control() {
  receiver -X POST -H 'Content-Type: application/json' --data "$1" "https://${AUDIT_HOST}:${AUDIT_PORT}/control" >/dev/null
}

receiver_stats() {
  receiver "https://${AUDIT_HOST}:${AUDIT_PORT}/stats"
}

csrf() {
  jq -r '.csrfToken' "$FIXTURE_WORKDIR/$1.session.json"
}

api_request() {
  local user=$1 method=$2 path=$3 data=${4:-}
  local headers body
  headers=$(mktemp "$ARTIFACT_DIR/headers.XXXXXX")
  body=$(mktemp "$ARTIFACT_DIR/body.XXXXXX")
  local args=(-sk -D "$headers" -o "$body" -w '%{http_code}' -b "$FIXTURE_WORKDIR/$user.cookie" -X "$method" "$BASE$path")
  if [[ "$method" != GET ]]; then
    args+=(-H "X-CSRF-Token: $(csrf "$user")" -H 'Content-Type: application/json')
  fi
  if [[ -n "$data" ]]; then
    args+=(--data "$data")
  fi
  LAST_STATUS=$(curl "${args[@]}")
  LAST_BODY=$body
  LAST_REQUEST_ID=$(awk 'tolower($1)=="x-request-id:" {gsub(/\r/,"",$2); print $2}' "$headers" | tail -1)
}

expect_status() {
  local user=$1 method=$2 path=$3 expected=$4 data=${5:-}
  api_request "$user" "$method" "$path" "$data"
  if [[ "$LAST_STATUS" != "$expected" ]]; then
    printf 'response body: %s\n' "$(tail -c 2000 "$LAST_BODY")" >&2
    die "${method} ${path} as ${user} returned ${LAST_STATUS}, expected ${expected}"
  fi
  [[ -n "$LAST_REQUEST_ID" ]] || die "${method} ${path} did not return X-Request-ID"
}

wait_event() {
  local request_id=$1 user=$2 action=$3 result=$4
  local stats
  for _ in $(seq 1 150); do
    stats=$(receiver_stats 2>/dev/null || true)
    if [[ -n "$stats" ]] && jq -e --arg id "$request_id" --arg user "$user" --arg action "$action" --arg result "$result" \
      '[.events[] | select(.requestId == $id and .user == $user and .action == $action and .result == $result and .verified == true)] | length == 1' \
      <<<"$stats" >/dev/null 2>&1; then
      grep -Fxq "$request_id" "$DELIVERED_IDS" || printf '%s\n' "$request_id" >>"$DELIVERED_IDS"
      return 0
    fi
    sleep 0.2
  done
  stats=$(receiver_stats 2>/dev/null || true)
  printf 'last receiver stats: %s\n' "$stats" >&2
  die "audit event ${request_id} (${user}/${action}/${result}) was not delivered"
}

wait_attempts() {
  local minimum=$1 received=$2
  local stats attempts events
  for _ in $(seq 1 150); do
    stats=$(receiver_stats 2>/dev/null || true)
    if [[ -n "$stats" ]]; then
      attempts=$(jq -r '.attempts' <<<"$stats")
      events=$(jq -r '.received' <<<"$stats")
      if (( attempts >= minimum && events == received )); then
        return 0
      fi
    fi
    sleep 0.2
  done
  printf 'last receiver stats: %s\n' "$(receiver_stats 2>/dev/null || true)" >&2
  die "receiver did not reach attempts>=${minimum}, received=${received}"
}

helm_upgrade() {
  helm upgrade "$RELEASE" "$ROOT/deploy/helm/dosh" -n "$NAMESPACE" --reuse-values "$@" --wait --timeout 3m >/dev/null
  kubectl -n "$NAMESPACE" rollout status deployment/dosh --timeout=120s >/dev/null
}

pod_logs() {
  kubectl -n "$NAMESPACE" logs deployment/dosh --tail=-1 2>/dev/null || true
}

log "Verifying baseline private-CA, token and HMAC delivery"
sleep 1
receiver_control '{"reset":true,"authMode":"both"}'
expect_status admin POST "/api/v1/secrets/${TARGET_NAMESPACE}/e2e-secret/reveal" 200
baseline_id=$LAST_REQUEST_ID
wait_event "$baseline_id" admin@example.test reveal success
record "private_ca_both id=${baseline_id} user=admin@example.test action=reveal result=success"

log "Verifying business success and denial event matrix"
expect_status viewer POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-demo/scale" 403 '{"replicas":2}'
viewer_id=$LAST_REQUEST_ID
wait_event "$viewer_id" viewer@example.test scale denied

expect_status operator POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-demo/scale" 200 '{"replicas":2}'
operator_id=$LAST_REQUEST_ID
wait_event "$operator_id" operator@example.test scale success

expect_status admin POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-admin/restart" 200
restart_id=$LAST_REQUEST_ID
wait_event "$restart_id" admin@example.test restart success

kubectl -n "$TARGET_NAMESPACE" apply -f - >/dev/null <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: e2e-audit-delete
spec:
  replicas: 0
  selector: { matchLabels: { app: e2e-audit-delete } }
  template:
    metadata: { labels: { app: e2e-audit-delete } }
    spec:
      containers:
        - name: demo
          image: 192.168.122.10:5000/dosh/dosh:0.1.2
          command: ["/usr/local/bin/dosh"]
EOF
expect_status admin DELETE "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-audit-delete" 202
delete_id=$LAST_REQUEST_ID
wait_event "$delete_id" admin@example.test delete success
for _ in $(seq 1 60); do
  kubectl -n "$TARGET_NAMESPACE" get deployment e2e-audit-delete >/dev/null 2>&1 || break
  sleep 0.5
done
kubectl -n "$TARGET_NAMESPACE" get deployment e2e-audit-delete >/dev/null 2>&1 && die "Deployment delete was not applied"
record "event_matrix scale_denied=${viewer_id} scale_success=${operator_id} restart=${restart_id} delete=${delete_id}"

log "Verifying validation failures are audited without writes"
expect_status admin POST "/api/v1/workloads/pod/${TARGET_NAMESPACE}/e2e-demo/scale" 501 '{"replicas":2}'
invalid_kind_id=$LAST_REQUEST_ID
wait_event "$invalid_kind_id" admin@example.test scale failure

expect_status admin POST "/api/v1/workloads/daemonset/${TARGET_NAMESPACE}/e2e-demo/scale" 501 '{"replicas":2}'
unsupported_scale_id=$LAST_REQUEST_ID
wait_event "$unsupported_scale_id" admin@example.test scale failure

expect_status admin POST "/api/v1/workloads/job/${TARGET_NAMESPACE}/e2e-demo/restart" 501
unsupported_restart_id=$LAST_REQUEST_ID
wait_event "$unsupported_restart_id" admin@example.test restart failure

expect_status admin DELETE "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-demo?propagation=Invalid" 400
invalid_propagation_id=$LAST_REQUEST_ID
wait_event "$invalid_propagation_id" admin@example.test delete failure
kubectl -n "$TARGET_NAMESPACE" get deployment e2e-demo >/dev/null || \
  die "invalid deletion propagation changed the target resource"
record "failure_audit invalid_kind=${invalid_kind_id} unsupported_scale=${unsupported_scale_id} unsupported_restart=${unsupported_restart_id} invalid_propagation=${invalid_propagation_id} resource_unchanged=true"

log "Verifying token-only delivery"
helm_upgrade --set audit.webhook.tokenKey=token --set audit.webhook.hmacKeyKey=
receiver_control '{"authMode":"token"}'
expect_status admin POST "/api/v1/secrets/${TARGET_NAMESPACE}/e2e-secret/reveal" 200
token_id=$LAST_REQUEST_ID
wait_event "$token_id" admin@example.test reveal success
record "token_only id=${token_id} authMode=token"

log "Verifying HMAC-only delivery"
helm_upgrade --set audit.webhook.tokenKey= --set audit.webhook.hmacKeyKey=hmac-key
receiver_control '{"authMode":"hmac"}'
expect_status admin POST "/api/v1/secrets/${TARGET_NAMESPACE}/e2e-secret/reveal" 200
hmac_id=$LAST_REQUEST_ID
wait_event "$hmac_id" admin@example.test reveal success
record "hmac_only id=${hmac_id} authMode=hmac"

log "Restoring both credentials and verifying transient 503 retry"
helm_upgrade --set audit.webhook.tokenKey=token --set audit.webhook.hmacKeyKey=hmac-key
receiver_control '{"reset":true,"authMode":"both","failStatus":503,"failCount":2}'
expect_status admin POST "/api/v1/secrets/${TARGET_NAMESPACE}/e2e-secret/reveal" 200
retry_id=$LAST_REQUEST_ID
wait_attempts 3 1
wait_event "$retry_id" admin@example.test reveal success
record "transient_503 id=${retry_id} attempts>=3 received=1"

log "Verifying persistent 503 is bounded and observable"
receiver_control '{"reset":true,"authMode":"both","failStatus":503,"failCount":100}'
expect_status admin POST "/api/v1/secrets/${TARGET_NAMESPACE}/e2e-secret/reveal" 200
persistent_id=$LAST_REQUEST_ID
wait_attempts 3 0
sleep 0.5
pod_logs | grep -Fq 'audit_webhook_delivery_failed' || die "persistent webhook failure was not logged"
record "persistent_503 id=${persistent_id} attempts=3 received=0 delivery_failed=logged"

log "Verifying timeout is bounded and recovery resumes delivery"
helm_upgrade --set-string audit.webhook.timeout=1s
receiver_control '{"reset":true,"authMode":"both","delayMillis":1500}'
expect_status admin POST "/api/v1/secrets/${TARGET_NAMESPACE}/e2e-secret/reveal" 200
timeout_id=$LAST_REQUEST_ID
wait_attempts 3 0
sleep 0.5
pod_logs | grep -Fq 'audit_webhook_delivery_failed' || die "timeout failure was not logged"
receiver_control '{"reset":true,"authMode":"both"}'
expect_status admin POST "/api/v1/secrets/${TARGET_NAMESPACE}/e2e-secret/reveal" 200
recovery_id=$LAST_REQUEST_ID
wait_event "$recovery_id" admin@example.test reveal success
record "timeout id=${timeout_id} attempts=3 received=0; recovery=${recovery_id}"
helm_upgrade --set-string audit.webhook.timeout=5s

log "Verifying bounded queue pressure does not block business requests"
helm_upgrade --set audit.webhook.queueSize=1 --set-string audit.webhook.timeout=1s
receiver_control '{"reset":true,"authMode":"both","delayMillis":1500}'
for _ in $(seq 1 8); do
  expect_status admin POST "/api/v1/secrets/${TARGET_NAMESPACE}/e2e-secret/reveal" 200
done
for _ in $(seq 1 30); do
  pod_logs | grep -Fq 'audit_webhook_queue_full' && break
  sleep 0.2
done
pod_logs | grep -Fq 'audit_webhook_queue_full' || die "queue saturation was not observable"
record "queue_full queueSize=1 businessRequests=200 audit_webhook_queue_full=logged"
helm_upgrade --set audit.webhook.queueSize=1024 --set-string audit.webhook.timeout=5s
receiver_control '{"reset":true,"authMode":"both"}'

log "Verifying read-only denial is audited without a write"
helm_upgrade --set runtime.readOnly=true
expect_status admin POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-demo/scale" 403 '{"replicas":1}'
readonly_id=$LAST_REQUEST_ID
wait_event "$readonly_id" admin@example.test scale denied
record "read_only_denied id=${readonly_id} result=denied"
helm_upgrade --set runtime.readOnly=false

log "Verifying SIGTERM drains queued audit records"
receiver_control '{"reset":true,"authMode":"both","delayMillis":300}'
ids_file="$ARTIFACT_DIR/sigterm-request-ids.txt"
: >"$ids_file"
for _ in $(seq 1 20); do
  expect_status admin POST "/api/v1/secrets/${TARGET_NAMESPACE}/e2e-secret/reveal" 200
  printf '%s\n' "$LAST_REQUEST_ID" >>"$ids_file"
done
old_pods=$(kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/instance="$RELEASE" -o name)
kubectl -n "$NAMESPACE" rollout restart deployment/dosh >/dev/null
kubectl -n "$NAMESPACE" rollout status deployment/dosh --timeout=120s >/dev/null
ids_json=$(jq -R -s 'split("\n") | map(select(length > 0))' "$ids_file")
for _ in $(seq 1 200); do
  stats=$(receiver_stats 2>/dev/null || true)
  if [[ -n "$stats" ]] && jq -e --argjson ids "$ids_json" \
    '([.events[].requestId] as $got | all($ids[]; . as $id | $got | index($id)))' <<<"$stats" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done
stats=$(receiver_stats)
jq -e --argjson ids "$ids_json" \
  '([.events[].requestId] as $got | all($ids[]; . as $id | $got | index($id)))' <<<"$stats" >/dev/null || \
  die "SIGTERM drain lost queued audit records"
for pod in $old_pods; do
  kubectl -n "$NAMESPACE" logs "$pod" --tail=-1 2>/dev/null | grep -F 'audit_flush_failed' && die "audit flush failed during SIGTERM" || true
done
record "sigterm_drain records=20 all_received=true"
cat "$ids_file" >>"$DELIVERED_IDS"
receiver_control '{"authMode":"both"}'

verify_loki_events() {
  [[ "$AUDIT_BACKEND" == "loki" ]] || return 0
  local start end response ids_json missing=0 event_count
  start="$(( $(date +%s) - 900 ))000000000"
  end="$(( $(date +%s) + 60 ))000000000"
  ids_json=$(jq -R -s 'split("\n") | map(select(length > 0)) | unique' "$DELIVERED_IDS")
  for _ in $(seq 1 120); do
    response=$(curl -sS -G --cacert "$WORKDIR/loki-ca.crt" -H 'X-Scope-OrgID: dosh' \
      --data-urlencode 'query={job="dosh-audit"}' \
      --data-urlencode "start=$start" --data-urlencode "end=$end" --data-urlencode 'limit=5000' \
      "https://127.0.0.1:${LOKI_LOCAL_PORT}/loki/api/v1/query_range" 2>/dev/null || true)
    if [[ -n "$response" ]] && jq -e --argjson ids "$ids_json" \
      '[.data.result[]?.values[]?[1] | fromjson? | .requestId] as $got | all($ids[]; . as $id | $got | index($id))' \
      <<<"$response" >/dev/null 2>&1; then
      break
    fi
    sleep 0.5
  done
  [[ -n "$response" ]] || die "Loki query returned no response"
  event_count=$(jq '[.data.result[]?.values[]?[1]] | length' <<<"$response")
  while IFS= read -r request_id; do
    jq -e --arg id "$request_id" \
      '[.data.result[]?.values[]?[1] | fromjson? | select(.requestId == $id)] | length >= 1' \
      <<<"$response" >/dev/null || { printf 'missing Loki requestId: %s\n' "$request_id" >&2; missing=$((missing + 1)); }
  done < <(jq -r '.[]' <<<"$ids_json")
  (( missing == 0 )) || die "Loki is missing ${missing} audited request IDs"
  jq -e '[.data.result[]?.stream | select(.job == "dosh-audit" and .service == "dosh")] | length > 0' \
    <<<"$response" >/dev/null || die "Loki stream labels are missing"
  wrong_tenant_count=$(curl -sS -G --cacert "$WORKDIR/loki-ca.crt" -H 'X-Scope-OrgID: other' \
    --data-urlencode 'query={job="dosh-audit"}' \
    --data-urlencode "start=$start" --data-urlencode "end=$end" \
    "https://127.0.0.1:${LOKI_LOCAL_PORT}/loki/api/v1/query_range" | jq '[.data.result[]?.values[]?[1]] | length')
  [[ "$wrong_tenant_count" == 0 ]] || die "Loki tenant isolation leaked ${wrong_tenant_count} events"
  record "loki_verified events=${event_count} request_ids=$(jq 'length' <<<"$ids_json") labels=job/service tenant_isolation=isolated"
}

verify_loki_events

log "Scanning logs for credential and Secret leakage"
scan_targets=("$RECEIVER_LOG" "$ARTIFACT_DIR/pod-logs.txt")
pod_logs >"$ARTIFACT_DIR/pod-logs.txt"
for value in "$AUDIT_TOKEN" "$AUDIT_HMAC_KEY" 'e2e-secret-value'; do
  if grep -Fq -- "$value" "${scan_targets[@]}"; then
    die "sensitive value was written to logs"
  fi
done
record "leak_scan token=0 hmac=0 secret=0"

log "Audit Webhook E2E passed"
FINAL_REVISION=$(helm -n "$NAMESPACE" history "$RELEASE" -o json | jq -r 'max_by(.revision).revision')
record "audit_result=pass namespace=${NAMESPACE} release=${RELEASE} revision=${FINAL_REVISION} image=${TEST_IMAGE} endpoint=${BASE}"
{
  printf '\nAudit Webhook E2E: pass\n'
  printf '  release: %s/%s\n' "$RELEASE" "$NAMESPACE"
  printf '  image: %s:%s%s\n' "$IMAGE_REPOSITORY" "$IMAGE_TAG" "${IMAGE_DIGEST:+@${IMAGE_DIGEST}}"
  printf '  helm: %s/%s revision %s (final)\n' "$NAMESPACE" "$RELEASE" "$FINAL_REVISION"
  printf '  endpoint: %s\n' "$BASE"
  printf '  receiver: https://%s:%s\n' "$AUDIT_HOST" "$AUDIT_PORT"
  printf '  evidence: %s\n' "$EVIDENCE"
} | tee -a "$EVIDENCE"

if [[ "$KEEP_ARTIFACTS" == 1 ]]; then
  printf 'Evidence retained at %s\n' "$EVIDENCE"
fi
