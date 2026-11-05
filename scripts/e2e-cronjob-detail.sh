#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/root/.local/bin:$PATH

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
NAMESPACE=${DOSH_E2E_NAMESPACE:-dosh-e2e-oidc}
RELEASE=${DOSH_E2E_RELEASE:-dosh-oidc-e2e}
TARGET_NAMESPACE=${DOSH_E2E_TARGET_NAMESPACE:-dosh-e2e-target}
NODE_IP=${DOSH_E2E_NODE_IP:-192.168.122.100}
NODE_PORT=${DOSH_E2E_NODE_PORT:-32627}
BASE_URL=${DOSH_E2E_BASE_URL:-https://${NODE_IP}:${NODE_PORT}}
CRONJOB_NAME=${DOSH_E2E_CRONJOB_NAME:-dosh-cronjob-detail-check}
LOGIN_USER=${DOSH_E2E_LOGIN_USER:-viewer}
RUN_ID=${DOSH_E2E_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}
ARTIFACT_DIR=${DOSH_E2E_ARTIFACT_DIR:-/var/lib/dosh/e2e-runs/${RUN_ID}-cronjob-detail}
WORKDIR=$(mktemp -d)
CRONJOB_CREATED=0

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
cleanup() {
  if [[ "$CRONJOB_CREATED" == 1 ]]; then
    kubectl -n "$TARGET_NAMESPACE" delete cronjob "$CRONJOB_NAME" \
      --ignore-not-found --cascade=background --wait=false >/dev/null 2>&1 || true
  fi
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

for command in kubectl helm curl jq; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done

[[ "$TARGET_NAMESPACE" != dosh ]] || die "refusing to create test resources in production namespace"
production_port=$(kubectl -n dosh get service dosh -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null || true)
[[ "$production_port" != "$NODE_PORT" ]] || die "test NodePort collides with production dosh service"

mkdir -p "$ARTIFACT_DIR"
ARTIFACT_LOG="$ARTIFACT_DIR/cronjob-detail.log"
DETAIL_JSON="$ARTIFACT_DIR/detail.json"

log "Checking isolated OIDC release"
kubectl -n "$NAMESPACE" get deployment dosh -o jsonpath='{.status.readyReplicas}' | grep -qx '1' \
  || die "dosh test release is not ready"
curl -skf "$BASE_URL/healthz" >/dev/null || die "dosh health endpoint is unavailable"
curl -skf "$BASE_URL/readyz" >/dev/null || die "dosh readiness endpoint is unavailable"

log "Creating CronJob without a selector"
kubectl -n "$TARGET_NAMESPACE" apply -f - >/dev/null <<EOF
apiVersion: batch/v1
kind: CronJob
metadata:
  name: ${CRONJOB_NAME}
  labels: { app: dosh-e2e-cronjob-detail }
spec:
  schedule: "0 0 31 2 *"
  suspend: true
  jobTemplate:
    spec:
      template:
        metadata:
          labels: { app: dosh-e2e-cronjob-detail }
        spec:
          restartPolicy: Never
          containers:
            - name: check
              image: busybox:1.36
              command: ["true"]
EOF
CRONJOB_CREATED=1

log "Logging in as ${LOGIN_USER} through the signed test IdP"
cookie_jar="$WORKDIR/${LOGIN_USER}.cookie"
login_headers="$WORKDIR/${LOGIN_USER}.headers"
curl -sk -D "$login_headers" -o /dev/null -c "$cookie_jar" \
  "$BASE_URL/api/v1/auth/oidc/login?returnTo=%2Fworkloads%3Fnamespace%3D${TARGET_NAMESPACE}"
authorize_url=$(awk 'tolower($1)=="location:" {sub(/\r$/,"",$2); print $2}' "$login_headers" | tail -1)
[[ -n "$authorize_url" ]] || die "OIDC login did not return an IdP redirect"
if [[ "$authorize_url" == *\?* ]]; then
  authorize_url="${authorize_url}&user=${LOGIN_USER}"
else
  authorize_url="${authorize_url}?user=${LOGIN_USER}"
fi
login_status=$(curl -skL -b "$cookie_jar" -c "$cookie_jar" -o "$WORKDIR/login.body" -w '%{http_code}' "$authorize_url")
[[ "$login_status" == 200 ]] || die "OIDC login failed with HTTP $login_status"

log "Fetching CronJob detail without a selector"
endpoint="$BASE_URL/api/v1/workloads/cronjob/${TARGET_NAMESPACE}/${CRONJOB_NAME}"
http_status=$(curl -sk -b "$cookie_jar" -o "$DETAIL_JSON" -w '%{http_code}' "$endpoint")
[[ "$http_status" == 200 ]] || die "CronJob detail request failed with HTTP $http_status"
jq -e --arg name "$CRONJOB_NAME" --arg namespace "$TARGET_NAMESPACE" \
  '.kind == "CronJob" and .name == $name and .namespace == $namespace and .status == "Suspended" and (.selector | type == "object" and length == 0) and (.pods | type == "array" and length == 0)' \
  "$DETAIL_JSON" >/dev/null || die "CronJob detail without selector is not safe"

log "Deleting CronJob and verifying cleanup"
kubectl -n "$TARGET_NAMESPACE" delete cronjob "$CRONJOB_NAME" \
  --ignore-not-found --cascade=background --wait=true --timeout=30s >/dev/null
if kubectl -n "$TARGET_NAMESPACE" get cronjob "$CRONJOB_NAME" >/dev/null 2>&1; then
  die "CronJob still exists after deletion"
fi
CRONJOB_CREATED=0

pod_image=$(kubectl -n "$NAMESPACE" get deployment dosh -o jsonpath='{.spec.template.spec.containers[0].image}')
release_revision=$(helm -n "$NAMESPACE" status "$RELEASE" -o json 2>/dev/null | jq -r '.version // empty')
{
  printf 'cronjob_detail=pass;http_status=%s;name=%s;namespace=%s;selector_empty=true;pods_array=true;status=Suspended;cleanup=pass\n' \
    "$http_status" "$CRONJOB_NAME" "$TARGET_NAMESPACE"
  printf 'endpoint=%s\n' "$endpoint"
  printf 'release=%s/%s;revision=%s\n' "$RELEASE" "$NAMESPACE" "${release_revision:-unknown}"
  printf 'image=%s\n' "$pod_image"
  printf 'artifact=%s\n' "$ARTIFACT_DIR"
} | tee "$ARTIFACT_LOG"

printf '\nCronJob detail E2E: pass\n'
