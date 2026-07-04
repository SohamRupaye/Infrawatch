package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	enginepkg "github.com/SohamRupaye/infrawatch/apps/engine/pkg"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Broadcaster subscribes to all four Redis streams and fans each message
// to all connected WebSocket clients via the Hub.
type Broadcaster struct {
	bus    *enginepkg.Bus
	hub    *Hub
	logger *zap.Logger
}

// NewBroadcaster creates a Broadcaster.
func NewBroadcaster(bus *enginepkg.Bus, hub *Hub, logger *zap.Logger) *Broadcaster {
	return &Broadcaster{bus: bus, hub: hub, logger: logger}
}

// Run starts one subscriber goroutine per stream and blocks until ctx is done.
func (b *Broadcaster) Run(ctx context.Context) {
	streams := []struct {
		stream  string
		evtType string
	}{
		{enginepkg.StreamMetrics, "metric"},
		{enginepkg.StreamStateChange, "state_change"},
		{enginepkg.StreamHealing, "healing"},
		{enginepkg.StreamAnomalies, "anomaly"},
	}

	for _, s := range streams {
		s := s
		go b.bus.Subscribe(ctx, s.stream, "$", func(_ string, payload []byte) {
			env := envelope{Type: s.evtType, Payload: json.RawMessage(payload)}
			data, err := json.Marshal(env)
			if err != nil {
				b.logger.Sugar().Errorw("broadcaster: marshal failed", "type", s.evtType, "error", err)
				return
			}
			b.hub.Broadcast(data)
		})
	}
	<-ctx.Done()
}

// ServeWS upgrades an HTTP connection to WebSocket and pumps hub messages to the browser.
func ServeWS(hub *Hub, logger *zap.Logger) gin.HandlerFunc {
	sugar := logger.Sugar()
	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			sugar.Warnw("ws upgrade failed", "error", err)
			return
		}

		client := hub.NewClient()
		defer hub.Unregister(client)

		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		for {
			select {
			case msg, ok := <-client.Send:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if !ok {
					conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			case <-client.done:
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
		}
	}
}
