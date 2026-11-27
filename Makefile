.PHONY: prep build run test test-integration check cover clean migrate-up migrate-down run-postgres stop-postgres setup dev fmt lint dbshell deploy deploy-logs deploy-dbshell

# Configuration
POSTGRES_CONTAINER_NAME ?= pelokalan-postgres
POSTGRES_DB=pelokalan
POSTGRES_USER=postgres
POSTGRES_PASS=postgres
# the port make prep chose (DB_PORT in .env), else 5432
POSTGRES_PORT=$(or $(shell grep -E '^DB_PORT=' .env 2>/dev/null | tail -n1 | cut -d= -f2),5432)
POSTGRES_IMAGE_TAG ?= postgres:16

# Build the application
build:
	go build -o bin/server ./cmd/server

# Run the application (migrations run automatically on start)
run:
	go run ./cmd/server

# Run tests (integration tests are skipped without TEST_DATABASE_URL)
test:
	go test ./...

# Every test, including the HTTP integration tests, against a throwaway database on the local
# PostgreSQL (make prep). Each run creates pelokalan_test_<random> and drops it; your data is untouched.
TEST_DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASS)@localhost:$(POSTGRES_PORT)/postgres?sslmode=disable
test-integration:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 ./...

# Everything a change must pass: formatting, go vet and every test (integration included)
check:
	@./scripts/check-fmt.sh
	go vet ./...
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 ./...

# Coverage of all packages by all tests (integration included): total on the last line, HTML in bin/coverage.html
cover:
	@mkdir -p bin
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 -coverpkg=./... -coverprofile=bin/coverage.out ./... > /dev/null
	go tool cover -func=bin/coverage.out | tail -n 1
	go tool cover -html=bin/coverage.out -o bin/coverage.html

# Clean build artifacts
clean:
	rm -rf bin/

# Prepare a development machine: .env, PostgreSQL in Docker (data kept in a volume), Go modules.
# Safe to run again; afterwards `make run` starts the app. See scripts/prep.sh.
prep:
	@POSTGRES_CONTAINER_NAME=${POSTGRES_CONTAINER_NAME} POSTGRES_IMAGE_TAG=${POSTGRES_IMAGE_TAG} ./scripts/prep.sh

# Start PostgreSQL in Docker (the same as prep: existing data is kept)
run-postgres: prep

# Stop the PostgreSQL container (its data is kept; make prep starts it again)
stop-postgres:
	docker stop ${POSTGRES_CONTAINER_NAME} || true

# Database migrations using golang-migrate (the server also runs them on start)
migrate-up:
	migrate -path ./migrations -database "postgres://${POSTGRES_USER}:${POSTGRES_PASS}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable" up

migrate-down:
	migrate -path ./migrations -database "postgres://${POSTGRES_USER}:${POSTGRES_PASS}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable" down

# Setup: the same as prep (the server runs migrations on start)
setup: prep

# Development with hot reload (requires air)
dev:
	air

fmt:
	go fmt ./...

# Lint code (requires golangci-lint)
lint:
	golangci-lint run

dbshell:
	docker exec -ti -e POSTGRES_PASSWORD=${POSTGRES_PASS} ${POSTGRES_CONTAINER_NAME} psql -U ${POSTGRES_USER} -d ${POSTGRES_DB}

DEPLOY_HOST ?= user@192.168.200.1
DEPLOY_DIR  ?= src/pelokalan

# Copy the source to the server, rebuild the image there and recreate only the app container
deploy:
	tar --exclude=./.env --exclude=./bin --exclude=./.git --exclude=./.claude -czf - . | \
		ssh $(DEPLOY_HOST) 'mkdir -p ~/$(DEPLOY_DIR) && tar -xzf - -C ~/$(DEPLOY_DIR)'
	ssh $(DEPLOY_HOST) 'cd ~/$(DEPLOY_DIR) && docker build -q -t pelokalan:latest . && cd ~ && docker compose up -d --no-deps pelokalan'
	ssh $(DEPLOY_HOST) 'sleep 3 && docker logs --tail 5 pelokalan'

deploy-logs:
	ssh $(DEPLOY_HOST) 'docker logs -f --tail 100 pelokalan'

deploy-dbshell:
	ssh -t $(DEPLOY_HOST) 'docker exec -ti pelokalan-db psql -U pelokalan -d pelokalan'
