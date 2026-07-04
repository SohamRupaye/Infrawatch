package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	enginepkg "github.com/SohamRupaye/infrawatch/apps/engine/pkg"
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
// Publishes a reset command to the engine via Redis and locally clears the circuit snapshot.
func (h *CircuitHandler) Reset(c *gin.Context) {
	name := c.Param("service")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := h.deps.Bus.PublishCircuitReset(ctx, name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to publish reset command: " + err.Error()})
		return
	}

	// Optimistic local update
	h.deps.Store.UpdateCircuit(name, enginepkg.BreakerSnapshot{
		State:          enginepkg.BreakerStateClosed,
		LastTransition: time.Now(),
	})

	h.deps.Bus.PublishStateChange(ctx, enginepkg.StateChangeEvent{
		ServiceName:   name,
		PreviousState: "OPEN",
		NewState:      "RECOVERING",
		Reason:        "circuit manually reset via API",
		Timestamp:     time.Now(),
	})

	c.JSON(http.StatusOK, gin.H{"service": name, "message": "circuit reset command sent"})
}
