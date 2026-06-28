package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// CircuitHandler exposes circuit breaker state and the manual reset action.
type CircuitHandler struct{ deps Deps }

func NewCircuitHandler(deps Deps) *CircuitHandler { return &CircuitHandler{deps} }

// List godoc
// GET /api/v1/circuit
func (h *CircuitHandler) List(c *gin.Context) {
	snaps := h.deps.Store.AllCircuits()
	c.JSON(http.StatusOK, gin.H{"circuits": snaps, "total": len(snaps)})
}

// Get godoc
// GET /api/v1/circuit/:service
func (h *CircuitHandler) Get(c *gin.Context) {
	name := c.Param("service")
	snaps := h.deps.Store.AllCircuits()
	snap, ok := snaps[name]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "no circuit found for service: " + name})
		return
	}
	c.JSON(http.StatusOK, snap)
}

// Reset godoc
// POST /api/v1/circuit/:service/reset
// Publishes a reset command to the engine via Redis. The engine performs the
// actual reset and publishes the real resulting breaker snapshot back over
// Redis — this handler does not assert success itself.
func (h *CircuitHandler) Reset(c *gin.Context) {
	name := c.Param("service")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := h.deps.Bus.PublishCircuitReset(ctx, name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to publish reset command: " + err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"service": name,
		"message": "circuit reset command sent — engine will act on it at the next decision cycle",
	})
}
