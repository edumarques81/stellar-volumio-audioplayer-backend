# Stellar Volumio Audioplayer Backend - Makefile
# Standard Go project targets

# Project settings
BINARY_NAME := stellar
BIN_DIR := bin
CMD_DIR := cmd/stellar
COVER_FILE := coverage.out
COVER_HTML := coverage.html

# Build settings
GO := go
GOFLAGS := -v
LDFLAGS := -s -w

# Cross-compilation for Raspberry Pi (ARM64)
PI_GOOS := linux
PI_GOARCH := arm64
PI_CC := aarch64-linux-musl-gcc
PI_BINARY := $(BIN_DIR)/stellar-arm64

# Default target - builds for Raspberry Pi 5 (ARM64)
.DEFAULT_GOAL := build

# Phony targets
.PHONY: all build build-local build-pi clean test test-verbose test-race coverage lint fmt vet check deps tidy run help

## help: Show this help message
help:
	@echo "Stellar Volumio Audioplayer Backend"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/  /'

## all: Run lint, test, and build for Pi
all: lint test build

## build: Cross-compile for Raspberry Pi 5 (ARM64 Linux) - DEFAULT TARGET
build:
	@echo "Cross-compiling for Raspberry Pi 5 (ARM64)..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=1 CC=$(PI_CC) GOOS=$(PI_GOOS) GOARCH=$(PI_GOARCH) \
		$(GO) build -ldflags='-linkmode external -extldflags "-static"' \
		-o $(PI_BINARY) ./$(CMD_DIR)
	@echo "Binary built: $(PI_BINARY)"

## build-local: Build the binary for the current platform (macOS dev)
build-local:
	@echo "Building $(BINARY_NAME) for local platform..."
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "Binary built: $(BIN_DIR)/$(BINARY_NAME)"

## clean: Remove build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR)
	@rm -f $(COVER_FILE) $(COVER_HTML)
	$(GO) clean

## test: Run all tests
test:
	@echo "Running tests..."
	$(GO) test ./...

## test-verbose: Run all tests with verbose output
test-verbose:
	@echo "Running tests (verbose)..."
	$(GO) test -v ./...

## test-race: Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	$(GO) test -race ./...

## coverage: Run tests with coverage report
coverage:
	@echo "Running tests with coverage..."
	$(GO) test -coverprofile=$(COVER_FILE) -covermode=atomic ./...
	@echo "Coverage report: $(COVER_FILE)"
	@$(GO) tool cover -func=$(COVER_FILE) | tail -1

## coverage-html: Generate HTML coverage report
coverage-html: coverage
	@echo "Generating HTML coverage report..."
	$(GO) tool cover -html=$(COVER_FILE) -o $(COVER_HTML)
	@echo "HTML report: $(COVER_HTML)"

## lint: Run golangci-lint (install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null 2>&1 || (echo "golangci-lint not installed. Run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest" && exit 1)
	golangci-lint run ./...

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	@echo "Done."

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

## check: Run fmt, vet, and lint
check: fmt vet lint

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download

## tidy: Tidy go.mod and go.sum
tidy:
	@echo "Tidying modules..."
	$(GO) mod tidy

## run: Build and run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BIN_DIR)/$(BINARY_NAME)

## install-tools: Install development tools
install-tools:
	@echo "Installing development tools..."
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Done."
