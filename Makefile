# jj-stacked Makefile

# Get version from git tag or commit hash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

# Binary names and paths
BINARY := jj-stacked
ALIAS := jjk
CMD_PATH := ./cmd/jj-stacked
INSTALL_DIR := $(shell go env GOPATH)/bin

.PHONY: build build-all test lint fmt install uninstall clean all check

# Default target
all: build

# Build the primary binary with version embedded
build:
	go build $(LDFLAGS) -o $(BINARY) $(CMD_PATH)

# Build both jj-stacked and jjk binaries
build-all: build
	cp $(BINARY) $(ALIAS)

# Run tests with race detector
test:
	go test -race ./...

# Run golangci-lint
lint:
	golangci-lint run ./...

# Format code
fmt:
	go fmt ./...
	goimports -w .

# Install both binaries to GOPATH/bin
install: build
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)
	ln -sf $(INSTALL_DIR)/$(BINARY) $(INSTALL_DIR)/$(ALIAS)
	@echo "Installed $(BINARY) and $(ALIAS) to $(INSTALL_DIR)"

# Uninstall both binaries
uninstall:
	rm -f $(INSTALL_DIR)/$(BINARY)
	rm -f $(INSTALL_DIR)/$(ALIAS)
	@echo "Uninstalled $(BINARY) and $(ALIAS) from $(INSTALL_DIR)"

# Clean build artifacts
clean:
	rm -f $(BINARY) $(ALIAS)
	go clean ./...

# Run all checks (useful for CI)
check: fmt lint test build
