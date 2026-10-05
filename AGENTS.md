# Working on this repository

mss is a Go CLI that indexes local coding-agent session transcripts and answers
`search` / `show` / `ctx` / `last` queries. One design rule sits behind every
change: **it runs only when a person or a script invokes it.** No daemon, no
background writes, no I/O on a path the user did not ask for.

## Build and test

- `make build` writes `./mss`; `make test` runs `go test ./...`.
- `go vet ./...` must stay clean and `gofmt -l .` empty.
- SQLite-backed stores (opencode, Cursor) are read through the `sqlite3` CLI;
  tests that need it skip when it is missing.

## Contracts that tests enforce

- `docs/registry/registry.json` must match the loader list in
  `internal/sources/registry.go`, every fixture path it names must exist, and
  every `docs/registry/*.md` page must be linked from `docs/registry/README.md`
  (`internal/sources/registry*_test.go`).
- `docs/ARCHITECTURE.md` carries one parser-table row per harness, and the
  count in the sentence above the table must match
  (`internal/sources/architecture_table_test.go`).
- The JSON emitted by search/show/last must stay in sync with
  `docs/json-output.md` (`internal/search/*json_contract_test.go`).
- `internal/index` tests pin ingest counts, dedup and cross-pass behavior;
  run `go test ./internal/...` before claiming a change is done.

## Style

- Comments explain why, not what. User-facing strings are lowercase sentences
  that name the exact command to run.
- Prefer deleting code to adding a flag. A behavior nobody asked for is a bug
  in this repository.
