# Forge Security Model

## Core Principle

> **The host machine is sacred.** Every architectural decision is evaluated by one question: "If this goes wrong, does the host survive?" The answer must always be **yes**.

## Threat Model

### Who is the attacker?

The "attacker" in Forge's threat model is:
1. **Compromised AI agent** — An agent that goes rogue or is manipulated
2. **Malicious session request** — Someone who obtained your session token
3. **Runaway build process** — Accidental infinite loops, memory leaks, fork bombs

### What are we protecting?

1. **Host filesystem** — No writes outside designated directories
2. **Host processes** — No interference with host applications
3. **Host network** — No unauthorized outbound connections
4. **Host resources** — CPU, RAM, and disk must remain available

## Isolation Layers

### Layer 1: Docker Container

```
┌─────────────────────────────────────────────┐
│ Container (iris-dev)                         │
│  - User: forge (non-root)                    │
│  - No sudo, no su                            │
│  - Limited PIDs (512)                        │
│  - Limited RAM (4GB)                         │
│  - Limited CPU (2 cores)                     │
│  - No network by default                     │
│  - Read-only mounts except /workspace        │
└─────────────────────────────────────────────┘
                    │
         Docker Security Options
         --security-opt no-new-privileges
         --cap-drop ALL
```

### Layer 2: Resource Limits

| Resource | Limit | Enforced By |
|----------|-------|-------------|
| RAM | 4GB | Docker cgroup |
| CPU | 2 cores | Docker cgroup |
| PIDs | 512 | Docker cgroup |
| Disk | 500MB | Forge watchdog |
| Time | 120 min max | Forge TTL timer |

### Layer 3: Circuit Breakers

Automatic kill triggers:
- **CPU sustained**: >90% for 60 seconds
- **RAM pressure**: >95% of container limit
- **PID flood**: >512 processes
- **Disk storm**: >500MB writes in session
- **Network scan**: >50 unique IPs in 10 seconds

### Layer 4: Network Isolation

```
Container Network Modes:
  1. "none" — No network access (default for builds)
  2. Limited — Only specific allowlist:
     - crates.io
     - github.com
     - static.rust-lang.org
```

### Layer 5: Ephemeral Credentials

- SSH keypair generated per session
- Private key delivered via TLS (Cloudflare Tunnel)
- Private key deleted from host after injection
- Container destroyed on session end
- No persistent access possible

## What Docker Does NOT Isolate

⚠️ **Important limitations:**

1. **Kernel namespace** — Container shares host kernel
2. **Kernel exploits** — Container can escape via kernel bugs
3. **Device access** — Passed-through devices can be used maliciously
4. **Docker group** — Membership equals root access

## Security Assumptions

### We Assume:
- ✅ You trust yourself (the host user)
- ✅ Your machine is not shared with untrusted users
- ✅ Docker group membership is acceptable
- ✅ Your kernel is reasonably up-to-date

### We Do NOT Assume:
- ❌ The AI agent is benign
- ❌ Network requests are safe
- ❌ Build processes are well-behaved
- ❌ GPU/Camera devices won't be misused

## Best Practices

### DO:
- ✅ Keep Forge updated
- ✅ Use strong random tokens (32+ hex chars)
- ✅ Review alarms.log regularly
- ✅ Set appropriate resource limits
- ✅ Use Cloudflare Tunnel (HTTPS)
- ✅ Rotate tokens periodically

### DO NOT:
- ❌ Run Forge as root
- ❌ Share session tokens
- ❌ Leave sessions running unattended
- ❌ Enable GPU/Camera when not needed
- ❌ Increase TTL limits unnecessarily
- ❌ Run on shared/public servers

## Incident Response

### If a circuit breaker triggers:
1. Container is killed automatically
2. Alarm is logged and sent to webhook
3. Session is invalidated
4. No host action required

### If you suspect compromise:
1. `forge kill --all` — Kill all sessions
2. `systemctl stop forge` — Stop the server
3. Rotate tokens in config
4. Restart with `systemctl start forge`

## Reporting Security Issues

Please report vulnerabilities privately to the project maintainer.
