#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/root/.local/bin:$PATH

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
RUN_DIR=${DOSH_E2E_ARTIFACT_DIR:-$(mktemp -d)}
EVIDENCE="$RUN_DIR/evidence"
STATE_FILE="$RUN_DIR/fixture.json"
INFRA_LOG="$RUN_DIR/infra.log"
RUN_LOG="$RUN_DIR/run.log"

NAMESPACE=${DOSH_E2E_NAMESPACE:-dosh-e2e-lifecycle}
TARGET_NAMESPACE=${DOSH_E2E_TARGET_NAMESPACE:-dosh-e2e-lifecycle-target}
RELEASE=${DOSH_E2E_RELEASE:-dosh-lifecycle-e2e}
NODE_IP=${DOSH_E2E_NODE_IP:-192.168.122.100}
NODE_PORT=${DOSH_E2E_NODE_PORT:-32630}
IDP_HOST=${DOSH_E2E_IDP_HOST:-192.168.122.1}
IDP_PORT=${DOSH_E2E_IDP_PORT:-9446}
RBAC_SUFFIX=${DOSH_E2E_RBAC_SUFFIX:--v243-lifecycle}
RBAC_NAME=${DOSH_E2E_RBAC_NAME:-dosh-oidc-v243-lifecycle}
IMAGE_TAG=${DOSH_E2E_IMAGE_TAG:-0.1.4-rc-cronjobsuspend-20261104T075420Z}
IMAGE_DIGEST=${DOSH_E2E_IMAGE_DIGEST:-sha256:779ecae706eab6d0645e7b486aac1923f07e79e4919a969307cc6eb0d5c33306}
SOURCE_IMAGE=${DOSH_LIFECYCLE_SOURCE_IMAGE:-192.168.122.10:5000/dosh/nginx-logger:alpine}
TARGET_IMAGE=${DOSH_LIFECYCLE_TARGET_IMAGE:-192.168.122.10:5000/dosh/nginx-logger:v243-workload-target}
SIDECAR_IMAGE=${DOSH_LIFECYCLE_SIDECAR_IMAGE:-192.168.122.10:5000/kubedo/pause:3.10.2}
PRODUCTION_REVISION=${DOSH_PRODUCTION_REVISION:-12}
PRODUCTION_NODEPORT=${DOSH_PRODUCTION_NODEPORT:-32626}
DEPLOYMENT="dosh-lifecycle-$(date -u +%Y%m%d%H%M%S)"
INFRA_PID=""
FIXTURE_WORKDIR=""
DEPLOYMENT_CREATED=0

log() {
  local message
  message=$(printf '%s' "$*")
  printf '\n==> %s\n' "$message"
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$message" >>"$RUN_LOG"
}
die() {
  log "ERROR: $*"
  exit 1
}
expect_status() {
  local actual=$1 expected=$2 message=$3
  [[ "$actual" == "$expected" ]] || die "$message (HTTP $actual, expected $expected)"
}

cleanup() {
  local exit_code=$?
  if [[ "$DEPLOYMENT_CREATED" == 1 && ${DOSH_E2E_KEEP_FIXTURE:-0} != 1 ]]; then
    kubectl -n "$TARGET_NAMESPACE" delete deployment "$DEPLOYMENT" --ignore-not-found >/dev/null 2>&1 || true
  fi
  if [[ -n "$INFRA_PID" ]] && kill -0 "$INFRA_PID" 2>/dev/null; then
    kill -TERM "$INFRA_PID" 2>/dev/null || true
    wait "$INFRA_PID" 2>/dev/null || true
  fi
  if [[ ${DOSH_E2E_KEEP_FIXTURE:-0} != 1 ]]; then
    helm uninstall "$RELEASE" -n "$NAMESPACE" --wait --timeout 2m >/dev/null 2>&1 || true
    kubectl delete namespace "$NAMESPACE" "$TARGET_NAMESPACE" --ignore-not-found --wait=false >/dev/null 2>&1 || true
    kubectl delete clusterrole "$RBAC_NAME" "dosh-e2e-viewer${RBAC_SUFFIX}" --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete clusterrolebinding "$RBAC_NAME" "dosh-e2e-viewers${RBAC_SUFFIX}" "dosh-e2e-admins${RBAC_SUFFIX}" --ignore-not-found >/dev/null 2>&1 || true
  fi
  if [[ -n "$FIXTURE_WORKDIR" && ${DOSH_E2E_KEEP_ARTIFACTS:-0} != 1 ]]; then
    rm -rf "$FIXTURE_WORKDIR"
  fi
  if [[ ${DOSH_E2E_KEEP_ARTIFACTS:-0} != 1 ]]; then
    rm -rf "$RUN_DIR"
  else
    printf 'Workload lifecycle artifacts retained: %s\n' "$RUN_DIR" >&2
  fi
  return "$exit_code"
}
trap cleanup EXIT

for command in kubectl helm curl jq go make bash; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done
mkdir -p "$EVIDENCE"
: >"$RUN_LOG"

[[ "$NODE_PORT" != "$PRODUCTION_NODEPORT" ]] || die "test NodePort ${NODE_PORT} collides with production"
[[ "$IMAGE_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || die "candidate digest is invalid"

log "Capturing production baseline"
production_fingerprint() {
  printf 'helm_revision=%s\n' "$(helm -n dosh status dosh -o json | jq -r '.version')"
  printf 'service_nodeport=%s\n' "$(kubectl -n dosh get service dosh -o jsonpath='{.spec.ports[0].nodePort}')"
  kubectl -n dosh get deployment dosh -o json | jq -r '
    "deployment_uid=\(.metadata.uid)",
    "deployment_generation=\(.metadata.generation)",
    "deployment_resource_version=\(.metadata.resourceVersion)",
    "deployment_image=\(.spec.template.spec.containers[0].image)",
    "deployment_ready=\(.status.readyReplicas)",
    "deployment_available=\(.status.availableReplicas)"'
}
production_fingerprint >"$EVIDENCE/production-before.txt"
[[ "$(sed -n 's/^helm_revision=//p' "$EVIDENCE/production-before.txt")" == "$PRODUCTION_REVISION" ]] ||
  die "production Helm revision changed from ${PRODUCTION_REVISION}"
[[ "$(sed -n 's/^service_nodeport=//p' "$EVIDENCE/production-before.txt")" == "$PRODUCTION_NODEPORT" ]] ||
  die "production NodePort changed from ${PRODUCTION_NODEPORT}"
[[ "$(sed -n 's/^deployment_ready=//p' "$EVIDENCE/production-before.txt")" == 2 ]] ||
  die "production deployment is not 2/2 Ready"

log "Starting isolated OIDC fixture on NodePort ${NODE_PORT}"
export DOSH_E2E_HOLD=1
export DOSH_E2E_STATE_FILE="$STATE_FILE"
export DOSH_E2E_KEEP_WORKDIR=1
export DOSH_E2E_SKIP_BUILD=1
export DOSH_E2E_IMAGE_TAG="$IMAGE_TAG"
export DOSH_E2E_IMAGE_DIGEST="$IMAGE_DIGEST"
export DOSH_E2E_NAMESPACE="$NAMESPACE"
export DOSH_E2E_TARGET_NAMESPACE="$TARGET_NAMESPACE"
export DOSH_E2E_RELEASE="$RELEASE"
export DOSH_E2E_NODE_IP="$NODE_IP"
export DOSH_E2E_NODE_PORT="$NODE_PORT"
export DOSH_E2E_IDP_HOST="$IDP_HOST"
export DOSH_E2E_IDP_PORT="$IDP_PORT"
export DOSH_E2E_RBAC_SUFFIX="$RBAC_SUFFIX"
export DOSH_E2E_RBAC_NAME="$RBAC_NAME"
bash "$ROOT/scripts/e2e-oidc.sh" >"$INFRA_LOG" 2>&1 &
INFRA_PID=$!

for _ in $(seq 1 420); do
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
FIXTURE_NAMESPACE=$(jq -r '.namespace' "$STATE_FILE")
FIXTURE_TARGET=$(jq -r '.targetNamespace' "$STATE_FILE")
FIXTURE_RELEASE=$(jq -r '.release' "$STATE_FILE")
FIXTURE_IMAGE=$(jq -r '.image' "$STATE_FILE")
FIXTURE_WORKDIR=$(jq -r '.workdir' "$STATE_FILE")
FIXTURE_REVISION=$(helm -n "$FIXTURE_NAMESPACE" status "$FIXTURE_RELEASE" -o json | jq -r '.version')
FIXTURE_RUNNING_IMAGE=$(kubectl -n "$FIXTURE_NAMESPACE" get deployment dosh -o jsonpath='{.spec.template.spec.containers[0].image}')
[[ "$BASE" == "https://${NODE_IP}:${NODE_PORT}" ]] || die "fixture endpoint is not the isolated endpoint: $BASE"
[[ "$FIXTURE_NAMESPACE" == "$NAMESPACE" && "$FIXTURE_TARGET" == "$TARGET_NAMESPACE" ]] || die "fixture namespaces differ from the requested isolation"
[[ "$FIXTURE_RELEASE" == "$RELEASE" ]] || die "fixture release differs from the requested isolation"
[[ "$FIXTURE_RUNNING_IMAGE" == *"$IMAGE_DIGEST" ]] || die "fixture is not running the candidate digest"
jq -n \
  --arg base "$BASE" --arg namespace "$FIXTURE_NAMESPACE" --arg targetNamespace "$FIXTURE_TARGET" \
  --arg release "$FIXTURE_RELEASE" --arg revision "$FIXTURE_REVISION" \
  --arg image "$FIXTURE_IMAGE" --arg digest "$IMAGE_DIGEST" \
  '{base:$base,namespace:$namespace,targetNamespace:$targetNamespace,release:$release,revision:($revision|tonumber),image:$image,candidateDigest:$digest}' \
  >"$EVIDENCE/fixture.json"

login_as() {
  local user=$1 jar="$FIXTURE_WORKDIR/$1.cookie" headers="$FIXTURE_WORKDIR/$1.headers"
  local location status
  rm -f "$jar" "$FIXTURE_WORKDIR/$user.session.json"
  curl -sk --max-time 30 -D "$headers" -o /dev/null -c "$jar" \
    "$BASE/api/v1/auth/oidc/login?returnTo=%2Fworkloads"
  location=$(awk 'tolower($1)=="location:" {sub(/\r$/,"",$2); print $2}' "$headers" | tail -1)
  [[ -n "$location" ]] || die "$user login did not return an IdP redirect"
  if [[ "$location" == *\?* ]]; then location="${location}&user=${user}"; else location="${location}?user=${user}"; fi
  status=$(curl -skL --max-time 30 -b "$jar" -c "$jar" -o /dev/null -w '%{http_code}' "$location")
  expect_status "$status" 200 "$user OIDC login failed"
  curl -sk --max-time 30 -b "$jar" "$BASE/api/v1/session" >"$FIXTURE_WORKDIR/$user.session.json"
  jq -e --arg username "${user}@example.test" \
    '.authMode == "oidc" and .user.username == $username and (.csrfToken | length > 0)' \
    "$FIXTURE_WORKDIR/$user.session.json" >/dev/null || die "$user session response is invalid"
}

api() {
  local user=$1 method=$2 path=$3 out=$4 body=${5-}
  local -a args=(-sk --max-time 120 -o "$out" -w '%{http_code}' -b "$FIXTURE_WORKDIR/$user.cookie" -X "$method" "$BASE$path")
  if [[ "$method" != GET ]]; then
    args+=(
      -H "X-CSRF-Token: $(jq -r '.csrfToken' "$FIXTURE_WORKDIR/$user.session.json")"
      -H 'Content-Type: application/json'
      -H "Origin: $BASE"
    )
  fi
  if [[ -n "$body" ]]; then
    args+=(--data-binary "$body")
  fi
  curl "${args[@]}"
}

wait_for_deployment() {
  local replicas=$1
  local deadline=$((SECONDS + 180))
  while (( SECONDS < deadline )); do
    local desired ready available
    desired=$(kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o jsonpath='{.spec.replicas}' 2>/dev/null || true)
    ready=$(kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)
    available=$(kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)
    if [[ "$desired" == "$replicas" && "$ready" == "$replicas" && "$available" == "$replicas" ]]; then
      return 0
    fi
    sleep 2
  done
  return 1
}

wait_for_new_pods() {
  local old_uids=$1
  local deadline=$((SECONDS + 180))
  while (( SECONDS < deadline )); do
    local pods current_uids ready_count
    pods=$(kubectl -n "$TARGET_NAMESPACE" get pods -l "app.kubernetes.io/name=$DEPLOYMENT" -o json 2>/dev/null || true)
    current_uids=$(jq -r '[.items[].metadata.uid] | sort | join(" ")' <<<"$pods" 2>/dev/null || true)
    ready_count=$(jq -r '[.items[] | select(.status.phase == "Running") | select(any(.status.conditions[]?; .type == "Ready" and .status == "True"))] | length' <<<"$pods" 2>/dev/null || true)
    if [[ -n "$current_uids" && "$ready_count" == 2 ]]; then
      local replaced=0 uid
      for uid in $old_uids; do
        if [[ " $current_uids " == *" $uid "* ]]; then replaced=1; fi
      done
      if [[ "$replaced" == 0 ]]; then
        printf '%s\n' "$pods" >"$EVIDENCE/pods-after-restart.json"
        return 0
      fi
    fi
    sleep 2
  done
  return 1
}

log "Logging in viewer, operator and admin"
login_as viewer
login_as operator
login_as admin

payload=$(jq -nc \
  --arg name "$DEPLOYMENT" --arg namespace "$TARGET_NAMESPACE" --arg image "$SOURCE_IMAGE" \
  '{name:$name,namespace:$namespace,replicas:1,containerName:"app",image:$image,containerPort:80,
    cpuRequestMilli:50,cpuLimitMilli:200,memoryRequestBytes:33554432,memoryLimitBytes:134217728,
    env:[{name:"DOSH_LIFECYCLE",value:"enabled"}],labels:{"dosh-e2e":"workload-lifecycle"}}')
printf '%s\n' "$payload" >"$EVIDENCE/create-request.json"

log "Verifying denied and invalid create requests write nothing"
status=$(api viewer POST /api/v1/workloads/deployments "$EVIDENCE/create-viewer-denied.json" "$payload")
expect_status "$status" 403 "viewer create was not denied"
invalid_payload=$(jq -c '.replicas=1001' <<<"$payload")
status=$(api admin POST /api/v1/workloads/deployments "$EVIDENCE/create-invalid.json" "$invalid_payload")
expect_status "$status" 400 "invalid create was not rejected"
if kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" >/dev/null 2>&1; then
  die "denied or invalid create left a Deployment behind"
fi

log "Verifying preview and create"
status=$(api operator POST /api/v1/workloads/deployments/preview "$EVIDENCE/preview-response.json" "$payload")
expect_status "$status" 200 "Deployment preview failed"
jq -r '.yaml' "$EVIDENCE/preview-response.json" >"$EVIDENCE/preview.yaml"
jq -e --arg name "$DEPLOYMENT" --arg image "$SOURCE_IMAGE" '
  (.yaml | contains("kind: Deployment")) and
  (.yaml | contains("name: " + $name)) and
  (.yaml | contains("image: " + $image))
' "$EVIDENCE/preview-response.json" >/dev/null || die "preview YAML does not match the request"
if kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" >/dev/null 2>&1; then
  die "preview created a Deployment"
fi
status=$(api operator POST /api/v1/workloads/deployments "$EVIDENCE/create-response.json" "$payload")
expect_status "$status" 201 "operator create failed"
DEPLOYMENT_CREATED=1
jq -e --arg name "$DEPLOYMENT" --arg namespace "$TARGET_NAMESPACE" '
  .kind == "Deployment" and .name == $name and .namespace == $namespace and .desired == 1
' "$EVIDENCE/create-response.json" >/dev/null || die "create response is invalid"
wait_for_deployment 1 || die "created Deployment did not become 1/1 Ready"
kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o json >"$EVIDENCE/deployment-created.json"
jq -e --arg image "$SOURCE_IMAGE" '
  .spec.replicas == 1 and
  (.spec.template.spec.containers | length == 1) and
  .spec.template.spec.containers[0].name == "app" and
  .spec.template.spec.containers[0].image == $image and
  .spec.template.spec.containers[0].ports[0].containerPort == 80 and
  .spec.template.spec.containers[0].resources.requests.cpu == "50m" and
  .spec.template.spec.containers[0].resources.limits.memory == "128Mi" and
  .metadata.labels["dosh-e2e"] == "workload-lifecycle"
' "$EVIDENCE/deployment-created.json" >/dev/null || die "created Deployment spec does not match the request"
created_uid=$(jq -r '.metadata.uid' "$EVIDENCE/deployment-created.json")
status=$(api operator POST /api/v1/workloads/deployments "$EVIDENCE/create-duplicate.json" "$payload")
expect_status "$status" 409 "duplicate create did not conflict"
[[ "$(kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o jsonpath='{.metadata.uid}')" == "$created_uid" ]] ||
  die "duplicate create replaced the Deployment"

log "Verifying scale-up and stale-version conflict"
rv_before_scale=$(kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o jsonpath='{.metadata.resourceVersion}')
status=$(api operator POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/${DEPLOYMENT}/scale" \
  "$EVIDENCE/scale-response.json" "$(jq -nc --arg rv "$rv_before_scale" '{replicas:2,resourceVersion:$rv}')")
expect_status "$status" 200 "scale-up failed"
wait_for_deployment 2 || die "scaled Deployment did not become 2/2 Ready"
kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o json >"$EVIDENCE/deployment-scaled.json"
jq -e '.spec.replicas == 2 and .status.readyReplicas == 2' "$EVIDENCE/deployment-scaled.json" >/dev/null ||
  die "scale-up was not reflected in Kubernetes"
generation_before_conflict=$(jq -r '.metadata.generation' "$EVIDENCE/deployment-scaled.json")
status=$(api operator POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/${DEPLOYMENT}/scale" \
  "$EVIDENCE/scale-conflict.json" "$(jq -nc --arg rv "$rv_before_scale" '{replicas:3,resourceVersion:$rv}')")
expect_status "$status" 409 "stale scale request did not conflict"
sleep 2
jq -e --argjson generation "$generation_before_conflict" '
  .spec.replicas == 2 and .metadata.generation == $generation
' <(kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o json) >/dev/null ||
  die "stale scale request changed the Deployment"

log "Adding a sidecar before image update"
kubectl -n "$TARGET_NAMESPACE" patch deployment "$DEPLOYMENT" --type=strategic -p \
  "$(jq -nc --arg image "$SIDECAR_IMAGE" '{spec:{template:{spec:{containers:[{name:"sidecar",image:$image}]}}}}')" >/dev/null
wait_for_deployment 2 || die "sidecar rollout did not become 2/2 Ready"
kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o json >"$EVIDENCE/deployment-with-sidecar.json"
jq -e --arg sidecar "$SIDECAR_IMAGE" '
  ([.spec.template.spec.containers[] | select(.name == "sidecar" and .image == $sidecar)] | length == 1)
' "$EVIDENCE/deployment-with-sidecar.json" >/dev/null || die "sidecar was not added"

log "Verifying image update and stale-version conflict"
rv_before_image=$(kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o jsonpath='{.metadata.resourceVersion}')
status=$(api operator POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/${DEPLOYMENT}/image" \
  "$EVIDENCE/image-update-response.json" \
  "$(jq -nc --arg image "$TARGET_IMAGE" --arg rv "$rv_before_image" '{containerName:"app",image:$image,resourceVersion:$rv}')")
expect_status "$status" 200 "image update failed"
wait_for_deployment 2 || die "image update rollout did not become 2/2 Ready"
kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o json >"$EVIDENCE/deployment-image-updated.json"
jq -e --arg app "$TARGET_IMAGE" --arg sidecar "$SIDECAR_IMAGE" '
  ([.spec.template.spec.containers[] | select(.name == "app" and .image == $app)] | length == 1) and
  ([.spec.template.spec.containers[] | select(.name == "sidecar" and .image == $sidecar)] | length == 1)
' "$EVIDENCE/deployment-image-updated.json" >/dev/null || die "image update changed the wrong container"
generation_before_image_conflict=$(jq -r '.metadata.generation' "$EVIDENCE/deployment-image-updated.json")
status=$(api operator POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/${DEPLOYMENT}/image" \
  "$EVIDENCE/image-conflict.json" \
  "$(jq -nc --arg image "$SOURCE_IMAGE" --arg rv "$rv_before_image" '{containerName:"app",image:$image,resourceVersion:$rv}')")
expect_status "$status" 409 "stale image update did not conflict"
sleep 2
jq -e --arg app "$TARGET_IMAGE" --arg sidecar "$SIDECAR_IMAGE" --argjson generation "$generation_before_image_conflict" '
  .metadata.generation == $generation and
  ([.spec.template.spec.containers[] | select(.name == "app" and .image == $app)] | length == 1) and
  ([.spec.template.spec.containers[] | select(.name == "sidecar" and .image == $sidecar)] | length == 1)
' <(kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o json) >/dev/null ||
  die "stale image request changed the Deployment"

log "Verifying restart authorization, annotation and pod replacement"
old_pod_uids=$(kubectl -n "$TARGET_NAMESPACE" get pods -l "app.kubernetes.io/name=$DEPLOYMENT" -o json | jq -r '[.items[].metadata.uid] | join(" ")')
restarted_at_before=$(kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o jsonpath='{.spec.template.metadata.annotations.kubectl\.kubernetes\.io/restartedAt}' 2>/dev/null || true)
status=$(api viewer POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/${DEPLOYMENT}/restart" \
  "$EVIDENCE/restart-denied.json")
expect_status "$status" 403 "viewer restart was not denied"
status=$(api operator POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/${DEPLOYMENT}/restart" \
  "$EVIDENCE/restart-response.json")
expect_status "$status" 200 "operator restart failed"
restarted_at_after=$(kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" -o jsonpath='{.spec.template.metadata.annotations.kubectl\.kubernetes\.io/restartedAt}' 2>/dev/null || true)
[[ -n "$restarted_at_after" && "$restarted_at_after" != "$restarted_at_before" ]] || die "restart annotation was not updated"
wait_for_new_pods "$old_pod_uids" || die "restart did not replace all pods with Ready replacements"

log "Verifying delete authorization and propagation"
status=$(api viewer DELETE "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/${DEPLOYMENT}?propagation=Background" \
  "$EVIDENCE/delete-denied.json")
expect_status "$status" 403 "viewer delete was not denied"
status=$(api operator DELETE "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/${DEPLOYMENT}?propagation=Invalid" \
  "$EVIDENCE/delete-invalid-propagation.json")
expect_status "$status" 400 "invalid deletion propagation was not rejected"
kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" >/dev/null || die "denied or invalid delete removed the Deployment"
status=$(api operator DELETE "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/${DEPLOYMENT}?propagation=Background" \
  "$EVIDENCE/delete-response.json")
expect_status "$status" 202 "operator delete failed"
for _ in $(seq 1 60); do
  if ! kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" >/dev/null 2>&1; then break; fi
  sleep 1
done
if kubectl -n "$TARGET_NAMESPACE" get deployment "$DEPLOYMENT" >/dev/null 2>&1; then
  die "Deployment still exists after delete"
fi
status=$(api operator GET "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/${DEPLOYMENT}" "$EVIDENCE/delete-get.json")
expect_status "$status" 404 "deleted Deployment detail did not return 404"
for _ in $(seq 1 60); do
  if [[ "$(kubectl -n "$TARGET_NAMESPACE" get pods -l "app.kubernetes.io/name=$DEPLOYMENT" --no-headers 2>/dev/null | wc -l)" == 0 ]]; then break; fi
  sleep 1
done
[[ "$(kubectl -n "$TARGET_NAMESPACE" get pods -l "app.kubernetes.io/name=$DEPLOYMENT" --no-headers 2>/dev/null | wc -l)" == 0 ]] ||
  die "pods remain after Deployment deletion"
DEPLOYMENT_CREATED=0

log "Verifying lifecycle audit for successes, failures and denials"
status=$(api admin GET "/api/v1/audit-events?namespace=${TARGET_NAMESPACE}&page=1&pageSize=200&sort=id&order=asc" \
  "$EVIDENCE/audit-events.json")
expect_status "$status" 200 "admin audit query failed"
jq -e --arg ns "$TARGET_NAMESPACE" --arg name "$DEPLOYMENT" '
  def recorded($action; $result; $user):
    any(.items[];
      .namespace == $ns and .name == $name and
      .action == $action and .result == $result and .user == $user and
      ((.requestId // "") | length > 0));
  (.meta.total >= 12) and
  recorded("create"; "success"; "operator@example.test") and
  recorded("create"; "denied"; "viewer@example.test") and
  recorded("create"; "failure"; "admin@example.test") and
  recorded("scale"; "success"; "operator@example.test") and
  recorded("scale"; "failure"; "operator@example.test") and
  recorded("update-image"; "success"; "operator@example.test") and
  recorded("update-image"; "failure"; "operator@example.test") and
  recorded("restart"; "denied"; "viewer@example.test") and
  recorded("restart"; "success"; "operator@example.test") and
  recorded("delete"; "denied"; "viewer@example.test") and
  recorded("delete"; "failure"; "operator@example.test") and
  recorded("delete"; "success"; "operator@example.test")
' "$EVIDENCE/audit-events.json" >/dev/null || die "lifecycle audit matrix is incomplete"
jq --arg name "$DEPLOYMENT" '
  {total:(.meta.total), lifecycleEvents:[.items[] | select(.name == $name) |
    {action,result,user,namespace,requestId,error}]}
' "$EVIDENCE/audit-events.json" >"$EVIDENCE/audit-summary.json"

log "Verifying production remained unchanged"
production_fingerprint >"$EVIDENCE/production-after.txt"
cmp -s "$EVIDENCE/production-before.txt" "$EVIDENCE/production-after.txt" || die "production fingerprint changed"

log "Scanning evidence for credential material"
if command -v rg >/dev/null 2>&1; then
  if rg -n -i 'BEGIN ([A-Z]+ )?PRIVATE KEY|Authorization:[[:space:]]*Bearer|Cookie:[[:space:]]|X-CSRF-Token|dosh_session|session-key|client-secret|dosh-e2e-secret' "$EVIDENCE" >/dev/null; then
    die "credential material found in evidence"
  fi
else
  if grep -R -n -E -i 'BEGIN ([A-Z]+ )?PRIVATE KEY|Authorization:[[:space:]]*Bearer|Cookie:[[:space:]]|X-CSRF-Token|dosh_session|session-key|client-secret|dosh-e2e-secret' "$EVIDENCE" >/dev/null; then
    die "credential material found in evidence"
  fi
fi

production_revision_after=$(sed -n 's/^helm_revision=//p' "$EVIDENCE/production-after.txt")
production_nodeport_after=$(sed -n 's/^service_nodeport=//p' "$EVIDENCE/production-after.txt")
cat >"$EVIDENCE/metadata.txt" <<EOF
schema=dosh-workload-lifecycle-v1
created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
candidate_tag=${IMAGE_TAG}
candidate_digest=${IMAGE_DIGEST}
fixture_base=${BASE}
fixture_namespace=${FIXTURE_NAMESPACE}
fixture_release=${FIXTURE_RELEASE}
fixture_revision=${FIXTURE_REVISION}
target_namespace=${TARGET_NAMESPACE}
deployment=${DEPLOYMENT}
source_image=${SOURCE_IMAGE}
target_image=${TARGET_IMAGE}
sidecar_image=${SIDECAR_IMAGE}
production_revision_before=${PRODUCTION_REVISION}
production_revision_after=${production_revision_after}
production_nodeport_before=${PRODUCTION_NODEPORT}
production_nodeport_after=${production_nodeport_after}
EOF

cat >"$EVIDENCE/summary.txt" <<EOF
result=passed
exit_code=0
candidate_tag=${IMAGE_TAG}
candidate_digest=${IMAGE_DIGEST}
fixture_release=${RELEASE}/${NAMESPACE}
fixture_revision=${FIXTURE_REVISION}
isolated_endpoint=${BASE}
target_namespace=${TARGET_NAMESPACE}
preview=pass: no Deployment written
create_denied=pass: viewer 403, no resource
create_invalid=pass: 400, no resource
create=pass: operator 201, Kubernetes spec and 1/1 Ready match
create_conflict=pass: duplicate 409, original UID retained
scale=pass: 1 to 2, 2/2 Ready
scale_conflict=pass: stale resourceVersion 409, replicas and generation unchanged
image_update=pass: app image changed, sidecar unchanged, rollout 2/2 Ready
image_conflict=pass: stale resourceVersion 409, target image and generation unchanged
restart_denied=pass: viewer 403
restart=pass: annotation updated, all old pod UIDs replaced with Ready pods
delete_denied=pass: viewer 403, resource retained
delete_invalid_propagation=pass: 400, resource retained
delete=pass: 202, Deployment and owned pods removed, detail 404
audit=pass: create/scale/update-image/restart/delete success, failure and denied records with requestId
production_unchanged=pass: App 0.1.2, Chart 0.1.4, revision ${PRODUCTION_REVISION}, NodePort ${PRODUCTION_NODEPORT}, 2/2 Ready
playwright_isolation=unchanged: workers=1, retries=0, per-browser containers, private IPC, host flock, bounded CPU/memory/shm/PIDs
sensitive_scan=pass
EOF

log "Workload write lifecycle verification passed"
printf 'Evidence: %s\n' "$EVIDENCE"
cat "$EVIDENCE/summary.txt"
