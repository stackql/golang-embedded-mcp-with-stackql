// Command vendored is the same program as minimal with the server binary
// compiled straight into the executable: `go generate` fetches the release
// bundle for this platform once (sha256-verified against the pins the
// stackql-mcp-go module ships) and writes the go:embed glue; at run time
// there is no download, no network - one self-contained binary.
//
//	go generate ./cmd/vendored
//	go build ./cmd/vendored
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	stackqlmcp "github.com/stackql/stackql-mcp-go/embed"
)

//go:generate go run github.com/stackql/stackql-mcp-go/cmd/stackql-mcp-fetch -platform auto -package main

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vendored:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	server, err := stackqlmcp.StartServer(ctx, stackqlmcp.Options{
		Binary: StackqlMCPBinary(), // <- the only change from minimal
		Mode:   stackqlmcp.ModeReadOnly,
		Auth:   map[string]any{"github": map[string]any{"type": "null_auth"}},
	})
	if err != nil {
		return err
	}
	defer server.Close()

	listed, err := server.Session.ListTools(ctx, nil)
	if err != nil {
		return err
	}
	fmt.Printf("embedded (vendored) stackql mcp: %d tools, read_only\n", len(listed.Tools))

	sql := "SELECT login, public_repos FROM github.orgs.orgs WHERE org = 'stackql'"
	fmt.Printf("run_select_query: %s\n", sql)
	sel, err := server.Session.CallTool(ctx, &mcp.CallToolParams{
		Name: "run_select_query", Arguments: map[string]any{"sql": sql},
	})
	if err != nil {
		return err
	}
	var sb strings.Builder
	for _, c := range sel.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	fmt.Println(strings.TrimSpace(sb.String()))
	return nil
}
