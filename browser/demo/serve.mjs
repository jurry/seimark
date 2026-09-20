import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';

// Serves browser/, not browser/demo/: the page imports ../dist/webrtc/index.js.
const root = fileURLToPath(new URL('..', import.meta.url));
const port = Number(process.argv[2] ?? 8088);
const api = process.env.MEDIAMTX_API ?? 'http://127.0.0.1:9997';

const types = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.map': 'application/json; charset=utf-8',
};

// From Node, so no browser CORS is involved: the page's own check can only be weaker.
async function reportServer() {
  try {
    // Any HTTP answer means a server is there; the API may well reply 401.
    await fetch(`${api}/v3/config/global/get`, { signal: AbortSignal.timeout(2000) });
    process.stdout.write(
      'MediaMTX is up\n' +
        '  WHIP: http://127.0.0.1:8889/seimark/whip\n' +
        '  WHEP: http://127.0.0.1:8889/seimark/whep\n',
    );
  } catch {
    process.stdout.write(
      `no media server answering at ${api}; start one from browser/ with:\n` +
        '  docker compose -f e2e/docker-compose.yml up -d\n',
    );
  }
}

await reportServer();

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
