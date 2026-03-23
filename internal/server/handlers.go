package server

import (
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "time"

        "github.com/abdou/forge/internal/alarm"
        "github.com/abdou/forge/internal/container"
        "github.com/abdou/forge/internal/session"
        "github.com/abdou/forge/internal/watchdog"
        "github.com/google/uuid"
        "github.com/rs/zerolog/log"
)

// SessionStartRequest is the request body for starting a session
type SessionStartRequest struct {
        TTLMinutes int    `json:"ttl_minutes"`
        GPU        bool   `json:"gpu"`
        Camera     bool   `json:"camera"`
        Label      string `json:"label"`
}

// SessionStartResponse is returned when a session starts
type SessionStartResponse struct {
        SessionID        string    `json:"session_id"`
        SSHHost          string    `json:"ssh_host"`
        SSHPort          int       `json:"ssh_port"`
        SSHUser          string    `json:"ssh_user"`
        SSHPrivateKey    string    `json:"ssh_private_key"`
        WebTerminalURL   string    `json:"web_terminal_url"`
        WebTerminalPass  string    `json:"web_terminal_password"`
        ExpiresAt        time.Time `json:"expires_at"`
}

// handleSessionStart creates a new build session
func (s *Server) handleSessionStart(w http.ResponseWriter, r *http.Request) {
        var req SessionStartRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
                return
        }

        // Validate TTL
        ttl := s.cfg.Session.DefaultTTLMinutes
        if req.TTLMinutes > 0 {
                ttl = req.TTLMinutes
        }
        if ttl > s.cfg.Session.MaxTTLMinutes {
                ttl = s.cfg.Session.MaxTTLMinutes
        }
        if ttl < 5 {
                ttl = 5
        }

        // Check concurrent limit
        if s.sessionStore.Count() >= s.cfg.Session.MaxConcurrent {
                http.Error(w, `{"error":"maximum concurrent sessions reached"}`, http.StatusTooManyRequests)
                return
        }

        // Check GPU/Camera permissions
        if req.GPU && !s.cfg.Passthrough.AllowGPU {
                http.Error(w, `{"error":"GPU passthrough not allowed"}`, http.StatusForbidden)
                return
        }
        if req.Camera && !s.cfg.Passthrough.AllowCamera {
                http.Error(w, `{"error":"Camera passthrough not allowed"}`, http.StatusForbidden)
                return
        }

        // Generate session ID
        sessionID := session.GenerateID()

        // Allocate ports
        sshPort, err := s.sessionStore.AllocateSSHPort()
        if err != nil {
                http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusServiceUnavailable)
                return
        }

        ttydPort, err := s.sessionStore.AllocateTTYDPort()
        if err != nil {
                http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusServiceUnavailable)
                return
        }

        // Generate SSH keypair
        privateKey, publicKey, err := session.GenerateKeypair()
        if err != nil {
                http.Error(w, `{"error":"failed to generate keypair"}`, http.StatusInternalServerError)
                return
        }

        // Generate ttyd password
        ttydPassword := uuid.New().String()[:8]

        // Create container
        containerResp, err := s.containerMgr.Start(r.Context(), container.StartRequest{
                SessionID:    sessionID,
                SSHPort:      sshPort,
                TTYDPort:     ttydPort,
                PublicKey:    publicKey,
                GPU:          req.GPU,
                Camera:       req.Camera,
                TTLMinutes:   ttl,
                WorkspaceDir: s.cfg.Session.WorkspaceDir,
        })
        if err != nil {
                http.Error(w, fmt.Sprintf(`{"error":"failed to start container: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        // Create session record
        sess := &session.Session{
                ID:         sessionID,
                ContainerID: containerResp.ContainerID,
                Label:      req.Label,
                CreatedAt:  time.Now(),
                ExpiresAt:  time.Now().Add(time.Duration(ttl) * time.Minute),
                SSHPort:    sshPort,
                TTYDPort:   ttydPort,
                PrivateKey: privateKey,
                PublicKey:  publicKey,
                GPU:        req.GPU,
                Camera:     req.Camera,
                State:      session.StateRunning,
        }

        // Save private key to disk
        if err := session.SavePrivateKey(s.cfg.Session.WorkspaceDir, sessionID, privateKey); err != nil {
                log.Warn().Err(err).Msg("Failed to save private key")
        }

        // Add to store
        if err := s.sessionStore.Add(sess); err != nil {
                s.containerMgr.Kill(containerResp.ContainerID)
                http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusServiceUnavailable)
                return
        }

        // Start watchdog
        wd := watchdog.New(sessionID, containerResp.ContainerID, s.containerMgr, s.alarmDispatcher, &s.cfg.Watchdog)
        wd.Start()

        // Start TTL timer
        go s.startTTLTimer(sess, wd)

        // Build response
        tunnelHost := s.cfg.Server.TunnelHost
        if tunnelHost == "" {
                tunnelHost = "localhost"
        }

        resp := SessionStartResponse{
                SessionID:       sessionID,
                SSHHost:         tunnelHost,
                SSHPort:         sshPort,
                SSHUser:         "forge",
                SSHPrivateKey:   privateKey,
                WebTerminalURL:  fmt.Sprintf("https://%s/term/%s", tunnelHost, sessionID),
                WebTerminalPass: ttydPassword,
                ExpiresAt:       sess.ExpiresAt,
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusCreated)
        json.NewEncoder(w).Encode(resp)

        log.Info().
                Str("session_id", sessionID).
                Int("ssh_port", sshPort).
                Int("ttl", ttl).
                Bool("gpu", req.GPU).
                Bool("camera", req.Camera).
                Msg("Session started")
}

// handleSessionEnd terminates a session
func (s *Server) handleSessionEnd(w http.ResponseWriter, r *http.Request) {
        sessionID := r.PathValue("id")
        if sessionID == "" {
                http.Error(w, `{"error":"missing session id"}`, http.StatusBadRequest)
                return
        }

        sess, ok := s.sessionStore.Get(sessionID)
        if !ok {
                http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
                return
        }

        // Kill container
        if err := s.containerMgr.Kill(sess.ContainerID); err != nil {
                log.Warn().Err(err).Str("session", sessionID).Msg("Error killing container")
        }

        // Delete private key
        session.DeletePrivateKey(s.cfg.Session.WorkspaceDir, sessionID)

        // Remove from store
        s.sessionStore.Delete(sessionID)

        w.WriteHeader(http.StatusNoContent)

        log.Info().Str("session_id", sessionID).Msg("Session ended")
}

// handleSessionGet returns session details
func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
        sessionID := r.PathValue("id")
        if sessionID == "" {
                http.Error(w, `{"error":"missing session id"}`, http.StatusBadRequest)
                return
        }

        sess, ok := s.sessionStore.Get(sessionID)
        if !ok {
                http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(sess)
}

// handleSessionStats returns current session stats
func (s *Server) handleSessionStats(w http.ResponseWriter, r *http.Request) {
        sessionID := r.PathValue("id")
        if sessionID == "" {
                http.Error(w, `{"error":"missing session id"}`, http.StatusBadRequest)
                return
        }

        sess, ok := s.sessionStore.Get(sessionID)
        if !ok {
                http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
                return
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(sess.Stats)
}

// handleSessionReplay returns the session recording
func (s *Server) handleSessionReplay(w http.ResponseWriter, r *http.Request) {
        sessionID := r.PathValue("id")
        if sessionID == "" {
                http.Error(w, `{"error":"missing session id"}`, http.StatusBadRequest)
                return
        }

        // Recording is stored in workspace
        recordingPath := fmt.Sprintf("%s/%s/workspace/recording.cast", s.cfg.Session.WorkspaceDir, sessionID)
        
        http.ServeFile(w, r, recordingPath)
}

// handleSessionKill forcibly kills a session (admin only)
func (s *Server) handleSessionKill(w http.ResponseWriter, r *http.Request) {
        sessionID := r.PathValue("id")
        if sessionID == "" {
                http.Error(w, `{"error":"missing session id"}`, http.StatusBadRequest)
                return
        }

        sess, ok := s.sessionStore.Get(sessionID)
        if !ok {
                http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
                return
        }

        // Kill container
        if err := s.containerMgr.Kill(sess.ContainerID); err != nil {
                log.Warn().Err(err).Str("session", sessionID).Msg("Error killing container")
        }

        // Delete private key
        session.DeletePrivateKey(s.cfg.Session.WorkspaceDir, sessionID)

        // Remove from store
        s.sessionStore.Delete(sessionID)

        // Dispatch alarm
        s.alarmDispatcher.Dispatch(alarm.Event{
                Type:      alarm.EventTypeKill,
                SessionID: sessionID,
                Reason:    "Manual kill via API",
                Timestamp: time.Now(),
        })

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{
                "killed":     true,
                "session_id": sessionID,
        })

        log.Info().Str("session_id", sessionID).Msg("Session killed by admin")
}

// handleSessionList returns all active sessions (admin only)
func (s *Server) handleSessionList(w http.ResponseWriter, r *http.Request) {
        sessions := s.sessionStore.List()

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]interface{}{
                "sessions": sessions,
                "count":    len(sessions),
        })
}

// handleImageRebuild rebuilds the Docker image (admin only)
func (s *Server) handleImageRebuild(w http.ResponseWriter, r *http.Request) {
        // Check for active sessions
        if s.sessionStore.Count() > 0 {
                http.Error(w, `{"error":"cannot rebuild with active sessions"}`, http.StatusConflict)
                return
        }

        w.Header().Set("Content-Type", "text/plain")
        w.WriteHeader(http.StatusOK)

        flusher, ok := w.(http.Flusher)
        if !ok {
                http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
                return
        }

        // Build image
        if err := s.containerMgr.BuildImage(r.Context(), "docker/iris-dev", w); err != nil {
                fmt.Fprintf(w, "\n\nError: %s\n", err)
                return
        }

        flusher.Flush()
        fmt.Fprintf(w, "\n\nImage rebuilt successfully!\n")
}

// handleWebTerminal proxies web terminal requests
func (s *Server) handleWebTerminal(w http.ResponseWriter, r *http.Request) {
        sessionID := r.PathValue("id")
        if sessionID == "" {
                http.Error(w, `{"error":"missing session id"}`, http.StatusBadRequest)
                return
        }

        sess, ok := s.sessionStore.Get(sessionID)
        if !ok {
                http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
                return
        }

        // Reverse proxy to ttyd
        target := fmt.Sprintf("http://127.0.0.1:%d", sess.TTYDPort)
        
        // Simple proxy
        resp, err := http.Get(target + r.URL.Path[len("/term/"+sessionID):])
        if err != nil {
                http.Error(w, `{"error":"ttyd not reachable"}`, http.StatusBadGateway)
                return
        }
        defer resp.Body.Close()

        for k, v := range resp.Header {
                w.Header()[k] = v
        }
        w.WriteHeader(resp.StatusCode)
        io.Copy(w, resp.Body)
}

// handleWebTerminalWS handles WebSocket for web terminal
func (s *Server) handleWebTerminalWS(w http.ResponseWriter, r *http.Request) {
        // WebSocket proxying is complex; this is a placeholder
        // In production, use gorilla/websocket or similar
        http.Error(w, `{"error":"WebSocket proxying not implemented"}`, http.StatusNotImplemented)
}

// startTTLTimer handles session expiration
func (s *Server) startTTLTimer(sess *session.Session, wd *watchdog.Watchdog) {
        time.Sleep(time.Until(sess.ExpiresAt))

        // Check if session still exists
        if _, ok := s.sessionStore.Get(sess.ID); !ok {
                return // Already killed
        }

        log.Info().Str("session_id", sess.ID).Msg("Session TTL expired")

        // Stop watchdog
        wd.Stop()

        // Kill container
        s.containerMgr.Kill(sess.ContainerID)

        // Delete private key
        session.DeletePrivateKey(s.cfg.Session.WorkspaceDir, sess.ID)

        // Remove from store
        s.sessionStore.Delete(sess.ID)

        // Dispatch alarm
        s.alarmDispatcher.Dispatch(alarm.Event{
                Type:      alarm.EventTypeExpire,
                SessionID: sess.ID,
                Reason:    fmt.Sprintf("TTL expired after %d minutes", s.cfg.Session.DefaultTTLMinutes),
                Timestamp: time.Now(),
        })
}
