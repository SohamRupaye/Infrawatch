package health

import (
	"context"

	"go.uber.org/zap"
)

// AlertSignal is an engine-internal representation of an external alert
// notification (e.g. from Alertmanager), decoupled from whatever wire format
// carried it in.
type AlertSignal struct {
	ServiceName string
	Status      string // "firing" or "resolved"
	Reason      string
}

// AlertSignalHandler applies external alert signals to passive-mode
// services: it sets/clears the service's alert floor and triggers an
// immediate re-evaluation so the effect is visible without waiting for the
// next tick.
type AlertSignalHandler struct {
	poller   *Poller
	stateMgr *StateManager
	logger   *zap.Logger
}

// NewAlertSignalHandler creates an AlertSignalHandler.
func NewAlertSignalHandler(poller *Poller, stateMgr *StateManager, logger *zap.Logger) *AlertSignalHandler {
	return &AlertSignalHandler{poller: poller, stateMgr: stateMgr, logger: logger}
}

// Handle applies sig to the named service. Unknown or non-passive services
// are logged and dropped — defense in depth alongside the API's own mapping
// check, since the two run as separate processes.
func (h *AlertSignalHandler) Handle(ctx context.Context, sig AlertSignal) {
	sugar := h.logger.Sugar()

	svc, ok := h.poller.Lookup(sig.ServiceName)
	if !ok {
		sugar.Warnw("alert signal for unknown service, dropping", "service", sig.ServiceName)
		return
	}
	if !svc.IsPassive() {
		sugar.Warnw("alert signal for non-passive service, dropping", "service", sig.ServiceName)
		return
	}

	switch sig.Status {
	case "firing":
		h.stateMgr.SetAlertFloor(sig.ServiceName, sig.Reason)
		sugar.Infow("alert floor set", "service", sig.ServiceName, "reason", sig.Reason)
	case "resolved":
		h.stateMgr.ClearAlertFloor(sig.ServiceName)
		sugar.Infow("alert floor cleared", "service", sig.ServiceName)
	default:
		sugar.Warnw("unknown alert signal status, dropping", "service", sig.ServiceName, "status", sig.Status)
		return
	}

	h.poller.PollNow(ctx, sig.ServiceName)
}
