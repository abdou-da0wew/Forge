# Forge

**Self-Hosted AI Agent Development Environment Daemon**

Forge is a self-hosted daemon that exposes a secure API over Cloudflare Tunnel, allowing AI agents to spin up Docker containers with full development stacks on demand. It grants temporary sandboxed terminal sessions with hardware limits and circuit breakers for automatic kill triggers.

## Features

- **On-Demand Containers**: Spins up Docker containers with full Iris development stack
- **Secure Tunneling**: Exposes API over Cloudflare Tunnel (no port forwarding needed)
- **Ephemeral SSH Access**: Temporary SSH sessions with auto-generated ed25519 keypairs
- **Circuit Breakers**: 5 automatic kill triggers (CPU, RAM, PID, disk, network scan)
- **Hardware Passthrough**: GPU and V4L2 camera device passthrough support
- **Web Terminal**: ttyd-based web terminal for browser access
- **Session Management**: Full session lifecycle with logging and replay

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         Forge Daemon                        │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ HTTP Server │  │  Watchdog   │  │  Container Manager  │  │
│  │ (HMAC Auth) │  │ (Breakers)  │  │   (Docker SDK)      │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         │                │                     │            │
│         └────────────────┴─────────────────────┘            │
│                          │                                  │
│  ┌───────────────────────┴───────────────────────────────┐  │
│  │              Cloudflare Tunnel (HTTPS)                │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                             │
                             ▼
                    ┌────────────────┐
                    │  Docker Engine │
                    │  (iris-dev)    │
                    └────────────────┘
```

## Quick Start

```bash
# Clone the repository
git clone https://github.com/abdou-da0wew/Forge.git
cd Forge

# Build
go build -o forge ./cmd/forge

# Configure
cp config.toml.example config.toml
# Edit config.toml with your settings

# Run
./forge
```

## Configuration

```toml
[forge]
listen = "127.0.0.1:8080"
secret = "your-hmac-secret-key"
tunnel_token = "your-cloudflare-tunnel-token"

[container]
image = "iris-dev:latest"
cpu_limit = 4        # cores
memory_limit = 8192  # MB
pid_limit = 4096
disk_limit = 10240   # MB
max_sessions = 5

[watchdog]
check_interval = 5   # seconds
cpu_threshold = 90   # percent
memory_threshold = 90
pid_threshold = 80
disk_threshold = 85
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/session/create` | POST | Create new development session |
| `/session/{id}` | GET | Get session status |
| `/session/{id}/kill` | POST | Kill session |
| `/session/{id}/ssh` | GET | Get SSH connection info |

## Circuit Breakers

Forge implements 5 automatic kill triggers:

1. **CPU Breaker**: Kills if CPU usage exceeds threshold for duration
2. **Memory Breaker**: Kills if memory usage exceeds threshold
3. **PID Breaker**: Kills if process count exceeds limit
4. **Disk Breaker**: Kills if disk usage exceeds threshold
5. **Network Scan Breaker**: Kills on suspicious network activity

## Security

- HMAC-SHA256 authentication for all API requests
- Ephemeral ed25519 SSH keypairs (auto-rotated)
- Container isolation with resource limits
- Automatic session termination on violations
- Audit logging for all operations

## Built By

**Divee** - Autonomous AI Development Agent  
Part of the [Triecbot](https://triecbot.xyz/divee) ecosystem.

> *"If a task is done twice, automate it. If a system can run itself, build an agent."*

## License

MIT License
