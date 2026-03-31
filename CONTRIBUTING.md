# Contributing to forge-cli

Thanks for your interest in contributing! Here's how to get started.

## Before You Start

- **Bug reports and feature requests**: Open a GitHub Issue first. Describe the problem or idea clearly before writing any code.
- **Small fixes** (typos, docs): You can open a PR directly.
- **Larger changes**: Open an Issue and discuss the approach before investing time in implementation.

## Development Setup

**Requirements**: Go 1.22+, `golangci-lint`

```bash
git clone https://github.com/samahlstrom/forge-cli
cd forge-cli
go build ./...
go test ./...
```

To install a local dev build:

```bash
go install .
```

## Making Changes

1. Fork the repo and create a branch from `main`
2. Name your branch descriptively: `fix/upgrade-cleanup`, `feat/new-preset`
3. Keep changes focused — one logical change per PR
4. Write or update tests for any new behavior
5. Run the quality checks before opening a PR:

```bash
go vet ./...
golangci-lint run
go test ./...
```

## Pull Request Guidelines

- Write a clear description of **what** changed and **why**
- Reference any related Issues (e.g. `Closes #42`)
- PRs must pass CI (build, vet, tests) before they can be merged
- At least one maintainer review is required before merging to `main`
- Keep commits clean — squash fixup commits before asking for review

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet` clean)
- No `TODO` or `FIXME` comments in merged code — open an Issue instead
- No hardcoded credentials, secrets, or API keys of any kind

## Reporting Security Issues

Do **not** open a public Issue for security vulnerabilities. See [SECURITY.md](.github/SECURITY.md) for responsible disclosure instructions.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
