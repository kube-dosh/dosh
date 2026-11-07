# Commercial Release Gates

`commercial-gates.json` binds the release decision to one immutable candidate digest and to every
commercial gate that is still external to the repository. It is a control record, not an approval by
itself. The file must not contain passwords, tokens, private keys, cookies, session values or secret
material.

## Required Gates

The validator requires exactly these gate IDs:

- `production-siem-retention-alerting`: SIEM delivery, tenant isolation, retention, alerting and access approval.
- `edge-safari-real`: real Edge and macOS/iOS Safari execution; WebKit containers do not satisfy this gate.
- `github-actions-branch-protection`: real GitHub Actions runs, retained artifacts and protected `main`.
- `offsite-worm-retention`: off-host WORM evidence retention and documented access approval.
- `production-change-authorization`: named change ticket, maintenance window, rollback owner and explicit authorization.

`production-change-authorization` cannot use `risk-accepted`; it must be `passed`.

## Statuses

- `pending`: release is blocked. An owner may be recorded while closure work is in progress.
- `passed`: requires distinct owner and approver, RFC3339 verification time, unexpired RFC3339 expiry,
  at least one HTTPS or local evidence item, and the SHA-256 of that evidence.
- `risk-accepted`: requires distinct owner and approver, written acceptance with document SHA-256,
  rationale, containment, rollback plan and an unexpired expiry. It is allowed only where the gate
  definition permits it.

Local evidence paths are resolved beneath `/var/lib/dosh` by `make release-gate-check`; paths that
escape that root are rejected. The validator also detects expired evidence, invalid timestamps,
missing evidence, duplicate/unknown gates, invalid candidate digests, failed evidence hashes and
unapproved production changes.

## Validation

```bash
make release-gate-check

# Evaluate against a fixed time for a reproducible release review:
bash scripts/release-gate.sh validate \
  --file deploy/release/commercial-gates.json \
  --evidence-root /var/lib/dosh \
  --binding-root . \
  --now 2026-11-04T08:00:00Z
```

Exit codes are intentionally distinct:

- `0`: every required gate is passed or covered by a valid risk acceptance.
- `1`: the manifest is structurally invalid or evidence cannot be verified.
- `2`: the manifest is valid, but the release is blocked by pending or expired gates.

CI runs `make release-gate-schema-check`, which uses `--allow-blocked --skip-bindings` to validate the
schema, candidate fields and evidence formats without pretending that pending external gates are
approved. `--skip-bindings` is intentional for pull-request and branch CI: bindings name an immutable
candidate digest that does not exist until the release build. It still validates binding path/hash
pairing and digest syntax, but does not compare the checked-in binding contents with the current source
tree. A production decision must run `make release-gate-check`, which performs full binding validation,
and require exit code `0`.

## Gate Report

Generate a deterministic Markdown report for release review or evidence retention:

```bash
make release-report \
  REPORT_OUT=/var/lib/dosh/release-reports/<ticket>-gate-report.md \
  GATE_NOW=<RFC3339>
```

The report records the candidate, gate manifest SHA-256, evaluation time, every gate owner/approver,
expiry and change ticket, evidence hashes, blockers and validation errors. It is written with a
sibling `.sha256` file. The command exits `2` while gates are blocked, but still writes the report so
the blocking evidence can be reviewed and retained. Add `REPORT_ALLOW_BLOCKED=1` only when a blocked
report is intentionally being archived; it never changes any gate status. The report contains no
credentials, session material or raw Secret data.

Candidate identity is bound to checked-in copies of the source and documentation bindings. The
validator checks the binding file hashes, candidate digest/tag/app/chart fields, every listed source
and documentation file hash, and the source tree hash. Any runtime source or bound-document change
invalidates the gate until a new candidate and new bindings are produced.

The current manifest is bound to candidate
`0.1.5-rc-token-20261105T012750Z@sha256:c114113009ddad67313f37c288fe4cc6f7d13848de01d22dd68499146744ea2b`
and intentionally reports every external gate as `pending`. v2.47 makes Kubernetes TokenReview the
default login, keeps OIDC and custom CAs optional, and supports both HTTP and HTTPS. It also grants the
minimum `serviceaccounts/impersonate` permission required for ServiceAccount tokens and uses
Kubernetes Lease-backed login rate limiting in token mode.

The isolated acceptance environment is `dosh-token-e2e/dosh-token-e2e`, NodePort `32628`; production
`dosh` remains App `0.1.2` / Chart `0.1.4` / revision 12 on NodePort `32626`. v2.46 and earlier evidence
is retained for traceability but is not inherited by v2.47. The documentation binding records the
evidence archive manifest and all bound document hashes. Do not change a gate status without adding the
signed approval evidence and regenerating the release record.

The v2.47 source binding is `deploy/release/bindings/source-binding-v247-token.txt`. Its laboratory
evidence archive is `/var/lib/dosh/evidence/c114113009dd/20261105T014514Z-v247-final-token-r1`
(23 files, manifest SHA-256 `734cbdbc10b15e4d48f56665eed24e5e170ec68622ce8a5390b0a92114570005`).
This archive does not approve any external gate and does not authorize a production change.

## Evidence Hash

For a local evidence file:

```bash
sha256sum /var/lib/dosh/e2e-runs/<run>/<evidence-file>
```

Record the result as `sha256:<64 lowercase hex>`. For an external evidence URL, retain a downloaded
copy in controlled storage and hash that retained copy; do not use a mutable dashboard URL as the only
evidence.

## Production Baseline

Before an authorized production change, capture the read-only release baseline:

```bash
make release-baseline \
  BASELINE_OUT=/var/lib/dosh/release-baselines/$(date -u +%Y%m%dT%H%M%SZ)-prod-dosh.json
```

The command uses only `helm list/history/get values` and `kubectl get/version/config current-context`.
It records the Helm revision/chart/application version, a SHA-256 of raw values without storing those
values, Deployment image and readiness, Service NodePorts, PDB status, Pod placement/restarts and
Secret names/key names. Secret data is never emitted. The output is paired with a standard
`<baseline>.sha256` file.

Before changing production, verify that the live release still matches the authorized baseline:

```bash
make release-baseline-verify \
  BASELINE_IN=/var/lib/dosh/release-baselines/<approved-baseline>.json
```

Verification fails with exit code `2` on Helm revision/chart/app drift, image or values-hash drift,
NodePort/Service changes, PDB specification changes, Secret name/type/key changes, Pod count or
readiness changes, or a PDB that currently allows no voluntary disruption. Pod names and restart
counters are intentionally ignored. A sibling `.sha256` file is mandatory when present and must match.

## Release Plan

After all commercial gates are approved, generate a deterministic change plan:

```bash
make release-plan \
  BASELINE_IN=/var/lib/dosh/release-baselines/<approved-baseline>.json \
  PLAN_OUT=/var/lib/dosh/release-plans/<ticket>-release-plan.json \
  CHANGE_TICKET=<change-ticket> \
  WINDOW_START=<RFC3339> \
  WINDOW_END=<RFC3339>
```

Plan generation refuses to run while any gate is pending or expired, requires the ticket to match the
passed `production-change-authorization` gate, and requires a fully ready read-only baseline. The
result binds the gate manifest hash, baseline hash, target image digest, Helm upgrade command,
post-release checks and rollback command, and is paired with a `.sha256` file. Creating a plan does
not execute Helm or Kubernetes writes. The baseline and its sibling `.sha256` file are both required;
plan generation rejects a missing or mismatched baseline checksum.
