package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/term"
	"nhooyr.io/websocket"
)

// runInteractive handles interactive PTY session
func (c *Client) runInteractive(ctx context.Context, conn *websocket.Conn, ack *AckFrame) int {
	// Save original terminal state
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[forge] Warning: could not set raw mode: %v\n", err)
	} else {
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGWINCH)

	var exitCode int = 0
	var wg sync.WaitGroup

	// Goroutine: stdin -> WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := os.Stdin.Read(buf)
				if err != nil {
					cancel()
					return
				}
				if n > 0 {
					if err := conn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
						cancel()
						return
					}
				}
			}
		}
	}()

	// Goroutine: WebSocket -> stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				msgType, data, err := conn.Read(ctx)
				if err != nil {
					// Check for close frame
					status := websocket.CloseStatus(err)
					if status != -1 {
						exitCode = c.parseExitCode(err, status)
					}
					cancel()
					return
				}

				switch msgType {
				case websocket.MessageText:
					var frame map[string]interface{}
					if json.Unmarshal(data, &frame) == nil {
						frameType, _ := frame["type"].(string)
						switch frameType {
						case "kill":
							exitCode = c.handleKillFrameInteractive(frame)
							cancel()
							return
						case "exit":
							exitCode = c.handleExitFrame(frame)
							cancel()
							return
						case "resize":
							// Server acknowledged resize, ignore
						}
					}
				case websocket.MessageBinary:
					os.Stdout.Write(data)
				}
			}
		}
	}()

	// Signal handler
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case sig := <-sigChan:
				switch sig {
				case syscall.SIGWINCH:
					// Terminal resize
					cols, rows := getTerminalSize()
					resize := ResizeFrame{
						Type: "resize",
						Cols: cols,
						Rows: rows,
					}
					data, _ := json.Marshal(resize)
					conn.Write(ctx, websocket.MessageText, data)
				case syscall.SIGINT:
					// Ctrl+C - send to container
					conn.Write(ctx, websocket.MessageBinary, []byte{0x03})
				default:
					// Other signals - cleanup
					cancel()
					return
				}
			}
		}
	}()

	// Wait for completion
	wg.Wait()

	// Restore terminal
	if oldState != nil {
		term.Restore(int(os.Stdin.Fd()), oldState)
	}

	return exitCode
}

// handleKillFrameInteractive handles kill frame in interactive mode
func (c *Client) handleKillFrameInteractive(frame map[string]interface{}) int {
	// Restore terminal first
	term.Restore(int(os.Stdin.Fd()), nil)

	reason, _ := frame["reason"].(string)
	sessionID, _ := frame["session_id"].(string)

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "[forge] !! Session killed by Forge server")
	fmt.Fprintf(os.Stderr, "[forge] Reason: %s (%s)\n", reason, GetBreakerReason(reason))
	fmt.Fprintf(os.Stderr, "[forge] Session ID: %s\n", sessionID)

	return 137
}

// getTerminalSizePlatform returns terminal size using golang.org/x/term
func getTerminalSizePlatform() (uint16, uint16, error) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0, 0, err
	}
	return uint16(width), uint16(height), nil
}
