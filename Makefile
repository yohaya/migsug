.PHONY: build run test clean install build-linux build-windows build-darwin help

# Default target
all: build

# Build for current platform
build:
	@echo "Building migsug..."
	@chmod +x scripts/build-with-version.sh
	@bash scripts/build-with-version.sh
	@echo "Build complete: bin/*/migsug"

# Build for Linux (Proxmox)
build-linux:
	@echo "Building for Linux..."
	@chmod +x scripts/build-with-version.sh
	@bash scripts/build-with-version.sh linux amd64
	@bash scripts/build-with-version.sh linux arm64
	@echo "Build complete: bin/linux-*/migsug"

# Build for Windows
build-windows:
	@echo "Building for Windows (amd64)..."
	@chmod +x scripts/build-with-version.sh
	@bash scripts/build-with-version.sh windows amd64
	@echo "Build complete: bin/windows-amd64/migsug.exe"

# Build for macOS
build-darwin:
	@echo "Building for macOS..."
	@chmod +x scripts/build-with-version.sh
	@bash scripts/build-with-version.sh darwin amd64
	@bash scripts/build-with-version.sh darwin arm64
	@echo "Build complete: bin/darwin-*/migsug"

# Build for all platforms
build-all: build-linux build-windows build-darwin
	@echo "All builds complete"

# Run the application
run:
	go run cmd/migsug/main.go $(ARGS)

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -f coverage.out coverage.html
	rm -f migsug.log
	@echo "Clean complete"

# Install to system (requires sudo)
install: build-linux
	@echo "Installing to /usr/local/bin..."
	sudo cp bin/linux-amd64/migsug /usr/local/bin/migsug
	sudo chmod +x /usr/local/bin/migsug
	@echo "Installation complete"

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy
	@echo "Dependencies updated"

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "Format complete"

# Lint code (requires golangci-lint)
lint:
	@echo "Linting code..."
	golangci-lint run ./...

# Show help
help:
	@echo "Proxmox VM Migration Suggester - Build Targets"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build          Build for current platform (default)"
	@echo "  build-linux    Build for Linux (Proxmox)"
	@echo "  build-windows  Build for Windows"
	@echo "  build-darwin   Build for macOS"
	@echo "  build-all      Build for all platforms"
	@echo "  run            Run the application (use ARGS=... for arguments)"
	@echo "  test           Run tests"
	@echo "  test-coverage  Run tests with coverage report"
	@echo "  clean          Remove build artifacts"
	@echo "  install        Install to /usr/local/bin (Linux, requires sudo)"
	@echo "  deps           Download and tidy dependencies"
	@echo "  fmt            Format code"
	@echo "  lint           Lint code (requires golangci-lint)"
	@echo "  help           Show this help message"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make run ARGS='--api-token=root@pam!token=secret'"
	@echo "  make build-linux && scp bin/linux-amd64/migsug root@proxmox:/usr/local/bin/migsug"
