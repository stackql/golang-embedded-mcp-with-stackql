// Package agentloop is a Claude agent loop wired to the tools of an embedded
// StackQL MCP server: MCP tool definitions become Anthropic tool definitions,
// tool_use blocks are relayed to the server, and every SQL statement the
// model runs is echoed to stderr so each step is inspectable.
//
// Shared by every agent in embedded/ (act 3). The loop is the same whether
// the server arrived as a sidecar or vendored; only the server's Options
// differ, and that is decided in each program's main.go.
package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	stackqlmcp "github.com/stackql/stackql-mcp-go/embed"
)

const maxToolResultBytes = 64 * 1024

// Session is a conversation with Claude over an embedded server's tools.
// History persists across Ask calls, so a REPL keeps its context.
type Session struct {
	llm      anthropic.Client
	server   *stackqlmcp.Client
	model    anthropic.Model
	tools    []anthropic.ToolUnionParam
	names    []string
	messages []anthropic.MessageParam
}

// New lists the server's MCP tools and converts them to Anthropic tool
// definitions. The Anthropic client reads ANTHROPIC_API_KEY.
func New(ctx context.Context, server *stackqlmcp.Client, model string) (*Session, error) {
	listed, err := server.Session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("listing MCP tools: %w", err)
	}
	var (
		tools []anthropic.ToolUnionParam
		names []string
	)
	for _, t := range listed.Tools {
		schemaJSON, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("marshalling schema for %s: %w", t.Name, err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal(schemaJSON, &schema); err != nil {
			return nil, fmt.Errorf("converting schema for %s: %w", t.Name, err)
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: schema.Properties,
				Required:   schema.Required,
			},
		}})
		names = append(names, t.Name)
	}
	return &Session{
		llm:    anthropic.NewClient(), // reads ANTHROPIC_API_KEY
		server: server,
		model:  anthropic.Model(model),
		tools:  tools,
		names:  names,
	}, nil
}

// ToolCount reports how many MCP tools the session exposes to the model.
func (s *Session) ToolCount() int { return len(s.tools) }

// ToolNames lists the MCP tools the session exposes to the model.
func (s *Session) ToolNames() []string { return s.names }

// Pull installs providers into the server's local registry cache through the
// pull_provider tool, so the agent's first query does not pay for it.
// Idempotent; needs no credentials.
func Pull(ctx context.Context, server *stackqlmcp.Client, providers ...string) error {
	for _, p := range providers {
		res, err := server.Session.CallTool(ctx, &mcp.CallToolParams{
			Name: "pull_provider", Arguments: map[string]any{"provider": p},
		})
		if err != nil {
			return fmt.Errorf("pulling provider %s: %w", p, err)
		}
		if res.IsError {
			return fmt.Errorf("pulling provider %s: %s", p, FirstLine(toolResultText(res)))
		}
	}
	return nil
}

// Render fills `{{ NAME }}` placeholders in a prompt from lookup. A
// placeholder with no value is an error, not a blank: a sweep with an empty
// region or zone id would send the agent off guessing.
func Render(template string, lookup func(name string) string) (string, error) {
	var (
		out     strings.Builder
		missing []string
		rest    = template
	)
	for {
		start := strings.Index(rest, "{{")
		if start < 0 {
			break
		}
		out.WriteString(rest[:start])
		after := rest[start+2:]
		end := strings.Index(after, "}}")
		if end < 0 {
			out.WriteString(rest[start:])
			rest = ""
			break
		}
		name := strings.TrimSpace(after[:end])
		if v := lookup(name); v != "" {
			out.WriteString(v)
		} else {
			missing = append(missing, name)
		}
		rest = after[end+2:]
	}
	out.WriteString(rest)
	if len(missing) > 0 {
		return "", fmt.Errorf("prompts/task.md needs these set in the environment (.env): %s", strings.Join(missing, ", "))
	}
	return out.String(), nil
}

// Ask appends the user message, runs the agent loop until the model stops
// calling tools, and returns the model's final text.
func (s *Session) Ask(ctx context.Context, system, user string, maxTurns int) (string, error) {
	s.messages = append(s.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(user)))
	for turn := 0; turn < maxTurns; turn++ {
		msg, err := s.llm.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     s.model,
			MaxTokens: 8192,
			System:    []anthropic.TextBlockParam{{Text: system}},
			Messages:  s.messages,
			Tools:     s.tools,
		})
		if err != nil {
			return "", fmt.Errorf("anthropic API: %w", err)
		}
		s.messages = append(s.messages, msg.ToParam())

		var (
			results   []anthropic.ContentBlockParamUnion
			finalText strings.Builder
		)
		for _, block := range msg.Content {
			switch block.Type {
			case "text":
				finalText.WriteString(block.Text)
			case "tool_use":
				results = append(results, s.callTool(ctx, block.ID, block.Name, block.Input))
			}
		}
		if len(results) == 0 {
			return finalText.String(), nil
		}
		if text := strings.TrimSpace(finalText.String()); text != "" {
			fmt.Fprintf(os.Stderr, "\n%s\n", text)
		}
		s.messages = append(s.messages, anthropic.NewUserMessage(results...))
	}
	return "", fmt.Errorf("agent did not finish within %d turns", maxTurns)
}

// callTool relays one tool call to the embedded server and echoes what ran.
func (s *Session) callTool(ctx context.Context, id, name string, input json.RawMessage) anthropic.ContentBlockParamUnion {
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return anthropic.NewToolResultBlock(id, "invalid tool input: "+err.Error(), true)
	}
	if sql, ok := args["sql"].(string); ok {
		fmt.Fprintf(os.Stderr, "\n  [%s]\n  %s\n", name, strings.ReplaceAll(strings.TrimSpace(sql), "\n", "\n  "))
	} else {
		compact, _ := json.Marshal(args)
		fmt.Fprintf(os.Stderr, "\n  [%s] %s\n", name, compact)
	}
	res, err := s.server.Session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  tool transport error: %v\n", err)
		return anthropic.NewToolResultBlock(id, "tool call failed: "+err.Error(), true)
	}
	text := toolResultText(res)
	if len(text) > maxToolResultBytes {
		text = text[:maxToolResultBytes] + "\n[truncated]"
	}
	if res.IsError {
		fmt.Fprintf(os.Stderr, "  -> error: %s\n", FirstLine(text))
	} else {
		fmt.Fprintf(os.Stderr, "  -> ok (%d bytes)\n", len(text))
	}
	return anthropic.NewToolResultBlock(id, text, res.IsError)
}

func toolResultText(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// FirstLine trims s to its first line.
func FirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
