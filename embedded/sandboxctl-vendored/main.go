// Command sandboxctl-vendored is an on-demand infrastructure concierge: an
// embedded stackql MCP server, vendored straight into this executable, wired
// to a Claude agent loop. Act 3, the reference app: mode escalation as a
// workflow, and the single-binary distribution story (`go generate` once,
// then one file that starts with no network).
//
// Developers ask for ephemeral cloud sandboxes in plain language; the
// agent plans, prices, provisions, and labels everything with an expiry so
// `sandboxctl reap` can tear it down later. Nothing is created before the
// plan and cost estimate pass an explicit approval gate, and every SQL
// statement the agent runs is printed.
//
//	go generate ./embedded/sandboxctl-vendored
//	go run ./embedded/sandboxctl-vendored request "a small linux vm and a bucket in sydney for 2 days" --project my-gcp-project
//	go run ./embedded/sandboxctl-vendored reap --project my-gcp-project
//	go run ./embedded/sandboxctl-vendored tools
//
// Requires ANTHROPIC_API_KEY (request/reap) and GOOGLE_CREDENTIALS (a GCP
// service account key JSON, stackql's default google auth) in the
// environment. `sandboxctl tools` needs neither.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stackql/golang-embedded-mcp-with-stackql/embedded/internal/agentloop"
	stackqlmcp "github.com/stackql/stackql-mcp-go/embed"
)

// The server binary for this platform, fetched once at build time. The
// generated stackql_mcp_<platform>_gen.go (gitignored) carries it with
// go:embed and exposes StackqlMCPBinary().
//
//go:generate go run github.com/stackql/stackql-mcp-go/cmd/stackql-mcp-fetch -platform auto -package main

const defaultModel = "claude-sonnet-5"

// approvalHandler answers an elicitation/create request from the server;
// it is the type of go-sdk's ClientOptions.ElicitationHandler.
type approvalHandler = func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sandboxctl:", err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	if len(argv) == 0 {
		usage()
		return fmt.Errorf("a subcommand is required")
	}
	switch argv[0] {
	case "request":
		return cmdRequest(argv[1:])
	case "reap":
		return cmdReap(argv[1:])
	case "tools":
		return cmdTools(argv[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q", argv[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  sandboxctl request "<what you need>" --project <gcp-project> [--expires 48h] [--approve] [--model <id>]
  sandboxctl reap --project <gcp-project> [--approve] [--model <id>]
  sandboxctl tools
`)
}

func model(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if env := os.Getenv("ANTHROPIC_MODEL"); env != "" {
		return env
	}
	return defaultModel
}

// startServer spawns the embedded stackql MCP server in the given mode. The
// server binary is vendored: extracted from this executable into the shared
// cache on first run, no download.
//
// The write modes are a server-side contract: in safe the server asks the
// client for approval before every mutation, in delete_safe before every
// DELETE, over MCP elicitation, and refuses a client that cannot answer.
// approvals is the handler that answers; nil is fine for read_only.
// StartServer does not expose client options, so for the write phases the
// module's CommandLine (the same acquisition and launch arguments) feeds a
// go-sdk client built here with the handler attached.
func startServer(ctx context.Context, mode stackqlmcp.Mode, approvals approvalHandler) (*stackqlmcp.Client, error) {
	fmt.Fprintf(os.Stderr, "starting embedded stackql mcp server (vendored, mode=%s)\n", mode)
	opts := stackqlmcp.Options{
		Binary: StackqlMCPBinary(),
		Mode:   mode,
	}
	if approvals == nil {
		return stackqlmcp.StartServer(ctx, opts)
	}
	path, args, err := stackqlmcp.CommandLine(opts)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, args...)
	cmd.Stderr = os.Stderr
	client := mcp.NewClient(
		&mcp.Implementation{Name: "sandboxctl", Version: "0.1.0"},
		&mcp.ClientOptions{ElicitationHandler: approvals}, // advertises the elicitation capability
	)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to the embedded server: %w", err)
	}
	return &stackqlmcp.Client{Session: session, BinaryPath: path, Mode: mode}, nil
}

// approver answers the server's per-statement approval requests. The plan was
// approved as a whole at the gate; the server still shows each gated
// statement and waits for a y at the terminal, unless --approve pre-approved
// the run (cron, CI). Declining makes the server refuse that one statement.
func approver(preApproved bool) approvalHandler {
	return func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		fmt.Fprintf(os.Stderr, "\n  [approval requested by the server]\n  %s\n",
			strings.ReplaceAll(strings.TrimSpace(req.Params.Message), "\n", "\n  "))
		if preApproved {
			fmt.Fprintln(os.Stderr, "  -> accepted (--approve)")
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{}}, nil
		}
		ok, err := confirm(false, "  approve this statement?")
		if err != nil {
			return nil, err
		}
		if !ok {
			return &mcp.ElicitResult{Action: "decline"}, nil
		}
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{}}, nil
	}
}

func checkEnv(needAnthropic bool) error {
	if needAnthropic && os.Getenv("ANTHROPIC_API_KEY") == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}
	if os.Getenv("GOOGLE_CREDENTIALS") == "" {
		fmt.Fprintln(os.Stderr, "warning: GOOGLE_CREDENTIALS is not set; google provider calls will fail to authenticate")
	}
	return nil
}

// confirm is the approval boundary: nothing mutating runs before a yes.
func confirm(approved bool, prompt string) (bool, error) {
	if approved {
		fmt.Fprintln(os.Stderr, "pre-approved with --approve")
		return true, nil
	}
	fmt.Printf("\n%s [y/N]: ", prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("reading approval (use --approve for non-interactive runs): %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func cmdRequest(argv []string) error {
	fs := flag.NewFlagSet("request", flag.ContinueOnError)
	project := fs.String("project", "", "GCP project id (required)")
	expires := fs.Duration("expires", 48*time.Hour, "sandbox lifetime")
	approve := fs.Bool("approve", false, "skip the interactive approval gate")
	modelFlag := fs.String("model", "", "Anthropic model id")
	// Accept the description before or after the flags: the flag package
	// stops at the first positional, so re-parse the remainder.
	if err := fs.Parse(argv); err != nil {
		return err
	}
	rest := fs.Args()
	var request string
	if len(rest) > 0 {
		request = rest[0]
		if err := fs.Parse(rest[1:]); err != nil {
			return err
		}
		rest = fs.Args()
	}
	if request == "" || len(rest) != 0 {
		return fmt.Errorf(`request takes exactly one quoted description, for example: sandboxctl request "a small linux vm in sydney" --project my-project`)
	}
	if *project == "" {
		return fmt.Errorf("--project is required")
	}
	if err := checkEnv(true); err != nil {
		return err
	}
	expiresAt := time.Now().Add(*expires).UTC()
	ctx := context.Background()

	// Phase 1: plan and price with a read_only server. The agent cannot
	// create anything here even if it tries.
	planServer, err := startServer(ctx, stackqlmcp.ModeReadOnly, nil)
	if err != nil {
		return err
	}
	agent, err := agentloop.New(ctx, planServer, model(*modelFlag))
	if err != nil {
		planServer.Close()
		return err
	}
	fmt.Println("== phase 1: plan and cost estimate (read_only) ==")
	plan, err := agent.Ask(ctx, planSystemPrompt, planUserPrompt(request, *project, expiresAt), 40)
	planServer.Close()
	if err != nil {
		return err
	}
	fmt.Printf("\n%s\n", plan)

	ok, err := confirm(*approve, "create these resources?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("not approved; nothing was created")
		return nil
	}

	// Phase 2: provision with a fresh server in safe mode. The server asks
	// for approval before each INSERT; approver answers at the terminal.
	provServer, err := startServer(ctx, stackqlmcp.ModeSafe, approver(*approve))
	if err != nil {
		return err
	}
	defer provServer.Close()
	agent, err = agentloop.New(ctx, provServer, model(*modelFlag))
	if err != nil {
		return err
	}
	fmt.Println("\n== phase 2: provision (safe) ==")
	report, err := agent.Ask(ctx, provisionSystemPrompt, provisionUserPrompt(plan, *project, expiresAt), 40)
	if err != nil {
		return err
	}
	fmt.Printf("\n%s\n", report)
	return nil
}

func cmdReap(argv []string) error {
	fs := flag.NewFlagSet("reap", flag.ContinueOnError)
	project := fs.String("project", "", "GCP project id (required)")
	approve := fs.Bool("approve", false, "skip the interactive approval gate (for cron)")
	modelFlag := fs.String("model", "", "Anthropic model id")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *project == "" {
		return fmt.Errorf("--project is required")
	}
	if err := checkEnv(true); err != nil {
		return err
	}
	ctx := context.Background()
	now := time.Now().UTC()

	// Phase 1: find expired sandboxes with a read_only server.
	findServer, err := startServer(ctx, stackqlmcp.ModeReadOnly, nil)
	if err != nil {
		return err
	}
	agent, err := agentloop.New(ctx, findServer, model(*modelFlag))
	if err != nil {
		findServer.Close()
		return err
	}
	fmt.Println("== phase 1: find expired sandboxes (read_only) ==")
	findings, err := agent.Ask(ctx, reapFindSystemPrompt, reapFindUserPrompt(*project, now), 40)
	findServer.Close()
	if err != nil {
		return err
	}
	fmt.Printf("\n%s\n", findings)
	if strings.Contains(findings, "NOTHING-TO-REAP") {
		fmt.Println("no expired sandboxes")
		return nil
	}

	ok, err := confirm(*approve, "tear down the expired resources listed above?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("not approved; nothing was deleted")
		return nil
	}

	// Phase 2: tear down with delete_safe. The server asks for approval
	// before each DELETE; approver answers at the terminal.
	reapServer, err := startServer(ctx, stackqlmcp.ModeDeleteSafe, approver(*approve))
	if err != nil {
		return err
	}
	defer reapServer.Close()
	agent, err = agentloop.New(ctx, reapServer, model(*modelFlag))
	if err != nil {
		return err
	}
	fmt.Println("\n== phase 2: tear down (delete_safe) ==")
	report, err := agent.Ask(ctx, reapTeardownSystemPrompt, reapTeardownUserPrompt(findings, *project), 40)
	if err != nil {
		return err
	}
	fmt.Printf("\n%s\n", report)
	return nil
}

// cmdTools lists the embedded server's MCP tools. It needs no credentials
// and doubles as a smoke check that the embedded binary starts.
func cmdTools(argv []string) error {
	fs := flag.NewFlagSet("tools", flag.ContinueOnError)
	if err := fs.Parse(argv); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	server, err := startServer(ctx, stackqlmcp.ModeReadOnly, nil)
	if err != nil {
		return err
	}
	defer server.Close()
	listed, err := server.Session.ListTools(ctx, nil)
	if err != nil {
		return err
	}
	info := server.Session.InitializeResult()
	fmt.Printf("server: %s %s (binary: %s)\n", info.ServerInfo.Name, info.ServerInfo.Version, server.BinaryPath)
	for _, t := range listed.Tools {
		fmt.Printf("  %-24s %s\n", t.Name, agentloop.FirstLine(t.Description))
	}
	return nil
}
