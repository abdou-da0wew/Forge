package server

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "sync"
        "time"

        "github.com/abdou/forge/internal/container"
        "github.com/abdou/forge/internal/session"
        "github.com/docker/docker/api/types"
        dockercontainer "github.com/docker/docker/api/types/container"
        "github.com/rs/zerolog/log"
        "nhooyr.io/websocket"
)

// handleWebSocketSession handles WebSocket connections from forge-proxy
func (s *Server) handleWebSocketSession(w http.ResponseWriter, r *http.Request) {
        // Validate auth token from header
        authHeader := r.Header.Get("Authorization")
        if authHeader == "" {
                http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
                return
        }

        // Parse "Bearer <token>"
        token := ""
        if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
                token = authHeader[7:]
        }

        // Validate token (simple check - in production use HMAC validation)
        if token == "" || (token != s.cfg.Auth.Token && token != s.cfg.Server.AdminToken) {
                http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
                return
        }

        // Check for resume parameter
        resumeID := r.URL.Query().Get("resume")
        if resumeID != "" {
                s.handleWebSocketResume(w, r, resumeID)
                return
        }

        // Accept WebSocket connection
        conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
                CompressionMode: websocket.CompressionDisabled,
        })
        if err != nil {
                log.Error().Err(err).Msg("WebSocket accept failed")
                return
        }
        defer conn.Close(websocket.StatusInternalError, "closing")

        ctx, cancel := context.WithCancel(r.Context())
        defer cancel()

        // Read handshake frame
        msgType, data, err := conn.Read(ctx)
        if err != nil {
                log.Error().Err(err).Msg("Failed to read handshake")
                return
        }

        if msgType != websocket.MessageText {
                conn.Close(websocket.StatusPolicyViolation, "expected text frame for handshake")
                return
        }

        var handshake struct {
                Type       string `json:"type"`
                ExecCmd    string `json:"exec_cmd"`
                GPU        bool   `json:"gpu"`
                Camera     bool   `json:"camera"`
                TTLMinutes int    `json:"ttl_minutes"`
                Label      string `json:"label"`
                TermCols   uint16 `json:"term_cols"`
                TermRows   uint16 `json:"term_rows"`
        }

        if err := json.Unmarshal(data, &handshake); err != nil {
                conn.Close(websocket.StatusPolicyViolation, "invalid handshake format")
                return
        }

        if handshake.Type != "hello" {
                conn.Close(websocket.StatusPolicyViolation, "expected hello frame")
                return
        }

        // Create session
        sessionID := session.GenerateID()

        // Allocate ports
        sshPort, err := s.sessionStore.AllocateSSHPort()
        if err != nil {
                s.sendErrorFrame(conn, "no available ports")
                return
        }

        ttydPort, err := s.sessionStore.AllocateTTYDPort()
        if err != nil {
                s.sessionStore.ReleasePort(sshPort)
                s.sendErrorFrame(conn, "no available ports")
                return
        }

        // Start container
        containerResp, err := s.containerMgr.Start(ctx, container.StartRequest{
                SessionID:    sessionID,
                SSHPort:      sshPort,
                TTYDPort:     ttydPort,
                GPU:          handshake.GPU,
                Camera:       handshake.Camera,
                TTLMinutes:   handshake.TTLMinutes,
                WorkspaceDir: s.cfg.Session.WorkspaceDir,
        })
        if err != nil {
                s.sessionStore.ReleasePort(sshPort)
                s.sessionStore.ReleasePort(ttydPort)
                s.sendErrorFrame(conn, fmt.Sprintf("failed to start container: %s", err))
                return
        }

        // Create session record
        sess := &session.Session{
                ID:          sessionID,
                ContainerID: containerResp.ContainerID,
                Label:       handshake.Label,
                CreatedAt:   time.Now(),
                ExpiresAt:   time.Now().Add(time.Duration(handshake.TTLMinutes) * time.Minute),
                SSHPort:     sshPort,
                TTYDPort:    ttydPort,
                GPU:         handshake.GPU,
                Camera:      handshake.Camera,
                State:       session.StateRunning,
        }
        s.sessionStore.Add(sess)

        // Send ack frame
        ack := map[string]interface{}{
                "type":         "ready",
                "session_id":   sessionID,
                "container_id": containerResp.ContainerID,
                "gpu_active":   handshake.GPU && s.cfg.Passthrough.AllowGPU,
        }
        ackData, _ := json.Marshal(ack)
        if err := conn.Write(ctx, websocket.MessageText, ackData); err != nil {
                s.containerMgr.Kill(containerResp.ContainerID)
                s.sessionStore.Delete(sessionID)
                return
        }

        log.Info().
                Str("session_id", sessionID).
                Str("container_id", containerResp.ContainerID[:12]).
                Bool("gpu", handshake.GPU).
                Bool("camera", handshake.Camera).
                Msg("WebSocket session started")

        // Create exec in container
        execCmd := handshake.ExecCmd
        if execCmd == "" {
                execCmd = "bash" // Interactive mode
        }

        // Run exec and pipe I/O
        exitCode := s.runContainerExec(ctx, conn, containerResp.ContainerID, execCmd, handshake.TermCols, handshake.TermRows)

        // Send exit frame
        exitFrame := map[string]interface{}{
                "type":       "exit",
                "code":       exitCode,
                "session_id": sessionID,
        }
        exitData, _ := json.Marshal(exitFrame)
        conn.Write(ctx, websocket.MessageText, exitData)

        // Close connection
        conn.Close(websocket.StatusNormalClosure, fmt.Sprintf("exit:%d", exitCode))

        // Cleanup
        s.containerMgr.Kill(containerResp.ContainerID)
        s.sessionStore.Delete(sessionID)

        log.Info().
                Str("session_id", sessionID).
                Int("exit_code", exitCode).
                Msg("WebSocket session ended")
}

// handleWebSocketResume handles reconnection to existing session
func (s *Server) handleWebSocketResume(w http.ResponseWriter, r *http.Request, sessionID string) {
        // For now, just return error - full resume requires keeping exec alive
        http.Error(w, `{"error":"session resume not implemented"}`, http.StatusNotImplemented)
}

// runContainerExec runs a command in the container and pipes I/O through WebSocket
func (s *Server) runContainerExec(ctx context.Context, wsConn *websocket.Conn, containerID, cmd string, cols, rows uint16) int {
        // Create exec instance
        execConfig := types.ExecConfig{
                AttachStdin:  true,
                AttachStdout: true,
                AttachStderr: true,
                Tty:          true,
                Cmd:          []string{"sh", "-c", cmd},
        }

        execResp, err := s.containerMgr.GetClient().ContainerExecCreate(ctx, containerID, execConfig)
        if err != nil {
                log.Error().Err(err).Msg("Failed to create exec")
                return 1
        }

        // Attach to exec
        attachResp, err := s.containerMgr.GetClient().ContainerExecAttach(ctx, execResp.ID, types.ExecStartCheck{Tty: true})
        if err != nil {
                log.Error().Err(err).Msg("Failed to attach to exec")
                return 1
        }
        defer attachResp.Close()

        // Start exec
        if err := s.containerMgr.GetClient().ContainerExecStart(ctx, execResp.ID, types.ExecStartCheck{}); err != nil {
                log.Error().Err(err).Msg("Failed to start exec")
                return 1
        }

        var wg sync.WaitGroup
        exitCode := 0

        // Goroutine: WebSocket -> Container stdin
        wg.Add(1)
        go func() {
                defer wg.Done()
                for {
                        msgType, data, err := wsConn.Read(ctx)
                        if err != nil {
                                return
                        }

                        switch msgType {
                        case websocket.MessageBinary:
                                // Write to container stdin
                                attachResp.Conn.Write(data)
                        case websocket.MessageText:
                                // Handle control frames
                                var frame map[string]interface{}
                                if json.Unmarshal(data, &frame) == nil {
                                        if frame["type"] == "resize" {
                                                // Handle resize
                                                cols, _ := frame["cols"].(float64)
                                                rows, _ := frame["rows"].(float64)
                                                if cols > 0 && rows > 0 {
                                                        s.containerMgr.GetClient().ContainerExecResize(ctx, execResp.ID, dockercontainer.ResizeOptions{
                                                                Height: uint(rows),
                                                                Width:  uint(cols),
                                                        })
                                                }
                                        }
                                }
                        }
                }
        }()

        // Goroutine: Container stdout -> WebSocket
        wg.Add(1)
        go func() {
                defer wg.Done()
                buf := make([]byte, 4096)
                for {
                        n, err := attachResp.Reader.Read(buf)
                        if err != nil {
                                if err != io.EOF {
                                        log.Debug().Err(err).Msg("Container read error")
                                }
                                return
                        }
                        if n > 0 {
                                if err := wsConn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
                                        return
                                }
                        }
                }
        }()

        // Wait for exec to complete
        for {
                inspect, err := s.containerMgr.GetClient().ContainerExecInspect(ctx, execResp.ID)
                if err != nil {
                        break
                }
                if !inspect.Running {
                        exitCode = inspect.ExitCode
                        break
                }
                time.Sleep(100 * time.Millisecond)
        }

        // Cancel context to stop goroutines
        wg.Wait()

        return exitCode
}

// sendErrorFrame sends an error frame to the client
func (s *Server) sendErrorFrame(conn *websocket.Conn, errMsg string) {
        errFrame := map[string]interface{}{
                "type":  "error",
                "error": errMsg,
        }
        data, _ := json.Marshal(errFrame)
        conn.Write(context.Background(), websocket.MessageText, data)
}
