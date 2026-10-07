# Recipes run under Bash because the mutation targets source a Bash-only library.
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

test-go:
	./scripts/test-go.sh

test-web:
	cd web && npx vitest run

test-integration:
	cd server && go test -race -timeout 5m ./tests/integration/

test-coverage:
	cd server && go test -race -coverprofile=coverage.out -covermode=atomic ./... && go tool cover -func=coverage.out

# Raised max_locks_per_transaction fits migrations that create the whole schema in one
# transaction; --shm-size holds the 32 MiB parallel-query segments Postgres puts in /dev/shm.
postgres-test-up:
	docker rm -f opengate-pg-test 2>/dev/null || true
	docker run -d --rm --name opengate-pg-test \
		-e POSTGRES_USER=opengate -e POSTGRES_PASSWORD=opengate -e POSTGRES_DB=opengate_test \
		--shm-size=1g -p 5432:5432 postgres:17-alpine -c max_connections=400 -c max_locks_per_transaction=256
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
		docker exec opengate-pg-test pg_isready -U opengate -d opengate_test >/dev/null 2>&1 && break; \
		sleep 1; \
	done
	@echo "Postgres test DB ready. Export:"
	@echo "  export POSTGRES_TEST_URL=\"postgres://opengate:opengate@localhost:5432/opengate_test?sslmode=disable\""

postgres-test-down:
	docker rm -f opengate-pg-test 2>/dev/null || true

# One VictoriaMetrics serves every package of the Go run; the image pin matches testvm's.
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
	@bash scripts/require-tool.sh shellcheck
	@bash scripts/require-tool.sh shfmt
	scripts/shell-quality.sh check

shell-fmt:
	@bash scripts/require-tool.sh shfmt
	scripts/shell-quality.sh format

shell-test:
	scripts/shell-quality.sh test

shell-quality: shell-check shell-test

lint-deploy:
	@bash scripts/require-tool.sh yamllint
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
	@bash scripts/require-tool.sh tflint
	tflint --init --chdir=deploy/terraform && tflint --chdir=deploy/terraform --format=compact
	@$(MAKE) terraform-test
	cd deploy && docker compose -f docker-compose.test.yml config --quiet
	@bash scripts/require-tool.sh trivy
	trivy config --severity HIGH,CRITICAL --exit-code 1 deploy/ \
	  && trivy config --severity HIGH,CRITICAL --exit-code 1 Dockerfile

# Runs each module's and the root's tftest suite against mock providers; needs terraform >= 1.7.
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

# Prints the refresh-only plan diff with local OCI credentials; exit 2 means drift.
terraform-drift:
	terraform -chdir=deploy/terraform plan -refresh-only -detailed-exitcode

# Port-forwards the in-cluster Grafana to http://localhost:3000 until Ctrl-C.
tunnel:
	@echo "Grafana -> http://localhost:3000 (Ctrl-C to stop)"
	@kubectl -n monitoring port-forward svc/monitoring-grafana 3000:3000

ssh:
	@deploy/scripts/bastion-session.sh ssh

# Runs parse-tfplan.sh over canned plan fixtures that cover each gate decision.
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

# Scans git history; --no-git would also read gitignored build artifacts.
secrets-scan:
	@bash scripts/require-tool.sh gitleaks
	gitleaks detect --config .gitleaks.toml --no-banner --redact

# Checkov scans only the frameworks .checkov.yaml lists; gitleaks covers secrets.
iac-policy:
	@bash scripts/require-tool.sh checkov
	@# The `helm` framework renders deploy/helm/** charts before scanning.
	@bash scripts/require-tool.sh helm
	checkov --config-file .checkov.yaml

# Runs the same scan with --soft-fail so findings do not fail the target.
iac-policy-fix:
	checkov --config-file .checkov.yaml --soft-fail

# Hadolint checks Dockerfile rules Checkov lacks, such as BIDI smuggling and layer ordering.
lint-dockerfile:
	@bash scripts/require-tool.sh hadolint
	hadolint Dockerfile

# Conftest runs the Rego policies over workflow YAML and, when present, a terraform plan JSON.
iac-policy-custom:
	@bash scripts/require-tool.sh conftest
	conftest test --policy policy/github_actions .github/workflows/*.yml
	@# The terraform policy reads a plan JSON because the HCL2 parser leaves ${var.X} unresolved.
	@if [ -f /tmp/tfplan.json ]; then \
	  conftest test --policy policy/terraform /tmp/tfplan.json; \
	else \
	  echo "(skipping terraform Rego check: /tmp/tfplan.json not present)"; \
	fi

# Lints the charts, schema-validates their rendered manifests and runs the k8s Rego policy.
lint-k8s:
	@bash scripts/require-tool.sh helm
	@bash scripts/require-tool.sh kubeconform
	@bash scripts/require-tool.sh conftest
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
	@bash scripts/require-tool.sh oapi-codegen
	cd server && oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml > internal/api/openapi_gen.go && git diff --exit-code internal/api/openapi_gen.go
	cd web && npm run --silent generate:api && git diff --exit-code src/types/api.d.ts

golden:
	cd agent && GENERATE_GOLDEN=1 cargo test -p mesh-protocol --test golden_test
	# Go encodes go_*.bin and writes .meta.json sidecars for every .bin in testdata/golden/.
	cd server && GENERATE_GOLDEN=1 go test ./internal/protocol/ -run "TestGenerateReverseGoldens|TestGenerateForwardSidecars"
	cd server && go test ./internal/protocol/ -run TestGolden
	cd agent && cargo test -p mesh-protocol --test reverse_golden_test

ci: lint test build

# Copies one static agent binary into the Docker build context, which excludes the cargo target.
agent-binary:
	cd agent && cargo build --release --target x86_64-unknown-linux-musl -p mesh-agent
	mkdir -p deploy/agent-bin
	cp agent/target/x86_64-unknown-linux-musl/release/mesh-agent deploy/agent-bin/mesh-agent

# Smoke tests run after Playwright: the first user on a fresh database becomes administrator,
# and Playwright's global setup claims that account.
e2e: agent-binary
	bash deploy/scripts/e2e-stack-up.sh
	@(cd web && npx playwright test); rc=$$?; \
		bash deploy/scripts/smoke-test.sh --host 127.0.0.1 --port 8080 --metrics-port 8081 --mode local --scheme http || rc=1; \
		cd deploy && DOCKER_CONFIG="$$(../scripts/docker-credstore-guard.sh)" docker compose -f docker-compose.test.yml down -v; \
		exit $$rc

# Runs every scenario CI runs. LOADTEST_RUN_ID defaults to the clock so the nightly's
# cleanup can remove a local run's identities.
load-test:
	LOADTEST_BASE_URL=http://localhost:8080 \
	LOADTEST_RUN_ID=$${LOADTEST_RUN_ID:-local-$$(date -u +%Y%m%d%H%M%S)} \
	  sh -c 'for s in api-baseline concurrent-agents relay-throughput; do \
	    scripts/loadtest-k6-run.sh $$s load/k6/scenarios/$$s.js || exit $$?; \
	  done'

load-test-quic:
	cd server && go run ./tests/loadtest/ -agents=100 -addr=127.0.0.1:9090

sonar-coverage:
	cd server && go test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...
	cd agent && cargo llvm-cov nextest --workspace --lcov --output-path lcov.info \
		--ignore-filename-regex '(/tests/)'
	./scripts/rust-lcov-relativize.sh agent/lcov.info "$$(pwd)"
	cd web && npx vitest run --coverage

sonar: sonar-coverage
	@test -n "$$SONAR_TOKEN" || { echo "ERROR: SONAR_TOKEN not set. Export it or add to .env"; exit 1; }
	@./scripts/sonar-scan.sh

sonar-quick:
	@test -n "$$SONAR_TOKEN" || { echo "ERROR: SONAR_TOKEN not set. Export it or add to .env"; exit 1; }
	. scripts/lib/tool-versions.sh; docker run --rm \
		-e SONAR_TOKEN="$$SONAR_TOKEN" \
		-v "$$(pwd):/usr/src" \
		-w /usr/src \
		sonarsource/sonar-scanner-cli:"$$TOOL_VERSION_SONAR_SCANNER_IMAGE" \
		-Dsonar.qualitygate.wait=true \
		-Dsonar.scanner.skipJreProvisioning=true \
		-Dsonar.branch.name=dev

clean:
	cd agent && cargo clean
	cd server && rm -rf bin/
	cd web && rm -rf dist/ node_modules/.cache

mutate: mutate-rust mutate-go mutate-web

mutate-rust:
	@bash scripts/require-tool.sh cargo-mutants
	@# Mutates the CI scope shards from mutation-shards.sh, then merges them into one report.
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
	@bash scripts/require-tool.sh gremlins
	@if [ -z "$$POSTGRES_TEST_URL" ]; then \
	  echo "WARNING: POSTGRES_TEST_URL not set; api/db tests will skip and many mutants will be NOT COVERED."; \
	  echo "         Start a test Postgres (see .github/workflows/ci.yml) and set:"; \
	  echo "         export POSTGRES_TEST_URL=\"postgres://opengate:opengate@localhost:5432/opengate_test?sslmode=disable\""; \
	fi
	@# Each CI shard from mutation-shards.sh walks the narrowest path holding its units.
	. scripts/lib/mutation-shards.sh; \
	reports=""; \
	for shard in $$(mutation_go_shards); do \
	  scan="$$(mutation_go_shard_scan_path $$shard)"; \
	  excl="$$(mutation_go_shard_exclude_regex $$shard)"; \
	  coef="$$(mutation_go_shard_timeout_coefficient $$shard)"; \
	  coef_flag=""; [ -n "$$coef" ] && coef_flag="--timeout-coefficient $$coef"; \
	  echo ">> mutating shard $$shard (walks: $$scan) (exclude: $$excl) (coef: $${coef:-baseline})"; \
	  ( cd server && gremlins unleash "$$scan" -E "$$excl" $$coef_flag --output "mutation-report-$$shard.json" ) || true; \
	  reports="$$reports server/mutation-report-$$shard.json"; \
	done; \
	./scripts/mutation-merge-go.sh server/mutation-report.json $$reports; \
	echo ">> merged Go mutation report: server/mutation-report.json"

mutate-web:
	cd web && npx stryker run

# libFuzzer over the wire decoder, bounded to FUZZ_RUNS iterations; needs the nightly toolchain.
FUZZ_RUNS ?= 100000
# The build target is the host triple; cargo-fuzz otherwise targets the triple it was built for.
FUZZ_TARGET ?= $(shell rustc -vV | sed -n 's/^host: //p')
fuzz-rust:
	@bash scripts/require-tool.sh cargo-fuzz
	@rustup toolchain list | grep -q '^nightly' || { echo "ERROR: nightly toolchain not found. Install with: rustup toolchain install nightly"; exit 1; }
	cd agent/fuzz && cargo +nightly fuzz run --target $(FUZZ_TARGET) decode -- -runs=$(FUZZ_RUNS)

taint-go:
	@bash scripts/require-tool.sh gosec
	cd server && gosec -conf .gosec.json ./...

taint-web:
	cd web && npx eslint --config eslint.security.config.js src/

# Scans fully by default; PENTEST_BASELINE_REF switches it to diff-only mode.
pentest-review:
	@command -v semgrep >/dev/null 2>&1 || [ -x "$$HOME/.local/bin/semgrep" ] || { echo "ERROR: semgrep not found. Install with: bash scripts/install-semgrep.sh"; exit 1; }
	bash scripts/pentest-review.sh

dead-code:
	@bash scripts/require-tool.sh staticcheck
	cd agent && cargo clippy --workspace --all-targets -- -W dead_code
	cd server && staticcheck -checks U1000 ./...
	cd web && npx ts-prune -p tsconfig.app.json -i 'src/types/api\.d\.ts'
