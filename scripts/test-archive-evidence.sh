#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ARCHIVER="$ROOT/scripts/archive-evidence.sh"
TMP=$(mktemp -d)
trap 'rm -rf -- "$TMP"' EXIT

DIGEST="sha256:$(printf 'a%.0s' {1..64})"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

mkdir -p "$TMP/source"
printf 'suite=evidence-archive\nresult=pass\n' >"$TMP/source/evidence.log"
printf 'scenario=negative-path\n' >"$TMP/source/case.txt"

bash "$ARCHIVER" \
  --candidate-digest "$DIGEST" \
  --label regression \
  --output-root "$TMP/archive" \
  "$TMP/source" >/dev/null

ARCHIVE=$(find "$TMP/archive" -mindepth 2 -maxdepth 2 -type d -name '*-regression' -print -quit)
[[ -n "$ARCHIVE" ]] || fail 'archive directory was not created'
[[ -f "$ARCHIVE/manifest.sha256" ]] || fail 'manifest.sha256 is missing'
[[ -f "$ARCHIVE/manifest-digest.txt" ]] || fail 'manifest-digest.txt is missing'
[[ -f "$ARCHIVE/summary.txt" ]] || fail 'summary.txt is missing'

grep -Eq '  \./summary\.txt$' "$ARCHIVE/manifest.sha256" || fail 'summary.txt is not covered by manifest.sha256'
grep -Eq '  \./metadata\.txt$' "$ARCHIVE/manifest.sha256" || fail 'metadata.txt is not covered by manifest.sha256'
if grep -q 'manifest-digest.txt' "$ARCHIVE/manifest.sha256"; then
  fail 'manifest-digest.txt must not be self-referential'
fi
if grep -q '^manifest_sha256=' "$ARCHIVE/summary.txt"; then
  fail 'summary.txt must not contain a self-referential manifest hash'
fi
(
  cd "$ARCHIVE"
  sha256sum -c manifest.sha256 >/dev/null
  sha256sum -c manifest-digest.txt >/dev/null
) || fail 'archive checksum verification failed'

printf 'credential-bearing log\nAuthorization: Bearer test-token-value\n' >"$TMP/bad-token.log"
if bash "$ARCHIVER" --candidate-digest "$DIGEST" --output-root "$TMP/archive" "$TMP/bad-token.log" >/dev/null 2>&1; then
  fail 'credential-bearing evidence was accepted'
fi

mkdir -p "$TMP/only-excluded"
printf 'redacted\n' >"$TMP/only-excluded/headers.abc123"
if bash "$ARCHIVER" --candidate-digest "$DIGEST" --output-root "$TMP/archive" "$TMP/only-excluded" >/dev/null 2>&1; then
  fail 'directory without safe evidence files was accepted'
fi

if bash "$ARCHIVER" --candidate-digest sha256:short --output-root "$TMP/archive" "$TMP/source/evidence.log" >/dev/null 2>&1; then
  fail 'invalid candidate digest was accepted'
fi

printf 'evidence archive checks: pass\n'
