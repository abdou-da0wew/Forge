package alarm

import (
        "bytes"
        "context"
        "encoding/json"
        "fmt"
        "net/http"
        "os"
        "os/exec"
        "path/filepath"
        "sync"
        "time"

        "github.com/abdou/forge/internal/config"
        "github.com/rs/zerolog/log"
)

// EventType categorizes alarm events
type EventType string

const (
        EventTypeBreaker   EventType = "breaker"
        EventTypeKill      EventType = "kill"
        EventTypeExpire    EventType = "expire"
        EventTypeError     EventType = "error"
)

// Event represents an alarm event
type Event struct {
        Type      EventType `json:"type"`
        SessionID string    `json:"session_id"`
        Breaker   string    `json:"breaker,omitempty"`
        Reason    string    `json:"reason"`
        Timestamp time.Time `json:"timestamp"`
}

// Dispatcher handles alarm notifications
type Dispatcher struct {
        cfg     *config.Config
        logFile *os.File
        mu      sync.Mutex
}

// NewDispatcher creates a new alarm dispatcher
func NewDispatcher(cfg *config.Config) *Dispatcher {
        d := &Dispatcher{cfg: cfg}

        // Open log file
        if cfg.Alarm.LogFile != "" {
                if err := os.MkdirAll(filepath.Dir(cfg.Alarm.LogFile), 0755); err != nil {
                        log.Warn().Err(err).Msg("Failed to create alarm log directory")
                } else {
                        f, err := os.OpenFile(cfg.Alarm.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
                        if err != nil {
                                log.Warn().Err(err).Msg("Failed to open alarm log file")
                        } else {
                                d.logFile = f
                        }
                }
        }

        return d
}

// Dispatch sends an alarm through all configured channels
func (d *Dispatcher) Dispatch(event Event) {
        event.Timestamp = time.Now()

        // Log to file
        d.logToFile(event)

        // Desktop notification
        if d.cfg.Alarm.DesktopNotify {
                go d.sendDesktopNotify(event)
        }

        // Webhook
        if d.cfg.Alarm.WebhookURL != "" {
                go d.sendWebhook(event)
        }

        // Structured log
        log.Warn().
                Str("type", string(event.Type)).
                Str("session", event.SessionID).
                Str("breaker", event.Breaker).
                Str("reason", event.Reason).
                Msg("Alarm dispatched")
}

func (d *Dispatcher) logToFile(event Event) {
        if d.logFile == nil {
                return
        }

        d.mu.Lock()
        defer d.mu.Unlock()

        data, err := json.Marshal(event)
        if err != nil {
                log.Warn().Err(err).Msg("Failed to marshal alarm event")
                return
        }

        d.logFile.Write(data)
        d.logFile.Write([]byte("\n"))
}

func (d *Dispatcher) sendDesktopNotify(event Event) {
        title := "Forge Alert"
        body := fmt.Sprintf("Session %s: %s", event.SessionID[:8], event.Reason)
        
        if event.Type == EventTypeBreaker {
                title = fmt.Sprintf("Forge Circuit Breaker: %s", event.Breaker)
        }

        cmd := exec.Command("notify-send", "-u", "critical", title, body)
        cmd.Env = append(os.Environ(), "DISPLAY=:0")
        
        if err := cmd.Run(); err != nil {
                log.Debug().Err(err).Msg("Desktop notification failed (this is OK in headless environments)")
        }
}

func (d *Dispatcher) sendWebhook(event Event) {
        ctx, cancel := context.WithTimeout(context.Background(), 
                time.Duration(d.cfg.Alarm.WebhookTimeoutSeconds)*time.Second)
        defer cancel()

        data, err := json.Marshal(event)
        if err != nil {
                log.Warn().Err(err).Msg("Failed to marshal webhook payload")
                return
        }

        req, err := http.NewRequestWithContext(ctx, "POST", d.cfg.Alarm.WebhookURL, bytes.NewReader(data))
        if err != nil {
                log.Warn().Err(err).Msg("Failed to create webhook request")
                return
        }

        req.Header.Set("Content-Type", "application/json")

        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                log.Warn().Err(err).Str("url", d.cfg.Alarm.WebhookURL).Msg("Webhook POST failed")
                return
        }
        defer resp.Body.Close()

        if resp.StatusCode >= 400 {
                log.Warn().Int("status", resp.StatusCode).Msg("Webhook returned error status")
        }
}

// Close cleans up resources
func (d *Dispatcher) Close() error {
        if d.logFile != nil {
                return d.logFile.Close()
        }
        return nil
}
