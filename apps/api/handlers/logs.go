package handlers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	apiwsocket "github.com/SohamRupaye/infrawatch/apps/api/websocket"
	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var logUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// LogHandler serves Docker container logs over HTTP (snapshot) and WebSocket (live).
type LogHandler struct{ deps Deps }

func NewLogHandler(deps Deps) *LogHandler { return &LogHandler{deps} }

// Tail godoc
// GET /api/v1/logs/:container?tail=500
// Streams the last N log lines to the HTTP response body as plain text.
func (h *LogHandler) Tail(c *gin.Context) {
	containerName := c.Param("container")
	tail, _ := strconv.Atoi(c.DefaultQuery("tail", "500"))
	if tail <= 0 {
		tail = 500
	}

	socketPath := h.deps.Cfg.DockerSocketPath
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	logRC, err := openDockerLogs(ctx, socketPath, containerName, tail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer logRC.Close()

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Transfer-Encoding", "chunked")
	c.Status(http.StatusOK)
	flusher, canFlush := c.Writer.(http.Flusher)

	scanner := bufio.NewScanner(logRC)
	for scanner.Scan() {
		line := scanner.Bytes()
		// Strip 8-byte Docker multiplexing header if present
		if len(line) > 8 {
			line = line[8:]
		}
		c.Writer.Write(append(line, '\n'))
		if canFlush {
			flusher.Flush()
		}
	}
}

// WSLogs upgrades to WebSocket and streams live Docker container logs.
// Route: GET /ws/logs/:container
func (h *LogHandler) WSLogs(_ *apiwsocket.Hub, logger *zap.Logger) gin.HandlerFunc {
	sugar := logger.Sugar()
	return func(c *gin.Context) {
		containerName := c.Param("container")

		conn, err := logUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			sugar.Warnw("ws log upgrade failed", "error", err)
			return
		}
		defer conn.Close()

		socketPath := h.deps.Cfg.DockerSocketPath
		if socketPath == "" {
			socketPath = "/var/run/docker.sock"
		}

		ctx, cancel := context.WithCancel(c.Request.Context())
		defer cancel()

		logRC, err := openDockerLogs(ctx, socketPath, containerName, 500)
		if err != nil {
			conn.WriteMessage(websocket.TextMessage, []byte("ERROR: "+err.Error()))
			return
		}
		defer logRC.Close()

		scanner := bufio.NewScanner(logRC)
		for scanner.Scan() {
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			line := scanner.Bytes()
			if len(line) > 8 {
				line = line[8:]
			}
			if err := conn.WriteMessage(websocket.TextMessage, line); err != nil {
				return
			}
		}
	}
}

func openDockerLogs(ctx context.Context, socketPath, containerName string, tail int) (io.ReadCloser, error) {
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost("unix://"+socketPath),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}

	tailStr := strconv.Itoa(tail)
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       tailStr,
	}

	rc, err := cli.ContainerLogs(ctx, containerName, opts)
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("container logs: %w", err)
	}

	// Wrap so cli.Close() is called when the caller closes the reader
	return &dockerLogReader{ReadCloser: rc, cli: cli}, nil
}

type dockerLogReader struct {
	io.ReadCloser
	cli *dockerclient.Client
}

func (r *dockerLogReader) Close() error {
	err := r.ReadCloser.Close()
	r.cli.Close()
	return err
}
