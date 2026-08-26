BINARY  := server
PKG     := ./...
IMAGE   := dyson-sphere/service
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w \
	-X gitlab.com/navetoocool/dyson-sphere/internal/build.Version=$(VERSION) \
	-X gitlab.com/navetoocool/dyson-sphere/internal/build.Commit=$(COMMIT)

.PHONY: run build test lint tidy clean docker-build docker-run

run:
	go run -ldflags="$(LDFLAGS)" ./cmd/server

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY) ./cmd/server

test:
	go test -race -cover $(PKG)

lint:
	go vet $(PKG)
	@command -v golangci-lint >/dev/null 2>&1 \
		&& golangci-lint run \
		|| echo "golangci-lint not installed (brew install golangci-lint) -- ran go vet only"

tidy:
	go mod tidy

clean:
	rm -rf bin

docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

docker-run: docker-build
	docker run --rm -p 8080:8080 \
		-e OTEL_EXPORTER_OTLP_ENDPOINT=$(OTEL_EXPORTER_OTLP_ENDPOINT) \
		-e OTEL_SERVICE_NAME=dyson-sphere-service \
		$(IMAGE):$(VERSION)
