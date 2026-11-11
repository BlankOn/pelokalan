.PHONY: build run test clean migrate-up migrate-down run-postgres stop-postgres dev fmt lint dbshell deploy deploy-logs deploy-dbshell

# Configuration
NETWORK=pelokalan-network
POSTGRES_CONTAINER_NAME=pelokalan-postgres
POSTGRES_DB=pelokalan
POSTGRES_USER=postgres
POSTGRES_PASS=postgres
POSTGRES_PORT=5432
POSTGRES_IMAGE_TAG=postgres:16

# Build the application
build:
	go build -o bin/server ./cmd/server

# Run the application (migrations run automatically on start)
run:
	go run ./cmd/server

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -rf bin/

# Docker network
create-network:
	docker network create ${NETWORK} || true

# Run PostgreSQL in Docker
run-postgres: stop-postgres create-network
	docker run -d \
		--network=${NETWORK} \
		--name ${POSTGRES_CONTAINER_NAME} \
		-e POSTGRES_DB=${POSTGRES_DB} \
		-e POSTGRES_USER=${POSTGRES_USER} \
		-e POSTGRES_PASSWORD=${POSTGRES_PASS} \
		-p ${POSTGRES_PORT}:5432 \
		${POSTGRES_IMAGE_TAG}
	@echo "Waiting for PostgreSQL to be ready..."
	@sleep 3
	@until docker exec ${POSTGRES_CONTAINER_NAME} pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}; do sleep 1; done
	@echo "PostgreSQL is ready!"

# Stop PostgreSQL container
stop-postgres:
	docker stop ${POSTGRES_CONTAINER_NAME} || true
	docker rm ${POSTGRES_CONTAINER_NAME} || true

# Database migrations using golang-migrate (the server also runs them on start)
migrate-up:
	migrate -path ./migrations -database "postgres://${POSTGRES_USER}:${POSTGRES_PASS}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable" up

migrate-down:
	migrate -path ./migrations -database "postgres://${POSTGRES_USER}:${POSTGRES_PASS}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable" down

# Setup: run postgres and migrate
setup: run-postgres migrate-up
	@echo "Setup complete! Database is ready."

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
