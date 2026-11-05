#!/usr/bin/env bash
set -euo pipefail

ROOT=${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}
status=0

fail() {
  printf 'E2E image binding violation: %s\n' "$*" >&2
  status=1
}

require_literal() {
  local file=$1
  local literal=$2
  grep -Fq -- "$literal" "$ROOT/$file" || fail "$file must contain: $literal"
}

require_literal scripts/e2e-oidc.sh '${IMAGE_DIGEST:+--set-string image.digest="$IMAGE_DIGEST"}'
require_literal scripts/e2e-oidc-rollback.sh 'export DOSH_E2E_IMAGE_DIGEST="$IMAGE_DIGEST"'
require_literal scripts/e2e-soak.sh 'export DOSH_E2E_IMAGE_DIGEST="$IMAGE_DIGEST"'
require_literal scripts/e2e-kube-retry.sh 'export DOSH_E2E_IMAGE_DIGEST="$DOSH_IMAGE_DIGEST"'
require_literal scripts/e2e-metrics.sh 'export DOSH_E2E_IMAGE_DIGEST='
require_literal scripts/e2e-audit-webhook.sh 'DOSH_E2E_IMAGE_DIGEST="$IMAGE_DIGEST"'
require_literal scripts/e2e-log-stream.sh '--set-string image.digest="$IMAGE_DIGEST"'

if (( status != 0 )); then
  exit 1
fi

printf 'E2E image binding checks: pass\n'
