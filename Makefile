# ==============================================================================
# WARNING: etcd-synthetic-load intentionally STRESSES ETCD.
# NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT. Lab/Test/Dev only.
# See README.md before running load-plan / make load-real.
# ==============================================================================

BINARY_NAME    := etcd-synthetic-load
BIN_DIR        := bin
IMAGE_TOOL     ?= podman
IMAGE_NAME     ?= quay.io/dasmlab/etcd-synthetic-load
IMAGE_TAG      ?= latest
VERSION        ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS        := -X main.version=$(VERSION)

.PHONY: help
help: ## Show this help
	@echo "etcd-synthetic-load -- NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT (see README.md)"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: web
web: ## Build Vue UI into cmd/.../static for go:embed
	cd web && npm ci && npm run build
	rm -rf cmd/$(BINARY_NAME)/static/*
	cp -a web/dist/. cmd/$(BINARY_NAME)/static/
	@touch cmd/$(BINARY_NAME)/static/.gitkeep

.PHONY: build
build: ## Build the CLI/UI binary into ./bin (embeds whatever is in static/)
	mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

.PHONY: build-all
build-all: web build ## Build UI then Go binary

.PHONY: test
test: ## Run go vet + go test
	go vet ./...
	go test ./...

.PHONY: fmt
fmt: ## gofmt all Go source
	gofmt -l -w $$(find . -name '*.go' -not -path './vendor/*')

.PHONY: image
image: ## Build the container image (podman/docker)
	$(IMAGE_TOOL) build -t $(IMAGE_NAME):$(IMAGE_TAG) -f Containerfile .

.PHONY: image-push
image-push: ## Push the container image
	$(IMAGE_TOOL) push $(IMAGE_NAME):$(IMAGE_TAG)

.PHONY: configure
configure: build ## Write data/runtime.yaml for PROD-2 lab
	./$(BIN_DIR)/$(BINARY_NAME) configure --force \
		--display-name PROD-2 \
		--api-server https://api.2026-prod-2.ocp.dasmlab.org:6443 \
		--data-dir ./data \
		--out ./data/runtime.yaml

.PHONY: generate
generate: build ## Generate default ~5.6GiB S/M/L plan into data/plans
	ESL_DATA_DIR=./data ESL_RUNTIME_CONFIG=./data/runtime.yaml \
	./$(BIN_DIR)/$(BINARY_NAME) generate --target config/target.example.yaml

.PHONY: load-dry-run
load-dry-run: build ## Dry-run the newest plan (no cluster writes)
	@PLAN=$$(ls -1 data/plans/*.yaml 2>/dev/null | tail -1); \
	test -n "$$PLAN" || (echo "no plan — run make generate first"; exit 1); \
	ESL_DATA_DIR=./data ./$(BIN_DIR)/$(BINARY_NAME) load-plan --plan "$$PLAN" --dry-run

.PHONY: status
status: build ## Report current synthetic namespaces/objects on the cluster
	./$(BIN_DIR)/$(BINARY_NAME) status

.PHONY: cleanup
cleanup: build ## Delete everything this tool created (with confirmation prompt)
	./$(BIN_DIR)/$(BINARY_NAME) cleanup

.PHONY: serve
serve: build ## Run local UI+API on :8080
	ESL_DATA_DIR=./data ESL_RUNTIME_CONFIG=./data/runtime.yaml \
	./$(BIN_DIR)/$(BINARY_NAME) serve --listen :8080

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) web/dist
