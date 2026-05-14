.PHONY: build test test-short test-integration test-coverage lint lint-deploy fmt verify-codegen golden ci clean e2e load-test load-test-quic sonar sonar-coverage sonar-quick \
	mutate mutate-rust mutate-go mutate-web taint-go taint-web dead-code \
	terraform-test terraform-drift

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
	cd server && go test -race -timeout 5m ./...

test-web:
	cd web && npx vitest run

test-integration:
	cd server && go test -race -timeout 5m ./tests/integration/

test-coverage:
	cd server && go test -race -coverprofile=coverage.out -covermode=atomic ./... && go tool cover -func=coverage.out

lint: lint-deploy
	cd agent && cargo clippy --workspace -- -D warnings
	cd server && go vet ./...
	cd web && npx eslint src/
	actionlint

DEPLOY_DUMMY_ENV := JWT_SECRET=dummy AMT_USER=admin AMT_PASS=dummy \
	VAPID_CONTACT=dummy IMAGE_TAG=latest DOMAIN=example.com \
	POSTGRES_PASSWORD=dummy

lint-deploy:
	@command -v yamllint >/dev/null 2>&1 || { echo "ERROR: yamllint not found. Install with: pip install yamllint"; exit 1; }
	yamllint -c .yamllint.yml deploy/
	terraform -chdir=deploy/terraform fmt -check -recursive
	terraform -chdir=deploy/terraform init -backend=false -input=false >/dev/null 2>&1
	terraform -chdir=deploy/terraform validate
	@command -v tflint >/dev/null 2>&1 || { echo "ERROR: tflint not found. Install from: https://github.com/terraform-linters/tflint"; exit 1; }
	tflint --init --chdir=deploy/terraform && tflint --chdir=deploy/terraform --format=compact
	@# Output-sensitivity grep — cheaper and more deterministic than asserting in tftest.
	@# instance_id and cd_nsg_id contain OCIDs consumed via GitHub Secrets — they MUST
	@# stay marked `sensitive = true` in deploy/terraform/outputs.tf so they never appear
	@# in plan/apply logs.
	@for out in instance_id cd_nsg_id; do \
	  grep -A3 "output \"$$out\"" deploy/terraform/outputs.tf | grep -q "sensitive *= *true" \
	    || { echo "ERROR: output \"$$out\" must have sensitive = true in deploy/terraform/outputs.tf"; exit 1; }; \
	done
	@$(MAKE) terraform-test
	cd deploy && $(DEPLOY_DUMMY_ENV) docker compose config --quiet
	cd deploy && $(DEPLOY_DUMMY_ENV) STAGING_JWT_SECRET=dummy STAGING_POSTGRES_PASSWORD=dummy \
	  docker compose -f docker-compose.yml -f docker-compose.staging.yml config --quiet
	cd deploy && docker compose -f docker-compose.test.yml config --quiet
	@command -v caddy >/dev/null 2>&1 || { echo "ERROR: caddy not found. Install from: https://caddyserver.com/docs/install"; exit 1; }
	caddy fmt --diff deploy/caddy/Caddyfile && caddy fmt --diff deploy/caddy/Caddyfile.staging \
	  && caddy validate --config deploy/caddy/Caddyfile --adapter caddyfile \
	  && caddy validate --config deploy/caddy/Caddyfile.staging --adapter caddyfile
	@command -v trivy >/dev/null 2>&1 || { echo "ERROR: trivy not found. Install from: https://aquasecurity.github.io/trivy"; exit 1; }
	trivy config --severity HIGH,CRITICAL --exit-code 1 deploy/ \
	  && trivy config --severity HIGH,CRITICAL --exit-code 1 Dockerfile
	bash deploy/tests/validate-configs.sh

# Module-invariant assertions for the Terraform config (mock_provider, no OCI creds).
# Each submodule and the root carry their own tftest suite; the umbrella target runs
# all three. Requires terraform >= 1.7 for `expect_failures` against variable validation.
terraform-test:
	terraform -chdir=deploy/terraform/modules/networking init -backend=false -input=false >/dev/null
	terraform -chdir=deploy/terraform/modules/networking test
	terraform -chdir=deploy/terraform/modules/compute init -backend=false -input=false >/dev/null
	terraform -chdir=deploy/terraform/modules/compute test
	terraform -chdir=deploy/terraform init -backend=false -input=false >/dev/null
	terraform -chdir=deploy/terraform test

# Local mirror of the .github/workflows/terraform-drift.yml plan step.
# Uses the operator's local OCI creds (terraform.tfvars + ~/.oci/terraform-credentials)
# and prints the refresh-only diff. Exit 2 = drift detected; exit 0 = clean.
terraform-drift:
	terraform -chdir=deploy/terraform plan -refresh-only -detailed-exitcode

fmt:
	cd agent && cargo fmt --all
	cd server && gofmt -w .
	cd web && npx prettier --write src/

verify-codegen:
	@command -v oapi-codegen >/dev/null 2>&1 || { echo "ERROR: oapi-codegen not found in PATH. Install with: go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.6.0"; exit 1; }
	cd server && oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml > internal/api/openapi_gen.go && git diff --exit-code internal/api/

golden:
	cd agent && GENERATE_GOLDEN=1 cargo test -p mesh-protocol --test golden_test
	cd server && go test ./internal/protocol/ -run TestGolden

ci: lint test build

e2e:
	cd deploy && docker compose -f docker-compose.test.yml up -d --build --wait
	cd web && npx playwright test
	cd deploy && docker compose -f docker-compose.test.yml down -v

load-test:
	k6 run --env BASE_URL=http://localhost:8080 load/k6/scenarios/api-baseline.js
	k6 run --env BASE_URL=http://localhost:8080 load/k6/scenarios/relay-throughput.js

load-test-quic:
	cd server && go run ./tests/loadtest/ -agents=100 -addr=127.0.0.1:9090

sonar-coverage:
	cd server && go test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...
	cd agent && cargo llvm-cov nextest --workspace --lcov --output-path lcov.info \
		--ignore-filename-regex '(main\.rs|/webrtc\.rs|/terminal\.rs|/session/mod\.rs|/session/relay\.rs|/tests/)'
	cd web && npx vitest run --coverage

sonar: sonar-coverage
	@test -n "$$SONAR_TOKEN" || { echo "ERROR: SONAR_TOKEN not set. Export it or add to .env"; exit 1; }
	docker run --rm \
		-e SONAR_TOKEN="$$SONAR_TOKEN" \
		-v "$$(pwd):/usr/src" \
		-w /usr/src \
		sonarsource/sonar-scanner-cli:latest \
		-Dsonar.qualitygate.wait=true \
		-Dsonar.branch.name=dev

sonar-quick:
	@test -n "$$SONAR_TOKEN" || { echo "ERROR: SONAR_TOKEN not set. Export it or add to .env"; exit 1; }
	docker run --rm \
		-e SONAR_TOKEN="$$SONAR_TOKEN" \
		-v "$$(pwd):/usr/src" \
		-w /usr/src \
		sonarsource/sonar-scanner-cli:latest \
		-Dsonar.qualitygate.wait=true \
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
	cd agent && OPENGATE_GOLDEN_DIR=$(CURDIR)/testdata/golden cargo mutants --workspace --no-shuffle

mutate-go:
	@command -v gremlins >/dev/null 2>&1 || { echo "ERROR: gremlins not found. Install with: go install github.com/go-gremlins/gremlins/cmd/gremlins@latest"; exit 1; }
	@if [ -z "$$POSTGRES_TEST_URL" ]; then \
	  echo "WARNING: POSTGRES_TEST_URL not set; api/db tests will skip and many mutants will be NOT COVERED."; \
	  echo "         Start a test Postgres (see .github/workflows/ci.yml) and set:"; \
	  echo "         export POSTGRES_TEST_URL=\"postgres://opengate:opengate@localhost:5432/opengate_test?sslmode=disable\""; \
	fi
	cd server && gremlins unleash .

mutate-web:
	cd web && npx stryker run

# Static taint linting — catches data-flow paths from sources to sinks.
taint-go:
	@command -v gosec >/dev/null 2>&1 || { echo "ERROR: gosec not found. Install with: go install github.com/securego/gosec/v2/cmd/gosec@latest"; exit 1; }
	cd server && gosec -conf .gosec.json ./...

taint-web:
	cd web && npx eslint --config eslint.security.config.js src/

# Dead-code & unused-symbol sweep across all three languages.
dead-code:
	@command -v staticcheck >/dev/null 2>&1 || { echo "ERROR: staticcheck not found. Install with: go install honnef.co/go/tools/cmd/staticcheck@latest"; exit 1; }
	cd agent && cargo clippy --workspace --all-targets -- -W dead_code
	cd server && staticcheck -checks U1000 ./...
	cd web && npx ts-prune -p tsconfig.app.json -i 'src/types/api\.d\.ts'
