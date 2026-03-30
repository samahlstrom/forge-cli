.PHONY: build install clean test vet

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.2.0")

build:
	go build -ldflags "-X github.com/samahlstrom/forge-cli/internal/static.Version=$(VERSION)" -o bin/forge .

install:
	go install -ldflags "-X github.com/samahlstrom/forge-cli/internal/static.Version=$(VERSION)" .

clean:
	rm -f bin/forge bin/forge-go

test:
	go test ./...

vet:
	go vet ./...
