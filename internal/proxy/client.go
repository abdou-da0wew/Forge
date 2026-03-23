package proxy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

// Config holds proxy client configuration
type Config struct {
	ServerURL   string
	Token       string
	ExecCmd     string
	Interactive bool
	TTLMinutes  int
	GPU         bool
	Camera      bool
	Label       string
}

// Client is the forge-proxy client
type Client struct {
	cfg Config
}

// NewClient creates a new proxy client
func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg}
}

// Run connects to the Forge server and runs the session
func (c *Client) Run() int {
	// Print startup message
	c.printStartup()

	// Normalize URL (https:// -> wss://)
	wsURL := c.normalizeURL(c.cfg.ServerURL)
	wsURL = wsURL + "/ws/session"

	// Get terminal size
	cols, rows := getTerminalSize()

	// Create context with timeout for connection
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build handshake frame
	handshake := HandshakeFrame{
		Type:       "hello",
		ExecCmd:    c.cfg.ExecCmd,
		GPU:        c.cfg.GPU,
		Camera:     c.cfg.Camera,
		TTLMinutes: c.cfg.TTLMinutes,
		Label:      c.cfg.Label,
		TermCols:   cols,
		TermRows:   rows,
	}

	// Connect to server
	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": {"Bearer " + c.cfg.Token},
		},
	}

	fmt.Fprintf(os.Stderr, "[forge] Connecting to %s...\n", wsURL)

	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[forge] Connection failed: %v\n", err)
		return 1
	}
	defer conn.Close(websocket.StatusInternalError, "closing")

	// Send handshake
	if err := c.sendHandshake(ctx, conn, handshake); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] Handshake failed: %v\n", err)
		return 1
	}

	// Wait for ack
	ack, err := c.waitForAck(ctx, conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[forge] Server error: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "[forge] Session %s ready — gpu:%v camera:%v\n",
		ack.SessionID, ack.GPUActive, c.cfg.Camera)
	fmt.Fprintf(os.Stderr, "[forge] Container: %s\n", ack.ContainerID[:12])

	if c.cfg.ExecCmd != "" {
		fmt.Fprintf(os.Stderr, "[forge] Running: %s\n", c.cfg.ExecCmd)
	}

	// Run the appropriate mode
	if c.cfg.Interactive {
		return c.runInteractive(ctx, conn, ack)
	}
	return c.runExec(ctx, conn, ack)
}

// normalizeURL converts https:// to wss:// for WebSocket
func (c *Client) normalizeURL(url string) string {
	url = strings.TrimSpace(url)
	if strings.HasPrefix(url, "https://") {
		return "wss://" + strings.TrimPrefix(url, "https://")
	}
	if strings.HasPrefix(url, "http://") {
		return "ws://" + strings.TrimPrefix(url, "http://")
	}
	if !strings.HasPrefix(url, "ws://") && !strings.HasPrefix(url, "wss://") {
		// Assume https if no scheme
		return "wss://" + url
	}
	return url
}

// printStartup prints the startup banner
func (c *Client) printStartup() {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "╔══════════════════════════════════════════╗")
	fmt.Fprintln(os.Stderr, "║          forge-proxy v1.0.0              ║")
	fmt.Fprintln(os.Stderr, "║   AI Agent Build Environment Client      ║")
	fmt.Fprintln(os.Stderr, "╚══════════════════════════════════════════╝")
	fmt.Fprintln(os.Stderr)
}

// getTerminalSize returns the current terminal dimensions
func getTerminalSize() (cols, rows uint16) {
	cols, rows = 80, 24 // defaults

	// Try to get actual size
	if width, height, err := getTerminalDimensions(); err == nil {
		cols = width
		rows = height
	}

	return cols, rows
}

// getTerminalDimensions is platform-specific (implemented in interactive.go)
func getTerminalDimensions() (uint16, uint16, error) {
	// This will be implemented using golang.org/x/term
	return getTerminalSizePlatform()
}
