// Command minimal is the smallest embedding. Start the embedded StackQL MCP
// server read-only, list its tools, run one SELECT over the github provider
// with no credentials, prove writes are refused, shut down. First run
// downloads and sha256-verifies the pinned server bundle; after that it is
// offline.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	stackqlmcp "github.com/stackql/stackql-mcp-go/embed"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "minimal:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	server, err := stackqlmcp.StartServer(ctx, stackqlmcp.Options{
		Mode: stackqlmcp.ModeReadOnly, // writes refused server-side, whatever the caller asks
		Auth: map[string]any{"github": map[string]any{"type": "null_auth"}},
	})
	if err != nil {
		return err
	}
	defer server.Close()

	listed, err := server.Session.ListTools(ctx, nil)
	if err != nil {
		return err
	}
	fmt.Printf("embedded stackql mcp: %d tools, read_only\n", len(listed.Tools))
	for _, t := range listed.Tools {
		fmt.Printf("  %s\n", t.Name)
	}

	sql := "SELECT login, public_repos, followers FROM github.orgs.orgs WHERE org = 'stackql'"
	fmt.Printf("\nrun_select_query: %s\n", sql)
	sel, err := server.Session.CallTool(ctx, &mcp.CallToolParams{
		Name: "run_select_query", Arguments: map[string]any{"sql": sql},
	})
	if err != nil {
		return err
	}
	fmt.Println(text(sel))

	mutation := "INSERT INTO github.repos.repos(data__name) SELECT 'nope'"
	fmt.Printf("\nrun_mutation_query (should be refused): %s\n", mutation)
	mut, err := server.Session.CallTool(ctx, &mcp.CallToolParams{
		Name: "run_mutation_query", Arguments: map[string]any{"sql": mutation},
	})
	if err != nil {
		return err
	}
	if !mut.IsError {
		return fmt.Errorf("UNEXPECTED: the mutation was not refused")
	}
	fmt.Printf("refused, as designed: %s\n", firstLine(text(mut)))
	return nil
}

func text(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
