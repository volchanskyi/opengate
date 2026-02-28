.PHONY: build test test-short test-integration test-coverage lint fmt golden ci e2e clean

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
	cd server && go test -race -timeout 5m ./internal/integration/

test-coverage:
	cd server && go test -race -coverprofile=coverage.out -covermode=atomic ./... && go tool cover -func=coverage.out

lint:
	cd agent && cargo clippy --workspace -- -D warnings
	cd server && go vet ./...
	cd web && npx eslint src/

fmt:
	cd agent && cargo fmt --all
	cd server && gofmt -w .
	cd web && npx prettier --write src/

golden:
	cd agent && GENERATE_GOLDEN=1 cargo test -p mesh-protocol --test golden
	cd server && go test ./internal/protocol/ -run TestGolden

ci: lint test build

e2e:
	cd tests && docker compose up -d
	cd tests/e2e && npx playwright test
	cd tests && docker compose down

clean:
	cd agent && cargo clean
	cd server && rm -rf bin/
	cd web && rm -rf dist/ node_modules/.cache
