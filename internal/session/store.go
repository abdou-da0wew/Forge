package session

import (
        "crypto/ed25519"
        "crypto/rand"
        "encoding/pem"
        "fmt"
        "os"
        "path/filepath"
        "sync"
        "time"

        "github.com/abdou/forge/internal/config"
        "github.com/rs/zerolog/log"
        "golang.org/x/crypto/ssh"
)

// Session represents an active build session
type Session struct {
        ID           string            `json:"id"`
        ContainerID  string            `json:"container_id"`
        Label        string            `json:"label"`
        CreatedAt    time.Time         `json:"created_at"`
        ExpiresAt    time.Time         `json:"expires_at"`
        SSHPort      int               `json:"ssh_port"`
        TTYDPort     int               `json:"ttyd_port"`
        PrivateKey   string            `json:"-"`
        PublicKey    string            `json:"-"`
        GPU          bool              `json:"gpu"`
        Camera       bool              `json:"camera"`
        State        SessionState      `json:"state"`
        Stats        SessionStats      `json:"stats"`
}

// SessionState represents the current state of a session
type SessionState string

const (
        StateStarting SessionState = "starting"
        StateRunning  SessionState = "running"
        StateEnding   SessionState = "ending"
        StateKilled   SessionState = "killed"
        StateExpired  SessionState = "expired"
)

// SessionStats holds runtime statistics
type SessionStats struct {
        CPUPercent    float64 `json:"cpu_percent"`
        RAMMB         int64   `json:"ram_mb"`
        PIDs          int     `json:"pids"`
        DiskWriteMB   float64 `json:"disk_write_mb"`
        NetworkRxMB   float64 `json:"network_rx_mb"`
        NetworkTxMB   float64 `json:"network_tx_mb"`
}

// Store manages all active sessions
type Store struct {
        cfg     *config.Config
        sessions map[string]*Session
        mu       sync.RWMutex
        portMap  map[int]bool // Track used ports
}

// NewStore creates a new session store
func NewStore(cfg *config.Config) *Store {
        return &Store{
                cfg:      cfg,
                sessions: make(map[string]*Session),
                portMap:  make(map[int]bool),
        }
}

// Add creates and stores a new session
func (s *Store) Add(sess *Session) error {
        s.mu.Lock()
        defer s.mu.Unlock()

        if len(s.sessions) >= s.cfg.Session.MaxConcurrent {
                return fmt.Errorf("maximum concurrent sessions reached (%d)", s.cfg.Session.MaxConcurrent)
        }

        s.sessions[sess.ID] = sess
        s.portMap[sess.SSHPort] = true
        s.portMap[sess.TTYDPort] = true

        return nil
}

// Get retrieves a session by ID
func (s *Store) Get(id string) (*Session, bool) {
        s.mu.RLock()
        defer s.mu.RUnlock()

        sess, ok := s.sessions[id]
        return sess, ok
}

// Delete removes a session
func (s *Store) Delete(id string) {
        s.mu.Lock()
        defer s.mu.Unlock()

        if sess, ok := s.sessions[id]; ok {
                delete(s.portMap, sess.SSHPort)
                delete(s.portMap, sess.TTYDPort)
                delete(s.sessions, id)
        }
}

// List returns all active sessions
func (s *Store) List() []*Session {
        s.mu.RLock()
        defer s.mu.RUnlock()

        result := make([]*Session, 0, len(s.sessions))
        for _, sess := range s.sessions {
                result = append(result, sess)
        }
        return result
}

// Count returns the number of active sessions
func (s *Store) Count() int {
        s.mu.RLock()
        defer s.mu.RUnlock()
        return len(s.sessions)
}

// AllocateSSHPort finds an available SSH port
func (s *Store) AllocateSSHPort() (int, error) {
        s.mu.Lock()
        defer s.mu.Unlock()

        for port := s.cfg.Session.SSHPortRangeStart; port <= s.cfg.Session.SSHPortRangeEnd; port++ {
                if !s.portMap[port] {
                        s.portMap[port] = true
                        return port, nil
                }
        }

        return 0, fmt.Errorf("no available SSH ports in range %d-%d",
                s.cfg.Session.SSHPortRangeStart, s.cfg.Session.SSHPortRangeEnd)
}

// AllocateTTYDPort finds an available TTYD port
func (s *Store) AllocateTTYDPort() (int, error) {
        s.mu.Lock()
        defer s.mu.Unlock()

        for port := s.cfg.Session.TTYDPortRangeStart; port <= s.cfg.Session.TTYDPortRangeEnd; port++ {
                if !s.portMap[port] {
                        s.portMap[port] = true
                        return port, nil
                }
        }

        return 0, fmt.Errorf("no available TTYD ports in range %d-%d",
                s.cfg.Session.TTYDPortRangeStart, s.cfg.Session.TTYDPortRangeEnd)
}

// UpdateStats updates the stats for a session
func (s *Store) UpdateStats(id string, stats SessionStats) {
        s.mu.Lock()
        defer s.mu.Unlock()

        if sess, ok := s.sessions[id]; ok {
                sess.Stats = stats
        }
}

// SetState updates the state of a session
func (s *Store) SetState(id string, state SessionState) {
        s.mu.Lock()
        defer s.mu.Unlock()

        if sess, ok := s.sessions[id]; ok {
                sess.State = state
        }
}

// ReleasePort releases a port back to the pool
func (s *Store) ReleasePort(port int) {
        s.mu.Lock()
        defer s.mu.Unlock()
        delete(s.portMap, port)
}

// GenerateID creates a new session ID
func GenerateID() string {
        b := make([]byte, 8)
        rand.Read(b)
        return fmt.Sprintf("sess_%x", b)
}

// GenerateKeypair creates an ed25519 keypair for SSH authentication
func GenerateKeypair() (privateKeyPEM, authorizedKey string, err error) {
        // Generate ed25519 key
        pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
        if err != nil {
                return "", "", fmt.Errorf("failed to generate key: %w", err)
        }

        // Create SSH public key
        sshPubKey, err := ssh.NewPublicKey(pubKey)
        if err != nil {
                return "", "", fmt.Errorf("failed to create SSH public key: %w", err)
        }
        authorizedKey = string(ssh.MarshalAuthorizedKey(sshPubKey))

        // Create PEM block for private key
        pemBlock := &pem.Block{
                Type:  "OPENSSH PRIVATE KEY",
                Bytes: privKey.Seed(),
        }
        privateKeyPEM = string(pem.EncodeToMemory(pemBlock))

        return privateKeyPEM, authorizedKey, nil
}

// SavePrivateKey writes the private key to the session directory
func SavePrivateKey(workspaceDir, sessionID, privateKey string) error {
        keyPath := filepath.Join(workspaceDir, sessionID, "private_key")
        
        if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
                return err
        }
        
        if err := os.WriteFile(keyPath, []byte(privateKey), 0600); err != nil {
                return err
        }
        
        log.Debug().Str("path", keyPath).Msg("Saved private key")
        return nil
}

// DeletePrivateKey removes the private key file
func DeletePrivateKey(workspaceDir, sessionID string) error {
        keyPath := filepath.Join(workspaceDir, sessionID, "private_key")
        
        if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
                return err
        }
        
        log.Debug().Str("path", keyPath).Msg("Deleted private key")
        return nil
}
