# OVN Metrics Collector Makefile

# Variables
BINARY_NAME=ovn-metrics-collector
VERSION?=latest
DOCKER_IMAGE=ovn-metrics-collector
DOCKER_TAG=$(DOCKER_IMAGE):$(VERSION)

# Build targets
.PHONY: all build clean test docker-build docker-push help

all: build

build:
	@echo "Building $(BINARY_NAME)..."
	go mod tidy
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o $(BINARY_NAME) .
	@echo "Build complete: $(BINARY_NAME)"

build-cross:
	@echo "Building for multiple architectures..."
	GOOS=linux GOARCH=amd64 go build -o $(BINARY_NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -o $(BINARY_NAME)-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build -o $(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -o $(BINARY_NAME)-darwin-arm64 .

test:
	@echo "Running tests..."
	go test -v ./...

clean:
	@echo "Cleaning up..."
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_NAME)-*
	docker rmi $(DOCKER_TAG) 2>/dev/null || true

docker-build:
	@echo "Building Docker image $(DOCKER_TAG)..."
	docker build -t $(DOCKER_TAG) .

docker-push: docker-build
	@echo "Pushing Docker image $(DOCKER_TAG)..."
	docker push $(DOCKER_TAG)

run:
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME)

run-dev:
	@echo "Running $(BINARY_NAME) in development mode..."
	./$(BINARY_NAME) \
		--loglevel=4 \
		--enable-pprof=true \
		--metrics-bind-address="0.0.0.0:9476" \
		--ovn-metrics-bind-address="0.0.0.0:9310"

install: build
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	sudo cp $(BINARY_NAME) /usr/local/bin/

uninstall:
	@echo "Removing $(BINARY_NAME) from /usr/local/bin..."
	sudo rm -f /usr/local/bin/$(BINARY_NAME)

deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod verify

help:
	@echo "Available targets:"
	@echo "  build        - Build the binary"
	@echo "  build-cross  - Build for multiple architectures"
	@echo "  test         - Run tests"
	@echo "  clean        - Clean build artifacts"
	@echo "  docker-build - Build Docker image"
	@echo "  docker-push  - Build and push Docker image"
	@echo "  run          - Run the binary"
	@echo "  run-dev      - Run with development settings"
	@echo "  install      - Install to /usr/local/bin"
	@echo "  uninstall    - Remove from /usr/local/bin"
	@echo "  deps         - Download and verify dependencies"
	@echo "  help         - Show this help" 