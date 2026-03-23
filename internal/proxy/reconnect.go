package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"nhooyr.io/websocket"
)

// ReconnectConfig holds reconnection settings
type ReconnectConfig struct {
	MaxAttempts  int           // Maximum reconnection attempts
	InitialDelay time.Duration // Initial delay before first reconnect
	MaxDelay     time.Duration // Maximum delay between attempts
}

// DefaultReconnectConfig returns default reconnection settings
func DefaultReconnectConfig() ReconnectConfig {
	return ReconnectConfig{
		MaxAttempts:  5,
		InitialDelay: 2 * time.Second,
		MaxDelay:     32 * time.Second,
	}
}

// ReconnectLoop handles reconnection with exponential backoff
func (c *Client) ReconnectLoop(ctx context.Context, sessionID string, cfg ReconnectConfig) (*websocket.Conn, error) {
	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		fmt.Fprintf(os.Stderr, "[forge] Connection lost. Reconnecting... (attempt %d/%d)\n",
			attempt, cfg.MaxAttempts)

		// Wait with exponential backoff
		time.Sleep(delay)
		delay = delay * 2
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}

		// Try to reconnect
		wsURL := c.normalizeURL(c.cfg.ServerURL)
		wsURL = wsURL + "/ws/session?resume=" + sessionID

		opts := &websocket.DialOptions{
			HTTPHeader: http.Header{
				"Authorization": {"Bearer " + c.cfg.Token},
			},
		}

		conn, _, err := websocket.Dial(ctx, wsURL, opts)
		if err != nil {
			lastErr = err
			continue
		}

		// Send resize frame with current terminal size
		cols, rows := getTerminalSize()
		resize := ResizeFrame{
			Type: "resize",
			Cols: cols,
			Rows: rows,
		}

		data, err := json.Marshal(resize)
		if err != nil {
			conn.Close(websocket.StatusInternalError, "resize failed")
			lastErr = err
			continue
		}

		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			conn.Close(websocket.StatusInternalError, "resize failed")
			lastErr = err
			continue
		}

		fmt.Fprintf(os.Stderr, "[forge] Reconnected to session %s\n", sessionID)
		return conn, nil
	}

	return nil, fmt.Errorf("could not reconnect after %d attempts: %w", cfg.MaxAttempts, lastErr)
}
