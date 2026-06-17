# Load variables from .env (if present) and export them to recipe shells.
-include .env
export

MIGRATIONS_PATH ?= migrations
DB_URL := postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=$(DB_SSLMODE)

.PHONY: help run build tidy test \
	migrate-create migrate-up migrate-down migrate-force migrate-version

help: ## Show this help.
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

run: ## Run the HTTP server.
	go run ./cmd/server

build: ## Build the server binary into ./build/main.
	go build -o ./build/main ./cmd/server

tidy: ## Tidy module dependencies.
	go mod tidy

test: ## Run the test suite.
	go test ./...

## --- Database migrations (requires golang-migrate CLI) ---
## Install: https://github.com/golang-migrate/migrate/tree/master/cmd/migrate

migrate-create: ## Create a new migration: make migrate-create NAME=add_something
	migrate create -ext sql -dir $(MIGRATIONS_PATH) -seq $(NAME)

migrate-up: ## Apply all up migrations.
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" up

migrate-down: ## Roll back migrations: make migrate-down NUM=1 (omit NUM for all).
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" down $(NUM)

migrate-force: ## Force the schema to a version: make migrate-force VER=1
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" force $(VER)

migrate-version: ## Print the current migration version.
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" version
