.DEFAULT_GOAL := help
SHELL := /bin/bash

BINARY := bin/roundly
GO_FILES := $(shell find . -name '*.go' -not -path './web/node_modules/*')

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: setup
setup: ## Install frontend dependencies and Go tools
	cd web && npm install
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go mod download

.PHONY: sqlc
sqlc: ## Regenerate the typed query layer from db/queries
	sqlc generate

.PHONY: build
build: web-build ## Build the single binary with the frontend embedded
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o $(BINARY) ./cmd/server
	@echo "built $(BINARY)"

.PHONY: web-build
web-build: ## Build the frontend into web/dist
	cd web && npm run build

# The Go server reads its configuration from the environment, not from a file,
# so `.env` has to be exported into the shell before it can see any of it. The
# `set -a` makes every assignment in the file an export; `-` on the include is
# not used because the file is optional and sourcing it must not fail.
.PHONY: run
run: ## Run the API only, for use with the Vite dev server
	set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/server

.PHONY: dev
dev: ## Run the Vite dev server (expects `make run` in another terminal)
	cd web && npm run dev

.PHONY: test
test: ## Run Go tests
	go test ./...

.PHONY: test-web
test-web: ## Type-check the frontend
	cd web && npm run lint

.PHONY: check
check: ## Format check, vet, and test everything
	gofmt -l ./cmd ./internal ./db ./web
	go vet ./...
	go test ./...
	cd web && npm run lint

.PHONY: fmt
fmt: ## Format Go source
	gofmt -w ./cmd ./internal ./db ./web

.PHONY: clean
clean: ## Remove build output
	rm -rf bin web/dist
	git checkout -- web/dist/index.html 2>/dev/null || true

.PHONY: docker
docker: ## Build the Docker image
	docker build -t roundly:latest .
