package proxy

// Frame types for the forge-proxy ↔ forge-server wire protocol

// HandshakeFrame is sent by proxy to server on connection
type HandshakeFrame struct {
	Type       string `json:"type"`        // "hello"
	Token      string `json:"token"`       // Deprecated: use header instead
	ExecCmd    string `json:"exec_cmd"`    // Empty if interactive
	GPU        bool   `json:"gpu"`
	Camera     bool   `json:"camera"`
	TTLMinutes int    `json:"ttl_minutes"`
	Label      string `json:"label"`
	TermCols   uint16 `json:"term_cols"`
	TermRows   uint16 `json:"term_rows"`
}

// AckFrame is sent by server to confirm session is ready
type AckFrame struct {
	Type        string `json:"type"`        // "ready"
	SessionID   string `json:"session_id"`
	ContainerID string `json:"container_id"`
	GPUActive   bool   `json:"gpu_active"`
}

// KillFrame is sent by server when circuit breaker triggers
type KillFrame struct {
	Type      string `json:"type"`   // "kill"
	Reason    string `json:"reason"` // "cpu-sustained" | "ram-pressure" | ...
	SessionID string `json:"session_id"`
}

// ResizeFrame is sent by proxy when terminal resizes
type ResizeFrame struct {
	Type string `json:"type"` // "resize"
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// ExitFrame is sent by server before closing with exit code
type ExitFrame struct {
	Type     string `json:"type"`  // "exit"
	Code     int    `json:"code"`  // Exit code
	SessionID string `json:"session_id"`
}

// BreakerReasonText maps breaker reasons to human-readable messages
var BreakerReasonText = map[string]string{
	"cpu-sustained":  "CPU usage exceeded 90% for 60 seconds",
	"ram-pressure":   "RAM usage exceeded container limit",
	"pid-flood":      "Process count exceeded 512",
	"disk-storm":     "Disk write rate exceeded 500MB for session",
	"network-scan":   "Suspicious network scanning detected",
	"admin-kill":     "Manually terminated by machine owner",
	"ttl-expired":    "Session time limit reached",
}

// GetBreakerReason returns human-readable reason text
func GetBreakerReason(reason string) string {
	if text, ok := BreakerReasonText[reason]; ok {
		return text
	}
	return "Unknown reason: " + reason
}
