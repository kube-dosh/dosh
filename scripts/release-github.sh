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

tag="${RELEASE_TAG:-${GITHUB_REF_NAME:-}}"
[[ -n "$tag" ]] || die "RELEASE_TAG or GITHUB_REF_NAME is required"
[[ "$tag" =~ ^v([0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?)$ ]] || die "release tag must look like v1.2.3 or v1.2.3-rc.1: ${tag}"
version="${BASH_REMATCH[1]}"

repo="${GITHUB_REPOSITORY:-kube-dosh/dosh}"
owner="${GITHUB_REPOSITORY_OWNER:-${repo%%/*}}"
image_repo="${IMAGE_REPOSITORY:-ghcr.io/${owner}/dosh}"
chart_repo="${CHART_OCI_REPOSITORY:-oci://ghcr.io/${owner}/charts}"
registry="${image_repo%%/*}"
commit="${GITHUB_SHA:-$(git rev-parse HEAD)}"
token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
actor="${GITHUB_ACTOR:-${IMAGE_REGISTRY_USER:-}}"
skip_publish="${SKIP_PUBLISH:-0}"
stage="${RELEASE_STAGE:-all}"

case "$stage" in
  all|artifacts|image|publish) ;;
  *) die "RELEASE_STAGE must be one of all, artifacts, image or publish: ${stage}" ;;
esac

need git
case "$stage" in
  all)
    for cmd in go npm helm sha256sum tar; do need "$cmd"; done
    if [[ "$skip_publish" != 1 ]]; then
      for cmd in docker jq gh; do need "$cmd"; done
    fi
    ;;
  artifacts)
    for cmd in go helm sha256sum tar; do need "$cmd"; done
    ;;
  image)
    for cmd in docker jq; do need "$cmd"; done
    ;;
  publish)
    for cmd in gh helm; do need "$cmd"; done
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

ensure_image_binaries() {
  [[ -x bin/dosh-linux-amd64 ]] || die "bin/dosh-linux-amd64 is missing"
  [[ -x bin/dosh-linux-arm64 ]] || die "bin/dosh-linux-arm64 is missing"
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
  mkdir -p bin dist/stage

  for goarch in amd64 arm64; do
    case "$goarch" in
      amd64) artifact_arch=amd64 ;;
      arm64) artifact_arch=aarch64 ;;
      *) die "unsupported architecture: $goarch" ;;
    esac

    binary="bin/dosh-linux-${goarch}"
    CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build \
      -trimpath \
      -ldflags "$ldflags" \
      -o "$binary" \
      ./cmd/dosh

    cp "$binary" "dist/dosh_${version}_linux_${artifact_arch}"
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
  ensure_image_binaries
  [[ -n "$token" ]] || die "GH_TOKEN or GITHUB_TOKEN is required to publish the image"
  [[ -n "$actor" ]] || die "GITHUB_ACTOR is required to publish the image"
  mkdir -p dist

  printf '%s' "$token" | docker login "$registry" --username "$actor" --password-stdin >/dev/null

  tag_args=(--tag "${image_repo}:${version}")
  if [[ "$version" != *-* ]]; then
    tag_args+=(--tag "${image_repo}:latest")
  fi

  docker buildx build \
    --file Dockerfile.release \
    --platform=linux/amd64,linux/arm64 \
    --push \
    --metadata-file dist/image-metadata.json \
    "${tag_args[@]}" \
    .

  digest=$(jq -r '."containerimage.digest" // empty' dist/image-metadata.json)
  [[ -n "$digest" ]] || die "container image digest is missing from dist/image-metadata.json"
  printf '%s@%s\n' "$image_repo" "$digest" > dist/container-image.txt
}

publish_release() {
  [[ -n "$token" ]] || die "GH_TOKEN or GITHUB_TOKEN is required to publish the release"
  [[ -n "$actor" ]] || die "GITHUB_ACTOR is required to publish the release"
  [[ -s dist/container-image.txt ]] || die "dist/container-image.txt is missing"
  chart_path="dist/dosh-${chart_version}.tgz"
  [[ -f "$chart_path" ]] || die "Helm chart package is missing: ${chart_path}"
  image_ref=$(head -n1 dist/container-image.txt)

  cat > dist/release-notes.md <<EOF
## Dosh ${tag}

- Source commit: \`${commit}\`
- Container image: \`${image_ref}\`
- Helm chart: \`dosh-${chart_version}.tgz\`

### Container image

\`\`\`bash
docker pull ${image_repo}:${version}
\`\`\`

Supported platforms: \`linux/amd64\`, \`linux/arm64\`.

### Helm chart

\`\`\`bash
helm install dosh oci://${registry}/${owner}/charts/dosh \\
  --version ${chart_version} \\
  --namespace dosh
\`\`\`

The chart archive is also attached to this release.

### Linux binaries

- \`dosh_${version}_linux_amd64.tar.gz\`: Linux x86-64
- \`dosh_${version}_linux_aarch64.tar.gz\`: Linux ARM64
- Raw binaries and \`SHA256SUMS\` are attached as well.
EOF

  printf '%s' "$token" | helm registry login "$registry" --username "$actor" --password-stdin >/dev/null
  push_chart_with_retry "$chart_path" "$chart_repo"

  export GH_TOKEN="$token"
  if gh release view "$tag" >/dev/null 2>&1; then
    gh release edit "$tag" --title "Dosh ${tag}" --notes-file dist/release-notes.md
  else
    gh release create "$tag" \
      --title "Dosh ${tag}" \
      --notes-file dist/release-notes.md \
      --verify-tag
  fi

  gh release upload "$tag" \
    "dist/dosh_${version}_linux_amd64" \
    "dist/dosh_${version}_linux_aarch64" \
    "dist/dosh_${version}_linux_amd64.tar.gz" \
    "dist/dosh_${version}_linux_aarch64.tar.gz" \
    dist/SHA256SUMS \
    dist/container-image.txt \
    "$chart_path" \
    --clobber

  printf 'release published: %s\nimage: %s\nchart: %s (%s)\n' "$tag" "$image_ref" "$chart_path" "${chart_repo}/dosh:${chart_version}"
}

push_chart_with_retry() {
  local chart_path=$1
  local chart_repo=$2
  local chart_name
  local attempt delay

  chart_name=$(basename "$chart_path" .tgz)
  chart_name=${chart_name%-*}
  if helm show chart "${chart_repo}/${chart_name}" --version "$chart_version" >/dev/null 2>&1; then
    printf 'release: Helm chart already published: %s/%s:%s\n' "$chart_repo" "$chart_name" "$chart_version" >&2
    return 0
  fi

  for attempt in 1 2 3; do
    if helm push "$chart_path" "$chart_repo" >/dev/null; then
      return 0
    fi
    if (( attempt == 3 )); then
      printf 'release: helm push failed after %d attempts\n' "$attempt" >&2
      return 1
    fi
    delay=$((attempt * 10))
    printf 'release: helm push attempt %d failed; retrying in %ds\n' "$attempt" "$delay" >&2
    sleep "$delay"
  done
}

case "$stage" in
  artifacts)
    build_artifacts
    printf 'release artifacts prepared: version=%s chart=%s\n' "$version" "$chart_version"
    ;;
  image)
    build_image
    printf 'container image published: %s\n' "$(head -n1 dist/container-image.txt)"
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
