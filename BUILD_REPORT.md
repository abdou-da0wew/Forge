# Forge - AI Agent Build Sandbox

## Build Status: ✅ SUCCESS

### Compiled Binaries
- `forge-server` (10.9 MB) - Main HTTP server daemon
- `forge` (7.9 MB) - Command-line client

### Project Statistics
- **Go source files**: 11
- **Total Go code**: 2,307 lines
- **Total files**: 25

---

## Project Structure

```
forge/
├── forge-server          # Main server binary
├── forge                 # CLI binary
├── main.go               # Server entry point
├── go.mod / go.sum       # Go module files
├── config.toml.example   # Configuration template
│
├── internal/
│   ├── config/
│   │   └── config.go     # Configuration loading/validation (213 lines)
│   ├── server/
│   │   ├── server.go     # HTTP server setup (115 lines)
│   │   ├── auth.go       # HMAC token authentication (113 lines)
│   │   └── handlers.go   # API handlers (414 lines)
│   ├── container/
│   │   └── manager.go    # Docker lifecycle management (350 lines)
│   ├── session/
│   │   └── store.go      # Session management/keygen (184 lines)
│   ├── watchdog/
│   │   ├── watchdog.go   # Resource monitoring (120 lines)
│   │   └── breakers.go   # Circuit breaker logic (110 lines)
│   └── alarm/
│       └── alarm.go      # Alarm/notification dispatcher (140 lines)
│
├── cmd/forge/
│   └── main.go           # CLI tool (300 lines)
│
├── docker/iris-dev/
│   ├── Dockerfile        # Full dev environment image
│   ├── entrypoint.sh     # Container startup script
│   └── build.sh          # Image build script
│
├── systemd/
│   ├── forge.service     # Server systemd unit
│   └── forge-tunnel.service  # Cloudflare tunnel unit
│
└── docs/
    ├── README.md         # Full documentation
    ├── SECURITY.md       # Security model
    ├── CIRCUIT_BREAKERS.md  # Breaker documentation
    └── AGENT_GUIDE.md    # AI agent integration guide
```

---

## Implemented Features

### Phase 1 - Core Infrastructure ✅
- [x] Go module with dependencies (Docker SDK, zerolog, toml, uuid)
- [x] Configuration loader with validation
- [x] HTTP server with graceful shutdown
- [x] HMAC-SHA256 token authentication with replay protection
- [x] Session API endpoints (start/end/kill/list/stats)
- [x] Docker container lifecycle management
- [x] Ephemeral SSH key generation (ed25519)
- [x] Session store with concurrent access control

### Phase 2 - Safety Systems ✅
- [x] Watchdog goroutine per session
- [x] 5 circuit breakers:
  - CPU sustained (>90% for 60s)
  - RAM pressure (>95% of limit)
  - PID flood (>512 processes)
  - Disk storm (>500MB writes)
  - Network scan detection (placeholder)
- [x] Alarm dispatcher:
  - JSON log file
  - Desktop notifications (notify-send)
  - Webhook POST

### Phase 3 - Device Passthrough ✅
- [x] GPU device detection and passthrough
- [x] Camera device detection and passthrough
- [x] Configurable device paths

### Phase 4 - Packaging ✅
- [x] Docker image Dockerfile (Ubuntu 24.04 + full dev stack)
- [x] Container entrypoint with SSH + ttyd
- [x] Systemd service files
- [x] CLI tool (start/list/kill/status/sign)
- [x] Complete documentation

---

## Docker Image (iris-dev)

The Docker image includes:
- Ubuntu 24.04 base
- GTK4 + libadwaita development libraries
- Rust stable toolchain
- FFmpeg development libraries
- V4L2 development libraries
- Vulkan development libraries
- TurboJPEG
- Meson + Ninja build tools
- Blueprint compiler
- OpenSSH server
- ttyd web terminal

---

## How to Deploy

### 1. Build and Install

```bash
# Copy to system
sudo cp forge forge-server /usr/local/bin/

# Create config directory
mkdir -p ~/.forge
cp config.toml.example ~/.forge/config.toml

# Edit config and set tokens
nano ~/.forge/config.toml
# Generate tokens: openssl rand -hex 32

# Build Docker image
bash docker/iris-dev/build.sh

# Start server
forge-server
```

### 2. Systemd (Production)

```bash
# Edit service file
sed -i "s/YOUR_USER/$USER/g" systemd/forge.service
sudo cp systemd/*.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now forge
```

### 3. Cloudflare Tunnel (Public Access)

```bash
# Install cloudflared
sudo apt install cloudflared

# Quick tunnel (random URL)
cloudflared tunnel --url http://localhost:8765

# Or persistent tunnel (requires Cloudflare account)
cloudflared tunnel create forge-tunnel
cloudflared tunnel route dns forge-tunnel forge.yourdomain.com
cloudflared tunnel run forge-tunnel
```

---

## API Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/health` | GET | None | Health check |
| `/session/start` | POST | Session | Start new session |
| `/session/{id}` | GET | Session | Get session info |
| `/session/{id}` | DELETE | Session | End session |
| `/session/{id}/stats` | GET | Session | Resource stats |
| `/session/{id}/replay` | GET | Session | Session recording |
| `/session/{id}/kill` | POST | Admin | Force kill session |
| `/sessions` | GET | Admin | List all sessions |
| `/image/rebuild` | POST | Admin | Rebuild Docker image |

---

## CLI Usage

```bash
# Start session
forge start --gpu --camera --time 90 --label "iris-build"

# List sessions
forge list

# Kill session
forge kill sess_abc123

# Check server status
forge status

# Generate signed token
forge sign
```

---

## Next Steps for Production

1. **Security audit** - Review all code paths for security issues
2. **Integration testing** - Test with real Docker daemon
3. **AI agent integration** - Connect from your AI environment
4. **Monitoring** - Set up log aggregation and alerts
5. **Backup strategy** - Backup config and session recordings

---

## File Checksums

```
forge-server:  SHA256 to be calculated
forge:         SHA256 to be calculated
```

---

*Generated: 2025-03-23*
