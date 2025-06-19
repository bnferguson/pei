# Variables
BINARY_NAME=pei
DOCKER_IMAGE=pei
DOCKER_TAG=latest
CONFIG_FILE=example/pei.yaml

# Go related variables
GO=go
GOFMT=gofmt
GOLINT=golangci-lint
GOFILES=$(shell find . -name "*.go" -type f)
GOTEST=$(GO) test -v

# Docker related variables
DOCKER=docker
DOCKER_BUILD=$(DOCKER) build
DOCKER_RUN=$(DOCKER) run
DOCKER_RM=$(DOCKER) rm -f

.PHONY: all build clean test fmt lint docker-build docker-run docker-clean help

# Default target
all: clean fmt lint test build

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	$(GO) build -o $(BINARY_NAME)

# Clean build files
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_NAME).test
	rm -f coverage.out

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -w $(GOFILES)

# Run linter
lint:
	@echo "Running linter..."
	$(GOLINT) run

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	$(DOCKER_BUILD) -t $(DOCKER_IMAGE):$(DOCKER_TAG) -f example/Dockerfile .

# Run Docker container
docker-run: docker-build
	@echo "Running Docker container..."
	$(DOCKER_RUN) --rm \
		-v $(PWD)/$(CONFIG_FILE):/$(CONFIG_FILE) \
		--name $(BINARY_NAME) \
		$(DOCKER_IMAGE):$(DOCKER_TAG) -c /$(CONFIG_FILE)

# Run Docker container in detached mode
docker-run-detached: docker-build
	@echo "Running Docker container in detached mode..."
	$(DOCKER_RUN) -d \
		-v $(PWD)/$(CONFIG_FILE):/$(CONFIG_FILE) \
		--name $(BINARY_NAME) \
		$(DOCKER_IMAGE):$(DOCKER_TAG) -c /$(CONFIG_FILE)

# Stop and remove Docker container
docker-clean:
	@echo "Cleaning Docker container..."
	$(DOCKER_RM) $(BINARY_NAME) || true

# Run the application locally (requires sudo for PID 1)
run-local: build
	@echo "Running $(BINARY_NAME) locally..."
	sudo ./$(BINARY_NAME) -c $(CONFIG_FILE)

# Run the application in development mode (without PID 1 requirement)
run-dev: build
	@echo "Running $(BINARY_NAME) in development mode..."
	./$(BINARY_NAME) -c $(CONFIG_FILE)

# Show help
help:
	@echo "Available targets:"
	@echo "  all              - Clean, format, lint, test, and build"
	@echo "  build            - Build the application"
	@echo "  clean            - Clean build files"
	@echo "  test             - Run tests"
	@echo "  test-coverage    - Run tests with coverage"
	@echo "  fmt              - Format code"
	@echo "  lint             - Run linter"
	@echo "  docker-build     - Build Docker image"
	@echo "  docker-run       - Build and run Docker container"
	@echo "  docker-run-detached - Build and run Docker container in detached mode"
	@echo "  docker-clean     - Stop and remove Docker container"
	@echo "  run-local        - Run the application locally (requires sudo)"
	@echo "  run-dev          - Run the application in development mode"
	@echo "  help             - Show this help message"
