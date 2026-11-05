#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
RUNNER="$ROOT/scripts/run-playwright-container.sh"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

for required in --shm-size= --memory= --memory-swap= --cpus= --pids-limit= --ulimit; do
  grep -Fq -- "$required" "$RUNNER" || fail "Playwright runner lacks $required"
done
grep -Fq -- 'timeout --foreground' "$RUNNER" || fail "Playwright runner lacks a hard timeout"
grep -Fq -- 'DOSH_PLAYWRIGHT_LOCK_FILE' "$RUNNER" || fail "Playwright runner lacks an overridable lock file"
grep -Fq -- 'flock -n 9' "$RUNNER" || fail "Playwright runner lacks host-level mutual exclusion"
grep -Fq -- 'another Playwright run is active' "$RUNNER" || fail "Playwright runner lacks a clear lock contention error"
grep -Fq -- 'MEMORY=${DOSH_PLAYWRIGHT_MEMORY:-4g}' "$RUNNER" || fail "Playwright runner default memory must be 4g"
grep -Fq -- 'CPUS=${DOSH_PLAYWRIGHT_CPUS:-2}' "$RUNNER" || fail "Playwright runner default CPU count must be 2"
grep -Fq -- 'MAX_MEMORY=6g' "$RUNNER" || fail "Playwright runner lacks a memory safety cap"
grep -Fq -- 'MAX_CPUS=4' "$RUNNER" || fail "Playwright runner lacks a CPU safety cap"
grep -Fq -- 'MAX_SHM_SIZE=1g' "$RUNNER" || fail "Playwright runner lacks a shared-memory safety cap"
grep -Fq -- 'MAX_PIDS_LIMIT=2048' "$RUNNER" || fail "Playwright runner lacks a PID safety cap"
grep -Fq -- '--network=none --ipc=private' "$RUNNER" || fail "Playwright runner must default to isolated network and IPC"
grep -Fq -- '--workers=1' "$RUNNER" || fail "Playwright runner must force one worker"
grep -Fq -- '--retries=0' "$RUNNER" || fail "Playwright runner must disable retries"
grep -Fq -- 'concurrency overrides are not allowed' "$RUNNER" || fail "Playwright runner accepts concurrency overrides"
grep -Fq -- 'retry overrides are not allowed' "$RUNNER" || fail "Playwright runner accepts retry overrides"
grep -Fq -- 'projects=(chromium firefox)' "$RUNNER" || fail "Playwright safe default is missing Chromium/Firefox"
grep -Fq -- 'projects=(chromium firefox webkit)' "$RUNNER" || fail "Playwright isolated WebKit matrix is missing"
grep -Fq -- 'DOSH_PLAYWRIGHT_ALLOW_WEBKIT' "$RUNNER" || fail "WebKit must require an explicit isolated-runner opt-in"
grep -Fq -- 'refusing to run WebKit by default' "$RUNNER" || fail "WebKit default guard is missing"
grep -Fq -- '--ipc=host' "$ROOT/web/package.json" "$ROOT/scripts/e2e-oidc-browser.sh" "$ROOT/.github/workflows/ci.yml" 2>/dev/null &&
  fail "host IPC must not be shared with Playwright containers"
for config in playwright.config.ts playwright.oidc.config.ts playwright.local.config.ts playwright.performance.config.ts; do
  grep -Fq -- 'workers: 1' "$ROOT/web/$config" || fail "$config workers must be 1"
  grep -Fq -- 'retries: 0' "$ROOT/web/$config" || fail "$config retries must be 0"
done
for config in playwright.config.ts playwright.oidc.config.ts playwright.local.config.ts; do
  grep -Fq -- 'allowWebKit' "$ROOT/web/$config" || fail "$config lacks a WebKit host guard"
  grep -Fq -- 'DOSH_PLAYWRIGHT_ALLOW_WEBKIT' "$ROOT/web/$config" ||
    fail "$config ignores the WebKit isolated-runner opt-in"
done
grep -Fq -- 'refusing to run WebKit directly on the host' "$ROOT/scripts/e2e-oidc-browser.sh" ||
  fail "OIDC browser script lacks a direct-host WebKit guard"
for script in e2e e2e:chromium e2e:firefox e2e:webkit e2e:container e2e:oidc e2e:oidc:chromium e2e:local e2e:performance; do
  node -e 'const scripts=require(process.argv[1]).scripts; const name=process.argv[2]; const command=scripts[name] ?? ""; if (!command.includes("run-playwright-container.sh") || command.includes("npx playwright")) process.exit(1)' \
    "$ROOT/web/package.json" "$script" || fail "npm script $script bypasses the isolated Playwright runner"
done
for required in '--shm-size=1g' '--memory=4g' '--memory-swap=4g' '--cpus=2' '--pids-limit=2048' '--ulimit core=0'; do
  grep -Fq -- "$required" "$ROOT/.github/workflows/ci.yml" || fail "CI Playwright containers lack $required"
done
grep -Fq -- 'timeout-minutes: 20' "$ROOT/.github/workflows/ci.yml" || fail "CI Playwright jobs lack a hard timeout"
grep -Fq -- 'max-parallel: 1' "$ROOT/.github/workflows/ci.yml" ||
  fail "CI Playwright matrix must execute browser jobs serially"
grep -Fq -- 'project: [chromium, firefox, webkit]' "$ROOT/.github/workflows/ci.yml" ||
  fail "CI must run each browser as a separate matrix job"
grep -Fq -- 'npx playwright test --project=${{ matrix.project }} --workers=1 --retries=0' "$ROOT/.github/workflows/ci.yml" ||
  fail "CI matrix must run exactly one Playwright project per container without retries"

DRY_RUN=$(DOSH_PLAYWRIGHT_DRY_RUN=1 "$RUNNER" 2>&1) || fail "safe default Playwright dry-run failed"
[[ $(grep -c 'podman run' <<<"$DRY_RUN") -eq 2 ]] || fail "safe default must schedule two containers"
grep -Fq -- '--project=chromium' <<<"$DRY_RUN" || fail "safe default is missing Chromium"
grep -Fq -- '--project=firefox' <<<"$DRY_RUN" || fail "safe default is missing Firefox"
if grep -Fq -- '--project=webkit' <<<"$DRY_RUN"; then
  fail "safe default must not run WebKit"
fi
while IFS= read -r line; do
  for required in '--network=none' '--ipc=private' '--workers=1' '--retries=0' '--shm-size=1g' '--memory=4g' '--memory-swap=4g' '--cpus=2' '--pids-limit=2048' '--ulimit core=0'; do
    grep -Fq -- "$required" <<<"$line" || fail "Playwright container lacks $required"
  done
done < <(grep 'podman run' <<<"$DRY_RUN")

DRY_RUN=$(DOSH_PLAYWRIGHT_DRY_RUN=1 "$RUNNER" --project=chromium --project=firefox --retries=0 2>&1) ||
  fail "multi-project Playwright dry-run failed"
[[ $(grep -c 'podman run' <<<"$DRY_RUN") -eq 2 ]] || fail "multi-project run must schedule two containers"
while IFS= read -r line; do
  [[ $(grep -o -- '--project=' <<<"$line" | wc -l) -eq 1 ]] ||
    fail "each container must receive exactly one Playwright project"
  grep -Fq -- '--network=none' <<<"$line" || fail "multi-project run must remain network-isolated"
  grep -Fq -- '--workers=1' <<<"$line" || fail "multi-project run must remain single-worker"
  grep -Fq -- '--retries=0' <<<"$line" || fail "multi-project run must disable retries"
done < <(grep 'podman run' <<<"$DRY_RUN")

if DOSH_PLAYWRIGHT_DRY_RUN=1 "$RUNNER" --project=webkit >/dev/null 2>&1; then
  fail "direct WebKit request was not rejected"
fi

if DOSH_PLAYWRIGHT_DRY_RUN=1 "$RUNNER" --project=chromium-production >/dev/null 2>&1; then
  fail "performance project without the performance config was not rejected"
fi

PERF_DRY_RUN=$(DOSH_PLAYWRIGHT_DRY_RUN=1 DOSH_PLAYWRIGHT_CONFIG=playwright.performance.config.ts \
  "$RUNNER" --project=chromium-production 2>&1) || fail "isolated performance Playwright dry-run failed"
for required in '--network=none' '--ipc=private' '--workers=1' '--retries=0' '--shm-size=1g' \
  '--memory=4g' '--memory-swap=4g' '--cpus=2' '--pids-limit=2048' '--ulimit core=0' \
  '--project=chromium-production' '--config=playwright.performance.config.ts'; do
  grep -Fq -- "$required" <<<"$PERF_DRY_RUN" || fail "isolated performance run lacks $required"
done

if DOSH_PLAYWRIGHT_DRY_RUN=1 DOSH_PLAYWRIGHT_MEMORY=8g "$RUNNER" --project=chromium >/dev/null 2>&1; then
  fail "memory limit above the Playwright safety cap was accepted"
fi
if DOSH_PLAYWRIGHT_DRY_RUN=1 DOSH_PLAYWRIGHT_CPUS=8 "$RUNNER" --project=chromium >/dev/null 2>&1; then
  fail "CPU limit above the Playwright safety cap was accepted"
fi
if DOSH_PLAYWRIGHT_DRY_RUN=1 DOSH_PLAYWRIGHT_PIDS_LIMIT=4096 "$RUNNER" --project=chromium >/dev/null 2>&1; then
  fail "PID limit above the Playwright safety cap was accepted"
fi
if DOSH_PLAYWRIGHT_DRY_RUN=1 "$RUNNER" --project=chromium --workers=2 >/dev/null 2>&1; then
  fail "worker override was not rejected"
fi

if DOSH_PLAYWRIGHT_DRY_RUN=1 "$RUNNER" --project=chromium --retries=2 >/dev/null 2>&1; then
  fail "retry override was not rejected"
fi

printf 'playwright isolation checks: pass\n'
