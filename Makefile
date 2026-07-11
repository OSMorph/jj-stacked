# jj-stacked Makefile

# Get version from git tag or commit hash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

# Binary names and paths
BINARY := jj-stacked
ALIAS := jjk
CMD_PATH := ./cmd/jj-stacked
BUILD_DIR ?= bin
PREFIX ?= $(HOME)/.local
INSTALL_DIR ?= $(PREFIX)/bin

.PHONY: build build-all test lint fmt fmt-check install uninstall clean all check

# Default target
all: build

# Build the primary binary with version embedded
build:
	mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD_PATH)

# Build jj-stacked and create the jjk alias
build-all: build
	ln -sf $(BINARY) $(BUILD_DIR)/$(ALIAS)

# Run tests with race detector
test:
	go test -race ./...

# Run golangci-lint
lint:
	golangci-lint run ./...

# Format code
fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

# Verify formatting without rewriting the worktree.
fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"

# Install both command names to INSTALL_DIR
install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	ln -sf $(INSTALL_DIR)/$(BINARY) $(INSTALL_DIR)/$(ALIAS)
	@echo "Installed $(BINARY) and $(ALIAS) to $(INSTALL_DIR)"

# Uninstall both binaries
uninstall:
	rm -f $(INSTALL_DIR)/$(BINARY)
	rm -f $(INSTALL_DIR)/$(ALIAS)
	@echo "Uninstalled $(BINARY) and $(ALIAS) from $(INSTALL_DIR)"

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	go clean ./...

# Run all checks (useful for CI)
check: fmt-check lint test build
