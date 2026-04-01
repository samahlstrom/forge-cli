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
	git push --follow-tags
	@echo ""
	@echo "Waiting for GitHub release workflow..."
	@TAG=$$(git describe --tags --exact-match HEAD 2>/dev/null) && \
	echo "Tag: $$TAG" && \
	gh run list --workflow=auto-release.yml --limit=1 --json status,conclusion,databaseId -q '.[0]' && \
	echo "Watching release run..." && \
	RUN_ID="" && \
	for i in 1 2 3 4 5 6 7 8 9 10; do \
		RUN_ID=$$(gh run list --workflow=auto-release.yml --limit=1 --json databaseId -q '.[0].databaseId') && \
		if [ -n "$$RUN_ID" ]; then break; fi; \
		sleep 2; \
	done && \
	if [ -n "$$RUN_ID" ]; then \
		gh run watch $$RUN_ID && \
		echo "" && \
		echo "Upgrading forge via Homebrew..." && \
		brew upgrade samahlstrom/tap/forge && \
		echo "" && \
		forge version; \
	else \
		echo "Could not find workflow run. Check: gh run list --workflow=auto-release.yml"; \
	fi
