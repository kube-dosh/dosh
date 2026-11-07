#!/usr/bin/env bash
set -euo pipefail

ROOT=${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}
cd "$ROOT"

status=0
violation() {
  printf 'version violation: %s\n' "$*" >&2
  status=1
}

app_version=$(sed -n 's/^VERSION ?= //p' Makefile | head -n1)
chart_version=$(awk '$1 == "version:" { print $2; exit }' deploy/helm/dosh/Chart.yaml)
chart_app_version=$(awk '$1 == "appVersion:" { gsub(/"/, "", $2); print $2; exit }' deploy/helm/dosh/Chart.yaml)
image_tag=$(awk '$1 == "tag:" { gsub(/"/, "", $2); print $2; exit }' deploy/helm/dosh/values.yaml)
openapi_version=$(awk '/^info:/ { in_info=1; next } in_info && /^  version:/ { print $2; exit }' api/openapi.yaml)
package_version=$(sed -n 's/^  "version": "\([^"]*\)",/\1/p' web/package.json | head -n1)
lock_versions=$(sed -n 's/^[[:space:]]*"version": "\([^"]*\)",/\1/p' web/package-lock.json | head -n2)

if [[ -z "$app_version" ]]; then
  violation "Makefile VERSION is missing"
fi
if [[ ! "$chart_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
  violation "Chart.yaml version is not semver: ${chart_version:-<empty>}"
fi

for entry in \
  "Chart.yaml appVersion=${chart_app_version}" \
  "values.yaml image.tag=${image_tag}" \
  "api/openapi.yaml info.version=${openapi_version}" \
  "web/package.json version=${package_version}"; do
  source=${entry%%=*}
  value=${entry#*=}
  if [[ -n "$app_version" && "$value" != "$app_version" ]]; then
    violation "$source is ${value:-<empty>}, expected $app_version"
  fi
done

while IFS= read -r lock_version; do
  if [[ -n "$app_version" && "$lock_version" != "$app_version" ]]; then
    violation "web/package-lock.json version is ${lock_version:-<empty>}, expected $app_version"
  fi
done <<<"$lock_versions"

if [[ -n "$app_version" ]] && ! grep -Fq -- "make image TAG=$app_version" README.md; then
  violation "README.md does not document make image TAG=$app_version"
fi
if [[ -n "$app_version" ]] && ! grep -Fq -- "dosh/dosh:$app_version" README.md; then
  violation "README.md does not document image dosh/dosh:$app_version"
fi

if (( status != 0 )); then
  exit 1
fi

printf 'version checks: pass (app=%s, chart=%s)\n' "$app_version" "$chart_version"
