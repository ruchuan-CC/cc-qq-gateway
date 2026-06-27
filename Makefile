BINARY := cc-qq-gateway
PKG := ./cmd/cc-qq-gateway

.PHONY: all build run test vet fmt tidy clean

all: build

build:
	go build -o bin/$(BINARY) $(PKG)

run:
	go run $(PKG) -config config.toml

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

clean:
	rm -rf bin
