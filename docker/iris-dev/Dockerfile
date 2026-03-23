FROM ubuntu:24.04

LABEL maintainer="Forge Project"
LABEL description="Iris development environment with full GTK4/Rust/FFmpeg stack"

ENV DEBIAN_FRONTEND=noninteractive
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    curl \
    git \
    pkg-config \
    ca-certificates \
    # Build tools
    meson \
    ninja-build \
    blueprint-compiler \
    # GTK4 and libadwaita
    libgtk-4-dev \
    libadwaita-1-dev \
    # V4L2 camera support
    libv4l-dev \
    v4l-utils \
    # FFmpeg for video encoding
    libavcodec-dev \
    libavformat-dev \
    libavutil-dev \
    libswscale-dev \
    ffmpeg \
    # TurboJPEG for MJPEG decoding
    libturbojpeg-dev \
    # Vulkan for GPU compute
    libvulkan-dev \
    mesa-vulkan-drivers \
    vulkan-tools \
    # SSH server
    openssh-server \
    # Web terminal
    ttyd \
    # Utilities
    sudo \
    less \
    vim \
    htop \
    strace \
    && rm -rf /var/lib/apt/lists/*

# Install Rust (stable toolchain)
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
ENV PATH="/root/.cargo/bin:${PATH}"

# Pre-warm Cargo cache with common dependencies
RUN cargo install cargo-watch cargo-nextest 2>/dev/null || true

# Create and configure forge user (non-root, no sudo)
RUN useradd -m -s /bin/bash forge && \
    mkdir -p /home/forge/.ssh && \
    chmod 700 /home/forge/.ssh && \
    chown -R forge:forge /home/forge

# Configure SSH
RUN mkdir -p /run/sshd && \
    sed -i 's/#PasswordAuthentication yes/PasswordAuthentication no/' /etc/ssh/sshd_config && \
    sed -i 's/#PubkeyAuthentication yes/PubkeyAuthentication yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config && \
    sed -i 's/#MaxStartups.*/MaxStartups 3/' /etc/ssh/sshd_config && \
    sed -i 's/#MaxSessions.*/MaxSessions 1/' /etc/ssh/sshd_config && \
    sed -i 's/#ClientAliveInterval.*/ClientAliveInterval 300/' /etc/ssh/sshd_config && \
    sed -i 's/#ClientAliveCountMax.*/ClientAliveCountMax 2/' /etc/ssh/sshd_config

# Create workspace
RUN mkdir -p /workspace && chown forge:forge /workspace
WORKDIR /workspace

# Copy entrypoint
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Expose ports (SSH and web terminal)
EXPOSE 22 7681

# Health check
HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
    CMD pgrep sshd && pgrep ttyd || exit 1

ENTRYPOINT ["/entrypoint.sh"]
