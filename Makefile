.PHONY: build test test-short test-integration test-coverage lint lint-deploy fmt golden ci clean

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
	@command -v tflint >/dev/null && (tflint --init --chdir=deploy/terraform && tflint --chdir=deploy/terraform --format=compact) || echo "SKIP: tflint not installed"
	cd deploy && $(DEPLOY_DUMMY_ENV) docker compose config --quiet
	cd deploy && $(DEPLOY_DUMMY_ENV) STAGING_JWT_SECRET=dummy \
	  docker compose -f docker-compose.yml -f docker-compose.staging.yml config --quiet
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

golden:
	cd agent && GENERATE_GOLDEN=1 cargo test -p mesh-protocol --test golden_test
	cd server && go test ./internal/protocol/ -run TestGolden

ci: lint test build

clean:
	cd agent && cargo clean
	cd server && rm -rf bin/
	cd web && rm -rf dist/ node_modules/.cache
