# Act 1 - StackQL primer

One engine, several ways to consume it, across providers and planes. Slides: STACKQL >>, Cloud Providers as Data Sources, How to use StackQL. Three providers: `aws` (compute, storage, Cost Explorer), `cloudflare` (the edge) and `github` (the code), so the blocks need `AWS_*`, `CLOUDFLARE_*` and `STACKQL_GITHUB_*` from `.env` exported; the GitHub blocks and the Go app also run with zero credentials. Every query here answers in about a second of API time (measured; the CLI adds about 1.5 s of start-up per `exec` on Windows, none in the shell). The two closers, the cross-plane `UNION ALL` and the cross-provider CTE JOIN (Cloudflare A records joined to the EC2 instances they point at), are the bridge into acts 2 and 3; the JOIN returns a row per built stack (`stacks/aws-webserver`, `stacks/service-footprint`).

The `.sh` and `.iql` files are paste blocks, not batch scripts: copy one block at a time into a shell (or into `stackql shell`) from the repo root with `.env` exported (`set -a; . ./.env; set +a`). Needs `stackql` on PATH ([installing StackQL](https://stackql.io/docs/installing-stackql)); the embedded programs in act 3 acquire their own copy.

| File | Shows |
|---|---|
| `shell.iql` | `stackql shell`: `REGISTRY PULL`, `SHOW PROVIDERS`, discovery (`SHOW SERVICES`, `SHOW RESOURCES`, `SHOW EXTENDED METHODS`, `DESCRIBE EXTENDED`), then `JSON_EXTRACT` and `json_each` over provider JSON, SQLite built-ins (`strftime`, `julianday`, `GROUP_CONCAT`, `json_array_length`), window functions (`SUM ... OVER (PARTITION BY)`, `ROW_NUMBER`, `RANK`), a JOIN, Cost Explorer, the cross-plane `UNION ALL`, the cross-provider JOIN, and the mutation verbs |
| `exec.sh` | `stackql exec`: `--output table`, `json`, `jsonl`, `csv`, to stdout or a file (`-f`), `-H` (no header), `-d` (delimiter), piped into `jq` and `column`, queries from a file (`-i`), jsonnet templating (`--iqldata` + `--var`), `--dryrun` |
| `srv.sh` | `stackql srv` (Postgres wire protocol) with `psql` and the Go app |
| `queries/` | `exposure.iql` (plain `-i`), `finops.iql` + `vars.jsonnet` (the Cost Explorer window from `--var month=`), `cross-provider.iql` + `footprint-vars.jsonnet` |
| `pgwire-go-app/` | Go: [pgx](https://github.com/jackc/pgx) talking to `stackql srv` over the Postgres wire protocol, public GitHub data, zero credentials (`stackql srv --pgsrv.port 5466` in one terminal, `go run ./primer/pgwire-go-app` in another) |

Notes that matter on the day:

- Timing: `aws` describe calls, Cloudflare, Cost Explorer and single-object GitHub calls (the org, one repo's branches or releases) are all about a second. GitHub org-wide lists page through the API (`repos` 6 s, `branches` joined across repos 9 s, `workflow_runs` and `commits` 20 pages, 20 to 50 s) and are not in the flow.
- `json_each` in the same `FROM` as a provider table drops the `WHERE` filters; put the filtered provider query in a CTE and `json_each` over the CTE (1.4, 1.11). The `aws` provider wraps lists in an `item` key: `json_each(tags, '$.item')`.
- Window functions work in the innermost query over the provider table (1.6, 1.7, 1.10); an outer query over a derived table cannot add one.
- Fan-out: a JOIN or an `IN (...)` on a key column becomes one API call per key. The IN lists here are on plain columns.
- `ORDER BY` with `LIMIT` in the same SELECT applies the limit first; wrap the ordered query in a subquery (1.10).
- Booleans come back as `true`/`false` text but are compared as `0`/`1` (`protected = 1`, `is_egress = 0`). Over the wire protocol every column arrives as text; `pgwire-go-app` pins pgx to the simple query protocol (`default_query_exec_mode=simple_protocol` in the DSN) because that is what the server speaks, and converts in Go before doing arithmetic.
- Windows builds of `stackql` default `--approot` to the current directory; add `--approot ~/.stackql` to each `stackql` command there. On macOS and Linux the default is `~/.stackql`.
- `exec.sh` writes `exposure.jsonl`, `volumes.csv` and `records.psv` into the repo root; they are gitignored.
- Column widths are chosen to fit an 80x24 terminal.
