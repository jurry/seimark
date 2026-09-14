import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';

// Serves browser/, not browser/demo/: the page imports ../dist/webrtc/index.js.
const root = fileURLToPath(new URL('..', import.meta.url));
const port = Number(process.argv[2] ?? 8088);

const types = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.map': 'application/json; charset=utf-8',
};

createServer(async (req, res) => {
  const path = normalize(decodeURIComponent(new URL(req.url, 'http://x').pathname));
  const file = join(root, path.endsWith('/') ? join(path, 'index.html') : path);

  if (!file.startsWith(root)) {
    res.writeHead(403).end('forbidden');

    return;
  }

  try {
    const body = await readFile(file);
    res.writeHead(200, {
      'content-type': types[extname(file)] ?? 'application/octet-stream',
      'cache-control': 'no-store',
    });
    res.end(body);
  } catch {
    res.writeHead(404).end('not found');
  }
}).listen(port, '127.0.0.1', () => {
  process.stdout.write(`demo: http://127.0.0.1:${port}/demo/\n`);
});
