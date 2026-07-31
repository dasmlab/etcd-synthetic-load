# ==============================================================================
# WARNING: etcd-synthetic-load intentionally STRESSES ETCD.
# NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT. Lab/Test/Dev only.
# See README.md before running `make load` or `make load-real`.
# ==============================================================================

BINARY_NAME    := etcd-synthetic-load
MODULE         := github.com/dasmlab/etcd-synthetic-load
BIN_DIR        := bin
IMAGE_TOOL     ?= podman
IMAGE_NAME     ?= quay.io/dasmlab/etcd-synthetic-load
IMAGE_TAG      ?= latest
VERSION        ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS        := -X main.version=$(VERSION)

PROFILE        ?= profile.yaml

.PHONY: help
help: ## Show this help
	@echo "etcd-synthetic-load -- NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT (see README.md)"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the CLI binary into ./bin
	mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

.PHONY: test
test: ## Run go vet + go test
	go vet ./...
	go test ./...

.PHONY: fmt
fmt: ## gofmt all Go source
	gofmt -l -w $$(find . -name '*.go' -not -path './vendor/*')

.PHONY: image
image: ## Build the container image (podman/docker, override IMAGE_TOOL)
	$(IMAGE_TOOL) build -t $(IMAGE_NAME):$(IMAGE_TAG) -f Containerfile .

.PHONY: image-push
image-push: ## Push the container image
	$(IMAGE_TOOL) push $(IMAGE_NAME):$(IMAGE_TAG)

.PHONY: plan
plan: build ## Generate profile.yaml aimed at the reference target (~5.6GiB / 120k secrets / 80k configmaps)
	./$(BIN_DIR)/$(BINARY_NAME) plan \
		--target-gib 5.6 \
		--target-secrets 120000 \
		--target-configmaps 80000 \
		-o $(PROFILE)

.PHONY: load-dry-run
load-dry-run: build ## Show what `load` would create, without touching any cluster
	./$(BIN_DIR)/$(BINARY_NAME) load --profile $(PROFILE) --dry-run

.PHONY: load-real
load-real: build ## DANGEROUS: actually create objects on whatever cluster your kubeconfig points at
	@echo "================================================================================"
	@echo " WARNING: this will create real objects on your CURRENT kubeconfig context."
	@echo " NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT."
	@echo "================================================================================"
	./$(BIN_DIR)/$(BINARY_NAME) load --profile $(PROFILE) --i-understand-this-stresses-etcd

.PHONY: status
status: build ## Report current synthetic namespaces/objects on the cluster
	./$(BIN_DIR)/$(BINARY_NAME) status

.PHONY: cleanup
cleanup: build ## Delete everything this tool created (with confirmation prompt)
	./$(BIN_DIR)/$(BINARY_NAME) cleanup

.PHONY: cleanup-yes
cleanup-yes: build ## Delete everything this tool created, no confirmation prompt
	./$(BIN_DIR)/$(BINARY_NAME) cleanup --yes --wait

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)
