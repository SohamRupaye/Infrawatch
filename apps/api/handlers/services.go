package handlers

import (
	"net/http"
	"sort"
	"sync"

	"github.com/SohamRupaye/infrawatch/apps/api/store"
	engineconfig "github.com/SohamRupaye/infrawatch/apps/engine/config"
	"github.com/gin-gonic/gin"
)

// fallbackRegistry is a pkg-level in-memory store for active fallback URLs.
// The healing package's fallback.go manages this in the engine process;
// the API maintains its own view populated from the state_change stream.
var (
	fallbackMu      sync.RWMutex
	activeFallbacks = make(map[string]string)
)

// SetFallback records an active fallback URL.
func SetFallback(service, url string) {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()
	activeFallbacks[service] = url
}

// ClearFallback removes the fallback entry for a service.
func ClearFallback(service string) {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()
	delete(activeFallbacks, service)
}

// AllFallbacks returns a copy of all active fallbacks.
func AllFallbacks() map[string]string {
	fallbackMu.RLock()
	defer fallbackMu.RUnlock()
	out := make(map[string]string, len(activeFallbacks))
	for k, v := range activeFallbacks {
		out[k] = v
	}
	return out
}

// ServiceHandler handles service health and state endpoints.
type ServiceHandler struct{ deps Deps }

func NewServiceHandler(deps Deps) *ServiceHandler { return &ServiceHandler{deps} }

// List godoc
// GET /api/v1/services
// Returns all known services with their current health state plus active fallback URLs.
func (h *ServiceHandler) List(c *gin.Context) {
	views := h.deps.Store.AllServices()
	tagFilter := c.Query("tag")
	fallbacks := AllFallbacks()
	tagsByService := map[string][]string{}
	if cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath); err == nil {
		tagsByService = serviceTagsMap(cfg.Services)
	}
	for i := range views {
		if url, ok := fallbacks[views[i].Name]; ok {
			views[i].FallbackURL = url
		}
		views[i].Tags = tagsByService[views[i].Name]
	}

	if tagFilter != "" {
		filtered := make([]store.ServiceView, 0, len(views))
		for _, v := range views {
			if hasTag(v.Tags, tagFilter) {
				filtered = append(filtered, v)
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"services": filtered,
			"total":    len(filtered),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"services": views,
		"total":    len(views),
	})
}

// Get godoc
// GET /api/v1/services/:name
// Returns the full state view for a single service.
func (h *ServiceHandler) Get(c *gin.Context) {
	name := c.Param("name")
	view, ok := h.deps.Store.GetService(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found: " + name})
		return
	}
	fallbackMu.RLock()
	if url, ok := activeFallbacks[name]; ok {
		view.FallbackURL = url
	}
	fallbackMu.RUnlock()
	if cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath); err == nil {
		view.Tags = serviceTagsMap(cfg.Services)[name]
	}
	c.JSON(http.StatusOK, view)
}

// TagSummaryHandler lists all tags and the number of mapped services.
type TagSummaryHandler struct{ deps Deps }

func NewTagSummaryHandler(deps Deps) *TagSummaryHandler { return &TagSummaryHandler{deps: deps} }

// List godoc
// GET /api/v1/tags
func (h *TagSummaryHandler) List(c *gin.Context) {
	cfg, err := loadConfigFile(h.deps.Cfg.ConfigPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load config: " + err.Error()})
		return
	}

	counts := make(map[string]int)
	for _, svc := range cfg.Services {
		for _, tag := range svc.Tags {
			if tag == "" {
				continue
			}
			counts[tag]++
		}
	}

	type tagItem struct {
		Tag      string `json:"tag"`
		Services int    `json:"services"`
	}

	items := make([]tagItem, 0, len(counts))
	for tag, n := range counts {
		items = append(items, tagItem{Tag: tag, Services: n})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Tag < items[j].Tag })

	c.JSON(http.StatusOK, gin.H{"tags": items, "total": len(items)})
}

func serviceTagsMap(services []engineconfig.ServiceConfig) map[string][]string {
	out := make(map[string][]string, len(services))
	for _, svc := range services {
		tags := make([]string, 0, len(svc.Tags))
		tags = append(tags, svc.Tags...)
		out[svc.Name] = tags
	}
	return out
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}
