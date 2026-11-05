#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=${DOSH_EVIDENCE_ROOT:-/var/lib/dosh/evidence}
LABEL=evidence
DIGEST=
SOURCES=()
STAGE=

usage() {
  cat >&2 <<'EOF'
Usage: archive-evidence.sh --candidate-digest sha256:<64 hex> [--label <name>] [--output-root <dir>] <evidence-file-or-dir>...

Environment:
  DOSH_EVIDENCE_ROOT  Override the default archive root /var/lib/dosh/evidence.

Only explicitly listed files and directories are archived. Private keys, certificates,
cookies, sessions, fixture metadata, request/response header-body dumps, password/secret
files and logs containing credential headers are excluded or rejected.
EOF
  exit 2
}

die() { printf 'error: %s\n' "$*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --candidate-digest)
      [[ $# -ge 2 ]] || usage
      DIGEST=$2
      shift 2
      ;;
    --label)
      [[ $# -ge 2 ]] || usage
      LABEL=$2
      shift 2
      ;;
    --output-root)
      [[ $# -ge 2 ]] || usage
      ROOT=$2
      shift 2
      ;;
    -h|--help)
      usage
      ;;
    --)
      shift
      while [[ $# -gt 0 ]]; do SOURCES+=("$1"); shift; done
      ;;
    -*)
      die "unknown option: $1"
      ;;
    *)
      SOURCES+=("$1")
      shift
      ;;
  esac
done

[[ "$DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || die "candidate digest must be sha256:<64 lowercase hex>"
[[ "$LABEL" =~ ^[A-Za-z0-9._-]+$ ]] || die "label may contain only letters, digits, dot, underscore and dash"
[[ ${#SOURCES[@]} -gt 0 ]] || usage
command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"

for src in "${SOURCES[@]}"; do
  [[ -f "$src" || -d "$src" ]] || die "evidence source does not exist: $src"
  [[ -r "$src" ]] || die "evidence source is not readable: $src"
done

SHORT=${DIGEST#sha256:}
SHORT=${SHORT:0:12}
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
FINAL="$ROOT/$SHORT/${STAMP}-${LABEL}"
[[ ! -e "$FINAL" ]] || die "archive destination already exists: $FINAL"

umask 077
mkdir -p "$ROOT/$SHORT"
STAGE=$(mktemp -d "$ROOT/$SHORT/.archive.XXXXXX")
cleanup() {
  if [[ -n "$STAGE" && -d "$STAGE" ]]; then
    rm -rf -- "$STAGE"
  fi
}
trap cleanup EXIT

mkdir -p "$STAGE/files"
: >"$STAGE/excluded.txt"
index=0
file_count=0
contains_credentials() {
  LC_ALL=C grep -Eiq 'BEGIN ([A-Z]+ )?PRIVATE KEY|Authorization:[[:space:]]*Bearer[[:space:]]+[A-Za-z0-9._~+/-]+|Cookie:[[:space:]]*[^[:space:]]+|DOSH_(ADMIN_PASSWORD_HASH|SESSION_KEY|OIDC_CLIENT_SECRET|AUDIT_WEBHOOK_(TOKEN|HMAC_KEY))[[:space:]]*=' "$1"
}
is_excluded_path() {
  local rel=$1 base=${1##*/}
  case "/$rel/" in
    */fixture-certs/*|*/.git/*|*/node_modules/*) return 0 ;;
  esac
  case "$base" in
    *.key|*.pem|*.crt|*.cookie|*cookie*|*session*|*password*|*secret*|fixture.json|headers.*|body.*) return 0 ;;
  esac
  return 1
}
for src in "${SOURCES[@]}"; do
  index=$((index + 1))
  base=$(basename -- "$src")
  if [[ -f "$src" ]]; then
    if is_excluded_path "$base"; then
      die "refusing to archive sensitive file name: $base"
    fi
    contains_credentials "$src" && die "refusing to archive credential material found in: $src"
    name=$(printf '%02d-%s' "$index" "$base")
    cp --preserve=mode,timestamps -- "$src" "$STAGE/files/$name"
    file_count=$((file_count + 1))
    continue
  fi

  copied=0
  while IFS= read -r -d '' entry; do
    rel=${entry#"$src"/}
    if is_excluded_path "$rel"; then
      printf '%s\n' "$rel" >>"$STAGE/excluded.txt"
      continue
    fi
    contains_credentials "$entry" && die "refusing to archive credential material found in: $entry"
    dest_dir="$STAGE/files/$(printf '%02d-%s' "$index" "$base")/$(dirname -- "$rel")"
    mkdir -p "$dest_dir"
    cp --preserve=mode,timestamps -- "$entry" "$dest_dir/"
    copied=$((copied + 1))
    file_count=$((file_count + 1))
  done < <(find "$src" -type f -print0 | LC_ALL=C sort -z)
  (( copied > 0 )) || die "no safe evidence files found in directory: $src"
done
(( file_count > 0 )) || die "no evidence files selected"

{
  printf 'candidate_digest=%s\n' "$DIGEST"
  printf 'label=%s\n' "$LABEL"
  printf 'created_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'host=%s\n' "$(hostname -f 2>/dev/null || hostname)"
  printf 'source_count=%s\n' "${#SOURCES[@]}"
  for src in "${SOURCES[@]}"; do
    if [[ -d "$src" ]]; then
      printf 'source_dir=%s\n' "$src"
    else
      printf 'source=%s\n' "$src"
    fi
  done
} >"$STAGE/metadata.txt"

FILE_COUNT=$file_count
TOTAL_BYTES=$(find "$STAGE/files" -type f -printf '%s\n' | awk '{sum += $1} END {print sum + 0}')
{
  printf 'archive_status=ready\n'
  printf 'candidate_digest=%s\n' "$DIGEST"
  printf 'file_count=%s\n' "$FILE_COUNT"
  printf 'total_bytes=%s\n' "$TOTAL_BYTES"
} >"$STAGE/summary.txt"

(
  cd "$STAGE"
  find . -type f ! -name manifest.sha256 ! -name manifest-digest.txt -print0 \
    | LC_ALL=C sort -z | xargs -0 sha256sum >manifest.sha256
)

MANIFEST_HASH=$(sha256sum "$STAGE/manifest.sha256" | awk '{print $1}')
printf '%s  manifest.sha256\n' "$MANIFEST_HASH" >"$STAGE/manifest-digest.txt"

mv -- "$STAGE" "$FINAL"
STAGE=
chmod -R go-rwx "$FINAL"
(
  cd "$FINAL"
  sha256sum -c manifest.sha256 >/dev/null
  sha256sum -c manifest-digest.txt >/dev/null
)

printf 'evidence archived: %s\n' "$FINAL"
printf 'manifest: %s/manifest.sha256\n' "$FINAL"
printf 'manifest_digest: %s/manifest-digest.txt\n' "$FINAL"
printf 'manifest_sha256: %s\n' "$MANIFEST_HASH"
printf 'files: %s\n' "$FILE_COUNT"
