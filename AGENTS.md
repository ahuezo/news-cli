# Repository Guidelines

## Project Structure & Module Organization
`main.go` is the CLI entrypoint. Command definitions live in `cmd/`, with `cmd/root.go` wiring Cobra subcommands such as `fetch`, `list`, `search`, and `credentials`. Core application logic is under `internal/`: `internal/crawler/` handles RSS ingestion, `internal/db/` manages SQLite access, `internal/config/` stores credential handling, and `internal/models/` defines shared query and article types. The compiled local binary is `./telecom-news`; avoid committing rebuilt binaries unless the release process requires it.

## Build, Test, and Development Commands
Use Go 1.22+ and ensure GCC is available because `github.com/mattn/go-sqlite3` uses CGO.

- `go build -o telecom-news .` builds the CLI binary used in the README examples.
- `go run . list --limit 5` runs the app without creating a persistent build artifact.
- `go test ./...` runs all package tests; currently this is mainly a regression check because the repository has no committed `_test.go` files yet.
- `go mod tidy` syncs dependencies after adding or removing imports.

## Coding Style & Naming Conventions
Follow standard Go formatting and keep files `gofmt`-clean. Use tabs for indentation, exported identifiers in `CamelCase`, unexported identifiers in `mixedCase`, and package names in short lowercase form (`crawler`, `models`, `config`). Keep Cobra command help text concrete and example-driven, matching existing patterns like `telecom-news fetch "Light Reading"` and flag names such as `--sort-by` or `--db`.

## Testing Guidelines
Place tests next to the code they cover using `_test.go` suffixes and Go’s `testing` package. Prefer table-driven tests for filters, date parsing, source matching, and SQLite query behavior. Before opening a PR, run `go test ./...` and, for command changes, sanity-check the affected flow with `go run . <command>`.

## Commit & Pull Request Guidelines
The current history starts with a concise, imperative commit (`Initial commit`). Keep that style: short subject lines such as `Add source validation command` or `Fix list date filtering`. PRs should explain the user-facing change, note any schema or credential-handling impact, and include example CLI output when command behavior changes.

## Security & Configuration Tips
Do not commit local databases or credential files. By default the app uses `~/.telecom-news.db` and `~/.telecom-news-creds.json`; override them with `--db` and `--creds` when testing isolated scenarios.
