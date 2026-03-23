package container

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net"
        "os"
        "os/exec"
        "path/filepath"
        "time"

        "github.com/abdou/forge/internal/config"
        "github.com/docker/docker/api/types"
        "github.com/docker/docker/api/types/container"
        "github.com/docker/docker/api/types/mount"
        "github.com/docker/docker/client"
        "github.com/docker/go-connections/nat"
        "github.com/rs/zerolog/log"
)

// Manager handles Docker container lifecycle
type Manager struct {
        cfg    *config.Config
        client *client.Client
}

// NewManager creates a new container manager
func NewManager(cfg *config.Config) (*Manager, error) {
        // Create Docker client
        cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
        if err != nil {
                return nil, fmt.Errorf("failed to create Docker client: %w", err)
        }

        // Test connection
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()

        _, err = cli.Ping(ctx)
        if err != nil {
                return nil, fmt.Errorf("failed to connect to Docker daemon: %w (is docker running?)", err)
        }

        return &Manager{
                cfg:    cfg,
                client: cli,
        }, nil
}

// StartRequest contains parameters for starting a container
type StartRequest struct {
        SessionID    string
        SSHPort      int
        TTYDPort     int
        PublicKey    string
        GPU          bool
        Camera       bool
        TTLMinutes   int
        WorkspaceDir string
}

// RunningContainer contains info about a started container
type RunningContainer struct {
        ContainerID string
        SSHPort     int
        TTYDPort    int
        Workspace   string
}

// Start creates and starts a new container
func (m *Manager) Start(ctx context.Context, req StartRequest) (*RunningContainer, error) {
        // Create workspace directory
        workspaceDir := filepath.Join(req.WorkspaceDir, req.SessionID, "workspace")
        if err := os.MkdirAll(workspaceDir, 0755); err != nil {
                return nil, fmt.Errorf("failed to create workspace: %w", err)
        }

        // Build container config
        containerConfig := &container.Config{
                Image: m.cfg.Docker.Image,
                ExposedPorts: nat.PortSet{
                        "22/tcp":  struct{}{},
                        "7681/tcp": struct{}{},
                },
                Env: []string{
                        fmt.Sprintf("SESSION_ID=%s", req.SessionID),
                        fmt.Sprintf("TTL_MINUTES=%d", req.TTLMinutes),
                },
                User: "forge",
        }

        // Build host config
        hostConfig := &container.HostConfig{
                PortBindings: nat.PortMap{
                        "22/tcp": []nat.PortBinding{
                                {HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", req.SSHPort)},
                        },
                        "7681/tcp": []nat.PortBinding{
                                {HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", req.TTYDPort)},
                        },
                },
                Resources: container.Resources{
                        Memory:     int64(m.cfg.Session.MemoryLimitGB) * 1024 * 1024 * 1024,
                        NanoCPUs:   int64(m.cfg.Session.CPULimit) * 1e9,
                        PidsLimit:  int64(m.cfg.Session.PIDLimit),
                },
                Mounts: []mount.Mount{
                        {
                                Type:   mount.TypeBind,
                                Source: workspaceDir,
                                Target: "/workspace",
                        },
                },
                SecurityOpt: []string{"no-new-privileges"},
        }

        // Add GPU devices if requested
        if req.GPU && m.cfg.Passthrough.AllowGPU {
                devices := m.detectGPU()
                hostConfig.Devices = append(hostConfig.Devices, devices...)
                log.Info().Strs("devices", m.cfg.Passthrough.GPUDevicePaths).Msg("GPU passthrough enabled")
        }

        // Add camera devices if requested
        if req.Camera && m.cfg.Passthrough.AllowCamera {
                devices := m.detectCamera()
                hostConfig.Devices = append(hostConfig.Devices, devices...)
                log.Info().Strs("devices", m.cfg.Passthrough.CameraDevicePaths).Msg("Camera passthrough enabled")
        }

        // Create container
        resp, err := m.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, req.SessionID)
        if err != nil {
                return nil, fmt.Errorf("failed to create container: %w", err)
        }

        // Start container
        if err := m.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
                m.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
                return nil, fmt.Errorf("failed to start container: %w", err)
        }

        log.Info().
                Str("container_id", resp.ID[:12]).
                Str("session_id", req.SessionID).
                Int("ssh_port", req.SSHPort).
                Msg("Container started")

        // Wait for SSH to be ready
        if err := m.waitForSSH(req.SSHPort, 30*time.Second); err != nil {
                m.Kill(resp.ID)
                return nil, fmt.Errorf("SSH not ready: %w", err)
        }

        // Inject public key
        if err := m.injectPublicKey(ctx, resp.ID, req.PublicKey); err != nil {
                m.Kill(resp.ID)
                return nil, fmt.Errorf("failed to inject public key: %w", err)
        }

        return &RunningContainer{
                ContainerID: resp.ID,
                SSHPort:     req.SSHPort,
                TTYDPort:    req.TTYDPort,
                Workspace:   workspaceDir,
        }, nil
}

// Kill forcefully stops and removes a container
func (m *Manager) Kill(containerID string) error {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()

        log.Info().Str("container_id", containerID[:12]).Msg("Killing container")

        // Kill container
        if err := m.client.ContainerKill(ctx, containerID, "SIGKILL"); err != nil {
                if !isErrNotFound(err) {
                        log.Warn().Err(err).Msg("Failed to kill container")
                }
        }

        // Remove container
        if err := m.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
                Force:         true,
                RemoveVolumes: true,
        }); err != nil {
                if !isErrNotFound(err) {
                        return fmt.Errorf("failed to remove container: %w", err)
                }
        }

        log.Info().Str("container_id", containerID[:12]).Msg("Container removed")
        return nil
}

// Stats returns a channel of container stats
func (m *Manager) Stats(ctx context.Context, containerID string) (<-chan *ContainerStats, error) {
        statsChan, err := m.client.ContainerStats(ctx, containerID, true)
        if err != nil {
                return nil, err
        }

        result := make(chan *ContainerStats, 10)

        go func() {
                defer close(result)
                defer statsChan.Body.Close()

                decoder := json.NewDecoder(statsChan.Body)
                for {
                        var stats types.Stats
                        if err := decoder.Decode(&stats); err != nil {
                                if err != io.EOF {
                                        log.Warn().Err(err).Msg("Error decoding stats")
                                }
                                return
                        }

                        result <- parseStats(&stats)
                }
        }()

        return result, nil
}

// ContainerStats holds parsed stats
type ContainerStats struct {
        CPUPercent  float64
        RAMBytes    uint64
        RAMPercent  float64
        PIDs        uint64
        DiskRead    uint64
        DiskWrite   uint64
        NetworkRx   uint64
        NetworkTx   uint64
        Timestamp   time.Time
}

// waitForSSH waits for SSH port to become available
func (m *Manager) waitForSSH(port int, timeout time.Duration) error {
        deadline := time.Now().Add(timeout)

        for time.Now().Before(deadline) {
                conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 1*time.Second)
                if err == nil {
                        conn.Close()
                        return nil
                }
                time.Sleep(500 * time.Millisecond)
        }

        return fmt.Errorf("timeout waiting for SSH on port %d", port)
}

// injectPublicKey adds the public key to the container's authorized_keys
func (m *Manager) injectPublicKey(ctx context.Context, containerID, publicKey string) error {
        // Create exec instance
        execConfig := types.ExecConfig{
                Cmd:          []string{"sh", "-c", fmt.Sprintf("mkdir -p ~/.ssh && echo '%s' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys", publicKey)},
                AttachStdout: true,
                AttachStderr: true,
                User:         "forge",
        }

        execResp, err := m.client.ContainerExecCreate(ctx, containerID, execConfig)
        if err != nil {
                return err
        }

        // Attach to exec
        attachResp, err := m.client.ContainerExecAttach(ctx, execResp.ID, types.ExecStartCheck{})
        if err != nil {
                return err
        }
        defer attachResp.Close()

        // Start exec
        if err := m.client.ContainerExecStart(ctx, execResp.ID, types.ExecStartCheck{}); err != nil {
                return err
        }

        // Wait for completion
        for {
                inspect, err := m.client.ContainerExecInspect(ctx, execResp.ID)
                if err != nil {
                        return err
                }
                if !inspect.Running {
                        if inspect.ExitCode != 0 {
                                return fmt.Errorf("exec failed with exit code %d", inspect.ExitCode)
                        }
                        break
                }
                time.Sleep(100 * time.Millisecond)
        }

        return nil
}

// detectGPU returns device mappings for GPU passthrough
func (m *Manager) detectGPU() []container.DeviceMapping {
        var devices []container.DeviceMapping

        for _, path := range m.cfg.Passthrough.GPUDevicePaths {
                if _, err := os.Stat(path); err == nil {
                        devices = append(devices, container.DeviceMapping{
                                PathOnHost:        path,
                                PathInContainer:   path,
                                CgroupPermissions: "rwm",
                        })
                }
        }

        return devices
}

// detectCamera returns device mappings for camera passthrough
func (m *Manager) detectCamera() []container.DeviceMapping {
        var devices []container.DeviceMapping

        for _, path := range m.cfg.Passthrough.CameraDevicePaths {
                if _, err := os.Stat(path); err == nil {
                        devices = append(devices, container.DeviceMapping{
                                PathOnHost:        path,
                                PathInContainer:   path,
                                CgroupPermissions: "rwm",
                        })
                }
        }

        return devices
}

// parseStats converts Docker stats to our format
func parseStats(stats *types.Stats) *ContainerStats {
        // CPU percentage
        cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
        systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)
        cpuPercent := 0.0
        if systemDelta > 0 && cpuDelta > 0 {
                cpuPercent = (cpuDelta / systemDelta) * float64(len(stats.CPUStats.CPUUsage.PercpuUsage)) * 100.0
        }

        // Memory
        memUsage := stats.MemoryStats.Usage
        memLimit := stats.MemoryStats.Limit
        memPercent := 0.0
        if memLimit > 0 {
                memPercent = float64(memUsage) / float64(memLimit) * 100.0
        }

        // Network
        var netRx, netTx uint64
        for _, v := range stats.Networks {
                netRx += v.RxBytes
                netTx += v.TxBytes
        }

        // Disk I/O
        var diskRead, diskWrite uint64
        for _, v := range stats.BlkioStats.IOServiceBytesRecursive {
                switch v.Op {
                case "read":
                        diskRead += v.Value
                case "write":
                        diskWrite += v.Value
                }
        }

        return &ContainerStats{
                CPUPercent: cpuPercent,
                RAMBytes:   memUsage,
                RAMPercent: memPercent,
                PIDs:       stats.PidsStats.Current,
                DiskRead:   diskRead,
                DiskWrite:  diskWrite,
                NetworkRx:  netRx,
                NetworkTx:  netTx,
                Timestamp:  time.Now(),
        }
}

// isErrNotFound checks if error is "container not found"
func isErrNotFound(err error) bool {
        return err != nil && (client.IsErrNotFound(err) || 
                (err.Error() != "" && (err.Error() == "container not found" || 
                        err.Error() == "No such container")))
}

// EnsureImage pulls the image if not present
func (m *Manager) EnsureImage(ctx context.Context) error {
        // Check if image exists
        _, _, err := m.client.ImageInspectWithRaw(ctx, m.cfg.Docker.Image)
        if err == nil {
                log.Debug().Str("image", m.cfg.Docker.Image).Msg("Image already exists")
                return nil
        }

        log.Info().Str("image", m.cfg.Docker.Image).Msg("Pulling image...")

        reader, err := m.client.ImagePull(ctx, m.cfg.Docker.Image, types.ImagePullOptions{})
        if err != nil {
                return fmt.Errorf("failed to pull image: %w", err)
        }
        defer reader.Close()

        // Drain output
        io.Copy(io.Discard, reader)

        log.Info().Str("image", m.cfg.Docker.Image).Msg("Image pulled successfully")
        return nil
}

// BuildImage builds the iris-dev image from Dockerfile
func (m *Manager) BuildImage(ctx context.Context, dockerfileDir string, output io.Writer) error {
        log.Info().Str("dir", dockerfileDir).Msg("Building iris-dev image")

        // Use exec to run docker build (easier than API)
        cmd := exec.CommandContext(ctx, "docker", "build", "-t", m.cfg.Docker.Image, dockerfileDir)
        cmd.Stdout = output
        cmd.Stderr = output

        if err := cmd.Run(); err != nil {
                return fmt.Errorf("docker build failed: %w", err)
        }

        log.Info().Str("image", m.cfg.Docker.Image).Msg("Image built successfully")
        return nil
}
