.PHONY: build test check fmt vet clean docs

BINARY_NAME=milanuncios

build:
	go build -ldflags "-X main.version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/$(BINARY_NAME) ./cmd/milanuncios

docs:
	go run ./cmd/docsgen

test:
	go test ./... -count=1

fmt:
	gofmt -w .

vet:
	go vet ./...

check: vet test
	test -z "$$(gofmt -l .)"

clean:
	go clean

