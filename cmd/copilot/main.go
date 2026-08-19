// Command copilot - ask questions about your actual cloud estate in English.
//
// The embedded StackQL MCP server's tools are handed to a Claude agent loop
// (anthropic-sdk-go). Every answer is grounded in a SQL query the model ran
// against live provider state; the tool-call trace goes to stderr. The
// server is read-only: however the model is prompted, writes are refused
// server-side.
//
//	go run ./cmd/copilot --check                # no model call, no key needed
//	go run ./cmd/copilot -p "your question"     # one-shot
//	go run ./cmd/copilot                        # REPL
//
// Needs ANTHROPIC_API_KEY (except --check). The github provider runs with
// null_auth, so no cloud credentials are required for the default demo.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/stackql/golang-embedded-mcp-with-stackql/internal/agentloop"
	stackqlmcp "github.com/stackql/stackql-mcp-go/embed"
)

const systemPrompt = `You are a cloud operations copilot. You answer questions about the user's
actual cloud and SaaS estate by querying it with StackQL SQL via the tools
you have been given. Discover before you query: list providers, services
and resources, and describe a resource to learn its fields, rather than
guessing table or column names. Prefer run_select_query. Always show the
SQL you ran alongside the answer. You are read-only: do not attempt
mutations; if asked to change something, explain what you would run and
that this session refuses writes.`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "copilot:", err)
		os.Exit(1)
	}
}

func run() error {
	check := flag.Bool("check", false, "preflight without a model call")
	prompt := flag.String("p", "", "one-shot: answer this prompt and exit")
	maxTurns := flag.Int("max-turns", 20, "maximum agent turns per question")
	modelFlag := flag.String("model", "", "Anthropic model id")
	flag.StringVar(prompt, "prompt", *prompt, "alias for -p")
	flag.Parse()

	model := *modelFlag
	if model == "" {
		model = os.Getenv("COPILOT_MODEL")
	}
	if model == "" {
		model = "claude-sonnet-5"
	}

	fmt.Fprintln(os.Stderr, "copilot: starting the embedded StackQL MCP server (read-only, github null_auth) ...")
	ctx := context.Background()
	server, err := stackqlmcp.StartServer(ctx, stackqlmcp.Options{
		Mode: stackqlmcp.ModeReadOnly,
		Auth: map[string]any{"github": map[string]any{"type": "null_auth"}},
	})
	if err != nil {
		return err
	}
	defer server.Close()

	session, err := agentloop.New(ctx, server, model)
	if err != nil {
		return err
	}

	hasKey := os.Getenv("ANTHROPIC_API_KEY") != ""
	if *check {
		if hasKey {
			fmt.Printf("copilot: check OK - server + %d MCP tools ready, ANTHROPIC_API_KEY present (model %s)\n", session.ToolCount(), model)
		} else {
			fmt.Printf("copilot: check OK - server + %d MCP tools ready (set ANTHROPIC_API_KEY to run the agent)\n", session.ToolCount())
		}
		return nil
	}
	if !hasKey {
		return fmt.Errorf("ANTHROPIC_API_KEY is not set (use --check for a key-less preflight)")
	}

	ask := func(question string) error {
		answer, err := session.Ask(ctx, systemPrompt, question, *maxTurns)
		if err != nil {
			return err
		}
		fmt.Println(strings.TrimSpace(answer))
		return nil
	}

	if *prompt != "" {
		return ask(*prompt)
	}

	fmt.Fprintln(os.Stderr, "copilot: REPL - empty line or Ctrl+C to exit")
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\n> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return nil
		}
		if err := ask(line); err != nil {
			return err
		}
	}
}
