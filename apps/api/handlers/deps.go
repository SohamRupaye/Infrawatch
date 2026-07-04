package handlers

import (
	"github.com/SohamRupaye/infrawatch/apps/api/store"
	apiwsocket "github.com/SohamRupaye/infrawatch/apps/api/websocket"
	enginepkg "github.com/SohamRupaye/infrawatch/apps/engine/pkg"
	"go.uber.org/zap"
)

// Deps carries the shared dependencies injected into every handler.
type Deps struct {
	Bus    *enginepkg.Bus
	Cfg    *APIConfig
	Hub    *apiwsocket.Hub
	Store  *store.StateStore
	DB     *store.DBReader // nil when TimescaleDB is not configured; handlers degrade gracefully
	Logger *zap.Logger
}

// APIConfig holds the subset of config the API handlers need.
// The full engine config is loaded by main.go; only these fields
// are passed through Deps to avoid importing the engine config package.
type APIConfig struct {
	DockerSocketPath string
	JWTSecret        string
	ConfigPath       string
}
