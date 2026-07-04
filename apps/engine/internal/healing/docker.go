package healing

import (
	"context"
	"fmt"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"go.uber.org/zap"
)

// DockerHealer restarts containers via the Docker Unix socket.
type DockerHealer struct {
	socketPath string
	logger     *zap.Logger
}

// NewDockerHealer creates a DockerHealer.
func NewDockerHealer(socketPath string, logger *zap.Logger) *DockerHealer {
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}
	return &DockerHealer{socketPath: socketPath, logger: logger}
}

// Restart stops and starts the container named in svc.ContainerName.
// It creates a fresh Docker client on each invocation to avoid stale
// connections after a long idle period.
func (d *DockerHealer) Restart(ctx context.Context, svc config.ServiceConfig) HealResult {
	if svc.ContainerName == "" {
		return HealResult{
			Action:  "docker_restart",
			Success: false,
			Error:   fmt.Errorf("docker_restart: container_name is not set for service %s", svc.Name),
		}
	}

	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost("unix://"+d.socketPath),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return HealResult{
			Action:  "docker_restart",
			Success: false,
			Error:   fmt.Errorf("docker_restart: failed to create docker client: %w", err),
		}
	}
	defer cli.Close()

	sugar := d.logger.Sugar()
	sugar.Infow("stopping container", "container", svc.ContainerName)

	stopTimeout := 10 // seconds
	stopOpts := container.StopOptions{Timeout: &stopTimeout}
	if err := cli.ContainerStop(ctx, svc.ContainerName, stopOpts); err != nil {
		// Non-fatal: container may already be stopped (which is fine — we just start it)
		sugar.Warnw("container stop returned error (may already be stopped)",
			"container", svc.ContainerName,
			"error", err,
		)
	}

	sugar.Infow("starting container", "container", svc.ContainerName)
	startOpts := container.StartOptions{}
	if err := cli.ContainerStart(ctx, svc.ContainerName, startOpts); err != nil {
		return HealResult{
			Action:  "docker_restart",
			Success: false,
			Error:   fmt.Errorf("docker_restart: ContainerStart failed: %w", err),
		}
	}

	sugar.Infow("container restarted successfully", "container", svc.ContainerName)
	return HealResult{
		Action:    "docker_restart",
		Success:   true,
		Timestamp: time.Now(),
	}
}

// ContainerLogs streams the last N lines of logs from containerName to a
// channel. The caller owns closing the context to stop streaming.
func ContainerLogs(ctx context.Context, socketPath, containerName string, tail int) (<-chan []byte, error) {
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}

	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost("unix://"+socketPath),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker logs: failed to create client: %w", err)
	}

	tailStr := fmt.Sprintf("%d", tail)
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       tailStr,
		Timestamps: false,
	}

	reader, err := cli.ContainerLogs(ctx, containerName, opts)
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("docker logs: ContainerLogs failed: %w", err)
	}

	ch := make(chan []byte, 256)
	go func() {
		defer cli.Close()
		defer reader.Close()
		defer close(ch)

		buf := make([]byte, 4096)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := reader.Read(buf)
			if n > 0 {
				// Docker multiplexes stdout/stderr with an 8-byte header.
				// Strip the header if present (first byte is stream type, next 3 are padding, 4 are length).
				line := make([]byte, n)
				copy(line, buf[:n])
				if n > 8 {
					line = line[8:]
				}
				select {
				case ch <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	return ch, nil
}
