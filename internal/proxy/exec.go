package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"nhooyr.io/websocket"
)

// runExec handles non-interactive command execution
func (c *Client) runExec(ctx context.Context, conn *websocket.Conn, ack *AckFrame) int {
	// Create buffered writer for stdout
	stdout := bufio.NewWriter(os.Stdout)
	defer stdout.Flush()

	// Read frames in a loop
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			// Check for close frame with exit code
			status := websocket.CloseStatus(err)
			if status != -1 {
				return c.parseExitCode(err, status)
			}
			fmt.Fprintf(os.Stderr, "[forge] Connection error: %v\n", err)
			return 1
		}

		switch msgType {
		case websocket.MessageText:
			// Control message
			var frame map[string]interface{}
			if err := json.Unmarshal(data, &frame); err != nil {
				continue
			}

			frameType, _ := frame["type"].(string)
			switch frameType {
			case "kill":
				return c.handleKillFrame(frame)
			case "exit":
				return c.handleExitFrame(frame)
			}

		case websocket.MessageBinary:
			// PTY output - write to stdout
			stdout.Write(data)
			stdout.Flush()
		}
	}
}

// handleKillFrame processes a kill frame from the server
func (c *Client) handleKillFrame(frame map[string]interface{}) int {
	reason, _ := frame["reason"].(string)
	sessionID, _ := frame["session_id"].(string)

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "[forge] !! Session killed by Forge server")
	fmt.Fprintf(os.Stderr, "[forge] Reason: %s (%s)\n", reason, GetBreakerReason(reason))
	fmt.Fprintf(os.Stderr, "[forge] Session ID: %s\n", sessionID)
	fmt.Fprintln(os.Stderr, "[forge] Partial output may be missing.")

	return 137 // SIGKILL exit code
}

// handleExitFrame processes an exit frame from the server
func (c *Client) handleExitFrame(frame map[string]interface{}) int {
	code, _ := frame["code"].(float64)
	return int(code)
}

// parseExitCode extracts exit code from WebSocket close error
func (c *Client) parseExitCode(err error, status websocket.StatusCode) int {
	// Try to parse exit code from close message
	closeErr, ok := err.(*websocket.CloseError)
	if ok {
		// Check for exit code in reason
		reason := string(closeErr.Reason)
		if len(reason) > 5 && reason[:5] == "exit:" {
			var code int
			fmt.Sscanf(reason[5:], "%d", &code)
			return code
		}
	}

	// Map WebSocket status codes to exit codes
	switch status {
	case websocket.StatusNormalClosure:
		return 0
	case websocket.StatusGoingAway:
		return 0
	default:
		return 1
	}
}

// sendHandshake sends the initial handshake frame
func (c *Client) sendHandshake(ctx context.Context, conn *websocket.Conn, handshake HandshakeFrame) error {
	data, err := json.Marshal(handshake)
	if err != nil {
		return fmt.Errorf("failed to marshal handshake: %w", err)
	}

	return conn.Write(ctx, websocket.MessageText, data)
}

// waitForAck waits for the server's acknowledgment
func (c *Client) waitForAck(ctx context.Context, conn *websocket.Conn) (*AckFrame, error) {
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read ack: %w", err)
	}

	if msgType != websocket.MessageText {
		return nil, fmt.Errorf("expected text frame for ack, got binary")
	}

	var ack AckFrame
	if err := json.Unmarshal(data, &ack); err != nil {
		return nil, fmt.Errorf("failed to parse ack: %w", err)
	}

	if ack.Type != "ready" {
		// Check if it's an error
		var errFrame struct {
			Type  string `json:"type"`
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &errFrame) == nil && errFrame.Error != "" {
			return nil, fmt.Errorf("server error: %s", errFrame.Error)
		}
		return nil, fmt.Errorf("unexpected frame type: %s", ack.Type)
	}

	return &ack, nil
}
