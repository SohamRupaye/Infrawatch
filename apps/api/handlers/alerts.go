package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// AlertHandler serves alert history and acknowledgement APIs.
type AlertHandler struct{ deps Deps }

func NewAlertHandler(deps Deps) *AlertHandler { return &AlertHandler{deps: deps} }

// History godoc
// GET /api/v1/alerts/history?service=&channel=&unacked=true&limit=100
func (h *AlertHandler) History(c *gin.Context) {
	if h.deps.DB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "alert history requires TimescaleDB",
		})
		return
	}

	limit := 100
	if q := c.Query("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be a positive integer"})
			return
		}
		limit = n
	}

	rows, err := h.deps.DB.QueryAlertHistory(
		c.Request.Context(),
		c.Query("service"),
		c.Query("channel"),
		c.Query("unacked") == "true",
		limit,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query alert history: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"alerts": rows, "total": len(rows)})
}

type ackAlertRequest struct {
	AcknowledgedBy string `json:"acknowledged_by"`
}

// Acknowledge godoc
// POST /api/v1/alerts/:id/ack
func (h *AlertHandler) Acknowledge(c *gin.Context) {
	if h.deps.DB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "alert history requires TimescaleDB",
		})
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid alert id"})
		return
	}

	req := ackAlertRequest{AcknowledgedBy: "manual"}
	_ = c.ShouldBindJSON(&req)
	if req.AcknowledgedBy == "" {
		req.AcknowledgedBy = "manual"
	}

	ok, err := h.deps.DB.AcknowledgeAlert(c.Request.Context(), id, req.AcknowledgedBy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to acknowledge alert: " + err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "alert not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":              true,
		"alert_id":        id,
		"acknowledged_at": time.Now().UTC(),
		"acknowledged_by": req.AcknowledgedBy,
	})
}
