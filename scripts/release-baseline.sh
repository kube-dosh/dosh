#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BIN=$(mktemp "${TMPDIR:-/tmp}/dosh-releasebaseline.XXXXXX")
trap 'rm -f "$BIN"' EXIT

(
  cd "$ROOT"
  go build -trimpath -o "$BIN" ./tools/releasebaseline
)

"$BIN" "$@"
