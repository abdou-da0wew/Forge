# Circuit Breakers

Forge implements 5 automatic circuit breakers that kill sessions when resource abuse is detected.

## Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                    Forge Watchdog Loop                          │
│                                                                  │
│   Every 5 seconds:                                               │
│   1. Poll Docker stats API                                       │
│   2. Record stats to history                                     │
│   3. Check each breaker condition                                │
│   4. If triggered → kill container → dispatch alarm              │
└─────────────────────────────────────────────────────────────────┘
```

## Breaker 1: CPU Sustained

**Trigger**: CPU usage > 90% for 60 consecutive seconds

**Purpose**: Stop runaway compilations, infinite loops, crypto mining

**Configuration**:
```toml
[watchdog]
cpu_sustained_threshold_pct = 90
cpu_sustained_duration_seconds = 60
```

**How it works**:
- Watchdog keeps rolling 12-sample history (12 × 5s = 60s)
- All 12 samples must exceed threshold
- Single spike doesn't trigger — must be sustained

**Example scenario**:
```rust
// This triggers breaker after ~60 seconds
loop {
    let _ = (0..1_000_000).map(|x| x * x).collect::<Vec<_>>();
}
```

## Breaker 2: RAM Pressure

**Trigger**: RAM usage > 95% of container limit

**Purpose**: Stop memory leaks, prevent OOM on host

**Configuration**:
```toml
[watchdog]
ram_threshold_pct = 95
```

**How it works**:
- Checks `memory_stats.usage` from Docker API
- Compares against `memory_stats.limit` (container cgroup limit)
- Percentage-based: works regardless of actual limit

**Example scenario**:
```rust
// This triggers breaker when approaching 4GB
let mut data = Vec::new();
loop {
    data.extend_from_slice(&[0u8; 1_000_000]); // 1MB chunks
}
```

## Breaker 3: PID Flood

**Trigger**: Process count > 512

**Purpose**: Stop fork bombs, process spawning attacks

**Configuration**:
```toml
[watchdog]
pid_limit = 512
[session]
pid_limit = 512  # Docker cgroup limit (hard limit)
```

**How it works**:
- Watchdog checks `pids_stats.current`
- Docker cgroup also enforces hard limit
- Watchdog provides early warning before cgroup kill

**Example scenario**:
```bash
# Fork bomb - stopped at 512 processes
:(){ :|:& };:
```

## Breaker 4: Disk Storm

**Trigger**: Total disk writes > 500MB in session

**Purpose**: Stop disk filling attacks, excessive I/O

**Configuration**:
```toml
[watchdog]
disk_write_limit_mb = 500
```

**How it works**:
- Sums `blkio_stats.io_service_bytes_recursive` for "write" operations
- Accumulates across entire session
- Cannot be reset by clearing files

**Example scenario**:
```bash
# Writing to /workspace fills disk quota
dd if=/dev/zero of=/workspace/bigfile bs=1M count=1000
```

## Breaker 5: Network Scan

**Trigger**: >50 unique destination IPs in 10 seconds

**Purpose**: Detect port scanning, C2 communication attempts

**Configuration**:
```toml
[watchdog]
network_scan_ip_count = 50
network_scan_window_seconds = 10
```

**How it works**:
- Parses `/proc/net/tcp` inside container
- Tracks unique remote IP addresses
- Rolling 10-second window
- **Note**: Only works when container has network access

**Example scenario**:
```bash
# Port scanning triggers breaker
for i in $(seq 1 100); do
    nc -z 192.168.1.$i 80 &
done
```

## Trigger Actions

When any breaker fires:

1. **Log**: JSON event to `~/.forge/alarms.log`
2. **Kill**: `docker kill --signal SIGKILL <container>`
3. **Remove**: `docker rm -f <container>`
4. **Notify**: `notify-send` to desktop (if DISPLAY set)
5. **Webhook**: POST to configured URL
6. **Cleanup**: Delete SSH private key from host
7. **Invalidate**: Remove session from store

## Tuning Guidelines

### For Development Workstations (8-16GB RAM):
```toml
[watchdog]
cpu_sustained_threshold_pct = 90    # Default
cpu_sustained_duration_seconds = 60 # Default
ram_threshold_pct = 95              # Default
pid_limit = 512                     # Default
disk_write_limit_mb = 500          # Default
```

### For Lower-Spec Machines (4-8GB RAM):
```toml
[session]
memory_limit_gb = 2
cpu_limit = 1

[watchdog]
ram_threshold_pct = 90  # More aggressive
```

### For High-Performance Builds (32GB+ RAM):
```toml
[session]
memory_limit_gb = 8
cpu_limit = 4
pid_limit = 1024

[watchdog]
disk_write_limit_mb = 2000  # Allow more
```

## Testing Breakers

```bash
# Test CPU breaker
docker exec <container> stress-ng --cpu 4 --timeout 90

# Test RAM breaker
docker exec <container> python3 -c "x='a'*int(5e9)"

# Test PID breaker
docker exec <container> bash -c ':(){ :|:& };:'

# Test disk breaker
docker exec <container> dd if=/dev/zero of=/workspace/test bs=1M count=600

# All should trigger within expected timeframe
# Check: tail ~/.forge/alarms.log
```

## Monitoring

Watch watchdog logs in real-time:
```bash
forge logs <session-id>
# or
tail -f ~/.forge/sessions/<id>/watchdog.log
```
