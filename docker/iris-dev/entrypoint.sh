#!/bin/bash
set -e

# Forge Container Entrypoint
# Starts SSH server and web terminal

SESSION_ID="${SESSION_ID:-unknown}"
TTL_MINUTES="${TTL_MINUTES:-60}"
TTYD_PASSWORD="${TTYD_PASSWORD:-}"

echo "Forge container starting..."
echo "Session: $SESSION_ID"
echo "TTL: $TTL_MINUTES minutes"

# Generate host keys if missing
if [ ! -f /etc/ssh/ssh_host_ed25519_key ]; then
    ssh-keygen -t ed25519 -f /etc/ssh/ssh_host_ed25519_key -N '' -q
fi
if [ ! -f /etc/ssh/ssh_host_rsa_key ]; then
    ssh-keygen -t rsa -b 4096 -f /etc/ssh/ssh_host_rsa_key -N '' -q
fi

# Ensure SSH directory permissions
mkdir -p /run/sshd
chmod 755 /run/sshd

# Start SSH server
echo "Starting SSH server on port 22..."
/usr/sbin/sshd -D &
SSHD_PID=$!

# Wait for SSH to be ready
sleep 1
if ! kill -0 $SSHD_PID 2>/dev/null; then
    echo "ERROR: SSH server failed to start"
    exit 1
fi

# Start ttyd web terminal
TTYD_OPTS="-p 7681 -W"  # -W allows write access
if [ -n "$TTYD_PASSWORD" ]; then
    TTYD_OPTS="$TTYD_OPTS -c forge:$TTYD_PASSWORD"
fi

echo "Starting ttyd web terminal on port 7681..."
ttyd $TTYD_OPTS bash &
TTYD_PID=$!

# Wait for ttyd to be ready
sleep 1
if ! kill -0 $TTYD_PID 2>/dev/null; then
    echo "ERROR: ttyd failed to start"
    kill $SSHD_PID 2>/dev/null || true
    exit 1
fi

echo "Forge container ready!"
echo "SSH: port 22 (user: forge)"
echo "Web Terminal: http://localhost:7681"

# Handle shutdown gracefully
cleanup() {
    echo "Shutting down..."
    kill $SSHD_PID $TTYD_PID 2>/dev/null || true
    exit 0
}
trap cleanup SIGTERM SIGINT

# Keep container running and monitor processes
while kill -0 $SSHD_PID 2>/dev/null && kill -0 $TTYD_PID 2>/dev/null; do
    sleep 5
done

# If we reach here, one of the processes died
echo "ERROR: A critical process died"
exit 1
