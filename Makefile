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
