BINARY := server
PKG    := ./...

.PHONY: run build test lint tidy clean

run:
	go run ./cmd/server

build:
	go build -o bin/$(BINARY) ./cmd/server

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
