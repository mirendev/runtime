const http = require('http');
const port = process.env.PORT || 3000;

const server = http.createServer((req, res) => {
  // Return different status codes based on path
  if (req.url === '/health') {
    // Health check endpoint - always fast and successful
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
      status: 'healthy',
      timestamp: new Date().toISOString(),
      uptime: process.uptime()
    }));
  } else if (req.url === '/error500') {
    res.writeHead(500, { 'Content-Type': 'text/plain' });
    res.end('Internal Server Error\n');
  } else if (req.url === '/error404') {
    res.writeHead(404, { 'Content-Type': 'text/plain' });
    res.end('Not Found\n');
  } else if (req.url === '/error400') {
    res.writeHead(400, { 'Content-Type': 'text/plain' });
    res.end('Bad Request\n');
  } else if (req.url === '/slow') {
    // Simulate slow endpoint
    setTimeout(() => {
      res.writeHead(200, { 'Content-Type': 'text/plain' });
      res.end('Slow response\n');
    }, 2000);
  } else {
    res.writeHead(200, { 'Content-Type': 'text/plain' });
    res.end('OK\n');
  }
});

server.listen(port, () => {
  console.log(`Error test server running at http://localhost:${port}/`);
  console.log('Available endpoints:');
  console.log('  / - 200 OK');
  console.log('  /error400 - 400 Bad Request');
  console.log('  /error404 - 404 Not Found');
  console.log('  /error500 - 500 Internal Server Error');
  console.log('  /slow - 200 OK (2s delay)');
});