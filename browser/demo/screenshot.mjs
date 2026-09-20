// Regenerates demo/screenshot.png: publish and watch against a real WHIP server.
// Needs MediaMTX (see e2e/WHIP.md); fails rather than shooting an idle page.
import { chromium } from '@playwright/test';
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';

const PORT = Number(process.env.DEMO_PORT ?? 8088);
const API = process.env.MEDIAMTX_API ?? 'http://127.0.0.1:9997';
const WHIP = process.env.WHIP_URL ?? 'http://127.0.0.1:8889/seimark/whip';
const SHOT = new URL('screenshot.png', import.meta.url).pathname;
const WANT_MARKERS = 100;
const COMPOSE = 'docker compose -f e2e/docker-compose.yml up -d';

async function answers(url, init) {
  try {
    await fetch(url, { ...init, signal: AbortSignal.timeout(2000) });

    return true;
  } catch {
    return false;
  }
}

// A server without its API port exposed still serves WHIP, so either answer will do.
if (
  !(await answers(`${API}/v3/config/global/get`)) &&
  !(await answers(WHIP, { method: 'OPTIONS' }))
) {
  console.error(`No media server answering at ${API} or ${WHIP}. From browser/, run:\n  ${COMPOSE}`);
  process.exit(1);
}

function portTaken(port) {
  return new Promise((resolve) => {
    const probe = createServer();
    probe.once('error', () => resolve(true));
    probe.once('listening', () => probe.close(() => resolve(false)));
    probe.listen(port, '127.0.0.1');
  });
}

const reuse = await portTaken(PORT);
const server = reuse
  ? null
  : spawn('node', [new URL('serve.mjs', import.meta.url).pathname, String(PORT)], {
      cwd: new URL('..', import.meta.url).pathname,
      stdio: 'ignore',
    });
if (server !== null) await new Promise((r) => setTimeout(r, 1500));

const browser = await chromium.launch({
  args: [
    '--use-fake-device-for-media-stream',
    '--use-fake-ui-for-media-stream',
    '--autoplay-policy=no-user-gesture-required',
  ],
});

try {
  const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });
  await page.goto(`http://127.0.0.1:${PORT}/demo/`);

  await page.click('#start');
  await page.click('#watch-start');
  await page.waitForFunction(
    (want) => Number(document.getElementById('w-markers').textContent) > want,
    WANT_MARKERS,
    { timeout: 60_000 },
  );

  await page.screenshot({ path: SHOT, fullPage: true });
  const seen = await page.evaluate(() => ({
    markers: document.getElementById('w-markers').textContent,
    frames: document.getElementById('w-frames').textContent,
  }));
  process.stdout.write(`${SHOT}: watch saw ${seen.frames} frames, ${seen.markers} markers\n`);

  await page.click('#watch-stop').catch(() => undefined);
  await page.click('#stop').catch(() => undefined);
  await page.waitForTimeout(1000);
} finally {
  await browser.close();
  server?.kill();
}
