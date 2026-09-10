// Command sre-agent-sidecar is an SRE assurance agent on an embedded StackQL
// MCP server. Act 3, the sidecar demo (slide "SIDECAR DETAILED").
//
// The four parts every agent has, in the order they appear below:
//
//	model    Claude, through anthropic-sdk-go (ANTHROPIC_API_KEY)
//	tools    the StackQL MCP tools, served by a stackql binary the module
//	         downloads, sha256-verifies and caches on first run (the
//	         sidecar), launched over stdio in read_only mode
//	prompts  prompts/system.md (persona and StackQL context) and
//	         prompts/task.md (the sweep), compiled in with go:embed,
//	         placeholders filled from the environment
//	loop     internal/agentloop: run the turns, echo each tool call, print
//	         the report
//
// Setup:
//
//	set -a; . ./.env; set +a     # ANTHROPIC_API_KEY, AWS_*, CLOUDFLARE_*, DEMO_*
//	go run ./embedded/sre-agent-sidecar --check     # server, providers, tools; no model call
//	go run ./embedded/sre-agent-sidecar             # the sweep (prompts/task.md)
//	go run ./embedded/sre-agent-sidecar "Which security groups allow port 22 from anywhere?"
//
// Tool calls go to stderr as they happen; the report goes to stdout.
package main

import (
	"bufio"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/stackql/golang-embedded-mcp-with-stackql/embedded/internal/agentloop"
	stackqlmcp "github.com/stackql/stackql-mcp-go/embed"
)

//go:embed prompts/system.md
var systemPrompt string

//go:embed prompts/task.md
var taskPrompt string

var providers = []string{"aws", "cloudflare"}

const (
	name         = "sre-agent-sidecar"
	modelEnv     = "SRE_AGENT_MODEL"
	defaultModel = "claude-sonnet-5"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		os.Exit(1)
	}
}

func run() error {
	check := flag.Bool("check", false, "pre-flight: start the server, pull the providers, list the tools, exit (no model call)")
	repl := flag.Bool("repl", false, "interactive: ask questions in turn, history kept")
	maxTurns := flag.Int("max-turns", 40, "max tool-call rounds for one answer")
	modelFlag := flag.String("model", "", "Claude model (default "+modelEnv+" or "+defaultModel+")")
	flag.Parse()
	model := firstOf(*modelFlag, os.Getenv(modelEnv), defaultModel)

	// The server. Sidecar: the stackql release pinned in the module is
	// downloaded, verified and cached under ~/.stackql/mcp-server-bin on the
	// first run; every later start is offline. read_only: mutations are
	// refused by the server itself, whatever the model asks for. Provider
	// credentials (AWS_*, CLOUDFLARE_API_TOKEN) come from the environment.
	ctx := context.Background()
	fmt.Fprintf(os.Stderr, "%s: starting the embedded StackQL MCP server (sidecar, read_only)\n", name)
	server, err := stackqlmcp.StartServer(ctx, stackqlmcp.Options{
		Mode: stackqlmcp.ModeReadOnly,
	})
	if err != nil {
		return fmt.Errorf("starting the embedded StackQL MCP server: %w", err)
	}
	defer server.Close()
	fmt.Fprintf(os.Stderr, "%s: server binary %s\n", name, server.BinaryPath)
	if err := agentloop.Pull(ctx, server, providers...); err != nil {
		return err
	}

	// The tools: whatever the server advertises, as go-sdk hands them back.
	session, err := agentloop.New(ctx, server, model)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "connected: %d stackql tools (%s)\n\n", session.ToolCount(), server.Mode)
	if *check {
		for _, t := range session.ToolNames() {
			fmt.Println("  " + t)
		}
		return nil
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is not set (use --check for a key-less preflight)")
	}
	fmt.Fprintf(os.Stderr, "%s: model %s\n\n", name, model)

	// The loop: the model runs the tool-call rounds; every SQL statement is
	// echoed on stderr so the room sees it, the answer goes to stdout.
	ask := func(question string) error {
		answer, err := session.Ask(ctx, systemPrompt, question, *maxTurns)
		if err != nil {
			return err
		}
		fmt.Println(strings.TrimSpace(answer))
		return nil
	}

	// The prompts: markdown compiled into the binary; {{ NAME }} from .env.
	// A positional argument replaces the task; --repl takes questions in turn.
	if flag.NArg() > 0 {
		return ask(strings.Join(flag.Args(), " "))
	}
	if *repl {
		return loop(ask)
	}
	task, err := agentloop.Render(taskPrompt, os.Getenv)
	if err != nil {
		return err
	}
	return ask(task)
}

func loop(ask func(string) error) error {
	fmt.Fprintln(os.Stderr, name+": REPL - empty line or Ctrl+C to exit")
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

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
