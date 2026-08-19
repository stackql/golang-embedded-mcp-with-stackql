# CLAUDE.md

## What this repo is

The long-lived Go demo repo for embedded StackQL MCP: working demonstrations of building agentic applications in Go on the published `github.com/stackql/stackql-mcp-go` module. One module (`go 1.25`), four programs:

- `cmd/minimal` - the smallest embedding: `StartServer`, `ModeReadOnly`, github `null_auth`, list tools, one `run_select_query`, one refused `run_mutation_query`. Sidecar acquisition, zero credentials.
- `cmd/vendored` - the same program with the server compiled in: `go generate ./cmd/vendored` runs `stackql-mcp-fetch` (downloads and pin-verifies the bundle, writes the `go:embed` glue), then the binary is self-contained with no network at run time. The fetched binary and `stackql_mcp_*_gen.go` are gitignored - always `go generate` before building this program.
- `cmd/copilot` - the agent: MCP tools -> a Claude agent loop (`anthropic-sdk-go`). `--check` preflights without a model call; `-p "..."` is one-shot; no args is a REPL. Needs `ANTHROPIC_API_KEY`.
- `cmd/sandboxctl` - the reference app: an on-demand infrastructure concierge. Plans and prices in `read_only`, provisions in `safe` behind an explicit human approval gate, labels everything with an expiry, `reap` tears down in `delete_safe`. Mode escalation as a workflow. `request`/`reap` need `ANTHROPIC_API_KEY` + `GOOGLE_CREDENTIALS`; `tools` needs neither. Originally shipped alongside the module in its pre-monorepo repo.

`internal/agentloop` is the shared Claude agent loop (MCP tool conversion, tool relay, persistent history, SQL trace to stderr) used by copilot and sandboxctl - change it once, both benefit.

It is not a library and not a product; the module it demonstrates lives in stackql/stackql under `packaging/mcpb/go` and is published through the `stackql/stackql-mcp-go` mirror repo (tag `v<stackql version>`).

## Versioning contract

The module version equals the stackql release it embeds; `go.mod` pins `v0.10.605` and a server bump is a normal `go get github.com/stackql/stackql-mcp-go@v<new>`. Sidecar first run downloads the bundle from `releases.stackql.io`, verifies it against the pins shipped in the module, and caches the binary under `~/.stackql/mcp-server-bin/<version>/<platform>/`; `STACKQL_MCP_BIN` and `STACKQL_MCP_BUNDLE` override acquisition. Never point go.mod at a git branch of the mirror - it is a publish artefact whose tree is overwritten each release.

## Build, run, test

```sh
gofmt -l . && go vet ./... && go build ./...
go run ./cmd/minimal                        # zero credentials
go run ./cmd/copilot --check                # zero credentials
go run ./cmd/sandboxctl tools               # zero credentials
go generate ./cmd/vendored && go run ./cmd/vendored
go run ./cmd/copilot -p "question"          # needs ANTHROPIC_API_KEY
```

Models default to `claude-sonnet-5` (`--model`, `COPILOT_MODEL` for copilot, `ANTHROPIC_MODEL` for sandboxctl).

## CI

`.github/workflows/ci.yml`, on every push to main and PR. `build-test`: gofmt, `go generate ./cmd/vendored` (network: fetches the pinned bundle), vet, build, then the four zero-credential smokes. `agent-live`: one real one-shot `copilot` prompt; the `ANTHROPIC_API_KEY` org secret is already configured, so pushes make one real (small) Claude call. No other secrets exist or are needed.

## Writing style (house rules)

- Australian spelling. Matter-of-fact prose; no hyperbole, no marketing adjectives.
- Hyphens only (`-`), never `--` as punctuation or em dashes. Arrows are `->`. QWERTY characters only.
- No stacked markdown headings (a heading immediately followed by another heading).

## References

- Module source: https://github.com/stackql/stackql/tree/main/packaging/mcpb/go (the packaging CLAUDE.md there documents the full wrapper contract); docs at https://pkg.go.dev/github.com/stackql/stackql-mcp-go/embed
- StackQL MCP docs: https://stackql.io/docs/mcp
- Siblings: rust-embedded-mcp-with-stackql, dotnet-embedded-mcp-with-stackql (same shape: minimal + agent + reference app, same CI pattern)
