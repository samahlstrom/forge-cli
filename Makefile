.PHONY: setup build clean test vet ship

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.2.0")

setup:
	git config core.hooksPath .githooks
	@echo "Git hooks configured."

build:
	go build -ldflags "-X github.com/samahlstrom/forge-cli/internal/static.Version=$(VERSION)" -o bin/forge .

clean:
	rm -f bin/forge bin/forge-go

test:
	go test ./...

vet:
	go vet ./...

# Push, wait for GitHub Action to release, then brew upgrade locally
ship:
	@echo "Pushing to origin (tags included)..."
	@git push --follow-tags
	@echo ""
	@TAG=$$(git describe --tags --exact-match HEAD 2>/dev/null) && \
	echo "Tag: $$TAG" && \
	echo "Waiting for release workflow to start..." && \
	sleep 5 && \
	RUN_ID=$$(gh run list --workflow=auto-release.yml --limit=1 --json databaseId -q '.[0].databaseId') && \
	echo "Watching run $$RUN_ID..." && \
	gh run watch $$RUN_ID --exit-status && \
	echo "" && \
	echo "Upgrading forge via Homebrew..." && \
	brew upgrade samahlstrom/tap/forge && \
	echo "" && \
	forge --version
