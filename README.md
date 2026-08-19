# Go-native agentic cloud operations with embedded MCP

Working demonstrations of building agentic applications in Go on an embedded [StackQL](https://stackql.io) MCP server.

The idea in one paragraph: StackQL treats cloud and SaaS providers as data sources accessed via SQL. Agents doing platform engineering, SRE and audit work need to query, reason about and act on actual running state, not on state files. The StackQL MCP server is the agent interface to that engine, with a small fixed tool surface and safety modes gating writes. The [`stackql-mcp-go`](https://pkg.go.dev/github.com/stackql/stackql-mcp-go/embed) module embeds that server in a Go application - sidecar (downloaded and sha256-verified on first run) or vendored straight into the binary with `go:generate` + `go:embed` - and hands you a connected [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) client. Wire those tools into a Claude agent loop ([anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go)) and every action the agent takes is a readable SQL statement.

## The programs

| Program | What it shows | Needs |
|---|---|---|
| [cmd/minimal](cmd/minimal/) | The smallest embedding: `StartServer`, `ModeReadOnly`, github `null_auth`, list tools, one `run_select_query`, one refused `run_mutation_query`. | Go 1.25+; no credentials |
| [cmd/vendored](cmd/vendored/) | The same program with the server compiled in: `go generate` fetches the pin-verified bundle once, `go:embed` carries it - one self-contained binary, no network at run time. | Go 1.25+; no credentials |
| [cmd/copilot](cmd/copilot/) | The agent. MCP tools -> a Claude agent loop. Ask questions about your estate in English; every answer shows the SQL it ran (tool-call trace on stderr). `--check` preflights without a model call; `-p` is one-shot; otherwise REPL. | `ANTHROPIC_API_KEY` |
| [cmd/sandboxctl](cmd/sandboxctl/) | The reference app: an on-demand infrastructure concierge. Plans and prices in `read_only`, provisions in `safe` behind an explicit approval gate, labels everything with an expiry, and `reap` tears expired sandboxes down in `delete_safe`. Mode escalation as a workflow. | `ANTHROPIC_API_KEY` + `GOOGLE_CREDENTIALS` (`tools` subcommand needs neither) |

## Quick start

```sh
git clone https://github.com/stackql/golang-embedded-mcp-with-stackql
cd golang-embedded-mcp-with-stackql

go run ./cmd/minimal                        # smallest embedding, zero credentials
go run ./cmd/copilot --check                # agent preflight, zero credentials
go run ./cmd/sandboxctl tools               # reference app's tool listing, zero credentials

go generate ./cmd/vendored                  # fetch + verify the bundle (build time, once)
go run ./cmd/vendored                       # then no network at run time

export ANTHROPIC_API_KEY=sk-ant-...         # then the agent for real:
go run ./cmd/copilot -p "which stackql org repos have no license? show the SQL"
go run ./cmd/copilot                        # REPL

# the full concierge (also needs GOOGLE_CREDENTIALS):
go run ./cmd/sandboxctl request "a small linux vm and a bucket in sydney for 2 days" --project my-project
go run ./cmd/sandboxctl reap --project my-project
```

First run of a sidecar program downloads the platform's server bundle from `releases.stackql.io`, verifies it against the sha256 pins shipped in the module, and caches the binary under `~/.stackql/mcp-server-bin/<version>/<platform>/`. Later runs are offline.

## Why this shape

- The agent's tools are the StackQL MCP tools, so what the agent decides to do is always a readable SQL statement. The tool-call trace is the audit trail.
- Safety is a server-side contract: modes gate writes however the model is prompted. `copilot` stays `read_only`; `sandboxctl` escalates deliberately - plan in `read_only`, provision in `safe`, tear down in `delete_safe` - with a human approval between phases.
- The github provider's `null_auth` mode means `minimal`, `vendored`, `copilot --check` and `sandboxctl tools` run with zero credentials. Real provider credentials in the environment unlock the same programs against AWS/Azure/Google/Databricks.
- Vendored (`go:generate` + `go:embed`) is the single-binary story: `sandboxctl`-style tools ship as one file that starts with no network.

## Versioning

`stackql-mcp-go` is version-locked to the stackql release it embeds; the module tag is `v<stackql version>` (this repo pins `v0.10.605`; pin the minor and a server bump is a normal `go get -u`). Module source lives in [stackql/stackql `packaging/mcpb/go`](https://github.com/stackql/stackql/tree/main/packaging/mcpb/go) and is published through the [stackql/stackql-mcp-go](https://github.com/stackql/stackql-mcp-go) mirror.

## Model

The agents default to `claude-sonnet-5`; override with `--model`, `COPILOT_MODEL` (copilot) or `ANTHROPIC_MODEL` (sandboxctl).

## CI

[.github/workflows/ci.yml](.github/workflows/ci.yml) builds everything, runs the zero-credential smokes (`minimal`, vendored generate+run, `copilot --check`, `sandboxctl tools`) on every push and PR. When the `ANTHROPIC_API_KEY` repo secret is configured, an `agent-live` job additionally runs one real one-shot `copilot` prompt.

## References

- Module and docs: [pkg.go.dev/github.com/stackql/stackql-mcp-go/embed](https://pkg.go.dev/github.com/stackql/stackql-mcp-go/embed); source in [stackql/stackql `packaging/mcpb/go`](https://github.com/stackql/stackql/tree/main/packaging/mcpb/go)
- StackQL MCP server: [stackql.io/docs/mcp](https://stackql.io/docs/mcp)
- Server bundles: built by [stackql/stackql `packaging/mcpb`](https://github.com/stackql/stackql/tree/main/packaging/mcpb), published on [stackql/stackql releases](https://github.com/stackql/stackql/releases)
- Siblings: [rust-embedded-mcp-with-stackql](https://github.com/stackql/rust-embedded-mcp-with-stackql), [dotnet-embedded-mcp-with-stackql](https://github.com/stackql/dotnet-embedded-mcp-with-stackql)

MIT licensed.
