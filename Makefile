SHELL := /bin/bash
GO ?= go
NPM ?= npm
IMAGE ?= 192.168.122.10:5000/dosh/dosh
TAG ?= dev
TRIVY_IMAGE ?= ghcr.io/aquasecurity/trivy@sha256:2e6c00d711ab4ae41d8a30c2c1fd3bd3b62704c177bb120f95012e939edfb3df
TRIVY_TIMEOUT ?= 20m
TRIVY_GODEBUG ?= http2client=0
TRIVY_DB_FLAGS ?=
IMAGE_TAR ?= /tmp/dosh-image-$(TAG).tar
VERSION ?= 0.1.6
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GATE_FILE ?= deploy/release/commercial-gates.json
NAMESPACE ?= dosh
RELEASE ?= dosh
BASELINE_OUT ?=
BASELINE_IN ?=
PLAN_OUT ?=
CHANGE_TICKET ?=
WINDOW_START ?=
WINDOW_END ?=
REPORT_OUT ?=
GATE_NOW ?=
REPORT_ALLOW_BLOCKED ?=

.PHONY: all fmt vet test race capacity-test build web-install web-typecheck web-test web-build web-bundle-check web-e2e web-e2e-container web-performance e2e-cronjob-detail e2e-workload-lifecycle e2e-log-stream e2e-audit-loki lint arch api-check version-check playwright-check e2e-image-check evidence-check release-gate-check release-gate-schema-check release-baseline release-baseline-verify release-plan release-report image image-scan push helm-lint helm-check helm-template verify release-check clean

all: fmt vet test web-typecheck web-build

fmt:
	gofmt -w cmd internal web/embed.go

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

capacity-test:
	$(GO) test -tags=capacity ./internal/performance -run TestCapacityLatencySLO -count=1 -v

build: web-build
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X dosh/internal/version.Version=$(VERSION) -X dosh/internal/version.Commit=$(COMMIT) -X dosh/internal/version.Date=$(BUILD_DATE)" -o bin/dosh ./cmd/dosh

web-install:
	cd web && $(NPM) ci

web-typecheck:
	cd web && $(NPM) run typecheck

web-test:
	cd web && $(NPM) test

web-build:
	cd web && $(NPM) run build

web-bundle-check:
	cd web && $(NPM) run bundle:check

web-e2e:
	cd web && $(NPM) run e2e

web-e2e-container:
	cd web && $(NPM) run e2e:container

web-performance:
	cd web && $(NPM) run e2e:performance

lint: vet
	cd web && $(NPM) run typecheck

arch:
	@bash scripts/check-architecture.sh

api-check:
	cd web && $(NPM) run api:check

version-check:
	@bash scripts/check-version.sh

playwright-check:
	@bash scripts/check-playwright-isolation.sh

e2e-image-check:
	@bash scripts/check-e2e-image-binding.sh

e2e-cronjob-detail:
	@bash scripts/e2e-cronjob-detail.sh

e2e-workload-lifecycle:
	@DOSH_E2E_KEEP_ARTIFACTS=$${DOSH_E2E_KEEP_ARTIFACTS:-1} bash scripts/e2e-workload-lifecycle.sh

e2e-log-stream:
	@bash scripts/e2e-log-stream.sh

e2e-audit-loki:
	@DOSH_E2E_AUDIT_BACKEND=loki DOSH_E2E_KEEP_ARTIFACTS=1 bash scripts/e2e-audit-webhook.sh

evidence-check:
	@bash scripts/test-archive-evidence.sh

release-gate-check:
	@bash scripts/release-gate.sh validate --file "$(GATE_FILE)" --evidence-root /var/lib/dosh --binding-root .

release-gate-schema-check:
	@bash scripts/release-gate.sh validate --file "$(GATE_FILE)" --binding-root . --allow-blocked

release-baseline:
	@test -n "$(BASELINE_OUT)" || { echo "error: BASELINE_OUT is required" >&2; exit 2; }
	@bash scripts/release-baseline.sh capture --namespace "$(NAMESPACE)" --release "$(RELEASE)" --output "$(BASELINE_OUT)"

release-baseline-verify:
	@test -n "$(BASELINE_IN)" || { echo "error: BASELINE_IN is required" >&2; exit 2; }
	@bash scripts/release-baseline.sh verify --baseline "$(BASELINE_IN)"

release-plan:
	@test -n "$(PLAN_OUT)" -a -n "$(CHANGE_TICKET)" -a -n "$(WINDOW_START)" -a -n "$(WINDOW_END)" -a -n "$(BASELINE_IN)" || { echo "error: PLAN_OUT, CHANGE_TICKET, WINDOW_START, WINDOW_END and BASELINE_IN are required" >&2; exit 2; }
	@bash scripts/release-gate.sh plan --file "$(GATE_FILE)" --baseline "$(BASELINE_IN)" --ticket "$(CHANGE_TICKET)" --window-start "$(WINDOW_START)" --window-end "$(WINDOW_END)" --output "$(PLAN_OUT)" --namespace "$(NAMESPACE)" --release "$(RELEASE)" --evidence-root /var/lib/dosh --binding-root .

release-report:
	@test -n "$(REPORT_OUT)" || { echo "error: REPORT_OUT is required" >&2; exit 2; }
	@bash scripts/release-gate.sh report --file "$(GATE_FILE)" --output "$(REPORT_OUT)" --evidence-root /var/lib/dosh --binding-root . $(if $(GATE_NOW),--now "$(GATE_NOW)",) $(if $(filter 1 true yes,$(REPORT_ALLOW_BLOCKED)),--allow-blocked,)

image: build
	podman build -t $(IMAGE):$(TAG) .

image-scan:
	@trap 'rm -f "$(IMAGE_TAR)"' EXIT; \
	podman save --format docker-archive -o "$(IMAGE_TAR)" "$(IMAGE):$(TAG)"; \
	podman run --rm --network=host \
		-e GODEBUG="$(TRIVY_GODEBUG)" \
		-e TRIVY_DB_REPOSITORY=ghcr.io/aquasecurity/trivy-db:2 \
		-e TRIVY_JAVA_DB_REPOSITORY=ghcr.io/aquasecurity/trivy-java-db:1 \
		-v dosh-trivy-cache:/root/.cache/trivy \
		-v "$(IMAGE_TAR):$(IMAGE_TAR):ro" \
		$(TRIVY_IMAGE) image --input "$(IMAGE_TAR)" \
		--severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 --timeout "$(TRIVY_TIMEOUT)" \
		$(TRIVY_DB_FLAGS)

push:
	podman push $(IMAGE):$(TAG)

helm-lint:
	helm lint deploy/helm/dosh

helm-check: helm-lint
	bash scripts/check-helm.sh

helm-template: helm-check

verify: all arch api-check helm-check version-check playwright-check e2e-image-check release-gate-schema-check

release-check: verify race web-test capacity-test web-e2e-container web-performance web-bundle-check evidence-check
	$(MAKE) image TAG="$(TAG)"
	$(MAKE) image-scan TAG="$(TAG)"

clean:
	rm -rf bin web/dist
