.PHONY: setup build install release clean test vet

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.2.0")
INSTALL_PATH ?= /opt/homebrew/bin/forge

setup:
	git config core.hooksPath .githooks
	@echo "Git hooks configured."

build:
	go build -ldflags "-X github.com/samahlstrom/forge-cli/internal/static.Version=$(VERSION)" -o bin/forge .

install:
	go build -ldflags "-X github.com/samahlstrom/forge-cli/internal/static.Version=$(VERSION)" -o $(INSTALL_PATH) .
	@echo "Installed forge $(VERSION) → $(INSTALL_PATH)"

release:
	git tag -a v$(VERSION) -m "v$(VERSION)" 2>/dev/null || true
	git push origin v$(VERSION) 2>/dev/null || true
	goreleaser release --clean

clean:
	rm -f bin/forge bin/forge-go

test:
	go test ./...

vet:
	go vet ./...
