# Contributing to FolderMCP

Thank you for your interest in contributing! This guide covers what you
need to get started.

## Development Setup

| Requirement | Version |
|-------------|---------|
| Go          | 1.25+   |
| Python      | 3.10+   |
| uv          | latest  |

```bash
git clone https://github.com/gtm-k/foldermcp.git
cd foldermcp
make build        # compile binary to ./bin/foldermcp
make test         # run all tests with race detection
```

## Running Tests

```bash
make test           # full suite (go test -race -count=1 ./...)
make lint           # go vet ./...
```

Tests that need a Python interpreter are automatically skipped when
`python3`/`python` is not found in `PATH`.

## Adding a New Language Introspector

1. Create `internal/introspect/<lang>.go`.
2. Implement the `IntrospectorPlugin` interface:

```go
type IntrospectorPlugin interface {
    CanHandle(filePath string) bool
    ExtractTools(filePath string) ([]ToolMetadata, error)
    InferDependencies(filePath string) ([]Dependency, error)
}
```

3. Register the plugin in `NewRegistry()` inside `registry.go`.
4. Add test fixtures under `testdata/<lang>_simple/`.
5. Write tests in `internal/introspect/<lang>_test.go`.

## Code Style

- Format with `gofmt` (enforced by CI).
- Lint with `go vet` at minimum; `golangci-lint` is recommended.
- Keep exported symbols documented with Go doc comments.
- Python code should pass `ruff check`.

## Commit Messages

Use the conventional style:

```
feat: add shell introspector
fix: handle empty YAML config gracefully
docs: update quickstart example
```

## Pull Request Process

1. Fork the repository and create a feature branch from `main`.
2. Make your changes in small, focused commits.
3. Ensure `make test` and `make lint` pass locally.
4. Open a pull request against `main` with a clear description.
5. A maintainer will review and may request changes before merging.

## Reporting Issues

Open a GitHub Issue with:
- Steps to reproduce
- Expected vs. actual behaviour
- Output of `foldermcp doctor`
- Go and Python versions (`go version`, `python --version`)

## License

By contributing you agree that your contributions will be licensed under
the project's Apache License 2.0.
