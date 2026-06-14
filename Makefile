.DEFAULT_GOAL := help

PROTO_DIR := plugins/db/api/proto


.PHONY: proto
proto:  ## Regenerate gRPC stubs from plugins/db/api/proto/db.proto.
	protoc -I $(PROTO_DIR) \
		--go_out=$(PROTO_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_DIR) --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/db.proto


.PHONY: test
test:  ## Run all unit tests with the race detector (skips integration).
	go test ./... -race


.PHONY: test-short
test-short:  ## Run unit tests only (skip integration / -tags=integration).
	go test ./... -short -race


.PHONY: integration-test
integration-test: build  ## Run real-mysqld E2E (needs Nix + ~30-60s mysqld warmup).
	PATH=$(CURDIR)/dist:$$PATH go test -tags=integration -timeout=10m -v ./tests/integration/...


.PHONY: lint
lint:  ## golangci-lint run.
	golangci-lint run ./...


.PHONY: fmt
fmt:  ## gofumpt + gci.
	gofumpt -w .
	gci write --skip-generated .


.PHONY: build
build:  ## Build host + plugins under dist/.
	mkdir -p dist
	go build -o dist/bough ./cmd/bough
	go build -o dist/bough-plugin-mysql ./cmd/bough-plugin-mysql


.PHONY: clean
clean:  ## Remove build artefacts.
	rm -rf dist


.PHONY: help
help:  ## Show all targets.
	@grep -E '^[a-zA-Z_-]+:.*?##' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?##"}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
