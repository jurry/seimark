import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { readFileSync, existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { trackSequence, reader } from '../src/webrtc/reader.ts';
import { attach } from '../src/webrtc/attach.ts';
import { SeimarkError } from '../src/errors.ts';

const root = fileURLToPath(new URL('..', import.meta.url));

/**
 * Runs body with the standard transform API and just enough of the worker
 * plumbing present for attach to take the worker path under node:test.
 * `makeWorker` stands in for the Worker constructor, so a test can make it throw
 * the way a content security policy does.
 */
function withTransform(body: () => void, makeWorker: () => unknown = () => ({
  addEventListener: () => {},
  postMessage: () => {},
  terminate: () => {},
})): void {
  const globals = globalThis as unknown as {
    RTCRtpScriptTransform?: unknown;
    Worker?: unknown;
    Blob?: unknown;
    URL?: unknown;
  };
  const previous = {
    ctor: globals.RTCRtpScriptTransform,
    worker: globals.Worker,
    url: globals.URL,
  };

  globals.RTCRtpScriptTransform = class {
    constructor(_worker: unknown, _options: unknown) {}
  };
  globals.Worker = function (this: unknown) {
    return makeWorker();
  };
  globals.URL = class {
    static createObjectURL(): string {
      return 'blob:seimark-test';
    }
    static revokeObjectURL(): void {}
  };

  try {
    body();
  } finally {
    globals.RTCRtpScriptTransform = previous.ctor;
    globals.Worker = previous.worker;
    globals.URL = previous.url;
  }
}

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

// The four conditions attach settles before any frame flows. Each throws
// synchronously, so none of them needs a peer connection.
test('attach throws unsupported_browser when the browser has neither transform API', () => {
  const globals = globalThis as unknown as { RTCRtpScriptTransform?: unknown };
  const previous = globals.RTCRtpScriptTransform;
  delete globals.RTCRtpScriptTransform;

  try {
    const sender = { track: {} } as unknown as RTCRtpSender;
    assert.throws(
      () => attach(sender),
      (e: unknown) => e instanceof SeimarkError && e.code === 'unsupported_browser',
    );
  } finally {
    globals.RTCRtpScriptTransform = previous;
  }
});

test('attach throws invalid_argument for a stream id that is not 8 bytes', () => {
  withTransform(() => {
    const sender = { track: {} } as unknown as RTCRtpSender;
    assert.throws(
      () => attach(sender, { streamId: Uint8Array.of(1, 2, 3) }),
      (e: unknown) => e instanceof SeimarkError && e.code === 'invalid_argument',
    );
  });
});

test('attach throws invalid_argument for a sender with no track', () => {
  withTransform(() => {
    const sender = { track: null } as unknown as RTCRtpSender;
    assert.throws(
      () => attach(sender),
      (e: unknown) => e instanceof SeimarkError && e.code === 'invalid_argument',
    );
  });
});

test('attach throws already_attached for a sender that is already attached', () => {
  withTransform(() => {
    const sender = { track: {}, transform: null } as unknown as RTCRtpSender;
    attach(sender);
    assert.throws(
      () => attach(sender),
      (e: unknown) => e instanceof SeimarkError && e.code === 'already_attached',
    );
  });
});

test('attach throws csp_blocked when the policy refuses the blob worker', () => {
  withTransform(
    () => {
      const sender = { track: {}, transform: null } as unknown as RTCRtpSender;
      assert.throws(
        () => attach(sender),
        (e: unknown) => e instanceof SeimarkError && e.code === 'csp_blocked',
      );
    },
    () => {
      throw new Error('Refused to create a worker from blob:');
    },
  );
});

test('a sender can be attached again after detach on the worker path', () => {
  withTransform(() => {
    const sender = { track: {}, transform: null } as unknown as RTCRtpSender;
    attach(sender).detach();
    assert.doesNotThrow(() => attach(sender));
  });
});
