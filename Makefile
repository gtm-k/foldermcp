BINARY_NAME := foldermcp
CMD_DIR := ./cmd/foldermcp
BUILD_DIR := ./bin
VERSION ?= dev

GO := go
GOFLAGS := -v
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test lint clean install

## build: Compile the foldermcp binary
build:
	$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

## test: Run all tests with race detection
test:
	$(GO) test -race -count=1 ./...

## lint: Run go vet on all packages
lint:
	$(GO) vet ./...

## clean: Remove build artifacts
clean:
	rm -rf $(BUILD_DIR)
	$(GO) clean -cache -testcache

## install: Install binary to GOPATH/bin
install:
	$(GO) install $(CMD_DIR)

.PHONY: v3-build v3-test v3-lint v3-proto

## v3-build: Compile the v3 indexer daemon (cgo required + mattn sqlite_fts5)
v3-build:
	CGO_ENABLED=1 $(GO) build -tags "cgo sqlite_fts5" -o $(BUILD_DIR)/foldermcp-v3 $(CMD_DIR)

## v3-test: Run v3 tests with race detection (cgo + sqlite_fts5 for FTS5 virtual tables)
v3-test:
	CGO_ENABLED=1 $(GO) test -race -tags "cgo sqlite_fts5" ./internal/v3/...

## v3-lint: Run golangci-lint over the v3 subtree (cgo + sqlite_fts5 tags)
v3-lint:
	golangci-lint run --build-tags "cgo,sqlite_fts5" ./internal/v3/...

## v3-proto: Regenerate v3 protobuf stubs
v3-proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       internal/v3/proto/foldermcp.proto

V3_MODEL_DIR=internal/v3/embed/model

## v3-fetch-model: Download all-MiniLM-L6-v2 ONNX model and tokenizer
v3-fetch-model:
	mkdir -p $(V3_MODEL_DIR)
	test -f $(V3_MODEL_DIR)/model.onnx || curl -L -o $(V3_MODEL_DIR)/model.onnx https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx
	test -f $(V3_MODEL_DIR)/tokenizer.json || curl -L -o $(V3_MODEL_DIR)/tokenizer.json https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/tokenizer.json
