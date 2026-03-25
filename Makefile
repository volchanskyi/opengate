.PHONY: build test test-short test-integration test-coverage lint lint-deploy fmt verify-codegen golden ci clean e2e load-test load-test-quic

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
	VAPID_CONTACT=dummy IMAGE_TAG=latest DOMAIN=example.com

lint-deploy:
	@command -v yamllint >/dev/null && yamllint -c .yamllint.yml deploy/ || echo "SKIP: yamllint not installed"
	terraform -chdir=deploy/terraform fmt -check -recursive
	terraform -chdir=deploy/terraform init -backend=false -input=false >/dev/null 2>&1
	terraform -chdir=deploy/terraform validate
	@command -v tflint >/dev/null && (tflint --init --chdir=deploy/terraform && tflint --chdir=deploy/terraform --format=compact) || echo "SKIP: tflint not installed"
	cd deploy && $(DEPLOY_DUMMY_ENV) docker compose config --quiet
	cd deploy && $(DEPLOY_DUMMY_ENV) STAGING_JWT_SECRET=dummy \
	  docker compose -f docker-compose.yml -f docker-compose.staging.yml config --quiet
	cd deploy && docker compose -f docker-compose.test.yml config --quiet
	@command -v caddy >/dev/null && (caddy fmt --diff deploy/caddy/Caddyfile && caddy fmt --diff deploy/caddy/Caddyfile.staging \
	  && caddy validate --config deploy/caddy/Caddyfile --adapter caddyfile \
	  && caddy validate --config deploy/caddy/Caddyfile.staging --adapter caddyfile) || echo "SKIP: caddy not installed"
	@command -v trivy >/dev/null && (trivy config --severity HIGH,CRITICAL --exit-code 1 deploy/ \
	  && trivy config --severity HIGH,CRITICAL --exit-code 1 Dockerfile) || echo "SKIP: trivy not installed"
	bash deploy/tests/validate-configs.sh

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

clean:
	cd agent && cargo clean
	cd server && rm -rf bin/
	cd web && rm -rf dist/ node_modules/.cache
