# CLAUDE.md

## What this repo is

The long-lived Go demo repo for embedded StackQL MCP: working, repeatable demonstrations of building agentic applications in Go on the published `github.com/stackql/stackql-mcp-go` module. It holds the demo code and runbook for the talk "Golang-native agentic platform engineering with embedded MCP" (Jeff Aven, Golang user group, September 2026) and the reference app (`sandboxctl`) that shipped alongside the module before the module moved into stackql/stackql `packaging/mcpb/go`. It is not a library and not a product; the module it demonstrates lives in stackql/stackql under `packaging/mcpb/go` and is published through the `stackql/stackql-mcp-go` mirror repo (tag `v<stackql version>`).

The deck is mastered in Google Slides; `slides/` holds the pptx it started from (derived from the Rust deck in the sibling repo, `../rust-embedded-mcp-with-stackql`). When the deck and this file disagree, the deck wins and this file gets updated. When asked for slide content, produce paste-ready text, not slide files.

## The talk in one paragraph

StackQL treats cloud and SaaS providers as data sources accessed via SQL. Agents in the platform engineering / SRE / observability space need to query, reason about, and act on actual running state, not on state files. The StackQL MCP server is the agent interface to the StackQL engine, with a small fixed tool surface (16 tools) and safety modes gating writes. The `stackql-mcp-go` module embeds that MCP server in a Go application, either as a sidecar (the release pinned in the module, downloaded and sha256-verified at first run) or vendored straight into the Go application (`go generate` + `go:embed`, single self-contained binary). The concrete examples are three small agents with the same four parts, model, prompts, context and MCP tools, on one shared agent loop: `audit-agent-sidecar` (GitHub, zero credentials) audits a repo against the golden path; `sre-agent-sidecar` sweeps a service's footprint across AWS and Cloudflare; `finops-agent-vendored` reports month-to-date spend and waste from Cost Explorer. The audience is engineers who program in Go.

## Demo flow

Three acts, in this order, matching the deck and RUNBOOK.md. Each act is runnable on its own from a clean shell in the repo root with `.env` exported. Slot is 45 minutes including slides: roughly 40% on the primer side (act 1 + act 2 + the StackQL slides) and 60% on embedded MCP (the MCP and Go slides + act 3).

Act 1 - StackQL primer (slides "STACKQL >>", "Cloud Providers as Data Sources", "How to use StackQL"), about 7 minutes. `primer/` is paste blocks, not batch scripts: `shell.iql` into `stackql shell`, `exec.sh` and `srv.sh` one block at a time. `queries/` holds the `-i` and jsonnet examples. `pgwire-go-app/` is the Go reference app for the Application & Data Integration row: pgx over `stackql srv`, public GitHub data, zero credentials. See `primer/README.md` for the timing budget and the engine quirks each block works around.

Act 2 - stackql-deploy (slide "How to use StackQL", Platform Automation row), about 4 minutes. `stacks/service-footprint/` (aws + awscc + cloudflare) is the footprint the SRE sweep reads and `drift.sh` drifts; `stacks/aws-webserver/` backs the act 1 cross-provider query; `stacks/golden-path/` (github) is the zero-credential variant and what the audit agent audits against. Export `.env` as well as passing `--env-file`. See `stacks/README.md`.

Act 3 - embedded MCP in a Go application (slides "STACKQL MCP" through "THE DEMOS"), about 13 minutes. Six programs in `embedded/`; the suffix says how the server arrives:

- `minimal-sidecar` and `minimal-vendored` (slides "SIDECAR DETAILED", "VENDORED DETAILED"): the smallest embedding, `StartServer`, `ModeReadOnly`, github `null_auth`, list tools, one `run_select_query`, one refused `run_mutation_query`. The vendored one differs in `Options.Binary` and a `//go:generate` directive.
- `audit-agent-sidecar`, `sre-agent-sidecar`, `finops-agent-vendored` (slide "THE AGENT LOOP"): one `main.go` plus `prompts/system.md` (persona and StackQL context) and `prompts/task.md` (the job), compiled in with `go:embed` and rendered with `{{ NAME }}` from the environment (`{{ TODAY }}` from the clock in finops). `--check` preflights without a model call; a positional argument replaces the task; `--repl` takes questions in turn. All three run the server in `ModeReadOnly`. Models default to `claude-sonnet-5` (`<AGENT>_MODEL`, `--model`).
- `sandboxctl-vendored`: the reference app. Plans and prices in `read_only`, provisions in `safe` behind a typed approval, labels everything with an expiry, `reap` tears down in `delete_safe`. Mode escalation as a workflow. Needs `ANTHROPIC_API_KEY` + `GOOGLE_CREDENTIALS`; `tools` needs neither.

`embedded/internal/agentloop` is the shared Claude agent loop (MCP tool conversion, tool relay, persistent history, SQL trace to stderr, `Pull` for providers, `Render` for prompts) used by every agent - change it once, all benefit.

Safety modes are a server-side contract, verified against the pinned binary: `read_only` refuses every write; `safe` asks the client for approval before every mutation and `delete_safe` before every DELETE, over MCP elicitation, and refuses a client that cannot answer; `full_access` asks nothing. `StartServer` builds a go-sdk client with no elicitation handler, so a write phase needs its own client: `sandboxctl-vendored` uses `stackqlmcp.CommandLine` (same acquisition and launch arguments) with `mcp.ClientOptions{ElicitationHandler: ...}` and answers at the terminal (`--approve` pre-accepts). The Go module README's mode table is the short form; the elicitation behaviour is the one that bites.

## Versioning contract

The module version equals the stackql release it embeds; `go.mod` pins `v0.11.669` and a server bump is a normal `go get github.com/stackql/stackql-mcp-go@v<new>`. Sidecar first run downloads the bundle from `releases.stackql.io`, verifies it against the pins shipped in the module, and caches the binary under `~/.stackql/mcp-server-bin/<version>/<platform>/`; vendored programs extract into the same cache. `STACKQL_MCP_BIN` and `STACKQL_MCP_BUNDLE` override acquisition. Never point go.mod at a git branch of the mirror - it is a publish artefact whose tree is overwritten each release.

## Build, run, test

```sh
go generate ./embedded/minimal-vendored ./embedded/finops-agent-vendored ./embedded/sandboxctl-vendored   # before anything compiles; network
gofmt -l . && go vet ./... && go build ./...
go run ./embedded/minimal-sidecar                 # zero credentials
go run ./embedded/minimal-vendored                # zero credentials, no network
go run ./embedded/audit-agent-sidecar --check     # zero credentials (also sre-agent-sidecar, finops-agent-vendored)
go run ./embedded/sandboxctl-vendored tools       # zero credentials
go run ./embedded/audit-agent-sidecar "question"  # needs ANTHROPIC_API_KEY
```

The fetched binaries (`embedded/*/stackql-mcp-*`) and generated glue (`embedded/*/stackql_mcp_*_gen.go`) are gitignored and platform-specific: each machine and each CI runner generates its own. A stale linux glue file on a Windows checkout is build-tagged out, so the symptom of a missing `go generate` is `undefined: StackqlMCPBinary`.

## Repo layout

```
golang-embedded-mcp-with-stackql/
  CLAUDE.md
  README.md                  the talk, the three acts, the programs
  RUNBOOK.md                 exact commands in demo order, copy/paste-able
  .env.example               every variable, all optional
  go.mod                     one module at the root: github.com/stackql/stackql-mcp-go, go-sdk, anthropic-sdk-go, pgx
  slides/                    the pptx the Google Slides deck started from
  primer/                    act 1: shell.iql, exec.sh, srv.sh, queries/, pgwire-go-app/, README.md
  stacks/                    act 2: service-footprint/ (+ drift.sh), aws-webserver/, golden-path/, README.md
  embedded/                  act 3
    internal/agentloop/      the shared loop
    minimal-sidecar/         main.go
    minimal-vendored/        main.go (+ generated glue, gitignored)
    audit-agent-sidecar/     main.go, prompts/system.md, prompts/task.md
    sre-agent-sidecar/       main.go, prompts/system.md, prompts/task.md
    finops-agent-vendored/   main.go, prompts/system.md, prompts/task.md (+ generated glue)
    sandboxctl-vendored/     main.go, prompts.go (+ generated glue)
    README.md                the wiring, sidecar vs vendored, safety modes, prompts
  .github/workflows/ci.yml   gofmt, go generate, vet, build, zero-credential smokes, agent-live
```

## CI

`.github/workflows/ci.yml`, on every push to main and PR. `build-test`: gofmt, `go generate` for the three vendored programs (network: fetches the pinned bundle), vet, build, then the zero-credential smokes: both minimal programs, the three `--check` preflights (the SRE and FinOps ones pull `aws` and `cloudflare` from the registry, no credentials), `sandboxctl-vendored tools`. `agent-live`: one question to `audit-agent-sidecar`; the `ANTHROPIC_API_KEY` org secret is already configured, so pushes make one real (small) Claude call. No other secrets exist or are needed.

## Writing style (house rules)

- Australian spelling. Matter-of-fact prose; no hyperbole, no marketing adjectives.
- Hyphens only (`-`), never `--` as punctuation or em dashes. Arrows are `->`. QWERTY characters only.
- No stacked markdown headings (a heading immediately followed by another heading).
- Say "cloud as data sources accessed via SQL", not "cloud as SQL tables". Say "vendored straight into the Go application", not "into the binary".
- Examples are deliberately small. Each agent is one source file and two prompt files; if it grows past that it has stopped being a demo. Sidecar and vendored must stay a diff of a few lines.
- Every runnable thing has a comment at the top saying which act and slide it belongs to.
- Shell scripts (there is one, `stacks/service-footprint/drift.sh`): `set -euo pipefail`, self-contained, print each statement before running it, `--approot` explicit.

## Demo-day constraints

- Assume conference wifi is bad or absent. The pre-flight block in RUNBOOK.md leaves the machine able to start every act offline: providers pulled, the server bundle cached, vendored programs generated and built. Reads against GitHub and calls to Anthropic still need the network; the runbook has fallbacks.
- The main flow needs AWS, Cloudflare and Anthropic credentials in `.env` (exported); sandboxctl adds Google. The GitHub path (`audit-agent-sidecar`, `stacks/golden-path`) keeps a zero-credential route for people who clone the repo.
- Provider quirks that bite on stage are listed in `primer/README.md` and encoded in the agents' `system.md` files: `ORDER BY` + `LIMIT` in one SELECT limits first; `IN (...)` on a key column fans out; booleans compare as `0`/`1`; `json_each` beside a provider table drops `WHERE` filters (CTE first); a flat three-table JOIN does not plan (CTE first); agents do better on flat one-row-per-item resources than JSON blobs; `github.repos.repos` by `owner` + `repo` returns nothing (use `org` lists); GitHub org-wide lists take 5 to 50 s.
- The demo mutates a real sandbox: the AWS account and the `stackql.xyz` zone via the stacks, a GCP project via sandboxctl. Identity tags find the resources; governance tags are desired state. Never make an `exists` query depend on a tag that is allowed to drift.

## References

- Module source: https://github.com/stackql/stackql/tree/main/packaging/mcpb/go (the packaging CLAUDE.md there documents the full wrapper contract); docs at https://pkg.go.dev/github.com/stackql/stackql-mcp-go/embed
- StackQL MCP docs: https://stackql.io/docs/mcp
- stackql-deploy: https://github.com/stackql/stackql-deploy-rs, installer at get-stackql-deploy.io, docs at https://stackql-deploy.io
- Siblings: rust-embedded-mcp-with-stackql (same three-act shape; this repo's primer and stacks are ported from it), dotnet-embedded-mcp-with-stackql
