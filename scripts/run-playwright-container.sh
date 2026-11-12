#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
IMAGE=${DOSH_PLAYWRIGHT_IMAGE:-mcr.microsoft.com/playwright:v1.65.0-noble}
MEMORY=${DOSH_PLAYWRIGHT_MEMORY:-4g}
CPUS=${DOSH_PLAYWRIGHT_CPUS:-2}
SHM_SIZE=${DOSH_PLAYWRIGHT_SHM_SIZE:-1g}
PIDS_LIMIT=${DOSH_PLAYWRIGHT_PIDS_LIMIT:-2048}
TIMEOUT=${DOSH_PLAYWRIGHT_TIMEOUT:-20m}
CONFIG=${DOSH_PLAYWRIGHT_CONFIG:-}
HOST_NETWORK=${DOSH_PLAYWRIGHT_HOST_NETWORK:-0}
DRY_RUN=${DOSH_PLAYWRIGHT_DRY_RUN:-0}
ALLOW_WEBKIT=${DOSH_PLAYWRIGHT_ALLOW_WEBKIT:-0}
LOCK_FILE=${DOSH_PLAYWRIGHT_LOCK_FILE:-${TMPDIR:-/tmp}/dosh-playwright.lock}
MAX_MEMORY=6g
MAX_CPUS=4
MAX_SHM_SIZE=1g
MAX_PIDS_LIMIT=2048

log() { printf '\n==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

[[ "$MEMORY" =~ ^[1-9][0-9]*[mg]$ ]] || die "invalid memory limit: $MEMORY"
[[ "$CPUS" =~ ^[1-9][0-9]*([.][0-9]+)?$ ]] || die "invalid CPU limit: $CPUS"
[[ "$SHM_SIZE" =~ ^[1-9][0-9]*[mg]$ ]] || die "invalid shared-memory limit: $SHM_SIZE"
[[ "$PIDS_LIMIT" =~ ^[1-9][0-9]*$ ]] || die "invalid PID limit: $PIDS_LIMIT"

memory_mib() {
  case "$1" in
    *g) printf '%s\n' "$(( ${1%g} * 1024 ))" ;;
    *m) printf '%s\n' "${1%m}" ;;
  esac
}

(( $(memory_mib "$MEMORY") <= $(memory_mib "$MAX_MEMORY") )) ||
  die "memory limit $MEMORY exceeds the Playwright safety cap of $MAX_MEMORY"
(( $(memory_mib "$SHM_SIZE") <= $(memory_mib "$MAX_SHM_SIZE") )) ||
  die "shared-memory limit $SHM_SIZE exceeds the Playwright safety cap of $MAX_SHM_SIZE"
awk -v requested="$CPUS" -v maximum="$MAX_CPUS" 'BEGIN { exit !(requested <= maximum) }' ||
  die "CPU limit $CPUS exceeds the Playwright safety cap of $MAX_CPUS"
(( PIDS_LIMIT <= MAX_PIDS_LIMIT )) ||
  die "PID limit $PIDS_LIMIT exceeds the Playwright safety cap of $MAX_PIDS_LIMIT"

command -v podman >/dev/null 2>&1 || die "podman is required"
command -v timeout >/dev/null 2>&1 || die "timeout is required"
command -v flock >/dev/null 2>&1 || die "flock is required"

exec 9>"$LOCK_FILE" || die "cannot open Playwright lock file: $LOCK_FILE"
flock -n 9 || die "another Playwright run is active; refusing to overlap browser tests"

projects=()
extra_args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --project)
      [[ $# -ge 2 ]] || die "--project requires a value"
      projects+=("$2")
      shift 2
      ;;
    --project=*)
      projects+=("${1#--project=}")
      shift
      ;;
    --workers|--workers=*|--fully-parallel)
      die "Playwright concurrency overrides are not allowed; browser tests run serially with workers=1"
      ;;
    --retries=0)
      shift
      ;;
    --retries|--retries=*)
      die "Playwright retry overrides are not allowed; browser tests run once with retries=0"
      ;;
    *)
      extra_args+=("$1")
      shift
      ;;
  esac
done

if [[ ${#projects[@]} -eq 0 ]]; then
  if [[ "$ALLOW_WEBKIT" == 1 ]]; then
    projects=(chromium firefox webkit)
  else
    projects=(chromium firefox)
  fi
fi

for project in "${projects[@]}"; do
  case "$project" in
    chromium|firefox) ;;
    chromium-production)
      [[ "$CONFIG" == *performance* ]] ||
        die "chromium-production is only allowed with the performance Playwright config"
      ;;
    webkit)
      [[ "$ALLOW_WEBKIT" == 1 ]] ||
        die "refusing to run WebKit by default; use the isolated runner with DOSH_PLAYWRIGHT_ALLOW_WEBKIT=1"
      ;;
    *) die "unknown Playwright project: $project" ;;
  esac
done

network_args=(--network=none --ipc=private)
if [[ "$HOST_NETWORK" == 1 ]]; then
  network_args=(--network=host --ipc=private)
fi

forward_env=()
for name in DOSH_E2E_BASE_URL DOSH_E2E_TARGET_NAMESPACE DOSH_E2E_OIDC_STATE_TTL_MS \
  DOSH_E2E_CLUSTER_NAME DOSH_E2E_LOCAL_USERNAME DOSH_E2E_LOCAL_PASSWORD DOSH_E2E_TOKEN \
  DOSH_E2E_SCREENSHOT_DIR; do
  if [[ -n "${!name:-}" ]]; then
    forward_env+=(-e "$name")
  fi
done

for project in "${projects[@]}"; do
  command=(npx playwright test --workers=1 --retries=0)
  if [[ -n "$CONFIG" ]]; then
    command+=("--config=$CONFIG")
  fi
  command+=("--project=$project")
  command+=("${extra_args[@]}")

  log "Running Playwright for ${project} with memory=${MEMORY}, cpus=${CPUS}, shm=${SHM_SIZE}, pids=${PIDS_LIMIT}, timeout=${TIMEOUT}"
  run_args=(
    timeout --foreground --signal=TERM --kill-after=30s "$TIMEOUT"
    podman run --rm "${network_args[@]}"
      --shm-size="$SHM_SIZE"
      --memory="$MEMORY"
      --memory-swap="$MEMORY"
      --cpus="$CPUS"
      --pids-limit="$PIDS_LIMIT"
      --ulimit core=0
      -v "$ROOT:/work:Z" -w /work/web
      -e CI=1
      "${forward_env[@]}"
      "$IMAGE" "${command[@]}"
  )
  if [[ "$DRY_RUN" == 1 ]]; then
    printf '%q ' "${run_args[@]}"
    printf '\n'
    continue
  fi
  "${run_args[@]}"
done
