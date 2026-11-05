#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/root/.local/bin:$PATH

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
STATE_DIR=${DOSH_E2E_ARTIFACT_DIR:-$(mktemp -d)}
STATE_FILE="$STATE_DIR/fixture.json"
INFRA_LOG="$STATE_DIR/infra.log"
EVIDENCE="$STATE_DIR/oidc-rollback-evidence.log"
IMAGE_TAG=${DOSH_E2E_IMAGE_TAG:-0.1.3-oidc-e2e-$(date +%s)}
IMAGE_DIGEST=${DOSH_E2E_IMAGE_DIGEST:-}
SKIP_BUILD=${DOSH_E2E_SKIP_BUILD:-1}
INFRA_PID=""
FIXTURE_WORKDIR=""
ADMIN_REPLICAS=1
CHECK_COUNT=0

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
cleanup() {
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
    printf 'OIDC rollback artifacts retained: %s\n' "$STATE_DIR"
  fi
}
trap cleanup EXIT

for command in kubectl helm curl jq go make bash; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done
mkdir -p "$STATE_DIR"

log "Starting isolated OIDC fixture for rollback exercise"
export DOSH_E2E_HOLD=1
export DOSH_E2E_STATE_FILE="$STATE_FILE"
export DOSH_E2E_KEEP_WORKDIR=1
export DOSH_E2E_SKIP_BUILD="$SKIP_BUILD"
export DOSH_E2E_IMAGE_TAG="$IMAGE_TAG"
export DOSH_E2E_IMAGE_DIGEST="$IMAGE_DIGEST"
bash "$ROOT/scripts/e2e-oidc.sh" >"$INFRA_LOG" 2>&1 &
INFRA_PID=$!

for _ in $(seq 1 300); do
  if [[ -s "$STATE_FILE" ]]; then
    break
  fi
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
RELEASE=$(jq -r '.release' "$STATE_FILE")
IMAGE=$(jq -r '.image' "$STATE_FILE")
FIXTURE_WORKDIR=$(jq -r '.workdir' "$STATE_FILE")
[[ "$BASE" == https://* && "$BASE" != *:32626 ]] || die "fixture endpoint is not isolated: $BASE"

latest_revision() {
  helm -n "$NAMESPACE" history "$RELEASE" -o json | jq -r '.[-1].revision'
}

login_as() {
  local user=$1
  local jar="$FIXTURE_WORKDIR/${user}.cookie"
  local headers="$FIXTURE_WORKDIR/${user}.login.headers"
  local location status
  curl -sk -D "$headers" -o /dev/null -c "$jar" \
    "$BASE/api/v1/auth/oidc/login?returnTo=%2Fworkloads%3Fnamespace%3D${TARGET_NAMESPACE}"
  location=$(awk 'tolower($1)=="location:" {sub(/\r$/,"",$2); print $2}' "$headers" | tail -1)
  [[ -n "$location" ]] || die "$user login did not return an IdP redirect"
  if [[ "$location" == *\?* ]]; then
    location="${location}&user=${user}"
  else
    location="${location}?user=${user}"
  fi
  status=$(curl -skL -b "$jar" -c "$jar" -o /dev/null -w '%{http_code}' "$location")
  [[ "$status" == 200 ]] || die "$user OIDC login failed with HTTP $status"
  curl -sk -b "$jar" "$BASE/api/v1/session" >"$FIXTURE_WORKDIR/${user}.session.json"
  jq -e --arg username "${user}@example.test" \
    '.authMode == "oidc" and .user.username == $username and (.csrfToken | length > 0)' \
    "$FIXTURE_WORKDIR/${user}.session.json" >/dev/null || die "$user session is invalid"
}

csrf() { jq -r '.csrfToken' "$FIXTURE_WORKDIR/$1.session.json"; }
status_of() {
  local jar=$1 method=$2 path=$3 data=${4:-}
  local args=(-sk -o /dev/null -w '%{http_code}' -b "$jar" -X "$method" "$BASE$path")
  if [[ "$method" != GET ]]; then
    args+=(-H "X-CSRF-Token: $(csrf "$(basename "$jar" .cookie)")" -H 'Content-Type: application/json')
  fi
  if [[ -n "$data" ]]; then
    args+=(--data "$data")
  fi
  curl "${args[@]}"
}

check_matrix() {
  local phase=$1
  local admin_scale replicas
  CHECK_COUNT=$((CHECK_COUNT + 1))
  admin_scale=$ADMIN_REPLICAS
  if [[ "$admin_scale" == 1 ]]; then
    replicas=0
  else
    replicas=1
  fi
  ADMIN_REPLICAS=$replicas

  kubectl -n "$NAMESPACE" rollout status deployment/dosh --timeout=180s >/dev/null
  curl -skf "$BASE/healthz" >/dev/null || die "$phase healthz failed"
  curl -skf "$BASE/readyz" >/dev/null || die "$phase readyz failed"
  login_as viewer
  login_as operator
  login_as admin

  jq -e '.capabilities.read == true and .capabilities.scale == false and .capabilities.revealSecrets == false' \
    "$FIXTURE_WORKDIR/viewer.session.json" >/dev/null || die "$phase viewer capabilities regressed"
  jq -e '.capabilities.read == true and .capabilities.revealSecrets == false' \
    "$FIXTURE_WORKDIR/operator.session.json" >/dev/null || die "$phase operator capabilities regressed"
  jq -e '.capabilities.read == true and .capabilities.scale == true and .capabilities.delete == true' \
    "$FIXTURE_WORKDIR/admin.session.json" >/dev/null || die "$phase admin capabilities regressed"

  [[ "$(status_of "$FIXTURE_WORKDIR/viewer.cookie" GET "/api/v1/workloads?namespace=${TARGET_NAMESPACE}")" == 200 ]] || die "$phase viewer read failed"
  [[ "$(status_of "$FIXTURE_WORKDIR/viewer.cookie" POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-demo/scale" '{"replicas":2}')" == 403 ]] || die "$phase viewer write was not denied"
  [[ "$(status_of "$FIXTURE_WORKDIR/operator.cookie" POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-demo/scale" '{"replicas":2}')" == 200 ]] || die "$phase operator target scale failed"
  [[ "$(status_of "$FIXTURE_WORKDIR/operator.cookie" POST "/api/v1/workloads/deployment/default/e2e-demo/scale" '{"replicas":2}')" == 403 ]] || die "$phase operator escaped target namespace"
  [[ "$(status_of "$FIXTURE_WORKDIR/admin.cookie" POST "/api/v1/workloads/deployment/${TARGET_NAMESPACE}/e2e-admin/scale" "{\"replicas\":${admin_scale}}")" == 200 ]] || die "$phase admin scale failed"
  [[ "$(status_of "$FIXTURE_WORKDIR/admin.cookie" GET '/api/v1/nodes')" == 200 ]] || die "$phase admin node read failed"

  local logout_url
  logout_url=$(curl -skL -b "$FIXTURE_WORKDIR/admin.cookie" -c "$FIXTURE_WORKDIR/admin.cookie" \
    -o /dev/null -w '%{url_effective}' "$BASE/api/v1/auth/oidc/logout")
  [[ "$logout_url" == "$BASE/login" ]] || die "$phase RP logout returned $logout_url"
  [[ "$(curl -sk -o /dev/null -w '%{http_code}' -b "$FIXTURE_WORKDIR/admin.cookie" "$BASE/api/v1/session")" == 401 ]] || die "$phase logout did not clear session"

  local audit_log
  audit_log=$(kubectl -n "$NAMESPACE" logs deployment/dosh --tail=-1 2>/dev/null || true)
  for username in viewer@example.test operator@example.test admin@example.test; do
    grep -Fq "\"user\":\"${username}\"" <<<"$audit_log" || die "$phase audit missing ${username}"
  done
  grep -Fq '"result":"denied"' <<<"$audit_log" || die "$phase audit missing denied result"

  printf '%s\tbase=%s\trelease=%s\thealth=200\tready=200\tviewer=read200/write403\toperator=target200/other403\tadmin=scale200/nodes200\tlogout=401\n' \
    "$phase" "$BASE" "$NAMESPACE" >>"$EVIDENCE"
}

BASELINE_REVISION=$(latest_revision)
UPGRADE_ID="rollback-$(date +%Y%m%d%H%M%S)-${RANDOM}"
log "Baseline revision ${BASELINE_REVISION}; exercising OIDC matrix"
check_matrix baseline

log "Applying harmless OIDC upgrade ${UPGRADE_ID}"
helm upgrade "$RELEASE" "$ROOT/deploy/helm/dosh" -n "$NAMESPACE" \
  --reuse-values \
  --set "podAnnotations.e2e-rollback=${UPGRADE_ID}" \
  --wait --timeout 3m >/dev/null
UPGRADE_REVISION=$(latest_revision)
[[ "$UPGRADE_REVISION" -gt "$BASELINE_REVISION" ]] || die "upgrade did not create a new revision"
printf 'upgrade\tbaseline=%s\tupgraded=%s\tid=%s\n' "$BASELINE_REVISION" "$UPGRADE_REVISION" "$UPGRADE_ID" >>"$EVIDENCE"
check_matrix upgrade

log "Rolling back to baseline revision ${BASELINE_REVISION}"
helm rollback "$RELEASE" "$BASELINE_REVISION" -n "$NAMESPACE" --wait --timeout 3m >/dev/null
ROLLBACK_REVISION=$(latest_revision)
[[ "$ROLLBACK_REVISION" -gt "$UPGRADE_REVISION" ]] || die "rollback did not create a new revision"
printf 'rollback\ttarget=%s\tresult=%s\n' "$BASELINE_REVISION" "$ROLLBACK_REVISION" >>"$EVIDENCE"
check_matrix rollback

log "Restoring upgraded revision ${UPGRADE_REVISION}"
helm rollback "$RELEASE" "$UPGRADE_REVISION" -n "$NAMESPACE" --wait --timeout 3m >/dev/null
RESTORE_REVISION=$(latest_revision)
[[ "$RESTORE_REVISION" -gt "$ROLLBACK_REVISION" ]] || die "restore did not create a new revision"
printf 'restore\ttarget=%s\tresult=%s\n' "$UPGRADE_REVISION" "$RESTORE_REVISION" >>"$EVIDENCE"
check_matrix restore

{
  printf '\nrelease=%s namespace=%s image=%s\n' "$RELEASE" "$NAMESPACE" "$IMAGE"
  printf 'revisions baseline=%s upgrade=%s rollback=%s restore=%s\n' \
    "$BASELINE_REVISION" "$UPGRADE_REVISION" "$ROLLBACK_REVISION" "$RESTORE_REVISION"
} >>"$EVIDENCE"

log "OIDC upgrade/rollback/restore passed"
cat "$EVIDENCE"
if [[ ${DOSH_E2E_KEEP_ARTIFACTS:-0} == 1 ]]; then
  printf '\nEvidence retained at %s\n' "$EVIDENCE"
fi
