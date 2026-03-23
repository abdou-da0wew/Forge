# Agent Integration Guide

This guide explains exactly how an AI agent (Claude, ChatGPT, etc.) should use the Forge API.

## Quick Reference

```bash
# 1. Generate signed token
TOKEN=$(sign-request POST /session/start)

# 2. Start session
RESPONSE=$(curl -s -X POST https://<tunnel>/session/start \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"ttl_minutes":60,"gpu":true,"camera":true}')

# 3. Extract credentials
SSH_PORT=$(echo $RESPONSE | jq .ssh_port)
SSH_KEY=$(echo $RESPONSE | jq -r .ssh_private_key)

# 4. Connect and work
echo "$SSH_KEY" > /tmp/forge.key
chmod 600 /tmp/forge.key
ssh -i /tmp/forge.key -p $SSH_PORT forge@<tunnel>

# 5. Session auto-expires, or explicitly end
curl -X DELETE https://<tunnel>/session/<id> \
  -H "Authorization: Bearer $TOKEN"
```

## Authentication

Forge uses HMAC-SHA256 tokens to prevent:
- Unauthorized access
- Replay attacks
- Request tampering

### Token Format

```
<signature>:<timestamp>
```

- **signature**: HMAC-SHA256(secret, method + path + timestamp)
- **timestamp**: Unix epoch seconds

### Generating Tokens

```python
import hmac
import hashlib
import time

def sign_request(method, path, secret):
    timestamp = str(int(time.time()))
    message = method + path + timestamp
    signature = hmac.new(
        secret.encode(),
        message.encode(),
        hashlib.sha256
    ).hexdigest()
    return f"{signature}:{timestamp}"

# Usage
token = sign_request("POST", "/session/start", "your-secret-token")
```

```bash
# Bash
sign_request() {
    local method=$1
    local path=$2
    local secret=$3
    local timestamp=$(date +%s)
    local message="${method}${path}${timestamp}"
    local signature=$(echo -n "$message" | openssl dgst -sha256 -hmac "$secret" | awk '{print $2}')
    echo "${signature}:${timestamp}"
}
```

## API Endpoints

### POST /session/start

Start a new build session.

**Headers**:
```
Authorization: Bearer <token>
Content-Type: application/json
```

**Body**:
```json
{
  "ttl_minutes": 60,
  "gpu": true,
  "camera": false,
  "label": "iris-phase1-build"
}
```

**Response** (201):
```json
{
  "session_id": "sess_a1b2c3d4",
  "ssh_host": "forge.example.trycloudflare.com",
  "ssh_port": 32145,
  "ssh_user": "forge",
  "ssh_private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----\n",
  "web_terminal_url": "https://forge.example.trycloudflare.com/term/sess_a1b2c3d4",
  "web_terminal_password": "x7kR2mPq",
  "expires_at": "2025-03-23T15:30:00Z"
}
```

### DELETE /session/{id}

End a session early.

**Headers**:
```
Authorization: Bearer <token>
```

**Response** (204): Empty

### GET /session/{id}

Get session status.

**Response** (200):
```json
{
  "id": "sess_a1b2c3d4",
  "container_id": "abc123...",
  "label": "iris-build",
  "state": "running",
  "ssh_port": 32145,
  "ttyd_port": 33200,
  "gpu": true,
  "camera": false,
  "created_at": "2025-03-23T14:30:00Z",
  "expires_at": "2025-03-23T15:30:00Z",
  "stats": {
    "cpu_percent": 12.5,
    "ram_mb": 512,
    "pids": 42
  }
}
```

### GET /session/{id}/stats

Get current resource stats.

**Response** (200):
```json
{
  "cpu_percent": 85.2,
  "ram_mb": 1024,
  "pids": 156,
  "disk_write_mb": 45.3,
  "network_rx_mb": 2.1,
  "network_tx_mb": 0.5
}
```

### GET /session/{id}/replay

Download session recording (asciinema format).

**Response**: `.cast` file

### POST /session/{id}/kill (Admin)

Forcefully kill a session.

**Headers**:
```
Authorization: Admin <admin-token>
```

### POST /image/rebuild (Admin)

Rebuild the Docker image.

**Headers**:
```
Authorization: Admin <admin-token>
```

**Response**: Streaming build output

## Typical Session Flow

### 1. Request Session

```bash
# Sign request
TOKEN=$(sign-request POST /session/start)

# Start session
curl -X POST https://$FORGE_HOST/session/start \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @- <<EOF
{
  "ttl_minutes": 90,
  "gpu": true,
  "camera": true,
  "label": "iris-dev"
}
EOF
```

### 2. Save Credentials

```bash
# Parse response
SESSION_ID=$(jq -r .session_id)
SSH_PORT=$(jq -r .ssh_port)
SSH_KEY=$(jq -r .ssh_private_key)

# Save key
echo "$SSH_KEY" > /tmp/forge_session.key
chmod 600 /tmp/forge_session.key
```

### 3. Connect

```bash
# SSH connection
ssh -i /tmp/forge_session.key \
    -p $SSH_PORT \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    forge@$FORGE_HOST
```

### 4. Work Inside Container

```bash
# Inside container
cd /workspace

# Clone project
git clone https://github.com/yourproject

# Build
cargo build --release

# Test with GPU
cargo test gpu::tests

# Test with camera
cargo test camera::enumerator
```

### 5. Cleanup

```bash
# Exit container
exit

# Delete session (optional - auto-expires)
TOKEN=$(sign-request DELETE /session/$SESSION_ID)
curl -X DELETE https://$FORGE_HOST/session/$SESSION_ID \
  -H "Authorization: Bearer $TOKEN"

# Remove local key
rm /tmp/forge_session.key
```

## Error Handling

### 401 Unauthorized
- Invalid or expired token
- Regenerate token with fresh timestamp

### 429 Too Many Requests
- Maximum concurrent sessions reached
- Wait for existing sessions to expire or kill one

### 403 Forbidden
- GPU/camera passthrough not allowed by config
- Request without required flags

### 503 Service Unavailable
- No available ports
- Docker daemon not accessible
- Image not found

## Web Terminal Alternative

If SSH is not available, use the web terminal:

1. Open `web_terminal_url` in a browser
2. Enter `web_terminal_password`
3. Interactive shell appears

## Timeouts

- Request token max age: 5 minutes
- Session max TTL: 120 minutes
- Connection timeout: 30 seconds
- Watchdog poll: 5 seconds

## Best Practices

1. **Use minimum TTL needed** — Don't request 120 minutes for a 5-minute build
2. **Enable GPU/Camera only when needed** — Reduces attack surface
3. **Clean up explicitly** — End sessions when done, don't wait for expiry
4. **Handle disconnections** — Reconnect logic for dropped SSH sessions
5. **Check stats periodically** — Monitor resource usage via `/stats` endpoint
