package websocket

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

// Client represents a single connected WebSocket browser client.
// The send channel is buffered; if it fills, the client is dropped
// to avoid head-of-line blocking for other clients.
type Client struct {
	hub  *Hub
	Send chan []byte
	done chan struct{}
}

// Close signals the client's writer goroutine to stop.
func (c *Client) Close() {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
}

// Hub maintains the set of active WebSocket clients and broadcasts messages.
type Hub struct {
	mu         sync.RWMutex
	clients    map[*Client]struct{}
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	logger     *zap.Logger
}

// NewHub creates a Hub.
func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client, 32),
		unregister: make(chan *Client, 32),
		broadcast:  make(chan []byte, 512),
		logger:     logger,
	}
}

// NewClient creates a Client registered to this hub.
func (h *Hub) NewClient() *Client {
	c := &Client{
		hub:  h,
		Send: make(chan []byte, 256),
		done: make(chan struct{}),
	}
	h.register <- c
	return c
}

// Unregister queues a client for removal.
func (h *Hub) Unregister(c *Client) {
	h.unregister <- c
}

// Broadcast enqueues a message for all connected clients.
func (h *Hub) Broadcast(msg []byte) {
	select {
	case h.broadcast <- msg:
	default:
		// Drop if broadcast channel is full to avoid blocking the broadcaster
		h.logger.Sugar().Warn("hub: broadcast channel full, dropping message")
	}
}

// ClientCount returns the number of currently connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Run is the hub's event loop. Must be called in its own goroutine.
func (h *Hub) Run(ctx context.Context) {
	sugar := h.logger.Sugar()
	for {
		select {
		case <-ctx.Done():
			// Drain and close all clients
			h.mu.Lock()
			for c := range h.clients {
				c.Close()
				close(c.Send)
			}
			h.clients = make(map[*Client]struct{})
			h.mu.Unlock()
			return

		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			h.mu.Unlock()
			sugar.Debugw("ws client registered", "total", len(h.clients))

		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				c.Close()
				close(c.Send)
			}
			h.mu.Unlock()
			sugar.Debugw("ws client unregistered", "total", len(h.clients))

		case msg := <-h.broadcast:
			h.mu.RLock()
			for c := range h.clients {
				select {
				case c.Send <- msg:
				default:
					// Client send buffer full — drop it and schedule removal
					go func(cl *Client) { h.unregister <- cl }(c)
				}
			}
			h.mu.RUnlock()
		}
	}
}
