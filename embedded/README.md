# Act 3 - embedded MCP in a Go application

Six programs, one module, one agent loop. Every program embeds the StackQL MCP server via [`stackql-mcp-go`](https://pkg.go.dev/github.com/stackql/stackql-mcp-go/embed) and gets a connected [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) client back. The suffix says how the server arrives: `-sidecar` programs download, sha256-verify and cache it on first run; `-vendored` programs carry it inside the executable (`go generate` fetches it once, `go:embed` compiles it in) and extract it on first run. Everything else is the same shape, on purpose.

| Program | Server | Model | Task |
|---|---|---|---|
| `minimal-sidecar/` | sidecar | none | the smallest embedding: `StartServer`, list tools, one SELECT over github with no credentials, one refused INSERT |
| `minimal-vendored/` | vendored | none | the same program with `Options.Binary` set: the only change |
| `audit-agent-sidecar/` | sidecar | Claude (`ANTHROPIC_API_KEY`, `AUDIT_AGENT_MODEL`) | the golden-path audit of a GitHub org and repo: licenses, topics, labels, branch protection; github via `null_auth`, so zero cloud credentials |
| `sre-agent-sidecar/` | sidecar | Claude (`ANTHROPIC_API_KEY`, `SRE_AGENT_MODEL`) | the morning assurance sweep over the service footprint in AWS and Cloudflare: health, exposure, edge, governance |
| `finops-agent-vendored/` | vendored | Claude (`ANTHROPIC_API_KEY`, `FINOPS_AGENT_MODEL`) | the month-to-date FinOps report: spend by service from Cost Explorer, tagging gaps, waste |
| `sandboxctl-vendored/` | vendored | Claude (`ANTHROPIC_API_KEY`, `ANTHROPIC_MODEL`) | the reference app: an infrastructure concierge on Google Cloud that escalates modes behind a human approval gate |

Each agent is one `main.go` and two prompt files, `prompts/system.md` (persona and StackQL context) and `prompts/task.md` (the job), compiled in with `go:embed` and rendered with `{{ NAME }}` values from the environment. The three agents run the server in `ModeReadOnly`: they investigate and report, they cannot change anything, whatever the prompt. Models default to `claude-sonnet-5`; a model name is a string, so a different one is an env var or `--model`, not a rebuild.

## Run

```sh
set -a; . ./.env; set +a          # ANTHROPIC_API_KEY, AWS_*, CLOUDFLARE_*, DEMO_*, GOOGLE_CREDENTIALS
go generate ./embedded/minimal-vendored ./embedded/finops-agent-vendored ./embedded/sandboxctl-vendored   # the vendored programs, once per machine (network at build time)
go build ./...

go run ./embedded/minimal-sidecar
go run ./embedded/minimal-vendored

go run ./embedded/audit-agent-sidecar --check          # server, provider, tools; no model call
go run ./embedded/audit-agent-sidecar                  # the audit (prompts/task.md)
go run ./embedded/audit-agent-sidecar "which stackql org repos have no license? show the SQL"
go run ./embedded/audit-agent-sidecar --repl           # questions in turn, history kept

go run ./embedded/sre-agent-sidecar --check
go run ./embedded/sre-agent-sidecar                    # the sweep
go run ./embedded/sre-agent-sidecar "Which security groups in ap-southeast-2 allow port 22 from anywhere?"

go run ./embedded/finops-agent-vendored --check
go run ./embedded/finops-agent-vendored                # the report
go run ./embedded/finops-agent-vendored "What did EC2 cost us last month, by usage type?"

go run ./embedded/sandboxctl-vendored tools
go run ./embedded/sandboxctl-vendored request "a small linux vm and a bucket in sydney for 2 days" --project my-project
go run ./embedded/sandboxctl-vendored reap --project my-project
```

Every SQL statement an agent runs is echoed on stderr as `[run_select_query]` followed by the statement, while the answer goes to stdout, so the room sees the evidence and the report stays pipeable. `--max-turns` bounds the tool-call rounds (default 40).

## The wiring

The whole integration is a handful of lines. The module hands back a connected go-sdk session; the loop in `internal/agentloop` converts its tools to Anthropic tool definitions and relays every `tool_use` block back to it.

```go
server, err := stackqlmcp.StartServer(ctx, stackqlmcp.Options{Mode: stackqlmcp.ModeReadOnly})   // sidecar
agentloop.Pull(ctx, server, "aws", "cloudflare")                    // providers into the local registry cache
session, err := agentloop.New(ctx, server, "claude-sonnet-5")       // MCP tools -> anthropic.ToolParam
task, err := agentloop.Render(taskPrompt, os.Getenv)                // {{ NAME }} from the environment
answer, err := session.Ask(ctx, systemPrompt, task, 40)             // the loop: Messages.New, tool_use -> CallTool
```

The vendored agent differs in one field, `Binary: StackqlMCPBinary()`, and a `//go:generate` directive that produces the function. No build script: `go:embed` is a language feature.

## Sidecar vs vendored

| | Sidecar (`Options.Binary` unset) | Vendored (`go generate` + `go:embed`) |
|---|---|---|
| How the server arrives | downloaded at first run, for the release pinned in the module | fetched by `go generate` at build time (`stackql-mcp-fetch` downloads the `.mcpb`, verifies the sha256 pin, extracts the server binary, writes the glue), compiled in with `go:embed` |
| Integrity | sha256 checked against the pins shipped in the module (`platforms.json`) | sha256 recorded at generate time, re-verified before the binary runs |
| First run | download, verify, extract to `~/.stackql/mcp-server-bin/<version>/<platform>/`, launch | extract from the executable into the same cache, launch |
| Network at run time | first run of each release only | never |
| Artefact size | the app | app + the server binary (about 100 MB linux-x64, 130 MB windows-x64) |
| Choose when | many apps on one host, one cached server; easy server updates | air-gapped hosts, single-file distribution, demos on conference wifi |

`stackql-mcp-fetch -platform all` generates one file per supported platform behind build tags, so a cross-compiled release carries the right server for each target. The generated files and binaries are gitignored; CI regenerates them before building. Overrides that work in both: `STACKQL_MCP_BIN` (run a stackql you already have), `STACKQL_MCP_BUNDLE` (extract a local `.mcpb`), `Options.CacheDir`, `Options.AppRoot`.

## Safety modes

`ModeReadOnly` (default) -> `ModeSafe` -> `ModeDeleteSafe` -> `ModeFullAccess`. The mode is enforced inside the server: in `read_only` a `run_mutation_query` is refused whatever the model asks; in `safe` INSERT and UPDATE run and DELETE is refused; in `delete_safe` DELETE runs too. Escalation is a caller opt-in via `Options.Mode`. The three agents stay in `read_only`. `sandboxctl-vendored` is the write path: it plans and prices on a `read_only` server, prints the plan and the cost, and only after a typed `y` starts a second server in `safe` to provision; `reap` finds expired sandboxes in `read_only` and tears them down in `delete_safe`. Mode escalation as a workflow, with the human between phases.

## Prompts

The prompts are the product. `system.md` is the same shape in every agent: who the agent is, that the session is read-only, the MCP tools it has, how to pick the resource before writing SQL (discovery in two calls, flat one-row-per-item resources over JSON blobs, a tool-call budget, change approach rather than retry variants), how to write StackQL that lands on the first try (key columns in WHERE, plain-column filters, booleans as 0/1, JSON columns, the json_each CTE rule, fan-out, ORDER BY with LIMIT, the CTE for a third join), and how to report without narrating the debugging. `task.md` is the job, numbered, naming the resources to read so the agent spends its tool calls on evidence rather than archaeology. Edit either file and rebuild; nothing else changes.
