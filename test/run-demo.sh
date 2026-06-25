#!/usr/bin/env bash
# =============================================================================
# run-demo.sh — Infrawatch demo orchestrator
#
# Usage:
#   bash test/run-demo.sh start          # bring up the full demo stack
#   bash test/run-demo.sh stop           # tear it down
#   bash test/run-demo.sh status         # print service states
#   bash test/run-demo.sh scenario <name># trigger a named scenario
#   bash test/run-demo.sh tour           # run the auto-tour (all scenarios)
#   bash test/run-demo.sh logs           # tail demo-services logs
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_BASE="${ROOT_DIR}/docker/docker-compose.yml"
COMPOSE_DEMO="${SCRIPT_DIR}/docker-compose.demo.yml"
DEMO_HOST="localhost"
DEMO_PORT=4000
INFRAWATCH_URL="http://localhost:8080"
FRONTEND_URL="http://localhost:3000"

# Colour codes
RED='\033[0;31m'
GRN='\033[0;32m'
YLW='\033[0;33m'
BLU='\033[0;34m'
MAG='\033[0;35m'
CYN='\033[0;36m'
RST='\033[0m'
BOLD='\033[1m'

log()  { echo -e "${BLU}[demo]${RST} $*"; }
ok()   { echo -e "${GRN}  ✓${RST} $*"; }
warn() { echo -e "${YLW}  !${RST} $*"; }
fail() { echo -e "${RED}  ✗${RST} $*" >&2; }
step() { echo -e "\n${BOLD}${MAG}══ $* ${RST}"; }

scenario() {
  local name="$1"
  node "${SCRIPT_DIR}/run-scenario.js" \
    --host "${DEMO_HOST}" --port "${DEMO_PORT}" "${name}"
}

wait_healthy() {
  local service="$1"
  local max="${2:-60}"
  local i=0
  printf "${CYN}  Waiting for %s" "${service}"
  local container
  container=$(docker compose -f "${COMPOSE_BASE}" -f "${COMPOSE_DEMO}" ps -q "${service}" 2>/dev/null | head -1)
  while true; do
    local status
    status=$(docker inspect --format='{{.State.Health.Status}}' "${container}" 2>/dev/null || echo "none")
    if [ "${status}" = "healthy" ]; then
      echo -e " ${GRN}ready${RST}"
      return 0
    fi
    # service has no healthcheck — treat "running" as healthy
    local running
    running=$(docker inspect --format='{{.State.Running}}' "${container}" 2>/dev/null || echo "false")
    if [ "${status}" = "none" ] && [ "${running}" = "true" ]; then
      echo -e " ${GRN}ready${RST}"
      return 0
    fi
    printf "."
    sleep 2
    i=$((i + 2))
    if [ "${i}" -ge "${max}" ]; then
      echo -e " ${RED}timeout${RST}"
      return 1
    fi
  done
}

cmd_start() {
  step "Starting Infrawatch demo stack"
  log "Building and starting containers..."
  docker compose \
    -f "${COMPOSE_BASE}" \
    -f "${COMPOSE_DEMO}" \
    up --build -d

  wait_healthy "redis"      60
  wait_healthy "timescaledb" 90
  wait_healthy "demo-services" 60
  wait_healthy "api" 90

  echo
  ok "Stack is up!"
  echo
  echo -e "  ${BOLD}Dashboard:${RST}   ${FRONTEND_URL}"
  echo -e "  ${BOLD}API:${RST}         ${INFRAWATCH_URL}/api/v1/services"
  echo -e "  ${BOLD}Status page:${RST} ${INFRAWATCH_URL}/api/v1/public/status"
  echo -e "  ${BOLD}Control:${RST}     http://${DEMO_HOST}:${DEMO_PORT}/status"
  echo
  echo -e "  Run a scenario:  ${CYN}bash test/run-demo.sh scenario cascade_failure${RST}"
  echo -e "  Auto-tour:       ${CYN}bash test/run-demo.sh tour${RST}"
  echo -e "  Stop:            ${CYN}bash test/run-demo.sh stop${RST}"
  echo
}

cmd_stop() {
  step "Stopping demo stack"
  docker compose \
    -f "${COMPOSE_BASE}" \
    -f "${COMPOSE_DEMO}" \
    down
  ok "Stopped."
}

cmd_status() {
  node "${SCRIPT_DIR}/run-scenario.js" \
    --host "${DEMO_HOST}" --port "${DEMO_PORT}" status
}

cmd_scenario() {
  local name="${1:-}"
  if [ -z "${name}" ]; then
    node "${SCRIPT_DIR}/run-scenario.js" \
      --host "${DEMO_HOST}" --port "${DEMO_PORT}" list
    return
  fi
  scenario "${name}"
}

cmd_logs() {
  docker compose \
    -f "${COMPOSE_BASE}" \
    -f "${COMPOSE_DEMO}" \
    logs -f demo-services
}

# ─── Auto-tour ────────────────────────────────────────────────────────────────

cmd_tour() {
  step "Infrawatch Demo Tour"
  echo "This tour runs each scenario in sequence with a pause between each."
  echo "Keep the dashboard open at ${FRONTEND_URL} to watch things happen."
  echo
  read -rp "Press Enter to start the tour (Ctrl-C to abort)..."

  # 1. Cascade failure
  step "Scenario 1/7 — Cascade Failure"
  echo "user-service crashes → order-service & notification-svc follow."
  echo "Watch: circuit breakers open, incidents created, healing webhook fires at t=60s."
  scenario "cascade_failure"
  echo; read -rp "Watching... press Enter when ready for the next scenario (or wait ~80s)..."

  scenario "reset"

  # 2. Payment degraded
  step "Scenario 2/7 — Payment Service Degraded"
  echo "payment-service response time spikes to ~2.6s, triggering the anomaly detector."
  echo "Watch: StateBadge → DEGRADED, anomaly event in live feed, alert fired."
  scenario "payment_degraded"
  echo; read -rp "Watching... press Enter when ready (or wait ~100s)..."

  scenario "reset"

  # 3. Flapping service
  step "Scenario 3/7 — Flapping Service"
  echo "notification-svc alternates HEALTHY/DEAD every 15s for 3 minutes."
  echo "Watch: circuit breaker cycling CLOSED → OPEN → HALF_OPEN → CLOSED."
  scenario "flapping_service"
  echo; read -rp "Watching... press Enter when ready (or wait ~3m)..."

  scenario "reset"

  # 4. Gradual degradation
  step "Scenario 4/7 — Gradual Degradation"
  echo "inventory-service latency climbs to 3s over 2 minutes (DB regression)."
  echo "Watch: MetricChart slope, anomaly alert fires when multiplier exceeded."
  scenario "gradual_degradation"
  echo; read -rp "Watching... press Enter when ready (or wait ~2.5m)..."

  scenario "reset"

  # 5. Search overload
  step "Scenario 5/7 — Search Overload"
  echo "search-service error rate 40% + high latency. Simulates Redis pool exhaustion."
  echo "Watch: state → UNHEALTHY, circuit opens, clears at 45s."
  scenario "search_overload"
  echo; read -rp "Watching... press Enter when ready (or wait ~55s)..."

  scenario "reset"

  # 6. Multi-service failure
  step "Scenario 6/7 — Multi-Service Failure"
  echo "product-catalog, payment-service, and search-service all die simultaneously."
  echo "Watch: dependency graph lights up red, 3 incidents opened, staggered recovery."
  scenario "multi_service_failure"
  echo; read -rp "Watching... press Enter when ready (or wait ~80s)..."

  scenario "reset"

  # 7. Full outage
  step "Scenario 7/7 — Full Outage"
  echo "api-gateway goes completely down for 2 minutes."
  echo "Watch: public status page shows OUTAGE, all downstream services infer failure."
  scenario "full_outage"
  echo; read -rp "Watching... press Enter when ready (or wait ~2m)..."

  scenario "reset"

  step "Tour complete!"
  ok "All scenarios demonstrated. Run 'bash test/run-demo.sh stop' when finished."
}

# ─── Dispatch ─────────────────────────────────────────────────────────────────

CMD="${1:-help}"
shift || true

case "${CMD}" in
  start)    cmd_start ;;
  stop)     cmd_stop ;;
  status)   cmd_status ;;
  scenario) cmd_scenario "${1:-}" ;;
  tour)     cmd_tour ;;
  logs)     cmd_logs ;;
  *)
    echo
    echo -e "${BOLD}Infrawatch demo runner${RST}"
    echo
    echo "Usage: bash test/run-demo.sh <command>"
    echo
    echo "Commands:"
    echo "  start              Build and start the demo stack"
    echo "  stop               Tear down the demo stack"
    echo "  status             Print current service states"
    echo "  scenario [name]    Trigger a named scenario (omit name to list)"
    echo "  tour               Run all scenarios interactively"
    echo "  logs               Tail demo-services logs"
    echo
    echo "Available scenarios:"
    echo "  cascade_failure      user-service crashes, dependants follow"
    echo "  payment_degraded     payment-service latency spike → anomaly"
    echo "  flapping_service     notification-svc flaps for 3 min"
    echo "  gradual_degradation  inventory-service slow latency climb"
    echo "  search_overload      search-service error rate + latency spike"
    echo "  full_outage          api-gateway down for 2 min"
    echo "  multi_service_failure three unrelated services fail"
    echo "  rolling_recovery     bring all services back one-by-one"
    echo "  reset                immediately heal everything"
    echo
    ;;
esac
