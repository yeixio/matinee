.DEFAULT_GOAL := help
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/yeixio/matinee/internal/version.Version=$(VERSION)

.PHONY: help web build run dev test test-postgres lint demo docker helm-lint boxoffice-web boxoffice boxoffice-run boxoffice-dev boxoffice-docker

help: ## Show targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

web: ## Build the web player into web/dist
	cd web && pnpm install --frozen-lockfile && pnpm build

build: web ## Build bin/matinee with the web player embedded
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/matinee ./cmd/matinee

run: build ## Build and run on http://localhost:7171 with data in ./data
	./bin/matinee

dev: ## Run the web player with hot reload (start the server with `make run` first)
	cd web && pnpm dev

test: ## Go and web tests
	go test ./...
	cd web && pnpm typecheck && pnpm test
	cd boxoffice/web && pnpm typecheck && pnpm test

test-postgres: ## Go tests against PostgreSQL (set MATINEE_TEST_DATABASE_URL)
	go test -count=1 ./...

lint: ## go vet and gofmt
	go vet ./...
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

demo: ## Make a demo library in ./demo-media
	scripts/demo-library.sh ./demo-media

docker: ## Build the Docker image
	docker build --build-arg VERSION=$(VERSION) -t ghcr.io/yeixio/matinee:$(VERSION) .

helm-lint: ## Lint the Helm charts
	helm lint deploy/helm/matinee
	helm lint deploy/helm/boxoffice

boxoffice-web: ## Build Box Office's web UI into boxoffice/web/dist
	cd boxoffice/web && pnpm install --frozen-lockfile && pnpm build

boxoffice: boxoffice-web ## Build bin/boxoffice with its web UI embedded
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/boxoffice ./cmd/boxoffice

boxoffice-run: boxoffice ## Build and run Box Office on http://localhost:7272 with data in ./data/boxoffice
	BOXOFFICE_DATA_DIR=./data/boxoffice ./bin/boxoffice

boxoffice-dev: ## Run Box Office's web UI with hot reload (start it with `make boxoffice-run` first)
	cd boxoffice/web && pnpm dev

boxoffice-docker: ## Build Box Office's Docker image
	docker build -f cmd/boxoffice/Dockerfile --build-arg VERSION=$(VERSION) -t ghcr.io/yeixio/boxoffice:$(VERSION) .
