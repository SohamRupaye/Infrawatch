const http = require('http');
const url = require('url');

// Service states: true = healthy (200), false = dead (500)
const states = {
  auth: true,
  db: true,
  api: true
};

const server = http.createServer((req, res) => {
  const parsedUrl = url.parse(req.url, true);
  const path = parsedUrl.pathname;

  // Endpoint to break a service (e.g. /break/auth)
  if (path.startsWith('/break/')) {
    const service = path.split('/')[2];
    if (states[service] !== undefined) {
      states[service] = false;
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ message: `${service} is now BROKEN` }));
      return;
    }
  }

  // Endpoint to heal a service (e.g. /heal/auth)
  if (path.startsWith('/heal/')) {
    const service = path.split('/')[2];
    if (states[service] !== undefined) {
      states[service] = true;
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ message: `${service} is now HEALTHY` }));
      return;
    }
  }

  // Health check endpoints for Infrawatch to hit
  if (path === '/health/auth') {
    if (states.auth) {
      res.writeHead(200);
      res.end('OK');
    } else {
      res.writeHead(500);
      res.end('Internal Server Error');
    }
    return;
  }

  if (path === '/health/db') {
    if (states.db) {
      res.writeHead(200);
      res.end('OK');
    } else {
      res.writeHead(500);
      res.end('Internal Server Error');
    }
    return;
  }

  if (path === '/health/api') {
    // API Gateway depends on Auth and DB in our "demo"
    // So if auth or db is down, maybe api should be degraded?
    // Let's just return 200 unless it's explicitly broken.
    if (states.api) {
      res.writeHead(200);
      res.end('OK');
    } else {
      res.writeHead(500);
      res.end('Internal Server Error');
    }
    return;
  }

  res.writeHead(404);
  res.end('Not Found');
});

const PORT = 4000;
server.listen(PORT, '0.0.0.0', () => {
  console.log(`Demo services running on port ${PORT}`);
});
