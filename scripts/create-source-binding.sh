#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
OUTPUT=
TAG=${DOSH_IMAGE_TAG:-}
DIGEST=${DOSH_IMAGE_DIGEST:-}
IMAGE_ID=${DOSH_IMAGE_ID:-}
BUILD_LOG=${DOSH_BUILD_LOG:-}
DIGEST_FILE=${DOSH_DIGEST_FILE:-}

usage() {
  cat >&2 <<'EOF'
Usage: create-source-binding.sh [--output <file>] [--tag <tag>] [--digest <sha256>]
                                [--image-id <sha256>] [--build-log <file>]
                                [--digest-file <file>]

Environment alternatives: DOSH_IMAGE_TAG, DOSH_IMAGE_DIGEST, DOSH_IMAGE_ID,
DOSH_BUILD_LOG and DOSH_DIGEST_FILE.
EOF
  exit 2
}

die() { printf 'error: %s\n' "$*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output) [[ $# -ge 2 ]] || usage; OUTPUT=$2; shift 2 ;;
    --tag) [[ $# -ge 2 ]] || usage; TAG=$2; shift 2 ;;
    --digest) [[ $# -ge 2 ]] || usage; DIGEST=$2; shift 2 ;;
    --image-id) [[ $# -ge 2 ]] || usage; IMAGE_ID=$2; shift 2 ;;
    --build-log) [[ $# -ge 2 ]] || usage; BUILD_LOG=$2; shift 2 ;;
    --digest-file) [[ $# -ge 2 ]] || usage; DIGEST_FILE=$2; shift 2 ;;
    -h|--help) usage ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ -n "$OUTPUT" ]] || usage
[[ "$DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || die "digest must be sha256:<64 lowercase hex>"
[[ -f "$ROOT/bin/dosh" ]] || die "bin/dosh is missing; run make build first"
for file in "$BUILD_LOG" "$DIGEST_FILE"; do
  [[ -z "$file" || -f "$file" ]] || die "evidence file does not exist: $file"
done
if [[ -n "$DIGEST_FILE" ]]; then
  [[ "$(tr -d '[:space:]' <"$DIGEST_FILE")" == "$DIGEST" ]] ||
    die "digest file does not match --digest"
fi

sources=(
  cmd internal web/src api deploy/helm/dosh
  Dockerfile go.mod go.sum
  web/components.json web/embed.go web/index.html
  web/package.json web/package-lock.json
  web/postcss.config.js web/tailwind.config.ts
  web/tsconfig.json web/tsconfig.app.json web/tsconfig.e2e.json web/tsconfig.node.json
  web/vite.config.ts web/vitest.config.ts
)

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
: >"$tmp"

while IFS= read -r file; do
  rel=${file#"$ROOT"/}
  hash=$(sha256sum "$file" | awk '{print $1}')
  printf '%s  %s\n' "$hash" "$rel" >>"$tmp"
done < <(
  cd "$ROOT"
  for path in "${sources[@]}"; do
    if [[ -d "$path" ]]; then
      find "$path" -type f -print0
    elif [[ -f "$path" ]]; then
      printf '%s\0' "$path"
    else
      printf 'missing source path: %s\n' "$path" >&2
      exit 1
    fi
  done | LC_ALL=C sort -zu | xargs -0 -r -n1 realpath --
)

source_count=$(wc -l <"$tmp")
source_tree_sha256=$(sha256sum "$tmp" | awk '{print $1}')
binary_sha256=$(sha256sum "$ROOT/bin/dosh" | awk '{print $1}')
app_version=$(sed -n 's/^VERSION ?= //p' "$ROOT/Makefile" | head -n1)
chart_version=$(awk '$1 == "version:" { print $2; exit }' "$ROOT/deploy/helm/dosh/Chart.yaml")
openapi_version=$(awk '/^info:/ { active=1; next } active && /^  version:/ { print $2; exit }' "$ROOT/api/openapi.yaml")

mkdir -p "$(dirname -- "$OUTPUT")"
{
  printf 'schema=dosh-source-binding-v1\n'
  printf 'created_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'host=%s\n' "$(hostname -f 2>/dev/null || hostname)"
  printf 'app_version=%s\n' "$app_version"
  printf 'chart_version=%s\n' "$chart_version"
  printf 'openapi_version=%s\n' "$openapi_version"
  printf 'image_tag=%s\n' "$TAG"
  printf 'image_digest=%s\n' "$DIGEST"
  printf 'image_id=%s\n' "$IMAGE_ID"
  printf 'binary_sha256=%s\n' "$binary_sha256"
  printf 'source_count=%s\n' "$source_count"
  printf 'source_tree_sha256=%s\n' "$source_tree_sha256"
  if [[ -n "$BUILD_LOG" ]]; then
    printf 'build_log_sha256=%s\n' "$(sha256sum "$BUILD_LOG" | awk '{print $1}')"
  fi
  if [[ -n "$DIGEST_FILE" ]]; then
    printf 'digest_file_sha256=%s\n' "$(sha256sum "$DIGEST_FILE" | awk '{print $1}')"
  fi
  printf '\n[source_sha256]\n'
  cat "$tmp"
} >"$OUTPUT"

printf 'source binding written: %s\n' "$OUTPUT"
printf 'sources=%s binary_sha256=%s source_tree_sha256=%s\n' \
  "$source_count" "$binary_sha256" "$source_tree_sha256"
