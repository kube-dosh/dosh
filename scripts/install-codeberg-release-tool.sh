#!/usr/bin/env bash
set -Eeuo pipefail

die() {
  printf 'install-release-tool: %s\n' "$*" >&2
  exit 1
}

tool="${1:-}"
case "$tool" in
  helm|ko) ;;
  *) die "usage: $0 helm|ko" ;;
esac

case "$(uname -m)" in
  x86_64)
    helm_arch=amd64
    ko_arch=x86_64
    ;;
  aarch64|arm64)
    helm_arch=arm64
    ko_arch=arm64
    ;;
  *) die "unsupported runner architecture: $(uname -m)" ;;
esac

install_dir="${INSTALL_DIR:-${HOME}/.local/bin}"
mkdir -p "$install_dir"
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

download() {
  curl -fsSL --connect-timeout 15 --max-time 180 --retry 1 --retry-delay 2 -o "$1" "$2"
}

case "$tool" in
  helm)
    version="${HELM_VERSION:?HELM_VERSION is required}"
    archive="helm-v${version}-linux-${helm_arch}.tar.gz"
    download "${workdir}/${archive}" "https://get.helm.sh/${archive}"
    download "${workdir}/${archive}.sha256sum" "https://get.helm.sh/${archive}.sha256sum"
    (cd "$workdir" && sha256sum -c "${archive}.sha256sum")
    tar -xzf "${workdir}/${archive}" -C "$workdir"
    install -m 0755 "${workdir}/linux-${helm_arch}/helm" "${install_dir}/helm"
    ;;
  ko)
    version="${KO_VERSION:?KO_VERSION is required}"
    archive="ko_${version}_Linux_${ko_arch}.tar.gz"
    base_url="https://github.com/ko-build/ko/releases/download/v${version}"
    download "${workdir}/${archive}" "${base_url}/${archive}"
    download "${workdir}/checksums.txt" "${base_url}/checksums.txt"
    (cd "$workdir" && grep " ${archive}$" checksums.txt | sha256sum -c -)
    tar -xzf "${workdir}/${archive}" -C "$workdir"
    install -m 0755 "${workdir}/ko" "${install_dir}/ko"
    ;;
esac

printf 'installed %s into %s\n' "$tool" "$install_dir"
