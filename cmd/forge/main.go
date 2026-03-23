package main

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/abdou/forge/internal/config"
)

var (
	configPath = flag.String("config", "", "Path to config file")
	version    = "1.0.0"
)

func main() {
	flag.Parse()

	if len(flag.Args()) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := flag.Args()[0]
	args := flag.Args()[1:]

	switch cmd {
	case "start":
		cmdStart(args)
	case "list", "ls":
		cmdList(args)
	case "kill":
		cmdKill(args)
	case "logs":
		cmdLogs(args)
	case "replay":
		cmdReplay(args)
	case "rebuild":
		cmdRebuild(args)
	case "status":
		cmdStatus(args)
	case "sign":
		cmdSign(args)
	case "version", "-v", "--version":
		fmt.Printf("forge %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Forge - AI Agent Build Sandbox CLI

Usage:
  forge <command> [options]

Commands:
  start [options]     Start a new build session
  list, ls            List active sessions
  kill <id>           Kill a session
  logs <id>           View session logs
  replay <id>         Replay session recording
  rebuild             Rebuild the Docker image
  status              Show server status
  sign                Generate a signed token
  version             Show version
  help                Show this help

Start options:
  --gpu               Enable GPU passthrough
  --camera            Enable camera passthrough
  --time <min>        Session TTL in minutes (default: 60)
  --label <text>      Session label

Examples:
  forge start --gpu --camera --time 90 --label "iris-build"
  forge list
  forge kill sess_abc123
  forge rebuild

Config file: ~/.forge/config.toml`)
}

func cmdStart(args []string) {
	var (
		gpu    bool
		camera bool
		ttl    int
		label  string
	)

	fs := flag.NewFlagSet("start", flag.ExitOnError)
	fs.BoolVar(&gpu, "gpu", false, "Enable GPU passthrough")
	fs.BoolVar(&camera, "camera", false, "Enable camera passthrough")
	fs.IntVar(&ttl, "time", 60, "Session TTL in minutes")
	fs.StringVar(&label, "label", "", "Session label")
	fs.Parse(args)

	// Load config for token
	cfg := loadConfig()

	// Build request
	reqBody := map[string]interface{}{
		"ttl_minutes": ttl,
		"gpu":         gpu,
		"camera":      camera,
		"label":       label,
	}
	body, _ := json.Marshal(reqBody)

	// Sign request
	token := signRequest("POST", "/session/start", cfg.Auth.Token)

	// Make request
	req, err := http.NewRequest("POST", serverURL()+"/session/start", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		fmt.Fprintf(os.Stderr, "Error: %s\n", errResp["error"])
		os.Exit(1)
	}

	var result struct {
		SessionID       string `json:"session_id"`
		SSHPort         int    `json:"ssh_port"`
		SSHHost         string `json:"ssh_host"`
		SSHUser         string `json:"ssh_user"`
		SSHPrivateKey   string `json:"ssh_private_key"`
		WebTerminalURL  string `json:"web_terminal_url"`
		WebTerminalPass string `json:"web_terminal_password"`
		ExpiresAt       string `json:"expires_at"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	// Save private key
	keyPath := filepath.Join(os.TempDir(), result.SessionID+".key")
	os.WriteFile(keyPath, []byte(result.SSHPrivateKey), 0600)

	// Print session info
	fmt.Println("\n✅ Session started!")
	fmt.Printf("   Session ID: %s\n", result.SessionID)
	fmt.Printf("   SSH:        ssh -i %s -p %d %s@%s\n", keyPath, result.SSHPort, result.SSHUser, result.SSHHost)
	fmt.Printf("   Web:        %s (password: %s)\n", result.WebTerminalURL, result.WebTerminalPass)
	fmt.Printf("   Expires:    %s\n", result.ExpiresAt)
	fmt.Printf("   TTL:        %d minutes\n\n", ttl)

	// Optional: connect immediately
	fmt.Println("Press Enter to connect via SSH, or Ctrl+C to exit...")
	reader := bufio.NewReader(os.Stdin)
	reader.ReadString('\n')

	// Run SSH
	sshCmd := exec.Command("ssh", "-i", keyPath, "-p", strconv.Itoa(result.SSHPort),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", result.SSHUser, result.SSHHost))
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		sshCmd.Process.Kill()
	}()

	sshCmd.Run()

	// Cleanup
	fmt.Println("\nSession ended.")
}

func cmdList(args []string) {
	cfg := loadConfig()
	token := signRequest("GET", "/sessions", cfg.Server.AdminToken)

	req, _ := http.NewRequest("GET", serverURL()+"/sessions", nil)
	req.Header.Set("Authorization", "Admin "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result struct {
		Sessions []map[string]interface{} `json:"sessions"`
		Count    int                      `json:"count"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Count == 0 {
		fmt.Println("No active sessions")
		return
	}

	fmt.Printf("\n%-16s %-12s %-8s %-8s %-10s %-16s\n",
		"SESSION ID", "LABEL", "SSH", "TTYD", "STATE", "EXPIRES")
	fmt.Println(strings.Repeat("-", 80))

	for _, s := range result.Sessions {
		id := s["id"].(string)
		if len(id) > 14 {
			id = id[:14] + ".."
		}
		label, _ := s["label"].(string)
		if label == "" {
			label = "-"
		}
		if len(label) > 10 {
			label = label[:10] + ".."
		}
		sshPort := s["ssh_port"].(float64)
		ttydPort := s["ttyd_port"].(float64)
		state := s["state"].(string)

		fmt.Printf("%-16s %-12s %-8d %-8d %-10s %v\n",
			id, label, int(sshPort), int(ttydPort), state, s["expires_at"])
	}

	fmt.Printf("\nTotal: %d session(s)\n", result.Count)
}

func cmdKill(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: forge kill <session-id>")
		os.Exit(1)
	}

	sessionID := args[0]

	cfg := loadConfig()
	token := signRequest("DELETE", "/session/"+sessionID, cfg.Server.AdminToken)

	req, _ := http.NewRequest("DELETE", serverURL()+"/session/"+sessionID, nil)
	req.Header.Set("Authorization", "Admin "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		fmt.Printf("✅ Session %s killed\n", sessionID)
	} else {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		fmt.Fprintf(os.Stderr, "Error: %s\n", errResp["error"])
		os.Exit(1)
	}
}

func cmdLogs(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: forge logs <session-id>")
		os.Exit(1)
	}

	sessionID := args[0]
	cfg := loadConfig()

	logPath := filepath.Join(cfg.Session.WorkspaceDir, sessionID, "watchdog.log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Log file not found: %s\n", logPath)
		os.Exit(1)
	}

	// Tail the log file
	cmd := exec.Command("tail", "-f", logPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func cmdReplay(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: forge replay <session-id>")
		os.Exit(1)
	}

	sessionID := args[0]
	cfg := loadConfig()

	recordingPath := filepath.Join(cfg.Session.WorkspaceDir, sessionID, "workspace", "recording.cast")
	if _, err := os.Stat(recordingPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Recording not found: %s\n", recordingPath)
		os.Exit(1)
	}

	// Check if asciinema is installed
	if _, err := exec.LookPath("asciinema"); err != nil {
		fmt.Fprintln(os.Stderr, "asciinema not installed. Install with: apt install asciinema")
		os.Exit(1)
	}

	// Play with asciinema
	cmd := exec.Command("asciinema", "play", recordingPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func cmdRebuild(args []string) {
	cfg := loadConfig()
	token := signRequest("POST", "/image/rebuild", cfg.Server.AdminToken)

	req, _ := http.NewRequest("POST", serverURL()+"/image/rebuild", nil)
	req.Header.Set("Authorization", "Admin "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Stream output
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			break
		}
		fmt.Print(line)
	}

	fmt.Println("\n✅ Image rebuilt")
}

func cmdStatus(args []string) {
	resp, err := http.Get(serverURL() + "/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Server not responding: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("✅ Server status: %s\n", result["status"])
}

func cmdSign(args []string) {
	cfg := loadConfig()
	method := "POST"
	path := "/session/start"

	if len(args) >= 2 {
		method = args[0]
		path = args[1]
	}

	token := signRequest(method, path, cfg.Auth.Token)
	fmt.Println(token)
}

func serverURL() string {
	return "http://127.0.0.1:8765"
}

func loadConfig() *config.Config {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".forge", "config.toml")

	cfg := config.DefaultConfig()
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
	}
	return cfg
}

func signRequest(method, path, secret string) string {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	message := method + path + timestamp

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s:%s", signature, timestamp)
}
