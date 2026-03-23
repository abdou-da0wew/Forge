# Forge Proxy Protocol Specification

Version: 1.0.0

## Overview

The Forge Proxy Protocol defines the wire format for communication between `forge-proxy` (the client running in restricted AI agent environments) and `forge-server` (the daemon on the user's personal machine).

## Connection

1. **Transport**: WebSocket over HTTPS (WSS)
2. **Endpoint**: `GET /ws/session`
3. **Authentication**: Bearer token in `Authorization` header

## Frame Types

All frames are WebSocket messages. Text frames contain JSON control messages. Binary frames contain raw PTY data.

### Client → Server Frames

#### Handshake Frame (Text)

Sent immediately after WebSocket connection.

```json
{
    "type": "hello",
    "exec_cmd": "cargo build",
    "gpu": false,
    "camera": false,
    "ttl_minutes": 60,
    "label": "iris-build",
    "term_cols": 220,
    "term_rows": 50
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| type | string | Yes | Must be "hello" |
| exec_cmd | string | No | Command to run (empty for interactive mode) |
| gpu | bool | No | Request GPU passthrough |
| camera | bool | No | Request camera passthrough |
| ttl_minutes | int | No | Session TTL (default: 60) |
| label | string | No | Session label for identification |
| term_cols | uint16 | No | Terminal width (default: 80) |
| term_rows | uint16 | No | Terminal height (default: 24) |

#### Resize Frame (Text)

Sent when terminal size changes.

```json
{
    "type": "resize",
    "cols": 220,
    "rows": 50
}
```

#### PTY Input (Binary)

Raw bytes from stdin. Sent as binary WebSocket frames.

### Server → Client Frames

#### Ack Frame (Text)

Sent after successful session creation.

```json
{
    "type": "ready",
    "session_id": "sess_abc123",
    "container_id": "d3f9a2b1c4e5",
    "gpu_active": false
}
```

#### PTY Output (Binary)

Raw bytes from container PTY. Sent as binary WebSocket frames.

#### Kill Frame (Text)

Sent when a circuit breaker triggers.

```json
{
    "type": "kill",
    "reason": "cpu-sustained",
    "session_id": "sess_abc123"
}
```

#### Exit Frame (Text)

Sent before closing the connection.

```json
{
    "type": "exit",
    "code": 0,
    "session_id": "sess_abc123"
}
```

#### Error Frame (Text)

Sent when an error occurs.

```json
{
    "type": "error",
    "error": "failed to start container: image not found"
}
```

## Kill Reasons

| Reason | Description |
|--------|-------------|
| cpu-sustained | CPU usage exceeded 90% for 60 seconds |
| ram-pressure | RAM usage exceeded container limit |
| pid-flood | Process count exceeded 512 |
| disk-storm | Disk write rate exceeded 500MB |
| network-scan | Suspicious network scanning detected |
| admin-kill | Manually terminated by machine owner |
| ttl-expired | Session time limit reached |

## WebSocket Close Codes

| Code | Meaning |
|------|---------|
| 1000 | Normal closure (command completed) |
| 1001 | Going away (container error) |
| 1008 | Policy violation (auth failed) |
| 1011 | Internal error |

Close reason format: `exit:<code>` (e.g., `exit:0`, `exit:137`)

## Example Session

```
CLIENT                                              SERVER
  |                                                    |
  |-- WebSocket CONNECT /ws/session ------------------>|
  |                                                    |
  |<----------------- WebSocket ACCEPT ----------------|
  |                                                    |
  |--- Text: {"type":"hello",...} ------------------->|
  |                                                    |
  |<-- Text: {"type":"ready","session_id":"..."} ------|
  |                                                    |
  |--- Binary: "ls\n" -------------------------------->|
  |                                                    |
  |<-- Binary: "file1\nfile2\nfile3\n" ---------------|
  |                                                    |
  |--- Binary: "exit\n" ------------------------------>|
  |                                                    |
  |<-- Text: {"type":"exit","code":0} -----------------|
  |                                                    |
  |<---------------- WebSocket CLOSE exit:0 -----------|
  |                                                    |
```

## Security Considerations

1. **Token in Header**: Auth token is sent in the WebSocket handshake header, not in the handshake frame, to avoid server logging.

2. **HMAC Validation**: Server validates tokens using HMAC-SHA256 signature with timestamp to prevent replay attacks.

3. **TLS Required**: All connections must use WSS (WebSocket Secure) in production.

4. **Session Isolation**: Each session runs in an isolated Docker container with resource limits.

5. **Circuit Breakers**: Automatic session termination on resource abuse.
