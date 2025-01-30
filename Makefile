IMG ?= ghcr.io/ravichandra-eluri/store-forward-otel:latest

.PHONY: all build test lint docker-build docker-push run

all: build

build:
	go build -o bin/agent ./cmd/agent

test:
	go test ./... -v -coverprofile cover.out

lint:
	golangci-lint run ./...

docker-build:
	docker build -t $(IMG) .

docker-push:
	docker push $(IMG)

run:
	go run ./cmd/agent/main.go \
		--listen=:4317 \
		--endpoint=localhost:14317 \
		--buffer-dir=/tmp/saf-buffer \
		--retry-interval=10s
