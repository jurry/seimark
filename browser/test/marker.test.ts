import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decodeMarker, encodeMarker, isFormatUUID, FORMAT_UUID } from '../src/marker.ts';
import { SeimarkError } from '../src/errors.ts';

const base = {
  timeSource: 'send' as const,
  originTimeUs: 1789246800000000n,
  sequence: 0,
  streamId: new Uint8Array([0x9f, 0x3c, 0x1a, 0x77, 0xe2, 0xb0, 0x4d, 0x51]),
  payload: null,
};

test('round trip preserves every field', () => {
  const m = { ...base, sequence: 4294967295, payload: new Uint8Array([1, 2, 3]) };
  assert.deepEqual(decodeMarker(encodeMarker(m)), m);
});

test('an empty non-null payload sets the flag with length zero', () => {
  const out = encodeMarker({ ...base, payload: new Uint8Array() });
  assert.equal(out.length, 24);
  assert.equal(out[1]! & 0b10, 0b10);
  assert.deepEqual(decodeMarker(out).payload, new Uint8Array());
});

test('reserved flag bits are ignored on read', () => {
  const body = encodeMarker(base);
  body[1] = 0b1111_1100;
  assert.equal(decodeMarker(body).timeSource, 'send');
});

test('trailing bytes after the body are ignored', () => {
  const body = encodeMarker(base);
  const padded = new Uint8Array(body.length + 5);
  padded.set(body);
  assert.equal(decodeMarker(padded).sequence, 0);
});

test('a payload over the hard limit is payload_too_large', () => {
  let err: SeimarkError | undefined;
  try { encodeMarker({ ...base, payload: new Uint8Array(65536) }); } catch (e) { err = e as SeimarkError; }
  assert.equal(err?.code, 'payload_too_large');
});

test('a stream id of the wrong length is rejected', () => {
  let err: SeimarkError | undefined;
  try { encodeMarker({ ...base, streamId: new Uint8Array(7) }); } catch (e) { err = e as SeimarkError; }
  assert.equal(err?.code, 'invalid_argument');
});

test('isFormatUUID matches only the format uuid', () => {
  assert.ok(isFormatUUID(FORMAT_UUID));
  const other = Uint8Array.from(FORMAT_UUID);
  other[0] ^= 1;
  assert.equal(isFormatUUID(other), false);
});
