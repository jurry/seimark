import { test } from 'node:test';
import assert from 'node:assert/strict';
import { FrameHandler } from '../src/webrtc/handle.ts';
import { Writer } from '../src/writer.ts';
import { markersIn } from '../src/scan.ts';

const IDR = Uint8Array.of(0x65, 0x88);
const P = Uint8Array.of(0x41, 0x88);
const ID = Uint8Array.of(1, 2, 3, 4, 5, 6, 7, 8);

const annexb = (...units: Uint8Array[]) =>
  Uint8Array.from(units.flatMap((u) => [0x00, 0x00, 0x00, 0x01, ...u]));

function frame(au: Uint8Array, type: 'key' | 'delta' = 'key', captureTime?: number) {
  const copy = Uint8Array.from(au);
  return {
    data: copy.buffer.slice(0) as ArrayBuffer,
    type,
    getMetadata: () => (captureTime === undefined ? {} : { captureTime }),
  };
}

function handler(overrides: Partial<{ keyframesOnly: boolean }> = {}) {
  const warnings: Array<[string, string]> = [];
  const h = new FrameHandler({
    writer: new Writer({ streamId: ID, ...overrides }),
    keyframesOnly: overrides.keyframesOnly ?? false,
    nowUs: () => 1789246800000000n,
    captureUs: (t) => BigInt(Math.round(t * 1000)),
    onWarn: (code, message) => warnings.push([code, message]),
  });
  return { h, warnings };
}

test('marks a frame and updates lastMarker and stats', () => {
  const { h } = handler();
  const f = frame(annexb(IDR));
  h.handle(f);
  const markers = markersIn(new Uint8Array(f.data), 'annexb').markers;
  assert.equal(markers.length, 1);
  assert.equal(h.lastMarker?.sequence, 0);
  assert.equal(h.stats.framesSeen, 1);
  assert.equal(h.stats.framesMarked, 1);
});

test('probe chooses capture when captureTime is present', () => {
  const { h } = handler();
  assert.equal(h.probe(frame(annexb(IDR), 'key', 1234)), 'capture');
});

test('probe chooses send when captureTime is absent', () => {
  const { h } = handler();
  assert.equal(h.probe(frame(annexb(IDR))), 'send');
});

test('the verdict is taken from the first frame and never changes', () => {
  const { h } = handler();
  assert.equal(h.probe(frame(annexb(IDR))), 'send');
  assert.equal(h.probe(frame(annexb(IDR), 'key', 1234)), 'send');
});

test('a capture verdict is written into every marker flag', () => {
  const { h } = handler();
  const f = frame(annexb(IDR), 'key', 1234);
  h.handle(f);
  const m = markersIn(new Uint8Array(f.data), 'annexb').markers[0]!;
  assert.equal(m.timeSource, 'capture');
  assert.equal(m.originTimeUs, 1234000n);
});

test('a frame that cannot be marked passes through and is counted', () => {
  const { h, warnings } = handler();
  const f = frame(annexb(Uint8Array.of(0x67, 0x11)));
  const before = Uint8Array.from(new Uint8Array(f.data));
  h.handle(f);
  assert.deepEqual(new Uint8Array(f.data), before);
  assert.equal(h.stats.errors, 1);
  assert.equal(warnings.length, 1);
  assert.equal(warnings[0]?.[0], 'no_vcl');
});

test('an unmarkable frame warns only on the first occurrence', () => {
  const { h, warnings } = handler();
  h.handle(frame(annexb(Uint8Array.of(0x67, 0x11))));
  h.handle(frame(annexb(Uint8Array.of(0x67, 0x11))));
  h.handle(frame(annexb(Uint8Array.of(0x67, 0x11))));
  assert.equal(h.stats.errors, 3);
  assert.equal(warnings.length, 1);
});

test('a page onWarn callback that throws does not stop handling', () => {
  const h = new FrameHandler({
    writer: new Writer({ streamId: ID }),
    keyframesOnly: false,
    nowUs: () => 1789246800000000n,
    captureUs: (t) => BigInt(Math.round(t * 1000)),
    onWarn: () => {
      throw new Error('page bug');
    },
  });
  assert.doesNotThrow(() => {
    h.handle(frame(annexb(Uint8Array.of(0x67, 0x11))));
  });
  assert.equal(h.stats.errors, 1);
});

test('a page onWarn callback that throws does not stop setPayload', () => {
  const h = new FrameHandler({
    writer: new Writer({ streamId: ID }),
    keyframesOnly: false,
    nowUs: () => 1789246800000000n,
    captureUs: (t) => BigInt(Math.round(t * 1000)),
    onWarn: () => {
      throw new Error('page bug');
    },
  });
  assert.doesNotThrow(() => {
    h.setPayload(new Uint8Array(5000));
  });
});

test('an empty frame type passes through without an error', () => {
  const { h } = handler();
  const f = { ...frame(annexb(IDR)), type: 'empty' as const };
  h.handle(f);
  assert.equal(h.stats.framesMarked, 0);
  assert.equal(h.stats.errors, 0);
});

test('passthrough stops marking but keeps frames flowing', () => {
  const { h } = handler();
  h.passthrough = true;
  const f = frame(annexb(IDR));
  const before = Uint8Array.from(new Uint8Array(f.data));
  h.handle(f);
  assert.deepEqual(new Uint8Array(f.data), before);
  assert.equal(h.stats.framesMarked, 0);
});

test('the payload set by the page is written into the marker', () => {
  const { h } = handler();
  h.setPayload(Uint8Array.of(9, 9));
  const f = frame(annexb(IDR));
  h.handle(f);
  assert.deepEqual(markersIn(new Uint8Array(f.data), 'annexb').markers[0]!.payload, Uint8Array.of(9, 9));
});

test('a repeated error warns once, but a different code warns again', () => {
  const { h, warnings } = handler();
  const noVCL = () => frame(annexb(Uint8Array.of(0x67, 0x11)));

  h.handle(noVCL());
  h.handle(noVCL());
  h.handle(noVCL());
  assert.equal(h.stats.errors, 3);
  assert.equal(warnings.filter(([c]) => c === 'no_vcl').length, 1);

  const marked = frame(annexb(IDR));
  h.handle(marked);
  h.handle({ ...marked, data: marked.data });
  assert.equal(warnings.filter(([c]) => c === 'already_marked').length, 1);
  assert.ok(warnings.length >= 2, 'a second distinct code must still be reported');
});

test('a payload over the soft limit warns once when set, not per frame', () => {
  const { h, warnings } = handler();
  h.setPayload(new Uint8Array(4097));
  h.handle(frame(annexb(IDR)));
  h.handle(frame(annexb(IDR)));
  assert.equal(warnings.filter(([c]) => c === 'payload_above_soft_limit').length, 1);
});

test('sequence stays continuous across frames', () => {
  const { h } = handler();
  for (let i = 0; i < 5; i++) h.handle(frame(annexb(IDR), 'delta'));
  assert.equal(h.lastMarker?.sequence, 4);
});

test('keyframes-only leaves delta frames unmarked', () => {
  const { h } = handler({ keyframesOnly: true });
  h.handle(frame(annexb(P), 'delta'));
  assert.equal(h.stats.framesMarked, 0);
  assert.equal(h.stats.errors, 0);
});
