#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/root/.local/bin:$PATH

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PLAYWRIGHT_IMAGE=${DOSH_E2E_PLAYWRIGHT_IMAGE:-mcr.microsoft.com/playwright:v1.65.0-noble}
PLAYWRIGHT_IN_CONTAINER=${DOSH_E2E_PLAYWRIGHT_CONTAINER:-1}
STATE_DIR=${DOSH_E2E_ARTIFACT_DIR:-$(mktemp -d)}
STATE_FILE="$STATE_DIR/fixture.json"
INFRA_LOG="$STATE_DIR/infra.log"
LEAK_SCAN_LOG="$STATE_DIR/token-leak-scan.log"
NEGATIVE_AUDIT_LOG="$STATE_DIR/oidc-negative-audit.log"
EXPECTED_CALLBACK_FAILURES=${DOSH_E2E_EXPECTED_CALLBACK_FAILURES:-}
INFRA_PID=""
FIXTURE_WORKDIR=""

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
    printf 'OIDC browser artifacts retained: %s\n' "$STATE_DIR"
  fi
}
trap cleanup EXIT

for command in kubectl helm curl jq go make; do
  command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done
if [[ "$PLAYWRIGHT_IN_CONTAINER" == 1 ]]; then
  command -v podman >/dev/null 2>&1 || die "required command not found: podman"
fi
(cd "$ROOT/web" && node -e "require('@playwright/test')" >/dev/null)

if [[ -z "$EXPECTED_CALLBACK_FAILURES" ]]; then
  selected_projects=0
  for argument in "$@"; do
    case "$argument" in
      --project|--project=*) selected_projects=$((selected_projects + 1)) ;;
    esac
  done
  if (( selected_projects == 0 )); then
    browser_count=2
    if [[ ${DOSH_PLAYWRIGHT_ALLOW_WEBKIT:-0} == 1 ]]; then
      browser_count=3
    fi
    EXPECTED_CALLBACK_FAILURES=$((browser_count * 6))
  else
    EXPECTED_CALLBACK_FAILURES=$((selected_projects * 6))
  fi
fi

host_webkit_requested=0
previous_argument=
for argument in "$@"; do
  case "$argument" in
    --project=webkit) host_webkit_requested=1 ;;
  esac
  if [[ "$previous_argument" == "--project" && "$argument" == "webkit" ]]; then
    host_webkit_requested=1
  fi
  previous_argument=$argument
done
if [[ "$PLAYWRIGHT_IN_CONTAINER" != 1 && "$host_webkit_requested" == 1 &&
  "${DOSH_PLAYWRIGHT_ALLOW_WEBKIT:-0}" != 1 ]]; then
  die "refusing to run WebKit directly on the host; use the isolated container runner"
fi

mkdir -p "$STATE_DIR"

log "Starting isolated OIDC fixture"
export DOSH_E2E_HOLD=1
export DOSH_E2E_STATE_FILE="$STATE_FILE"
export DOSH_E2E_KEEP_WORKDIR=1
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

BASE_URL=$(jq -r '.base' "$STATE_FILE")
IDP_URL=$(jq -r '.idpURL' "$STATE_FILE")
NAMESPACE=$(jq -r '.namespace' "$STATE_FILE")
TARGET_NAMESPACE=$(jq -r '.targetNamespace' "$STATE_FILE")
RELEASE=$(jq -r '.release' "$STATE_FILE")
IMAGE=$(jq -r '.image' "$STATE_FILE")
FIXTURE_WORKDIR=$(jq -r '.workdir' "$STATE_FILE")
OIDC_STATE_TTL_MS=$(jq -r '.stateTTLMs' "$STATE_FILE")
[[ "$BASE_URL" == https://* && "$BASE_URL" != *:32626 ]] || die "fixture endpoint is not an isolated HTTPS NodePort: $BASE_URL"
[[ "$OIDC_STATE_TTL_MS" =~ ^[0-9]+$ && "$OIDC_STATE_TTL_MS" -gt 0 ]] || die "fixture state TTL is invalid: $OIDC_STATE_TTL_MS"
[[ "$EXPECTED_CALLBACK_FAILURES" =~ ^[0-9]+$ ]] || die "expected callback failure count is invalid: $EXPECTED_CALLBACK_FAILURES"

log "Running real-cluster OIDC browser matrix at ${BASE_URL}"
if [[ "$PLAYWRIGHT_IN_CONTAINER" == 1 ]]; then
  DOSH_PLAYWRIGHT_IMAGE="$PLAYWRIGHT_IMAGE" \
  DOSH_PLAYWRIGHT_CONFIG=playwright.oidc.config.ts \
  DOSH_PLAYWRIGHT_HOST_NETWORK=1 \
  DOSH_PLAYWRIGHT_TIMEOUT="${DOSH_E2E_PLAYWRIGHT_TIMEOUT:-20m}" \
  DOSH_E2E_BASE_URL="$BASE_URL" \
  DOSH_E2E_TARGET_NAMESPACE="$TARGET_NAMESPACE" \
  DOSH_E2E_OIDC_STATE_TTL_MS="$OIDC_STATE_TTL_MS" \
    bash "$ROOT/scripts/run-playwright-container.sh" "$@"
else
  (
    cd "$ROOT/web"
    DOSH_E2E_BASE_URL="$BASE_URL" \
    DOSH_E2E_TARGET_NAMESPACE="$TARGET_NAMESPACE" \
    DOSH_E2E_OIDC_STATE_TTL_MS="$OIDC_STATE_TTL_MS" \
      npx playwright test --config=playwright.oidc.config.ts "$@"
  )
fi

log "Scanning application logs and non-Secret Kubernetes configuration for tokens"
token_pattern='eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}'
test_access_token='dosh-e2e-access-token'
scan_failed=0
{
  printf 'namespace=%s\n' "$NAMESPACE"
  printf 'scanned_at=%s\n' "$(date -Is)"
  printf 'excluded=test-idp-log,k8s-Secret-storage\n'
  printf 'patterns=jwt,test-access-token\n'
} >"$LEAK_SCAN_LOG"

app_log=$(kubectl -n "$NAMESPACE" logs deployment/dosh --tail=-1 2>&1 || true)
if grep -Eq "$token_pattern|$test_access_token" <<<"$app_log"; then
  printf 'FAIL dosh application logs contain token material\n' | tee -a "$LEAK_SCAN_LOG"
  scan_failed=1
else
  printf 'PASS dosh application logs\n' >>"$LEAK_SCAN_LOG"
fi

callback_failures=$(grep -c '"msg":"oidc_callback_failed"' <<<"$app_log" || true)
denied_login_audits=$(printf '%s\n' "$app_log" | jq -Rr \
  'fromjson? | select(.msg == "audit_event" and .audit == true and .action == "login" and .result == "denied" and ((.request_id // "") | length > 0)) | .request_id' \
  2>/dev/null | wc -l)
{
  printf 'expected_callback_failures=%s\n' "$EXPECTED_CALLBACK_FAILURES"
  printf 'callback_failures=%s\n' "$callback_failures"
  printf 'denied_login_audits_with_request_id=%s\n' "$denied_login_audits"
} >"$NEGATIVE_AUDIT_LOG"
if (( callback_failures < EXPECTED_CALLBACK_FAILURES )); then
  printf 'FAIL OIDC callback failures were not all logged\n' | tee -a "$NEGATIVE_AUDIT_LOG"
  scan_failed=1
else
  printf 'PASS OIDC callback failures logged\n' >>"$NEGATIVE_AUDIT_LOG"
fi
if (( denied_login_audits < EXPECTED_CALLBACK_FAILURES )); then
  printf 'FAIL denied login audits are missing or lack request_id\n' | tee -a "$NEGATIVE_AUDIT_LOG"
  scan_failed=1
else
  printf 'PASS denied login audits carry request_id\n' >>"$NEGATIVE_AUDIT_LOG"
fi

non_secret_config=$(kubectl -n "$NAMESPACE" get configmap,deployment -o yaml 2>&1 || true)
if grep -Eq "$token_pattern|$test_access_token" <<<"$non_secret_config"; then
  printf 'FAIL ConfigMap/Deployment contain token material\n' | tee -a "$LEAK_SCAN_LOG"
  scan_failed=1
else
  printf 'PASS ConfigMap/Deployment\n' >>"$LEAK_SCAN_LOG"
fi

helm_values=$(helm -n "$NAMESPACE" get values -a "$RELEASE" 2>&1 || true)
if grep -Eq "$token_pattern|$test_access_token" <<<"$helm_values"; then
  printf 'FAIL Helm values contain token material\n' | tee -a "$LEAK_SCAN_LOG"
  scan_failed=1
else
  printf 'PASS Helm values\n' >>"$LEAK_SCAN_LOG"
fi

[[ "$scan_failed" == 0 ]] || {
  cat "$LEAK_SCAN_LOG" >&2
  die "OIDC token leak scan failed"
}

cat <<EOF

OIDC browser + leak scan: pass
  release:          ${RELEASE}/${NAMESPACE}
  endpoint:         ${BASE_URL}
  test IdP:         ${IDP_URL}
  target namespace: ${TARGET_NAMESPACE}
  image:            ${IMAGE}
  leak scan:        application logs, ConfigMap/Deployment, Helm values, browser URL/DOM/Web Storage/response bodies
  negative audit:   ${NEGATIVE_AUDIT_LOG}
  artifact log:     ${LEAK_SCAN_LOG}

The fixture process is stopped on exit; the isolated Helm release remains for evidence.
Set DOSH_E2E_KEEP_ARTIFACTS=1 to retain the fixture log, state and leak scan output.
EOF
