#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART="$ROOT/deploy/helm/dosh"

if [[ -n "${ARTIFACT_DIR:-}" ]]; then
  OUT="$ARTIFACT_DIR"
  mkdir -p "$OUT"
  CLEANUP=0
else
  OUT="$(mktemp -d)"
  CLEANUP=1
fi
trap '[[ "$CLEANUP" == 1 ]] && rm -rf "$OUT"' EXIT

helm lint "$CHART"

helm template dosh "$CHART" -n dosh --set auth.mode=local >"$OUT/dosh-local.yaml"
helm template dosh "$CHART" -n dosh --set auth.mode=local --set rbac.localReadAllResources=false >"$OUT/dosh-local-restricted.yaml"
helm template dosh "$CHART" -n dosh >"$OUT/dosh-token.yaml"
helm template dosh "$CHART" -n dosh \
  --set tls.enabled=false \
  --set auth.cookieSecure=false \
  >"$OUT/dosh-http.yaml"
helm template dosh "$CHART" -n dosh \
  --set image.digest=sha256:1111111111111111111111111111111111111111111111111111111111111111 \
  >"$OUT/dosh-digest.yaml"
helm template dosh "$CHART" -n dosh \
  --api-versions cilium.io/v2 \
  --set auth.mode=local \
  >"$OUT/dosh-local-cilium.yaml"
helm template dosh "$CHART" -n dosh \
  --set auth.mode=oidc \
  --set auth.oidc.issuer=https://idp.example.test \
  --set auth.oidc.clientID=dosh \
  --set auth.oidc.redirectURL=https://dosh.example.test:32626/api/v1/auth/oidc/callback \
  --set auth.oidc.ca.existingConfigMap=dosh-oidc-ca \
  --set networkPolicy.oidc.ipBlocks[0]=203.0.113.20/32 \
  >"$OUT/dosh-oidc.yaml"
helm template dosh "$CHART" -n dosh \
  --api-versions cilium.io/v2 \
  --set auth.mode=oidc \
  --set auth.oidc.issuer=https://idp.example.test \
  --set auth.oidc.clientID=dosh \
  --set auth.oidc.redirectURL=https://dosh.example.test:32626/api/v1/auth/oidc/callback \
  --set networkPolicy.oidc.ipBlocks[0]=203.0.113.20/32 \
  >"$OUT/dosh-oidc-cilium.yaml"
helm template dosh "$CHART" -n dosh \
  --set audit.webhook.enabled=true \
  --set audit.webhook.url=https://siem.example.test/audit \
  --set audit.webhook.tokenKey=token \
  --set audit.webhook.hmacKeyKey= \
  --set audit.webhook.ca.existingConfigMap=dosh-audit-ca \
  --set networkPolicy.webhook.ipBlocks[0]=203.0.113.30/32 \
  >"$OUT/dosh-audit.yaml"
helm template dosh "$CHART" -n dosh \
  --api-versions cilium.io/v2 \
  --set audit.webhook.enabled=true \
  --set audit.webhook.url=https://siem.example.test/audit \
  --set audit.webhook.tokenKey=token \
  --set audit.webhook.hmacKeyKey= \
  --set audit.webhook.ca.existingConfigMap=dosh-audit-ca \
  --set networkPolicy.webhook.ipBlocks[0]=203.0.113.30/32 \
  >"$OUT/dosh-audit-cilium.yaml"

require() {
  local file="$1"
  local pattern="$2"
  local description="$3"
  if ! grep -Eq -- "$pattern" "$file"; then
    echo "missing $description in $file: $pattern" >&2
    exit 1
  fi
}

reject() {
  local file="$1"
  local pattern="$2"
  local description="$3"
  if grep -Eq -- "$pattern" "$file"; then
    echo "unexpected $description in $file: $pattern" >&2
    exit 1
  fi
}

assert_manifest_headers() {
  local file="$1"
  if ! awk '
    function flush() {
      if (doc == "") return
      if (doc !~ /(^|\n)apiVersion:[[:space:]]*[^[:space:]]/) {
        printf "invalid YAML document without apiVersion in %s: %.120s\n", FILENAME, doc > "/dev/stderr"
        failed = 1
        exit 1
      }
      doc = ""
    }
    /^---[[:space:]]*$/ { flush(); next }
    /^[[:space:]]*#/ { next }
    /^[[:space:]]*$/ { next }
    { doc = doc $0 "\n" }
    END { if (!failed) flush() }
  ' "$file"; then
    exit 1
  fi
}

for rendered in "$OUT"/*.yaml; do
  assert_manifest_headers "$rendered"
done

require "$OUT/dosh-local.yaml" 'nodePort: 32626' 'local NodePort 32626'
require "$OUT/dosh-local.yaml" 'replicas: 2' 'local two replicas'
require "$OUT/dosh-local.yaml" 'name: DOSH_AUTH_MODE' 'local auth mode env'
require "$OUT/dosh-local.yaml" 'name: DOSH_ADMIN_USERNAME' 'local admin env'
require "$OUT/dosh-local.yaml" 'name: DOSH_LOGIN_RATE_LIMIT_MODE' 'local distributed login limiter env'
require "$OUT/dosh-local.yaml" 'value: "kubernetes"' 'local distributed login limiter mode'
require "$OUT/dosh-local.yaml" 'name: DOSH_LOGIN_RATE_LIMIT_NAMESPACE' 'local distributed login limiter namespace'
require "$OUT/dosh-local.yaml" 'kind: Role' 'local login limiter Role'
require "$OUT/dosh-local.yaml" 'resources: \["leases"\]' 'local login limiter Lease RBAC'
require "$OUT/dosh-local.yaml" 'verbs: \["get", "create", "update"\]' 'local login limiter Lease verbs'
require "$OUT/dosh-local.yaml" 'resources: \["endpointslices"\]' 'local EndpointSlice read RBAC'
require "$OUT/dosh-local.yaml" 'resources: \["ingresses", "ingressclasses"\]' 'local IngressClass read RBAC'
require "$OUT/dosh-local.yaml" 'resources: \["resourcequotas", "limitranges"\]' 'local namespace quota and limit read RBAC'
require "$OUT/dosh-local.yaml" 'apiGroups: \["\*"\]' 'local generic resource read-all RBAC group'
require "$OUT/dosh-local.yaml" 'resources: \["\*"\]' 'local generic resource read-all RBAC resources'
require "$OUT/dosh-local.yaml" 'nonResourceURLs: \["/api", "/api/\*", "/apis", "/apis/\*"' 'local API discovery non-resource RBAC'
require "$OUT/dosh-local.yaml" 'value: "2097152"' 'integer max request body'
reject "$OUT/dosh-local.yaml" 'name: DOSH_OIDC_ISSUER' 'local OIDC env'
reject "$OUT/dosh-local-restricted.yaml" 'resources: \["\*"\]' 'restricted local generic resource read-all RBAC'
require "$OUT/dosh-digest.yaml" 'image: "ghcr.io/kube-dosh/dosh@sha256:1111111111111111111111111111111111111111111111111111111111111111"' 'digest-pinned image'

require "$OUT/dosh-token.yaml" 'value: "token"' 'token auth mode value'
require "$OUT/dosh-token.yaml" 'name: DOSH_TLS_RELOAD_INTERVAL' 'TLS hot reload interval env'
require "$OUT/dosh-token.yaml" 'value: "30s"' 'TLS hot reload interval value'
require "$OUT/dosh-token.yaml" 'name: DOSH_TLS_CERT_FILE' 'TLS certificate file env'
require "$OUT/dosh-token.yaml" 'value: "/etc/dosh/tls/tls.crt"' 'TLS certificate file path'
require "$OUT/dosh-token.yaml" 'name: DOSH_TLS_KEY_FILE' 'TLS private key file env'
require "$OUT/dosh-token.yaml" 'value: "/etc/dosh/tls/tls.key"' 'TLS private key file path'
require "$OUT/dosh-token.yaml" 'secretName: dosh-tls' 'TLS Secret volume'
require "$OUT/dosh-token.yaml" 'mountPath: /etc/dosh/tls' 'TLS Secret read-only mount'
require "$OUT/dosh-token.yaml" 'defaultMode: 0440' 'TLS Secret file mode'
require "$OUT/dosh-token.yaml" 'name: DOSH_TOKEN_REVIEW_AUDIENCE' 'token review audience env'
require "$OUT/dosh-token.yaml" 'resources: \["users", "groups"\]' 'token impersonation RBAC'
require "$OUT/dosh-token.yaml" 'verbs: \["impersonate"\]' 'token impersonate verb'
require "$OUT/dosh-token.yaml" 'resources: \["subjectaccessreviews"\]' 'token SAR RBAC'
require "$OUT/dosh-token.yaml" 'resources: \["tokenreviews"\]' 'token TokenReview RBAC'
require "$OUT/dosh-token.yaml" 'resources: \["serviceaccounts"\]' 'token ServiceAccount impersonation RBAC'
require "$OUT/dosh-token.yaml" 'name: DOSH_LOGIN_RATE_LIMIT_MODE' 'token distributed login limiter env'
require "$OUT/dosh-token.yaml" 'resources: \["leases"\]' 'token login limiter Lease RBAC'
reject "$OUT/dosh-token.yaml" 'name: DOSH_ADMIN_PASSWORD_HASH' 'token local admin password env'
reject "$OUT/dosh-token.yaml" 'name: DOSH_OIDC_ISSUER' 'token OIDC env'

require "$OUT/dosh-http.yaml" 'name: DOSH_TLS_ENABLED' 'HTTP TLS disabled env'
require "$OUT/dosh-http.yaml" 'value: "false"' 'HTTP TLS disabled value'
require "$OUT/dosh-http.yaml" 'scheme: HTTP' 'HTTP probes'
require "$OUT/dosh-http.yaml" 'name: DOSH_COOKIE_SECURE' 'HTTP cookie policy env'
reject "$OUT/dosh-http.yaml" 'scheme: HTTPS' 'HTTP probes must not use HTTPS'

require "$OUT/dosh-oidc.yaml" 'nodePort: 32626' 'OIDC NodePort 32626'
require "$OUT/dosh-oidc.yaml" 'replicas: 2' 'OIDC two replicas'
require "$OUT/dosh-oidc.yaml" 'value: "oidc"' 'OIDC auth mode value'
require "$OUT/dosh-oidc.yaml" 'name: DOSH_OIDC_ISSUER' 'OIDC issuer env'
require "$OUT/dosh-oidc.yaml" 'name: DOSH_OIDC_CLIENT_SECRET' 'OIDC client secret env'
require "$OUT/dosh-oidc.yaml" 'key: client-secret' 'OIDC client secret key reference'
require "$OUT/dosh-oidc.yaml" 'name: DOSH_OIDC_CA_FILE' 'OIDC CA file env'
require "$OUT/dosh-oidc.yaml" 'name: DOSH_OIDC_STATE_TTL' 'OIDC state TTL env'
require "$OUT/dosh-oidc.yaml" 'value: "10m"' 'OIDC default state TTL'
require "$OUT/dosh-oidc.yaml" 'name: oidc-ca' 'OIDC CA volume'
require "$OUT/dosh-oidc.yaml" 'resources: \["users", "groups"\]' 'OIDC impersonation RBAC'
require "$OUT/dosh-oidc.yaml" 'verbs: \["impersonate"\]' 'OIDC impersonate verb'
require "$OUT/dosh-oidc.yaml" 'resources: \["subjectaccessreviews"\]' 'OIDC SAR RBAC'
require "$OUT/dosh-oidc.yaml" 'cidr: 203.0.113.20/32' 'OIDC IdP egress CIDR'
reject "$OUT/dosh-oidc.yaml" 'name: DOSH_ADMIN_PASSWORD_HASH' 'OIDC local admin password env'
reject "$OUT/dosh-oidc.yaml" 'name: DOSH_LOGIN_RATE_LIMIT_MODE' 'OIDC local login limiter env'
reject "$OUT/dosh-oidc.yaml" 'resources: \["leases"\]' 'OIDC local login limiter RBAC'
reject "$OUT/dosh-oidc.yaml" 'resources: \["endpointslices"\]' 'OIDC direct EndpointSlice RBAC'
reject "$OUT/dosh-oidc.yaml" 'resources: \["deployments", "statefulsets", "daemonsets"\]' 'OIDC business resource RBAC'
reject "$OUT/dosh-oidc.yaml" 'resources: \["pods", "pods/log"' 'OIDC pod resource RBAC'

require "$OUT/dosh-oidc-cilium.yaml" 'kind: CiliumNetworkPolicy' 'OIDC Cilium policy'
require "$OUT/dosh-oidc-cilium.yaml" 'toCIDR:' 'OIDC Cilium IdP egress'
require "$OUT/dosh-oidc-cilium.yaml" '203.0.113.20/32' 'OIDC Cilium IdP CIDR'
reject "$OUT/dosh-oidc-cilium.yaml" '0.0.0.0/0' 'OIDC Cilium broad egress'

require "$OUT/dosh-audit.yaml" 'name: DOSH_AUDIT_WEBHOOK_TOKEN' 'audit bearer token env'
require "$OUT/dosh-audit.yaml" 'key: token' 'audit bearer token key reference'
reject "$OUT/dosh-audit.yaml" 'name: DOSH_AUDIT_WEBHOOK_HMAC_KEY' 'audit HMAC env with token-only config'
require "$OUT/dosh-audit.yaml" 'name: DOSH_AUDIT_WEBHOOK_CA_FILE' 'audit CA file env'
require "$OUT/dosh-audit.yaml" 'name: audit-ca' 'audit CA volume'
require "$OUT/dosh-audit.yaml" 'cidr: 203.0.113.30/32' 'audit webhook egress CIDR'

require "$OUT/dosh-audit-cilium.yaml" 'kind: CiliumNetworkPolicy' 'audit Cilium policy'
require "$OUT/dosh-audit-cilium.yaml" 'cidr: 203.0.113.30/32' 'audit Cilium webhook egress CIDR'
require "$OUT/dosh-audit-cilium.yaml" 'port: "443"' 'audit Cilium webhook egress port'
reject "$OUT/dosh-audit-cilium.yaml" '0.0.0.0/0' 'audit Cilium broad egress'

expect_failure() {
  local description="$1"
  shift
  if helm template dosh "$CHART" -n dosh "$@" >/dev/null 2>&1; then
    echo "expected failure: $description" >&2
    exit 1
  fi
}

expect_failure 'OIDC without IdP egress CIDR' \
  --set auth.mode=oidc \
  --set auth.oidc.issuer=https://idp.example.test \
  --set auth.oidc.clientID=dosh \
  --set auth.oidc.redirectURL=https://dosh.example.test:32626/api/v1/auth/oidc/callback
expect_failure 'OIDC with non-HTTPS issuer' \
  --set auth.mode=oidc \
  --set auth.oidc.issuer=http://idp.example.test \
  --set auth.oidc.clientID=dosh \
  --set auth.oidc.redirectURL=https://dosh.example.test:32626/api/v1/auth/oidc/callback \
  --set networkPolicy.oidc.ipBlocks[0]=203.0.113.20/32
expect_failure 'unsupported auth mode' --set auth.mode=ldap
expect_failure 'TLS enabled without existing Secret' --set tls.enabled=true --set tls.existingSecret=
expect_failure 'invalid image digest' --set image.digest=sha256:not-a-digest
expect_failure 'audit webhook without token or HMAC' \
  --set audit.webhook.enabled=true \
  --set audit.webhook.url=https://siem.example.test/audit \
  --set audit.webhook.hmacKeyKey= \
  --set networkPolicy.webhook.ipBlocks[0]=203.0.113.30/32
expect_failure 'audit webhook with non-HTTPS URL' \
  --set audit.webhook.enabled=true \
  --set audit.webhook.url=http://siem.example.test/audit \
  --set audit.webhook.tokenKey=token \
  --set audit.webhook.hmacKeyKey= \
  --set networkPolicy.webhook.ipBlocks[0]=203.0.113.30/32
expect_failure 'audit webhook without egress CIDR under Cilium' \
  --api-versions cilium.io/v2 \
  --set audit.webhook.enabled=true \
  --set audit.webhook.url=https://siem.example.test/audit \
  --set audit.webhook.tokenKey=token \
  --set audit.webhook.hmacKeyKey=

echo "helm render checks: pass"
