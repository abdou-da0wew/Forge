package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/abdou/forge/internal/proxy"
)

var (
	version = "1.0.0"
)

func main() {
	var (
		url         = flag.String("url", "", "Forge server URL (required)")
		token       = flag.String("token", "", "Forge auth token (required)")
		execCmd     = flag.String("exec", "", "Command to run (exec mode)")
		interactive = flag.Bool("interactive", false, "Interactive PTY session")
		ttl         = flag.Int("time", 60, "Session TTL in minutes")
		gpu         = flag.Bool("gpu", false, "Request GPU passthrough")
		camera      = flag.Bool("camera", false, "Request camera passthrough")
		label       = flag.String("label", "", "Session label for forge list")
		showVersion = flag.Bool("version", false, "Show version")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("forge-proxy %s\n", version)
		os.Exit(0)
	}

	if *url == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "Error: --url and --token are required")
		printUsage()
		os.Exit(1)
	}

	if *execCmd == "" && !*interactive {
		fmt.Fprintln(os.Stderr, "Error: provide --exec <cmd> or --interactive")
		printUsage()
		os.Exit(1)
	}

	cfg := proxy.Config{
		ServerURL:   *url,
		Token:       *token,
		ExecCmd:     *execCmd,
		Interactive: *interactive,
		TTLMinutes:  *ttl,
		GPU:         *gpu,
		Camera:      *camera,
		Label:       *label,
	}

	os.Exit(proxy.NewClient(cfg).Run())
}

func printUsage() {
	fmt.Fprint(os.Stderr, `
forge-proxy - Connect to Forge build server from restricted environments

Usage:
  forge-proxy --url <url> --token <token> [options]

Modes:
  --exec <cmd>         Run command in container, stream output, exit with code
  --interactive        Get an interactive PTY session

Options:
  --url <url>          Forge server URL (required)
  --token <token>      Forge auth token (required)
  --time <min>         Session TTL in minutes (default: 60)
  --gpu                Request GPU passthrough
  --camera             Request camera passthrough
  --label <text>       Session label for identification
  --version            Show version

Examples:
  # Run a build command
  forge-proxy --url https://forge.example.com --token $TOKEN \
    --exec "cd /workspace && cargo build"

  # Get an interactive shell
  forge-proxy --url https://forge.example.com --token $TOKEN --interactive

  # Request GPU for testing
  forge-proxy --url $URL --token $TOKEN --gpu \
    --exec "cargo test pipeline::gpu::"

Environment:
  FORGE_URL            Default Forge server URL
  FORGE_TOKEN          Default Forge auth token
`)
}
