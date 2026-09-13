// End-to-end proof: browser stamps markers -> WHIP media server -> recording -> Go CLI reads them back.
// Needs MediaMTX (see browser/e2e/WHIP.md) running with recording enabled, and a built dist/.
import { chromium } from '@playwright/test';
import { spawn } from 'node:child_process';
import { execFileSync } from 'node:child_process';
import { readdirSync, statSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';

const WHIP = process.env.WHIP_URL ?? 'http://127.0.0.1:8889/seimark/whip';
const RECORDINGS = process.env.RECORDINGS ?? new URL('recordings', import.meta.url).pathname;
const SECONDS = Number(process.env.PUBLISH_SECONDS ?? 15);
const API = process.env.MEDIAMTX_API ?? 'http://127.0.0.1:9997';
const CLI = process.env.SEIMARK_CLI ?? 'seimark';

// Start the WHIP server unless one is already answering, and stop only what we started.
const compose = new URL('.', import.meta.url).pathname;
let started = false;

function reachable() {
  try {
    execFileSync('curl', ['-s', '-o', '/dev/null', '--max-time', '2', `${API}/v3/config/global/get`]);

    return true;
  } catch {
    return false;
  }
}

if (!reachable()) {
  if (process.env.NO_COMPOSE === '1') {
    console.error(`No WHIP server answering at ${API}, and NO_COMPOSE=1. See e2e/WHIP.md.`);
    process.exit(1);
  }
  process.stdout.write('starting the whip server with docker compose…\n');
  try {
    execFileSync('docker', ['compose', 'up', '-d'], { cwd: compose, stdio: 'inherit' });
  } catch {
    console.error(
      'Could not start the WHIP server. Install Docker, or run one yourself and set\n' +
        'WHIP_URL and RECORDINGS. The setup is in e2e/WHIP.md.',
    );
    process.exit(1);
  }
  started = true;
  for (let i = 0; i < 30 && !reachable(); i++) await new Promise((r) => setTimeout(r, 500));
  if (!reachable()) {
    console.error('The WHIP server did not come up in 15s. Try: docker compose -f e2e/docker-compose.yml logs');
    process.exit(1);
  }
}

mkdirSync(RECORDINGS, { recursive: true });
const before = new Set(readdirSync(RECORDINGS));

function shutdown() {
  if (!started) return;
  try {
    execFileSync('docker', ['compose', 'down'], { cwd: compose, stdio: 'ignore' });
  } catch { /* leave it running rather than fail the check over cleanup */ }
}
const server = spawn('node', ['demo/serve.mjs', '8098'], { stdio: 'ignore' });
await new Promise((r) => setTimeout(r, 1500));

const browser = await chromium.launch();
const page = await browser.newPage();
await page.goto('http://127.0.0.1:8098/demo/');
await page.evaluate((u) => {
  document.getElementById('whip').value = u;
}, WHIP);
await page.click('#start');
await new Promise((r) => setTimeout(r, SECONDS * 1000));

const onPage = await page.evaluate(() => {
  const rows = document.body.innerText.split('\n').filter(Boolean);
  const get = (k) => rows[rows.findIndex((x) => x.startsWith(k))]?.replace(/\s+/g, ' ');
  return { seen: get('frames seen'), seq: get('sequence'), errors: get('errors') };
});
await page.click('#stop').catch(() => undefined);
await new Promise((r) => setTimeout(r, 2000));
await browser.close();
server.kill();

const fresh = readdirSync(RECORDINGS).filter((f) => !before.has(f) && f.endsWith('.mp4'));
if (fresh.length === 0) throw new Error('no new recording appeared; is the server recording?');
const file = fresh
  .map((f) => join(RECORDINGS, f))
  .sort((a, b) => statSync(b).mtimeMs - statSync(a).mtimeMs)[0];

const out = execFileSync(CLI, ['dump', file], { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
const rows = out.trim().split('\n').filter(Boolean).map((l) => JSON.parse(l));
if (rows.length === 0) throw new Error('the recording carried no markers');

const seqs = rows.map((r) => r.sequence);
const ids = new Set(rows.map((r) => r.stream_id));
const gaps = seqs.filter((s, i) => i > 0 && s !== seqs[i - 1] + 1).length;
const duplicates = seqs.length - new Set(seqs).size;
const wall = (rows.at(-1).origin_us - rows[0].origin_us) / 1e6;
const container = (rows.at(-1).pts - rows[0].pts) / rows[0].timescale;

console.log(`page:        ${onPage.seen} | ${onPage.seq} | ${onPage.errors}`);
console.log(`recording:   ${file}`);
console.log(`markers:     ${rows.length}  seq ${seqs[0]}..${seqs.at(-1)}`);
console.log(`stream ids:  ${[...ids].join(', ')}`);
console.log(`gaps:        ${gaps}   duplicates: ${duplicates}`);
console.log(`wall clock:  ${wall.toFixed(2)}s   container: ${container.toFixed(2)}s   drift: ${Math.abs(wall - container) * 1000 | 0} ms`);

const problems = [];
if (gaps !== 0) problems.push(`${gaps} sequence gaps`);
if (duplicates !== 0) problems.push(`${duplicates} duplicate sequences`);
if (ids.size !== 1) problems.push(`${ids.size} stream ids, expected 1`);
if (seqs[0] !== 0) problems.push(`first sequence is ${seqs[0]}, expected 0`);
if (Math.abs(wall - container) > 1) problems.push(`clock drift ${(wall - container).toFixed(2)}s over ${wall.toFixed(0)}s`);

shutdown();

if (problems.length > 0) {
  console.error('FAILED: ' + problems.join('; '));
  process.exit(1);
}
console.log('PASS: every frame the browser stamped survived to the recording, in order.');
