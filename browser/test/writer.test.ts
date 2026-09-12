import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Writer } from '../src/writer.ts';
import { markersIn, stripMarkers } from '../src/scan.ts';
import { nalUnits, nalType } from '../src/nal.ts';
import { SeimarkError } from '../src/errors.ts';

const AUD = Uint8Array.of(0x09, 0x10);
const SPS = Uint8Array.of(0x67, 0x11);
const IDR = Uint8Array.of(0x65, 0x88);
const P = Uint8Array.of(0x41, 0x88);
const ID = Uint8Array.of(1, 2, 3, 4, 5, 6, 7, 8);

const annexb = (...units: Uint8Array[]) =>
  Uint8Array.from(units.flatMap((u) => [0x00, 0x00, 0x00, 0x01, ...u]));

test('marks before the first vcl unit and after the parameter sets', () => {
  const w = new Writer({ streamId: ID });
  const { data, marked } = w.mark(annexb(AUD, SPS, IDR), 'annexb', 100n, true);
  assert.ok(marked);
  const types = nalUnits(data, 'annexb').map((u) => nalType(u.data));
  assert.deepEqual(types, [9, 7, 6, 5]);
});

test('the sequence advances only on marked units', () => {
  const w = new Writer({ streamId: ID, keyframesOnly: true });
  assert.equal(w.sequence, 0);
  w.mark(annexb(SPS, IDR), 'annexb', 1n, true);
  assert.equal(w.sequence, 1);
  const r = w.mark(annexb(P), 'annexb', 2n, false);
  assert.equal(r.marked, false);
  assert.equal(w.sequence, 1);
});

test('a non-keyframe passes through unchanged in keyframes-only mode', () => {
  const w = new Writer({ streamId: ID, keyframesOnly: true });
  const au = annexb(P);
  assert.deepEqual(w.mark(au, 'annexb', 1n, false).data, au);
});

test('the marker carries the writer stream id, time and sequence', () => {
  const w = new Writer({ streamId: ID });
  w.mark(annexb(IDR), 'annexb', 5n, true);
  const { data } = w.mark(annexb(IDR), 'annexb', 1789246800000000n, true);
  const m = markersIn(data, 'annexb').markers[0]!;
  assert.equal(m.sequence, 1);
  assert.equal(m.originTimeUs, 1789246800000000n);
  assert.deepEqual(m.streamId, ID);
  assert.equal(m.timeSource, 'send');
});

test('capture time source is written into the flags', () => {
  const w = new Writer({ streamId: ID, timeSource: 'capture' });
  const { data } = w.mark(annexb(IDR), 'annexb', 1n, true);
  assert.equal(markersIn(data, 'annexb').markers[0]!.timeSource, 'capture');
});

test('marking an already marked unit is already_marked', () => {
  const w = new Writer({ streamId: ID });
  const once = w.mark(annexb(IDR), 'annexb', 1n, true).data;
  let err: SeimarkError | undefined;
  try { w.mark(once, 'annexb', 2n, true); } catch (e) { err = e as SeimarkError; }
  assert.equal(err?.code, 'already_marked');
});

test('strip then mark again gives the same bytes', () => {
  const a = new Writer({ streamId: ID });
  const first = a.mark(annexb(SPS, IDR), 'annexb', 7n, true).data;
  const b = new Writer({ streamId: ID });
  const again = b.mark(stripMarkers(first, 'annexb'), 'annexb', 7n, true).data;
  assert.deepEqual(again, first);
});

test('an access unit with no vcl unit is no_vcl', () => {
  const w = new Writer({ streamId: ID });
  let err: SeimarkError | undefined;
  try { w.mark(annexb(SPS), 'annexb', 1n, false); } catch (e) { err = e as SeimarkError; }
  assert.equal(err?.code, 'no_vcl');
});

test('keyframes-only skips a non-keyframe before looking for a vcl unit', () => {
  const w = new Writer({ streamId: ID, keyframesOnly: true });
  const r = w.mark(annexb(SPS), 'annexb', 1n, false);
  assert.equal(r.marked, false);
});

test('a payload over the soft limit is marked and warned about', () => {
  const w = new Writer({ streamId: ID });
  const r = w.mark(annexb(IDR), 'annexb', 1n, true, new Uint8Array(4097));
  assert.ok(r.marked);
  assert.equal(r.warning?.code, 'payload_above_soft_limit');
});

test('an omitted stream id is eight random bytes', () => {
  const a = new Writer();
  const b = new Writer();
  assert.equal(a.streamId.length, 8);
  assert.notDeepEqual(a.streamId, b.streamId);
});

test('the sequence wraps at 2^32', () => {
  const w = new Writer({ streamId: ID });
  (w as unknown as { seq: number }).seq = 0xffffffff;
  w.mark(annexb(IDR), 'annexb', 1n, true);
  assert.equal(w.sequence, 0);
});

test('marks length prefixed access units too', () => {
  const w = new Writer({ streamId: ID });
  const au = Uint8Array.of(0, 0, 0, 2, 0x65, 0x88);
  const { data } = w.mark(au, 'length', 1n, true);
  assert.equal(markersIn(data, 'length').markers.length, 1);
});
