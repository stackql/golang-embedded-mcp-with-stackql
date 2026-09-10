// Command audit-agent-sidecar is a repository governance audit agent on an
// embedded StackQL MCP server. Act 3, the zero-credential agent: the github
// provider runs with null_auth, so the sweep, the CI smoke and anyone who
// clones the repo need nothing but ANTHROPIC_API_KEY.
//
// The four parts every agent has, in the order they appear below:
//
//	model    Claude, through anthropic-sdk-go (ANTHROPIC_API_KEY)
//	tools    the StackQL MCP tools, served by a stackql binary the module
//	         downloads, sha256-verifies and caches on first run (the
//	         sidecar), launched over stdio in read_only mode
//	prompts  prompts/system.md (persona and StackQL context) and
//	         prompts/task.md (the audit), compiled in with go:embed,
//	         placeholders filled from the environment
//	loop     internal/agentloop: run the turns, echo each tool call, print
//	         the report
//
// Setup:
//
//	export ANTHROPIC_API_KEY=sk-ant-...      # GITHUB_ORG / GITHUB_REPO optional
//	go run ./embedded/audit-agent-sidecar --check    # server, provider, tools; no model call
//	go run ./embedded/audit-agent-sidecar            # the audit (prompts/task.md)
//	go run ./embedded/audit-agent-sidecar "which stackql org repos have no license? show the SQL"
//	go run ./embedded/audit-agent-sidecar --repl     # questions in turn, history kept
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

var providers = []string{"github"}

const (
	name         = "audit-agent-sidecar"
	modelEnv     = "AUDIT_AGENT_MODEL"
	defaultModel = "claude-sonnet-5"
	defaultOrg   = "stackql"
	defaultRepo  = "golang-embedded-mcp-with-stackql"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		os.Exit(1)
	}
}

func run() error {
	check := flag.Bool("check", false, "pre-flight: start the server, pull the provider, list the tools, exit (no model call)")
	repl := flag.Bool("repl", false, "interactive: ask questions in turn, history kept")
	maxTurns := flag.Int("max-turns", 40, "max tool-call rounds for one answer")
	modelFlag := flag.String("model", "", "Claude model (default "+modelEnv+" or "+defaultModel+")")
	flag.Parse()
	model := firstOf(*modelFlag, os.Getenv(modelEnv), defaultModel)

	// The server. Sidecar: the stackql release pinned in the module is
	// downloaded, verified and cached under ~/.stackql/mcp-server-bin on the
	// first run; every later start is offline. read_only: mutations are
	// refused by the server itself, whatever the model asks for. The github
	// provider runs anonymously (null_auth) unless a token is in the
	// environment; anonymous reads are rate-limited but enough for the audit.
	ctx := context.Background()
	opts := stackqlmcp.Options{Mode: stackqlmcp.ModeReadOnly}
	auth := "STACKQL_GITHUB_PASSWORD from the environment"
	if os.Getenv("STACKQL_GITHUB_PASSWORD") == "" {
		opts.Auth = map[string]any{"github": map[string]any{"type": "null_auth"}}
		auth = "null_auth, zero credentials"
	}
	fmt.Fprintf(os.Stderr, "%s: starting the embedded StackQL MCP server (sidecar, read_only, github %s)\n", name, auth)
	server, err := stackqlmcp.StartServer(ctx, opts)
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

	// The prompts: markdown compiled into the binary; {{ GITHUB_ORG }} and
	// {{ GITHUB_REPO }} from .env, with defaults so the audit runs from a
	// clean clone. A positional argument replaces the task; --repl takes
	// questions in turn.
	if flag.NArg() > 0 {
		return ask(strings.Join(flag.Args(), " "))
	}
	if *repl {
		return loop(ask)
	}
	task, err := agentloop.Render(taskPrompt, func(key string) string {
		switch key {
		case "GITHUB_ORG":
			return firstOf(os.Getenv(key), defaultOrg)
		case "GITHUB_REPO":
			return firstOf(os.Getenv(key), defaultRepo)
		}
		return os.Getenv(key)
	})
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
