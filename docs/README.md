# Forge - AI Agent Build Sandbox

**Forge** is a self-hosted daemon that lets AI agents run real Linux builds in isolated Docker containers on your personal machine — without ever touching the host.

## Why Forge?

AI agents (like Claude, ChatGPT, etc.) often run in restricted environments without system packages. They can't compile GTK4 applications, test GPU code, or access real hardware.

Forge solves this by:
- Exposing a secure HTTPS API (via Cloudflare Tunnel)
- Spinning up Docker containers with a full dev environment on demand
- Granting the agent a temporary SSH or web terminal session
- Monitoring everything with circuit breakers that auto-kill runaway processes
- Destroying the container when the session ends

**The host machine is sacred.** Every architectural decision is built around one question: "If this goes wrong, does the host survive?" The answer is always yes.

## Quick Start

### 1. Install Prerequisites

```bash
# Docker
sudo apt install docker.io
sudo usermod -aG docker $USER
sudo usermod -aG video $USER  # For camera access

# Go 1.22+
sudo apt install golang-go

# Cloudflare Tunnel (optional, for public access)
curl -L https://pkg.cloudflare.com/cloudflared/stable-linux-amd64.deb -o cloudflared.deb
sudo dpkg -i cloudflared.deb
```

### 2. Build Forge

```bash
cd forge

# Build server
go build -o forge-server .

# Build CLI
go build -o forge ./cmd/forge

# Install
sudo cp forge-server forge /usr/local/bin/
```

### 3. Build Docker Image

```bash
bash docker/iris-dev/build.sh
```

### 4. Configure

```bash
mkdir -p ~/.forge
cp config.toml.example ~/.forge/config.toml

# Edit config and generate tokens
nano ~/.forge/config.toml

# Generate tokens:
openssl rand -hex 32  # admin_token
openssl rand -hex 32  # session token
```

### 5. Run

```bash
# Start server
forge-server &

# Start tunnel (optional, for external access)
cloudflared tunnel --url http://localhost:8765 &

# Test
forge status
```

## Usage

### CLI Commands

```bash
# Start a session with GPU and camera
forge start --gpu --camera --time 90 --label "iris-build"

# List active sessions
forge list

# Kill a session
forge kill sess_abc123

# View session logs
forge logs sess_abc123

# Replay session recording
forge replay sess_abc123

# Rebuild Docker image
forge rebuild
```

### API Usage (for AI Agents)

```bash
# Generate signed token
TOKEN=$(forge-sign)

# Start session
curl -X POST https://your-tunnel.trycloudflare.com/session/start \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"ttl_minutes": 90, "gpu": true, "camera": true}'

# Response includes SSH credentials and web terminal URL
```

## Architecture

```
[ AI Agent (restricted) ]
         │
         │ HTTPS POST /session/start
         ▼
[ Cloudflare Tunnel ]
         │
         ▼
[ forge-server :8765 ] (localhost only)
         │
         │ Creates container with:
         │ - 4GB RAM limit
         │ - 2 CPU cores
         │ - 512 PID limit
         │ - No network by default
         ▼
[ Docker Container ]
   - iris-dev image
   - SSH on random port
   - Web terminal (ttyd)
   - GPU/Camera (if requested)
   - Watchdog monitoring
```

## Security Model

### What Forge Protects Against

✅ **Runaway builds** — CPU breaker kills after 60s at >90%
✅ **Memory leaks** — RAM breaker kills at 95% of limit
✅ **Fork bombs** — PID limit of 512
✅ **Disk filling** — 500MB write limit per session
✅ **Persistent access** — SSH keys deleted after session
✅ **Port scanning** — Network access restricted

### What Forge Does NOT Protect Against

⚠️ **Docker group = root equivalent** — If you're in the docker group, you can already get root
⚠️ **Kernel exploits** — Docker is not a VM; kernel bugs can escape containers
⚠️ **Malicious host user** — This is a single-user tool for your personal machine

**Do not run Forge on shared machines or servers you don't control.**

## Circuit Breakers

| Breaker | Condition | Action |
|---------|-----------|--------|
| `cpu-sustained` | CPU > 90% for 60 seconds | Kill container |
| `ram-pressure` | RAM > 95% of limit | Kill container |
| `pid-flood` | PIDs > 512 | Kill container |
| `disk-storm` | Disk writes > 500MB | Kill container |
| `network-scan` | >50 IPs in 10s | Kill container |

All triggers:
1. Log to `~/.forge/alarms.log`
2. Send desktop notification
3. POST to webhook (if configured)

## Device Passthrough

### GPU (AMD)

```bash
# Check devices
ls -la /dev/dri

# Container sees host GPU
docker run --rm --device /dev/dri iris-dev vulkaninfo --summary
```

### Camera (V4L2)

```bash
# Check devices
ls -la /dev/video*

# Container sees host camera
docker run --rm --device /dev/video0 iris-dev v4l2-ctl --list-devices
```

## Systemd Service

```bash
# Edit paths in systemd/forge.service
sed -i "s/YOUR_USER/$USER/g" systemd/forge.service

# Install
sudo cp systemd/forge*.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now forge

# Check status
systemctl status forge
```

## Troubleshooting

### "Docker daemon not accessible"
```bash
# Check Docker is running
sudo systemctl status docker

# Check user is in docker group
groups | grep docker

# Re-login if just added
newgrp docker
```

### "No available SSH ports"
- Check no other sessions are running: `forge list`
- Check port range in config: `ssh_port_range_start` to `ssh_port_range_end`

### "GPU not visible in container"
- Check host has `/dev/dri`: `ls -la /dev/dri`
- Check user is in `video` or `render` group: `groups`
- Start session with `--gpu` flag

### "Camera not visible in container"
- Check host has `/dev/video*`: `ls -la /dev/video*`
- Check user is in `video` group: `groups`
- Start session with `--camera` flag

## License

MIT

## Contributing

See CONTRIBUTING.md
