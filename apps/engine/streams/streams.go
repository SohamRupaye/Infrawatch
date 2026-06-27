// Package streams defines the canonical Redis Stream names used throughout
// Infrawatch.  Both the engine's internal event bus and the public enginepkg
// surface import from here, making this the single source of truth and
// preventing silent constant drift between the two.
package streams

const (
	Metrics        = "infrawatch:metrics"
	StateChange    = "infrawatch:state_changes"
	Healing        = "infrawatch:healing"
	Anomalies      = "infrawatch:anomalies"
	Config         = "infrawatch:service_config_commands"
	CircuitState   = "infrawatch:circuit_state"
	CircuitResets  = "infrawatch:circuit_resets"
	HealCommands   = "infrawatch:heal_commands"
	ExternalAlerts = "infrawatch:external_alert_signals"

	// MaxLen is the approximate Redis MAXLEN applied when publishing to any
	// event stream.  APPROX trimming keeps this O(1) without exact compaction.
	MaxLen = 10000
)
