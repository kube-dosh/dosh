#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

status=0
violation() {
  printf 'architecture violation: %s\n' "$*" >&2
  status=1
}

while IFS='|' read -r package imports; do
  [[ -n "$package" ]] || continue
  IFS=';' read -r -a dependencies <<<"$imports"

  case "$package" in
    dosh/internal/domain)
      for dependency in "${dependencies[@]}"; do
        [[ "$dependency" == *.* || "$dependency" == dosh/* ]] && \
          violation "$package imports non-stdlib dependency $dependency"
      done
      ;;
    dosh/internal/modules/*/app)
      module=${package#dosh/internal/modules/}
      module=${module%%/*}
      for dependency in "${dependencies[@]}"; do
        case "$dependency" in
          net/http|github.com/go-chi/*|k8s.io/*|dosh/internal/platform/*|dosh/internal/bootstrap)
            violation "$package imports transport, infrastructure, or composition dependency $dependency"
            ;;
          dosh/internal/modules/*)
            [[ "$dependency" == "dosh/internal/modules/$module/"* ]] || \
              violation "$package imports another module $dependency"
            ;;
        esac
      done
      ;;
    dosh/internal/modules/*/http)
      module=${package#dosh/internal/modules/}
      module=${module%%/*}
      for dependency in "${dependencies[@]}"; do
        case "$dependency" in
          k8s.io/*|dosh/internal/platform/kube|dosh/internal/bootstrap)
            violation "$package imports Kubernetes or composition dependency $dependency"
            ;;
          dosh/internal/modules/*)
            [[ "$dependency" == "dosh/internal/modules/$module/"* ]] || \
              violation "$package imports another module $dependency"
            ;;
        esac
      done
      ;;
    dosh/internal/platform/*)
      for dependency in "${dependencies[@]}"; do
        case "$dependency" in
          dosh/internal/bootstrap|dosh/internal/modules/*/http)
            violation "$package imports a higher-level adapter $dependency"
            ;;
        esac
      done
      ;;
  esac
done < <(go list -f '{{.ImportPath}}|{{join .Imports ";"}}' ./internal/...)

if matches=$(rg -n '^func init\(' internal cmd --glob '*.go' --glob '!*_test.go'); then
  violation "package init() is forbidden; got:\n$matches"
fi

if matches=$(rg -n 'chi\.NewRouter\(' internal --glob '*.go' --glob '!*_test.go' | rg -v '^internal/bootstrap/router\.go:' || true); [[ -n "$matches" ]]; then
  violation "chi.NewRouter() is only allowed in internal/bootstrap/router.go; got:\n$matches"
fi

if matches=$(rg -n '"/[^"]*(proxy|forward)[^"]*"' internal/modules/*/http internal/bootstrap/router.go --glob '*.go' || true); [[ -n "$matches" ]]; then
  violation "arbitrary proxy/forward routes are forbidden; got:\n$matches"
fi

if matches=$(rg -n '"/.*\{[^}]*(:\.\*|\*)\}' internal --glob '*.go' || true); [[ -n "$matches" ]]; then
  violation "wildcard HTTP routes are forbidden; got:\n$matches"
fi

if matches=$(rg -n 'os\.(Getenv|LookupEnv)' internal --glob '*.go' --glob '!*_test.go' | rg -v '^internal/platform/config/config\.go:' || true); [[ -n "$matches" ]]; then
  violation "environment access must be centralized in platform/config; got:\n$matches"
fi

if (( status != 0 )); then
  exit 1
fi

echo "architecture checks: pass"
