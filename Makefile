APP_NAME := bluesky-feeds
MODULE := github.com/blackmichael/bluesky-feeds
GO := go
GOFLAGS :=
LDFLAGS := -ldflags "-s -w"
BUILD_DIR := bin

.PHONY: all build build-publish run clean test test-verbose test-coverage lint fmt vet tidy check help \
	docker-up docker-down docker-reset docker-build docker-build-arm64 docker-save-arm64 docker-run docker-logs docker-stop-server docker-up-deps-only \
	generate publish unpublish migrate-up migrate-down migrate-force

## help: print this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'

## all: run checks then build
all: check build

## build: compile all binaries
build: build-server build-publish

## build-server: compile the server
build-server:
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server

## build-publish: compile the publish tool
build-publish:
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-publish ./cmd/publish

## run: run the application (ensure migrations are applied first)
run:
	$(GO) run ./cmd/server

## run-env: run the application with .env file loaded
run-env:
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; $(GO) run ./cmd/server

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR)
	$(GO) clean -cache -testcache

## test: run all tests
test:
	$(GO) test ./... -race

## test-verbose: run all tests with verbose output
test-verbose:
	$(GO) test ./... -race -v

## test-coverage: run tests with coverage report
test-coverage:
	$(GO) test ./... -race -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: run golangci-lint (install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
lint:
	golangci-lint run ./...

## fmt: format all Go source files
fmt:
	$(GO) fmt ./...
	goimports -w .

## vet: run go vet
vet:
	$(GO) vet ./...

## tidy: tidy and verify module dependencies
tidy:
	$(GO) mod tidy
	$(GO) mod verify

## check: format, vet, and test
check: fmt vet test

## generate: run go generate
generate:
	$(GO) generate ./...

## docker-up: start server and dependencies via docker-compose (configure via .env)
docker-up:
	docker-compose up --build -d
	@echo "Services started. Server available at http://localhost:3000"

## docker-up-deps-only: start only dependencies, no server
docker-up-deps-only:
	docker-compose up -d deps_only
	@echo "Services started."

## docker-down: stop local Postgres
docker-down:
	docker-compose down --volumes

# docker-logs: streams server logs to your shell
docker-logs:
	docker logs -f bluesky-feeds-server-1

docker-stop-server:
	docker stop bluesky-feeds-server-1

## docker-reset: stop Postgres and destroy data volume
docker-reset:
	docker-compose down -v

## publish: publish a feed generator record to BlueSky (use ARGS to pass flags)
## 	e.g. make publish ARGS='--rkey my-feed --name "My Feed" --description "A cool feed"'
publish: build-publish
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; $(BUILD_DIR)/$(APP_NAME)-publish $(ARGS)

## unpublish: delete a feed generator record from BlueSky
## 	e.g. make unpublish ARGS='--rkey my-feed'
unpublish: build-publish
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; $(BUILD_DIR)/$(APP_NAME)-publish --unpublish $(ARGS)

## migrate-up: run database migrations up (required before first run)
migrate-up:
	migrate -path migrations -database postgres://postgres:postgres@localhost:5432/bluesky_feeds?sslmode=disable up

## migrate-down: run database migrations down
migrate-down:
	migrate -path migrations -database "postgres://postgres:postgres@localhost:5432/bluesky_feeds?sslmode=disable" down

## migrate-force: force migration version (use with caution)
## 	e.g. make migrate-force VERSION=1
migrate-force:
	migrate -path migrations -database "postgres://postgres:postgres@localhost:5432/bluesky_feeds?sslmode=disable" force $(VERSION)

## setup: start database and run migrations (first-time setup)
setup: docker-up migrate-up

## docker-build: build Docker image for current platform
docker-build:
	docker build -t $(APP_NAME) .

## docker-build-arm64: build Docker image for ARM64
docker-build-arm64:
	docker build --build-arg TARGETARCH=arm64 -t $(APP_NAME):arm64 .

## docker-save-arm64: save ARM64 image to tar file
docker-save-arm64: docker-build-arm64
	docker save $(APP_NAME):arm64 | gzip > bin/$(APP_NAME)-arm64.tar.gz
	@echo "Image saved to bin/$(APP_NAME)-arm64.tar.gz"
