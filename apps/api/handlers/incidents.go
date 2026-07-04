package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/api/store"
	enginepkg "github.com/SohamRupaye/infrawatch/apps/engine/pkg"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Incident is a computed incident record.
type Incident struct {
	ID          string          `json:"id"`
	ServiceName string          `json:"service_name"`
	StartedAt   time.Time       `json:"started_at"`
	ResolvedAt  *time.Time      `json:"resolved_at,omitempty"`
	DurationSec float64         `json:"duration_sec,omitempty"`
	Open        bool            `json:"open"`
	Timeline    []IncidentEvent `json:"timeline"`
	Summary     string          `json:"summary"`
	RootState   string          `json:"root_state,omitempty"`
}

// IncidentEvent is one state change during an incident.
type IncidentEvent struct {
	Timestamp time.Time `json:"timestamp"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Reason    string    `json:"reason"`
}

// IncidentHandler builds and serves incident timelines.
// When TimescaleDB is configured it reads from the incidents and state_changes
// tables.  Otherwise it falls back to the original on-read computation from the
// Redis state-change stream.
type IncidentHandler struct{ deps Deps }

func NewIncidentHandler(deps Deps) *IncidentHandler { return &IncidentHandler{deps} }

// List godoc
// GET /api/v1/incidents?service=<name>
func (h *IncidentHandler) List(c *gin.Context) {
	serviceFilter := c.Query("service")
	tagFilter := c.Query("tag")

	var tagsByService map[string][]string
	if tagFilter != "" {
		cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load config: " + err.Error()})
			return
		}
		tagsByService = serviceTagsMap(cfg.Services)
	}

	if tagFilter != "" && serviceFilter != "" {
		if !hasTag(tagsByService[serviceFilter], tagFilter) {
			c.JSON(http.StatusOK, gin.H{"incidents": []Incident{}, "total": 0})
			return
		}
	}

	if h.deps.DB != nil {
		h.listFromDB(c, serviceFilter, tagFilter, tagsByService)
		return
	}
	h.listFromRedis(c, serviceFilter, tagFilter, tagsByService)
}

// Get godoc
// GET /api/v1/incidents/:id
func (h *IncidentHandler) Get(c *gin.Context) {
	id := c.Param("id")

	if h.deps.DB != nil {
		h.getFromDB(c, id)
		return
	}
	h.getFromRedis(c, id)
}

// Export godoc
// GET /api/v1/incidents/:id/export?format=json|markdown
func (h *IncidentHandler) Export(c *gin.Context) {
	id := c.Param("id")
	format := c.DefaultQuery("format", "json")

	var inc *Incident
	var err error

	if h.deps.DB != nil {
		inc, err = h.fetchFromDB(c.Request.Context(), id)
	} else {
		inc, err = h.fetchFromRedis(c.Request.Context(), id)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if inc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "incident not found: " + id})
		return
	}

	if format == "markdown" {
		c.Header("Content-Type", "text/markdown; charset=utf-8")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=incident-%s.md", id))
		c.String(http.StatusOK, renderMarkdown(*inc))
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=incident-%s.json", id))
	c.JSON(http.StatusOK, inc)
}

// ── TimescaleDB path ───────────────────────────────────────────────────────

func (h *IncidentHandler) listFromDB(c *gin.Context, service, tag string, tagsByService map[string][]string) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	dbIncidents, err := h.deps.DB.QueryIncidents(ctx, service)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query incidents: " + err.Error()})
		return
	}

	result := make([]Incident, 0, len(dbIncidents))
	for _, di := range dbIncidents {
		if tag != "" && !hasTag(tagsByService[di.Service], tag) {
			continue
		}
		result = append(result, dbIncidentToAPI(di))
	}

	c.JSON(http.StatusOK, gin.H{"incidents": result, "total": len(result)})
}

func (h *IncidentHandler) getFromDB(c *gin.Context, id string) {
	inc, err := h.fetchFromDB(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if inc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "incident not found: " + id})
		return
	}
	c.JSON(http.StatusOK, inc)
}

func (h *IncidentHandler) fetchFromDB(reqCtx context.Context, id string) (*Incident, error) {
	ctx, cancel := context.WithTimeout(reqCtx, 10*time.Second)
	defer cancel()

	di, err := h.deps.DB.GetIncidentByID(ctx, id)
	if err != nil || di == nil {
		return nil, err
	}
	inc := dbIncidentToAPI(*di)
	return &inc, nil
}

// dbIncidentToAPI converts a store.DBIncident (DB row) to the API Incident type.
func dbIncidentToAPI(di store.DBIncident) Incident {
	inc := Incident{
		ID:          di.ID,
		ServiceName: di.Service,
		StartedAt:   di.StartedAt,
		ResolvedAt:  di.ResolvedAt,
		Open:        di.Open,
		Summary:     di.Summary,
		RootState:   di.RootState,
		Timeline:    make([]IncidentEvent, 0, len(di.Timeline)),
	}

	if di.DurationMs != nil {
		inc.DurationSec = float64(*di.DurationMs) / 1000.0
	}

	for _, sc := range di.Timeline {
		inc.Timeline = append(inc.Timeline, IncidentEvent{
			Timestamp: sc.Time,
			From:      sc.FromState,
			To:        sc.ToState,
			Reason:    sc.Reason,
		})
	}

	// Auto-generate summary if DB didn't populate one
	if inc.Summary == "" {
		inc.Summary = buildSummary(inc)
	}

	return inc
}

// ── Redis stream fallback path ─────────────────────────────────────────────
// Original on-read computation retained for deployments without TimescaleDB.

func (h *IncidentHandler) listFromRedis(c *gin.Context, serviceFilter, tag string, tagsByService map[string][]string) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	msgs, err := h.deps.Bus.Client().XRevRangeN(ctx, enginepkg.StreamStateChange, "+", "-", 2000).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read state changes: " + err.Error()})
		return
	}
	reverseXMessages(msgs)

	incidents := buildIncidents(msgs)
	result := filterIncidents(incidents, serviceFilter)
	if tag != "" {
		filtered := make([]Incident, 0, len(result))
		for _, inc := range result {
			if hasTag(tagsByService[inc.ServiceName], tag) {
				filtered = append(filtered, inc)
			}
		}
		result = filtered
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Open != result[j].Open {
			return result[i].Open
		}
		return result[i].StartedAt.After(result[j].StartedAt)
	})

	c.JSON(http.StatusOK, gin.H{"incidents": result, "total": len(result)})
}

// GroupByTag godoc
// GET /api/v1/incidents/grouped/by-tag
func (h *IncidentHandler) GroupByTag(c *gin.Context) {
	cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load config: " + err.Error()})
		return
	}

	var incidents []Incident
	if h.deps.DB != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		rows, err := h.deps.DB.QueryIncidents(ctx, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query incidents: " + err.Error()})
			return
		}
		incidents = make([]Incident, 0, len(rows))
		for _, row := range rows {
			incidents = append(incidents, dbIncidentToAPI(row))
		}
	} else {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		msgs, err := h.deps.Bus.Client().XRevRangeN(ctx, enginepkg.StreamStateChange, "+", "-", 2000).Result()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read state changes: " + err.Error()})
			return
		}
		reverseXMessages(msgs)
		incidents = buildIncidents(msgs)
	}

	type tagGroup struct {
		Tag   string `json:"tag"`
		Total int    `json:"total"`
		Open  int    `json:"open"`
	}

	byTag := make(map[string]*tagGroup)
	tagsByService := serviceTagsMap(cfg.Services)
	for _, inc := range incidents {
		for _, tag := range tagsByService[inc.ServiceName] {
			g := byTag[tag]
			if g == nil {
				g = &tagGroup{Tag: tag}
				byTag[tag] = g
			}
			g.Total++
			if inc.Open {
				g.Open++
			}
		}
	}

	out := make([]tagGroup, 0, len(byTag))
	for _, g := range byTag {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	c.JSON(http.StatusOK, gin.H{"groups": out, "total": len(out)})
}

func (h *IncidentHandler) getFromRedis(c *gin.Context, id string) {
	inc, err := h.fetchFromRedis(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if inc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "incident not found: " + id})
		return
	}
	c.JSON(http.StatusOK, inc)
}

func (h *IncidentHandler) fetchFromRedis(reqCtx context.Context, id string) (*Incident, error) {
	ctx, cancel := context.WithTimeout(reqCtx, 10*time.Second)
	defer cancel()

	msgs, err := h.deps.Bus.Client().XRevRangeN(ctx, enginepkg.StreamStateChange, "+", "-", 2000).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to read state changes: %w", err)
	}
	reverseXMessages(msgs)

	for _, inc := range buildIncidents(msgs) {
		if inc.ID == id {
			return &inc, nil
		}
	}
	return nil, nil
}

// ── helpers ────────────────────────────────────────────────────────────────

func reverseXMessages(msgs []redis.XMessage) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}

func filterIncidents(all []Incident, service string) []Incident {
	if service == "" {
		return all
	}
	out := make([]Incident, 0)
	for _, inc := range all {
		if inc.ServiceName == service {
			out = append(out, inc)
		}
	}
	return out
}

func buildIncidents(msgs []redis.XMessage) []Incident {
	type openIncident struct {
		inc     Incident
		lastEvt IncidentEvent
	}
	open := make(map[string]*openIncident)
	var closed []Incident

	healthyStates := map[string]bool{"HEALTHY": true, "UNKNOWN": true}

	for _, msg := range msgs {
		payload, ok := msg.Values["payload"].(string)
		if !ok {
			continue
		}
		var evt enginepkg.StateChangeEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}

		ie := IncidentEvent{
			Timestamp: evt.Timestamp,
			From:      evt.PreviousState,
			To:        evt.NewState,
			Reason:    evt.Reason,
		}

		if oi, exists := open[evt.ServiceName]; exists {
			oi.inc.Timeline = append(oi.inc.Timeline, ie)
			oi.lastEvt = ie

			if healthyStates[evt.NewState] {
				resolvedAt := evt.Timestamp
				oi.inc.ResolvedAt = &resolvedAt
				oi.inc.Open = false
				oi.inc.DurationSec = resolvedAt.Sub(oi.inc.StartedAt).Seconds()
				oi.inc.Summary = buildSummary(oi.inc)
				closed = append(closed, oi.inc)
				delete(open, evt.ServiceName)
			}
		} else if !healthyStates[evt.NewState] {
			incID := fmt.Sprintf("%s-%d", evt.ServiceName, evt.Timestamp.UnixMilli())
			open[evt.ServiceName] = &openIncident{
				inc: Incident{
					ID:          incID,
					ServiceName: evt.ServiceName,
					StartedAt:   evt.Timestamp,
					Open:        true,
					Timeline:    []IncidentEvent{ie},
				},
				lastEvt: ie,
			}
		}
	}

	all := make([]Incident, 0, len(closed)+len(open))
	all = append(all, closed...)
	for _, oi := range open {
		oi.inc.Summary = buildSummary(oi.inc)
		all = append(all, oi.inc)
	}
	return all
}

func buildSummary(inc Incident) string {
	ws := worstState(inc.Timeline)
	if inc.Open {
		return fmt.Sprintf("%s has been %s for %.0fs and is still open.",
			inc.ServiceName, ws, time.Since(inc.StartedAt).Seconds())
	}
	return fmt.Sprintf("%s was %s for %.0fs. Recovered at %s.",
		inc.ServiceName, ws, inc.DurationSec, inc.ResolvedAt.Format("15:04:05"))
}

func worstState(evts []IncidentEvent) string {
	order := map[string]int{"DEAD": 4, "UNHEALTHY": 3, "RECOVERING": 2, "DEGRADED": 1}
	worst := "DEGRADED"
	for _, e := range evts {
		if order[e.To] > order[worst] {
			worst = e.To
		}
	}
	return worst
}

func renderMarkdown(inc Incident) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Incident Report: %s\n\n", inc.ID))
	sb.WriteString(fmt.Sprintf("**Service:** %s  \n", inc.ServiceName))
	sb.WriteString(fmt.Sprintf("**Started:** %s  \n", inc.StartedAt.Format(time.RFC3339)))
	if inc.ResolvedAt != nil {
		sb.WriteString(fmt.Sprintf("**Resolved:** %s  \n", inc.ResolvedAt.Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("**Duration:** %.0fs  \n", inc.DurationSec))
	} else {
		sb.WriteString("**Status:** OPEN  \n")
	}
	if inc.RootState != "" {
		sb.WriteString(fmt.Sprintf("**Root State:** %s  \n", inc.RootState))
	}
	sb.WriteString("\n## Summary\n\n")
	sb.WriteString(inc.Summary)
	sb.WriteString("\n\n## Timeline\n\n| Time | From | To | Reason |\n|------|------|----|--------|\n")
	for _, e := range inc.Timeline {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			e.Timestamp.Format("15:04:05"), e.From, e.To, e.Reason))
	}
	sb.WriteString("\n---\n*Generated by Infrawatch*\n")
	return sb.String()
}
