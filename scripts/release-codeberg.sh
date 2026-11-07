#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

die() {
  printf 'release: %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

ref_name="${FORGEJO_REF_NAME:-${GITHUB_REF_NAME:-}}"
tag="${RELEASE_TAG_INPUT:-${RELEASE_TAG:-${ref_name}}}"
[[ -n "$tag" ]] || die "RELEASE_TAG_INPUT, RELEASE_TAG or FORGEJO_REF_NAME is required"
if [[ -n "${RELEASE_TAG_INPUT:-}" && "${RELEASE_TAG_INPUT}" != "$ref_name" ]]; then
  die "manual release tag ${RELEASE_TAG_INPUT} must match the checked out tag ${ref_name:-<unknown>}"
fi
[[ "$tag" =~ ^v([0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?)$ ]] || die "release tag must look like v1.2.3 or v1.2.3-rc.1: ${tag}"
version="${BASH_REMATCH[1]}"

server="${FORGEJO_SERVER_URL:-${GITHUB_SERVER_URL:-https://codeberg.org}}"
server="${server%/}"
registry="${CODEBERG_REGISTRY:-${server#*://}}"
registry="${registry%%/*}"
owner="${FORGEJO_REPOSITORY_OWNER:-${GITHUB_REPOSITORY_OWNER:-aido}}"
repo="${FORGEJO_REPOSITORY:-${GITHUB_REPOSITORY:-${owner}/dosh}}"
commit="${FORGEJO_SHA:-${GITHUB_SHA:-$(git rev-parse HEAD)}}"
token="${RELEASE_TOKEN:-${FORGEJO_TOKEN:-${GITHUB_TOKEN:-}}}"
skip_publish="${SKIP_PUBLISH:-0}"
stage="${RELEASE_STAGE:-all}"
registry_user="${CODEBERG_REGISTRY_USER:-${FORGEJO_ACTOR:-${GITHUB_ACTOR:-$owner}}}"

case "$stage" in
  all|artifacts|image|publish) ;;
  *) die "RELEASE_STAGE must be one of all, artifacts, image or publish: ${stage}" ;;
esac

need git
case "$stage" in
  all)
    for cmd in go npm helm sha256sum tar; do need "$cmd"; done
    if [[ "$skip_publish" != 1 ]]; then
      for cmd in ko jq curl; do need "$cmd"; done
    fi
    ;;
  artifacts)
    for cmd in go helm sha256sum tar; do need "$cmd"; done
    ;;
  image)
    for cmd in go ko curl; do need "$cmd"; done
    ;;
  publish)
    for cmd in jq curl; do need "$cmd"; done
    ;;
esac

source_version=$(sed -n 's/^VERSION ?= //p' Makefile | head -n1)
chart_version=$(awk '$1 == "version:" { print $2; exit }' deploy/helm/dosh/Chart.yaml)
chart_app_version=$(awk '$1 == "appVersion:" { gsub(/"/, "", $2); print $2; exit }' deploy/helm/dosh/Chart.yaml)
[[ "$source_version" == "$version" ]] || die "tag version ${version} does not match Makefile VERSION ${source_version}"
[[ "$chart_app_version" == "$version" ]] || die "tag version ${version} does not match Chart appVersion ${chart_app_version}"
bash scripts/check-version.sh

build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
ldflags="-s -w -X dosh/internal/version.Version=${version} -X dosh/internal/version.Commit=${commit} -X dosh/internal/version.Date=${build_date}"

ensure_web_dist() {
  [[ -f web/dist/index.html ]] || die "web/dist/index.html is missing; build the web app first"
}

build_web() {
  if [[ "${SKIP_WEB_BUILD:-0}" != 1 ]]; then
    npm --prefix web ci --no-audit --no-fund
    npm --prefix web run build
  fi
  ensure_web_dist
}

build_artifacts() {
  ensure_web_dist
  rm -rf dist
  mkdir -p dist/stage

  for goarch in amd64 arm64; do
    case "$goarch" in
      amd64) artifact_arch=amd64 ;;
      arm64) artifact_arch=aarch64 ;;
      *) die "unsupported architecture: $goarch" ;;
    esac

    binary="dist/dosh_${version}_linux_${artifact_arch}"
    CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build \
      -trimpath \
      -ldflags "$ldflags" \
      -o "$binary" \
      ./cmd/dosh

    mkdir -p "dist/stage/${artifact_arch}"
    cp "$binary" "dist/stage/${artifact_arch}/dosh"
    cp README.md "dist/stage/${artifact_arch}/README.md"
    tar -C "dist/stage/${artifact_arch}" -czf "dist/dosh_${version}_linux_${artifact_arch}.tar.gz" dosh README.md
  done

  helm lint deploy/helm/dosh
  helm package deploy/helm/dosh --destination dist >/dev/null
  chart_path="dist/dosh-${chart_version}.tgz"
  [[ -f "$chart_path" ]] || die "Helm chart package not found: ${chart_path}"

  (
    cd dist
    sha256sum \
      "dosh_${version}_linux_amd64" \
      "dosh_${version}_linux_aarch64" \
      "dosh_${version}_linux_amd64.tar.gz" \
      "dosh_${version}_linux_aarch64.tar.gz" \
      "$(basename "$chart_path")" \
      > SHA256SUMS
  )
  rm -rf dist/stage
}

build_image() {
  ensure_web_dist
  [[ -n "$token" ]] || die "RELEASE_TOKEN, FORGEJO_TOKEN or GITHUB_TOKEN is required when publishing"
  mkdir -p dist

  printf '%s' "$token" | ko login "$registry" --username "$registry_user" --password-stdin >/dev/null

  tag_args=(-t "$version")
  if [[ "$version" != *-* ]]; then
    tag_args+=(-t latest)
  fi

  KO_DOCKER_REPO="${registry}/${owner}" ko build \
    --base-import-paths \
    --platform=linux/amd64,linux/arm64 \
    --image-user=65532:65532 \
    --sbom=none \
    --ldflags "$ldflags" \
    "${tag_args[@]}" \
    --image-refs dist/image-refs.txt \
    ./cmd/dosh

  [[ -s dist/image-refs.txt ]] || die "ko did not write dist/image-refs.txt"
  image_ref=$(head -n1 dist/image-refs.txt)
  [[ -n "$image_ref" ]] || die "ko did not return an image reference"
  printf '%s\n' "$image_ref" > dist/container-image.txt
}

publish_release() {
  [[ -n "$token" ]] || die "RELEASE_TOKEN, FORGEJO_TOKEN or GITHUB_TOKEN is required when publishing"
  [[ -s dist/container-image.txt ]] || die "dist/container-image.txt is missing"
  [[ -f "dist/dosh-${chart_version}.tgz" ]] || die "Helm chart package is missing"
  image_ref=$(head -n1 dist/container-image.txt)
  chart_path="dist/dosh-${chart_version}.tgz"

  cat > dist/release-notes.md <<EOF
## Dosh ${tag}

- Source commit: \`${commit}\`
- Container image: \`${image_ref}\`
- Helm chart: \`dosh-${chart_version}.tgz\`

### Container image

\`\`\`bash
docker pull ${registry}/${owner}/dosh:${version}
\`\`\`

Supported platforms: \`linux/amd64\`, \`linux/arm64\`.

### Helm chart

\`\`\`bash
helm repo add dosh ${server}/api/packages/${owner}/helm
helm repo update
helm install dosh dosh/dosh --version ${chart_version}
\`\`\`

The chart archive is also attached to this release.

### Linux binaries

- \`dosh_${version}_linux_amd64.tar.gz\`: Linux x86-64
- \`dosh_${version}_linux_aarch64.tar.gz\`: Linux ARM64
- Raw binaries and \`SHA256SUMS\` are attached as well.
EOF

  api="${server}/api/v1"
  auth_header="Authorization: token ${token}"
  prerelease=false
  if [[ "$version" == *-* ]]; then
    prerelease=true
  fi
  release_payload=$(jq -n \
    --arg tag "$tag" \
    --arg name "Dosh ${tag}" \
    --rawfile body dist/release-notes.md \
    --argjson prerelease "$prerelease" \
    '{tag_name: $tag, name: $name, body: $body, draft: false, prerelease: $prerelease}')

  release_file=$(mktemp)
  status=$(curl -sS -o "$release_file" -w '%{http_code}' \
    -H "$auth_header" \
    "${api}/repos/${repo}/releases/tags/${tag}")
  case "$status" in
    200)
      release_id=$(jq -r '.id' "$release_file")
      curl -fsS -X PATCH \
        -H "$auth_header" \
        -H 'Content-Type: application/json' \
        -d "$release_payload" \
        "${api}/repos/${repo}/releases/${release_id}" >/dev/null
      ;;
    404)
      release_json=$(curl -fsS -X POST \
        -H "$auth_header" \
        -H 'Content-Type: application/json' \
        -d "$release_payload" \
        "${api}/repos/${repo}/releases")
      release_id=$(jq -r '.id' <<<"$release_json")
      ;;
    *)
      cat "$release_file" >&2
      die "failed to look up release ${tag}: HTTP ${status}"
      ;;
  esac
  rm -f "$release_file"
  [[ -n "$release_id" && "$release_id" != null ]] || die "release id is missing"

  upload_asset() {
    local path=$1
    local name
    name=$(basename "$path")
    local existing
    existing=$(curl -fsS -H "$auth_header" "${api}/repos/${repo}/releases/${release_id}/assets" \
      | jq -r --arg name "$name" '[.[] | select(.name == $name)][0].id // empty')
    if [[ -n "$existing" ]]; then
      curl -fsS -X DELETE -H "$auth_header" "${api}/repos/${repo}/releases/assets/${existing}" >/dev/null
    fi
    curl -fsS -X POST \
      -H "$auth_header" \
      -H 'Content-Type: application/octet-stream' \
      --data-binary "@${path}" \
      "${api}/repos/${repo}/releases/${release_id}/assets?name=${name}" >/dev/null
  }

  upload_asset "dist/dosh_${version}_linux_amd64"
  upload_asset "dist/dosh_${version}_linux_aarch64"
  upload_asset "dist/dosh_${version}_linux_amd64.tar.gz"
  upload_asset "dist/dosh_${version}_linux_aarch64.tar.gz"
  upload_asset dist/SHA256SUMS
  upload_asset dist/container-image.txt
  upload_asset "$chart_path"

  upload_status=$(curl -sS -o /tmp/dosh-helm-upload.out -w '%{http_code}' \
    --user "${registry_user}:${token}" \
    --upload-file "$chart_path" \
    "${server}/api/packages/${owner}/helm/api/charts")
  case "$upload_status" in
    200|201|409) ;;
    *)
      cat /tmp/dosh-helm-upload.out >&2
      die "failed to publish Helm chart: HTTP ${upload_status}"
      ;;
  esac

  printf 'release published: %s\nimage: %s\nchart: %s\n' "$tag" "$image_ref" "$chart_path"
}

case "$stage" in
  artifacts)
    build_artifacts
    printf 'release artifacts prepared: version=%s chart=%s\n' "$version" "$chart_version"
    ;;
  image)
    build_image
    printf 'release image published: version=%s image=%s\n' "$version" "$image_ref"
    ;;
  publish)
    publish_release
    ;;
  all)
    build_web
    build_artifacts
    if [[ "$skip_publish" == 1 ]]; then
      printf 'release artifacts prepared: version=%s chart=%s (publishing skipped)\n' "$version" "$chart_version"
      exit 0
    fi
    build_image
    publish_release
    ;;
esac
