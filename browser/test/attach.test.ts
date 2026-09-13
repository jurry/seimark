import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { readFileSync, existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { trackSequence, reader } from '../src/webrtc/reader.ts';
import { SeimarkError } from '../src/errors.ts';

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

test('reader() throws SeimarkError with code unsupported_browser outside a browser', () => {
  const fakeReceiver = {} as RTCRtpReceiver;
  let caught: unknown;
  try {
    reader(fakeReceiver, () => {});
  } catch (e) {
    caught = e;
  }
  assert.ok(caught instanceof SeimarkError);
  assert.equal((caught as SeimarkError).code, 'unsupported_browser');
});

test('on the worker path, framesSeen counts frames, not markers', () => {
  class FakeWorker {
    listeners: Record<string, Array<(event: { data: unknown }) => void>> = {};
    addEventListener(type: string, cb: (event: { data: unknown }) => void): void {
      (this.listeners[type] ??= []).push(cb);
    }
    postMessage(): void {}
    terminate(): void {}
    emit(type: string, data: unknown): void {
      for (const cb of this.listeners[type] ?? []) cb({ data });
    }
  }

  let created: FakeWorker | undefined;
  const globals = globalThis as unknown as {
    RTCRtpScriptTransform?: unknown;
    Worker?: unknown;
  };
  const previousCtor = globals.RTCRtpScriptTransform;
  const previousWorker = globals.Worker;

  globals.RTCRtpScriptTransform = class {
    worker: unknown;
    options: unknown;
    constructor(worker: unknown, options: unknown) {
      this.worker = worker;
      this.options = options;
    }
  };
  globals.Worker = class extends FakeWorker {
    constructor() {
      super();
      created = this;
    }
  };

  try {
    const fakeReceiver = {} as RTCRtpReceiver;
    const rh = reader(fakeReceiver, () => {});
    const worker = created!;

    const marker = {
      timeSource: 'send' as const,
      originTimeUs: 0n,
      sequence: 0,
      streamId: Uint8Array.of(1, 2, 3, 4, 5, 6, 7, 8),
      payload: null,
    };

    worker.emit('message', { kind: 'frames', count: 4 });
    worker.emit('message', { kind: 'marker', marker });
    worker.emit('message', { kind: 'frames', count: 8 });

    assert.equal(rh.stats.framesSeen, 8);
    assert.equal(rh.stats.markersFound, 1);
  } finally {
    globals.RTCRtpScriptTransform = previousCtor;
    globals.Worker = previousWorker;
  }
});
