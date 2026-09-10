# Golang-native agentic platform engineering with embedded MCP

Demo code for a demonstration of Golang-native agentic platform engineering with embedded MCP.

[StackQL](https://github.com/stackql/stackql) treats cloud and SaaS providers as data sources accessed via SQL. Agents doing platform engineering, SRE and audit work need to query, reason about and act on actual running state, not on state files. The StackQL MCP server is the agent interface to that engine, with a small fixed tool surface and safety modes gating writes.

The [`stackql-mcp-go`](https://pkg.go.dev/github.com/stackql/stackql-mcp-go/embed) module embeds that server in a Go application, either as a sidecar (downloaded and sha256-verified at first run) or vendored straight into the Go application with `go generate` + `go:embed` (one self-contained binary, no network at run time), and hands you a connected [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) client. The worked examples are three small agents with the same four parts, model, prompts, context and MCP tools, on one shared [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) agent loop: a repository governance audit over GitHub (zero credentials, server as a sidecar), an SRE assurance sweep over a service's footprint in AWS and Cloudflare (sidecar), and a FinOps report from Cost Explorer (server vendored into the binary). Every action an agent takes is a readable SQL statement.

## Demo phases

| Phase | Directory | What it shows | Needs |
|---|---|---|---|
| 1 | [primer/](primer/) | StackQL itself: `stackql shell`, `stackql exec`, `stackql srv` with `psql` and a Go app over the Postgres wire protocol (pgx) | `stackql` on PATH; AWS + Cloudflare for most blocks, none for the GitHub ones and the Go app |
| 2 | [stacks/](stacks/) | `stackql-deploy`: a service's footprint across AWS and Cloudflare converged as data, a drift script, and a zero-credential GitHub variant | `stackql-deploy` on PATH; AWS + Cloudflare (or a GitHub token) |
| 3 | [embedded/](embedded/) | Embedded MCP in Go: the smallest sidecar and vendored embeddings, three agents, and the reference app | Go 1.25+; `ANTHROPIC_API_KEY` for the agents |

Phase 3, the programs (the suffix is how the server arrives):

| Program | Server | What it does | Needs |
|---|---|---|---|
| `minimal-sidecar` | sidecar | `StartServer`, `ModeReadOnly`, github `null_auth`, list tools, one `run_select_query`, one refused `run_mutation_query` | nothing |
| `minimal-vendored` | vendored | the same program with the server compiled in: `go generate` once, then no network at run time | nothing |
| `audit-agent-sidecar` | sidecar | golden-path audit of a GitHub org and repo: licenses, topics, labels, branch protection; `--repl` for questions in turn | `ANTHROPIC_API_KEY` |
| `sre-agent-sidecar` | sidecar | the morning assurance sweep over the service footprint: health, exposure, edge, governance | `ANTHROPIC_API_KEY`, `AWS_*`, `CLOUDFLARE_*`, `DEMO_*` |
| `finops-agent-vendored` | vendored | the month-to-date FinOps report: spend by service from Cost Explorer, tagging gaps, waste | `ANTHROPIC_API_KEY`, `AWS_*` |
| `sandboxctl-vendored` | vendored | the reference app: plans and prices in `read_only`, provisions in `safe` behind an approval gate, `reap` tears down in `delete_safe` | `ANTHROPIC_API_KEY`, `GOOGLE_CREDENTIALS` (`tools` needs neither) |

## Demo Runbook

### Prep

```sh
# cp .env.example .env            # fill in AWS_*, CLOUDFLARE_*, DEMO_*, ANTHROPIC_API_KEY
set -a; . ./.env; set +a
for p in aws awscc cloudflare github; do stackql exec "REGISTRY PULL $p"; done
go generate ./embedded/minimal-vendored ./embedded/finops-agent-vendored ./embedded/sandboxctl-vendored   # network: fetches the pinned bundle
go build ./...
go run ./embedded/minimal-sidecar                 # caches the sidecar server bundle (v0.11.669)
go run ./embedded/sre-agent-sidecar --check
go run ./embedded/finops-agent-vendored --check
stackql-deploy build stacks/service-footprint dev --env-file .env    # the footprint the shell join and the SRE sweep read; about 30 s
```

### `stackql` Usage

`shell` examples:

```sh
stackql shell                    # paste 1.2 (discovery) and 1.13 (the cross-provider JOIN) from primer/shell.iql; 1.12 if there is time
```

`exec` examples:

```sh
stackql exec --output json "SELECT name, status, JSON_EXTRACT(plan, '$.name') AS plan FROM cloudflare.zones.zones" | jq .
stackql exec -i primer/queries/finops.iql --iqldata primer/queries/vars.jsonnet --var month=$(date +%Y-%m) --output csv
```

`srv` examples using the `stackql` engine over the Postgres wire protocol, from psql and from Go.

```sh
stackql srv --pgsrv.port 5466    # in a second terminal
psql -h localhost -p 5466 -U stackql -d stackql -c "SELECT instance_id, instance_type, JSON_EXTRACT(state, '$.name') AS state, public_ip_address AS ip FROM aws.ec2.instances WHERE region = '$AWS_REGION'"
# show primer/pgwire-go-app/main.go (pgx, simple query protocol, public GitHub data), then run it
go run ./primer/pgwire-go-app
```

### MCP/SRE Examples

Break something (slide THE AGENT LOOP): someone changed it in the console. Each statement is printed before it runs.

```sh
stacks/service-footprint/drift.sh tag ssh edge   # drops the owner tag, opens port 22 to the world, deletes the A record
```

The agents, one sidecar and one vendored (slides SIDECAR DETAILED, VENDORED DETAILED, THE DEMOS).

```sh
ls ~/.stackql/mcp-server-bin/0.11.669/            # the sidecar server, downloaded and sha256-verified at first run
go run ./embedded/sre-agent-sidecar               # the sweep: one PASS, three ATTENTION, fixing SQL shown, not run
go build -o /tmp/finops-agent-vendored ./embedded/finops-agent-vendored && ls -lh /tmp/finops-agent-vendored   # one file, engine inside
/tmp/finops-agent-vendored                        # month-to-date spend, tagging gaps, waste; no download at run time
```

### Revert or Teardown

```sh
stackql-deploy build stacks/service-footprint dev --env-file .env      # converge, if you want to run the sweep again
stackql-deploy teardown stacks/service-footprint dev --env-file .env
```

## Versioning

`stackql-mcp-go` is version-locked to the stackql release it embeds; the module tag is `v<stackql version>` (this repo pins `v0.11.669`; a server bump is a normal `go get github.com/stackql/stackql-mcp-go@v<new>`). Module source lives in [stackql/stackql `packaging/mcpb/go`](https://github.com/stackql/stackql/tree/main/packaging/mcpb/go) and is published through the [stackql/stackql-mcp-go](https://github.com/stackql/stackql-mcp-go) mirror.

## CI

[.github/workflows/ci.yml](.github/workflows/ci.yml) generates the vendored glue, builds everything and runs the zero-credential smokes (both minimal programs, the three `--check` preflights, `sandboxctl-vendored tools`) on every push and PR. When the `ANTHROPIC_API_KEY` secret is configured, an `agent-live` job additionally asks `audit-agent-sidecar` one question.

## References

- Module and docs: [pkg.go.dev/github.com/stackql/stackql-mcp-go/embed](https://pkg.go.dev/github.com/stackql/stackql-mcp-go/embed); source in [stackql/stackql `packaging/mcpb/go`](https://github.com/stackql/stackql/tree/main/packaging/mcpb/go)
- StackQL MCP server: [stackql.io/docs/mcp](https://stackql.io/docs/mcp)
- stackql-deploy: [stackql-deploy.io](https://stackql-deploy.io), [stackql/stackql-deploy-rs](https://github.com/stackql/stackql-deploy-rs)
- Server bundles: built by [stackql/stackql `packaging/mcpb`](https://github.com/stackql/stackql/tree/main/packaging/mcpb), published on [stackql/stackql releases](https://github.com/stackql/stackql/releases)
- Provider registry: [registry.stackql.app](https://registry.stackql.app)
- Siblings: [rust-embedded-mcp-with-stackql](https://github.com/stackql/rust-embedded-mcp-with-stackql), [dotnet-embedded-mcp-with-stackql](https://github.com/stackql/dotnet-embedded-mcp-with-stackql)

MIT licensed.
