package server

import (
	"context"
	"net/http"
	"time"

	"github.com/abdou/forge/internal/alarm"
	"github.com/abdou/forge/internal/config"
	"github.com/abdou/forge/internal/container"
	"github.com/abdou/forge/internal/session"
	"github.com/rs/zerolog/log"
)

// Server is the main Forge HTTP server
type Server struct {
	cfg            *config.Config
	httpServer     *http.Server
	containerMgr   *container.Manager
	sessionStore   *session.Store
	alarmDispatcher *alarm.Dispatcher
}

// New creates a new Forge server
func New(cfg *config.Config) (*Server, error) {
	// Create session store
	sessionStore := session.NewStore(cfg)
	
	// Create container manager
	containerMgr, err := container.NewManager(cfg)
	if err != nil {
		return nil, err
	}
	
	// Create alarm dispatcher
	alarmDispatcher := alarm.NewDispatcher(cfg)
	
	// Create HTTP server
	mux := http.NewServeMux()
	
	s := &Server{
		cfg: cfg,
		containerMgr: containerMgr,
		sessionStore: sessionStore,
		alarmDispatcher: alarmDispatcher,
		httpServer: &http.Server{
			Addr:         cfg.Server.ListenAddr,
			Handler:      mux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}
	
	// Register routes
	s.registerRoutes(mux)
	
	return s, nil
}

// registerRoutes sets up all HTTP routes
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Health check (no auth required)
	mux.HandleFunc("GET /health", s.handleHealth)
	
	// Session endpoints (require session auth)
	sessionAuth := s.authMiddleware(false)
	mux.HandleFunc("POST /session/start", sessionAuth(s.handleSessionStart))
	mux.HandleFunc("DELETE /session/{id}", sessionAuth(s.handleSessionEnd))
	mux.HandleFunc("GET /session/{id}", sessionAuth(s.handleSessionGet))
	mux.HandleFunc("GET /session/{id}/stats", sessionAuth(s.handleSessionStats))
	mux.HandleFunc("GET /session/{id}/replay", sessionAuth(s.handleSessionReplay))
	
	// Admin endpoints (require admin auth)
	adminAuth := s.authMiddleware(true)
	mux.HandleFunc("POST /session/{id}/kill", adminAuth(s.handleSessionKill))
	mux.HandleFunc("GET /sessions", adminAuth(s.handleSessionList))
	mux.HandleFunc("POST /image/rebuild", adminAuth(s.handleImageRebuild))
	
	// Web terminal proxy (dynamic, checked per-request)
	mux.HandleFunc("GET /term/{id}/", s.handleWebTerminal)
	mux.HandleFunc("GET /term/{id}/ws", s.handleWebTerminalWS)
	mux.HandleFunc("POST /term/{id}/", s.handleWebTerminal)
}

// Start begins listening for connections
func (s *Server) Start() error {
	log.Info().Str("addr", s.cfg.Server.ListenAddr).Msg("HTTP server started")
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info().Msg("Shutting down server...")
	
	// Kill all active sessions
	sessions := s.sessionStore.List()
	for _, sess := range sessions {
		log.Info().Str("session_id", sess.ID).Msg("Killing session during shutdown")
		if err := s.containerMgr.Kill(sess.ContainerID); err != nil {
			log.Error().Err(err).Str("session_id", sess.ID).Msg("Failed to kill container")
		}
		s.sessionStore.Delete(sess.ID)
	}
	
	// Shutdown HTTP server
	return s.httpServer.Shutdown(ctx)
}

// handleHealth returns server health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
