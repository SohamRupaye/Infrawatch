#!/usr/bin/env node
/**
 * Infrawatch scenario runner
 *
 * Triggers named failure scenarios against the demo-services control plane.
 * Can be used interactively or scripted into run-demo.sh.
 *
 * Usage:
 *   node test/run-scenario.js [--host HOST] [--port PORT] <scenario>
 *   node test/run-scenario.js list
 *   node test/run-scenario.js status
 *
 * Scenarios:
 *   cascade_failure       user-service crashes → dependant cascade
 *   payment_degraded      payment-service latency spike → anomaly alert
 *   flapping_service      notification-svc flaps every 15 s for 3 min
 *   gradual_degradation   inventory-service latency climbs over 2 min
 *   search_overload       search-service error rate + latency spike
 *   full_outage           api-gateway goes completely down for 2 min
 *   multi_service_failure three unrelated services fail simultaneously
 *   rolling_recovery      bring all services back one-by-one
 *   reset                 immediately heal everything
 */

'use strict';

const http = require('http');

// ─── CLI parsing ──────────────────────────────────────────────────────────────

const args = process.argv.slice(2);
let host     = 'localhost';
let port     = 4000;
let scenario = null;

for (let i = 0; i < args.length; i++) {
  if (args[i] === '--host' && args[i + 1]) { host = args[++i]; continue; }
  if (args[i] === '--port' && args[i + 1]) { port = parseInt(args[++i], 10); continue; }
  scenario = args[i];
}

if (!scenario) {
  console.error('Usage: node run-scenario.js [--host HOST] [--port PORT] <scenario|list|status>');
  process.exit(1);
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

function httpRequest(method, path, body) {
  return new Promise((resolve, reject) => {
    const payload  = body ? JSON.stringify(body) : null;
    const options  = {
      hostname: host,
      port,
      path,
      method,
      headers: {
        'Content-Type':   'application/json',
        'Content-Length': payload ? Buffer.byteLength(payload) : 0,
      },
    };
    const req = http.request(options, res => {
      let data = '';
      res.on('data', c => (data += c));
      res.on('end', () => {
        try {
          resolve({ status: res.statusCode, body: JSON.parse(data) });
        } catch (_) {
          resolve({ status: res.statusCode, body: data });
        }
      });
    });
    req.on('error', reject);
    if (payload) req.write(payload);
    req.end();
  });
}

// ─── Commands ─────────────────────────────────────────────────────────────────

async function listScenarios() {
  const res = await httpRequest('GET', '/status');
  if (res.status !== 200) {
    console.error(`Failed to reach demo-services at ${host}:${port}`);
    process.exit(1);
  }
  console.log('\nAvailable scenarios:');
  for (const s of res.body.availableScenarios) {
    console.log(`  ${s}`);
  }
  console.log();
}

async function printStatus() {
  const res = await httpRequest('GET', '/status');
  if (res.status !== 200) {
    console.error(`Failed to reach demo-services at ${host}:${port}`);
    process.exit(1);
  }
  const { services } = res.body;
  console.log('\nService states:');
  const col = Math.max(...Object.keys(services).map(k => k.length));
  for (const [name, info] of Object.entries(services)) {
    const stateIcon = info.state === 'healthy' ? '✓' : info.state === 'degraded' ? '~' : '✗';
    const extra     = info.degradeMs > 0 ? ` (+${info.degradeMs}ms)` : '';
    const errRate   = info.errorRate > 0 ? ` err=${(info.errorRate * 100).toFixed(0)}%` : '';
    console.log(`  ${stateIcon} ${name.padEnd(col)}  ${info.state}${extra}${errRate}`);
  }
  console.log();
}

async function triggerScenario(name) {
  console.log(`\nTriggering scenario: ${name} → http://${host}:${port}`);
  const res = await httpRequest('POST', `/scenario/${name}`);
  if (res.status === 200) {
    console.log(`✓ ${res.body.message}`);
  } else if (res.status === 404) {
    console.error(`✗ Unknown scenario '${name}'.`);
    console.error(`  Available: ${res.body.available.join(', ')}`);
    process.exit(1);
  } else {
    console.error(`✗ Unexpected response ${res.status}:`, res.body);
    process.exit(1);
  }
}

// ─── Main ─────────────────────────────────────────────────────────────────────

(async () => {
  try {
    if (scenario === 'list') {
      await listScenarios();
    } else if (scenario === 'status') {
      await printStatus();
    } else {
      await triggerScenario(scenario);
    }
  } catch (err) {
    if (err.code === 'ECONNREFUSED') {
      console.error(`\n✗ Cannot connect to demo-services at ${host}:${port}`);
      console.error('  Is the demo stack running?');
      console.error('  Run: bash test/run-demo.sh start');
    } else {
      console.error('\n✗ Error:', err.message);
    }
    process.exit(1);
  }
})();
