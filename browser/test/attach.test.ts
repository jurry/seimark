import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { readFileSync, existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { trackSequence } from '../src/webrtc/reader.ts';

const root = fileURLToPath(new URL('..', import.meta.url));

test('the worker build produces a source string containing the codec', () => {
  execFileSync('node', ['build/worker.mjs'], { cwd: root, encoding: 'utf8' });
  const generated = root + 'src/webrtc/worker-source.ts';
  assert.ok(existsSync(generated));
  const text = readFileSync(generated, 'utf8');
  assert.match(text, /export const WORKER_SOURCE/);
  assert.ok(text.length > 1000, 'worker source looks empty');
});

test('trackSequence reports a gap', () => {
  const state = new Map<string, number>();
  assert.deepEqual(trackSequence(state, 'aa', 0), { gap: false, duplicate: false });
  assert.deepEqual(trackSequence(state, 'aa', 1), { gap: false, duplicate: false });
  assert.deepEqual(trackSequence(state, 'aa', 5), { gap: true, duplicate: false });
});

test('trackSequence reports a duplicate', () => {
  const state = new Map<string, number>();
  trackSequence(state, 'aa', 0);
  trackSequence(state, 'aa', 1);
  assert.deepEqual(trackSequence(state, 'aa', 1), { gap: false, duplicate: true });
});

test('trackSequence treats a new stream id as a fresh stream', () => {
  const state = new Map<string, number>();
  trackSequence(state, 'aa', 9);
  assert.deepEqual(trackSequence(state, 'bb', 0), { gap: false, duplicate: false });
  assert.equal(state.size, 2);
});

test('trackSequence handles the uint32 wrap without calling it a gap', () => {
  const state = new Map<string, number>();
  trackSequence(state, 'aa', 0xffffffff);
  assert.deepEqual(trackSequence(state, 'aa', 0), { gap: false, duplicate: false });
});

test('trackSequence reports a gap across the uint32 wrap', () => {
  const state = new Map<string, number>();
  trackSequence(state, 'aa', 0xfffffffe);
  assert.deepEqual(trackSequence(state, 'aa', 2), { gap: true, duplicate: false });
});

test('a duplicate does not move the expected sequence forward', () => {
  const state = new Map<string, number>();
  trackSequence(state, 'aa', 5);
  trackSequence(state, 'aa', 3);
  assert.deepEqual(trackSequence(state, 'aa', 6), { gap: false, duplicate: false });
});
