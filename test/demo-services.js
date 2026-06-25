/**
 * Infrawatch Demo Services
 *
 * Simulates a realistic microservice platform (e-commerce backend):
 *
 *   user-service        – auth & user management          (tags: core, auth)
 *   product-catalog     – product data & pricing          (tags: core, data)
 *   inventory-service   – stock tracking                  (tags: data, warehouse)
 *   payment-service     – payment processing              (tags: business, critical)
 *   order-service       – order orchestration             (tags: business, critical)
 *   notification-svc    – email/SMS dispatch              (tags: messaging)
 *   search-service      – full-text product search        (tags: search)
 *   api-gateway         – external-facing edge router     (tags: edge)
 *
 * Health endpoints: GET /health/<service-name>
 * Control plane:    POST /services/<service-name>/break
 *                   POST /services/<service-name>/heal
 *                   POST /services/<service-name>/degrade   (body: { latencyMs })
 *                   POST /scenario/<name>
 *                   GET  /status
 *
 * Built with zero npm dependencies — runs on node:20-alpine.
 */

'use strict';

const http = require('http');
const url  = require('url');

// ─── Service registry ────────────────────────────────────────────────────────

const services = {
  'user-service': {
    baseLatency:  25,
    jitter:       15,
    state:        'healthy',   // healthy | degraded | dead
    errorRate:    0,           // [0,1] probability of a random 503
    degradeMs:    0,           // extra ms added on top of baseLatency when degraded
  },
  'product-catalog': {
    baseLatency:  30,
    jitter:       20,
    state:        'healthy',
    errorRate:    0,
    degradeMs:    0,
  },
  'inventory-service': {
    baseLatency:  40,
    jitter:       25,
    state:        'healthy',
    errorRate:    0,
    degradeMs:    0,
  },
  'payment-service': {
    baseLatency:  80,
    jitter:       40,
    state:        'healthy',
    errorRate:    0,
    degradeMs:    0,
  },
  'order-service': {
    baseLatency:  60,
    jitter:       30,
    state:        'healthy',
    errorRate:    0,
    degradeMs:    0,
  },
  'notification-svc': {
    baseLatency:  20,
    jitter:       10,
    state:        'healthy',
    errorRate:    0,
    degradeMs:    0,
  },
  'search-service': {
    baseLatency:  55,
    jitter:       30,
    state:        'healthy',
    errorRate:    0,
    degradeMs:    0,
  },
  'api-gateway': {
    baseLatency:  10,
    jitter:       5,
    state:        'healthy',
    errorRate:    0,
    degradeMs:    0,
  },
};

// Scheduled timers held per-scenario so they can be cleared on reset.
let activeTimers = [];

// ─── Helpers ─────────────────────────────────────────────────────────────────

function randInt(min, max) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

function respond(res, status, body) {
  const payload = typeof body === 'string' ? body : JSON.stringify(body);
  res.writeHead(status, {
    'Content-Type': typeof body === 'string' ? 'text/plain' : 'application/json',
    'Content-Length': Buffer.byteLength(payload),
  });
  res.end(payload);
}

function schedule(fn, ms) {
  const t = setTimeout(fn, ms);
  activeTimers.push(t);
  return t;
}

function clearAllTimers() {
  activeTimers.forEach(t => clearTimeout(t));
  activeTimers = [];
}

function setServiceState(name, state) {
  if (!services[name]) return false;
  services[name].state = state;
  console.log(`[demo] ${name} → ${state}`);
  return true;
}

function degradeService(name, extraMs) {
  if (!services[name]) return false;
  services[name].degradeMs = extraMs;
  services[name].state     = 'degraded';
  console.log(`[demo] ${name} → degraded (+${extraMs}ms latency)`);
  return true;
}

function setErrorRate(name, rate) {
  if (!services[name]) return false;
  services[name].errorRate = rate;
  return true;
}

function healAll() {
  clearAllTimers();
  for (const svc of Object.values(services)) {
    svc.state     = 'healthy';
    svc.errorRate = 0;
    svc.degradeMs = 0;
  }
  console.log('[demo] ALL services healed');
}

// ─── Health check handler ─────────────────────────────────────────────────────

function handleHealth(res, name) {
  const svc = services[name];
  if (!svc) { respond(res, 404, 'Unknown service'); return; }

  if (svc.state === 'dead') {
    respond(res, 500, JSON.stringify({ status: 'DOWN', service: name }));
    return;
  }

  // Random error injection (for flapping simulation)
  if (svc.errorRate > 0 && Math.random() < svc.errorRate) {
    respond(res, 503, JSON.stringify({ status: 'ERROR', service: name }));
    return;
  }

  const latency = svc.baseLatency + randInt(0, svc.jitter) + svc.degradeMs;

  setTimeout(() => {
    const body = {
      status:     svc.state === 'degraded' ? 'DEGRADED' : 'OK',
      service:    name,
      latency_ms: latency,
      timestamp:  new Date().toISOString(),
    };
    respond(res, 200, body);
  }, latency);
}

// ─── Scenarios ────────────────────────────────────────────────────────────────

const scenarios = {

  /**
   * cascade_failure
   * user-service crashes → order-service and notification-svc become
   * unhealthy (they depend on it). After 60 s the healing webhook fires
   * and user-service recovers, pulling the dependants back up.
   */
  cascade_failure() {
    clearAllTimers();
    console.log('[scenario] CASCADE FAILURE starting');
    setServiceState('user-service', 'dead');
    // Dependants degrade shortly after (simulating timeout-based health failures)
    schedule(() => setServiceState('order-service',    'dead'), 8000);
    schedule(() => setServiceState('notification-svc', 'dead'), 12000);
    // Healing fires via webhook after 60 s
    schedule(() => {
      console.log('[scenario] CASCADE FAILURE healing...');
      setServiceState('user-service', 'healthy');
      schedule(() => setServiceState('order-service',    'healthy'), 5000);
      schedule(() => setServiceState('notification-svc', 'healthy'), 8000);
    }, 60000);
  },

  /**
   * payment_degraded
   * payment-service becomes extremely slow, triggering the anomaly
   * detector. After 90 s it self-heals.
   */
  payment_degraded() {
    clearAllTimers();
    console.log('[scenario] PAYMENT DEGRADED starting');
    degradeService('payment-service', 2500); // +2500 ms → ~2.6 s response
    schedule(() => {
      console.log('[scenario] PAYMENT DEGRADED recovering');
      services['payment-service'].degradeMs = 0;
      services['payment-service'].state     = 'healthy';
    }, 90000);
  },

  /**
   * flapping_service
   * notification-svc alternates between healthy and dead every 15 s for
   * 3 minutes. Demonstrates circuit breaker open/half-open cycles.
   */
  flapping_service() {
    clearAllTimers();
    console.log('[scenario] FLAPPING SERVICE starting (notification-svc)');
    let flips = 0;
    const max = 12; // 12 flips × 15 s = 3 min
    function flip() {
      if (flips >= max) {
        setServiceState('notification-svc', 'healthy');
        console.log('[scenario] FLAPPING SERVICE ended');
        return;
      }
      const nextState = flips % 2 === 0 ? 'dead' : 'healthy';
      setServiceState('notification-svc', nextState);
      flips++;
      schedule(flip, 15000);
    }
    flip();
  },

  /**
   * gradual_degradation
   * inventory-service latency climbs from baseline to 3 s over 2 minutes,
   * simulating a slow DB query regression or memory leak.
   */
  gradual_degradation() {
    clearAllTimers();
    console.log('[scenario] GRADUAL DEGRADATION starting (inventory-service)');
    const steps     = 12;
    const maxExtraMs = 3000;
    let step = 0;
    function tick() {
      step++;
      const extra = Math.round((step / steps) * maxExtraMs);
      services['inventory-service'].degradeMs = extra;
      services['inventory-service'].state     = extra > 500 ? 'degraded' : 'healthy';
      console.log(`[scenario] GRADUAL DEGRADATION step ${step}/${steps}: +${extra}ms`);
      if (step < steps) {
        schedule(tick, 10000);
      } else {
        console.log('[scenario] GRADUAL DEGRADATION peaked — waiting for alert...');
        schedule(() => {
          console.log('[scenario] GRADUAL DEGRADATION healing');
          services['inventory-service'].degradeMs = 0;
          services['inventory-service'].state     = 'healthy';
        }, 30000);
      }
    }
    tick();
  },

  /**
   * search_overload
   * search-service error rate climbs to 40 % (simulating Redis pool
   * exhaustion) and latency spikes. Clears after 45 s.
   */
  search_overload() {
    clearAllTimers();
    console.log('[scenario] SEARCH OVERLOAD starting');
    setErrorRate('search-service', 0.4);
    degradeService('search-service', 800);
    schedule(() => {
      console.log('[scenario] SEARCH OVERLOAD clearing');
      setErrorRate('search-service', 0);
      services['search-service'].degradeMs = 0;
      services['search-service'].state     = 'healthy';
    }, 45000);
  },

  /**
   * full_outage
   * api-gateway goes down. From Infrawatch's perspective every external
   * call fails. Demonstrates the "DEAD" FSM state + incident generation.
   * Heals after 2 minutes.
   */
  full_outage() {
    clearAllTimers();
    console.log('[scenario] FULL OUTAGE starting (api-gateway)');
    setServiceState('api-gateway', 'dead');
    schedule(() => {
      console.log('[scenario] FULL OUTAGE recovering (api-gateway)');
      setServiceState('api-gateway', 'healthy');
    }, 120000);
  },

  /**
   * multi_service_failure
   * Three unrelated services fail simultaneously. Tests Infrawatch's
   * ability to open multiple circuit breakers and fire grouped alerts.
   * Each recovers at a different time.
   */
  multi_service_failure() {
    clearAllTimers();
    console.log('[scenario] MULTI-SERVICE FAILURE starting');
    setServiceState('product-catalog', 'dead');
    setServiceState('payment-service', 'dead');
    setServiceState('search-service',  'dead');
    schedule(() => {
      console.log('[scenario] MULTI-SERVICE FAILURE — product-catalog recovers');
      setServiceState('product-catalog', 'healthy');
    }, 30000);
    schedule(() => {
      console.log('[scenario] MULTI-SERVICE FAILURE — payment-service recovers');
      setServiceState('payment-service', 'healthy');
    }, 50000);
    schedule(() => {
      console.log('[scenario] MULTI-SERVICE FAILURE — search-service recovers');
      setServiceState('search-service', 'healthy');
    }, 70000);
  },

  /**
   * rolling_recovery
   * Heal all services one-by-one with 10 s gaps. Useful after a
   * multi-failure to narrate a realistic on-call recovery runbook.
   */
  rolling_recovery() {
    clearAllTimers();
    console.log('[scenario] ROLLING RECOVERY starting');
    const names = Object.keys(services);
    names.forEach((name, i) => {
      schedule(() => {
        setServiceState(name, 'healthy');
        services[name].errorRate = 0;
        services[name].degradeMs = 0;
      }, i * 10000);
    });
  },

  /**
   * reset
   * Immediately heal everything and cancel all scheduled scenario steps.
   */
  reset() {
    healAll();
    console.log('[scenario] RESET complete');
  },
};

// ─── HTTP server ──────────────────────────────────────────────────────────────

const server = http.createServer((req, res) => {
  const { pathname } = url.parse(req.url, true);
  const method = req.method.toUpperCase();

  // ── GET /status ─────────────────────────────────────────────────────────
  if (method === 'GET' && pathname === '/status') {
    respond(res, 200, {
      services: Object.fromEntries(
        Object.entries(services).map(([name, s]) => [
          name,
          { state: s.state, errorRate: s.errorRate, degradeMs: s.degradeMs },
        ])
      ),
      availableScenarios: Object.keys(scenarios),
    });
    return;
  }

  // ── GET /health/<name> ───────────────────────────────────────────────────
  const healthMatch = pathname.match(/^\/health\/(.+)$/);
  if (method === 'GET' && healthMatch) {
    handleHealth(res, healthMatch[1]);
    return;
  }

  // ── POST /services/<name>/break ──────────────────────────────────────────
  const breakMatch = pathname.match(/^\/services\/(.+)\/break$/);
  if (method === 'POST' && breakMatch) {
    const ok = setServiceState(breakMatch[1], 'dead');
    respond(res, ok ? 200 : 404, { ok, service: breakMatch[1], state: 'dead' });
    return;
  }

  // ── POST /services/<name>/heal ───────────────────────────────────────────
  const healMatch = pathname.match(/^\/services\/(.+)\/heal$/);
  if (method === 'POST' && healMatch) {
    const name = healMatch[1];
    const ok   = setServiceState(name, 'healthy');
    if (ok) {
      services[name].errorRate = 0;
      services[name].degradeMs = 0;
    }
    respond(res, ok ? 200 : 404, { ok, service: name, state: 'healthy' });
    return;
  }

  // ── POST /services/<name>/degrade ────────────────────────────────────────
  const degradeMatch = pathname.match(/^\/services\/(.+)\/degrade$/);
  if (method === 'POST' && degradeMatch) {
    let body = '';
    req.on('data', c => (body += c));
    req.on('end', () => {
      let latencyMs = 1000;
      try { latencyMs = JSON.parse(body).latencyMs || 1000; } catch (_) {}
      const ok = degradeService(degradeMatch[1], latencyMs);
      respond(res, ok ? 200 : 404, { ok, service: degradeMatch[1], degradeMs: latencyMs });
    });
    return;
  }

  // ── POST /scenario/<name> ────────────────────────────────────────────────
  const scenarioMatch = pathname.match(/^\/scenario\/(.+)$/);
  if (method === 'POST' && scenarioMatch) {
    const name = scenarioMatch[1];
    if (scenarios[name]) {
      scenarios[name]();
      respond(res, 200, { ok: true, scenario: name, message: `Scenario '${name}' started` });
    } else {
      respond(res, 404, {
        ok: false,
        error: `Unknown scenario '${name}'`,
        available: Object.keys(scenarios),
      });
    }
    return;
  }

  // ── Fallthrough ──────────────────────────────────────────────────────────
  respond(res, 404, { error: 'Not found', path: pathname });
});

const PORT = process.env.PORT || 4000;
server.listen(PORT, '0.0.0.0', () => {
  console.log(`[demo] Infrawatch demo services listening on :${PORT}`);
  console.log(`[demo] Services: ${Object.keys(services).join(', ')}`);
  console.log(`[demo] Scenarios: ${Object.keys(scenarios).join(', ')}`);
  console.log(`[demo] Control plane: GET /status | POST /scenario/<name>`);
});
