# Makefile for Asterisk Service

ASTERISK_IMAGE_NAME ?= menta2l/asterisk-service
ASTERISK_IMAGE_TAG ?= latest
DOCKER_REGISTRY ?=
VERSION ?= 1.0.0

.PHONY: build-server
build-server:
	@echo "Building Asterisk server..."
	@go build -ldflags "-X main.version=$(VERSION) -s -w" -o ./bin/asterisk-server ./cmd/server

.PHONY: docker
docker:
	@echo "Building Docker image $(ASTERISK_IMAGE_NAME):$(ASTERISK_IMAGE_TAG)..."
	@docker build \
		-t $(ASTERISK_IMAGE_NAME):$(ASTERISK_IMAGE_TAG) \
		-t $(ASTERISK_IMAGE_NAME):latest \
		--build-arg APP_VERSION=$(VERSION) \
		-f ./Dockerfile \
		.

.PHONY: run-server
run-server:
	@go run ./cmd/server -c ./configs

.PHONY: wire
wire:
	@cd ./cmd/server && wire

.PHONY: proto
proto:
	@buf generate
	@buf build -o cmd/server/assets/descriptor.bin

.PHONY: tidy
tidy:
	@go mod tidy

.PHONY: test
test:
	@go test -race -v ./...

.PHONY: clean
clean:
	@rm -rf ./bin

.PHONY: generate
generate: proto wire tidy
	@echo "Generation complete!"
