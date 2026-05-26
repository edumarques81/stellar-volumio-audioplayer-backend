# Stellar Volumio Audioplayer Backend - Makefile
# Standard Go project targets

# Project settings
BINARY_NAME := stellar
BIN_DIR := bin
CMD_DIR := cmd/stellar
COVER_FILE := coverage.out
COVER_HTML := coverage.html

# stellar-spectrum daemon (M1.B) — runs on the Pi alongside MPD, forwards
# computed FFT frames to the Mac backend over HTTP.
SPECTRUM_BINARY_NAME := stellar-spectrum
SPECTRUM_CMD_DIR := cmd/stellar-spectrum
SPECTRUM_PI_BINARY := $(BIN_DIR)/stellar-spectrum-arm64

# stellar-airplay daemon — runs on the Pi alongside shairport-sync,
# tails the metadata pipe and forwards parsed state to the Mac backend.
AIRPLAY_BINARY_NAME := stellar-airplay
AIRPLAY_CMD_DIR := cmd/stellar-airplay
AIRPLAY_PI_BINARY := $(BIN_DIR)/stellar-airplay-arm64

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
.PHONY: all build build-local build-pi build-darwin build-windows build-spectrum build-spectrum-local build-airplay build-airplay-local clean test test-verbose test-race coverage lint fmt vet check deps tidy run help

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

# Cross-compilation for macOS (Darwin ARM64) — M1.A target
DARWIN_BINARY := $(BIN_DIR)/stellar-darwin-arm64

## build-darwin: Cross-compile for macOS (ARM64) — M1.A portability target
build-darwin:
	@echo "Cross-compiling for macOS (Darwin ARM64)..."
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=arm64 \
		$(GO) build -ldflags "$(LDFLAGS)" -o $(DARWIN_BINARY) ./$(CMD_DIR)
	@echo "Binary built: $(DARWIN_BINARY)"

# Cross-compilation for Windows (AMD64) — M1.A target. Stubs only on Windows;
# this target exists to catch linker errors and missing-impl gaps that go vet
# alone misses.
WINDOWS_BINARY := $(BIN_DIR)/stellar-windows-amd64.exe

## build-windows: Cross-compile for Windows (AMD64) — M1.A portability target
build-windows:
	@echo "Cross-compiling for Windows (AMD64)..."
	@mkdir -p $(BIN_DIR)
	GOOS=windows GOARCH=amd64 \
		$(GO) build -ldflags "$(LDFLAGS)" -o $(WINDOWS_BINARY) ./$(CMD_DIR)
	@echo "Binary built: $(WINDOWS_BINARY)"

## build-spectrum: Cross-compile stellar-spectrum daemon for Raspberry Pi 5 (ARM64 Linux)
build-spectrum:
	@echo "Cross-compiling $(SPECTRUM_BINARY_NAME) for Raspberry Pi 5 (ARM64)..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=$(PI_GOOS) GOARCH=$(PI_GOARCH) \
		$(GO) build -ldflags "$(LDFLAGS)" \
		-o $(SPECTRUM_PI_BINARY) ./$(SPECTRUM_CMD_DIR)
	@echo "Binary built: $(SPECTRUM_PI_BINARY)"

## build-spectrum-local: Build stellar-spectrum for host (macOS dev / smoke test)
build-spectrum-local:
	@echo "Building $(SPECTRUM_BINARY_NAME) for local platform..."
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(SPECTRUM_BINARY_NAME) ./$(SPECTRUM_CMD_DIR)
	@echo "Binary built: $(BIN_DIR)/$(SPECTRUM_BINARY_NAME)"

## build-airplay: Cross-compile stellar-airplay daemon for Raspberry Pi 5 (ARM64 Linux)
build-airplay:
	@echo "Cross-compiling $(AIRPLAY_BINARY_NAME) for Raspberry Pi 5 (ARM64)..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=$(PI_GOOS) GOARCH=$(PI_GOARCH) \
		$(GO) build -ldflags "$(LDFLAGS)" \
		-o $(AIRPLAY_PI_BINARY) ./$(AIRPLAY_CMD_DIR)
	@echo "Binary built: $(AIRPLAY_PI_BINARY)"

## build-airplay-local: Build stellar-airplay for host (macOS dev / smoke test)
build-airplay-local:
	@echo "Building $(AIRPLAY_BINARY_NAME) for local platform..."
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(AIRPLAY_BINARY_NAME) ./$(AIRPLAY_CMD_DIR)
	@echo "Binary built: $(BIN_DIR)/$(AIRPLAY_BINARY_NAME)"

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
