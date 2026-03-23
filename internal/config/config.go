package config

import (
        "errors"
        "fmt"
        "os"
        "path/filepath"

        "github.com/BurntSushi/toml"
)

// Config is the main configuration structure
type Config struct {
        Server     ServerConfig     `toml:"server"`
        Auth       AuthConfig       `toml:"auth"`
        Docker     DockerConfig     `toml:"docker"`
        Session    SessionConfig    `toml:"session"`
        Watchdog   WatchdogConfig   `toml:"watchdog"`
        Alarm      AlarmConfig      `toml:"alarm"`
        Passthrough PassthroughConfig `toml:"passthrough"`
}

// ServerConfig holds server-specific settings
type ServerConfig struct {
        ListenAddr string `toml:"listen_addr"`
        LogLevel   string `toml:"log_level"`
        AdminToken string `toml:"admin_token"`
        TunnelHost string `toml:"tunnel_host"`
}

// AuthConfig holds authentication settings
type AuthConfig struct {
        Token              string `toml:"token"`
        MaxRequestAgeSeconds int   `toml:"max_request_age_seconds"`
}

// DockerConfig holds Docker-related settings
type DockerConfig struct {
        Image  string `toml:"image"`
        Socket string `toml:"socket"`
}

// SessionConfig holds session management settings
type SessionConfig struct {
        DefaultTTLMinutes    int    `toml:"default_ttl_minutes"`
        MaxTTLMinutes        int    `toml:"max_ttl_minutes"`
        WorkspaceDir         string `toml:"workspace_dir"`
        MaxConcurrent        int    `toml:"max_concurrent"`
        SSHPortRangeStart    int    `toml:"ssh_port_range_start"`
        SSHPortRangeEnd      int    `toml:"ssh_port_range_end"`
        TTYDPortRangeStart   int    `toml:"ttyd_port_range_start"`
        TTYDPortRangeEnd     int    `toml:"ttyd_port_range_end"`
        MemoryLimitGB        int    `toml:"memory_limit_gb"`
        CPULimit             int    `toml:"cpu_limit"`
        PIDLimit             int    `toml:"pid_limit"`
}

// WatchdogConfig holds watchdog settings
type WatchdogConfig struct {
        PollIntervalSeconds        int `toml:"poll_interval_seconds"`
        CPUSustainedThresholdPct   int `toml:"cpu_sustained_threshold_pct"`
        CPUSustainedDurationSeconds int `toml:"cpu_sustained_duration_seconds"`
        RAMThresholdPct            int `toml:"ram_threshold_pct"`
        PIDLimit                   int `toml:"pid_limit"`
        DiskWriteLimitMB           int `toml:"disk_write_limit_mb"`
        NetworkScanIPCount         int `toml:"network_scan_ip_count"`
        NetworkScanWindowSeconds   int `toml:"network_scan_window_seconds"`
}

// AlarmConfig holds alarm/notification settings
type AlarmConfig struct {
        LogFile             string `toml:"log_file"`
        DesktopNotify       bool   `toml:"desktop_notify"`
        WebhookURL          string `toml:"webhook_url"`
        WebhookTimeoutSeconds int   `toml:"webhook_timeout_seconds"`
}

// PassthroughConfig holds device passthrough settings
type PassthroughConfig struct {
        AllowGPU         bool     `toml:"allow_gpu"`
        AllowCamera      bool     `toml:"allow_camera"`
        GPUDevicePaths   []string `toml:"gpu_device_paths"`
        CameraDevicePaths []string `toml:"camera_device_paths"`
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *Config {
        home, _ := os.UserHomeDir()
        
        return &Config{
                Server: ServerConfig{
                        ListenAddr: "127.0.0.1:8765",
                        LogLevel:   "info",
                },
                Auth: AuthConfig{
                        MaxRequestAgeSeconds: 300,
                },
                Docker: DockerConfig{
                        Image:  "iris-dev:latest",
                        Socket: "/var/run/docker.sock",
                },
                Session: SessionConfig{
                        DefaultTTLMinutes:  60,
                        MaxTTLMinutes:      120,
                        WorkspaceDir:       filepath.Join(home, ".forge", "sessions"),
                        MaxConcurrent:      3,
                        SSHPortRangeStart:  32000,
                        SSHPortRangeEnd:    33000,
                        TTYDPortRangeStart: 33000,
                        TTYDPortRangeEnd:   34000,
                        MemoryLimitGB:      4,
                        CPULimit:           2,
                        PIDLimit:           512,
                },
                Watchdog: WatchdogConfig{
                        PollIntervalSeconds:         5,
                        CPUSustainedThresholdPct:    90,
                        CPUSustainedDurationSeconds: 60,
                        RAMThresholdPct:             95,
                        PIDLimit:                    512,
                        DiskWriteLimitMB:            500,
                        NetworkScanIPCount:          50,
                        NetworkScanWindowSeconds:    10,
                },
                Alarm: AlarmConfig{
                        LogFile:             filepath.Join(home, ".forge", "alarms.log"),
                        DesktopNotify:       true,
                        WebhookTimeoutSeconds: 10,
                },
                Passthrough: PassthroughConfig{
                        AllowGPU:          true,
                        AllowCamera:       true,
                        GPUDevicePaths:    []string{"/dev/dri/card0", "/dev/dri/renderD128"},
                        CameraDevicePaths: []string{"/dev/video0"},
                },
        }
}

// Load reads config from file or returns defaults
func Load(path string) (*Config, error) {
        cfg := DefaultConfig()
        
        // Determine config path
        if path == "" {
                home, _ := os.UserHomeDir()
                path = filepath.Join(home, ".forge", "config.toml")
        }
        
        // Check if file exists
        if _, err := os.Stat(path); os.IsNotExist(err) {
                return nil, fmt.Errorf("config file not found: %s (copy config.toml.example)", path)
        }
        
        // Parse TOML
        if _, err := toml.DecodeFile(path, cfg); err != nil {
                return nil, fmt.Errorf("failed to parse config: %w", err)
        }
        
        // Validate
        if err := cfg.Validate(); err != nil {
                return nil, fmt.Errorf("config validation failed: %w", err)
        }
        
        return cfg, nil
}

// Validate checks that config values are valid
func (c *Config) Validate() error {
        // Server
        if c.Server.ListenAddr == "" {
                return errors.New("server.listen_addr is required")
        }
        if c.Server.AdminToken == "" {
                return errors.New("server.admin_token is required (generate with: openssl rand -hex 32)")
        }
        if len(c.Server.AdminToken) < 32 {
                return errors.New("server.admin_token too short (minimum 32 characters)")
        }
        
        // Auth
        if c.Auth.Token == "" {
                return errors.New("auth.token is required (generate with: openssl rand -hex 32)")
        }
        if len(c.Auth.Token) < 32 {
                return errors.New("auth.token too short (minimum 32 characters)")
        }
        
        // Session
        if c.Session.DefaultTTLMinutes < 5 {
                return errors.New("session.default_ttl_minutes must be at least 5")
        }
        if c.Session.MaxTTLMinutes < c.Session.DefaultTTLMinutes {
                return errors.New("session.max_ttl_minutes must be >= default_ttl_minutes")
        }
        if c.Session.MaxConcurrent < 1 {
                return errors.New("session.max_concurrent must be at least 1")
        }
        if c.Session.SSHPortRangeStart >= c.Session.SSHPortRangeEnd {
                return errors.New("session.ssh_port_range_start must be < ssh_port_range_end")
        }
        
        // Docker
        if c.Docker.Image == "" {
                return errors.New("docker.image is required")
        }
        
        // Watchdog
        if c.Watchdog.PollIntervalSeconds < 1 {
                return errors.New("watchdog.poll_interval_seconds must be at least 1")
        }
        
        return nil
}
