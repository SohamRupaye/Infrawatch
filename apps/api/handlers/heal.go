package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	enginepkg "github.com/SohamRupaye/infrawatch/apps/engine/pkg"
	"github.com/gin-gonic/gin"
)

// HealHandler exposes manual healing trigger and healing event history.
type HealHandler struct{ deps Deps }

func NewHealHandler(deps Deps) *HealHandler { return &HealHandler{deps} }

// History godoc
// GET /api/v1/healing?limit=50
// Returns the last N healing events from the Redis healing stream.
func (h *HealHandler) History(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	msgs, err := h.deps.Bus.Client().XRevRangeN(ctx, enginepkg.StreamHealing, "+", "-", int64(limit)).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read healing events: " + err.Error()})
		return
	}

	out := make([]enginepkg.HealingEvent, 0, len(msgs))
	for _, msg := range msgs {
		payload, ok := msg.Values["payload"].(string)
		if !ok {
			continue
		}
		var evt enginepkg.HealingEvent
		if err := json.Unmarshal([]byte(payload), &evt); err == nil {
			out = append(out, evt)
		}
	}

	c.JSON(http.StatusOK, gin.H{"events": out, "total": len(out)})
}

// Trigger godoc
// POST /api/v1/services/:name/heal
// Queues a manual heal command for the engine.
func (h *HealHandler) Trigger(c *gin.Context) {
	name := c.Param("name")

	if _, ok := h.deps.Store.GetService(name); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found: " + name})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := h.deps.Bus.PublishHealCommand(ctx, name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to queue heal command: " + err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"service": name,
		"message": "heal command queued — engine will act on it at the next decision cycle",
	})
}
