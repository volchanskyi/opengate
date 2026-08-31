# scripts/lib/mutation-shards.sh is a Bash library (arrays, [[ ]]), and the
# mutation targets source it. Make defaults to /bin/sh, which is dash on Debian
# and Ubuntu, so name the interpreter rather than depending on the host's
# /bin/sh being a Bash.
SHELL := /bin/bash

.PHONY: build test test-short test-integration test-coverage lint lint-deploy fmt verify-codegen golden ci clean e2e agent-binary load-test load-test-quic sonar sonar-coverage sonar-quick \
	mutate mutate-rust mutate-go mutate-web fuzz-rust taint-go taint-web pentest-review dead-code \
	terraform-test terraform-drift \
	secrets-scan iac-policy iac-policy-fix iac-policy-custom lint-dockerfile lint-k8s \
	test-parse-tfplan shell-check shell-fmt shell-test shell-quality \
	tunnel ssh

build:
	cd agent && cargo build --workspace
	cd server && go build ./...
	cd web && npm run build

test: test-rust test-go test-web

test-short:
	cd agent && cargo test --workspace
	cd server && go test -short ./...
	cd web && npx vitest run

test-rust:
	cd agent && cargo test --workspace

# One Postgres and one VictoriaMetrics back the whole run — see scripts/test-go.sh
# for why the per-binary fallback would otherwise start a dozen-plus containers.
test-go:
	./scripts/test-go.sh

test-web:
	cd web && npx vitest run

test-integration:
	cd server && go test -race -timeout 5m ./tests/integration/

test-coverage:
	cd server && go test -race -coverprofile=coverage.out -covermode=atomic ./... && go tool cover -func=coverage.out

# Local Postgres for running the Go test suite. testutil.NewTestStore
# creates one schema per test for parallel-safe isolation; with `go test`
# at default parallelism the working set of transient connections exceeds
# the Postgres 100-conn default, hence `-c max_connections=400`.
#
# The lock table is sized once at startup as max_locks_per_transaction ×
# max_connections, so raising the connection ceiling without raising the
# per-transaction one leaves each migration — which creates the whole schema,
# every table and index, in a single transaction — competing for the default
# 64. Enough of them in flight together exhausts the table and the migration
# fails with "out of shared memory" rather than anything about the schema.
# Mirrors the ci.yml / mutation.yml setup so CI and local behave the same.
postgres-test-up:
	docker rm -f opengate-pg-test 2>/dev/null || true
	docker run -d --rm --name opengate-pg-test \
		-e POSTGRES_USER=opengate -e POSTGRES_PASSWORD=opengate -e POSTGRES_DB=opengate_test \
		-p 5432:5432 postgres:17-alpine -c max_connections=400 -c max_locks_per_transaction=256
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
		docker exec opengate-pg-test pg_isready -U opengate -d opengate_test >/dev/null 2>&1 && break; \
		sleep 1; \
	done
	@echo "Postgres test DB ready. Export:"
	@echo "  export POSTGRES_TEST_URL=\"postgres://opengate:opengate@localhost:5432/opengate_test?sslmode=disable\""

postgres-test-down:
	docker rm -f opengate-pg-test 2>/dev/null || true

# One VictoriaMetrics for the whole Go run. testvm memoizes per test BINARY and
# `go test ./tests/...` builds one per package, so with the URL unset each
# VictoriaMetrics-touching package starts its own store — several of them holding
# a fleet's worth of series at once is enough memory pressure for the kernel to
# kill one mid-run. Image pin matches testvm's; scripts/tests/victoriametrics-prereq.test.sh
# fails the gauntlet if the two drift.
victoriametrics-test-up:
	docker rm -f opengate-vm-test 2>/dev/null || true
	docker run -d --rm --name opengate-vm-test \
		-p 8428:8428 victoriametrics/victoria-metrics:v1.114.0
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
		curl -fsS --max-time 2 http://127.0.0.1:8428/health >/dev/null 2>&1 && break; \
		sleep 1; \
	done
	@echo "VictoriaMetrics test store ready. Export:"
	@echo "  export VICTORIAMETRICS_TEST_URL=\"http://127.0.0.1:8428\""

victoriametrics-test-down:
	docker rm -f opengate-vm-test 2>/dev/null || true

lint: lint-deploy pentest-review
	cd agent && cargo clippy --workspace -- -D warnings
	cd server && go vet ./...
	cd web && npx eslint src/
	actionlint

shell-check:
	@command -v shellcheck >/dev/null 2>&1 || { echo "ERROR: shellcheck not found. Run scripts/install-shell-tools.sh"; exit 1; }
	@command -v shfmt >/dev/null 2>&1 || { echo "ERROR: shfmt not found. Run scripts/install-shell-tools.sh"; exit 1; }
	scripts/shell-quality.sh check

shell-fmt:
	@command -v shfmt >/dev/null 2>&1 || { echo "ERROR: shfmt not found. Run scripts/install-shell-tools.sh"; exit 1; }
	scripts/shell-quality.sh format

shell-test:
	scripts/shell-quality.sh test

shell-quality: shell-check shell-test

lint-deploy:
	@command -v yamllint >/dev/null 2>&1 || { echo "ERROR: yamllint not found. Install with: pip install yamllint"; exit 1; }
	yamllint -c .yamllint.yml deploy/
	@$(MAKE) secrets-scan
	@$(MAKE) lint-dockerfile
	@$(MAKE) iac-policy
	@$(MAKE) iac-policy-custom
	@$(MAKE) lint-k8s
	@$(MAKE) test-parse-tfplan
	terraform -chdir=deploy/terraform fmt -check -recursive
	terraform -chdir=deploy/terraform init -backend=false -input=false >/dev/null 2>&1
	terraform -chdir=deploy/terraform validate
	@command -v tflint >/dev/null 2>&1 || { echo "ERROR: tflint not found. Install from: https://github.com/terraform-linters/tflint"; exit 1; }
	tflint --init --chdir=deploy/terraform && tflint --chdir=deploy/terraform --format=compact
	@$(MAKE) terraform-test
	cd deploy && docker compose -f docker-compose.test.yml config --quiet
	@command -v trivy >/dev/null 2>&1 || { echo "ERROR: trivy not found. Install from: https://aquasecurity.github.io/trivy"; exit 1; }
	trivy config --severity HIGH,CRITICAL --exit-code 1 deploy/ \
	  && trivy config --severity HIGH,CRITICAL --exit-code 1 Dockerfile

# Module-invariant assertions for the Terraform config (mock_provider, no OCI creds).
# Each submodule and the root carry their own tftest suite; the umbrella target runs
# all of them. Requires terraform >= 1.7 for `expect_failures` against variable validation.
terraform-test:
	terraform -chdir=deploy/terraform/modules/networking init -backend=false -input=false >/dev/null
	terraform -chdir=deploy/terraform/modules/networking test
	terraform -chdir=deploy/terraform/modules/bastion init -backend=false -input=false >/dev/null
	terraform -chdir=deploy/terraform/modules/bastion test
	terraform -chdir=deploy/terraform/modules/oke init -backend=false -input=false >/dev/null
	terraform -chdir=deploy/terraform/modules/oke test
	terraform -chdir=deploy/terraform/modules/backups init -backend=false -input=false >/dev/null
	terraform -chdir=deploy/terraform/modules/backups test
	terraform -chdir=deploy/terraform init -backend=false -input=false >/dev/null
	terraform -chdir=deploy/terraform test

# Local mirror of the .github/workflows/terraform-drift.yml plan step.
# Uses the operator's local OCI creds (terraform.tfvars + ~/.oci/terraform-credentials)
# and prints the refresh-only diff. Exit 2 = drift detected; exit 0 = clean.
terraform-drift:
	terraform -chdir=deploy/terraform plan -refresh-only -detailed-exitcode

# ----------------------------------------------------------------------------
# Operator access via OCI Bastion (replaces the static ssh_allowed_cidr rule).
# Both targets shell into deploy/scripts/bastion-session.sh, which caches the
# active session OCID + expiry at ~/.cache/opengate/bastion-session.json so
# subsequent invocations within the 3 h TTL skip the 5–10 s session create.
# Prerequisites and IAM setup live in docs/infrastructure/OCI-Terraform.md → "Operator
# access via OCI Bastion".
# ----------------------------------------------------------------------------

# `make tunnel` — port-forward the in-cluster Grafana :3000
# to localhost via kubectl. Post-OKE-cutover the monitoring UIs are ClusterIP
# services (not host ports on the decommissioned VM), so the bastion can no
# longer reach them. Operator browses to http://localhost:3000; Ctrl-C tears the
# forward down.
tunnel:
	@echo "Grafana -> http://localhost:3000 (Ctrl-C to stop)"
	@kubectl -n monitoring port-forward svc/monitoring-grafana 3000:3000

# `make ssh` — Managed SSH session to the OKE worker node (interactive shell).
ssh:
	@deploy/scripts/bastion-session.sh ssh

# TDD harness for deploy/scripts/parse-tfplan.sh (S4 of the IaC pyramid).
# Three canned tfplan fixtures cover the gate decision matrix.
test-parse-tfplan:
	@deploy/scripts/parse-tfplan.sh deploy/tests/fixtures/tfplan/no-changes.json >/dev/null \
	  && echo "  ok: no-changes → exit 0"
	@deploy/scripts/parse-tfplan.sh deploy/tests/fixtures/tfplan/safe-add.json  >/dev/null \
	  && echo "  ok: safe-add → exit 0"
	@! deploy/scripts/parse-tfplan.sh deploy/tests/fixtures/tfplan/destroy-protected.json >/dev/null 2>&1 \
	  && echo "  ok: destroy-protected (no override) → exit 1"
	@deploy/scripts/parse-tfplan.sh deploy/tests/fixtures/tfplan/destroy-protected.json --approve-destroy >/dev/null \
	  && echo "  ok: destroy-protected (--approve-destroy) → exit 0"
	@echo "test-parse-tfplan: PASS"

# ----------------------------------------------------------------------------
# IaC Security Testing Pyramid (S1–S4 of iac-security-testing-pyramid plan).
# ----------------------------------------------------------------------------

# L2 — Secrets scanning. Default mode scans git history; --no-git would scan
# the working tree but pulls in gitignored build artifacts. Pre-commit-side
# `gitleaks protect --staged` lives in the /precommit skill.
secrets-scan:
	@command -v gitleaks >/dev/null 2>&1 || { echo "ERROR: gitleaks not found. Install: https://github.com/gitleaks/gitleaks/releases"; exit 1; }
	gitleaks detect --config .gitleaks.toml --no-banner --redact

# L4 — Built-in policy scanning (Checkov, 4 frameworks; secrets framework is
# disabled because gitleaks already owns that surface — see .checkov.yaml).
iac-policy:
	@command -v checkov >/dev/null 2>&1 || { echo "ERROR: checkov not found. Install: pipx install checkov"; exit 1; }
	@# The `helm` framework renders deploy/helm/** charts before scanning.
	@command -v helm >/dev/null 2>&1 || { echo "ERROR: helm not found (required by Checkov's helm framework). Install from: https://helm.sh/docs/intro/install/"; exit 1; }
	checkov --config-file .checkov.yaml

# Triage helper: same surface, --soft-fail so the operator can review findings
# without the gate going red. Use during baseline refresh / new-framework rollout.
iac-policy-fix:
	checkov --config-file .checkov.yaml --soft-fail

# L4 — Dockerfile policy. Hadolint is orthogonal to Checkov's Docker rule set
# (catches BIDI smuggling, layer ordering, pin-missing). Kept as a separate
# tool so each can be invoked / silenced independently.
lint-dockerfile:
	@command -v hadolint >/dev/null 2>&1 || { echo "ERROR: hadolint not found. Install: https://github.com/hadolint/hadolint/releases"; exit 1; }
	hadolint Dockerfile

# L5 — Project-specific Rego policies (Conftest). Reads JSON-converted plan
# files for terraform, raw YAML for compose + workflows. The terraform target
# requires a plan-file (generated by `terraform plan -out=`), which in turn
# needs the remote backend init (operator-only path); the compose/actions
# checks run against committed files directly so they always work in CI.
iac-policy-custom:
	@command -v conftest >/dev/null 2>&1 || { echo "ERROR: conftest not found. Install: https://github.com/open-policy-agent/conftest/releases"; exit 1; }
	conftest test --policy policy/github_actions .github/workflows/*.yml
	@# Terraform policy needs a plan-file (HCL2 parser leaves ${var.X} unresolved).
	@# Operator: terraform plan -out=/tmp/tfplan.binary && terraform show -json /tmp/tfplan.binary > /tmp/tfplan.json
	@if [ -f /tmp/tfplan.json ]; then \
	  conftest test --policy policy/terraform /tmp/tfplan.json; \
	else \
	  echo "(skipping terraform Rego check: /tmp/tfplan.json not present)"; \
	fi

# Helm chart validation (Phase 13b PR-B). helm lint → schema-validate the
# rendered manifests (kubeconform, ignoring CRDs) → unit-test the k8s Rego
# policy → run it against every overlay's rendered output. Checkov's helm
# framework runs separately via `make iac-policy`.
lint-k8s:
	@command -v helm >/dev/null 2>&1 || { echo "ERROR: helm not found. Install from: https://helm.sh/docs/intro/install/"; exit 1; }
	@command -v kubeconform >/dev/null 2>&1 || { echo "ERROR: kubeconform not found. Install from: https://github.com/yannh/kubeconform/releases"; exit 1; }
	@command -v conftest >/dev/null 2>&1 || { echo "ERROR: conftest not found. Install: https://github.com/open-policy-agent/conftest/releases"; exit 1; }
	helm lint deploy/helm/opengate -f deploy/helm/opengate/ci/test-values.yaml
	conftest verify --policy policy/k8s
	@for vals in ci/test-values values-staging values-production; do \
	  echo "==> opengate ($$vals)"; \
	  helm template og deploy/helm/opengate -f deploy/helm/opengate/$$vals.yaml > /tmp/og-k8s-render.yaml; \
	  kubeconform -strict -ignore-missing-schemas -summary /tmp/og-k8s-render.yaml; \
	  conftest test -p policy/k8s /tmp/og-k8s-render.yaml; \
	done
	helm lint deploy/helm/monitoring
	@echo "==> opengate-monitoring"
	helm template mon deploy/helm/monitoring > /tmp/mon-k8s-render.yaml
	kubeconform -strict -ignore-missing-schemas -summary /tmp/mon-k8s-render.yaml
	conftest test -p policy/k8s /tmp/mon-k8s-render.yaml

fmt:
	cd agent && cargo fmt --all
	cd server && gofmt -w .
	cd web && npx prettier --write src/

verify-codegen:
	@command -v oapi-codegen >/dev/null 2>&1 || { echo "ERROR: oapi-codegen not found in PATH. Install with: go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.6.0"; exit 1; }
	cd server && oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml > internal/api/openapi_gen.go && git diff --exit-code internal/api/openapi_gen.go
	# The web client generates from the same spec. Without this the TypeScript
	# types drift silently until the Docker web build type-checks them, which is
	# ten minutes into the gauntlet rather than one second in.
	cd web && npm run --silent generate:api && git diff --exit-code src/types/api.d.ts

golden:
	# Forward goldens: Rust encodes, Go verifies. Generates testdata/golden/*.bin.
	cd agent && GENERATE_GOLDEN=1 cargo test -p mesh-protocol --test golden_test
	# Reverse goldens: Go encodes go_*.bin, Rust verifies (in the reverse_golden_test below).
	# Also writes .meta.json sidecars for every .bin in testdata/golden/.
	cd server && GENERATE_GOLDEN=1 go test ./internal/protocol/ -run "TestGenerateReverseGoldens|TestGenerateForwardSidecars"
	# Forward verification (Go side): decode and assert all Rust-side fixtures.
	cd server && go test ./internal/protocol/ -run TestGolden
	# Reverse verification (Rust side): decode and assert all Go-encoded fixtures.
	cd agent && cargo test -p mesh-protocol --test reverse_golden_test

ci: lint test build

# DOCKER_CONFIG is sanitized by docker-credstore-guard.sh so a broken local
# credential helper (e.g. WSL docker-credential-desktop.exe) cannot break pulls
# of public base images. No-op in CI (no broken credsStore there).
# Sole owner of the Compose lifecycle: bring the stack up, run Playwright, then
# tear down in the same recipe line so the teardown also runs when the suite
# fails, while the suite's exit code still propagates to make.
# Smoke tests run AFTER Playwright, not before: registering the first user on a
# fresh database auto-promotes it to administrator, and Playwright's global
# setup asserts that its own bootstrap account got that promotion. A smoke run
# that went first would take the slot and fail every E2E run. Both share one
# compose lifecycle, and either failing fails the target.
# The agent as the browser test stack runs it: one static binary, staged where
# the Docker build context can reach it. The cargo target tree itself is
# excluded from the context (it is tens of gigabytes), so the binary is copied
# out rather than referenced in place.
agent-binary:
	cd agent && cargo build --release --target x86_64-unknown-linux-musl -p mesh-agent
	mkdir -p deploy/agent-bin
	cp agent/target/x86_64-unknown-linux-musl/release/mesh-agent deploy/agent-bin/mesh-agent

e2e: agent-binary
	bash deploy/scripts/e2e-stack-up.sh
	@(cd web && npx playwright test); rc=$$?; \
		bash deploy/scripts/smoke-test.sh --host 127.0.0.1 --port 8080 --mode local --scheme http || rc=1; \
		cd deploy && DOCKER_CONFIG="$$(../scripts/docker-credstore-guard.sh)" docker compose -f docker-compose.test.yml down -v; \
		exit $$rc

# Every scenario CI runs, so a local pass and a nightly pass mean the same
# thing. A target that omits one leaves that scenario's thresholds discovered
# only by the nightly.
# LOADTEST_RUN_ID names every identity a run creates, so a local run is
# removable by the same cleanup the nightly uses. It defaults to the clock here
# because a workstation has no run number of its own.
load-test:
	LOADTEST_BASE_URL=http://localhost:8080 \
	LOADTEST_RUN_ID=$${LOADTEST_RUN_ID:-local-$$(date -u +%Y%m%d%H%M%S)} \
	  sh -c 'for s in api-baseline concurrent-agents relay-throughput; do \
	    scripts/loadtest-k6-run.sh $$s load/k6/scenarios/$$s.js || exit $$?; \
	  done'

load-test-quic:
	cd server && go run ./tests/loadtest/ -agents=100 -addr=127.0.0.1:9090

# The Rust ignore regex carves out code with no in-process harness:
#   main.rs        — binary entry points: process/runtime wiring, covered by E2E
#   webrtc.rs      — RTCPeerConnection/ICE callbacks, need a live STUN stack
#   terminal.rs    — PTY + shell subprocess owned by the host
#   session/relay.rs — capture/writer loops driven by a live screen source
#   /tests/        — the test files themselves
sonar-coverage:
	cd server && go test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...
	cd agent && cargo llvm-cov nextest --workspace --lcov --output-path lcov.info \
		--ignore-filename-regex '(/tests/)'
	./scripts/rust-lcov-relativize.sh agent/lcov.info "$$(pwd)"
	cd web && npx vitest run --coverage

sonar: sonar-coverage
	@test -n "$$SONAR_TOKEN" || { echo "ERROR: SONAR_TOKEN not set. Export it or add to .env"; exit 1; }
	@for attempt in 1 2 3; do \
		out=$$(docker run --rm \
			-e SONAR_TOKEN="$$SONAR_TOKEN" \
			-v "$$(pwd):/usr/src" \
			-w /usr/src \
			sonarsource/sonar-scanner-cli:latest \
			-Dsonar.qualitygate.wait=true \
			-Dsonar.scanner.skipJreProvisioning=true \
			-Dsonar.branch.name=dev 2>&1); rc=$$?; \
		printf '%s\n' "$$out"; \
		[ $$rc -eq 0 ] && exit 0; \
		if printf '%s' "$$out" | grep -q 'QUALITY GATE STATUS: FAILED'; then \
			echo "::error::sonar quality gate FAILED — fix coverage/issues (not transient, not retrying)"; exit 1; \
		fi; \
		if [ "$$attempt" -eq 3 ]; then echo "::error::sonar scan failed after 3 attempts (transient infra error)"; exit 1; fi; \
		echo "::warning::sonar scan attempt $$attempt failed (transient — e.g. SonarCloud plugin-CDN EOFException); retrying in $$((attempt * 15))s"; \
		sleep $$((attempt * 15)); \
	done

sonar-quick:
	@test -n "$$SONAR_TOKEN" || { echo "ERROR: SONAR_TOKEN not set. Export it or add to .env"; exit 1; }
	docker run --rm \
		-e SONAR_TOKEN="$$SONAR_TOKEN" \
		-v "$$(pwd):/usr/src" \
		-w /usr/src \
		sonarsource/sonar-scanner-cli:latest \
		-Dsonar.qualitygate.wait=true \
		-Dsonar.scanner.skipJreProvisioning=true \
		-Dsonar.branch.name=dev

clean:
	cd agent && cargo clean
	cd server && rm -rf bin/
	cd web && rm -rf dist/ node_modules/.cache

# ----------------------------------------------------------------------------
# Structural-testing tooling (developer-facing; CI gates land in PR 9).
# ----------------------------------------------------------------------------

# Mutation testing — surfaces test-suite quality (surviving mutants = test gaps).
mutate: mutate-rust mutate-go mutate-web

mutate-rust:
	@command -v cargo-mutants >/dev/null 2>&1 || { echo "ERROR: cargo-mutants not found. Install with: cargo install cargo-mutants"; exit 1; }
	@# Run the same scope shards as CI (scripts/lib/mutation-shards.sh): each
	@# shard mutates one package restricted to its files, then merge into one
	@# report — mirrors .github/workflows/mutation.yml.
	. scripts/lib/mutation-shards.sh; \
	outcomes=""; \
	for shard in $$(mutation_rust_shards); do \
	  mapfile -t shard_args < <(mutation_rust_shard_args $$shard); \
	  echo ">> mutating shard $$shard ($${shard_args[*]})"; \
	  ( cd agent && OPENGATE_GOLDEN_DIR=$(CURDIR)/testdata/golden \
	    cargo mutants "$${shard_args[@]}" --no-shuffle --output "mutants-$$shard" ) || true; \
	  outcomes="$$outcomes agent/mutants-$$shard/mutants.out/outcomes.json"; \
	done; \
	mkdir -p agent/mutants.out; \
	./scripts/mutation-merge-rust.sh agent/mutants.out/outcomes.json $$outcomes; \
	echo ">> merged Rust mutation report: agent/mutants.out/outcomes.json"

mutate-go:
	@command -v gremlins >/dev/null 2>&1 || { echo "ERROR: gremlins not found. Install with: go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0"; exit 1; }
	@if [ -z "$$POSTGRES_TEST_URL" ]; then \
	  echo "WARNING: POSTGRES_TEST_URL not set; api/db tests will skip and many mutants will be NOT COVERED."; \
	  echo "         Start a test Postgres (see .github/workflows/ci.yml) and set:"; \
	  echo "         export POSTGRES_TEST_URL=\"postgres://opengate:opengate@localhost:5432/opengate_test?sslmode=disable\""; \
	fi
	@# Run the same mutation-unit shards as CI (scripts/lib/mutation-shards.sh):
	@# each shard mutates the whole module restricted to its files/dirs via -E, then
	@# merge into one report — mirrors .github/workflows/mutation.yml.
	. scripts/lib/mutation-shards.sh; \
	reports=""; \
	for shard in $$(mutation_go_shards); do \
	  excl="$$(mutation_go_shard_exclude_regex $$shard)"; \
	  coef="$$(mutation_go_shard_timeout_coefficient $$shard)"; \
	  coef_flag=""; [ -n "$$coef" ] && coef_flag="--timeout-coefficient $$coef"; \
	  echo ">> mutating shard $$shard (exclude: $$excl) (coef: $${coef:-baseline})"; \
	  ( cd server && gremlins unleash . -E "$$excl" $$coef_flag --output "mutation-report-$$shard.json" ) || true; \
	  reports="$$reports server/mutation-report-$$shard.json"; \
	done; \
	./scripts/mutation-merge-go.sh server/mutation-report.json $$reports; \
	echo ">> merged Go mutation report: server/mutation-report.json"

mutate-web:
	cd web && npx stryker run

# Coverage-guided fuzzing — libFuzzer over mesh-protocol's wire decoder.
# Bounded (FUZZ_RUNS iterations) so it terminates; libFuzzer needs nightly.
# The always-run regression is the stable corpus replay in
# crates/mesh-protocol/tests/decode_corpus_test.rs (runs in plain `cargo test`).
FUZZ_RUNS ?= 100000
# cargo-fuzz defaults its build --target to the triple cargo-fuzz itself was
# compiled for. CI installs the musl prebuilt (via cargo-binstall), which would
# make it build the fuzz target for x86_64-unknown-linux-musl — whose std is not
# installed — and fail with E0463. Pin the build to the running host's triple so
# the prebuilt std is always present, regardless of how cargo-fuzz was installed.
FUZZ_TARGET ?= $(shell rustc -vV | sed -n 's/^host: //p')
fuzz-rust:
	@command -v cargo-fuzz >/dev/null 2>&1 || { echo "ERROR: cargo-fuzz not found. Install with: cargo install cargo-fuzz"; exit 1; }
	@rustup toolchain list | grep -q '^nightly' || { echo "ERROR: nightly toolchain not found. Install with: rustup toolchain install nightly"; exit 1; }
	cd agent/fuzz && cargo +nightly fuzz run --target $(FUZZ_TARGET) decode -- -runs=$(FUZZ_RUNS)

# Static taint linting — catches data-flow paths from sources to sinks.
taint-go:
	@command -v gosec >/dev/null 2>&1 || { echo "ERROR: gosec not found. Install with: go install github.com/securego/gosec/v2/cmd/gosec@latest"; exit 1; }
	cd server && gosec -conf .gosec.json ./...

taint-web:
	cd web && npx eslint --config eslint.security.config.js src/

# Adversarial pen-test gate (ADR-027): custom Semgrep rules + OpenAPI
# spec-drift over the diff. Full scan by default (no baseline → every finding
# evaluated); the gauntlet and CI pass PENTEST_BASELINE_REF for diff-only mode.
pentest-review:
	@command -v semgrep >/dev/null 2>&1 || [ -x "$$HOME/.local/bin/semgrep" ] || { echo "ERROR: semgrep not found. Install with: bash scripts/install-semgrep.sh"; exit 1; }
	bash scripts/pentest-review.sh

# Dead-code & unused-symbol sweep across all three languages.
dead-code:
	@command -v staticcheck >/dev/null 2>&1 || { echo "ERROR: staticcheck not found. Install with: go install honnef.co/go/tools/cmd/staticcheck@latest"; exit 1; }
	cd agent && cargo clippy --workspace --all-targets -- -W dead_code
	cd server && staticcheck -checks U1000 ./...
	cd web && npx ts-prune -p tsconfig.app.json -i 'src/types/api\.d\.ts'
