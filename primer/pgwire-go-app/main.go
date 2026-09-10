// Command pgwire-go-app is a Go program talking to a stackql server over the
// Postgres wire protocol with pgx. Act 1, slide "How to use StackQL"
// (Application & Data Integration row): the engine behind `stackql srv` is
// the same one the shell, exec and the MCP server use, and any Postgres
// client can drive it.
//
// Public GitHub data, so it runs with zero credentials. Start the server
// first, then the app, from the repo root:
//
//	stackql srv --pgsrv.port 5466
//	go run ./primer/pgwire-go-app
//
// STACKQL_PG_DSN overrides the connection string. The server speaks the
// simple query protocol, so the DSN pins pgx to it and every column arrives
// as text; convert in Go before doing arithmetic.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

const defaultDSN = "postgres://stackql@localhost:5466/stackql?sslmode=disable&default_query_exec_mode=simple_protocol"

// estate inventory: the most starred active repos in the org
const reposSQL = `
SELECT name, stars, forks
FROM (
  SELECT name, stargazers_count AS stars, forks_count AS forks
  FROM github.repos.repos
  WHERE org = 'stackql' AND archived = 0
  ORDER BY stargazers_count DESC
) t
LIMIT 5`

// live operational signal: the latest CI runs
const runsSQL = `
SELECT name, head_branch AS branch, conclusion, substr(created_at, 1, 10) AS day
FROM (
  SELECT name, head_branch, conclusion, created_at
  FROM github.actions.workflow_runs
  WHERE owner = 'stackql' AND repo = 'stackql'
  ORDER BY created_at DESC
) t
LIMIT 5`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pgwire-go-app:", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("STACKQL_PG_DSN")
	if dsn == "" {
		dsn = defaultDSN
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connecting to stackql srv (is it running? stackql srv --pgsrv.port 5466): %w", err)
	}
	defer conn.Close(ctx)

	fmt.Println("stackql org: active repos by stars")
	if err := table(ctx, conn, reposSQL); err != nil {
		return err
	}
	fmt.Println("\nlatest CI runs on stackql/stackql")
	return table(ctx, conn, runsSQL)
}

// table runs one query and prints the result as an aligned text table.
func table(ctx context.Context, conn *pgx.Conn, sql string) error {
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return err
	}
	defer rows.Close()

	var header []string
	for _, f := range rows.FieldDescriptions() {
		header = append(header, f.Name)
	}
	var data [][]string
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return err
		}
		row := make([]string, len(values))
		for i, v := range values {
			if v != nil {
				row[i] = fmt.Sprint(v)
			}
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len(h)
	}
	for _, row := range data {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	line := func(cells []string) {
		var sb strings.Builder
		for i, cell := range cells {
			sb.WriteString(fmt.Sprintf("%-*s  ", widths[i], cell))
		}
		fmt.Println(strings.TrimRight(sb.String(), " "))
	}
	line(header)
	for _, row := range data {
		line(row)
	}
	return nil
}
